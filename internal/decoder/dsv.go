package decoder

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

type dsvDecoder struct {
	comma   rune
	header  bool
	columns []string
}

func newDSV(s spec.DSVSpec) (*dsvDecoder, error) {
	comma := ','
	if s.Delimiter != "" {
		r := []rune(s.Delimiter)
		if len(r) != 1 {
			return nil, fmt.Errorf("decoder: dsv delimiter must be a single character, got %q", s.Delimiter)
		}
		comma = r[0]
	}
	return &dsvDecoder{comma: comma, header: s.HasHeader, columns: s.Columns}, nil
}

func (d *dsvDecoder) Decode(ctx context.Context, r io.Reader, emit EmitFunc) error {
	cr := csv.NewReader(r)
	cr.Comma = d.comma
	cr.FieldsPerRecord = -1 // tolerate ragged rows; we map by position
	cr.LazyQuotes = true
	cr.ReuseRecord = true

	var names []string
	if !d.header {
		names = d.columns
	}
	seq := 0
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		row, err := cr.Read()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("dsv: read row %d: %w", seq+1, err)
		}
		if d.header && names == nil {
			names = append(names[:0:0], row...) // copy: ReuseRecord reuses the slice
			continue
		}
		rec := &canonical.Record{Seq: seq + 1, Fields: make([]canonical.Field, 0, len(row))}
		for i, cell := range row {
			name := columnName(names, i)
			rec.Set(name, canonical.StringVal(cell))
		}
		if err := emit(rec); err != nil {
			return err
		}
		seq++
	}
}

func columnName(names []string, i int) string {
	if i < len(names) && names[i] != "" {
		return names[i]
	}
	return fmt.Sprintf("col%d", i+1)
}
