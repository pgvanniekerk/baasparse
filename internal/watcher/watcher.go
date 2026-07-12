// Package watcher is the data plane: it polls each enabled pipeline's input
// location on the configured storage backend and processes any files it finds
// (BR-COL-012 — short-interval scan / object List).
//
// Multi-instance safety (BR-HA-003/004, BR-COL-006): before processing, a file
// is (1) skipped if already processed (re-arrival), and (2) claimed via the
// distributed FC_FILE_CLAIM lease, so exactly one instance processes it; a
// heartbeat renews the lease and a lost claim cancels the work; the record
// commits under a fence guard. A dead instance's lease expires and another
// takes over.
package watcher

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/pgvanniekerk/baasparse/internal/runner"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// maxStall is the number of no-progress lease takeovers after which a file is
// treated as poison and quarantined rather than retried forever (BR-COL-017).
const maxStall = 10

// MaxProcessing hard-caps a single file's processing so a hung file cannot block
// forever; the heartbeat's lost-claim cancel and the shutdown drain deadline
// still apply.
const MaxProcessing = 10 * time.Minute

// Watcher scans pipeline input locations on an interval. Each pipeline is scanned
// through the storage.Store resolved from its own Source (per-source backend),
// falling back to the process-default store for legacy pipelines. Resolved stores
// are cached by config key so an s3 client is built once, not per scan.
type Watcher struct {
	store        store.Store
	defaultStore storage.Store
	runner       *runner.Runner
	log          *slog.Logger
	outputPrefix string // default output location when a pipeline sets none
	donePrefix   string
	interval     time.Duration

	mQuar metric.Int64Counter // files quarantined (labelled by pipeline + reason_code)

	mu     sync.Mutex
	stores map[string]storage.Store // cache: SourceKey -> Store

	// Bounded per-instance worker pool: up to cap(sem) files process concurrently
	// (memory stays bounded at workers × buffer sizes, BR-NFR-001/002). inflight
	// dedups submissions within THIS instance so a file being processed is not
	// re-submitted on the next scan tick; cross-instance exclusivity is unchanged
	// (the FC_FILE_CLAIM lease). wg tracks workers for a clean shutdown drain.
	sem        chan struct{}
	wg         sync.WaitGroup
	inflightMu sync.Mutex
	inflight   map[string]struct{}
}

// New builds a Watcher. sg is the process-default store used when a pipeline's
// Source declares no storage of its own. workers bounds how many files this
// instance processes concurrently (<=0 selects the default of 4).
func New(st store.Store, sg storage.Store, rn *runner.Runner, log *slog.Logger, outputPrefix, donePrefix string, interval time.Duration, workers int) *Watcher {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	if workers <= 0 {
		workers = 4
	}
	quar, _ := otel.Meter("baasparse/watcher").Int64Counter(
		"baasparse_files_quarantined_total", metric.WithDescription("Files quarantined (poison/decode failures)"))
	return &Watcher{store: st, defaultStore: sg, runner: rn, log: log.With("component", "watcher"),
		outputPrefix: outputPrefix, donePrefix: donePrefix, interval: interval,
		mQuar: quar, stores: map[string]storage.Store{},
		sem: make(chan struct{}, workers), inflight: map[string]struct{}{}}
}

