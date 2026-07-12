package httpserver

import (
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// parseFormatSpec reads a FormatSpec from form fields prefixed by prefix
// ("input" or "output"), e.g. input_kind, input_delimiter, input_has_header,
// input_json_mode. For the INPUT format, the declared fields (decl_name/decl_type
// rows) are the single structure model — their names are the DSV columns (in
// order) and their types coerce each value at decode; there is no free-text
// columns field.
func parseFormatSpec(r *http.Request, prefix string) spec.FormatSpec {
	var fields []spec.FieldSpec
	if prefix == "input" {
		fields = parseInputFieldSpecs(r)
	}
	var fs spec.FormatSpec
	kind := spec.FormatKind(r.FormValue(prefix + "_kind"))
	switch kind {
	case spec.FormatDSV:
		fs = spec.FormatSpec{Kind: spec.FormatDSV, Fields: fields, DSV: &spec.DSVSpec{
			Delimiter: delimiterValue(r.FormValue(prefix + "_delimiter")),
			HasHeader: r.FormValue(prefix+"_has_header") == "on",
			Columns:   fieldNames(fields),
		}}
	case spec.FormatJSON:
		mode := r.FormValue(prefix + "_json_mode")
		if mode == "" {
			mode = "ndjson"
		}
		fs = spec.FormatSpec{Kind: spec.FormatJSON, Fields: fields, JSON: &spec.JSONSpec{Mode: mode}}
	case spec.FormatXML:
		fs = spec.FormatSpec{Kind: spec.FormatXML, Fields: fields, XML: &spec.XMLSpec{
			RecordElement: strings.TrimSpace(r.FormValue(prefix + "_xml_record")),
			RootElement:   strings.TrimSpace(r.FormValue(prefix + "_xml_root")),
		}}
	default:
		// default to NDJSON if unspecified
		fs = spec.FormatSpec{Kind: spec.FormatJSON, Fields: fields, JSON: &spec.JSONSpec{Mode: "ndjson"}}
	}
	// Container (input) / compression (output) ride on the format spec (TS 04
	// §4.4.8 / TS 07 §7.3): input objects may be tar.gz archives of member files;
	// output may be gzip-compressed as a single stream.
	if prefix == "input" && r.FormValue("input_container") == "on" {
		fs.Container = "targz"
		fs.MemberGlob = strings.TrimSpace(r.FormValue("input_member_glob"))
	}
	if prefix == "output" && r.FormValue("output_compress") == "on" {
		fs.Compress = "gzip"
	}
	return fs
}

// validateFormatSpec rejects configuration that cannot work at runtime — checked
// at save time, where the operator can see and fix it.
func validateFormatSpec(fs spec.FormatSpec) error {
	if fs.Container == "targz" && fs.MemberGlob != "" {
		if _, err := path.Match(fs.MemberGlob, "probe"); err != nil {
			return fmt.Errorf("member pattern %q is not a valid glob: %w", fs.MemberGlob, err)
		}
	}
	return nil
}

// parseInputFieldSpecs reads the declared input fields (decl_name/decl_type rows)
// as the format's field list — names+types that drive both DSV columns/coercion
// and the Step-4 output source dropdowns.
func parseInputFieldSpecs(r *http.Request) []spec.FieldSpec {
	names := r.Form["decl_name"]
	types := r.Form["decl_type"]
	var out []spec.FieldSpec
	for i, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		out = append(out, spec.FieldSpec{Name: n, Type: spec.ValueType(strings.TrimSpace(at(types, i)))})
	}
	return out
}

// fieldNames projects the declared field names in order (the DSV column list).
func fieldNames(fs []spec.FieldSpec) []string {
	if len(fs) == 0 {
		return nil
	}
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Name
	}
	return out
}

