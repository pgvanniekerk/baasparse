// Package collector defines the domain model, persistence port, and application
// service for C_COLLECTOR — the collection configuration for a pipeline.
//
// Exactly one collector exists per pipeline (P_PIPELINE), enforced by a unique
// constraint on C_P_UID. A collector binds a data source (DS_DATA_SOURCE) to a
// CollectionSpecification describing what to collect, optionally references a
// transformer (T_TRANSFORMER) with a matching TransformationSpecification, and
// carries a lifecycle Status. Mutating operations are recorded in the audit
// trail, so every collector row references the audit entry that created it.
//
// The service layer validates, before persisting, that:
//   - the CollectionSpecification conforms to the data source type's
//     DST_COLLECTION_SCHEMA (resolved via the linked data source);
//   - the TransformationSpecification conforms to the referenced transformer's
//     T_TRANSFORMATION_SCHEMA, and is present if and only if a transformer is
//     set.
package collector

import (
	"encoding/json"

	"github.com/google/uuid"
)

// Status is the lifecycle state of a collector (C_STATUS). Only StatusActive
// and StatusInactive are valid; the database enforces the same set via a CHECK
// constraint on C_STATUS.
type Status string

const (
	// StatusActive marks a collector as enabled.
	StatusActive Status = "ACTIVE"

	// StatusInactive marks a collector as disabled.
	StatusInactive Status = "INACTIVE"
)

// Valid reports whether s is a recognised collector status.
func (s Status) Valid() bool {
	return s == StatusActive || s == StatusInactive
}

// Collector represents a row in C_COLLECTOR.
type Collector struct {
	// Uid is the database-generated primary key (C_UID).
	Uid uuid.UUID `json:"uid"`

	// PipelineUid is the FK to P_PIPELINE (C_P_UID). It is unique: a pipeline
	// has at most one collector.
	PipelineUid uuid.UUID `json:"pipeline_uid"`

	// DataSourceUid is the FK to DS_DATA_SOURCE (C_DS_UID); the data source to
	// collect from. Its type governs which collection schema applies.
	DataSourceUid uuid.UUID `json:"data_source_uid"`

	// CollectionSpecification is the JSONB collection details
	// (C_COLLECTION_SPECIFICATION). It must conform to the data source type's
	// DST_COLLECTION_SCHEMA. Stored verbatim from the database.
	CollectionSpecification json.RawMessage `json:"collection_specification"`

	// TransformerUid is the optional FK to T_TRANSFORMER (C_T_UID). Nil for a
	// plain pass-through collector (no format conversion).
	TransformerUid *uuid.UUID `json:"transformer_uid"`

	// TransformationSpecification is the optional JSONB transformer
	// configuration (C_TRANSFORMATION_SPECIFICATION). It must conform to the
	// referenced transformer's T_TRANSFORMATION_SCHEMA and is nil exactly when
	// TransformerUid is nil.
	TransformationSpecification json.RawMessage `json:"transformation_specification"`

	// Status is the collector lifecycle state (C_STATUS).
	Status Status `json:"status"`

	// TransactionAuditUid is the FK to TA_TRANSACTION_AUDIT (C_TA_UID).
	// References the audit row that recorded this collector's creation.
	TransactionAuditUid uuid.UUID `json:"transaction_audit_uid"`
}

// CreateCommand carries the inputs required to create a new Collector.
// New collectors always start with StatusActive; the status cannot be chosen
// at creation time.
type CreateCommand struct {
	// PipelineUid is the pipeline this collector belongs to (must be unique
	// across collectors).
	PipelineUid uuid.UUID `json:"pipeline_uid"`

	// DataSourceUid is the data source to collect from.
	DataSourceUid uuid.UUID `json:"data_source_uid"`

	// CollectionSpecification must conform to the data source type's
	// DST_COLLECTION_SCHEMA.
	CollectionSpecification json.RawMessage `json:"collection_specification"`

	// TransformerUid optionally references the transformer to apply. Nil for a
	// pass-through collector.
	TransformerUid *uuid.UUID `json:"transformer_uid"`

	// TransformationSpecification must conform to the referenced transformer's
	// T_TRANSFORMATION_SCHEMA. It must be provided if and only if TransformerUid
	// is set.
	TransformationSpecification json.RawMessage `json:"transformation_specification"`

	// Username is the JWT subject claim of the caller, written to the audit
	// entry created atomically with the collector row.
	Username string `json:"username"`

	// CorrelationID groups all audit entries produced by the same HTTP request.
	CorrelationID string `json:"correlation_id"`
}

// UpdateCommand carries the inputs required to update an existing Collector.
// The data source, collection specification, transformer, transformation
// specification, and status may be changed. The pipeline link (C_P_UID) is
// immutable once a collector is created.
type UpdateCommand struct {
	// Uid identifies the collector to update (C_UID).
	Uid uuid.UUID `json:"uid"`

	// DataSourceUid is the new data source to collect from.
	DataSourceUid uuid.UUID `json:"data_source_uid"`

	// CollectionSpecification is the new collection details. It must conform to
	// the (possibly new) data source type's DST_COLLECTION_SCHEMA.
	CollectionSpecification json.RawMessage `json:"collection_specification"`

	// TransformerUid optionally references the transformer to apply. Nil for a
	// pass-through collector.
	TransformerUid *uuid.UUID `json:"transformer_uid"`

	// TransformationSpecification must conform to the referenced transformer's
	// T_TRANSFORMATION_SCHEMA. It must be provided if and only if TransformerUid
	// is set.
	TransformationSpecification json.RawMessage `json:"transformation_specification"`

	// Status is the new lifecycle status. Must be a valid Status.
	Status Status `json:"status"`

	// Username is the JWT subject claim of the caller, written to the audit entry.
	Username string `json:"username"`

	// CorrelationID groups all audit entries produced by the same HTTP request.
	CorrelationID string `json:"correlation_id"`
}
