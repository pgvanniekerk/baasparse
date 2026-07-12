package watcher

import (
	"context"
	"errors"
	"fmt"
	"path"
	"sort"
	"strconv"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/runner"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// maxBatchAttempts retires a batch that has delivered NOWHERE and cannot be
// completed, rather than retrying it forever. It is deliberately generous: a
// destination outage lasting minutes must not cost the batch.
//
// It does NOT apply to a batch that has already published to some destination.
// Such a batch is never abandoned at any attempt count — giving up would release
// its files, they would re-form as a new batch, and the destinations that already
// hold their records would receive them a second time. It stays open and keeps
// retrying only what it still owes.
const maxBatchAttempts = 200

// scanBatched is the consolidation path (TS 07 §7.3.2): instead of one output per
// input file, many input files are concatenated into ONE output per destination.
//
// Order matters here. OPEN batches are resumed FIRST, before any new batch is
// formed, because an open batch may already have published to some destinations —
// its member set is therefore frozen, and sweeping those files into a fresh batch
// would re-deliver their records to the destinations that already have them.
func (w *Watcher) scanBatched(scanCtx, procBase context.Context, p store.Pipeline, sg storage.Store, entries []storage.Entry, d dirs) {
	if p.SrcUID == 0 {
		return
	}
	open, err := w.store.ListOpenBatches(scanCtx, p.SrcUID)
	if err != nil {
		w.log.Error("list open batches failed", "function", "scanBatched", "pipeline", p.Name, "err", err.Error())
		return
	}
	byName := map[string]storage.Entry{}
	for _, e := range entries {
		byName[path.Base(e.Key)] = e
	}

	// 1) Finish what a previous attempt started.
	for _, b := range open {
		if scanCtx.Err() != nil {
			return
		}
		w.submitBatch(scanCtx, procBase, p, sg, b, byName, d)
	}

	// 2) Form a new batch from the files that are NOT already spoken for.
	pinned, err := w.store.BatchedFileNames(scanCtx, p.SrcUID)
	if err != nil {
		w.log.Error("list batched files failed", "function", "scanBatched", "pipeline", p.Name, "err", err.Error())
		return
	}
	// Re-arrival guard (BR-COL-006), the batch-path equivalent of the per-file
	// AlreadyProcessed/AlreadyQuarantined checks. A file that is already DONE but
	// still sitting in the input area — left there by the "leaveMarked" disposition,
	// re-dropped by an operator, or lingering because we died between committing its
	// record and moving it — must NEVER be batched again, or its records would be
	// delivered to every destination a second time.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, path.Base(e.Key))
	}
	settled, err := w.store.SettledFileNames(scanCtx, p.SrcUID, names)
	if err != nil {
		w.log.Error("re-arrival check failed", "function", "scanBatched", "pipeline", p.Name, "err", err.Error())
		return
	}
	free := make([]storage.Entry, 0, len(entries))
	for _, e := range entries {
		n := path.Base(e.Key)
		if pinned[n] || settled[n] {
			continue
		}
		free = append(free, e)
	}
	if len(free) == 0 {
		return
	}
	// Deterministic membership: oldest first, then by name. Two instances seeing
	// the same directory therefore propose the same batch and collide on the
	// identity index rather than forming two overlapping batches.
	sort.Slice(free, func(i, j int) bool {
		if !free[i].ModTime.Equal(free[j].ModTime) {
			return free[i].ModTime.Before(free[j].ModTime)
		}
		return free[i].Key < free[j].Key
	})

	bs := p.Batch
	limit := bs.Files()
	if len(free) > limit {
		free = free[:limit]
	}
	var bytes int64
	for _, e := range free {
		bytes += e.Size
	}

	// Admission delay: hold a partial group back until it is either big enough or
	// old enough. This is what gives an open-time trigger WITHOUT holding an output
	// file open across scans — the files simply wait in the input area, which is
	// already durable, already claimed by nobody, and already crash-safe.
	full := len(free) >= limit || (bs.MaxBytes > 0 && bytes >= bs.MaxBytes)
	if !full {
		age := bs.MaxAge()
		if age <= 0 {
			return // no age trigger: wait for a full batch rather than dribbling out tiny ones
		}
		oldest := free[0].ModTime
		if oldest.IsZero() || time.Since(oldest) < age {
			return // still filling up
		}
	}

	files := make([]store.BatchFile, 0, len(free))
	for _, e := range free {
		files = append(files, store.BatchFile{Name: path.Base(e.Key), Size: e.Size})
	}
	b, err := w.store.FormBatch(scanCtx, p.ID, p.SrcUID, files)
	if err != nil {
		if errors.Is(err, store.ErrBatchOverlap) {
			// Another instance formed a batch over these files between our listing and
			// our insert. Theirs wins; it is resumed on the next scan. Forming an
			// overlapping batch here would deliver the shared files twice.
			return
		}
		w.log.Error("form batch failed", "function", "scanBatched", "pipeline", p.Name, "err", err.Error())
		return
	}
	w.submitBatch(scanCtx, procBase, p, sg, b, byName, d)
}