// parseTransform reads a TransformSpec from the repeating field_* form arrays.
// The Source column of each output row is a dropdown chosen from the declared
// input fields (Step 2); concat rows collect comma-separated Parts and a Sep.
func parseTransform(r *http.Request) spec.TransformSpec {
	if r.FormValue("passthrough") == "on" {
		return spec.TransformSpec{PassThrough: true}
	}
	outputs := r.Form["field_output"]
	kinds := r.Form["field_kind"]
	sources := r.Form["field_source"]
	consts := r.Form["field_const"]
	types := r.Form["field_type"]
	parts := r.Form["field_parts"]
	seps := r.Form["field_sep"]

	var fields []spec.FieldMap
	for i, out := range outputs {
		out = strings.TrimSpace(out)
		if out == "" {
			continue
		}
		fm := spec.FieldMap{Output: out}
		fm.Kind = spec.FieldKind(at(kinds, i))
		if fm.Kind == "" {
			fm.Kind = spec.FieldFromInput
		}
		fm.Type = spec.ValueType(at(types, i))
		switch fm.Kind {
		case spec.FieldConst:
			fm.Const = at(consts, i)
		case spec.FieldConcat:
			fm.Parts = splitCSV(at(parts, i))
			fm.Sep = at(seps, i)
		default:
			fm.Source = strings.TrimSpace(at(sources, i))
		}
		fields = append(fields, fm)
	}
	if len(fields) == 0 {
		return spec.TransformSpec{PassThrough: true}
	}
	return spec.TransformSpec{Fields: fields}
}

// parseSource reads the Step-1 source configuration into a store.Source. Two
// modes: (a) a selected datasource — the backend/root/credentials come from the
// named datasource at resolution time, so here we only capture its id, the
// per-pipeline S3 bucket, the optional distinct output datasource, disposition and
// declared fields; or (b) inline config — the legacy path with an explicit
// backend and lifecycle areas. Secrets are supplied in plaintext; the store
// encrypts.
func parseSource(r *http.Request) store.Source {
	if dsID := atoi64(r.FormValue("datasource_id")); dsID != 0 {
		src := store.Source{
			DatasourceID:       dsID,
			OutputDatasourceID: atoi64(r.FormValue("output_datasource_id")),
			Disposition:        r.FormValue("disposition"),
			PollSeconds:        atoi(r.FormValue("poll_seconds")),
			Fields:             parseDeclaredFields(r),
		}
		// Per-pipeline S3 specifics (used only when the chosen datasource is s3).
		src.S3.Bucket = strings.TrimSpace(r.FormValue("ds_bucket"))
		src.S3.CreateBucket = r.FormValue("ds_create_bucket") == "on"
		src.OutputBucket = strings.TrimSpace(r.FormValue("ds_output_bucket"))
		return src
	}

	backend := r.FormValue("src_backend")
	if backend == "" {
		backend = "posix"
	}
	src := store.Source{
		Backend:       backend,
		InputDir:      strings.TrimSpace(r.FormValue("src_input_dir")),
		Root:          strings.TrimSpace(r.FormValue("src_root")),
		DoneDir:       strings.TrimSpace(r.FormValue("done_dir")),
		InProgressDir: strings.TrimSpace(r.FormValue("in_progress_dir")),
		QuarantineDir: strings.TrimSpace(r.FormValue("quarantine_dir")),
		OutputDir:     strings.TrimSpace(r.FormValue("output_dir")),
		Disposition:   r.FormValue("disposition"),
		PollSeconds:   atoi(r.FormValue("poll_seconds")),
		Fields:        parseDeclaredFields(r),
	}
	if backend == "s3" {
		src.S3 = parseS3(r, "src_s3")
	}
	if backend == "sftp" {
		src.Remote = parseRemote(r, "sftp")
	}
	return src
}

// parseDatasource reads the datasource setup form into a store.Datasource. An S3
// datasource stores only the connection (endpoint/region/credentials) — the bucket
// is provided per-pipeline, so it is deliberately not persisted here.
func parseDatasource(r *http.Request) store.Datasource {
	kind := r.FormValue("ds_kind")
	if kind == "" {
		kind = "posix"
	}
	d := store.Datasource{
		Name:      strings.TrimSpace(r.FormValue("ds_name")),
		Kind:      kind,
		CreatedBy: "operator",
	}
	switch kind {
	case "posix":
		d.Root = strings.TrimSpace(r.FormValue("ds_root"))
	case "s3":
		s3 := parseS3(r, "ds_s3")
		s3.Bucket = ""       // connection-only: bucket is per-pipeline
		s3.CreateBucket = false
		d.S3 = s3
	}
	return d
}

