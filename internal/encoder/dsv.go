package encoder

import (
	"encoding/csv"
	"fmt"
	"io"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

type dsvEncoder struct {
	comma   rune
	header  bool
	columns []string
	cw      *csv.Writer
}

func newDSV(s spec.DSVSpec) (*dsvEncoder, error) {
	comma := ','
	if s.Delimiter != "" {
		r := []rune(s.Delimiter)
		if len(r) != 1 {
			return nil, fmt.Errorf("encoder: dsv delimiter must be a single character, got %q", s.Delimiter)
		}
		comma = r[0]
	}
	// Default to writing a header row unless explicitly disabled by having no
	// header and no columns; for output a header is the friendly default.
	return &dsvEncoder{comma: comma, header: true, columns: s.Columns}, nil
}

func (e *dsvEncoder) Begin(w io.Writer, columns []string) error {
	if len(e.columns) == 0 {
		e.columns = columns
	}
	e.cw = csv.NewWriter(w)
	e.cw.Comma = e.comma
	if e.header {
		if err := e.cw.Write(e.columns); err != nil {
			return err
		}
	}
	return nil
}

func (e *dsvEncoder) Write(rec *canonical.Record) error {
	row := make([]string, len(e.columns))
	for i, col := range e.columns {
		if v, ok := rec.Get(col); ok {
			row[i] = v.String()
		}
	}
	return e.cw.Write(row)
}

func (e *dsvEncoder) End() error {
	e.cw.Flush()
	return e.cw.Error()
}