// cachedStore returns the store for key, building it once via build. An empty key
// means "no per-pipeline storage" → the process-default store. The cache is
// shared by source and destination resolution, so a source and a destination that
// resolve to the same backend+root share one Store instance (letting Relocate use
// a native Move instead of a cross-store copy).
func (w *Watcher) cachedStore(key string, build func() (storage.Store, error)) (storage.Store, error) {
	if key == "" {
		return w.defaultStore, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if s, ok := w.stores[key]; ok {
		return s, nil
	}
	s, err := build()
	if err != nil {
		return nil, err
	}
	w.stores[key] = s
	return s, nil
}

// storeFor resolves the storage.Store for a pipeline's source (input area).
func (w *Watcher) storeFor(ctx context.Context, p store.Pipeline) (storage.Store, error) {
	return w.cachedStore(storage.SourceKey(p.Source), func() (storage.Store, error) {
		return storage.NewForSource(ctx, p.Source)
	})
}

// destStoreFor resolves the storage.Store the pipeline writes output/done to. For
// a same-backend pipeline this is the source store; for a cross-backend pipeline
// (distinct output datasource) it is a separate store resolved from OutputDest.
func (w *Watcher) destStoreFor(ctx context.Context, p store.Pipeline, src storage.Store) (storage.Store, error) {
	if !p.CrossBackend() {
		return src, nil
	}
	return w.cachedStore(storage.DestKey(p.OutputDest), func() (storage.Store, error) {
		return storage.NewForDest(ctx, p.OutputDest)
	})
}

// dirs are the resolved per-source lifecycle areas for a pipeline.
type dirs struct {
	input, done, quar, out string
}

func (w *Watcher) resolveDirs(p store.Pipeline) dirs {
	d := dirs{
		input: firstNonEmpty(p.Source.InputDir, p.InputDir),
		done:  firstNonEmpty(p.Source.DoneDir, w.donePrefix),
		out:   firstNonEmpty(p.Source.OutputDir, p.OutputDir, w.outputPrefix),
	}
	d.quar = p.Source.QuarantineDir
	if d.quar == "" {
		d.quar = path.Join(d.done, "quarantine")
	}
	return d
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// Run scans until scanCtx is cancelled (SIGTERM). In-flight files are processed
// on procBase — a context the caller cancels only after a bounded drain grace —
// so a file drains to completion on shutdown (BR-HA-008) rather than aborting
// mid-flight against a closing pool.
func (w *Watcher) Run(scanCtx, procBase context.Context) {
	w.log.Info("watcher started", "interval", w.interval.String(), "backend", w.defaultStore.Backend(), "workers", cap(w.sem))
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-scanCtx.Done():
			// Stop claiming new files; wait for in-flight workers to drain (each is
			// bounded by procBase cancellation / MaxProcessing, BR-HA-008).
			w.wg.Wait()
			w.log.Info("watcher stopped")
			return
		case <-t.C:
			w.scan(scanCtx, procBase)
		}
	}
}

func (w *Watcher) scan(scanCtx, procBase context.Context) {
	pipes, err := w.store.ListPipelines(scanCtx)
	if err != nil {
		w.log.Error("list pipelines failed", "function", "scan", "err", err.Error())
		return
	}
	for _, p := range pipes {
		if scanCtx.Err() != nil {
			return
		}
		d := w.resolveDirs(p)
		if !p.Enabled || strings.TrimSpace(d.input) == "" {
			continue
		}
		sg, err := w.storeFor(scanCtx, p)
		if err != nil {
			w.log.Error("resolve source storage failed", "function", "scan", "pipeline", p.Name, "err", err.Error())
			continue
		}
		entries, err := sg.List(scanCtx, d.input)
		if err != nil {
			w.log.Error("list input failed", "function", "scan", "pipeline", p.Name, "input", d.input, "err", err.Error())
			continue
		}
		// Consolidating pipelines take the batch path: many input files are
		// concatenated into ONE output per destination, so files are grouped rather
		// than handled one at a time. Multiple destinations also require it, because
		// each destination's delivery must be tracked independently.
		if p.Batch.Enabled || len(p.Outputs) > 1 {
			w.scanBatched(scanCtx, procBase, p, sg, entries, d)
			continue
		}
		for _, e := range entries {
			if scanCtx.Err() != nil {
				return // shutting down — stop claiming new files (in-flight drains)
			}
			w.submit(scanCtx, procBase, p, sg, e, d)
		}
	}
}

// submit hands one detected file to the worker pool. Files already being
// processed by THIS instance are skipped (in-flight dedup); when all workers are
// busy it blocks until a slot frees (backpressure — detection never outruns
// capacity). Cross-instance dedup remains the claim lease inside handle.
func (w *Watcher) submit(scanCtx, procBase context.Context, p store.Pipeline, sg storage.Store, e storage.Entry, d dirs) {
	key := strconv.FormatInt(p.SrcUID, 10) + "|" + path.Base(e.Key)
	w.inflightMu.Lock()
	if _, busy := w.inflight[key]; busy {
		w.inflightMu.Unlock()
		return
	}
	w.inflight[key] = struct{}{}
	w.inflightMu.Unlock()

	release := func() {
		w.inflightMu.Lock()
		delete(w.inflight, key)
		w.inflightMu.Unlock()
	}

	select {
	case w.sem <- struct{}{}: // acquired a worker slot
	case <-scanCtx.Done():
		release()
		return
	}
	w.wg.Add(1)
	go func() {
		defer func() {
			// A panic anywhere in handle outside runProtected's shield must not
			// crash the instance (a poison file would crash-loop the whole cluster
			// via lease takeover, BR-COL-017) — convert it to a logged error; the
			// claim lease expires and the file is retried/quarantined normally.
			if rec := recover(); rec != nil {
				w.log.Error("recovered panic in file worker", "function", "submit", "pipeline", p.Name, "file", path.Base(e.Key), "err", fmt.Sprint(rec))
			}
			<-w.sem
			release()
			w.wg.Done()
		}()
		w.handle(procBase, p, sg, e, d)
	}()
}