// submitBatch runs one batch on a worker slot, holding an in-flight guard on the
// batch identity so this instance never runs the same batch twice concurrently.
func (w *Watcher) submitBatch(scanCtx, procBase context.Context, p store.Pipeline, sg storage.Store,
	b store.OpenBatch, byName map[string]storage.Entry, d dirs) {

	key := "batch|" + strconv.FormatInt(p.SrcUID, 10) + "|" + b.Identity
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
	case w.sem <- struct{}{}:
	case <-scanCtx.Done():
		release()
		return
	}
	w.wg.Add(1)
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				w.log.Error("recovered panic in batch worker", "function", "submitBatch",
					"pipeline", p.Name, "batch", b.Identity, "err", fmt.Sprint(rec))
			}
			<-w.sem
			release()
			w.wg.Done()
		}()
		w.handleBatch(procBase, p, sg, b, byName, d)
	}()
}

// handleBatch claims every member of a batch, runs it, and releases the claims.
//
// It is all-or-nothing on the CLAIMS (not on the destinations): a batch can only
// run if this instance holds every one of its members, because the output must
// contain all of them. If another instance holds one, we release what we took and
// let them finish it.
func (w *Watcher) handleBatch(ctx context.Context, p store.Pipeline, sg storage.Store,
	b store.OpenBatch, byName map[string]storage.Entry, d dirs) {

	ctx, cancelAll := context.WithTimeout(ctx, 2*MaxProcessing)
	defer cancelAll()

	doneStore, err := w.destStoreFor(ctx, p, sg)
	if err != nil {
		w.log.Error("resolve done storage failed", "function", "handleBatch", "pipeline", p.Name, "err", err.Error())
		return
	}

	// Claim every member. A member that has vanished from the input area (already
	// processed, or moved by an operator) makes the frozen set unreachable.
	members := make([]runner.Member, 0, len(b.Files))
	claimed := make([]store.Claim, 0, len(b.Files))
	releaseAll := func() {
		for _, c := range claimed {
			_ = w.store.ReleaseClaim(ctx, c)
		}
	}
	for _, f := range b.Files {
		e, ok := byName[f.Name]
		if !ok {
			// The file is gone. If every destination already has this batch, the
			// records are safe and the batch just needs closing — but we cannot close
			// it without the claims, so abandon it and let the operator see why.
			releaseAll()
			w.log.Error("batch member missing from input", "function", "handleBatch",
				"pipeline", p.Name, "batch", b.Identity, "file", f.Name)
			if n, terr := w.store.TouchBatchAttempt(ctx, b.UID, "member missing: "+f.Name); terr == nil && n >= maxBatchAttempts {
				// Refused when something already delivered — see AbandonBatch.
				if aerr := w.store.AbandonBatch(ctx, b.UID, "member missing from input: "+f.Name); aerr != nil {
					w.log.Error("STUCK BATCH: member missing but the batch is already partly delivered",
						"function", "handleBatch", "pipeline", p.Name, "batch", b.Identity, "file", f.Name)
				}
			}
			return
		}
		claim, ok, err := w.store.ClaimFile(ctx, p.SrcUID, f.Name)
		if err != nil {
			releaseAll()
			w.log.Error("claim failed", "function", "handleBatch", "pipeline", p.Name, "file", f.Name, "err", err.Error())
			return
		}
		if !ok {
			releaseAll() // another instance holds a member: it will run this batch
			return
		}
		claimed = append(claimed, claim)
		uid, err := w.store.NextFileUID(ctx)
		if err != nil {
			releaseAll()
			w.log.Error("allocate file uid failed", "function", "handleBatch", "pipeline", p.Name, "err", err.Error())
			return
		}
		members = append(members, runner.Member{
			Name: f.Name, Key: e.Key, Size: e.Size, FileUID: uid, Claim: claim,
		})
	}
	if len(members) == 0 {
		return
	}

	// Reserve each destination's delivery: sequence number and output name, once,
	// reused by every retry of this batch.
	dests, err := w.batchDests(ctx, p, sg, doneStore, b.Batch)
	if err != nil {
		releaseAll()
		w.log.Error("resolve destinations failed", "function", "handleBatch", "pipeline", p.Name, "err", err.Error())
		return
	}

	procCtx, cancel := context.WithTimeout(ctx, MaxProcessing)
	defer cancel()
	stopHB := w.startBatchHeartbeat(procCtx, p, members, cancel)

	res, perr := w.runBatchProtected(procCtx, p, sg, doneStore, b, members, dests, d)
	stopHB()

	if perr == nil && res.Committed {
		releaseAll() // no-op: CloseBatch released them; harmless if a claim is gone
		return
	}

	var badMember *runner.BadMemberError
	switch {
	case errors.Is(perr, runner.ErrLostClaim):
		w.log.Warn("batch claim lost; handed off", "function", "handleBatch", "pipeline", p.Name, "batch", b.Identity)
	case errors.As(perr, &badMember):
		// ONE file is malformed. Quarantine it on its own claim and drop it from the
		// batch — the other nine are innocent and must not be condemned with it, nor
		// stalled forever by a file that will never parse.
		w.quarantineMember(ctx, p, sg, b, members, badMember, d)
	default:
		if perr != nil {
			n, terr := w.store.TouchBatchAttempt(ctx, b.UID, perr.Error())
			if terr == nil && n >= maxBatchAttempts {
				// Only a batch that delivered NOWHERE may be abandoned; the store
				// refuses otherwise, because releasing a partially-delivered batch's
				// files would re-batch and re-send them to destinations that already
				// hold them.
				aerr := w.store.AbandonBatch(ctx, b.UID, perr.Error())
				switch {
				case aerr == nil:
					w.log.Error("batch abandoned after repeated failures (nothing was delivered)",
						"function", "handleBatch", "pipeline", p.Name, "batch", b.Identity,
						"attempts", n, "err", perr.Error())
				case errors.Is(aerr, store.ErrBatchPartiallyDelivered):
					w.log.Error("STUCK BATCH: a destination is failing but others already hold this data; "+
						"it will keep retrying and its files stay pinned — operator attention required",
						"function", "handleBatch", "pipeline", p.Name, "batch", b.Identity,
						"attempts", n, "delivered", res.Delivered, "failed", res.Failed, "err", perr.Error())
				default:
					w.log.Error("abandon batch failed", "function", "handleBatch",
						"pipeline", p.Name, "batch", b.Identity, "err", aerr.Error())
				}
			} else {
				w.log.Error("batch failed; will retry", "function", "handleBatch", "pipeline", p.Name,
					"batch", b.Identity, "delivered", res.Delivered, "failed", res.Failed, "err", perr.Error())
			}
		}
	}
	// Release the claims so the retry is lease-paced rather than a tight loop. The
	// batch stays OPEN, its delivered destinations stay delivered, and the next
	// scan resumes exactly where this one stopped.
	releaseAll()
}

