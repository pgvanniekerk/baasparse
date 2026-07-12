package pipeline

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/pgvanniekerk/baasparse/internal/spec"
)

func dsv(delim string, header bool, cols ...string) spec.FormatSpec {
	return spec.FormatSpec{Kind: spec.FormatDSV, DSV: &spec.DSVSpec{Delimiter: delim, HasHeader: header, Columns: cols}}
}
func js(mode string) spec.FormatSpec {
	return spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: mode}}
}

func TestDSVtoJSON_passthrough(t *testing.T) {
	in := "a,b,c\n1,2,3\n4,5,6\n"
	var out strings.Builder
	sp := Spec{Input: dsv(",", true), Transform: spec.TransformSpec{PassThrough: true}, Output: js("ndjson")}
	st, susp, err := Run(context.Background(), sp, strings.NewReader(in), &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(susp) != 0 || st.RecordsIn != 2 || st.RecordsOut != 2 {
		t.Fatalf("stats=%+v susp=%v", st, susp)
	}
	want := `{"a":"1","b":"2","c":"3"}` + "\n" + `{"a":"4","b":"5","c":"6"}` + "\n"
	if out.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", out.String(), want)
	}
}

func TestJSONtoDSV_withTransform(t *testing.T) {
	in := `{"msisdn":"27831234567","seconds":"90","junk":"x"}` + "\n" +
		`{"msisdn":"27829998888","seconds":"30","junk":"y"}` + "\n"
	var out strings.Builder
	tr := spec.TransformSpec{Fields: []spec.FieldMap{
		{Output: "a_number", Kind: spec.FieldFromInput, Source: "msisdn"},
		{Output: "minutes", Kind: spec.FieldFromInput, Source: "seconds", Type: spec.TypeInteger},
		{Output: "tag", Kind: spec.FieldConst, Const: "voice"},
	}}
	sp := Spec{Input: js("ndjson"), Transform: tr, Output: dsv(",", true)}
	st, susp, err := Run(context.Background(), sp, strings.NewReader(in), &out)
	if err != nil {
		t.Fatal(err)
	}
	if len(susp) != 0 || st.RecordsOut != 2 {
		t.Fatalf("stats=%+v susp=%v", st, susp)
	}
	want := "a_number,minutes,tag\n27831234567,90,voice\n27829998888,30,voice\n"
	if out.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", out.String(), want)
	}
}

func TestJSONtoDSV_arrayMode(t *testing.T) {
	in := `[{"x":1,"y":2},{"x":3,"y":4}]`
	var out strings.Builder
	sp := Spec{Input: js("array"), Transform: spec.TransformSpec{PassThrough: true}, Output: dsv(",", true)}
	_, _, err := Run(context.Background(), sp, strings.NewReader(in), &out)
	if err != nil {
		t.Fatal(err)
	}
	want := "x,y\n1,2\n3,4\n"
	if out.String() != want {
		t.Fatalf("got:\n%q\nwant:\n%q", out.String(), want)
	}
}

// TestSession_PerDestinationShape is the point of per-destination transforms: one
// decode, N different record SHAPES — not merely N encodings of the same shape.
// Billing takes two billable columns; revenue assurance takes everything plus a
// derived constant.
func TestSession_PerDestinationShape(t *testing.T) {
	in := spec.FormatSpec{
		Kind:   spec.FormatDSV,
		Fields: []spec.FieldSpec{{Name: "msisdn"}, {Name: "duration", Type: spec.TypeInteger}, {Name: "cell"}},
		DSV:    &spec.DSVSpec{Delimiter: ",", HasHeader: true, Columns: []string{"msisdn", "duration", "cell"}},
	}
	var billing, ra bytes.Buffer

	sess, err := NewMultiSession(Spec{Input: in}, []Target{
		{
			Name: "billing",
			Transform: spec.TransformSpec{Fields: []spec.FieldMap{
				{Output: "msisdn", Kind: spec.FieldFromInput, Source: "msisdn"},
				{Output: "duration", Kind: spec.FieldFromInput, Source: "duration"},
			}},
			Format: spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"},
				Columns: []string{"msisdn", "duration"}},
			W: &billing,
		},
		{
			Name: "revenue-assurance",
			Transform: spec.TransformSpec{Fields: []spec.FieldMap{
				{Output: "msisdn", Kind: spec.FieldFromInput, Source: "msisdn"},
				{Output: "duration", Kind: spec.FieldFromInput, Source: "duration"},
				{Output: "cell", Kind: spec.FieldFromInput, Source: "cell"},
				{Output: "tag", Kind: spec.FieldConst, Const: "RA"},
			}},
			Format: spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"},
				Columns: []string{"msisdn", "duration", "cell", "tag"}},
			W: &ra,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	data := "msisdn,duration,cell\n27831234567,90,JHB-012\n"
	if err := sess.Consume(context.Background(), strings.NewReader(data), "f.csv"); err != nil {
		t.Fatal(err)
	}
	stats, _, res := sess.Close()
	if stats.RecordsIn != 1 || stats.RecordsOut != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	for _, r := range res {
		if r.Err != nil {
			t.Fatalf("%s: %v", r.Name, r.Err)
		}
	}

	gotB := strings.TrimSpace(billing.String())
	wantB := `{"msisdn":"27831234567","duration":90}`
	if gotB != wantB {
		t.Fatalf("billing:\n got %s\nwant %s", gotB, wantB)
	}
	gotR := strings.TrimSpace(ra.String())
	wantR := `{"msisdn":"27831234567","duration":90,"cell":"JHB-012","tag":"RA"}`
	if gotR != wantR {
		t.Fatalf("revenue assurance:\n got %s\nwant %s", gotR, wantR)
	}
	// The two destinations received genuinely different records from one decode.
	if gotB == gotR {
		t.Fatal("destinations must be able to carry different fields")
	}
}
