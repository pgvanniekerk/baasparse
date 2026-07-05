package httpserver

import (
	"net/http"
	"strings"

	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// parseFormatSpec reads a FormatSpec from form fields prefixed by prefix
// ("input" or "output"), e.g. input_kind, input_delimiter, input_has_header,
// input_columns, input_json_mode.
func parseFormatSpec(r *http.Request, prefix string) spec.FormatSpec {
	kind := spec.FormatKind(r.FormValue(prefix + "_kind"))
	switch kind {
	case spec.FormatDSV:
		return spec.FormatSpec{Kind: spec.FormatDSV, DSV: &spec.DSVSpec{
			Delimiter: delimiterValue(r.FormValue(prefix + "_delimiter")),
			HasHeader: r.FormValue(prefix+"_has_header") == "on",
			Columns:   splitCSV(r.FormValue(prefix + "_columns")),
		}}
	case spec.FormatJSON:
		mode := r.FormValue(prefix + "_json_mode")
		if mode == "" {
			mode = "ndjson"
		}
		return spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: mode}}
	default:
		// default to NDJSON if unspecified
		return spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}}
	}
}

// parseTransform reads a TransformSpec from the repeating field_* form arrays.
func parseTransform(r *http.Request) spec.TransformSpec {
	if r.FormValue("passthrough") == "on" {
		return spec.TransformSpec{PassThrough: true}
	}
	outputs := r.Form["field_output"]
	kinds := r.Form["field_kind"]
	sources := r.Form["field_source"]
	consts := r.Form["field_const"]
	types := r.Form["field_type"]

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
		fm.Source = strings.TrimSpace(at(sources, i))
		fm.Const = at(consts, i)
		fm.Type = spec.ValueType(at(types, i))
		fields = append(fields, fm)
	}
	if len(fields) == 0 {
		return spec.TransformSpec{PassThrough: true}
	}
	return spec.TransformSpec{Fields: fields}
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
