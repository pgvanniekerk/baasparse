package encoder

import (
	"bytes"
	"encoding/csv"
	"testing"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// TestDSVEncoderByteCompatible asserts the hand-rolled DSV encoder produces
// byte-identical output to the previous encoding/csv implementation across
// quoting edge cases, for both comma and pipe delimiters.
func TestDSVEncoderByteCompatible(t *testing.T) {
	cols := []string{"plain", "comma", "quote", "nl", "cr", "lead", "pgdump", "uni", "i", "f", "b", "nul", "missing", "empty"}
	rec := &canonical.Record{Seq: 1}
	add := func(n string, v canonical.Value) { rec.Fields = append(rec.Fields, canonical.Field{Name: n, Val: v}) }
	add("plain", canonical.StringVal("hello"))
	add("comma", canonical.StringVal("a,b"))
	add("quote", canonical.StringVal(`say "hi"`))
	add("nl", canonical.StringVal("line1\nline2"))
	add("cr", canonical.StringVal("x\ry"))
	add("lead", canonical.StringVal(" padded"))
	add("pgdump", canonical.StringVal(`\.`))
	add("uni", canonical.StringVal("café|中"))
	add("i", canonical.IntVal(-42))
	add("f", canonical.FloatVal(3.14159))
	add("b", canonical.BoolVal(true))
	add("nul", canonical.NullVal())
	// "missing" never set; "empty" is the empty string
	add("empty", canonical.StringVal(""))

	for _, delim := range []string{",", "|", ";", "\t"} {
		// reference: previous implementation (encoding/csv, header + row)
		var ref bytes.Buffer
		cw := csv.NewWriter(&ref)
		cw.Comma = []rune(delim)[0]
		_ = cw.Write(cols)
		row := make([]string, len(cols))
		for i, c := range cols {
			if v, ok := rec.Get(c); ok {
				row[i] = v.String()
			}
		}
		_ = cw.Write(row)
		cw.Flush()

		// new encoder
		enc, err := newDSV(spec.DSVSpec{Delimiter: delim})
		if err != nil {
			t.Fatal(err)
		}
		var got bytes.Buffer
		if err := enc.Begin(&got, cols); err != nil {
			t.Fatal(err)
		}
		if err := enc.Write(rec); err != nil {
			t.Fatal(err)
		}
		if err := enc.End(); err != nil {
			t.Fatal(err)
		}

		if got.String() != ref.String() {
			t.Errorf("delimiter %q: output differs\n got: %q\nwant: %q", delim, got.String(), ref.String())
		}
	}
}

// TestDSVEncoderRoundTrip asserts encoding/csv can read back exactly what the
// new encoder wrote, cell for cell.
func TestDSVEncoderRoundTrip(t *testing.T) {
	cols := []string{"a", "b", "c"}
	recs := []*canonical.Record{
		{Seq: 1, Fields: []canonical.Field{
			{Name: "a", Val: canonical.StringVal("x,y\n\"z\"")},
			{Name: "b", Val: canonical.FloatVal(-0.5)},
			{Name: "c", Val: canonical.StringVal(" lead and trail ")},
		}},
		{Seq: 2, Fields: []canonical.Field{
			{Name: "c", Val: canonical.StringVal("out-of-order only c")},
		}},
	}
	enc, _ := newDSV(spec.DSVSpec{Delimiter: ","})
	var out bytes.Buffer
	_ = enc.Begin(&out, cols)
	for _, r := range recs {
		if err := enc.Write(r); err != nil {
			t.Fatal(err)
		}
	}
	_ = enc.End()

	cr := csv.NewReader(&out)
	rows, err := cr.ReadAll()
	if err != nil {
		t.Fatalf("csv read-back: %v\n%s", err, out.String())
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %d, want 3 (header + 2)", len(rows))
	}
	if rows[1][0] != "x,y\n\"z\"" || rows[1][1] != "-0.5" || rows[1][2] != " lead and trail " {
		t.Errorf("row1 = %q", rows[1])
	}
	if rows[2][0] != "" || rows[2][2] != "out-of-order only c" {
		t.Errorf("row2 = %q", rows[2])
	}
}
