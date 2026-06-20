// Package jsonschema provides a concrete JSON Schema validator for use with
// the datasource module's SchemaValidator function type.
//
// The implementation uses github.com/xeipuuv/gojsonschema which supports
// JSON Schema drafts 04, 06, and 07. The DST_TYPE schemas are authored as
// draft 2020-12 but only use keywords (properties, required, type, minLength,
// minimum, maximum, additionalProperties) that are backwards-compatible with
// earlier drafts, so validation works correctly in practice.
//
// When a library with full draft 2020-12 support becomes available via the
// module proxy, replace the Validate function body while keeping the same
// signature — the rest of the codebase is decoupled from this implementation.
package jsonschema

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/xeipuuv/gojsonschema"
)

// Validate checks that document conforms to schema.
// Both arguments are raw JSON bytes (e.g. json.RawMessage values).
// Returns nil when the document is valid; a descriptive error listing all
// validation failures otherwise.
//
// This function satisfies the datasource.SchemaValidator function type and
// can be passed directly to datasource.NewService.
func Validate(schema, document json.RawMessage) error {
	schemaLoader := gojsonschema.NewBytesLoader(schema)
	docLoader := gojsonschema.NewBytesLoader(document)

	compiled, err := gojsonschema.NewSchema(schemaLoader)
	if err != nil {
		// A compile error means the schema itself is malformed, which is a
		// data integrity problem (the seed migrations produced bad JSON Schema).
		return fmt.Errorf("jsonschema: compile schema: %w", err)
	}

	result, err := compiled.Validate(docLoader)
	if err != nil {
		// A runtime error during validation (e.g. the document is not valid JSON).
		return fmt.Errorf("jsonschema: validate: %w", err)
	}

	if result.Valid() {
		return nil
	}

	// Collect all validation errors into a single descriptive message.
	errs := make([]string, 0, len(result.Errors()))
	for _, e := range result.Errors() {
		errs = append(errs, e.String())
	}
	return fmt.Errorf("jsonschema: %s", strings.Join(errs, "; "))
}
