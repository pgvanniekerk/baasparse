package decoder

import (
	"strconv"
	"strings"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// coerceValue best-effort coerces a decoded value to a declared input type
// (spec.FieldSpec.Type). Unlike the transform's strict convert, a value that does
// not parse is left UNCHANGED — input decoding stays lenient (a bad cell surfaces
// as its original string; strict typing/validation is the transform's job). This
// gives DSV cells the same typed records JSON produces natively.
func coerceValue(v canonical.Value, t spec.ValueType) canonical.Value {
	if t == spec.TypeAuto || v.Kind == canonical.KindNull {
		return v
	}
	switch t {
	case spec.TypeString:
		return canonical.StringVal(v.String())
	case spec.TypeInteger:
		if i, err := strconv.ParseInt(strings.TrimSpace(v.String()), 10, 64); err == nil {
			return canonical.IntVal(i)
		}
	case spec.TypeNumber:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v.String()), 64); err == nil {
			return canonical.FloatVal(f)
		}
	case spec.TypeBoolean:
		if b, err := strconv.ParseBool(strings.TrimSpace(strings.ToLower(v.String()))); err == nil {
			return canonical.BoolVal(b)
		}
	}
	return v
}

// typeMap indexes declared field types by name (nil when no fields declared).
func typeMap(fields []spec.FieldSpec) map[string]spec.ValueType {
	if len(fields) == 0 {
		return nil
	}
	m := make(map[string]spec.ValueType, len(fields))
	for _, f := range fields {
		m[f.Name] = f.Type
	}
	return m
}
