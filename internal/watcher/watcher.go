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
	"strings"
	"sync/atomic"
	"time"

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

// Watcher scans pipeline input locations on an interval.
type Watcher struct {
	store        store.Store
	storage      storage.Store
	runner       *runner.Runner
	log          *slog.Logger
	outputPrefix string // default output location when a pipeline sets none
	donePrefix   string
	interval     time.Duration
}

// New builds a Watcher.
func New(st store.Store, sg storage.Store, rn *runner.Runner, log *slog.Logger, outputPrefix, donePrefix string, interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &Watcher{store: st, storage: sg, runner: rn, log: log.With("component", "watcher"),
		outputPrefix: outputPrefix, donePrefix: donePrefix, interval: interval}
}

// Run scans until scanCtx is cancelled (SIGTERM). In-flight files are processed
// on procBase — a context the caller cancels only after a bounded drain grace —
// so a file drains to completion on shutdown (BR-HA-008) rather than aborting
// mid-flight against a closing pool.
func (w *Watcher) Run(scanCtx, procBase context.Context) {
	w.log.Info("watcher started", "interval", w.interval.String(), "backend", w.storage.Backend())
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-scanCtx.Done():
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
		if !p.Enabled || strings.TrimSpace(p.InputDir) == "" {
			continue
		}
		entries, err := w.storage.List(scanCtx, p.InputDir)
		if err != nil {
			w.log.Error("list input failed", "function", "scan", "pipeline", p.Name, "input", p.InputDir, "err", err.Error())
			continue
		}
		outPrefix := p.OutputDir
		if outPrefix == "" {
			outPrefix = w.outputPrefix
		}
		for _, e := range entries {
			if scanCtx.Err() != nil {
				return // shutting down — stop claiming new files (in-flight drains)
			}
			w.handle(procBase, p, e, outPrefix)
		}
	}
}

// handle claims and processes one detected file, guarding against re-arrivals
// and concurrent processing by other instances. ctx is the processing base (not
// the scan/SIGTERM context), so a started file drains on shutdown.
func (w *Watcher) handle(ctx context.Context, p store.Pipeline, e storage.Entry, outPrefix string) {
	name := path.Base(e.Key)
	if p.SrcUID == 0 {
		return // pipeline has no published source yet
	}

	// Already quarantined as a poison file (BR-COL-017): ensure it is out of the
	// input location and skip — do not re-arm the retry loop.
	if q, err := w.store.AlreadyQuarantined(ctx, p.SrcUID, name); err == nil && q {
		if err := w.storage.Move(ctx, e.Key, path.Join(w.donePrefix, "quarantine", name)); err != nil {
			w.log.Warn("quarantine re-move failed", "function", "handle", "pipeline", p.Name, "file", name, "err", err.Error())
		}
		return
	}

	// Re-arrival of an already-processed file (BR-COL-006), or the crash/move
	// window where the record committed but the source lingered: move it to done
	// and skip — never reprocess.
	if done, err := w.store.AlreadyProcessed(ctx, p.SrcUID, name); err == nil && done {
		if err := w.storage.Move(ctx, e.Key, path.Join(w.donePrefix, name)); err != nil {
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
		var size int64
		if st, serr := w.storage.Stat(ctx, e.Key); serr == nil {
			size = st.Size
		}
		if ok, qerr := w.store.QuarantineFile(ctx, p.ID, p.SrcUID, name, size, claim); qerr != nil {
			w.log.Error("quarantine failed", "function", "handle", "pipeline", p.Name, "file", name, "err", qerr.Error())
			return
		} else if !ok {
			return // claim lost before quarantine
		}
		w.log.Error("poison file quarantined", "function", "handle", "pipeline", p.Name, "file", name, "stall", claim.StallCount)
		if err := w.storage.Move(ctx, e.Key, path.Join(w.donePrefix, "quarantine", name)); err != nil {
			w.log.Warn("quarantine move failed", "function", "handle", "file", name, "err", err.Error())
		}
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

	perr := w.runProtected(procCtx, p, e.Key, outPrefix, &claim)
	stopHB()

	switch {
	case perr == nil:
		// success — record + claim release done atomically in the runner
	case errors.Is(perr, runner.ErrLostClaim) || lost.Load():
		w.log.Warn("claim lost during processing; handed off", "function", "handle", "pipeline", p.Name, "file", name)
	default:
		// If another instance already handled this file (a scan race moved the
		// source out from under us), release our stray claim now. Otherwise leave
		// it to expire so the retry is lease-paced (not a tight loop).
		if done, derr := w.store.AlreadyProcessed(ctx, p.SrcUID, name); derr == nil && done {
			_ = w.store.ReleaseClaim(ctx, claim)
		} else {
			w.log.Error("process file failed", "function", "handle", "pipeline", p.Name, "file", name, "err", perr.Error())
		}
	}
}

// runProtected runs ProcessFile with a recover so a bug (panic) on one file
// converts to an error and does not crash the instance — preventing a poison
// file from crash-looping the cluster via takeover (BR-COL-017).
func (w *Watcher) runProtected(ctx context.Context, p store.Pipeline, key, outPrefix string, claim *store.Claim) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic processing file: %v", rec)
			w.log.Error("recovered panic", "function", "runProtected", "pipeline", p.Name, "key", key, "err", err.Error())
		}
	}()
	_, err = w.runner.ProcessFile(ctx, p, w.storage, key, outPrefix, w.donePrefix, claim)
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
