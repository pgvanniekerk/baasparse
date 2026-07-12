package encoder

import (
	"bytes"
	"encoding/xml"
	"strings"
	"testing"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// TestXMLEncoderRoundTrip encodes records (with escaping edge cases) and reads
// them back with encoding/xml, asserting well-formedness and value fidelity.
func TestXMLEncoderRoundTrip(t *testing.T) {
	cols := []string{"s", "esc", "uni", "i", "f", "b", "nul", "missing"}
	rec := &canonical.Record{Seq: 1, Fields: []canonical.Field{
		{Name: "s", Val: canonical.StringVal("hello world")},
		{Name: "esc", Val: canonical.StringVal(`a<b>&"c'd` + "\n\ttail")},
		{Name: "uni", Val: canonical.StringVal("café 中文 🚀")},
		{Name: "i", Val: canonical.IntVal(-42)},
		{Name: "f", Val: canonical.FloatVal(3.5)},
		{Name: "b", Val: canonical.BoolVal(true)},
		{Name: "nul", Val: canonical.NullVal()},
	}}

	enc := newXMLEnc(spec.XMLSpec{RootElement: "rows", RecordElement: "row"})
	var out bytes.Buffer
	if err := enc.Begin(&out, cols); err != nil {
		t.Fatal(err)
	}
	if err := enc.Write(rec); err != nil {
		t.Fatal(err)
	}
	if err := enc.End(); err != nil {
		t.Fatal(err)
	}

	type row struct {
		S   *string `xml:"s"`
		Esc *string `xml:"esc"`
		Uni *string `xml:"uni"`
		I   *int64  `xml:"i"`
		F   *string `xml:"f"`
		B   *bool   `xml:"b"`
		Nul *string `xml:"nul"`
		Mis *string `xml:"missing"`
	}
	type rows struct {
		Rows []row `xml:"row"`
	}
	var got rows
	if err := xml.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("not well-formed XML: %v\n%s", err, out.String())
	}
	if len(got.Rows) != 1 {
		t.Fatalf("rows = %d", len(got.Rows))
	}
	r := got.Rows[0]
	if r.S == nil || *r.S != "hello world" {
		t.Errorf("s = %v", r.S)
	}
	if r.Esc == nil || *r.Esc != `a<b>&"c'd`+"\n\ttail" {
		t.Errorf("esc = %q", *r.Esc)
	}
	if r.Uni == nil || *r.Uni != "café 中文 🚀" {
		t.Errorf("uni = %v", r.Uni)
	}
	if r.I == nil || *r.I != -42 {
		t.Errorf("i = %v", r.I)
	}
	if r.B == nil || !*r.B {
		t.Errorf("b = %v", r.B)
	}
	if r.Nul != nil || r.Mis != nil {
		t.Errorf("null/missing must omit the element: nul=%v missing=%v", r.Nul, r.Mis)
	}
}

// TestAppendXMLTextDifferential compares the hand-rolled escaper byte-for-byte
// with encoding/xml's EscapeText across edge cases.
func TestAppendXMLTextDifferential(t *testing.T) {
	cases := []string{
		"plain", "", `a<b>&"c'd`, "tabs\tand\nnewlines\rcr", "café 中文 🚀 ✓",
		"invalid \x01 control", "del \x7f ok", "fffd \xff\xfe raw bytes",
		"]]> sequence", strings.Repeat("x&<>\"'\n", 50),
	}
	for _, s := range cases {
		var ref bytes.Buffer
		_ = xml.EscapeText(&ref, []byte(s))
		got := appendXMLText(nil, s)
		if !bytes.Equal(got, ref.Bytes()) {
			t.Errorf("escape %q:\n got %q\nwant %q", s, got, ref.Bytes())
		}
	}
}

// TestSanitizeXMLNameAlwaysValid property-tests that whatever rune a column
// name contains, the sanitized element name parses as well-formed XML — the
// guarantee is checked with the stdlib parser itself (unicode.IsLetter admits
// runes the XML grammar rejects; the XML 1.0 range tables must not).
func TestSanitizeXMLNameAlwaysValid(t *testing.T) {
	var runes []rune
	for r := rune(0x20); r <= 0x500; r++ {
		runes = append(runes, r)
	}
	for _, r := range []rune{0x2000, 0x203F, 0x2183, 0x3000, 0x3006, 0xD7FF, 0xFDD0, 0xFE70, 0xFFFD, 0x10000, 0x2F800} {
		runes = append(runes, r)
	}
	for _, r := range runes {
		for _, name := range []string{string(r), "f" + string(r), string(r) + "f"} {
			n := xmlSafeName(name)
			doc := "<" + n + ">x</" + n + ">"
			var out struct {
				V string `xml:",chardata"`
			}
			if err := xml.Unmarshal([]byte(doc), &out); err != nil {
				t.Fatalf("rune U+%04X: sanitized name %q is not valid XML: %v", r, n, err)
			}
		}
	}
}

func TestXMLSafeName(t *testing.T) {
	cases := map[string]string{
		"plain":      "plain",
		"with space": "with_space",
		"9lead":      "_9lead",
		"":           "_",
		"a.b-c_d":    "a.b-c_d",
		"weird!name": "weird_name",
		"<tag>":      "_tag_",
		"中文":         "中文",
	}
	for in, want := range cases {
		if got := xmlSafeName(in); got != want {
			t.Errorf("xmlSafeName(%q) = %q, want %q", in, got, want)
		}
	}
}
