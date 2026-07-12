package store

import (
	"fmt"
	"strings"

	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// ValidatePipeline checks that a pipeline's destinations can actually be built from
// its input.
//
// The output fields REFERENCE the declared input fields by name, so the two are not
// independent: deleting an input field that a destination draws from leaves that
// destination emitting a column that can never have a value. The engine would not
// fail — it would happily write nulls forever — so the mistake has to be caught
// here, at save time, where the operator can still see what they broke.
//
// It is the reason editing a pipeline is not just "write the new document": the new
// input and the new outputs have to agree.
func ValidatePipeline(p Pipeline) error {
	if err := ValidateOutputs(p.Outputs); err != nil {
		return err
	}
	declared := declaredInputFields(p.Input)
	// An empty declaration is NOT a free pass. If it were, removing every input field
	// at once would slip past the very guard that refuses removing one of them — the
	// destinations would keep their mappings and emit nulls forever. It is only benign
	// when nothing references an input at all.
	for _, o := range p.Outputs {
		if o.Transform == nil || o.Transform.PassThrough {
			continue // takes whatever the record carries; nothing to dangle
		}
		for _, f := range o.Transform.Fields {
			for _, ref := range referencedInputs(f) {
				if len(declared) == 0 {
					return fmt.Errorf(
						"destination %q builds output field %q from input field %q, but the input declares no fields at all — "+
							"declare the input fields, or change that destination",
						o.Name, f.Output, ref)
				}
				if !declared[ref] {
					return fmt.Errorf(
						"destination %q builds output field %q from input field %q, which is not declared on the input — "+
							"add %q back to the input fields, or change that destination",
						o.Name, f.Output, ref, ref)
				}
			}
		}
	}
	return nil
}

// declaredInputFields is the set of field names the input declares.
func declaredInputFields(in spec.FormatSpec) map[string]bool {
	out := make(map[string]bool, len(in.Fields))
	for _, f := range in.Fields {
		if n := strings.TrimSpace(f.Name); n != "" {
			out[n] = true
		}
	}
	return out
}

// referencedInputs is every input field an output field draws from. A constant
// draws from none; a concat draws from each of its non-literal parts.
func referencedInputs(f spec.FieldMap) []string {
	switch f.Kind {
	case spec.FieldConst:
		return nil
	case spec.FieldConcat:
		var refs []string
		for _, p := range f.Parts {
			if p = strings.TrimSpace(p); p != "" && !isLiteral(p) {
				refs = append(refs, p)
			}
		}
		return refs
	default: // FieldFromInput (or unset, which the transform treats as from-input)
		src := strings.TrimSpace(f.Source)
		if src == "" {
			src = strings.TrimSpace(f.Output) // an unnamed source means "same name"
		}
		if src == "" {
			return nil
		}
		return []string{src}
	}
}

// isLiteral reports whether a concat part is a quoted literal ('-') rather than an
// input field reference.
func isLiteral(p string) bool {
	return len(p) >= 2 && p[0] == '\'' && p[len(p)-1] == '\''
}

// InputFieldsInUse maps each declared input field to the destinations that draw
// from it. The wizard uses it to explain WHY a field cannot be removed, rather than
// just refusing.
func InputFieldsInUse(p Pipeline) map[string][]string {
	use := map[string][]string{}
	for _, o := range p.Outputs {
		if o.Transform == nil || o.Transform.PassThrough {
			continue
		}
		for _, f := range o.Transform.Fields {
			for _, ref := range referencedInputs(f) {
				if !contains(use[ref], o.Name) {
					use[ref] = append(use[ref], o.Name)
				}
			}
		}
	}
	return use
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
