// Package pipeline runs the format-agnostic core over input streams:
// decode -> transform -> encode (BRS §5.1, streaming/pass-through mode,
// BR-CFG-010). It is deliberately DB-free so it can be unit-tested and driven
// both from the GUI preview and the file worker.
//
// A Session decodes MANY input streams and encodes MANY outputs, which is the
// shape both of the engine's compound features:
//
//   - many streams -> one output is CONSOLIDATION (ten 100-record files become one
//     1000-record file, TS 07 §7.3.2) and also archive containers, where each
//     tar.gz member is a stream (TS 04 §4.4.8). Records are decoded per stream with
//     a fresh decoder, but the encoder is shared, so the header is written once and
//     records simply accumulate.
//   - one stream -> many outputs is MULTI-DESTINATION delivery: the record is
//     decoded and transformed ONCE and then encoded per destination, so billing can
//     take pipe-delimited DSV with a narrow column set while revenue assurance takes
//     gzipped NDJSON with everything.
//
// Targets fail INDEPENDENTLY. If one destination's writer breaks mid-stream (its
// storage went away), that target is marked failed and the others keep encoding —
// a revenue-assurance outage must never stall the billing feed. The caller sees
// which targets survived and delivers only those.
package pipeline

import (
	"bufio"
	"context"
	"fmt"
	"io"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/decoder"
	"github.com/pgvanniekerk/baasparse/internal/encoder"
	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/transform"
)

// ReadBufferBytes is the input read-buffer size (engine-wide, settable at
// startup). Input streams are wrapped in a buffered reader of this size before
// decoding, so bytes are pulled from the backend in larger chunks — cutting the
// number of read syscalls on POSIX and, more importantly, the number of network
// round-trips when streaming from S3. 0 disables the wrap.
var ReadBufferBytes = 64 * 1024

// Spec fully describes one streaming pipeline: input format, transform, output format.
type Spec struct {
	Input     spec.FormatSpec
	Transform spec.TransformSpec
	Output    spec.FormatSpec
}

// Stats is the reconciliation-style outcome of a run (BR-REC-001: for a
// streaming pipeline, in = out + suspended).
type Stats struct {
	RecordsIn  int
	RecordsOut int
	Suspended  int
}

// Suspended captures a record that failed transform/encode (BR-ERR-001 shape,
// held in memory for the alpha preview; the file worker persists to SU_SUSPENSE).
// Member names the input stream the record came from — an archive member, or, when
// consolidating, the input FILE — so a bad record stays attributable after many
// files merge into one output.
type Suspended struct {
	Seq    int
	Member string
	// Dest names the destination the record failed to shape for. A record can be
	// perfectly valid for one destination and unshapeable for another, because each
	// applies its own transform.
	Dest       string
	ReasonCode string
	Detail     string
}

// EncodeError marks a failure on the OUTPUT/write side of the pipeline (the
// encoder's Begin/Write/End). Because the file worker streams output through an
// io.Pipe, a DESTINATION write failure (e.g. a storage outage on a cross-backend
// output) surfaces here — the encoder's write into the pipe fails. The worker
// therefore treats an EncodeError as a retryable infrastructure error rather than
// quarantining a valid input file, distinguishing it from a decode/content
// failure (a malformed input), which is the only thing that should be quarantined.
type EncodeError struct{ Err error }

func (e *EncodeError) Error() string { return e.Err.Error() }
func (e *EncodeError) Unwrap() error { return e.Err }

// Target is one output destination of a session: its own OUTPUT STRUCTURE (which
// fields it carries and how each is derived), its own format, and its own writer.
//
// Destinations differ in shape, not just in encoding — billing takes a narrow set
// of billable columns while revenue assurance takes everything — so each target
// applies its own transform to the SAME decoded record. Decode once, shape N ways.
type Target struct {
	Name      string
	Transform spec.TransformSpec
	Format    spec.FormatSpec
	W         io.Writer
}

// TargetResult reports one destination's outcome. Err is non-nil when THIS target
// failed while others may have succeeded — the caller delivers the healthy ones
// and retries the failed one.
type TargetResult struct {
	Name       string
	RecordsOut int
	Suspended  int
	Err        error
}

// Contribution is one input stream's share of the output: how many records it
// read, wrote and suspended, and which output record indexes it occupies. It is
// what keeps per-FILE reconciliation (in = out + suspended, BR-REC-001) and
// per-FILE lineage intact after consolidation merges ten files into one object.
// All targets receive the same records in the same order, so one contribution
// describes every target equally.
type Contribution struct {
	Member     string
	RecordsIn  int64
	RecordsOut int64
	Suspended  int64
	FirstIndex int64
	LastIndex  int64
}