// datasourceTestConfig builds a storage.Config from the datasource test form. An
// optional test bucket (ds_s3_bucket) lets the operator verify read/write against
// a specific bucket; when blank an s3 test validates the connection via
// ListBuckets.
func datasourceTestConfig(r *http.Request) storage.Config {
	kind := r.FormValue("ds_kind")
	if kind == "" {
		kind = "posix"
	}
	if kind == "s3" {
		s3 := parseS3(r, "ds_s3")
		return storage.Config{
			Backend: storage.BackendS3, Endpoint: s3.Endpoint, Region: s3.Region, Bucket: s3.Bucket,
			AccessKey: s3.AccessKey, SecretKey: s3.SecretKey, UseSSL: s3.UseSSL,
		}
	}
	return storage.Config{Backend: storage.BackendPOSIX, Root: strings.TrimSpace(r.FormValue("ds_root"))}
}

// parseDeclaredFields reads the Step-2 declared input catalogue (decl_name /
// decl_type array rows) — these populate the Step-4 source dropdowns.
func parseDeclaredFields(r *http.Request) []store.FieldDecl {
	names := r.Form["decl_name"]
	types := r.Form["decl_type"]
	var out []store.FieldDecl
	for i, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			continue
		}
		out = append(out, store.FieldDecl{Name: n, Type: strings.TrimSpace(at(types, i))})
	}
	return out
}

// parseArchive reads the optional Step-1 archiving policy; returns nil when the
// operator did not enable archiving.
func parseArchive(r *http.Request) *store.Archive {
	if r.FormValue("archive_enabled") != "on" {
		return nil
	}
	a := &store.Archive{
		Enabled:            true,
		AgeDays:            atoi(r.FormValue("archive_age_days")),
		Compression:        r.FormValue("archive_compression"),
		ScheduleSeconds:    atoi(r.FormValue("archive_schedule_seconds")),
		IncludeQuarantined: r.FormValue("archive_include_quarantined") == "on",
	}
	destBackend := r.FormValue("archive_dest_backend")
	if destBackend == "" {
		destBackend = "posix"
	}
	a.Dest = store.Dest{
		Backend: destBackend,
		Root:    strings.TrimSpace(r.FormValue("archive_dest_root")),
	}
	if destBackend == "s3" {
		a.Dest.S3 = parseS3(r, "archive_dest_s3")
	}
	if destBackend == "sftp" {
		a.Dest.Remote = parseRemote(r, "archive_dest_sftp")
	}
	return a
}

// parseS3 reads S3-compatible params under a form prefix (e.g. "src_s3").
func parseS3(r *http.Request, p string) store.S3Config {
	return store.S3Config{
		Endpoint:     strings.TrimSpace(r.FormValue(p + "_endpoint")),
		Region:       strings.TrimSpace(r.FormValue(p + "_region")),
		Bucket:       strings.TrimSpace(r.FormValue(p + "_bucket")),
		AccessKey:    strings.TrimSpace(r.FormValue(p + "_access_key")),
		SecretKey:    r.FormValue(p + "_secret_key"),
		UseSSL:       r.FormValue(p+"_use_ssl") == "on",
		CreateBucket: r.FormValue(p+"_create_bucket") == "on",
	}
}

// parseRemote reads SFTP transport params under a form prefix (e.g. "sftp").
func parseRemote(r *http.Request, p string) store.RemoteConfig {
	return store.RemoteConfig{
		Host:      strings.TrimSpace(r.FormValue(p + "_host")),
		Port:      atoi(r.FormValue(p + "_port")),
		User:      strings.TrimSpace(r.FormValue(p + "_user")),
		Password:  r.FormValue(p + "_password"),
		Key:       r.FormValue(p + "_key"),
		Path:      strings.TrimSpace(r.FormValue(p + "_path")),
		PostFetch: r.FormValue(p + "_post_fetch"),
		MovePath:  strings.TrimSpace(r.FormValue(p + "_move_path")),
		HostKey:   strings.TrimSpace(r.FormValue(p + "_host_key")),
	}
}

// atoi parses an integer form value, returning 0 on blank/invalid input.
func atoi(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0
	}
	return n
}

// atoi64 parses an int64 form value, returning 0 on blank/invalid input.
func atoi64(s string) int64 {
	n, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func at(ss []string, i int) string {
	if i < len(ss) {
		return ss[i]
	}
	return ""
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// delimiterValue maps friendly names to a delimiter rune string.
func delimiterValue(v string) string {
	switch v {
	case "", "comma":
		return ","
	case "tab":
		return "\t"
	case "pipe":
		return "|"
	case "semicolon":
		return ";"
	default:
		// take the first character as a custom delimiter
		rs := []rune(v)
		if len(rs) > 0 {
			return string(rs[0])
		}
		return ","
	}
}
