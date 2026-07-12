package decoder

import (
	"context"
	"strings"
	"testing"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

func decodeAllXML(t *testing.T, s spec.XMLSpec, fields []spec.FieldSpec, doc string) []canonical.Record {
	t.Helper()
	d := newXML(s, fields)
	var out []canonical.Record
	err := d.Decode(context.Background(), strings.NewReader(doc), func(r *canonical.Record) error {
		cp := canonical.Record{Seq: r.Seq, Fields: append([]canonical.Field(nil), r.Fields...)}
		out = append(out, cp)
		return nil
	})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func TestXMLDecoder(t *testing.T) {
	doc := `<?xml version="1.0"?>
<!-- header comment -->
<cdrs generated="today">
  <cdr id="1" zone="a">
    <msisdn>27831234567</msisdn>
    <seconds> 90 </seconds>
    <note>a &amp; b &lt;ok&gt;</note>
    <cdata><![CDATA[raw <text> & more]]></cdata>
    <nested><inner>x</inner><inner2>y</inner2></nested>
    <empty/>
  </cdr>
  <cdr id="2"><msisdn>27829998888</msisdn><seconds>30</seconds></cdr>
</cdrs>`

	recs := decodeAllXML(t, spec.XMLSpec{}, nil, doc) // auto-detect record element
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2", len(recs))
	}
	r1 := recs[0]
	want1 := map[string]string{
		"id": "1", "zone": "a", "msisdn": "27831234567", "seconds": "90",
		"note": "a & b <ok>", "cdata": "raw <text> & more", "nested": "xy", "empty": "",
	}
	if len(r1.Fields) != len(want1) {
		t.Fatalf("record1 fields = %+v", r1.Fields)
	}
	for k, w := range want1 {
		v, ok := r1.Get(k)
		if !ok || v.String() != w {
			t.Errorf("record1 %q = %q (ok=%v), want %q", k, v.String(), ok, w)
		}
	}
	// attributes come first, then children in document order
	if r1.Fields[0].Name != "id" || r1.Fields[2].Name != "msisdn" {
		t.Errorf("field order: %+v", r1.Fields)
	}
	if recs[1].Seq != 2 {
		t.Errorf("seq = %d, want 2", recs[1].Seq)
	}

	// explicit record element + declared-type coercion
	typed := decodeAllXML(t, spec.XMLSpec{RecordElement: "cdr"},
		[]spec.FieldSpec{{Name: "seconds", Type: spec.TypeInteger}}, doc)
	if v, _ := typed[0].Get("seconds"); v != canonical.IntVal(90) {
		t.Errorf("typed seconds = %+v, want IntVal(90)", v)
	}

	// empty document → zero records
	if n := len(decodeAllXML(t, spec.XMLSpec{}, nil, "")); n != 0 {
		t.Errorf("empty doc records = %d", n)
	}
}

// TestXMLDecoderNamespaceDecls asserts xmlns / xmlns:p declarations are markup,
// not data — they must not become fields (an xmlns:p would otherwise fabricate
// a field named "p" that can shadow a real one).
func TestXMLDecoderNamespaceDecls(t *testing.T) {
	doc := `<root><rec xmlns="urn:a" xmlns:p="urn:b" id="7"><p>real</p></rec></root>`
	recs := decodeAllXML(t, spec.XMLSpec{RecordElement: "rec"}, nil, doc)
	if len(recs) != 1 {
		t.Fatalf("records = %d", len(recs))
	}
	if len(recs[0].Fields) != 2 {
		t.Fatalf("fields = %+v (xmlns decls must be skipped)", recs[0].Fields)
	}
	if v, _ := recs[0].Get("id"); v.String() != "7" {
		t.Errorf("id = %+v", v)
	}
	if v, _ := recs[0].Get("p"); v.String() != "real" {
		t.Errorf("p = %q, want the CHILD element value, not the xmlns prefix", v.String())
	}
}

func TestXMLDecoderMalformed(t *testing.T) {
	d := newXML(spec.XMLSpec{}, nil)
	err := d.Decode(context.Background(), strings.NewReader("<root><rec><a>1</a></wrong></root>"), func(*canonical.Record) error { return nil })
	if err == nil {
		t.Fatal("mismatched tags: expected an error")
	}
}
