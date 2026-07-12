package decoder

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

type jsonDecoder struct {
	array bool // array/object document vs newline-delimited
	types map[string]spec.ValueType
}

func newJSON(s spec.JSONSpec, fields []spec.FieldSpec) *jsonDecoder {
	return &jsonDecoder{array: s.Mode == "array", types: typeMap(fields)}
}

func (d *jsonDecoder) Decode(ctx context.Context, r io.Reader, emit EmitFunc) error {
	if d.array {
		return d.decodeArray(ctx, r, emit)
	}
	return d.decodeNDJSON(ctx, r, emit)
}

// decodeNDJSON reads one JSON object per line (BR-DEC-002) through the fast
// flat-object parser: the only steady-state allocation per record is the line
// string, which keys and values are sliced from. One record is reused across
// lines — the pipeline consumes each record synchronously before the next
// (same contract as the DSV decoder).
func (d *jsonDecoder) decodeNDJSON(ctx context.Context, r io.Reader, emit EmitFunc) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	rec := &canonical.Record{}
	seq := 0
	for sc.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		line := string(b)
		if err := parseJSONObject(line, seq+1, d.types, rec); err != nil {
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
// record-at-a-time: each element is captured as a RawMessage (whose buffer the
// stdlib reuses across iterations) and parsed with the fast flat-object parser.
func (d *jsonDecoder) decodeArray(ctx context.Context, r io.Reader, emit EmitFunc) error {
	dec := json.NewDecoder(r)
	tok, err := dec.Token()
	if errors.Is(err, io.EOF) {
		// An empty stream carries zero records, exactly as ndjson mode treats it. An
		// empty member must not condemn the archive that contains it (BR-COL-016).
		return nil
	}
	if err != nil {
		return fmt.Errorf("json: read opening token: %w", err)
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return fmt.Errorf("json: array mode expects a top-level '[', got %v", tok)
	}
	var raw json.RawMessage
	rec := &canonical.Record{}
	seq := 0
	for dec.More() {
		if err := ctx.Err(); err != nil {
			return err
		}
		raw = raw[:0]
		if err := dec.Decode(&raw); err != nil {
			return fmt.Errorf("json: element %d: %w", seq+1, err)
		}
		// Copy to a string: parseJSONObject slices values out of its input, and
		// raw's backing array is reused by the next Decode.
		if err := parseJSONObject(string(raw), seq+1, d.types, rec); err != nil {
			return fmt.Errorf("json: element %d: %w", seq+1, err)
		}
		if err := emit(rec); err != nil {
			return err
		}
		seq++
	}
	// Consume the closing ']' and require EOF — trailing garbage (or a second
	// document) is an error, consistent with the NDJSON path's strictness, not
	// silently dropped.
	if tok, err := dec.Token(); err != nil {
		return fmt.Errorf("json: closing ']': %w", err)
	} else if delim, ok := tok.(json.Delim); !ok || delim != ']' {
		return fmt.Errorf("json: expected closing ']', got %v", tok)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return fmt.Errorf("json: trailing data after array")
	}
	return nil
}
