// Package fetcher is the SFTP fetch transport: for every pipeline whose
// Source.Backend=="sftp" it connects to the remote endpoint, lists the remote
// path and streams each new file into the pipeline's own input area (a posix or
// s3 storage.Store resolved from the Source) where the watcher then picks it up.
// SFTP is NOT a storage backend — it is only a way to move bytes into the input
// area, so exactly-once, claims and reconciliation stay unchanged downstream.
//
// Single-owner (BR-HA-003/004): each fetch cycle runs under the same cluster-wide
// advisory lock the reconciler uses (store.ReconcileOnce), so at most one instance
// fetches at a time. Per-pipeline Source.PollSeconds paces how often a pipeline is
// re-scanned; connection failures back off exponentially. In-flight transfers use
// the drain-bounded base context so a file finishes on SIGTERM (BR-HA-008).
package fetcher

import (
	"context"
	"errors"
	"log/slog"
	"path"
	"sync"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

const (
	// baseTick is how often the fetch loop wakes; per-pipeline PollSeconds gates
	// whether a given pipeline is actually re-scanned on a wake.
	baseTick = 5 * time.Second
	// defaultPollSeconds is the per-pipeline scan interval when Source.PollSeconds
	// is unset (0).
	defaultPollSeconds = 60
	// maxBackoff caps the exponential connection-failure backoff.
	maxBackoff = 10 * time.Minute
	// maxConcurrentFiles bounds concurrent file transfers across all pipelines.
	maxConcurrentFiles = 4
)

// Resolver builds the storage.Store a source's input area lives on. The default
// (storage.NewForSource) resolves an "sftp" source to a posix store rooted at
// Source.Root; integration may pass a shared/caching resolver instead.
type Resolver func(ctx context.Context, src store.Source) (storage.Store, error)

// Fetcher pulls remote files into pipelines' input areas over SFTP.
type Fetcher struct {
	store   store.Store
	resolve Resolver
	log     *slog.Logger
	hostKeys *hostKeyStore
	sem     chan struct{}

	mu     sync.Mutex
	stores map[string]storage.Store // cache: SourceKey -> input-area Store
	// nextDue/fails pace each pipeline (in-memory; a fresh owner just re-scans,
	// which is idempotent because already-present/processed files are skipped).
	nextDue map[int64]time.Time
	fails   map[int64]int
}

// New builds a Fetcher. st is the persistence store (pipelines + claims + the
// single-owner lock); resolve builds the input-area store for a Source (pass nil
// to use storage.NewForSource); log is the base logger.
func New(st store.Store, resolve Resolver, log *slog.Logger) *Fetcher {
	if resolve == nil {
		resolve = storage.NewForSource
	}
	return &Fetcher{
		store:    st,
		resolve:  resolve,
		log:      log.With("component", "fetcher"),
		hostKeys: newHostKeyStore(log),
		sem:      make(chan struct{}, maxConcurrentFiles),
		stores:   map[string]storage.Store{},
		nextDue:  map[int64]time.Time{},
		fails:    map[int64]int{},
	}
}

// Run fetches until scanCtx is cancelled (SIGTERM). Transfers already started run
// on procBase — a context the caller cancels only after a bounded drain grace —
// so a file finishes streaming into the input area on shutdown rather than
// aborting mid-copy. Mirrors watcher.Run's (scanCtx, procBase) drain contract.
func (f *Fetcher) Run(scanCtx, procBase context.Context) {
	f.log.Info("fetcher started", "tick", baseTick.String())
	t := time.NewTicker(baseTick)
	defer t.Stop()
	defer f.closeStores()
	for {
		select {
		case <-scanCtx.Done():
			f.log.Info("fetcher stopped")
			return
		case <-t.C:
			f.cycle(scanCtx, procBase)
		}
	}
}

// cycle runs one single-owner fetch pass. If another instance holds the lock it
// returns immediately (ran=false).
func (f *Fetcher) cycle(scanCtx, procBase context.Context) {
	_, err := f.store.ReconcileOnce(scanCtx, func(ctx context.Context) error {
		return f.fetchDue(scanCtx, procBase)
	})
	if err != nil && scanCtx.Err() == nil {
		f.log.Error("fetch cycle failed", "function", "cycle", "err", err.Error())
	}
}

// fetchDue scans every SFTP-source pipeline that is due this cycle.
func (f *Fetcher) fetchDue(scanCtx, procBase context.Context) error {
	pipes, err := f.store.ListSFTPSourcePipelines(scanCtx)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, p := range pipes {
		if scanCtx.Err() != nil {
			return nil
		}
		if due, ok := f.nextDue[p.ID]; ok && now.Before(due) {
			continue // not yet due / backing off
		}
		f.fetchPipeline(scanCtx, procBase, p)
	}
	return nil
}

// fetchPipeline connects to one pipeline's SFTP endpoint and streams every new
// remote file into the input area, then reschedules the pipeline.
func (f *Fetcher) fetchPipeline(scanCtx, procBase context.Context, p store.Pipeline) {
	if p.SrcUID == 0 {
		return // no published source yet
	}
	r := p.Source.Remote
	if r.Host == "" {
		f.reschedule(p, nil) // nothing to do; pace normally
		return
	}
	sg, err := f.storeFor(scanCtx, p)
	if err != nil {
		f.log.Error("resolve input storage failed", "function", "fetchPipeline", "pipeline", p.Name, "err", err.Error())
		f.reschedule(p, err)
		return
	}
	inputDir := firstNonEmpty(p.Source.InputDir, p.InputDir)

	conn, err := dialSFTP(scanCtx, r, f.hostKeyCallback(p))
	if err != nil {
		f.log.Warn("sftp connect failed", "function", "fetchPipeline", "pipeline", p.Name, "host", r.Host, "err", err.Error())
		f.reschedule(p, err)
		return
	}
	defer conn.Close()

	entries, err := conn.list(scanCtx, r.Path)
	if err != nil {
		f.log.Warn("sftp list failed", "function", "fetchPipeline", "pipeline", p.Name, "path", r.Path, "err", err.Error())
		f.reschedule(p, err)
		return
	}

	var wg sync.WaitGroup
loop:
	for _, name := range entries {
		if scanCtx.Err() != nil {
			break // shutting down — stop starting new transfers (in-flight drain)
		}
		if f.skip(scanCtx, p, sg, inputDir, name) {
			continue
		}
		select {
		case f.sem <- struct{}{}:
		case <-scanCtx.Done():
			break loop // stop starting new transfers; already-started ones drain
		}
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			defer func() { <-f.sem }()
			f.fetchOne(procBase, p, conn, sg, inputDir, name)
		}(name)
	}
	wg.Wait()
	f.reschedule(p, nil) // a fetch cycle completed without a connection-level error
}

