package runner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/klauspost/compress/gzip"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/pgvanniekerk/baasparse/internal/pipeline"
	"github.com/pgvanniekerk/baasparse/internal/reconcile"
	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// Member is one claimed input file of a batch.
type Member struct {
	Name    string
	Key     string
	Size    int64
	FileUID int64
	Claim   store.Claim
}

// Destination is one output of a batch: where it goes, on which store, under
// which reserved delivery (sequence + output name).
type Destination struct {
	Output   store.Output
	Store    storage.Store
	Delivery store.Delivery
}

// BadMemberError names the ONE input file in a batch whose content is bad. The
// caller drops it from the batch, quarantines it on its own claim, and re-forms
// the batch from the survivors — one malformed file must not condemn the other
// nine (and must not stall them forever by poisoning every retry).
type BadMemberError struct {
	Name   string
	Reason string
}

func (e *BadMemberError) Error() string { return e.Name + ": " + e.Reason }

// BatchResult reports what the batch actually achieved.
type BatchResult struct {
	Delivered []string // destination names that now hold the output
	Failed    []string // destination names to retry (their sequence stays reserved)
	Committed bool     // every destination delivered, so the files are DONE
	RecordsIn int
	Files     []store.ProcessedFile
}

// ProcessBatch concatenates N claimed input files into ONE output object per
// destination (TS 07 §7.3.2): ten 100-record files in, one 1000-record file out,
// per destination, each in that destination's own format.
//
// Destinations are written CONCURRENTLY and succeed or fail INDEPENDENTLY. A
// destination that is already DELIVERED for this batch (a previous attempt got
// there) is skipped entirely — that is what makes a retry safe rather than a
// source of duplicates. The contributing files are committed as DONE only when
// every destination has delivered (BR-COL-009); until then the batch stays OPEN
// and this returns a partial result for the caller to retry.
func (r *Runner) ProcessBatch(ctx context.Context, p store.Pipeline, src, doneStore storage.Store,
	b store.OpenBatch, members []Member, dests []Destination, donePrefix string) (BatchResult, error) {

	ctx, span := r.tracer.Start(ctx, "ProcessBatch")
	defer span.End()
	start := time.Now()
	span.SetAttributes(
		attribute.String("baasparse.pipeline", p.Name),
		attribute.String("baasparse.batch", b.Identity),
		attribute.Int("baasparse.batch_files", len(members)),
		attribute.Int("baasparse.destinations", len(dests)),
	)
	blog := r.log.With("function", "ProcessBatch", "pipeline", p.Name, "batch", b.Identity, "files", len(members))
	pAttr := metric.WithAttributes(attribute.String("pipeline", p.Name))

	// Only write destinations that do not already hold this batch.
	var pending []Destination
	res := BatchResult{}
	for _, d := range dests {
		if b.Delivered[d.Output.DSUID] {
			res.Delivered = append(res.Delivered, d.Output.Name)
			continue
		}
		pending = append(pending, d)
	}

	var sess *pipeline.Session
	var runs []*destRun
	// A panic anywhere between starting the destination writers and finishing them
	// would strand one goroutine (and one open output object) per destination, for
	// the life of the process. Guarantee they are torn down on every exit path.
	defer func() {
		for _, run := range runs {
			run.abort()
		}
	}()
	if len(pending) > 0 {
		var err error
		runs, sess, err = r.startDests(ctx, p, pending)
		if err != nil {
			return res, err
		}

		// One decode pass over every member, fanned out to all pending destinations.
		decodeErr := r.consumeMembers(ctx, sess, src, members)

		stats, susp, targets := sess.Close()
		res.RecordsIn = stats.RecordsIn

		// Finish each destination's stream.
		//
		// If the BATCH failed — a malformed member, an unreadable source, a
		// cancellation — every destination's pipe must be ABORTED, not closed cleanly,
		// even for destinations that encoded without complaint. Closing them cleanly
		// commits an object holding only the records decoded BEFORE the failure, at a
		// real sequence number. The batch then re-forms without the bad file and
		// delivers those same records again, so the destination ends up holding a
		// partial object AND a complete one and bills the overlap twice. An aborted
		// pipe makes Put fail, so nothing is ever committed.
		for i, run := range runs {
			abort := targets[i].Err
			if abort == nil {
				abort = decodeErr
			}
			run.finish(abort)
		}
		for _, run := range runs {
			<-run.done
		}

		// A bad input file is fatal to the WHOLE batch's encoding (the output would
		// be missing its records), but it is not fatal to the other files: name it so
		// the caller can quarantine it and re-form the batch without it.
		if decodeErr != nil {
			var bad *BadMemberError
			if errors.As(decodeErr, &bad) {
				return res, bad
			}
			return res, decodeErr
		}

		contribs := sess.Contributions()
		// Persist the per-file counts BEFORE any delivery is marked. If we crashed
		// between marking the last delivery and writing these, a resume would see the
		// batch fully delivered, find no counts, and commit the files as DONE with
		// ZERO records — a reconciliation lie about data that really was delivered.
		if err := r.store.RecordBatchContributions(ctx, b.UID, storeContribs(contribs)); err != nil {
			return res, fmt.Errorf("persist batch contributions: %w", err)
		}
		for i, run := range runs {
			d := pending[i]
			if err := run.err(targets[i].Err); err != nil {
				// This destination alone failed. Its sequence number stays reserved, so
				// the retry reuses it and no gap appears downstream.
				res.Failed = append(res.Failed, d.Output.Name)
				if merr := r.store.MarkDeliveryFailed(ctx, d.Delivery, err.Error()); merr != nil {
					blog.Warn("mark delivery failed", "destination", d.Output.Name, "err", merr.Error())
				}
				blog.Error("destination delivery failed", "destination", d.Output.Name, "err", err.Error())
				continue
			}
			d.Delivery.Records = int64(targets[i].RecordsOut)
			d.Delivery.SizeBytes = run.written()
			if err := r.store.MarkDelivered(ctx, d.Delivery); err != nil {
				return res, fmt.Errorf("record delivery %q: %w", d.Output.Name, err)
			}
			res.Delivered = append(res.Delivered, d.Output.Name)
			blog.Info("destination delivered", "destination", d.Output.Name,
				"output", d.Delivery.OutputName, "seq", d.Delivery.SequenceNo, "records", targets[i].RecordsOut)
		}

		if len(res.Failed) > 0 {
			// Partial: the healthy destinations HAVE their output and keep it. The
			// batch stays open; only the failed destinations are retried.
			return res, fmt.Errorf("batch %s: %d/%d destinations failed (%v)",
				b.Identity, len(res.Failed), len(dests), res.Failed)
		}
		res.Files = r.batchPFs(p, members, contribs, susp)
	} else {
		// Everything was already delivered on a previous attempt (we crashed between
		// the last delivery and the commit). Rebuild the file records from the counts
		// persisted at encode time — no re-read, no re-write, and no zero-record lie.
		res.Files = r.batchPFsFromBatch(p, members, b.Batch)
		for _, f := range b.Files {
			res.RecordsIn += int(f.RecordsIn)
		}
	}

	// Every destination holds the output: commit the files and release the claims,
	// atomically and only if we still hold every claim.
	claims := make([]store.Claim, len(members))
	for i, m := range members {
		claims[i] = m.Claim
	}
	contribByDest := map[int64][]store.Contribution{}
	var sessContribs []pipeline.Contribution
	if sess != nil {
		sessContribs = sess.Contributions()
	}
	lineage := storeContribs(sessContribs)
	if len(lineage) == 0 {
		// Resume path: no decode happened, so take the lineage from the counts
		// persisted when the batch was encoded — DC rows must not vanish just
		// because the commit is happening on a later attempt.
		// Rebuild the output offsets from the batch order: the files were encoded in
		// BF_SEQ order, so each one's index range follows the previous one's. Without
		// this, the resumed commit would write every contribution at offset 0 and the
		// per-file position within the consolidated object would be lost.
		var idx int64
		for _, f := range b.Files {
			c := store.Contribution{
				FileName: f.Name, Records: f.RecordsOut, RecordsIn: f.RecordsIn, Suspended: f.Suspended,
				FirstIndex: idx, LastIndex: idx + f.RecordsOut - 1,
			}
			if f.RecordsOut == 0 {
				c.FirstIndex, c.LastIndex = 0, -1
			}
			idx += f.RecordsOut
			lineage = append(lineage, c)
		}
	}
	for _, d := range dests {
		contribByDest[d.Output.DSUID] = append(contribByDest[d.Output.DSUID], lineage...)
	}
	dls := make([]store.Delivery, 0, len(dests))
	for _, d := range dests {
		dls = append(dls, d.Delivery)
	}

	committed, err := r.store.CloseBatch(ctx, b.Batch, res.Files, claims, dls, contribByDest)
	if err != nil {
		return res, fmt.Errorf("commit batch: %w", err)
	}
	if !committed {
		// A member's claim was taken over while we worked. The outputs are already
		// published and their deliveries recorded, so the winning instance will find
		// the batch fully delivered and commit it — we simply recorded nothing.
		blog.Warn("did not commit batch (claim lost); another instance will close it")
		return res, ErrLostClaim
	}
	res.Committed = true

	// Markers + source disposition, per file, exactly as the single-file path does.
	for _, m := range members {
		pf := findPF(res.Files, m.Name)
		if donePrefix != "" {
			mk := reconcile.Marker{
				FileUID: m.FileUID, PipelineID: p.ID, Name: m.Name, OutputName: batchOutputNames(dests),
				Size: m.Size, RecordsIn: pf.RecordsIn, RecordsOut: pf.RecordsOut, Suspended: pf.Suspended,
				CompletedAt: time.Now().UTC(),
			}
			if err := reconcile.WriteMarker(ctx, doneStore, donePrefix, mk); err != nil {
				blog.Warn("write completion marker failed", "file", m.Name, "err", err.Error())
			}
		}
		r.disposeSource(ctx, p, src, doneStore, m.Key, m.Name, donePrefix, blog)
	}

	r.mFiles.Add(ctx, int64(len(members)), pAttr)
	r.mIn.Add(ctx, int64(res.RecordsIn), pAttr)
	r.mDur.Record(ctx, time.Since(start).Seconds(), pAttr)
	blog.Info("batch consolidated", "files", len(members), "records", res.RecordsIn,
		"destinations", len(dests), "took", time.Since(start).String())
	return res, nil
}

