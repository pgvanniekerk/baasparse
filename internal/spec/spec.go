// Package spec holds the declarative, JSON-serialisable configuration shapes that
// describe how a pipeline decodes input, transforms records, and encodes output.
//
// These are the alpha-scope realisations of the BRS "configuration over code"
// intent (BR-CFG-001/002, BR-TRN-001): a Format Definition (FD_SPEC), a
// Transform Rule Set (TR_SPEC) and a Destination (DS_SPEC) are all data, stored
// as JSONB in PostgreSQL and edited through the GUI. Only DSV and JSON are wired
// in the alpha; the shape leaves room for the other formats.
package spec

// FormatKind enumerates the input/output encodings wired in the alpha.
type FormatKind string

const (
	FormatDSV  FormatKind = "dsv"
	FormatJSON FormatKind = "json"
)

// FormatSpec describes a file structure (a Format Definition, FD_SPEC). Exactly
// one of DSV/JSON is set, selected by Kind.
type FormatSpec struct {
	Kind FormatKind `json:"kind"`
	DSV  *DSVSpec   `json:"dsv,omitempty"`
	JSON *JSONSpec  `json:"json,omitempty"`
}

// DSVSpec configures a delimiter-separated format (CSV/TSV/pipe/...), per
// BR-DEC-003 (input) and BR-DST-002 (output).
type DSVSpec struct {
	Delimiter string   `json:"delimiter"` // single rune, e.g. "," "\t" "|"
	Quote     string   `json:"quote"`     // single rune, default '"'
	HasHeader bool     `json:"hasHeader"` // first row carries field names
	Columns   []string `json:"columns"`   // explicit names when HasHeader is false / for output order
}

// JSONSpec configures a JSON format, per BR-DEC-002 (input) and BR-DST-002 (output).
type JSONSpec struct {
	Mode string `json:"mode"` // "ndjson" (record-per-line) or "array"
}

// FieldKind selects how an output field's value is produced.
type FieldKind string

const (
	FieldFromInput FieldKind = "field"  // copy/rename/convert an input field
	FieldConst     FieldKind = "const"  // a literal constant
	FieldConcat    FieldKind = "concat" // join input fields and/or literals
)

// ValueType is an optional target type for an output field (BR-TRN-004).
type ValueType string

const (
	TypeString  ValueType = "string"
	TypeInteger ValueType = "integer"
	TypeNumber  ValueType = "number"
	TypeBoolean ValueType = "boolean"
	TypeAuto    ValueType = "" // keep the source value's own type
)

// FieldMap is one output-field definition in the transform modeller: it names
// the output field and says where its value comes from (BR-TRN-002/003/004/005).
type FieldMap struct {
	Output string    `json:"output"`          // output field name (required)
	Kind   FieldKind `json:"kind"`            // field | const | concat
	Source string    `json:"source,omitempty"` // input field (Kind=field)
	Const  string    `json:"const,omitempty"`  // literal (Kind=const)
	Parts  []string  `json:"parts,omitempty"`  // input fields / "'literal'" (Kind=concat)
	Sep    string    `json:"sep,omitempty"`    // separator for concat
	Type   ValueType `json:"type,omitempty"`   // optional convert
}

// TransformSpec is a Transform Rule Set (TR_SPEC): an ordered list of output
// field definitions. When PassThrough is set and Fields is empty, every input
// field is copied through unchanged (the natural default for a pure DSV<->JSON
// re-format).
type TransformSpec struct {
	PassThrough bool       `json:"passThrough"`
	Fields      []FieldMap `json:"fields,omitempty"`
}

// DestinationSpec is a Destination (DS_SPEC): where and in what format output is
// written (BR-DST-001/002).
type DestinationSpec struct {
	Format           FormatSpec `json:"format"`
	Dir              string     `json:"dir"`
	FilenameTemplate string     `json:"filenameTemplate,omitempty"` // {name},{ext},{seq}
}
