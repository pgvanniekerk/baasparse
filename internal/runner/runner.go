// Package runner executes a configured pipeline — either as an in-memory preview
// (GUI transform modeller, BR-UI-009) or against a real file with atomic output
// and a persisted Processed File record (data plane). All file access goes
// through the storage.Store abstraction (TS 16 §16.2), so the same code runs over
// POSIX/SFTP/S3 backends. It is shared by the GUI and the background watcher.
package runner

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strconv"
	"time"

	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/gzip"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/pgvanniekerk/baasparse/internal/container"
	"github.com/pgvanniekerk/baasparse/internal/pipeline"
	"github.com/pgvanniekerk/baasparse/internal/reconcile"
	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// Runner ties the pipeline core to persistence and storage.
type Runner struct {
	store   store.Store
	log     *slog.Logger
	tracer  trace.Tracer
	mFiles  metric.Int64Counter
	mIn     metric.Int64Counter
	mOut    metric.Int64Counter
	mSusp   metric.Int64Counter
	mDur    metric.Float64Histogram
}

// New builds a Runner.
func New(st store.Store, log *slog.Logger) *Runner {
	m := otel.Meter("baasparse/runner")
	files, _ := m.Int64Counter("baasparse_files_processed_total", metric.WithDescription("Files processed"))
	in, _ := m.Int64Counter("baasparse_records_in_total", metric.WithDescription("Records decoded"))
	out, _ := m.Int64Counter("baasparse_records_out_total", metric.WithDescription("Records distributed"))
	susp, _ := m.Int64Counter("baasparse_records_suspended_total", metric.WithDescription("Records suspended"))
	dur, _ := m.Float64Histogram("baasparse_file_processing_seconds", metric.WithDescription("File processing duration (s)"))
	return &Runner{
		store: st, log: log.With("component", "runner"), tracer: otel.Tracer("baasparse/runner"),
		mFiles: files, mIn: in, mOut: out, mSusp: susp, mDur: dur,
	}
}

// PreviewResult is the outcome of an in-memory transform preview.
type PreviewResult struct {
	Output    string
	Stats     pipeline.Stats
	Suspended []pipeline.Suspended
	Err       string
}

// Preview runs a pipeline spec over a sample and returns the rendered output —
// no persistence, no storage. This powers the live transform modeller.
func (r *Runner) Preview(ctx context.Context, sp pipeline.Spec, sample []byte) PreviewResult {
	var out bytes.Buffer
	st, susp, err := pipeline.Run(ctx, sp, bytes.NewReader(sample), &out)
	res := PreviewResult{Output: out.String(), Stats: st, Suspended: susp}
	if err != nil {
		res.Err = err.Error()
	}
	return res
}

type runResult struct {
	stats     pipeline.Stats
	suspended []pipeline.Suspended
	members   int // archive members decoded (0 = plain file)
	err       error
	// srcErr is set when err originated BELOW the decoder — in the input stream
	// itself (storage read failure, decompression-limit breach, cancellation) —
	// rather than in the record content. See srcReader.
	srcErr error
}

// srcReader records the first real error the INPUT stream returns. It exists to
// keep a class of misdiagnosis out of the quarantine table: a decoder handed a
// failing reader sees a truncated token and reports what looks like malformed
// content ("unterminated string at line 1041"), burying the actual cause. The
// NDJSON scanner does exactly this — bufio.Scanner treats any read error as EOF
// and hands the partial line to the parser — so a decompression-bomb breach
// could surface as a phantom DECODE_ERROR instead of ARCHIVE_LIMIT_EXCEEDED, and
// a transient S3 blip could quarantine a perfectly good file.
//
// Content errors are MANUFACTURED by the decoder from bytes it read cleanly;
// stream errors PASS THROUGH this Read. That difference is the classifier's
// ground truth, and it holds no matter what a decoder does with the error.
type srcReader struct {
	r   io.Reader
	err error
}

func (s *srcReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && s.err == nil {
		s.err = err
	}
	return n, err
}