// handle claims and processes one detected file, guarding against re-arrivals
// and concurrent processing by other instances. ctx is the processing base (not
// the scan/SIGTERM context), so a started file drains on shutdown.
func (w *Watcher) handle(ctx context.Context, p store.Pipeline, sg storage.Store, e storage.Entry, d dirs) {
	name := path.Base(e.Key)
	if p.SrcUID == 0 {
		return // pipeline has no published source yet
	}

	// Bound the ENTIRE handle — including the pre-claim checks and post-error
	// quarantine, which would otherwise run on the never-cancelled (in steady
	// state) procBase. Without this, a storage/DB call that stalls without
	// erroring pins a worker slot indefinitely; four such files would stall the
	// whole pool. 2×MaxProcessing leaves the inner per-file processing budget
	// intact while guaranteeing the slot is always reclaimed.
	ctx, cancelAll := context.WithTimeout(ctx, 2*MaxProcessing)
	defer cancelAll()

	// Resolve the output/done store — the same as the source for a same-backend
	// pipeline, or a separate store for a cross-backend one (read here, write there).
	dst, err := w.destStoreFor(ctx, p, sg)
	if err != nil {
		w.log.Error("resolve output storage failed", "function", "handle", "pipeline", p.Name, "err", err.Error())
		return
	}

	// Already quarantined as a poison file (BR-COL-017): ensure it is out of the
	// input location and skip — do not re-arm the retry loop. Quarantine lives on
	// the source store (it is about the input file), so a same-store Move suffices.
	if q, err := w.store.AlreadyQuarantined(ctx, p.SrcUID, name); err == nil && q {
		if err := sg.Move(ctx, e.Key, path.Join(d.quar, name)); err != nil {
			w.log.Warn("quarantine re-move failed", "function", "handle", "pipeline", p.Name, "file", name, "err", err.Error())
		}
		return
	}

	// Re-arrival of an already-processed file (BR-COL-006), or the crash/move
	// window where the record committed but the source lingered: move it to done
	// and skip — never reprocess. Under the "leaveMarked" disposition the source is
	// intentionally left in place, so we skip without moving. The done area is on
	// the destination store (cross-backend safe via Relocate).
	if done, err := w.store.AlreadyProcessed(ctx, p.SrcUID, name); err == nil && done {
		if p.Source.Disposition == "leaveMarked" {
			return
		}
		if err := storage.Relocate(ctx, dst, path.Join(d.done, name), sg, e.Key); err != nil {
			w.log.Warn("re-arrival move failed", "function", "handle", "pipeline", p.Name, "file", name, "err", err.Error())
		}
		return
	}

	// Claim the file — exactly one instance wins (BR-HA-003).
	claim, ok, err := w.store.ClaimFile(ctx, p.SrcUID, name)
	if err != nil {
		w.log.Error("claim failed", "function", "handle", "pipeline", p.Name, "file", name, "err", err.Error())
		return
	}
	if !ok {
		return // owned by another live instance (or lease not yet expired)
	}

	// Poison guard: a file that has been taken over repeatedly without progress is
	// quarantined (recorded as a QUARANTINED file-level state, BR-COL-017/BR-REC-008)
	// rather than retried forever.
	if claim.StallCount >= maxStall {
		reason := fmt.Sprintf("STALLED: no progress after %d lease takeovers", claim.StallCount)
		w.quarantine(ctx, p, sg, e.Key, name, reason, claim, d)
		return
	}

	// Heartbeat the lease while processing. procCtx derives from the drain-bounded
	// base (cancelled only after the shutdown grace), so an in-flight file drains
	// on SIGTERM (BR-HA-008); MaxProcessing bounds a hung file and the heartbeat's
	// onLost still cancels on a lost claim.
	var lost atomic.Bool
	procCtx, cancel := context.WithTimeout(ctx, MaxProcessing)
	defer cancel()
	stopHB := w.startHeartbeat(procCtx, p, name, claim, func() { lost.Store(true); cancel() })

	perr := w.runProtected(procCtx, p, sg, dst, e.Key, d, &claim)
	stopHB()

	var bad *runner.BadFileError
	switch {
	case perr == nil:
		// success — record + claim release done atomically in the runner
	case errors.Is(perr, runner.ErrLostClaim) || lost.Load():
		w.log.Warn("claim lost during processing; handed off", "function", "handle", "pipeline", p.Name, "file", name)
	case errors.As(perr, &bad):
		// Content failure (malformed/undecodable file): quarantine NOW with the
		// reason so the operator sees why, instead of retrying a file that will
		// never parse. On S3 there is no filesystem to inspect — this record is how
		// the failure becomes visible.
		w.quarantine(ctx, p, sg, e.Key, name, bad.Reason, claim, d)
	default:
		// Infrastructure error (storage/DB): if another instance already handled the
		// file (a scan race moved the source out from under us), release our stray
		// claim now. Otherwise leave it to expire so the retry is lease-paced (not a
		// tight loop).
		if done, derr := w.store.AlreadyProcessed(ctx, p.SrcUID, name); derr == nil && done {
			_ = w.store.ReleaseClaim(ctx, claim)
		} else {
			w.log.Error("process file failed", "function", "handle", "pipeline", p.Name, "file", name, "err", perr.Error())
		}
	}
}

