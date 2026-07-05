// Package canonical defines the internal, format-independent representation of a
// decoded record (BR-DEC-009). Every stage after decode operates on
// canonical.Record, so the pipeline is format-agnostic and any input format can
// be re-encoded to any output format (BR-TRN-006).
package canonical

import (
	"encoding/json"
	"strconv"
)

// Kind is the value type carried by a field.
type Kind uint8

const (
	KindNull Kind = iota
	KindString
	KindInt
	KindFloat
	KindBool
)

// Value is a single typed field value with explicit type (BR-DEC-009). Values
// keep their type from JSON input, and are all strings from DSV input.
type Value struct {
	Kind Kind
	S    string
	I    int64
	F    float64
	B    bool
}

// NullVal, StringVal, IntVal, FloatVal, BoolVal are Value constructors.
func NullVal() Value          { return Value{Kind: KindNull} }
func StringVal(s string) Value { return Value{Kind: KindString, S: s} }
func IntVal(i int64) Value    { return Value{Kind: KindInt, I: i} }
func FloatVal(f float64) Value { return Value{Kind: KindFloat, F: f} }
func BoolVal(b bool) Value    { return Value{Kind: KindBool, B: b} }

// String renders the value as text (for DSV output, logging, previews).
func (v Value) String() string {
	switch v.Kind {
	case KindString:
		return v.S
	case KindInt:
		return strconv.FormatInt(v.I, 10)
	case KindFloat:
		return strconv.FormatFloat(v.F, 'g', -1, 64)
	case KindBool:
		return strconv.FormatBool(v.B)
	default:
		return ""
	}
}

// Interface returns the natural Go value (for JSON encoding).
func (v Value) Interface() any {
	switch v.Kind {
	case KindString:
		return v.S
	case KindInt:
		return v.I
	case KindFloat:
		return v.F
	case KindBool:
		return v.B
	default:
		return nil
	}
}

// FromJSON converts a decoded JSON value into a canonical Value. Nested
// objects/arrays are preserved as their compact JSON text (flattening is a
// downstream/transform concern in the alpha).
func FromJSON(x any) Value {
	switch t := x.(type) {
	case nil:
		return NullVal()
	case string:
		return StringVal(t)
	case bool:
		return BoolVal(t)
	case float64:
		if t == float64(int64(t)) {
			return IntVal(int64(t))
		}
		return FloatVal(t)
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return IntVal(i)
		}
		if f, err := t.Float64(); err == nil {
			return FloatVal(f)
		}
		return StringVal(t.String())
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return NullVal()
		}
		return StringVal(string(b))
	}
}

// Field is a named value; a Record is an ordered list of fields.
type Field struct {
	Name string
	Val  Value
}

// Record is a decoded record in canonical form. Seq is the 1-based decode order
// (RecordSeq, TS 02 §2.5) — the record ordinal used for lineage everywhere.
type Record struct {
	Seq    int
	Fields []Field
}

// Get returns the value of the named field and whether it was present.
func (r *Record) Get(name string) (Value, bool) {
	for i := range r.Fields {
		if r.Fields[i].Name == name {
			return r.Fields[i].Val, true
		}
	}
	return Value{}, false
}

// Set appends or replaces a field, preserving insertion order.
func (r *Record) Set(name string, v Value) {
	for i := range r.Fields {
		if r.Fields[i].Name == name {
			r.Fields[i].Val = v
			return
		}
	}
	r.Fields = append(r.Fields, Field{Name: name, Val: v})
}

// Names returns the field names in order.
func (r *Record) Names() []string {
	out := make([]string, len(r.Fields))
	for i := range r.Fields {
		out[i] = r.Fields[i].Name
	}
	return out
}
