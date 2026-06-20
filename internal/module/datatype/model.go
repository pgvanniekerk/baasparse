// Package datatype defines the domain model and persistence port for
// DT_DATA_TYPE — a system-controlled catalog of supported data formats /
// encodings such as "JSON", "DSV", and "MESSAGEPACK".
//
// DT_DATA_TYPE rows are seeded at migration time and are read-only from the
// end-user's perspective. The Repository interface therefore exposes only
// query methods; no create/update/delete operations are supported.
package datatype

import "github.com/google/uuid"

// DataType represents a row in DT_DATA_TYPE.
type DataType struct {
	// Uid is the database-generated primary key (DT_UID).
	Uid uuid.UUID `json:"uid"`

	// Code is the short, machine-readable, globally unique identifier for the
	// data type, e.g. "JSON", "DSV", or "MESSAGEPACK" (DT_CODE). Used as the
	// stable lookup key in most service-layer calls.
	Code string `json:"code"`
}
