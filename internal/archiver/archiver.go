// Package archiver ages out completed (DONE, and optionally QUARANTINED) files
// from each pipeline's done area into a compressed archive object on a
// destination backend, then prunes the local originals and records the
// AR_ARCHIVE_RUN / ARF_ARCHIVE_RUN_FILE audit trail (flipping the processed
// files to ARCHIVED).
//
// It is a single-owner background loop: each pass runs under the cluster-wide
// advisory lock (store.ReconcileOnce) so exactly one instance archives at a
// time, and the per-run correctness backstop is the DB itself — ListArchivablePF
// never returns files already ARCHIVED, so a re-run is idempotent.
//
// Per-pipeline policy lives in the pipeline JSONB document (store.Archive): the
// eligibility age (AgeDays), compression, destination, cadence (ScheduleSeconds)
// and whether quarantined files are included. Files are streamed from the
// source store into the archive and out to the destination store with bounded
// memory (io.Pipe), and the archive is verified on the destination before any
// original is pruned. The completion markers under <done>/.markers are never
// touched, so DB↔storage reconciliation still holds after archival.
package archiver

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

const (
	// defaultPoll is the base loop interval; per-pipeline cadence is gated on top
	// of it via Archive.ScheduleSeconds.
	defaultPoll = 30 * time.Second
	// defaultScheduleSeconds is the per-pipeline cadence when Archive.ScheduleSeconds
	// is unset (hourly).
	defaultScheduleSeconds = 3600
	// defaultDoneDir mirrors the engine's default done prefix for legacy pipelines
	// whose Source declares no DoneDir.
	defaultDoneDir = "done"
)

// Archiver ages out completed files per pipeline Archive policy. Source and
// destination stores are resolved from each pipeline's Source / Archive.Dest and
// cached for the lifetime of Run so an s3 client is built once, not per pass.
type Archiver struct {
	store        store.Store
	defaultStore storage.Store
	log          *slog.Logger
	poll         time.Duration
	defaultDone  string

	srcCache  map[string]storage.Store // SourceKey -> Store ("" => defaultStore)
	destCache map[string]storage.Store // DestKey -> Store
	lastRun   map[int64]time.Time      // pipelineID -> last attempt (schedule gating)
}

// New builds an Archiver. sg is the process-default storage.Store used when a
// pipeline's Source declares no storage of its own (legacy pipelines).
func New(st store.Store, sg storage.Store, log *slog.Logger) *Archiver {
	return &Archiver{
		store:        st,
		defaultStore: sg,
		log:          log.With("component", "archiver"),
		poll:         defaultPoll,
		defaultDone:  defaultDoneDir,
		srcCache:     map[string]storage.Store{},
		destCache:    map[string]storage.Store{},
		lastRun:      map[int64]time.Time{},
	}
}

// Run drives the archival loop until scanCtx is cancelled (SIGTERM): it stops
// starting new passes then. Archive I/O runs on procBase — the drain-bounded
// context the caller cancels only after a shutdown grace — so an in-flight
// archive completes rather than aborting mid-offload (mirrors the watcher,
// BR-HA-008). Each pass is single-owner via the shared reconcile advisory lock.
func (a *Archiver) Run(scanCtx, procBase context.Context) {
	a.log.Info("archiver started", "poll", a.poll.String())
	defer a.closeStores()
	t := time.NewTicker(a.poll)
	defer t.Stop()
	for {
		select {
		case <-scanCtx.Done():
			a.log.Info("archiver stopped")
			return
		case <-t.C:
			a.tick(scanCtx, procBase)
		}
	}
}

// tick runs one single-owner archival pass. Only the instance that wins the
// cluster-wide advisory lock executes; the others no-op this tick.
func (a *Archiver) tick(scanCtx, procBase context.Context) {
	// The advisory-lock connection is acquired on procBase so a SIGTERM mid-pass
	// does not drop the lock before the in-flight archive drains.
	if _, err := a.store.ReconcileOnce(procBase, func(context.Context) error {
		return a.runPass(scanCtx, procBase)
	}); err != nil {
		a.log.Error("archive pass failed", "function", "tick", "err", err.Error())
	}
}

