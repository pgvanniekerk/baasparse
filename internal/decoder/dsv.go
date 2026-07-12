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
	types   map[string]spec.ValueType
}

func newDSV(s spec.DSVSpec, fields []spec.FieldSpec) (*dsvDecoder, error) {
	comma := ','
	if s.Delimiter != "" {
		r := []rune(s.Delimiter)
		if len(r) != 1 {
			return nil, fmt.Errorf("decoder: dsv delimiter must be a single character, got %q", s.Delimiter)
		}
		comma = r[0]
	}
	d := &dsvDecoder{comma: comma, header: s.HasHeader, columns: s.Columns}
	// Declared input fields are authoritative: their names are the columns (in
	// order) and their types coerce each cell — regardless of a header row, which
	// is then skipped rather than used for names.
	if len(fields) > 0 {
		d.columns = make([]string, len(fields))
		for i, f := range fields {
			d.columns[i] = f.Name
		}
		d.types = typeMap(fields)
	}
	return d, nil
}

func (d *dsvDecoder) Decode(ctx context.Context, r io.Reader, emit EmitFunc) error {
	cr := csv.NewReader(r)
	cr.Comma = d.comma
	cr.FieldsPerRecord = -1 // tolerate ragged rows; we map by position
	cr.LazyQuotes = true
	cr.ReuseRecord = true

	names := d.columns // declared/explicit column names (nil ⇒ take from header/colN)
	headerPending := d.header
	seq := 0
	// One record reused across rows: the pipeline consumes (transforms + encodes)
	// each record synchronously before the next Read, so resetting and refilling it
	// is safe and avoids a per-row Record + Fields-slice allocation. Fields are
	// appended directly (columns are unique by construction), skipping Set's dedup
	// scan (which was O(fields²) per record).
	rec := &canonical.Record{}
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
		if headerPending {
			headerPending = false
			if len(names) == 0 { // no declared names: fall back to the header row
				names = append(names[:0:0], row...) // copy: ReuseRecord reuses the slice
			}
			continue // the header row is never emitted as data
		}
		rec.Seq = seq + 1
		rec.Fields = rec.Fields[:0]
		for i, cell := range row {
			name := columnName(names, i)
			v := canonical.StringVal(cell)
			if d.types != nil {
				v = coerceValue(v, d.types[name])
			}
			rec.Fields = append(rec.Fields, canonical.Field{Name: name, Val: v})
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
