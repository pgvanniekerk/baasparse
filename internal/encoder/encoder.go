// Package encoder writes canonical records out in a configured target format
// (BR-DST-002), independent of the input format (BR-TRN-006). The alpha wires
// DSV and JSON; new encoders plug in behind the Encoder seam (BR-NFR-032).
package encoder

import (
	"fmt"
	"io"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// Encoder writes an output file: Begin (with the ordered output columns), then a
// Write per record, then End.
type Encoder interface {
	Begin(w io.Writer, columns []string) error
	Write(rec *canonical.Record) error
	End() error
}

// New builds the encoder for a format spec.
func New(fs spec.FormatSpec) (Encoder, error) {
	switch fs.Kind {
	case spec.FormatDSV:
		s := spec.DSVSpec{}
		if fs.DSV != nil {
			s = *fs.DSV
		}
		return newDSV(s)
	case spec.FormatJSON:
		s := spec.JSONSpec{}
		if fs.JSON != nil {
			s = *fs.JSON
		}
		return newJSON(s), nil
	case spec.FormatXML:
		s := spec.XMLSpec{}
		if fs.XML != nil {
			s = *fs.XML
		}
		return newXMLEnc(s), nil
	default:
		return nil, fmt.Errorf("encoder: unsupported format kind %q", fs.Kind)
	}
}