// consumeMembers decodes each claimed file into the shared session, in batch
// order, so the output is a deterministic concatenation.
func (r *Runner) consumeMembers(ctx context.Context, sess *pipeline.Session, src storage.Store, members []Member) error {
	for _, m := range members {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !sess.Live() {
			return fmt.Errorf("all destinations failed") // nothing left to write into
		}
		rc, err := src.Open(ctx, m.Key)
		if err != nil {
			return fmt.Errorf("open %s: %w", m.Key, err)
		}
		sr := &srcReader{r: rc}
		cerr := sess.Consume(ctx, sr, m.Name)
		rc.Close()
		if cerr == nil {
			continue
		}
		// Write-side and stream-side failures are NOT this file's fault.
		var encErr *pipeline.EncodeError
		if errors.As(cerr, &encErr) || errors.Is(cerr, context.Canceled) || errors.Is(cerr, context.DeadlineExceeded) {
			return cerr
		}
		if sr.err != nil {
			return fmt.Errorf("read %s (transient): %w", m.Name, sr.err)
		}
		// Content: this specific file is malformed.
		return &BadMemberError{Name: m.Name, Reason: "DECODE_ERROR: " + cerr.Error()}
	}
	return nil
}

// destRun is one destination's in-flight write: the pipeline encodes into pw,
// storage.Put drains pr. The two run concurrently so nothing is buffered in
// memory, and an aborted pipe means the destination never sees a partial object.
type destRun struct {
	d      Destination
	key    string
	pr     *io.PipeReader
	pw     *io.PipeWriter
	gz     *gzip.Writer
	count  *countingWriter
	putErr error
	done   chan struct{}
}

