package encoder

import (
	"bufio"
	"encoding/json"
	"io"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

type jsonEncoder struct {
	array   bool
	columns []string
	w       *bufio.Writer
	n       int
}

func newJSON(s spec.JSONSpec) *jsonEncoder {
	return &jsonEncoder{array: s.Mode == "array"}
}

func (e *jsonEncoder) Begin(w io.Writer, columns []string) error {
	e.columns = columns
	e.w = bufio.NewWriter(w)
	e.n = 0
	if e.array {
		return e.w.WriteByte('[')
	}
	return nil
}

func (e *jsonEncoder) Write(rec *canonical.Record) error {
	// Build an ordered object using the output columns. json.Marshal of a map
	// would lose order, so emit key/values in column order via a small builder.
	obj := newOrdered(rec, e.columns)
	b, err := json.Marshal(obj)
	if err != nil {
		return err
	}
	if e.array {
		if e.n > 0 {
			if err := e.w.WriteByte(','); err != nil {
				return err
			}
		}
		if _, err := e.w.Write(b); err != nil {
			return err
		}
	} else {
		if _, err := e.w.Write(b); err != nil {
			return err
		}
		if err := e.w.WriteByte('\n'); err != nil {
			return err
		}
	}
	e.n++
	return nil
}

func (e *jsonEncoder) End() error {
	if e.array {
		if err := e.w.WriteByte(']'); err != nil {
			return err
		}
	}
	return e.w.Flush()
}

// orderedObject is a json.Marshaler that preserves column order.
type orderedObject []kv

type kv struct {
	Key string
	Val any
}

func newOrdered(rec *canonical.Record, columns []string) orderedObject {
	if len(columns) == 0 {
		columns = rec.Names()
	}
	out := make(orderedObject, 0, len(columns))
	for _, c := range columns {
		v, _ := rec.Get(c)
		out = append(out, kv{Key: c, Val: v.Interface()})
	}
	return out
}

func (o orderedObject) MarshalJSON() ([]byte, error) {
	buf := []byte{'{'}
	for i, e := range o {
		if i > 0 {
			buf = append(buf, ',')
		}
		k, err := json.Marshal(e.Key)
		if err != nil {
			return nil, err
		}
		buf = append(buf, k...)
		buf = append(buf, ':')
		v, err := json.Marshal(e.Val)
		if err != nil {
			return nil, err
		}
		buf = append(buf, v...)
	}
	buf = append(buf, '}')
	return buf, nil
}
