// Package reconcile implements completion markers and startup DB↔storage
// reconciliation (BR-COL-009, BR-NFR-017). On completion the engine writes a
// small marker object alongside the done file (file UID, name, counts). On
// startup — or after a database restore behind the storage backend — a
// single-owner pass re-creates any Processed File the DB is missing from its
// marker (recovery, not reprocessing), so processed-file state survives a DB
// point-in-time restore.
package reconcile

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strconv"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// markersDir is the sub-location under the done prefix holding completion markers.
const markersDir = ".markers"

// Marker is the on-storage completion record for one processed file.
type Marker struct {
	FileUID     int64     `json:"fileUid"`
	PipelineID  int64     `json:"pipelineId"`
	Name        string    `json:"name"`
	OutputName  string    `json:"outputName"`
	Size        int64     `json:"size"`
	RecordsIn   int       `json:"recordsIn"`
	RecordsOut  int       `json:"recordsOut"`
	Suspended   int       `json:"suspended"`
	Members     int       `json:"members,omitempty"` // archive members decoded (0 = plain file)
	CompletedAt time.Time `json:"completedAt"`
}

// markerKey is the storage key of a file's completion marker.
func markerKey(donePrefix string, fileUID int64) string {
	return path.Join(donePrefix, markersDir, strconv.FormatInt(fileUID, 10)+".json")
}

// WriteMarker persists a completion marker (best-effort recovery aid; the
// Processed File row remains the authoritative record).
func WriteMarker(ctx context.Context, sg storage.Store, donePrefix string, m Marker) error {
	body, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return sg.Put(ctx, markerKey(donePrefix, m.FileUID), bytes.NewReader(body), storage.Meta{ContentType: "application/json"})
}

// Run performs single-owner startup reconciliation across EVERY pipeline: for
// each pipeline it lists the completion markers under that pipeline's own done
// area (resolved from its Source's storage backend + done dir, falling back to
// the process defaults) and re-creates any Processed File the database is missing
// (idempotent). Returns whether this instance ran it and how many rows it
// recovered.
func Run(ctx context.Context, db store.Store, defaultSg storage.Store, defaultDone string, log *slog.Logger) (ran bool, recovered int, err error) {
	log = log.With("component", "reconcile")
	ran, rerr := db.ReconcileOnce(ctx, func(ctx context.Context) error {
		pipes, perr := db.ListPipelines(ctx)
		if perr != nil {
			return fmt.Errorf("list pipelines: %w", perr)
		}
		cache := map[string]storage.Store{}
		defer func() {
			for _, s := range cache {
				_ = s.Close() // close only stores we created here (default excluded)
			}
		}()
		for _, p := range pipes {
			// Completion markers live beside the DONE files — on the OUTPUT store,
			// which for a cross-backend pipeline is a different backend than the
			// source. Recover from wherever the runner wrote them.
			sg, serr := markerStoreFor(ctx, p, defaultSg, cache)
			if serr != nil {
				log.Warn("resolve marker storage failed", "function", "Run", "pipeline", p.Name, "err", serr.Error())
				continue
			}
			done := p.Source.DoneDir
			if done == "" {
				done = defaultDone
			}
			n, rerr := recoverFrom(ctx, db, sg, done, log)
			if rerr != nil {
				log.Warn("reconcile pipeline failed", "function", "Run", "pipeline", p.Name, "err", rerr.Error())
				continue
			}
			recovered += n
		}
		return nil
	})
	if rerr != nil {
		return ran, recovered, rerr
	}
	if ran {
		log.Info("startup reconciliation complete", "function", "Run", "recovered", recovered)
	}
	return ran, recovered, nil
}

// markerStoreFor resolves the store that holds a pipeline's completion markers:
// the OUTPUT/dest store for a cross-backend pipeline (markers are written beside
// the done files there), otherwise the source store.
func markerStoreFor(ctx context.Context, p store.Pipeline, defaultSg storage.Store, cache map[string]storage.Store) (storage.Store, error) {
	if p.CrossBackend() {
		key := storage.DestKey(p.OutputDest)
		if s, ok := cache[key]; ok {
			return s, nil
		}
		s, err := storage.NewForDest(ctx, p.OutputDest)
		if err != nil {
			return nil, err
		}
		cache[key] = s
		return s, nil
	}
	return storeFor(ctx, p, defaultSg, cache)
}

// storeFor resolves the storage.Store for a pipeline's source, caching created
// stores by key. An empty key (legacy source) uses the process-default store.
func storeFor(ctx context.Context, p store.Pipeline, defaultSg storage.Store, cache map[string]storage.Store) (storage.Store, error) {
	key := storage.SourceKey(p.Source)
	if key == "" {
		return defaultSg, nil
	}
	if s, ok := cache[key]; ok {
		return s, nil
	}
	s, err := storage.NewForSource(ctx, p.Source)
	if err != nil {
		return nil, err
	}
	cache[key] = s
	return s, nil
}

// recoverFrom lists the markers under a done area and recovers missing files.
func recoverFrom(ctx context.Context, db store.Store, sg storage.Store, donePrefix string, log *slog.Logger) (int, error) {
	entries, lerr := sg.List(ctx, path.Join(donePrefix, markersDir))
	if lerr != nil {
		return 0, fmt.Errorf("list markers: %w", lerr)
	}
	recovered := 0
	for _, e := range entries {
		m, merr := readMarker(ctx, sg, e.Key)
		if merr != nil {
			log.Warn("skip unreadable marker", "function", "recoverFrom", "key", e.Key, "err", merr.Error())
			continue
		}
		ok, rec := db.RecoverProcessedFile(ctx, store.ProcessedFile{
			FileUID: m.FileUID, PipelineID: m.PipelineID, Name: m.Name, Size: m.Size,
			RecordsIn: m.RecordsIn, RecordsOut: m.RecordsOut, Suspended: m.Suspended,
			OutputName: m.OutputName, Status: "DONE",
		})
		if rec != nil {
			log.Warn("recover failed", "function", "recoverFrom", "file_uid", m.FileUID, "err", rec.Error())
			continue
		}
		if ok {
			recovered++
			log.Info("recovered processed file from marker", "function", "recoverFrom", "file_uid", m.FileUID, "file", m.Name)
		}
	}
	return recovered, nil
}

func readMarker(ctx context.Context, sg storage.Store, key string) (Marker, error) {
	rc, err := sg.Open(ctx, key)
	if err != nil {
		return Marker{}, err
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, 64*1024))
	if err != nil {
		return Marker{}, err
	}
	var m Marker
	if err := json.Unmarshal(data, &m); err != nil {
		return Marker{}, err
	}
	return m, nil
}