func (r *destRun) written() int64 { return r.count.n }

// abort tears the write down if it is still in flight. It is idempotent: on the
// normal path finish() has already closed the pipe and the Put goroutine has
// exited, so this is a no-op.
func (r *destRun) abort() {
	select {
	case <-r.done:
		return // already finished
	default:
	}
	_ = r.pw.CloseWithError(errAborted)
	<-r.done
}

var errAborted = errors.New("batch aborted")

// finish flushes and closes this destination's stream. A target error aborts the
// pipe so Put fails and never commits a truncated object.
func (r *destRun) finish(targetErr error) {
	if targetErr != nil {
		_ = r.pw.CloseWithError(targetErr)
		return
	}
	if r.gz != nil {
		if err := r.gz.Close(); err != nil {
			_ = r.pw.CloseWithError(err)
			return
		}
	}
	_ = r.pw.Close()
}

// err folds the encode-side and write-side outcomes for this destination.
func (r *destRun) err(targetErr error) error {
	if targetErr != nil {
		return targetErr
	}
	return r.putErr
}

// startDests opens one concurrent write per pending destination and builds the
// session that fans records into all of them.
func (r *Runner) startDests(ctx context.Context, p store.Pipeline, dests []Destination) ([]*destRun, *pipeline.Session, error) {
	runs := make([]*destRun, 0, len(dests))
	targets := make([]pipeline.Target, 0, len(dests))
	for _, d := range dests {
		pr, pw := io.Pipe()
		run := &destRun{d: d, key: joinKey(d.Output.Dir, d.Delivery.OutputName), pr: pr, pw: pw, done: make(chan struct{})}
		run.count = &countingWriter{w: pw}
		var w io.Writer = run.count
		if d.Output.Format.Compress == "gzip" {
			gz, _ := gzip.NewWriterLevel(run.count, gzip.BestSpeed)
			run.gz = gz
			w = gz
		}
		runs = append(runs, run)
		targets = append(targets, pipeline.Target{
			Name: d.Output.Name, Transform: destTransform(p, d.Output), Format: d.Output.Format, W: w,
		})

		go func(run *destRun) {
			defer close(run.done)
			run.putErr = run.d.Store.Put(ctx, run.key, run.pr,
				storage.Meta{ContentType: contentType(run.d.Output.Format)})
			// Release a blocked encoder if Put died early, so one dead destination
			// cannot wedge the whole batch.
			_ = run.pr.CloseWithError(run.putErr)
		}(run)
	}
	// The session carries only the INPUT spec: shaping is per destination now, so
	// each target brings its own transform.
	sess, err := pipeline.NewMultiSession(pipeline.Spec{Input: p.Input}, targets)
	if err != nil {
		for _, run := range runs {
			_ = run.pw.CloseWithError(err)
			<-run.done
		}
		return nil, nil, err
	}
	return runs, sess, nil
}

