// Package distributor defines the domain model, persistence port, and
// application service for D_DISTRIBUTOR — a distribution target attached to a
// pipeline.
//
// A pipeline may have many distributors (D_P_UID is not unique), each binding a
// data source (DS_DATA_SOURCE) to a DistributionSpecification describing where
// to deliver records. Each distributor has a Name that is unique within its
// pipeline, optionally references a transformer (T_TRANSFORMER) with a matching
// TransformationSpecification, and carries a lifecycle Status. Mutating
// operations are recorded in the audit trail, so every distributor row
// references the audit entry that created it.
//
// The service layer validates, before persisting, that:
//   - the DistributionSpecification conforms to the data source type's
//     DST_DISTRIBUTION_SCHEMA (resolved via the linked data source);
//   - the TransformationSpecification conforms to the referenced transformer's
//     T_TRANSFORMATION_SCHEMA, and is present if and only if a transformer is
//     set;
//   - the Name is unique within the owning pipeline.
package distributor

import (
	"encoding/json"

	"github.com/google/uuid"
)

// Status is the lifecycle state of a distributor (D_STATUS). Only StatusActive
// and StatusInactive are valid; the database enforces the same set via a CHECK
// constraint on D_STATUS.
type Status string

const (
	// StatusActive marks a distributor as enabled; it will receive records.
	StatusActive Status = "ACTIVE"

	// StatusInactive marks a distributor as disabled; it is skipped during
	// execution.
	StatusInactive Status = "INACTIVE"
)

// Valid reports whether s is a recognised distributor status.
func (s Status) Valid() bool {
	return s == StatusActive || s == StatusInactive
}

// Distributor represents a row in D_DISTRIBUTOR.
type Distributor struct {
	// Uid is the database-generated primary key (D_UID).
	Uid uuid.UUID `json:"uid"`

	// PipelineUid is the FK to P_PIPELINE (D_P_UID). A pipeline may have many
	// distributors, so this is not unique on its own.
	PipelineUid uuid.UUID `json:"pipeline_uid"`

	// DataSourceUid is the FK to DS_DATA_SOURCE (D_DS_UID); the data source to
	// distribute to. Its type governs which distribution schema applies.
	DataSourceUid uuid.UUID `json:"data_source_uid"`

	// Name is the human-friendly distributor name (D_NAME). It is unique within
	// the owning pipeline.
	Name string `json:"name"`

	// DistributionSpecification is the JSONB distribution details
	// (D_DISTRIBUTION_SPECIFICATION). It must conform to the data source type's
	// DST_DISTRIBUTION_SCHEMA. Stored verbatim from the database.
	DistributionSpecification json.RawMessage `json:"distribution_specification"`

	// TransformerUid is the optional FK to T_TRANSFORMER (D_T_UID). Nil for a
	// plain pass-through distributor (no format conversion).
	TransformerUid *uuid.UUID `json:"transformer_uid"`

	// TransformationSpecification is the optional JSONB transformer
	// configuration (D_TRANSFORMATION_SPECIFICATION). It must conform to the
	// referenced transformer's T_TRANSFORMATION_SCHEMA and is nil exactly when
	// TransformerUid is nil.
	TransformationSpecification json.RawMessage `json:"transformation_specification"`

	// Status is the distributor lifecycle state (D_STATUS).
	Status Status `json:"status"`

	// TransactionAuditUid is the FK to TA_TRANSACTION_AUDIT (D_TA_UID).
	// References the audit row that recorded this distributor's creation.
	TransactionAuditUid uuid.UUID `json:"transaction_audit_uid"`
}

// CreateCommand carries the inputs required to create a new Distributor.
// New distributors always start with StatusActive; the status cannot be chosen
// at creation time.
type CreateCommand struct {
	// PipelineUid is the pipeline this distributor belongs to.
	PipelineUid uuid.UUID `json:"pipeline_uid"`

	// DataSourceUid is the data source to distribute to.
	DataSourceUid uuid.UUID `json:"data_source_uid"`

	// Name is the distributor name; it must be unique within the pipeline.
	Name string `json:"name"`

	// DistributionSpecification must conform to the data source type's
	// DST_DISTRIBUTION_SCHEMA.
	DistributionSpecification json.RawMessage `json:"distribution_specification"`

	// TransformerUid optionally references the transformer to apply. Nil for a
	// pass-through distributor.
	TransformerUid *uuid.UUID `json:"transformer_uid"`

	// TransformationSpecification must conform to the referenced transformer's
	// T_TRANSFORMATION_SCHEMA. It must be provided if and only if TransformerUid
	// is set.
	TransformationSpecification json.RawMessage `json:"transformation_specification"`

	// Username is the JWT subject claim of the caller, written to the audit
	// entry created atomically with the distributor row.
	Username string `json:"username"`

	// CorrelationID groups all audit entries produced by the same HTTP request.
	CorrelationID string `json:"correlation_id"`
}

// UpdateCommand carries the inputs required to update an existing Distributor.
// The data source, name, distribution specification, transformer,
// transformation specification, and status may be changed. The pipeline link
// (D_P_UID) is immutable once a distributor is created.
type UpdateCommand struct {
	// Uid identifies the distributor to update (D_UID).
	Uid uuid.UUID `json:"uid"`

	// DataSourceUid is the new data source to distribute to.
	DataSourceUid uuid.UUID `json:"data_source_uid"`

	// Name is the new distributor name; it must remain unique within the
	// pipeline.
	Name string `json:"name"`

	// DistributionSpecification is the new distribution details. It must conform
	// to the (possibly new) data source type's DST_DISTRIBUTION_SCHEMA.
	DistributionSpecification json.RawMessage `json:"distribution_specification"`

	// TransformerUid optionally references the transformer to apply. Nil for a
	// pass-through distributor.
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
