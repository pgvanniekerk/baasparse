package pipeline

import (
	"archive/tar"
	"bytes"
	"compress/gzip" // stdlib on the build side; the engine reads with klauspost
	"context"
	"io"
	"strconv"
	"testing"

	kgzip "github.com/klauspost/compress/gzip"

	"github.com/pgvanniekerk/baasparse/internal/container"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// benchSpec builds a DSV(comma, header) -> JSON(ndjson) passthrough pipeline with
// nFields declared string columns.
func benchSpec(nFields int) Spec {
	fields := make([]spec.FieldSpec, nFields)
	cols := make([]string, nFields)
	for i := range fields {
		n := "f" + strconv.Itoa(i+1)
		fields[i] = spec.FieldSpec{Name: n}
		cols[i] = n
	}
	return Spec{
		Input:     spec.FormatSpec{Kind: spec.FormatDSV, Fields: fields, DSV: &spec.DSVSpec{Delimiter: ",", HasHeader: true, Columns: cols}},
		Transform: spec.TransformSpec{PassThrough: true},
		Output:    spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}},
	}
}

// genDSV builds an in-memory DSV: a header + nRows data rows of nFields cells
// (a deterministic mix of ints / words / decimals / msisdn-like tokens).
func genDSV(nFields, nRows int) []byte {
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	var b bytes.Buffer
	for i := 0; i < nFields; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("f")
		b.WriteString(strconv.Itoa(i + 1))
	}
	b.WriteByte('\n')
	for r := 0; r < nRows; r++ {
		for c := 0; c < nFields; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			switch c & 3 {
			case 0:
				b.WriteString(strconv.Itoa(r*100 + c))
			case 1:
				b.WriteString(words[(r+c)%len(words)])
				b.WriteByte('-')
				b.WriteString(strconv.Itoa(c))
			case 2:
				b.WriteString(strconv.FormatFloat(float64(r+c)*0.5, 'f', 3, 64))
			default:
				b.WriteString("27")
				b.WriteString(strconv.Itoa((r*7 + c) % 1000000000))
			}
		}
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// BenchmarkDSVToJSON measures the full decode->transform->encode hot path over a
// 50-field x 10000-row DSV file, ndjson output. Divide allocs/op by 10000 for
// per-record allocations.
func BenchmarkDSVToJSON(b *testing.B) {
	const nFields, nRows = 50, 10000
	sp := benchSpec(nFields)
	data := genDSV(nFields, nRows)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Run(context.Background(), sp, bytes.NewReader(data), io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

// genNDJSON builds an in-memory NDJSON document: nRows objects of nFields keys
// with a deterministic mix of strings / ints / floats / bools.
func genNDJSON(nFields, nRows int) []byte {
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	var b bytes.Buffer
	for r := 0; r < nRows; r++ {
		b.WriteByte('{')
		for c := 0; c < nFields; c++ {
			if c > 0 {
				b.WriteByte(',')
			}
			b.WriteString("\"f")
			b.WriteString(strconv.Itoa(c + 1))
			b.WriteString("\":")
			switch c & 3 {
			case 0:
				b.WriteString(strconv.Itoa(r*100 + c))
			case 1:
				b.WriteByte('"')
				b.WriteString(words[(r+c)%len(words)])
				b.WriteByte('-')
				b.WriteString(strconv.Itoa(c))
				b.WriteByte('"')
			case 2:
				b.WriteString(strconv.FormatFloat(float64(r+c)*0.5, 'f', 3, 64))
			default:
				if (r+c)&1 == 0 {
					b.WriteString("true")
				} else {
					b.WriteString("false")
				}
			}
		}
		b.WriteString("}\n")
	}
	return b.Bytes()
}

// jsonToDSVSpec builds a JSON(ndjson) -> DSV(comma, header) passthrough pipeline.
func jsonToDSVSpec(nFields int) Spec {
	fields := make([]spec.FieldSpec, nFields)
	cols := make([]string, nFields)
	for i := range fields {
		n := "f" + strconv.Itoa(i+1)
		fields[i] = spec.FieldSpec{Name: n}
		cols[i] = n
	}
	return Spec{
		Input:     spec.FormatSpec{Kind: spec.FormatJSON, Fields: fields, JSON: &spec.JSONSpec{Mode: "ndjson"}},
		Transform: spec.TransformSpec{PassThrough: true},
		Output:    spec.FormatSpec{Kind: spec.FormatDSV, DSV: &spec.DSVSpec{Delimiter: ",", Columns: cols}},
	}
}

// BenchmarkJSONToDSV measures the reverse hot path: NDJSON decode -> passthrough
// -> DSV encode, 50 fields x 10000 rows.
func BenchmarkJSONToDSV(b *testing.B) {
	const nFields, nRows = 50, 10000
	sp := jsonToDSVSpec(nFields)
	data := genNDJSON(nFields, nRows)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Run(context.Background(), sp, bytes.NewReader(data), io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkTarGzDSVToGzJSON measures the full phase-1 archive chain: a tar.gz of
// 10 DSV members (1000 rows x 50 fields each) decoded member-by-member through
// one shared encoder session, output gzip-compressed — i.e. gunzip+untar+decode
// +transform+encode+gzip, all streaming.
func BenchmarkTarGzDSVToGzJSON(b *testing.B) {
	const nFields, nRows, nMembers = 50, 1000, 10
	memberData := genDSV(nFields, nRows)
	var arc bytes.Buffer
	gz := gzip.NewWriter(&arc)
	tw := tar.NewWriter(gz)
	for m := 0; m < nMembers; m++ {
		_ = tw.WriteHeader(&tar.Header{Name: "m" + strconv.Itoa(m) + ".csv", Mode: 0o644, Size: int64(len(memberData)), Typeflag: tar.TypeReg})
		_, _ = tw.Write(memberData)
	}
	tw.Close()
	gz.Close()
	sp := benchSpec(nFields)
	b.SetBytes(int64(arc.Len()))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tg, err := container.OpenTarGz(bytes.NewReader(arc.Bytes()), "*.csv", container.Limits{})
		if err != nil {
			b.Fatal(err)
		}
		out, _ := kgzip.NewWriterLevel(io.Discard, kgzip.BestSpeed)
		sess, err := NewSession(sp, out)
		if err != nil {
			b.Fatal(err)
		}
		for tg.Next() {
			if err := sess.Consume(context.Background(), tg.Reader(), tg.Member()); err != nil {
				b.Fatal(err)
			}
		}
		if err := tg.Err(); err != nil {
			b.Fatal(err)
		}
		if _, _, err := sess.Close(); err != nil {
			b.Fatal(err)
		}
		out.Close()
		tg.Close()
	}
}

// genXML builds an in-memory XML document: nRows <rec> elements of nFields
// child elements under a <recs> root.
func genXML(nFields, nRows int) []byte {
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	var b bytes.Buffer
	b.WriteString("<?xml version=\"1.0\"?>\n<recs>\n")
	for r := 0; r < nRows; r++ {
		b.WriteString("<rec>")
		for c := 0; c < nFields; c++ {
			f := "f" + strconv.Itoa(c+1)
			b.WriteString("<")
			b.WriteString(f)
			b.WriteString(">")
			switch c & 3 {
			case 0:
				b.WriteString(strconv.Itoa(r*100 + c))
			case 1:
				b.WriteString(words[(r+c)%len(words)])
			case 2:
				b.WriteString(strconv.FormatFloat(float64(r+c)*0.5, 'f', 3, 64))
			default:
				b.WriteString("27")
				b.WriteString(strconv.Itoa((r*7 + c) % 1000000000))
			}
			b.WriteString("</")
			b.WriteString(f)
			b.WriteString(">")
		}
		b.WriteString("</rec>\n")
	}
	b.WriteString("</recs>\n")
	return b.Bytes()
}

// BenchmarkXMLToJSON measures XML decode -> passthrough -> NDJSON encode,
// 50 fields x 10000 rows (the XML decoder rides encoding/xml's tokenizer).
func BenchmarkXMLToJSON(b *testing.B) {
	const nFields, nRows = 50, 10000
	fields := make([]spec.FieldSpec, nFields)
	for i := range fields {
		fields[i] = spec.FieldSpec{Name: "f" + strconv.Itoa(i+1)}
	}
	sp := Spec{
		Input:     spec.FormatSpec{Kind: spec.FormatXML, Fields: fields, XML: &spec.XMLSpec{RecordElement: "rec"}},
		Transform: spec.TransformSpec{PassThrough: true},
		Output:    spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}},
	}
	data := genXML(nFields, nRows)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := Run(context.Background(), sp, bytes.NewReader(data), io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}
