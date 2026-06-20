// Package pipeline defines the domain model, persistence port, and application
// service for P_PIPELINE — a named, top-level ETL pipeline configuration.
//
// A pipeline carries a globally unique name, an optional description, and a
// lifecycle Status (ACTIVE/INACTIVE). Mutating operations are recorded in the
// audit trail, so every pipeline row references the audit entry that created
// it. The service layer enforces name uniqueness and status validity before
// persisting.
package pipeline

import "github.com/google/uuid"

// Status is the lifecycle state of a pipeline (P_STATUS). Only StatusActive
// and StatusInactive are valid; the database enforces the same set via a CHECK
// constraint on P_STATUS.
type Status string

const (
	// StatusActive marks a pipeline as enabled and eligible for execution.
	StatusActive Status = "ACTIVE"

	// StatusInactive marks a pipeline as disabled.
	StatusInactive Status = "INACTIVE"
)

// Valid reports whether s is a recognised pipeline status.
func (s Status) Valid() bool {
	return s == StatusActive || s == StatusInactive
}

// Pipeline represents a row in P_PIPELINE.
type Pipeline struct {
	// Uid is the database-generated primary key (P_UID).
	Uid uuid.UUID `json:"uid"`

	// Name is the globally unique, human-friendly pipeline name (P_NAME).
	Name string `json:"name"`

	// Description is an optional free-text description of the pipeline's
	// purpose (P_DESCRIPTION). Nil when the column is NULL.
	Description *string `json:"description"`

	// Status is the pipeline lifecycle state (P_STATUS).
	Status Status `json:"status"`

	// TransactionAuditUid is the FK to TA_TRANSACTION_AUDIT (P_TA_UID).
	// References the audit row that recorded this pipeline's creation.
	TransactionAuditUid uuid.UUID `json:"transaction_audit_uid"`
}

// CreateCommand carries the inputs required to create a new Pipeline.
// New pipelines always start with StatusActive; the status cannot be chosen at
// creation time.
type CreateCommand struct {
	// Name is the globally unique name for the new pipeline.
	Name string `json:"name"`

	// Description is the optional description of the pipeline. Nil leaves
	// P_DESCRIPTION NULL.
	Description *string `json:"description"`

	// Username is the JWT subject claim of the caller, written to the audit
	// entry created atomically with the pipeline row.
	Username string `json:"username"`

	// CorrelationID groups all audit entries produced by the same HTTP request.
	CorrelationID string `json:"correlation_id"`
}

// UpdateCommand carries the inputs required to update an existing Pipeline.
// Name, Description, and Status may all be changed.
type UpdateCommand struct {
	// Uid identifies the pipeline to update (P_UID).
	Uid uuid.UUID `json:"uid"`

	// Name is the new globally unique name for the pipeline.
	Name string `json:"name"`

	// Description is the new optional description. Nil sets P_DESCRIPTION NULL.
	Description *string `json:"description"`

	// Status is the new lifecycle status. Must be a valid Status.
	Status Status `json:"status"`

	// Username is the JWT subject claim of the caller, written to the audit entry.
	Username string `json:"username"`

	// CorrelationID groups all audit entries produced by the same HTTP request.
	CorrelationID string `json:"correlation_id"`
}
