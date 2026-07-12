package store

import (
	"strings"
	"testing"

	"github.com/pgvanniekerk/baasparse/internal/spec"
)

func pipeWith(inputs []string, tr spec.TransformSpec) Pipeline {
	fs := make([]spec.FieldSpec, len(inputs))
	for i, n := range inputs {
		fs[i] = spec.FieldSpec{Name: n}
	}
	return Pipeline{
		Name:    "p",
		Input:   spec.FormatSpec{Kind: spec.FormatDSV, Fields: fs},
		Outputs: []Output{{Name: "billing", Transform: &tr}},
	}
}

// TestValidatePipeline_DanglingInputReference is the rule the operator asked for:
// removing an input field that a destination draws from must be an ERROR. The engine
// would not fail on it — it would emit that column as null forever — so nothing else
// would ever tell them.
func TestValidatePipeline_DanglingInputReference(t *testing.T) {
	tr := spec.TransformSpec{Fields: []spec.FieldMap{
		{Output: "subscriber", Kind: spec.FieldFromInput, Source: "msisdn"},
		{Output: "secs", Kind: spec.FieldFromInput, Source: "duration"},
	}}
	if err := ValidatePipeline(pipeWith([]string{"msisdn", "duration"}, tr)); err != nil {
		t.Fatalf("valid pipeline rejected: %v", err)
	}
	// "duration" is dropped from the input, but billing still draws from it.
	err := ValidatePipeline(pipeWith([]string{"msisdn"}, tr))
	if err == nil {
		t.Fatal("removing an input field a destination uses must be refused")
	}
	for _, want := range []string{"billing", "secs", "duration"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error must name the destination, the output field and the missing input; got: %v", err)
		}
	}
}

// TestValidatePipeline_ConcatPartsAndLiterals: a concat references each of its
// non-literal parts, and a quoted literal is not an input field.
func TestValidatePipeline_ConcatPartsAndLiterals(t *testing.T) {
	tr := spec.TransformSpec{Fields: []spec.FieldMap{
		{Output: "route", Kind: spec.FieldConcat, Parts: []string{"cell", "'-'", "msisdn"}},
	}}
	if err := ValidatePipeline(pipeWith([]string{"cell", "msisdn"}, tr)); err != nil {
		t.Fatalf("literal '-' must not be treated as an input field: %v", err)
	}
	if err := ValidatePipeline(pipeWith([]string{"msisdn"}, tr)); err == nil {
		t.Fatal("a concat part that is not declared must be refused")
	}
}

// TestValidatePipeline_ConstantsAndPassThroughDoNotDangle.
func TestValidatePipeline_ConstantsAndPassThroughDoNotDangle(t *testing.T) {
	konst := spec.TransformSpec{Fields: []spec.FieldMap{
		{Output: "feed", Kind: spec.FieldConst, Const: "RA"},
	}}
	if err := ValidatePipeline(pipeWith(nil, konst)); err != nil {
		t.Fatalf("a constant references no input: %v", err)
	}
	pass := spec.TransformSpec{PassThrough: true}
	if err := ValidatePipeline(pipeWith([]string{"a"}, pass)); err != nil {
		t.Fatalf("pass-through references nothing specific: %v", err)
	}
}

// TestInputFieldsInUse explains WHY a field cannot be removed, rather than just
// refusing — the wizard shows this next to the field.
func TestInputFieldsInUse(t *testing.T) {
	tr := spec.TransformSpec{Fields: []spec.FieldMap{
		{Output: "subscriber", Kind: spec.FieldFromInput, Source: "msisdn"},
		{Output: "route", Kind: spec.FieldConcat, Parts: []string{"cell", "'-'", "msisdn"}},
	}}
	use := InputFieldsInUse(pipeWith([]string{"msisdn", "cell"}, tr))
	if len(use["msisdn"]) != 1 || use["msisdn"][0] != "billing" {
		t.Fatalf("msisdn should be reported in use by billing: %v", use)
	}
	if len(use["cell"]) != 1 {
		t.Fatalf("a concat part counts as in use: %v", use)
	}
	// A destination is listed once even when it draws on a field twice.
	if len(use["msisdn"]) != 1 {
		t.Fatalf("destination listed more than once: %v", use)
	}
}