// quarantine records a file as QUARANTINED with a reason (fence-guarded, so a lost
// claim is a no-op) and moves the source out of the input area into the quarantine
// area on the source store. Used for both poison-stall and content-error files.
func (w *Watcher) quarantine(ctx context.Context, p store.Pipeline, sg storage.Store, key, name, reason string, claim store.Claim, d dirs) {
	var size int64
	if st, serr := sg.Stat(ctx, key); serr == nil {
		size = st.Size
	}
	ok, qerr := w.store.QuarantineFile(ctx, p.ID, p.SrcUID, name, size, reason, claim)
	if qerr != nil {
		w.log.Error("quarantine failed", "function", "quarantine", "pipeline", p.Name, "file", name, "err", qerr.Error())
		return
	}
	if !ok {
		return // claim lost before quarantine — another owner will handle it
	}
	if w.mQuar != nil {
		w.mQuar.Add(ctx, 1, metric.WithAttributes(
			attribute.String("pipeline", p.Name),
			attribute.String("reason_code", reasonCode(reason))))
	}
	w.log.Error("file quarantined", "function", "quarantine", "pipeline", p.Name, "file", name, "reason", reason)
	if err := sg.Move(ctx, key, path.Join(d.quar, name)); err != nil {
		w.log.Warn("quarantine move failed", "function", "quarantine", "pipeline", p.Name, "file", name, "err", err.Error())
	}
}

// reasonCode extracts the leading UPPER_SNAKE code from a quarantine reason (e.g.
// "DECODE_ERROR" from "DECODE_ERROR: ..."), for use as a low-cardinality metric
// label. The full reason text is high-cardinality and stays in logs / the PF row.
func reasonCode(reason string) string {
	for i := 0; i < len(reason); i++ {
		c := reason[i]
		if c == ':' {
			if i > 0 {
				return reason[:i]
			}
			break
		}
		if !(c >= 'A' && c <= 'Z' || c == '_') {
			break
		}
	}
	return "PROCESSING_ERROR"
}

// runProtected runs ProcessFile with a recover so a bug (panic) on one file
// converts to an error and does not crash the instance — preventing a poison
// file from crash-looping the cluster via takeover (BR-COL-017).
func (w *Watcher) runProtected(ctx context.Context, p store.Pipeline, sg, dst storage.Store, key string, d dirs, claim *store.Claim) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic processing file: %v", rec)
			w.log.Error("recovered panic", "function", "runProtected", "pipeline", p.Name, "key", key, "err", err.Error())
		}
	}()
	_, err = w.runner.ProcessFile(ctx, p, sg, dst, key, d.out, d.done, claim)
	return err
}

// startHeartbeat renews the claim lease on an interval until stopped; if the
// claim is lost it invokes onLost. The returned stop function closes the loop
// and WAITS for the goroutine to exit, so no renewal can race after stop.
func (w *Watcher) startHeartbeat(ctx context.Context, p store.Pipeline, name string, claim store.Claim, onLost func()) func() {
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		t := time.NewTicker(store.HeartbeatSeconds * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-t.C:
				held, err := w.store.HeartbeatClaim(ctx, claim)
				if err != nil {
					continue // transient; the lease still has slack before expiry
				}
				if !held {
					w.log.Warn("claim lost (heartbeat)", "function", "heartbeat", "pipeline", p.Name, "file", name)
					onLost()
					return
				}
			}
		}
	}()
	var once bool
	return func() {
		if !once {
			once = true
			close(done)
		}
		<-stopped
	}
}
