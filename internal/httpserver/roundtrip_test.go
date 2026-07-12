package httpserver

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"

	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// Opening a pipeline in the editor and saving it WITHOUT TOUCHING ANYTHING must not
// change it. That sounds obvious and it is exactly what broke: the editor renders a
// subset of a pipeline, so every field it did not render came back as a zero value
// and silently erased what was stored.
//
// This drives the real round trip — buildWizardModel -> (what the browser hydrates
// and re-serializes) -> parseOutputs/pipelineFromForm -> mergeForEdit — and diffs the
// result against the original. Anything that differs is a pipeline the operator never
// edited being quietly rewritten.

// editUntouched simulates opening p in the editor and pressing Save with no changes.
func editUntouched(t *testing.T, p store.Pipeline) store.Pipeline {
	t.Helper()
	m := buildWizardModel(p)

	form := url.Values{}
	form.Set("name", m.Name)
	form.Set("description", m.Description)
	if m.Enabled {
		form.Set("enabled", "on")
	}
	form.Set("datasource_id", itoa(m.DatasourceID))
	form.Set("ds_bucket", m.Bucket)
	form.Set("disposition", m.Disposition)
	if m.PollSeconds > 0 {
		form.Set("poll_seconds", itoa(int64(m.PollSeconds)))
	}
	form.Set("input_kind", m.Input.Kind)
	form.Set("input_delimiter", m.Input.Delimiter)
	if m.Input.HasHeader {
		form.Set("input_has_header", "on")
	}
	form.Set("input_json_mode", m.Input.JSONMode)
	form.Set("input_xml_root", m.Input.XMLRoot)
	form.Set("input_xml_record", m.Input.XMLRecord)
	if m.Input.Container {
		form.Set("input_container", "on")
		form.Set("input_member_glob", m.Input.MemberGlob)
	}
	for _, f := range m.Input.Fields {
		form.Add("decl_name", f.Name)
		form.Add("decl_type", f.Type)
	}
	if m.Archive.Enabled {
		form.Set("archive_enabled", "on")
		form.Set("archive_age_days", itoa(int64(m.Archive.AgeDays)))
		form.Set("archive_compression", m.Archive.Compression)
		form.Set("archive_schedule_seconds", itoa(int64(m.Archive.ScheduleSeconds)))
		if m.Archive.IncludeQuarantined {
			form.Set("archive_include_quarantined", "on")
		}
		form.Set("archive_dest_backend", m.Archive.DestBackend)
		form.Set("archive_dest_root", m.Archive.DestRoot)
	}
	if m.Batch.Enabled {
		form.Set("batch_enabled", "on")
		form.Set("batch_max_files", itoa(int64(m.Batch.MaxFiles)))
		form.Set("batch_max_mb", itoa(m.Batch.MaxMB))
		form.Set("batch_max_age_seconds", itoa(int64(m.Batch.MaxAgeSeconds)))
	}
	for _, d := range m.Destinations {
		b, err := json.Marshal(d)
		if err != nil {
			t.Fatal(err)
		}
		form.Add("dest_json", string(b))
	}

	srv := &Server{}
	posted, err := srv.pipelineFromForm(&http.Request{Form: form})
	if err != nil {
		t.Fatalf("an untouched save was REJECTED: %v", err)
	}
	return mergeForEdit(p, posted)
}