// skip reports whether a remote file should not be fetched: it is already in the
// input area awaiting processing, already processed (a re-arrival), or quarantined
// as poison. Prevents re-fetching under PostFetch=="leave".
func (f *Fetcher) skip(ctx context.Context, p store.Pipeline, sg storage.Store, inputDir, name string) bool {
	if q, err := f.store.AlreadyQuarantined(ctx, p.SrcUID, name); err == nil && q {
		return true
	}
	if done, err := f.store.AlreadyProcessed(ctx, p.SrcUID, name); err == nil && done {
		return true
	}
	if _, err := sg.Stat(ctx, path.Join(inputDir, name)); err == nil {
		return true // already landed, not yet processed
	} else if !errors.Is(err, storage.ErrNotFound) {
		// transient stat error — be safe and skip this pass rather than duplicate
		f.log.Warn("input stat failed; skipping file this pass", "function", "skip", "pipeline", p.Name, "file", name, "err", err.Error())
		return true
	}
	return false
}

// fetchOne streams a single remote file into the input area and applies the
// remote post-fetch disposition (leave/delete/move).
func (f *Fetcher) fetchOne(ctx context.Context, p store.Pipeline, conn *sftpConn, sg storage.Store, inputDir, name string) {
	remotePath := path.Join(p.Source.Remote.Path, name)
	inputKey := path.Join(inputDir, name)
	if err := conn.stream(ctx, remotePath, sg, inputKey); err != nil {
		f.log.Error("fetch file failed", "function", "fetchOne", "pipeline", p.Name, "file", name, "err", err.Error())
		return
	}
	f.log.Info("fetched file", "function", "fetchOne", "pipeline", p.Name, "file", name, "input", inputKey)
	if err := conn.postFetch(ctx, p.Source.Remote, remotePath, name); err != nil {
		f.log.Warn("post-fetch disposition failed", "function", "fetchOne", "pipeline", p.Name, "file", name, "err", err.Error())
	}
}

// reschedule sets the pipeline's next-due time: PollSeconds on success, or an
// exponential backoff (capped) after a connection-level error.
func (f *Fetcher) reschedule(p store.Pipeline, err error) {
	poll := time.Duration(p.Source.PollSeconds) * time.Second
	if poll <= 0 {
		poll = defaultPollSeconds * time.Second
	}
	if err == nil {
		f.fails[p.ID] = 0
		f.nextDue[p.ID] = time.Now().Add(poll)
		return
	}
	n := f.fails[p.ID] + 1
	f.fails[p.ID] = n
	back := poll
	for i := 1; i < n && back < maxBackoff; i++ {
		back *= 2
	}
	if back > maxBackoff {
		back = maxBackoff
	}
	f.nextDue[p.ID] = time.Now().Add(back)
}

// hostKeyCallback builds the ssh host-key verifier for a pipeline: a hard pin when
// Source.Remote.HostKey is set, otherwise trust-on-first-use backed by the local
// known-hosts cache (the store exposes no pipeline-update method to persist the
// captured key, so it is remembered locally).
func (f *Fetcher) hostKeyCallback(p store.Pipeline) hostKeyVerifier {
	return hostKeyVerifier{pinned: p.Source.Remote.HostKey, store: f.hostKeys, log: f.log, pipeline: p.Name}
}

// storeFor resolves (and caches) the input-area storage.Store for a pipeline.
func (f *Fetcher) storeFor(ctx context.Context, p store.Pipeline) (storage.Store, error) {
	key := storage.SourceKey(p.Source)
	f.mu.Lock()
	defer f.mu.Unlock()
	if key != "" {
		if s, ok := f.stores[key]; ok {
			return s, nil
		}
	}
	s, err := f.resolve(ctx, p.Source)
	if err != nil {
		return nil, err
	}
	if key != "" {
		f.stores[key] = s
	}
	return s, nil
}

func (f *Fetcher) closeStores() {
	f.mu.Lock()
	defer f.mu.Unlock()
	for k, s := range f.stores {
		_ = s.Close()
		delete(f.stores, k)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
