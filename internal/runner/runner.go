// Package runner executes a configured pipeline — either as an in-memory preview
// (GUI transform modeller, BR-UI-009) or against a real file with atomic output
// and a persisted Processed File record (data plane). All file access goes
// through the storage.Store abstraction (TS 16 §16.2), so the same code runs over
// POSIX/SFTP/S3 backends. It is shared by the GUI and the background watcher.
package runner

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strconv"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

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
	err       error
}

// ErrLostClaim is returned when the file claim was taken over by another
// instance during processing, so this instance recorded nothing.
var ErrLostClaim = errors.New("lost file claim during processing")

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
func (r *Runner) ProcessFile(ctx context.Context, p store.Pipeline, st storage.Store, srcKey, outputPrefix, donePrefix string, claim *store.Claim) (store.ProcessedFile, error) {
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
		attribute.String("baasparse.backend", st.Backend()),
	)
	flog := r.log.With("function", "ProcessFile", "correlationID", cid, "pipeline", p.Name, "file", name, "backend", st.Backend())
	if sc := span.SpanContext(); sc.HasTraceID() {
		flog = flog.With("trace_id", sc.TraceID().String())
	}
	flog.Info("collecting file", "file_uid", fileUID, "src", srcKey)
	pAttr := metric.WithAttributes(attribute.String("pipeline", p.Name))

	var size int64
	if e, serr := st.Stat(ctx, srcKey); serr == nil {
		size = e.Size
	}

	rc, err := st.Open(ctx, srcKey)
	if err != nil {
		return store.ProcessedFile{}, fmt.Errorf("open source %s: %w", srcKey, err)
	}
	defer rc.Close()

	outName := outputName(name, fileUID, p.Output)
	outKey := joinKey(outputPrefix, outName)

	// Stream: the pipeline writes to a pipe; storage.Put reads it. A pipeline
	// error closes the pipe with that error, so Put aborts and never commits a
	// partial output (BR-STO-004, BR-DST-003).
	pr, pw := io.Pipe()
	resCh := make(chan runResult, 1)
	go func() {
		stats, susp, rerr := pipeline.Run(ctx, p.Runner(), rc, pw)
		_ = pw.CloseWithError(rerr)
		resCh <- runResult{stats, susp, rerr}
	}()

	putErr := st.Put(ctx, outKey, pr, storage.Meta{ContentType: contentType(p.Output)})
	// Close the read end so that if Put failed early (e.g. an S3 network error or a
	// full output volume) the pipeline goroutine's blocked pw.Write is released and
	// cannot leak. Harmless on the success path (Put already read to EOF).
	_ = pr.CloseWithError(putErr)
	res := <-resCh
	if res.err != nil {
		return store.ProcessedFile{}, fmt.Errorf("pipeline run: %w", res.err)
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
			_ = st.Delete(ctx, outKey)
			return store.ProcessedFile{}, fmt.Errorf("record (claimed): %w", cerr)
		}
		if !recorded {
			flog.Warn("did not record (claim lost / already processed); discarding output")
			_ = st.Delete(ctx, outKey)
			return store.ProcessedFile{}, ErrLostClaim
		}
	} else if err := r.store.RecordProcessedFile(ctx, pf); err != nil {
		// Upload path: a same-name re-processing (ErrDuplicate) or a genuine
		// failure — discard the output and surface the error to the handler.
		_ = st.Delete(ctx, outKey)
		return store.ProcessedFile{}, err
	}

	// Write the completion marker for DB↔storage reconciliation (BR-COL-009,
	// BR-NFR-017) — a durable record of completion independent of the database.
	if donePrefix != "" {
		mk := reconcile.Marker{
			FileUID: fileUID, PipelineID: p.ID, Name: name, OutputName: outName, Size: size,
			RecordsIn: res.stats.RecordsIn, RecordsOut: res.stats.RecordsOut, Suspended: res.stats.Suspended,
			CompletedAt: time.Now().UTC(),
		}
		if err := reconcile.WriteMarker(ctx, st, donePrefix, mk); err != nil {
			flog.Warn("write completion marker failed", "err", err.Error())
		}
	}

	// Move the source to done AFTER the record. A failure here is safe: the file
	// is already DONE, so a lingering source is caught as a re-arrival next scan
	// (AlreadyProcessed, BR-COL-006) and moved then — never reprocessed.
	if donePrefix != "" {
		if err := st.Move(ctx, srcKey, joinKey(donePrefix, name)); err != nil {
			flog.Warn("move to done failed (already recorded; re-arrival check will re-move)", "err", err.Error())
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
	}
	return fmt.Sprintf("%s-%d%s", base, uid, ext)
}

func contentType(out spec.FormatSpec) string {
	switch out.Kind {
	case spec.FormatJSON:
		if out.JSON != nil && out.JSON.Mode == "ndjson" {
			return "application/x-ndjson"
		}
		return "application/json"
	case spec.FormatDSV:
		return "text/csv"
	default:
		return "application/octet-stream"
	}
}