// countingWriter measures the bytes actually delivered (post-compression), for
// the delivery record.
type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// destTransform is this destination's own output structure, falling back to the
// pipeline-level one for pipelines written before destinations carried their own.
func destTransform(p store.Pipeline, o store.Output) spec.TransformSpec {
	if o.Transform != nil {
		return *o.Transform
	}
	return p.Transform
}

// batchPFs builds the per-file records. Consolidation must not cost per-file
// reconciliation: each input file keeps its own counts (in = out + suspended),
// taken from its contribution to the shared output.
func (r *Runner) batchPFs(p store.Pipeline, members []Member, contribs []pipeline.Contribution, susp []pipeline.Suspended) []store.ProcessedFile {
	byName := map[string]pipeline.Contribution{}
	for _, c := range contribs {
		byName[c.Member] = c
	}
	suspByName := map[string]int{}
	for _, s := range susp {
		suspByName[s.Member]++
	}
	out := make([]store.ProcessedFile, 0, len(members))
	for _, m := range members {
		c := byName[m.Name]
		out = append(out, store.ProcessedFile{
			FileUID:      m.FileUID,
			PipelineID:   p.ID,
			PipelineName: p.Name,
			Name:         m.Name,
			Size:         m.Size,
			Status:       "DONE",
			RecordsIn:    int(c.RecordsIn),
			RecordsOut:   int(c.RecordsOut),
			Suspended:    suspByName[m.Name],
			CollectedOn:  time.Now().UTC(),
		})
	}
	return out
}

