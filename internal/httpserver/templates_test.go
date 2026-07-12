package httpserver

import (
	"html/template"
	"io"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// TestTemplatesRender parses every embedded template and executes each page (and
// the connection-test fragment) with representative data, so a template
// syntax/field error surfaces in CI rather than at first page load.
func TestTemplatesRender(t *testing.T) {
	tmpl, err := template.New("").Funcs(funcMap()).ParseFS(templatesFS, "templates/*.html")
	if err != nil {
		t.Fatalf("parse templates: %v", err)
	}

	user := &store.User{Username: "admin", DisplayName: "Admin", Roles: []string{"Administrator"}}
	dss := []store.Datasource{
		{ID: 1, Name: "local", Kind: "posix", Root: "/data/file_data", CreatedOn: time.Now()},
		{ID: 2, Name: "minio", Kind: "s3", S3: store.S3Config{Endpoint: "minio:9000", Region: "us-east-1"}, CreatedOn: time.Now()},
	}
	files := []store.ProcessedFile{
		{FileUID: 1, PipelineName: "p", Name: "a.csv", OutputName: "a-1.json", Status: "DONE", RecordsIn: 3, RecordsOut: 3, CollectedOn: time.Now()},
		{FileUID: 2, PipelineName: "p", Name: "bad.csv", Status: "QUARANTINED", Reason: "FILE_PROCESSING_ERROR: dsv decode failed", CollectedOn: time.Now()},
	}

	pages := []struct {
		name string
		data any
	}{
		{"datasources", pageData{Active: "datasources", Data: dss, User: user}},
		{"datasources", pageData{Active: "datasources", Data: []store.Datasource(nil), User: user}},
		{"datasource_new", pageData{Active: "datasources", Data: nil, User: user}},
		{"datasource_new", pageData{Active: "datasources", Data: map[string]any{"Error": "boom"}, User: user}},
		{"pipeline_new", pageData{Active: "pipelines", Data: map[string]any{"Datasources": dss}, User: user}},
		{"pipeline_new", pageData{Active: "pipelines", Data: map[string]any{"Datasources": []store.Datasource(nil)}, User: user}},
		{"files", pageData{Active: "files", Data: files, User: user}},
		{"files", pageData{Active: "files", Data: []store.ProcessedFile(nil), User: user}},
		{"pipelines", pageData{Active: "pipelines", Data: []store.Pipeline(nil), User: user}},
	}
	for _, p := range pages {
		if err := tmpl.ExecuteTemplate(io.Discard, p.name, p.data); err != nil {
			t.Errorf("render %q: %v", p.name, err)
		}
	}

	frags := []struct {
		name string
		data any
	}{
		{"test_result", map[string]any{"OK": true, "Detail": "connection verified"}},
		{"test_result", map[string]any{"Error": "dial tcp: connection refused"}},
	}
	for _, f := range frags {
		if err := tmpl.ExecuteTemplate(io.Discard, f.name, f.data); err != nil {
			t.Errorf("render fragment %q: %v", f.name, err)
		}
	}
}

// TestParseOutputs_DifferentFieldsPerDestination is the point of the design:
// destinations do not merely subset one shared field list, they each define their
// own output structure. Billing takes two billable columns; revenue assurance takes
// everything plus a derived key.
func TestParseOutputs_DifferentFieldsPerDestination(t *testing.T) {
	billing := `{"name":"billing","kind":"file","format":{"kind":"dsv","delimiter":"pipe","hasHeader":true},
		"compress":false,"passThrough":false,
		"fields":[{"output":"msisdn","kind":"field","source":"msisdn"},
		          {"output":"duration","kind":"field","source":"duration","type":"integer"}]}`
	ra := `{"name":"revenue-assurance","kind":"file","format":{"kind":"json","jsonMode":"ndjson"},
		"compress":true,"passThrough":false,
		"fields":[{"output":"msisdn","kind":"field","source":"msisdn"},
		          {"output":"duration","kind":"field","source":"duration"},
		          {"output":"cell","kind":"field","source":"cell"},
		          {"output":"source_tag","kind":"const","const":"RA","type":"string"}]}`

	form := url.Values{}
	form.Add("dest_json", billing)
	form.Add("dest_json", ra)
	outs, err := parseOutputs(&http.Request{Form: form})
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(outs) != 2 {
		t.Fatalf("destinations = %d, want 2", len(outs))
	}

	b := outs[0]
	if b.Transform == nil || len(b.Transform.Fields) != 2 {
		t.Fatalf("billing must carry its OWN two fields: %+v", b.Transform)
	}
	if got := b.Format.Columns; len(got) != 2 || got[0] != "msisdn" || got[1] != "duration" {
		t.Fatalf("billing columns = %v", got)
	}
	if b.Format.Kind != spec.FormatDSV || b.Format.DSV.Delimiter != "|" || b.Format.Compress != "" {
		t.Fatalf("billing format = %+v compress=%q", b.Format, b.Format.Compress)
	}

	r := outs[1]
	if r.Transform == nil || len(r.Transform.Fields) != 4 {
		t.Fatalf("revenue assurance must carry its own FOUR fields: %+v", r.Transform)
	}
	if r.Transform.Fields[3].Kind != spec.FieldConst || r.Transform.Fields[3].Const != "RA" {
		t.Fatalf("derived field lost: %+v", r.Transform.Fields[3])
	}
	if r.Format.Compress != "gzip" {
		t.Fatalf("revenue assurance should be gzipped, got %q", r.Format.Compress)
	}
	// The two destinations genuinely differ in shape — not just in encoding.
	if len(b.Transform.Fields) == len(r.Transform.Fields) {
		t.Fatal("destinations must be able to carry different field sets")
	}
}

// TestParseOutputs_PassThrough: "every input field, unchanged" stays PassThrough —
// the cheapest path through the engine — rather than an expanded identity mapping.
func TestParseOutputs_PassThrough(t *testing.T) {
	form := url.Values{}
	form.Add("dest_json", `{"name":"all","kind":"file","format":{"kind":"json","jsonMode":"ndjson"},"passThrough":true}`)
	outs, err := parseOutputs(&http.Request{Form: form})
	if err != nil {
		t.Fatal(err)
	}
	if !outs[0].Transform.PassThrough || len(outs[0].Transform.Fields) != 0 {
		t.Fatalf("want pass-through, got %+v", outs[0].Transform)
	}
	if len(outs[0].Format.Columns) != 0 {
		t.Fatalf("pass-through must not pin columns: %v", outs[0].Format.Columns)
	}
}

// TestParseOutputs_RDBMSIsRefusedUntilDeliverable: a database destination can be
// configured, but saving it must be refused rather than silently dropping every
// record it was supposed to receive.
func TestParseOutputs_RDBMSIsRefusedUntilDeliverable(t *testing.T) {
	form := url.Values{}
	form.Add("dest_json", `{"name":"warehouse","kind":"rdbms","table":"cdr_fact","dbMode":"insert",
		"fields":[{"output":"msisdn","kind":"field","source":"msisdn"}]}`)
	outs, err := parseOutputs(&http.Request{Form: form})
	if err != nil {
		t.Fatal(err)
	}
	if outs[0].Kind != store.OutputRDBMS || outs[0].RDBMS.Table != "cdr_fact" {
		t.Fatalf("rdbms target = %+v", outs[0])
	}
	if outs[0].RDBMS.Columns["msisdn"] != "msisdn" {
		t.Fatalf("column mapping = %v", outs[0].RDBMS.Columns)
	}
	if err := store.ValidateOutputs(outs); err == nil {
		t.Fatal("a database destination must be refused until it can actually be delivered")
	}
}

// TestParseOutputs_MalformedRowIsRejected: a corrupt row must fail loudly, never
// silently produce a destination that writes the wrong thing.
func TestParseOutputs_MalformedRowIsRejected(t *testing.T) {
	form := url.Values{}
	form.Add("dest_json", `{"name":"ok","kind":"file"}`)
	form.Add("dest_json", `{not json`)
	if _, err := parseOutputs(&http.Request{Form: form}); err == nil {
		t.Fatal("a malformed destination row must be rejected")
	}
	form2 := url.Values{}
	form2.Add("dest_json", `{"name":"  ","kind":"file"}`)
	if _, err := parseOutputs(&http.Request{Form: form2}); err == nil {
		t.Fatal("a nameless destination must be rejected")
	}
}

// TestParseOutputs_RenameAndConcat covers what a checkbox list could not express and
// the field table can: renaming an input field on the way out, and building one
// from several. The output name is what downstream sees; the source is where the
// value comes from.
func TestParseOutputs_RenameAndConcat(t *testing.T) {
	form := url.Values{}
	form.Add("dest_json", `{"name":"billing","kind":"file",
		"format":{"kind":"dsv","delimiter":"pipe","hasHeader":true},"passThrough":false,
		"fields":[
			{"output":"subscriber","kind":"field","source":"msisdn"},
			{"output":"secs","kind":"field","source":"duration","type":"integer"},
			{"output":"route","kind":"concat","parts":["cell","'-'","msisdn"]},
			{"output":"feed","kind":"const","const":"BILLING"}
		]}`)
	outs, err := parseOutputs(&http.Request{Form: form})
	if err != nil {
		t.Fatal(err)
	}
	tr := outs[0].Transform
	if tr == nil || len(tr.Fields) != 4 {
		t.Fatalf("transform = %+v", tr)
	}
	// Renamed: downstream sees "subscriber", drawn from the input field "msisdn".
	if tr.Fields[0].Output != "subscriber" || tr.Fields[0].Source != "msisdn" {
		t.Fatalf("rename lost: %+v", tr.Fields[0])
	}
	if tr.Fields[1].Type != spec.TypeInteger {
		t.Fatalf("type lost: %+v", tr.Fields[1])
	}
	if tr.Fields[2].Kind != spec.FieldConcat || len(tr.Fields[2].Parts) != 3 {
		t.Fatalf("concat lost: %+v", tr.Fields[2])
	}
	// Columns follow the table's ORDER, under the OUTPUT names.
	want := []string{"subscriber", "secs", "route", "feed"}
	got := outs[0].Format.Columns
	if len(got) != 4 {
		t.Fatalf("columns = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("column %d = %q, want %q (order must follow the table)", i, got[i], want[i])
		}
	}
}