type target struct {
	name string
	enc  encoder.Encoder
	tr   *transform.Engine // this destination's own output structure
	w    io.Writer
	// cols projects the shaped record down to this destination's columns. Empty
	// means "whatever this destination's transform produced".
	cols    []string
	started bool
	out     int
	susp    int
	err     error // once set, this target is abandoned; the others carry on
}

// Session decodes one or more input streams into one or more outputs. Not safe
// for concurrent use. Consume decodes one stream (a whole file, or one archive
// member); Close finishes every output. Each encoder's Begin is deferred until the
// first record so a passthrough transform can take its columns from the data; a
// session that never sees a record still produces a valid empty output
// (BR-COL-016).
type Session struct {
	sp        Spec
	outs      []*target
	br        *bufio.Reader // reused across Consume calls (one buffer per session)
	stats     Stats
	suspended []Suspended
	member    string

	// Per-input-file lineage, so "which output did this file's records land in,
	// and where" stays answerable after consolidation merges ten files into one.
	contribs []Contribution
	index    int64 // running output record index across all streams
}

// NewSession builds a session writing to one output (the classic single-output
// case: preview, upload, and any pipeline with a single destination).
func NewSession(sp Spec, out io.Writer) (*Session, error) {
	return NewMultiSession(sp, []Target{{Name: "default", Transform: sp.Transform, Format: sp.Output, W: out}})
}

// NewMultiSession builds a session that fans every record out to each target,
// encoded in that target's own format.
func NewMultiSession(sp Spec, targets []Target) (*Session, error) {
	if len(targets) == 0 {
		return nil, fmt.Errorf("pipeline: session needs at least one target")
	}
	s := &Session{sp: sp}
	for _, t := range targets {
		enc, err := encoder.New(t.Format)
		if err != nil {
			return nil, fmt.Errorf("destination %q: %w", t.Name, err)
		}
		s.outs = append(s.outs, &target{
			name: t.Name, enc: enc, tr: transform.New(t.Transform), w: t.W, cols: t.Format.Columns,
		})
	}
	return s, nil
}

// Consume decodes one input stream into every live output. member labels the
// stream (an archive member, or an input file name when consolidating). A fresh
// decoder is built per stream (decoders are stateful: header rows, sequence
// numbers), but the encoders persist, so records append rather than restart.
func (s *Session) Consume(ctx context.Context, in io.Reader, member string) error {
	dec, err := decoder.New(s.sp.Input)
	if err != nil {
		return err
	}
	// Pull from the source in large chunks. The single session buffer is Reset
	// per stream rather than reallocated (an archive can have thousands of
	// members). Skip the wrap if the reader is already at least this buffered.
	if ReadBufferBytes > 0 {
		if br, ok := in.(*bufio.Reader); !ok || br.Size() < ReadBufferBytes {
			if s.br == nil {
				s.br = bufio.NewReaderSize(in, ReadBufferBytes)
			} else {
				s.br.Reset(in)
			}
			in = s.br
		}
	}
	s.member = member
	start := s.index
	beforeIn, beforeOut, beforeSusp := s.stats.RecordsIn, s.stats.RecordsOut, s.stats.Suspended
	err = dec.Decode(ctx, in, s.emit)
	// Record this stream's contribution even on failure: the caller needs to know
	// which file broke, and a partial contribution is still the truth about what
	// was written.
	in2, out2 := s.stats.RecordsIn-beforeIn, s.stats.RecordsOut-beforeOut
	if in2 > 0 || out2 > 0 {
		c := Contribution{
			Member: member, RecordsIn: int64(in2), RecordsOut: int64(out2),
			Suspended: int64(s.stats.Suspended - beforeSusp), FirstIndex: start, LastIndex: s.index - 1,
		}
		if out2 == 0 { // contributed no output records: no index range to claim
			c.FirstIndex, c.LastIndex = 0, -1
		}
		s.contribs = append(s.contribs, c)
	}
	return err
}