// runPass archives every due, archive-enabled pipeline. A single pipeline's
// failure is logged and skipped — it never aborts the others. On shutdown
// (scanCtx cancelled) it stops starting new pipeline archives; the current one
// drains on procBase.
func (a *Archiver) runPass(scanCtx, procBase context.Context) error {
	pipes, err := a.store.ListPipelines(procBase)
	if err != nil {
		return fmt.Errorf("list pipelines: %w", err)
	}
	now := time.Now()
	for _, p := range pipes {
		if scanCtx.Err() != nil {
			return nil // draining — stop starting new pipeline archives
		}
		if p.Archive == nil || !p.Archive.Enabled {
			continue
		}
		if !a.due(p, now) {
			continue
		}
		a.lastRun[p.ID] = now
		if err := a.archivePipeline(procBase, p); err != nil {
			a.log.Error("archive pipeline failed", "function", "runPass", "pipeline", p.Name, "err", err.Error())
			continue
		}
	}
	return nil
}

// due reports whether a pipeline is scheduled to be archived now.
func (a *Archiver) due(p store.Pipeline, now time.Time) bool {
	sched := p.Archive.ScheduleSeconds
	if sched <= 0 {
		sched = defaultScheduleSeconds
	}
	last, ok := a.lastRun[p.ID]
	if !ok {
		return true
	}
	return now.Sub(last) >= time.Duration(sched)*time.Second
}

// archivePipeline runs one pipeline's archival: select eligible files, build and
// offload the archive, verify it on the destination, then prune the local
// originals (keeping the completion markers) and record the audit trail. Any
// failure before pruning aborts without deleting anything.
func (a *Archiver) archivePipeline(ctx context.Context, p store.Pipeline) error {
	arc := p.Archive
	if !destConfigured(arc.Dest) {
		a.log.Warn("archive destination not configured; skipping", "function", "archivePipeline", "pipeline", p.Name)
		return nil
	}

	cutoff := time.Now().UTC().Add(-time.Duration(arc.AgeDays) * 24 * time.Hour)
	pfs, err := a.store.ListArchivablePF(ctx, p.ID, cutoff, arc.IncludeQuarantined)
	if err != nil {
		return fmt.Errorf("list archivable: %w", err)
	}
	if len(pfs) == 0 {
		return nil
	}

	srcStore, err := a.sourceStore(ctx, p.Source)
	if err != nil {
		return fmt.Errorf("resolve source storage: %w", err)
	}

	doneDir := firstNonEmpty(p.Source.DoneDir, a.defaultDone)
	quarDir := p.Source.QuarantineDir
	if quarDir == "" {
		quarDir = path.Join(doneDir, "quarantine")
	}

	// Map the physical objects currently present in the lifecycle areas by base
	// name. List excludes the .markers sub-area, so completion markers are never
	// selected for archival. Only files still physically present are archived;
	// files removed by a "delete"/"leaveMarked" disposition are simply skipped.
	present := map[string]storage.Entry{}
	if err := addPresent(ctx, srcStore, doneDir, present); err != nil {
		return fmt.Errorf("list done dir %q: %w", doneDir, err)
	}
	if arc.IncludeQuarantined {
		if err := addPresent(ctx, srcStore, quarDir, present); err != nil {
			return fmt.Errorf("list quarantine dir %q: %w", quarDir, err)
		}
	}

	var files []fileEntry
	var pfUIDs []int64
	for _, pf := range pfs {
		e, ok := present[pf.Name]
		if !ok {
			continue // no physical original to archive (already pruned / non-move disposition)
		}
		files = append(files, fileEntry{Name: pf.Name, Key: e.Key, Size: e.Size, ModTime: e.ModTime})
		pfUIDs = append(pfUIDs, pf.PFUID)
	}
	if len(files) == 0 {
		return nil
	}

	destStore, err := a.destStore(ctx, arc.Dest)
	if err != nil {
		return fmt.Errorf("resolve dest storage: %w", err)
	}

	comp := normComp(arc.Compression)
	archiveKey := archiveName(p, comp)

	// Build + stream the archive to the destination (bounded memory).
	m, size, err := buildAndOffload(ctx, srcStore, destStore, files, comp, archiveKey)
	if err != nil {
		return fmt.Errorf("build/offload archive %q: %w", archiveKey, err)
	}

	// Verify the archive landed intact before touching any original.
	if err := verify(ctx, destStore, archiveKey, size, m.SHA256); err != nil {
		return fmt.Errorf("verify archive %q: %w", archiveKey, err)
	}

	// Write the manifest sidecar (per-file checksums + the archive's own sha256).
	m.Pipeline = p.Name
	m.SizeBytes = size
	if err := putManifest(ctx, destStore, archiveKey, m); err != nil {
		return fmt.Errorf("write manifest for %q: %w", archiveKey, err)
	}

	// Prune the local originals (best-effort) — the archive is now durable and
	// verified. The completion markers are intentionally left in place so
	// DB↔storage reconciliation still recognises these files.
	pruned := true
	for _, f := range files {
		if derr := srcStore.Delete(ctx, f.Key); derr != nil {
			a.log.Warn("prune original failed", "function", "archivePipeline", "pipeline", p.Name, "file", f.Name, "err", derr.Error())
			pruned = false
		}
	}

	// Record the run + flip the processed files to ARCHIVED (one transaction).
	run := store.ArchiveRun{
		Compression: comp,
		ArchiveName: archiveKey,
		SizeBytes:   size,
		Checksum:    m.SHA256,
		Target:      destTarget(arc.Dest),
		Pruned:      pruned,
		PFUIDs:      pfUIDs,
	}
	arUID, err := a.store.MarkArchived(ctx, p.ID, run)
	if err != nil {
		return fmt.Errorf("mark archived (archive %q already offloaded): %w", archiveKey, err)
	}
	a.log.Info("archived pipeline files", "function", "archivePipeline", "pipeline", p.Name,
		"files", len(files), "archive", archiveKey, "sizeBytes", size, "compression", comp,
		"pruned", pruned, "ar_uid", arUID)
	return nil
}