func itoa(v int64) string {
	if v == 0 {
		return ""
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestRoundTrip_LegacyPipelineSurvivesAnUntouchedSave is the regression for the worst
// bug the review found. A pipeline created before destinations carried their own
// structure has Transform==nil on its single destination — which the RUNNER reads as
// "inherit the pipeline transform", but which the editor read as "pass-through".
// Opening it and saving it untouched replaced its whole field mapping: the feed
// silently stopped emitting sub|dur and started emitting every raw input column,
// unrenamed and untyped.
func TestRoundTrip_LegacyPipelineSurvivesAnUntouchedSave(t *testing.T) {
	tr := spec.TransformSpec{Fields: []spec.FieldMap{
		{Output: "sub", Kind: spec.FieldFromInput, Source: "msisdn", Type: spec.TypeString},
		{Output: "dur", Kind: spec.FieldFromInput, Source: "seconds", Type: spec.TypeInteger},
	}}
	// The shape store.GetPipeline now materializes for a legacy pipeline: the
	// synthesized destination carries the pipeline transform EXPLICITLY.
	before := store.Pipeline{
		ID: 1, Name: "legacy",
		Input: spec.FormatSpec{
			Kind:   spec.FormatDSV,
			Fields: []spec.FieldSpec{{Name: "msisdn"}, {Name: "seconds", Type: spec.TypeInteger}, {Name: "cell"}},
			DSV:    &spec.DSVSpec{Delimiter: "|", HasHeader: true, Columns: []string{"msisdn", "seconds", "cell"}},
		},
		Transform: tr,
		Outputs: []store.Output{{
			Name: "default", Kind: store.OutputFile, DSUID: 7, Transform: &tr,
			Format: spec.FormatSpec{Kind: spec.FormatDSV, DSV: &spec.DSVSpec{Delimiter: "|", HasHeader: true,
				Columns: []string{"sub", "dur"}}, Columns: []string{"sub", "dur"}},
		}},
		Source: store.Source{Backend: "posix", InputDir: "legacy/in", Disposition: "done"},
	}

	after := editUntouched(t, before)

	got := after.Outputs[0].Transform
	if got == nil || got.PassThrough {
		t.Fatalf("an untouched save replaced the field mapping with pass-through — "+
			"the feed would start emitting every raw column, unrenamed: %+v", got)
	}
	if len(got.Fields) != 2 || got.Fields[0].Output != "sub" || got.Fields[0].Source != "msisdn" {
		t.Fatalf("field mapping mangled: %s", mustJSON(t, got))
	}
	if got.Fields[1].Type != spec.TypeInteger {
		t.Fatalf("the integer coercion on dur was lost: %+v", got.Fields[1])
	}
	if after.Outputs[0].DSUID != 7 {
		t.Fatalf("the destination lost its identity (its output sequence would restart at 1)")
	}
	// The input must survive too: a legacy DSV input's columns come back through the
	// declared fields, and the '|' delimiter must not collapse to a comma.
	if d := after.Input.DSV; d == nil || d.Delimiter != "|" {
		t.Fatalf("input delimiter changed on an untouched save: %+v", d)
	}
	if len(after.Input.Fields) != 3 {
		t.Fatalf("input fields lost: %+v", after.Input.Fields)
	}
}

// TestRoundTrip_ArchiveConnectionSurvives: the wizard cannot show an S3 archive
// destination's endpoint, bucket or credentials — so an edit must carry them forward
// rather than blanking them. Otherwise archiving silently stops forever.
func TestRoundTrip_ArchiveConnectionSurvives(t *testing.T) {
	pass := spec.TransformSpec{PassThrough: true}
	before := store.Pipeline{
		ID: 2, Name: "arch",
		Input:   spec.FormatSpec{Kind: spec.FormatJSON, Fields: []spec.FieldSpec{{Name: "a"}}, JSON: &spec.JSONSpec{Mode: "ndjson"}},
		Outputs: []store.Output{{Name: "d", Kind: store.OutputFile, Transform: &pass, Format: spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}}}},
		Source:  store.Source{Backend: "posix", InputDir: "arch/in", Disposition: "done"},
		Archive: &store.Archive{
			Enabled: true, AgeDays: 30, Compression: "tar_gz", ScheduleSeconds: 3600,
			Dest: store.Dest{Backend: "s3", S3: store.S3Config{
				Endpoint: "minio:9000", Region: "us-east-1", Bucket: "cold",
				AccessKey: "AK", SecretKey: "SK", UseSSL: true,
			}},
		},
	}
	after := editUntouched(t, before)
	a := after.Archive
	if a == nil || !a.Enabled || a.AgeDays != 30 {
		t.Fatalf("archive policy lost: %+v", a)
	}
	if a.Dest.S3.Endpoint != "minio:9000" || a.Dest.S3.Bucket != "cold" || a.Dest.S3.SecretKey != "SK" {
		t.Fatalf("an untouched save destroyed the archive destination's connection — "+
			"archiving would silently stop forever: %s", mustJSON(t, a.Dest))
	}
	if !a.Dest.S3.UseSSL {
		t.Fatal("UseSSL was flipped off by a save that never touched it")
	}
}

// TestRoundTrip_ExplicitDestinationSurvives: the ordinary case must round-trip too —
// rename, concat, constant, types, compression, per-destination format.
func TestRoundTrip_ExplicitDestinationSurvives(t *testing.T) {
	tr := spec.TransformSpec{Fields: []spec.FieldMap{
		{Output: "subscriber", Kind: spec.FieldFromInput, Source: "msisdn"},
		{Output: "secs", Kind: spec.FieldFromInput, Source: "duration", Type: spec.TypeInteger},
		{Output: "route", Kind: spec.FieldConcat, Parts: []string{"cell", "'-'", "msisdn"}},
		{Output: "feed", Kind: spec.FieldConst, Const: "BILLING"},
	}}
	before := store.Pipeline{
		ID: 3, Name: "p",
		Input: spec.FormatSpec{Kind: spec.FormatDSV,
			Fields: []spec.FieldSpec{{Name: "msisdn"}, {Name: "duration", Type: spec.TypeInteger}, {Name: "cell"}},
			DSV:    &spec.DSVSpec{Delimiter: ",", HasHeader: true, Columns: []string{"msisdn", "duration", "cell"}}},
		Outputs: []store.Output{{
			Name: "billing", Kind: store.OutputFile, DSUID: 11, Transform: &tr,
			Format: spec.FormatSpec{Kind: spec.FormatDSV, Compress: "gzip",
				Columns: []string{"subscriber", "secs", "route", "feed"},
				DSV:     &spec.DSVSpec{Delimiter: "|", HasHeader: true, Columns: []string{"subscriber", "secs", "route", "feed"}}},
		}},
		Source: store.Source{Backend: "posix", InputDir: "p/in", Disposition: "done"},
		Batch:  store.BatchSpec{Enabled: true, MaxFiles: 10, MaxAgeSeconds: 60},
	}
	after := editUntouched(t, before)

	if mustJSON(t, after.Outputs[0].Transform) != mustJSON(t, before.Outputs[0].Transform) {
		t.Fatalf("destination structure changed on an untouched save:\nbefore %s\nafter  %s",
			mustJSON(t, before.Outputs[0].Transform), mustJSON(t, after.Outputs[0].Transform))
	}
	if after.Outputs[0].Format.Compress != "gzip" {
		t.Fatal("compression lost")
	}
	if after.Outputs[0].Format.DSV.Delimiter != "|" {
		t.Fatalf("destination delimiter changed: %q", after.Outputs[0].Format.DSV.Delimiter)
	}
	if mustJSON(t, after.Batch) != mustJSON(t, before.Batch) {
		t.Fatalf("consolidation settings changed: %s", mustJSON(t, after.Batch))
	}
	if mustJSON(t, after.Input) != mustJSON(t, before.Input) {
		t.Fatalf("input changed on an untouched save:\nbefore %s\nafter  %s",
			mustJSON(t, before.Input), mustJSON(t, after.Input))
	}
}