// archiveCorruption reports whether a stream error is a structural defect of the
// archive (bad gzip header/CRC, malformed tar, truncation) — content, and never
// valid on retry — rather than a transient failure of the object stream beneath
// it, which must be retried instead of quarantined.
func archiveCorruption(err error) bool {
	var ce flate.CorruptInputError
	return errors.Is(err, gzip.ErrChecksum) || errors.Is(err, gzip.ErrHeader) ||
		errors.Is(err, tar.ErrHeader) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.As(err, &ce)
}

// runStreams feeds the pipeline from a plain file, or — when the input Format
// Definition declares a tar.gz container (TS 04 §4.4.8) — from each matching
// archive member in order: fresh decoder per member, one shared encoder session
// (one output, header once). The archive stream gets one large read buffer in
// front of gzip (fewer S3 round-trips); Session reuses its own buffer per member.
func (r *Runner) runStreams(ctx context.Context, p store.Pipeline, in io.Reader, out io.Writer) runResult {
	if p.Input.Container != "targz" {
		src := &srcReader{r: in}
		st, susp, err := pipeline.Run(ctx, p.Runner(), src, out)
		return runResult{stats: st, suspended: susp, err: err, srcErr: src.err}
	}
	if pipeline.ReadBufferBytes > 0 {
		in = bufio.NewReaderSize(in, pipeline.ReadBufferBytes)
	}
	// The archive stream is itself a source: gzip/tar errors and limit breaches
	// surface through the member readers below, so capture at that seam.
	tg, err := container.OpenTarGz(in, p.Input.MemberGlob, container.Limits{})
	if err != nil {
		return runResult{err: err, srcErr: err} // bad gzip header: archive content
	}
	defer tg.Close()
	sess, err := pipeline.NewSession(p.Runner(), out)
	if err != nil {
		return runResult{err: err}
	}
	members := 0
	for tg.Next() {
		if err := ctx.Err(); err != nil {
			return runResult{members: members, err: err, srcErr: err}
		}
		members++
		src := &srcReader{r: tg.Reader()}
		if err := sess.Consume(ctx, src, tg.Member()); err != nil {
			// A failing member stream (limit breach, torn object read) is the real
			// cause even when the decoder reported it as a parse error; prefer it.
			if src.err != nil {
				return runResult{members: members, err: src.err, srcErr: src.err}
			}
			// Genuine content error: name the member. EncodeError is write-side and
			// already carries its own context.
			var ee *pipeline.EncodeError
			if errors.As(err, &ee) {
				return runResult{members: members, err: err}
			}
			return runResult{members: members, err: fmt.Errorf("member %q: %w", tg.Member(), err)}
		}
	}
	if err := tg.Err(); err != nil {
		return runResult{members: members, err: err, srcErr: err}
	}
	st, susp, targets := sess.Close()
	// Single-output path: the sole target's error IS the run's error.
	var cerr error
	for _, t := range targets {
		if t.Err != nil {
			cerr = t.Err
			break
		}
	}
	return runResult{stats: st, suspended: susp, members: members, err: cerr}
}

// ErrLostClaim is returned when the file claim was taken over by another
// instance during processing, so this instance recorded nothing.
var ErrLostClaim = errors.New("lost file claim during processing")

// BadFileError marks a file that failed decode/transform/encode — a content
// problem that will not resolve on retry (a malformed file, an unusable format).
// The watcher quarantines such a file immediately with Reason, rather than
// looping on it, and surfaces the reason to the operator (vs. infrastructure
// errors like a storage outage, which are retried). Per-record transform
// failures are suspensions, not BadFileErrors — only a whole-file failure is.
type BadFileError struct {
	Reason string
}

func (e *BadFileError) Error() string { return e.Reason }

