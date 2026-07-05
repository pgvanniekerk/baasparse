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

// Run performs single-owner startup reconciliation: it lists completion markers
// and re-creates any Processed File the database is missing (idempotent). Returns
// whether this instance ran it and how many rows it recovered.
func Run(ctx context.Context, db store.Store, sg storage.Store, donePrefix string, log *slog.Logger) (ran bool, recovered int, err error) {
	log = log.With("component", "reconcile")
	ran, rerr := db.ReconcileOnce(ctx, func(ctx context.Context) error {
		entries, lerr := sg.List(ctx, path.Join(donePrefix, markersDir))
		if lerr != nil {
			return fmt.Errorf("list markers: %w", lerr)
		}
		for _, e := range entries {
			m, merr := readMarker(ctx, sg, e.Key)
			if merr != nil {
				log.Warn("skip unreadable marker", "function", "Run", "key", e.Key, "err", merr.Error())
				continue
			}
			ok, rec := db.RecoverProcessedFile(ctx, store.ProcessedFile{
				FileUID: m.FileUID, PipelineID: m.PipelineID, Name: m.Name, Size: m.Size,
				RecordsIn: m.RecordsIn, RecordsOut: m.RecordsOut, Suspended: m.Suspended,
				OutputName: m.OutputName, Status: "DONE",
			})
			if rec != nil {
				log.Warn("recover failed", "function", "Run", "file_uid", m.FileUID, "err", rec.Error())
				continue
			}
			if ok {
				recovered++
				log.Info("recovered processed file from marker", "function", "Run", "file_uid", m.FileUID, "file", m.Name)
			}
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
