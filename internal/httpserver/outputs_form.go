package httpserver

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// destForm is one destination as the wizard posts it: a single JSON blob per row.
//
// It used to be fifteen parallel form arrays read by index, which was a standing
// trap — a row that omitted one value (an unchecked checkbox posts nothing) would
// shift every later row and silently hand one destination another's format. One
// self-describing blob per row makes that class of bug impossible, and it is the
// only shape that can carry a destination's own field list, which is variable
// length and cannot be flattened into parallel arrays at all.
type destForm struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"` // file | rdbms
	DatasourceID int64  `json:"datasourceID"`
	Bucket       string `json:"bucket"`
	Dir          string `json:"dir"`

	Format struct {
		Kind      string `json:"kind"` // dsv | json | xml
		Delimiter string `json:"delimiter"`
		HasHeader bool   `json:"hasHeader"`
		JSONMode  string `json:"jsonMode"`
		XMLRoot   string `json:"xmlRoot"`
		XMLRecord string `json:"xmlRecord"`
	} `json:"format"`
	Compress bool `json:"compress"`

	// PassThrough + Fields are THIS destination's output structure.
	PassThrough bool `json:"passThrough"`
	Fields      []struct {
		Output string   `json:"output"`
		Kind   string   `json:"kind"` // field | const | concat
		Source string   `json:"source"`
		Const  string   `json:"const"`
		Parts  []string `json:"parts"`
		Sep    string   `json:"sep"`
		Type   string   `json:"type"`
	} `json:"fields"`

	Table  string `json:"table"`
	DBMode string `json:"dbMode"`
}

// parseOutputs reads the wizard's destination rows. Each row posts one hidden
// "dest_json" input describing the whole destination, including its own output
// structure — destinations no longer merely subset a shared field list, they each
// define what they carry.
//
// A form with no rows at all (the preview/upload paths) returns nil, and the caller
// keeps its single legacy output.
func parseOutputs(r *http.Request) ([]store.Output, error) {
	blobs := r.Form["dest_json"]
	if len(blobs) == 0 {
		return nil, nil
	}
	out := make([]store.Output, 0, len(blobs))
	for i, b := range blobs {
		var df destForm
		if err := json.Unmarshal([]byte(b), &df); err != nil {
			return nil, fmt.Errorf("destination %d is malformed: %w", i+1, err)
		}
		name := strings.TrimSpace(df.Name)
		if name == "" {
			return nil, fmt.Errorf("destination %d has no name", i+1)
		}
		kind := df.Kind
		if kind == "" {
			kind = store.OutputFile
		}
		o := store.Output{
			Name:         name,
			Kind:         kind,
			DatasourceID: df.DatasourceID,
			Bucket:       strings.TrimSpace(df.Bucket),
			Dir:          strings.TrimSpace(df.Dir),
		}
		tr := destTransformSpec(df)
		o.Transform = &tr

		if kind == store.OutputRDBMS {
			o.RDBMS = &store.RDBMSTarget{Table: strings.TrimSpace(df.Table), Mode: df.DBMode}
			if cols := transformColumns(tr); len(cols) > 0 {
				o.RDBMS.Columns = map[string]string{}
				for _, c := range cols {
					o.RDBMS.Columns[c] = c // same-named column until a mapping UI exists
				}
			}
			out = append(out, o)
			continue
		}

		o.Format = destFormatSpec(df)
		// Compression is a DELIVERY property, not a format one — it decides how the
		// object lands, which is why it lives in the destination's Delivery section
		// and why an RDBMS destination has none.
		if df.Compress {
			o.Format.Compress = "gzip"
		}
		// This destination's own fields ARE its columns: pin them so the encoder emits
		// exactly those, in that order, instead of inferring from the first record.
		if cols := transformColumns(tr); len(cols) > 0 {
			o.Format.Columns = cols
			if o.Format.DSV != nil {
				o.Format.DSV.Columns = cols
			}
		}
		out = append(out, o)
	}
	return out, nil
}

// destTransformSpec builds one destination's output structure. A destination that
// takes every input field unchanged stays PassThrough — the cheapest path through
// the engine — rather than being expanded into an explicit identity mapping.
func destTransformSpec(df destForm) spec.TransformSpec {
	if df.PassThrough || len(df.Fields) == 0 {
		return spec.TransformSpec{PassThrough: true}
	}
	fields := make([]spec.FieldMap, 0, len(df.Fields))
	for _, f := range df.Fields {
		name := strings.TrimSpace(f.Output)
		if name == "" {
			continue
		}
		fm := spec.FieldMap{
			Output: name,
			Kind:   spec.FieldKind(f.Kind),
			Source: strings.TrimSpace(f.Source),
			Const:  f.Const,
			Sep:    f.Sep,
			Type:   spec.ValueType(f.Type),
		}
		if fm.Kind == "" {
			fm.Kind = spec.FieldFromInput
		}
		for _, p := range f.Parts {
			if p = strings.TrimSpace(p); p != "" {
				fm.Parts = append(fm.Parts, p)
			}
		}
		fields = append(fields, fm)
	}
	if len(fields) == 0 {
		return spec.TransformSpec{PassThrough: true}
	}
	return spec.TransformSpec{Fields: fields}
}

// transformColumns is the output column list a transform produces, in order.
// Empty for pass-through (the columns are whatever the record carries).
func transformColumns(tr spec.TransformSpec) []string {
	if tr.PassThrough || len(tr.Fields) == 0 {
		return nil
	}
	cols := make([]string, 0, len(tr.Fields))
	for _, f := range tr.Fields {
		cols = append(cols, f.Output)
	}
	return cols
}

func destFormatSpec(df destForm) spec.FormatSpec {
	switch spec.FormatKind(df.Format.Kind) {
	case spec.FormatDSV:
		return spec.FormatSpec{Kind: spec.FormatDSV, DSV: &spec.DSVSpec{
			Delimiter: delimiterValue(df.Format.Delimiter),
			HasHeader: df.Format.HasHeader,
		}}
	case spec.FormatXML:
		return spec.FormatSpec{Kind: spec.FormatXML, XML: &spec.XMLSpec{
			RootElement:   strings.TrimSpace(df.Format.XMLRoot),
			RecordElement: strings.TrimSpace(df.Format.XMLRecord),
		}}
	default:
		mode := df.Format.JSONMode
		if mode == "" {
			mode = "ndjson"
		}
		return spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: mode}}
	}
}

// parseBatch reads the consolidation settings. These are PIPELINE-level and cannot
// be per-destination: a batch is one frozen set of input files (the unit of
// exactly-once), and every destination receives that same set. Two destinations
// grouping files differently would deliver the overlap twice.
func parseBatch(r *http.Request) store.BatchSpec {
	b := store.BatchSpec{Enabled: r.FormValue("batch_enabled") == "on"}
	if !b.Enabled {
		return b
	}
	b.MaxFiles = int(atoi64(r.FormValue("batch_max_files")))
	b.MaxAgeSeconds = int(atoi64(r.FormValue("batch_max_age_seconds")))
	if mb := atoi64(r.FormValue("batch_max_mb")); mb > 0 {
		b.MaxBytes = mb * 1024 * 1024
	}
	return b
}

// hasDestinationRows reports whether the form posted any destination rows.
func hasDestinationRows(r *http.Request) bool { return len(r.Form["dest_json"]) > 0 }
