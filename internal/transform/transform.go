// Package transform applies a declarative Transform Rule Set to canonical records
// (BR-TRN-001): projection/rename (BR-TRN-002/003), type conversion
// (BR-TRN-004), derived/constant/concatenated fields (BR-TRN-005). It is pure
// and deterministic (BR-TRN-007).
package transform

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/pgvanniekerk/baasparse/internal/canonical"
	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// Engine applies one TransformSpec.
type Engine struct {
	spec spec.TransformSpec
}

// New builds a transform engine from a spec.
func New(s spec.TransformSpec) *Engine { return &Engine{spec: s} }

// Columns returns the ordered output column names this transform produces. For a
// passthrough transform the columns are taken from the sample record.
func (e *Engine) Columns(sample *canonical.Record) []string {
	if e.isPassThrough() {
		if sample != nil {
			return sample.Names()
		}
		return nil
	}
	cols := make([]string, 0, len(e.spec.Fields))
	for _, f := range e.spec.Fields {
		cols = append(cols, f.Output)
	}
	return cols
}

// Apply transforms one record, returning a new record. Errors carry a machine
// reason code prefix (TRN_) for suspense routing (TS 02 §2.3.1).
func (e *Engine) Apply(in *canonical.Record) (*canonical.Record, error) {
	if e.isPassThrough() {
		// Pass the record straight through — no copy. The pipeline consumes each
		// record (encodes it) synchronously before the decoder produces the next, so
		// there is no aliasing hazard, and this avoids a Record + Fields allocation
		// per row on the common DSV<->JSON re-format path.
		return in, nil
	}
	out := &canonical.Record{Seq: in.Seq, Fields: make([]canonical.Field, 0, len(e.spec.Fields))}
	for _, f := range e.spec.Fields {
		v, err := e.evalField(in, f)
		if err != nil {
			return nil, err
		}
		out.Set(f.Output, v)
	}
	return out, nil
}

func (e *Engine) isPassThrough() bool {
	return e.spec.PassThrough && len(e.spec.Fields) == 0
}

func (e *Engine) evalField(in *canonical.Record, f spec.FieldMap) (canonical.Value, error) {
	var v canonical.Value
	switch f.Kind {
	case spec.FieldConst:
		v = canonical.StringVal(f.Const)
	case spec.FieldConcat:
		v = canonical.StringVal(concat(in, f.Parts, f.Sep))
	case spec.FieldFromInput, "":
		src := f.Source
		if src == "" {
			src = f.Output
		}
		got, ok := in.Get(src)
		if !ok {
			v = canonical.NullVal()
		} else {
			v = got
		}
	default:
		return canonical.Value{}, fmt.Errorf("TRN_EXPR_ERROR: unknown field kind %q for %q", f.Kind, f.Output)
	}
	return convert(v, f.Type, f.Output)
}

// concat joins parts; a part wrapped in single quotes is a literal, otherwise it
// is an input field name (missing fields contribute empty text).
func concat(in *canonical.Record, parts []string, sep string) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			b.WriteString(sep)
		}
		if len(p) >= 2 && strings.HasPrefix(p, "'") && strings.HasSuffix(p, "'") {
			b.WriteString(p[1 : len(p)-1])
			continue
		}
		if v, ok := in.Get(p); ok {
			b.WriteString(v.String())
		}
	}
	return b.String()
}

// convert coerces a value to a target type (BR-TRN-004). Null passes through.
func convert(v canonical.Value, t spec.ValueType, field string) (canonical.Value, error) {
	if t == spec.TypeAuto || v.Kind == canonical.KindNull {
		return v, nil
	}
	switch t {
	case spec.TypeString:
		return canonical.StringVal(v.String()), nil
	case spec.TypeInteger:
		i, err := strconv.ParseInt(strings.TrimSpace(v.String()), 10, 64)
		if err != nil {
			return canonical.Value{}, fmt.Errorf("TRN_EXPR_ERROR: field %q not an integer: %q", field, v.String())
		}
		return canonical.IntVal(i), nil
	case spec.TypeNumber:
		f, err := strconv.ParseFloat(strings.TrimSpace(v.String()), 64)
		if err != nil {
			return canonical.Value{}, fmt.Errorf("TRN_EXPR_ERROR: field %q not a number: %q", field, v.String())
		}
		return canonical.FloatVal(f), nil
	case spec.TypeBoolean:
		b, err := strconv.ParseBool(strings.TrimSpace(strings.ToLower(v.String())))
		if err != nil {
			return canonical.Value{}, fmt.Errorf("TRN_EXPR_ERROR: field %q not a boolean: %q", field, v.String())
		}
		return canonical.BoolVal(b), nil
	default:
		return v, nil
	}
}