// batchDests resolves each destination's store and reserves its delivery.
func (w *Watcher) batchDests(ctx context.Context, p store.Pipeline, sg, doneStore storage.Store, b store.Batch) ([]runner.Destination, error) {
	out := make([]runner.Destination, 0, len(p.Outputs))
	for _, o := range p.Outputs {
		st := doneStore
		if o.HasDest() {
			var err error
			st, err = w.cachedStore(storage.DestKey(o.Dest), func() (storage.Store, error) {
				return storage.NewForDest(ctx, o.Dest)
			})
			if err != nil {
				return nil, fmt.Errorf("destination %q: %w", o.Name, err)
			}
		}
		o := o
		del, err := w.store.ReserveDelivery(ctx, p.ID, o, b, func(seq int64) string {
			return runner.BatchOutputName(p, o, seq)
		})
		if err != nil {
			return nil, fmt.Errorf("reserve delivery %q: %w", o.Name, err)
		}
		out = append(out, runner.Destination{Output: o, Store: st, Delivery: del})
	}
	return out, nil
}

func (w *Watcher) runBatchProtected(ctx context.Context, p store.Pipeline, sg, doneStore storage.Store,
	b store.OpenBatch, members []runner.Member, dests []runner.Destination, d dirs) (res runner.BatchResult, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("panic processing batch: %v", rec)
			w.log.Error("recovered panic", "function", "runBatchProtected", "pipeline", p.Name, "err", err.Error())
		}
	}()
	return w.runner.ProcessBatch(ctx, p, sg, doneStore, b, members, dests, d.done)
}

