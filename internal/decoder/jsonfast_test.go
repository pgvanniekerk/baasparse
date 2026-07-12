package decoder

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
)

// refParse is the previous implementation's semantics: encoding/json with
// UseNumber into a map, values converted via canonical.FromJSON. Used as the
// differential-reference for the fast parser.
func refParse(line string) (map[string]canonical.Value, error) {
	dec := json.NewDecoder(strings.NewReader(line))
	dec.UseNumber()
	var obj map[string]any
	if err := dec.Decode(&obj); err != nil {
		return nil, err
	}
	out := make(map[string]canonical.Value, len(obj))
	for k, v := range obj {
		out[k] = canonical.FromJSON(v)
	}
	return out, nil
}

// semanticallyEqual compares values, treating nested JSON (both render as
// strings) by unmarshalled structure rather than text: the reference compacts
// and key-sorts nested values via the map round-trip, while the fast parser
// preserves the raw source text.
func semanticallyEqual(t *testing.T, key string, got, want canonical.Value) bool {
	t.Helper()
	if got.Kind == canonical.KindString && want.Kind == canonical.KindString &&
		len(got.S) > 0 && (got.S[0] == '{' || got.S[0] == '[') {
		var a, b any
		if json.Unmarshal([]byte(got.S), &a) != nil || json.Unmarshal([]byte(want.S), &b) != nil {
			return got.S == want.S
		}
		ja, _ := json.Marshal(a)
		jb, _ := json.Marshal(b)
		return string(ja) == string(jb)
	}
	return got == want
}

func TestFastJSONParserDifferential(t *testing.T) {
	cases := []string{
		`{}`,
		`{"a":1}`,
		`{"a":1,"b":"two","c":3.5,"d":true,"e":false,"f":null}`,
		`{"s":"hello world","empty":"","space":" leading"}`,
		`{"esc":"a\"b\\c\/d\b\f\n\r\t"}`,
		`{"uni":"é中😀"}`,          // é 中 😀 (surrogate pair)
		`{"lone":"\ud800"}`,                            // lone high surrogate → U+FFFD
		`{"lonepair":"\ud800A"}`,                  // high surrogate + non-low
		`{"neg":-42,"exp":1e3,"nexp":-1.5E-2,"zero":0}`,
		`{"big":9223372036854775807,"over":9223372036854775808}`, // int64 max, +1 → float
		`{"huge":1e999}`,                               // float overflow → raw text
		`{"nested":{"z":1,"a":[1,2,{"k":"v"}]}}`,
		`{"arr":[1,"two",null,true]}`,
		`{"brace":"}{ not a brace"}`,
		`{"quote_in_nested":{"s":"a\"}b"}}`,
		"{ \"ws\" : 1 ,\t\"tab\" : \"v\" }",
		`{"utf8":"café 中文 🚀 ✓"}`,
		`{"controls":"a\u0001b\u001fc"}`,
		`{"dupfirst":1,"dupfirst":2}`, // duplicate keys — divergence, checked separately
	}
	for _, line := range cases {
		rec := &canonical.Record{}
		fastErr := parseJSONObject(line, 1, nil, rec)
		want, refErr := refParse(line)
		if (fastErr != nil) != (refErr != nil) {
			t.Errorf("%s: error divergence fast=%v ref=%v", line, fastErr, refErr)
			continue
		}
		if fastErr != nil {
			continue
		}
		if strings.Contains(line, "dupfirst") {
			// Documented divergence: fast keeps the FIRST occurrence (ref map kept
			// the last). Assert our defined behaviour.
			if v, _ := rec.Get("dupfirst"); v != canonical.IntVal(1) {
				t.Errorf("duplicate keys: want first-wins (1), got %+v", v)
			}
			continue
		}
		if len(rec.Fields) != len(want) {
			t.Errorf("%s: field count fast=%d ref=%d", line, len(rec.Fields), len(want))
			continue
		}
		for _, f := range rec.Fields {
			w, ok := want[f.Name]
			if !ok {
				t.Errorf("%s: unexpected key %q", line, f.Name)
				continue
			}
			if !semanticallyEqual(t, f.Name, f.Val, w) {
				t.Errorf("%s: key %q fast=%+v ref=%+v", line, f.Name, f.Val, w)
			}
		}
	}
}

func TestFastJSONParserErrors(t *testing.T) {
	bad := []string{
		``, `   `, `[1,2]`, `"str"`, `123`, `{`, `{"a"}`, `{"a":}`, `{"a":1,}`,
		`{"a":1 "b":2}`, `{"a":"unterminated`, `{"a":truex}`, `{"a":nul}`,
		`{"a":1}}`, `{"a":1} garbage`, `{"a":"\q"}`, `{"a":"\u12"}`, `{"a":.5}`,
		// malformed numbers (grammar violations encoding/json also rejects)
		`{"a":01}`, `{"a":-01.5}`, `{"a":1.}`, `{"a":--1}`, `{"a":-}`, `{"a":1e}`,
		`{"a":1e+}`, `{"a":1-2}`, `{"a":1..2}`, `{"a":1e++5}`, `{"a":+1}`,
		// malformed nested values (mismatched closers / garbage contents)
		`{"a":[1}}`, `{"a":{"b":1]}`, `{"a":[[1}]}`, `{"a":[1 2 oops]}`, `{"a":[1}]`,
		`{"a":{garbage}}`, `{"a":[1,]}`, `{"a":{"k":1,}}`,
		// raw control characters inside strings (must be escaped per RFC 8259)
		"{\"a\":\"x\x01y\"}", "{\"a\":\"tab\tinside\"}",
	}
	for _, line := range bad {
		rec := &canonical.Record{}
		if err := parseJSONObject(line, 1, nil, rec); err == nil {
			t.Errorf("%q: expected error, got fields %+v", line, rec.Fields)
		}
	}
	// overflow stays a value (grammar-valid, unrepresentable) — not an error
	rec := &canonical.Record{}
	if err := parseJSONObject(`{"huge":1e999}`, 1, nil, rec); err != nil {
		t.Fatalf("1e999 must keep raw text, got error: %v", err)
	}
	if v, _ := rec.Get("huge"); v != canonical.StringVal("1e999") {
		t.Errorf("1e999 = %+v, want StringVal", v)
	}
}

// TestFastJSONParserOrder asserts document key order is preserved (the old map
// implementation sorted keys; document order is the natural DSV column order).
func TestFastJSONParserOrder(t *testing.T) {
	rec := &canonical.Record{}
	if err := parseJSONObject(`{"z":1,"m":2,"a":3}`, 1, nil, rec); err != nil {
		t.Fatal(err)
	}
	want := []string{"z", "m", "a"}
	for i, f := range rec.Fields {
		if f.Name != want[i] {
			t.Fatalf("order: got %v", rec.Fields)
		}
	}
}
