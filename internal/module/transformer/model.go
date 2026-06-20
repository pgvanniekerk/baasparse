// Package transformer defines the domain model and persistence port for
// T_TRANSFORMER — a system-controlled registry of available data
// transformations. Each row describes how to convert data from one
// DT_DATA_TYPE (the "from" type) to another (the "to" type).
//
// T_TRANSFORMER rows are seeded at migration time and are read-only from the
// end-user's perspective. The Repository interface therefore exposes only
// query methods; no create/update/delete operations are supported.
package transformer

import (
	"encoding/json"

	"github.com/google/uuid"
)

// Transformer represents a row in T_TRANSFORMER.
// A transformer is uniquely identified by its (FromDataTypeUid, ToDataTypeUid)
// pair — at most one transformer exists per conversion direction.
type Transformer struct {
	// Uid is the database-generated primary key (T_UID).
	Uid uuid.UUID `json:"uid"`

	// FromDataTypeUid is the FK to DT_DATA_TYPE identifying the source data
	// type this transformer reads from (T_FROM_DT_UID).
	FromDataTypeUid uuid.UUID `json:"from_data_type_uid"`

	// ToDataTypeUid is the FK to DT_DATA_TYPE identifying the target data type
	// this transformer produces (T_TO_DT_UID).
	ToDataTypeUid uuid.UUID `json:"to_data_type_uid"`

	// TransformationSchema is a JSON Schema (draft 2020-12) document describing
	// the configuration options available for this transformation, e.g. field
	// mappings or delimiter settings (T_TRANSFORMATION_SCHEMA). Stored verbatim
	// from the database; validate with a JSON Schema library.
	TransformationSchema json.RawMessage `json:"transformation_schema"`

	// Description is an optional human-friendly explanation of what this
	// transformer does (T_DESCRIPTION). Nil when the column is NULL.
	Description *string `json:"description"`
}