// batchPFsFromBatch rebuilds the per-file records from the counts persisted when
// the batch was encoded — the resume path, where there is no decode to ask.
func (r *Runner) batchPFsFromBatch(p store.Pipeline, members []Member, b store.Batch) []store.ProcessedFile {
	byName := map[string]store.BatchFile{}
	for _, f := range b.Files {
		byName[f.Name] = f
	}
	out := make([]store.ProcessedFile, 0, len(members))
	for _, m := range members {
		f := byName[m.Name]
		out = append(out, store.ProcessedFile{
			FileUID: m.FileUID, PipelineID: p.ID, PipelineName: p.Name, Name: m.Name, Size: m.Size,
			Status: "DONE", RecordsIn: int(f.RecordsIn), RecordsOut: int(f.RecordsOut),
			Suspended: int(f.Suspended), CollectedOn: time.Now().UTC(),
		})
	}
	return out
}

// storeContribs converts the session's per-stream lineage into the persistence shape.
func storeContribs(cs []pipeline.Contribution) []store.Contribution {
	out := make([]store.Contribution, 0, len(cs))
	for _, c := range cs {
		out = append(out, store.Contribution{
			FileName: c.Member, Records: c.RecordsOut, RecordsIn: c.RecordsIn,
			Suspended: c.Suspended, FirstIndex: c.FirstIndex, LastIndex: c.LastIndex,
		})
	}
	return out
}

func findPF(pfs []store.ProcessedFile, name string) store.ProcessedFile {
	for _, pf := range pfs {
		if pf.Name == name {
			return pf
		}
	}
	return store.ProcessedFile{}
}

// batchOutputNames summarizes the objects a batch produced, for the completion
// marker (which is a per-FILE record, but a consolidated file lands in several).
func batchOutputNames(dests []Destination) string {
	if len(dests) == 1 {
		return dests[0].Delivery.OutputName
	}
	s := ""
	for i, d := range dests {
		if i > 0 {
			s += ","
		}
		s += d.Output.Name + "=" + d.Delivery.OutputName
	}
	return s
}

// disposeSource applies the pipeline's done disposition to one input file, after
// its record is committed. A failure here is safe: the file is already DONE, so a
// lingering source is caught as a re-arrival next scan and never reprocessed.
func (r *Runner) disposeSource(ctx context.Context, p store.Pipeline, src, doneStore storage.Store, key, name, donePrefix string, blog *slog.Logger) {
	switch p.Source.Disposition {
	case "delete":
		if err := src.Delete(ctx, key); err != nil {
			blog.Warn("delete source failed (already recorded)", "err", err.Error())
		}
	case "leaveMarked":
		// intentionally left in place
	default:
		if donePrefix != "" {
			if err := storage.Relocate(ctx, doneStore, joinKey(donePrefix, name), src, key); err != nil {
				blog.Warn("move to done failed (already recorded; re-arrival check will re-move)", "err", err.Error())
			}
		}
	}
}

// BatchOutputName renders a consolidated output's name. It carries the destination
// SEQUENCE, not an input file name — a batch has no single input file, and the
// sequence is what downstream uses to detect a missing output (BR-DST-011).
func BatchOutputName(p store.Pipeline, o store.Output, seq int64) string {
	base := slugName(p.Name) + "-" + slugName(o.Name) + "-" + fmt.Sprintf("%09d", seq)
	return base + outputExt(o.Format)
}

func slugName(s string) string {
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b = append(b, c)
		case c >= 'A' && c <= 'Z':
			b = append(b, c+32)
		default:
			if len(b) > 0 && b[len(b)-1] != '-' {
				b = append(b, '-')
			}
		}
	}
	for len(b) > 0 && b[len(b)-1] == '-' {
		b = b[:len(b)-1]
	}
	if len(b) == 0 {
		return "out"
	}
	return string(b)
}
