package pipeline

import (
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