// ProcessFile runs a pipeline against one stored object: it allocates a file
// UID, streams decode→transform→encode from the source object into an
// atomically-written output object, records the Processed File, and moves the
// source to the done prefix. Streaming via io.Pipe keeps memory bounded and
// preserves output atomicity on every backend (a mid-stream failure never
// commits a partial output).
//
// If claim is non-nil the record is committed under a fence-guarded transaction
// (CompleteClaimed): a takeover during processing makes this instance record
// nothing and return ErrLostClaim (BR-HA-004). If claim is nil (GUI upload) the
// record is unconditional.
//
// src is the input datasource's store (open + source disposition happen here);
// dst is the output datasource's store (output object, completion marker and the
// "done" move land here). For a same-backend pipeline dst == src. This split is
// what lets a pipeline read from one datasource and write done/output to another
// (e.g. read S3 → write FS).
func (r *Runner) ProcessFile(ctx context.Context, p store.Pipeline, src, dst storage.Store, srcKey, outputPrefix, donePrefix string, claim *store.Claim) (store.ProcessedFile, error) {
	start := time.Now()
	ctx, span := r.tracer.Start(ctx, "ProcessFile")
	defer span.End()

	name := path.Base(srcKey)
	fileUID, err := r.store.NextFileUID(ctx)
	if err != nil {
		return store.ProcessedFile{}, fmt.Errorf("allocate file uid: %w", err)
	}
	cid := strconv.FormatInt(fileUID, 10)
	span.SetAttributes(
		attribute.Int64("baasparse.file_uid", fileUID),
		attribute.String("baasparse.pipeline", p.Name),
		attribute.String("baasparse.backend", src.Backend()),
		attribute.String("baasparse.output_backend", dst.Backend()),
	)
	flog := r.log.With("function", "ProcessFile", "correlationID", cid, "pipeline", p.Name, "file", name, "backend", src.Backend())
	if sc := span.SpanContext(); sc.HasTraceID() {
		flog = flog.With("trace_id", sc.TraceID().String())
	}
	flog.Info("collecting file", "file_uid", fileUID, "src", srcKey)
	pAttr := metric.WithAttributes(attribute.String("pipeline", p.Name))

	var size int64
	if e, serr := src.Stat(ctx, srcKey); serr == nil {
		size = e.Size
	}

	rc, err := src.Open(ctx, srcKey)
	if err != nil {
		return store.ProcessedFile{}, fmt.Errorf("open source %s: %w", srcKey, err)
	}
	defer rc.Close()

	outName := outputName(name, fileUID, p.Output)
	outKey := joinKey(outputPrefix, outName)

	// Stream: the pipeline writes to a pipe; storage.Put reads it. A pipeline
	// error closes the pipe with that error, so Put aborts and never commits a
	// partial output (BR-STO-004, BR-DST-003). Compressed output chains a gzip
	// writer (BestSpeed — throughput over ratio, TS 07 §7.3) in front of the pipe;
	// gzip.Close flushes into the pipe, so a destination failure during the final
	// flush still classifies as a write-side (retryable) error.
	pr, pw := io.Pipe()
	resCh := make(chan runResult, 1)
	go func() {
		var w io.Writer = pw
		var gz *gzip.Writer
		if p.Output.Compress == "gzip" {
			gz, _ = gzip.NewWriterLevel(pw, gzip.BestSpeed)
			w = gz
		}
		res := r.runStreams(ctx, p, rc, w)
		if res.err == nil && gz != nil {
			if cerr := gz.Close(); cerr != nil {
				res.err = &pipeline.EncodeError{Err: fmt.Errorf("gzip close: %w", cerr)}
			}
		}
		_ = pw.CloseWithError(res.err)
		resCh <- res
	}()

	putErr := dst.Put(ctx, outKey, pr, storage.Meta{ContentType: contentType(p.Output)})
	// Close the read end so that if Put failed early (e.g. an S3 network error or a
	// full output volume) the pipeline goroutine's blocked pw.Write is released and
	// cannot leak. Harmless on the success path (Put already read to EOF).
	_ = pr.CloseWithError(putErr)
	res := <-resCh
	if res.err != nil {
		// Classify the failure. A destination write outage surfaces as an
		// EncodeError (the encoder's write into the output pipe fails when dst.Put
		// dies mid-stream), and a shutdown/timeout surfaces as a context error —
		// both are INFRASTRUCTURE/transient, so return a retryable error and let the
		// claim lease expire and re-run, rather than quarantining a valid input.
		// Only a genuine decode/content failure (a malformed input that will never
		// parse) becomes a BadFileError, which the watcher quarantines with a reason.
		var encErr *pipeline.EncodeError
		if errors.As(res.err, &encErr) || errors.Is(res.err, context.Canceled) ||
			errors.Is(res.err, context.DeadlineExceeded) || ctx.Err() != nil {
			return store.ProcessedFile{}, fmt.Errorf("pipeline run (transient): %w", res.err)
		}
		var limErr *container.LimitError
		if errors.As(res.err, &limErr) {
			// Decompression-bomb guard tripped: a content problem — quarantine with
			// its own reason code so operators can distinguish it from parse errors.
			return store.ProcessedFile{}, &BadFileError{Reason: "ARCHIVE_LIMIT_EXCEEDED: " + res.err.Error()}
		}
		// The error came up through the input stream rather than out of the record
		// content. If the archive is structurally broken it will never parse, so
		// quarantine it; anything else is the object stream faltering under us (a
		// torn S3 read, a stalled mount) and the file itself may be perfectly good —
		// let the claim lease expire and retry rather than condemning valid data.
		if res.srcErr != nil && !archiveCorruption(res.srcErr) {
			// Report srcErr, not res.err: the decoder's version of this failure is a
			// misleading parse error invented from a truncated read.
			return store.ProcessedFile{}, fmt.Errorf("input stream (transient): %w", res.srcErr)
		}
		return store.ProcessedFile{}, &BadFileError{Reason: "DECODE_ERROR: " + res.err.Error()}
	}
	if putErr != nil {
		return store.ProcessedFile{}, fmt.Errorf("write output %s: %w", outKey, putErr)
	}

	pf := store.ProcessedFile{
		FileUID:      fileUID,
		PipelineID:   p.ID,
		PipelineName: p.Name,
		Name:         name,
		Size:         size,
		Status:       "DONE",
		RecordsIn:    res.stats.RecordsIn,
		RecordsOut:   res.stats.RecordsOut,
		Suspended:    res.stats.Suspended,
		OutputName:   outName,
		CollectedOn:  time.Now().UTC(),
	}

	// Record the Processed File. Under a claim this is fence-guarded and atomic
	// with the claim release; a takeover or a lost claim-race means we recorded
	// nothing. On any not-recorded outcome the just-written output is deleted so
	// it does not orphan.
	if claim != nil {
		recorded, cerr := r.store.CompleteClaimed(ctx, pf, *claim)
		if cerr != nil {
			_ = dst.Delete(ctx, outKey)
			return store.ProcessedFile{}, fmt.Errorf("record (claimed): %w", cerr)
		}
		if !recorded {
			flog.Warn("did not record (claim lost / already processed); discarding output")
			_ = dst.Delete(ctx, outKey)
			return store.ProcessedFile{}, ErrLostClaim
		}
	} else if err := r.store.RecordProcessedFile(ctx, pf); err != nil {
		// Upload path: a same-name re-processing (ErrDuplicate) or a genuine
		// failure — discard the output and surface the error to the handler.
		_ = dst.Delete(ctx, outKey)
		return store.ProcessedFile{}, err
	}

	// Write the completion marker for DB↔storage reconciliation (BR-COL-009,
	// BR-NFR-017) — a durable record of completion independent of the database. It
	// lives on the OUTPUT store beside the done files (dst), so reconciliation of a
	// cross-backend pipeline reads markers from the destination it wrote them to.
	if donePrefix != "" {
		mk := reconcile.Marker{
			FileUID: fileUID, PipelineID: p.ID, Name: name, OutputName: outName, Size: size,
			RecordsIn: res.stats.RecordsIn, RecordsOut: res.stats.RecordsOut, Suspended: res.stats.Suspended,
			Members:     res.members,
			CompletedAt: time.Now().UTC(),
		}
		if err := reconcile.WriteMarker(ctx, dst, donePrefix, mk); err != nil {
			flog.Warn("write completion marker failed", "err", err.Error())
		}
	}

	// Dispose of the source AFTER the record, per the source's done disposition.
	// A failure here is safe: the file is already DONE, so a lingering source is
	// caught as a re-arrival next scan (AlreadyProcessed, BR-COL-006) and handled
	// then — never reprocessed.
	//   "" | "done"  → move to the done area (default)
	//   "delete"     → delete the source (marker already written)
	//   "leaveMarked"→ leave the source in place (marker prevents reprocessing)
	switch p.Source.Disposition {
	case "delete":
		if err := src.Delete(ctx, srcKey); err != nil {
			flog.Warn("delete source failed (already recorded)", "err", err.Error())
		}
	case "leaveMarked":
		// intentionally left in place
	default:
		if donePrefix != "" {
			// Relocate spans backends: a rename when dst == src, else copy-to-dst +
			// delete-from-src (cross-backend done area).
			if err := storage.Relocate(ctx, dst, joinKey(donePrefix, name), src, srcKey); err != nil {
				flog.Warn("move to done failed (already recorded; re-arrival check will re-move)", "err", err.Error())
			}
		}
	}

	if len(res.suspended) > 0 {
		flog.Warn("records suspended", "count", len(res.suspended), "first_reason", res.suspended[0].ReasonCode)
	}
	r.mFiles.Add(ctx, 1, pAttr)
	r.mIn.Add(ctx, int64(res.stats.RecordsIn), pAttr)
	r.mOut.Add(ctx, int64(res.stats.RecordsOut), pAttr)
	r.mSusp.Add(ctx, int64(res.stats.Suspended), pAttr)
	r.mDur.Record(ctx, time.Since(start).Seconds(), pAttr)
	flog.Info("file processed", "records_in", res.stats.RecordsIn, "records_out", res.stats.RecordsOut,
		"suspended", res.stats.Suspended, "output", outName, "status", "DONE")
	return pf, nil
}

