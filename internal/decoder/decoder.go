// Package decoder turns raw input bytes into canonical records per a configured
// Format Definition (BR-DEC-001..011). Decoding is streaming / record-at-a-time
// (BR-DEC-006) so memory is bounded by record size, not file size (BR-NFR-001).
//
// The alpha wires DSV and JSON. New decoders plug in behind the Decoder seam
// (BR-NFR-032) without touching callers.
package decoder

import (
	"context"
	"fmt"
	"io"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// EmitFunc receives each decoded record in order. Returning an error aborts decoding.
type EmitFunc func(*canonical.Record) error

// Decoder decodes an input stream into canonical records.
type Decoder interface {
	// Decode reads r to completion, calling emit for every decoded record.
	Decode(ctx context.Context, r io.Reader, emit EmitFunc) error
}

// New builds the decoder for a format spec.
func New(fs spec.FormatSpec) (Decoder, error) {
	switch fs.Kind {
	case spec.FormatDSV:
		if fs.DSV == nil {
			return nil, fmt.Errorf("decoder: dsv format selected but no dsv spec")
		}
		return newDSV(*fs.DSV)
	case spec.FormatJSON:
		if fs.JSON == nil {
			return nil, fmt.Errorf("decoder: json format selected but no json spec")
		}
		return newJSON(*fs.JSON), nil
	default:
		return nil, fmt.Errorf("decoder: unsupported format kind %q", fs.Kind)
	}
}