// quarantineMember removes one malformed file from a batch: it is quarantined
// under its own claim (so it is visible with a reason and never retried), dropped
// from the frozen set, and the remaining files re-form on the next scan.
func (w *Watcher) quarantineMember(ctx context.Context, p store.Pipeline, sg storage.Store,
	b store.OpenBatch, members []runner.Member, bad *runner.BadMemberError, d dirs) {

	var m runner.Member
	for _, x := range members {
		if x.Name == bad.Name {
			m = x
			break
		}
	}
	if m.Name == "" {
		return
	}
	// Dropping is refused once any destination has published this batch — the
	// member set is what those destinations were told.
	if err := w.store.DropBatchFile(ctx, b.Batch, bad.Name); err != nil {
		w.log.Error("cannot drop bad member from a partially delivered batch", "function", "quarantineMember",
			"pipeline", p.Name, "batch", b.Identity, "file", bad.Name, "err", err.Error())
		_ = w.store.AbandonBatch(ctx, b.UID, "bad member after partial delivery: "+bad.Name)
		return
	}
	w.quarantine(ctx, p, sg, m.Key, m.Name, bad.Reason, m.Claim, d)
	// The batch identity changes with its membership, so the old (now stale) batch
	// is retired and the survivors re-form next scan. This is only safe because
	// DropBatchFile above refused if anything had already delivered — re-forming a
	// partially-delivered batch would re-send its records.
	if aerr := w.store.AbandonBatch(ctx, b.UID, "re-formed without bad member "+bad.Name); aerr != nil {
		w.log.Error("could not retire batch after dropping bad member", "function", "quarantineMember",
			"pipeline", p.Name, "batch", b.Identity, "err", aerr.Error())
	}
	w.log.Warn("dropped bad member; batch will re-form without it", "function", "quarantineMember",
		"pipeline", p.Name, "batch", b.Identity, "file", bad.Name)
}

// startBatchHeartbeat renews every member's lease while the batch runs, cancelling
// the work if any one of them is lost — the output must contain all members, so a
// single lost claim invalidates the whole run.
func (w *Watcher) startBatchHeartbeat(ctx context.Context, p store.Pipeline, members []runner.Member, onLost func()) func() {
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		t := time.NewTicker(store.HeartbeatSeconds * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-t.C:
				for _, m := range members {
					ok, err := w.store.HeartbeatClaim(ctx, m.Claim)
					if err != nil {
						w.log.Warn("batch heartbeat failed", "function", "startBatchHeartbeat",
							"pipeline", p.Name, "file", m.Name, "err", err.Error())
						continue
					}
					if !ok {
						w.log.Warn("batch claim lost during processing", "function", "startBatchHeartbeat",
							"pipeline", p.Name, "file", m.Name)
						onLost()
						return
					}
				}
			}
		}
	}()
	return func() {
		close(stop)
		<-done
	}
}