// Close finishes every live output and reports each one's outcome. A target that
// failed mid-stream is reported with its error rather than aborting the rest.
func (s *Session) Close() (Stats, []Suspended, []TargetResult) {
	res := make([]TargetResult, 0, len(s.outs))
	for _, t := range s.outs {
		if t.err == nil {
			if !t.started {
				// Zero-record (or all-suspended) input: still produce a valid empty
				// output (BR-COL-016).
				if err := t.enc.Begin(t.w, s.columnsFor(t, nil)); err != nil {
					t.err = &EncodeError{Err: fmt.Errorf("destination %q: encoder begin (empty): %w", t.name, err)}
				}
			}
		}
		if t.err == nil {
			if err := t.enc.End(); err != nil {
				t.err = &EncodeError{Err: fmt.Errorf("destination %q: encoder end: %w", t.name, err)}
			}
		}
		res = append(res, TargetResult{Name: t.name, RecordsOut: t.out, Suspended: t.susp, Err: t.err})
	}
	// RecordsOut on the shared stats reflects the records the pipeline produced
	// (identical for every healthy target); per-target counts are in the results.
	return s.stats, s.suspended, res
}

// Contributions returns per-input-stream record lineage for the whole session.
func (s *Session) Contributions() []Contribution { return s.contribs }

// Live reports whether any target is still healthy. When none are, there is
// nothing left to write and the caller should stop decoding.
func (s *Session) Live() bool {
	for _, t := range s.outs {
		if t.err == nil {
			return true
		}
	}
	return false
}

func (s *Session) emit(rec *canonical.Record) error {
	s.stats.RecordsIn++

	wrote := false
	shaped := false // did ANY destination successfully shape this record?
	for _, t := range s.outs {
		if t.err != nil {
			continue // this destination already fell over; the others carry on
		}
		// Each destination shapes the record its own way. A transform failure is
		// specific to THAT destination — the record may be perfectly valid for the
		// others — so it suspends there and the rest still receive it.
		outRec, err := t.tr.Apply(rec)
		if err != nil {
			t.susp++
			s.suspended = append(s.suspended, Suspended{
				Seq: rec.Seq, Member: s.member, Dest: t.name,
				ReasonCode: reasonOf(err), Detail: err.Error(),
			})
			continue
		}
		shaped = true
		if !t.started {
			if err := t.enc.Begin(t.w, s.columnsFor(t, outRec)); err != nil {
				t.err = &EncodeError{Err: fmt.Errorf("destination %q: encoder begin: %w", t.name, err)}
				continue
			}
			t.started = true
		}
		if err := t.enc.Write(outRec); err != nil {
			t.err = &EncodeError{Err: fmt.Errorf("destination %q: encoder write (record %d): %w", t.name, rec.Seq, err)}
			continue
		}
		t.out++
		wrote = true
	}
	if !wrote {
		if !shaped {
			// No destination could shape it: the record itself is the problem, so it
			// is suspended for the pipeline rather than failing the run.
			s.stats.Suspended++
			return nil
		}
		// Every destination's WRITER is down: decoding on would burn CPU for nothing.
		return s.firstErr()
	}
	s.stats.RecordsOut++
	s.index++
	return nil
}

// columnsFor picks the columns this destination emits: its own projection when it
// declares one, otherwise everything its own transform produced.
func (s *Session) columnsFor(t *target, outRec *canonical.Record) []string {
	if len(t.cols) > 0 {
		return t.cols
	}
	return t.tr.Columns(outRec)
}

func (s *Session) firstErr() error {
	for _, t := range s.outs {
		if t.err != nil {
			return t.err
		}
	}
	return nil
}

// Run streams records from one input, transforms them, and writes them to out —
// the single-stream, single-output convenience over Session. It returns per-record
// suspensions rather than aborting the whole file on one bad record (BR-ERR-002,
// BR-DEC-007).
func Run(ctx context.Context, sp Spec, in io.Reader, out io.Writer) (Stats, []Suspended, error) {
	s, err := NewSession(sp, out)
	if err != nil {
		return Stats{}, nil, err
	}
	if err := s.Consume(ctx, in, ""); err != nil {
		return s.stats, s.suspended, err
	}
	stats, susp, res := s.Close()
	for _, r := range res {
		if r.Err != nil {
			return stats, susp, r.Err
		}
	}
	return stats, susp, nil
}

// reasonOf extracts the leading UPPER_SNAKE reason code from an error message
// (TS 02 §2.3.1), defaulting to a generic transform code.
func reasonOf(err error) string {
	msg := err.Error()
	for i := 0; i < len(msg); i++ {
		c := msg[i]
		if c == ':' {
			return msg[:i]
		}
		if !(c >= 'A' && c <= 'Z' || c == '_') {
			break
		}
	}
	return "TRN_EXPR_ERROR"
}
