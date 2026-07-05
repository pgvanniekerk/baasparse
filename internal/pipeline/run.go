// Package pipeline runs the format-agnostic core over a single input stream:
// decode -> transform -> encode (BRS §5.1, streaming/pass-through mode,
// BR-CFG-010). It is deliberately DB-free so it can be unit-tested and driven
// both from the GUI preview and the file worker.
package pipeline

import (
	"context"
	"fmt"
	"io"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/decoder"
	"github.com/pgvanniekerk/baasparse/internal/encoder"
	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/transform"
)

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
type Suspended struct {
	Seq        int
	ReasonCode string
	Detail     string
}

// Run streams records from in, transforms them, and writes them to out. It
// returns per-record suspensions rather than aborting the whole file on one bad
// record (BR-ERR-002, BR-DEC-007).
func Run(ctx context.Context, sp Spec, in io.Reader, out io.Writer) (Stats, []Suspended, error) {
	dec, err := decoder.New(sp.Input)
	if err != nil {
		return Stats{}, nil, err
	}
	enc, err := encoder.New(sp.Output)
	if err != nil {
		return Stats{}, nil, err
	}
	tr := transform.New(sp.Transform)

	// Determine output columns up front. For an explicit transform this is the
	// declared field list; for a passthrough we must peek the first record, so we
	// buffer records through a two-phase emit.
	var (
		stats     Stats
		suspended []Suspended
		started   bool
		columns   []string
	)

	begin := func(sample *canonical.Record) error {
		columns = tr.Columns(sample)
		if err := enc.Begin(out, columns); err != nil {
			return fmt.Errorf("encoder begin: %w", err)
		}
		started = true
		return nil
	}

	emit := func(rec *canonical.Record) error {
		stats.RecordsIn++
		outRec, err := tr.Apply(rec)
		if err != nil {
			stats.Suspended++
			suspended = append(suspended, Suspended{Seq: rec.Seq, ReasonCode: reasonOf(err), Detail: err.Error()})
			return nil
		}
		if !started {
			if err := begin(outRec); err != nil {
				return err
			}
		}
		if err := enc.Write(outRec); err != nil {
			return fmt.Errorf("encoder write (record %d): %w", rec.Seq, err)
		}
		stats.RecordsOut++
		return nil
	}

	if err := dec.Decode(ctx, in, emit); err != nil {
		return stats, suspended, err
	}
	if !started {
		// Zero-record (or all-suspended) file: still produce a valid empty output
		// (BR-COL-016).
		if err := enc.Begin(out, tr.Columns(nil)); err != nil {
			return stats, suspended, fmt.Errorf("encoder begin (empty): %w", err)
		}
	}
	if err := enc.End(); err != nil {
		return stats, suspended, fmt.Errorf("encoder end: %w", err)
	}
	return stats, suspended, nil
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