// sourceStore resolves (and caches) the storage.Store for a pipeline's source.
func (a *Archiver) sourceStore(ctx context.Context, src store.Source) (storage.Store, error) {
	key := storage.SourceKey(src)
	if key == "" {
		return a.defaultStore, nil
	}
	if s, ok := a.srcCache[key]; ok {
		return s, nil
	}
	s, err := storage.NewForSource(ctx, src)
	if err != nil {
		return nil, err
	}
	a.srcCache[key] = s
	return s, nil
}

// destStore resolves (and caches) the storage.Store for an archive destination.
func (a *Archiver) destStore(ctx context.Context, dest store.Dest) (storage.Store, error) {
	key := storage.DestKey(dest)
	if s, ok := a.destCache[key]; ok {
		return s, nil
	}
	s, err := storage.NewForDest(ctx, dest)
	if err != nil {
		return nil, err
	}
	a.destCache[key] = s
	return s, nil
}

// closeStores releases every store this archiver created (the process-default
// store is owned by the caller and excluded).
func (a *Archiver) closeStores() {
	for _, s := range a.srcCache {
		_ = s.Close()
	}
	for _, s := range a.destCache {
		_ = s.Close()
	}
}

// addPresent lists a lifecycle area and indexes its files by base name.
func addPresent(ctx context.Context, sg storage.Store, dir string, into map[string]storage.Entry) error {
	entries, err := sg.List(ctx, dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		into[path.Base(e.Key)] = e
	}
	return nil
}

// destConfigured reports whether an archive destination is usable (avoids
// silently writing archives to the process working directory).
func destConfigured(d store.Dest) bool {
	switch d.Backend {
	case storage.BackendS3:
		return d.S3.Bucket != ""
	default:
		return d.Root != ""
	}
}

// destTarget is a human-readable destination description for the audit row.
func destTarget(d store.Dest) string {
	if d.Backend == storage.BackendS3 {
		return "s3://" + d.S3.Bucket
	}
	b := d.Backend
	if b == "" {
		b = storage.BackendPOSIX
	}
	return b + ":" + d.Root
}

// normComp normalises the compression selector (empty => gzip default).
func normComp(c string) string {
	switch c {
	case "zip", "tar_gz", "gzip":
		return c
	default:
		return "gzip"
	}
}

// archiveName builds a stable, filesystem-safe archive object name.
func archiveName(p store.Pipeline, comp string) string {
	ext := "tar.gz"
	if comp == "zip" {
		ext = "zip"
	}
	return fmt.Sprintf("%s-%d-%s.%s", sanitize(p.Name), p.ID, time.Now().UTC().Format("20060102T150405Z"), ext)
}

func sanitize(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		return "pipeline"
	}
	return out
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
