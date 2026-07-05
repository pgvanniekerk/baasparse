package decoder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

type jsonDecoder struct {
	array bool // array/object document vs newline-delimited
}

func newJSON(s spec.JSONSpec) *jsonDecoder {
	return &jsonDecoder{array: s.Mode == "array"}
}

func (d *jsonDecoder) Decode(ctx context.Context, r io.Reader, emit EmitFunc) error {
	if d.array {
		return d.decodeArray(ctx, r, emit)
	}
	return d.decodeNDJSON(ctx, r, emit)
}

// decodeNDJSON reads one JSON object per line (BR-DEC-002).
func (d *jsonDecoder) decodeNDJSON(ctx context.Context, r io.Reader, emit EmitFunc) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	seq := 0
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		rec, err := objectToRecord(line, seq+1)
		if err != nil {
			return fmt.Errorf("json: line %d: %w", seq+1, err)
		}
		if err := emit(rec); err != nil {
			return err
		}
		seq++
	}
	return sc.Err()
}

// decodeArray streams the elements of a top-level JSON array (BR-DEC-002),
// record-at-a-time so the whole document is not buffered as objects.
func (d *jsonDecoder) decodeArray(ctx context.Context, r io.Reader, emit EmitFunc) error {
	dec := json.NewDecoder(r)
	dec.UseNumber()
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("json: read opening token: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return fmt.Errorf("json: array mode expects a top-level '[', got %v", tok)
	}
	seq := 0
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			return fmt.Errorf("json: element %d: %w", seq+1, err)
		}
		if err := emit(mapToRecord(obj, seq+1)); err != nil {
			return err
		}
		seq++
	}
	return nil
}

func objectToRecord(b []byte, seq int) (*canonical.Record, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	return mapToRecord(obj, seq), nil
}

func mapToRecord(obj map[string]any, seq int) *canonical.Record {
	// Preserve key order deterministically via json ordering isn't available from
	// a map; sort for stable output. The transform stage defines the real order.
	rec := &canonical.Record{Seq: seq, Fields: make([]canonical.Field, 0, len(obj))}
	for _, k := range sortedKeys(obj) {
		rec.Set(k, canonical.FromJSON(obj[k]))
	}
	return rec
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// simple insertion sort keeps deps minimal
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}
