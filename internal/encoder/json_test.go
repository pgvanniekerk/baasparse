package encoder

import (
	"bytes"
	"encoding/json"
	"math"
	"testing"
	"unicode/utf8"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// TestJSONEncoderInvalidUTF8 ensures invalid UTF-8 input (e.g. a Latin-1/CP-1252
// CSV cell, or a lone surrogate) is sanitized to U+FFFD so the output is always
// valid UTF-8 JSON — matching encoding/json, not the raw-byte pass-through.
func TestJSONEncoderInvalidUTF8(t *testing.T) {
	rec := &canonical.Record{Seq: 1, Fields: []canonical.Field{
		{Name: "latin1", Val: canonical.StringVal("caf\xe9")},           // lone 0xE9
		{Name: "surrogate", Val: canonical.StringVal("x\xed\xa0\x80y")}, // surrogate bytes
		{Name: "ok", Val: canonical.StringVal("café ✓")},                // valid UTF-8 stays
	}}
	cols := []string{"latin1", "surrogate", "ok"}
	enc := newJSON(spec.JSONSpec{Mode: "ndjson"})
	var buf bytes.Buffer
	_ = enc.Begin(&buf, cols)
	_ = enc.Write(rec)
	_ = enc.End()

	out := bytes.TrimSpace(buf.Bytes())
	if !utf8.Valid(out) {
		t.Fatalf("encoder emitted invalid UTF-8: % x", out)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("not valid JSON: %v\n%s", err, out)
	}
	if got["latin1"] != "caf�" {
		t.Errorf("latin1 = %q, want caf<U+FFFD>", got["latin1"])
	}
	if got["ok"] != "café ✓" {
		t.Errorf("ok = %q, want 'café ✓' (valid UTF-8 preserved)", got["ok"])
	}
}

// TestJSONEncoderCorrectness verifies the hand-rolled JSON encoder emits valid
// JSON whose values round-trip, including the escaping edge cases, and preserves
// output-column order (incl. null for a column absent from the record).
func TestJSONEncoderCorrectness(t *testing.T) {
	cols := []string{"s", "quote", "esc", "uni", "ctrl", "html", "i", "negf", "bigf", "b", "nul", "missing"}
	rec := &canonical.Record{Seq: 1}
	add := func(n string, v canonical.Value) { rec.Fields = append(rec.Fields, canonical.Field{Name: n, Val: v}) }
	add("s", canonical.StringVal("hello world"))
	add("quote", canonical.StringVal(`a"b\c`))
	add("esc", canonical.StringVal("line1\nline2\ttab\rcr"))
	add("uni", canonical.StringVal("café 中文 🚀"))
	add("ctrl", canonical.StringVal("x\x01y\x1f"))
	add("html", canonical.StringVal("<a>&</a>"))
	add("i", canonical.IntVal(-42))
	add("negf", canonical.FloatVal(-3.14159))
	add("bigf", canonical.FloatVal(1234567.89))
	add("b", canonical.BoolVal(true))
	add("nul", canonical.NullVal())
	// "missing" is intentionally not set on the record

	for _, mode := range []string{"ndjson", "array"} {
		enc := newJSON(spec.JSONSpec{Mode: mode})
		var buf bytes.Buffer
		if err := enc.Begin(&buf, cols); err != nil {
			t.Fatalf("%s begin: %v", mode, err)
		}
		if err := enc.Write(rec); err != nil {
			t.Fatalf("%s write: %v", mode, err)
		}
		if err := enc.End(); err != nil {
			t.Fatalf("%s end: %v", mode, err)
		}

		out := bytes.TrimSpace(buf.Bytes())
		var obj map[string]any
		if mode == "array" {
			var arr []map[string]any
			if err := json.Unmarshal(out, &arr); err != nil {
				t.Fatalf("%s: not valid JSON array: %v\n%s", mode, err, out)
			}
			if len(arr) != 1 {
				t.Fatalf("%s: want 1 element, got %d", mode, len(arr))
			}
			obj = arr[0]
		} else if err := json.Unmarshal(out, &obj); err != nil {
			t.Fatalf("%s: not valid JSON: %v\n%s", mode, err, out)
		}

		want := map[string]any{
			"s": "hello world", "quote": `a"b\c`, "esc": "line1\nline2\ttab\rcr",
			"uni": "café 中文 🚀", "ctrl": "x\x01y\x1f", "html": "<a>&</a>",
			"i": float64(-42), "b": true, "nul": nil, "missing": nil,
		}
		for k, w := range want {
			if got := obj[k]; got != w {
				t.Errorf("%s: field %q = %#v, want %#v", mode, k, got, w)
			}
		}
		if f, _ := obj["negf"].(float64); math.Abs(f-(-3.14159)) > 1e-9 {
			t.Errorf("%s: negf = %v, want -3.14159", mode, obj["negf"])
		}
		if f, _ := obj["bigf"].(float64); math.Abs(f-1234567.89) > 1e-3 {
			t.Errorf("%s: bigf = %v, want 1234567.89", mode, obj["bigf"])
		}
	}

	// column order must be preserved in the raw bytes
	enc := newJSON(spec.JSONSpec{Mode: "ndjson"})
	var buf bytes.Buffer
	_ = enc.Begin(&buf, cols)
	_ = enc.Write(rec)
	_ = enc.End()
	dec := json.NewDecoder(bytes.NewReader(buf.Bytes()))
	dec.Token() // '{'
	var order []string
	for dec.More() {
		tok, _ := dec.Token() // key
		order = append(order, tok.(string))
		var v any
		dec.Decode(&v) // value
	}
	for i := range cols {
		if i >= len(order) || order[i] != cols[i] {
			t.Fatalf("column order not preserved: got %v want %v", order, cols)
		}
	}
}