func joinKey(prefix, name string) string {
	if prefix == "" {
		return name
	}
	return path.Join(prefix, name)
}

func outputName(inName string, uid int64, out spec.FormatSpec) string {
	base := inName
	if ext := path.Ext(base); ext != "" {
		base = base[:len(base)-len(ext)]
	}
	return fmt.Sprintf("%s-%d%s", base, uid, outputExt(out))
}

// outputExt is the file extension implied by an output format (plus the
// compression suffix). Shared by the per-file and the consolidated naming so the
// two can never disagree about what a .ndjson.gz is.
func outputExt(out spec.FormatSpec) string {
	ext := ".out"
	switch out.Kind {
	case spec.FormatJSON:
		if out.JSON != nil && out.JSON.Mode == "ndjson" {
			ext = ".ndjson"
		} else {
			ext = ".json"
		}
	case spec.FormatDSV:
		ext = ".csv"
		if out.DSV != nil && out.DSV.Delimiter == "\t" {
			ext = ".tsv"
		}
	case spec.FormatXML:
		ext = ".xml"
	}
	if out.Compress == "gzip" {
		ext += ".gz"
	}
	return ext
}

func contentType(out spec.FormatSpec) string {
	if out.Compress == "gzip" {
		return "application/gzip"
	}
	switch out.Kind {
	case spec.FormatJSON:
		if out.JSON != nil && out.JSON.Mode == "ndjson" {
			return "application/x-ndjson"
		}
		return "application/json"
	case spec.FormatDSV:
		return "text/csv"
	case spec.FormatXML:
		return "application/xml"
	default:
		return "application/octet-stream"
	}
}
