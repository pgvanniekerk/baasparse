// Package datasource defines the domain model, persistence port, and
// application service for DS_DATA_SOURCE — a user-configured connection to a
// data source of a specific DataSourceType.
//
// The service layer is responsible for validating the ConnectionSpecification
// JSON against the schema defined by the referenced DataSourceType before
// persisting the record.
package datasource

import (
	"encoding/json"

	"github.com/google/uuid"
)

// DataSource represents a row in DS_DATA_SOURCE.
// It binds a DataSourceType (which governs which JSON schemas apply) to a
// user-supplied connection specification that satisfies the type's
// ConnectionSchema.
type DataSource struct {
	// Uid is the database-generated primary key (DS_UID).
	Uid uuid.UUID `json:"uid"`

	// DataSourceTypeUid is the FK to DST_TYPE (DS_DST_UID).
	// Determines which connection, collection, and distribution schemas apply.
	DataSourceTypeUid uuid.UUID `json:"data_source_type_uid"`

	// Name is the unique human-readable identifier for this data source
	// (DS_NAME).
	Name string `json:"name"`

	// ConnectionSpecification is the JSONB object holding the connection
	// details (DS_CONNECTION_SPECIFICATION). It must conform to the referenced
	// DataSourceType's ConnectionSchema; the service validates this before
	// persisting.
	ConnectionSpecification json.RawMessage `json:"connection_specification"`

	// TransactionAuditUid is the FK to TA_TRANSACTION_AUDIT (DS_TA_UID).
	// References the audit row that recorded this data source's creation.
	TransactionAuditUid uuid.UUID `json:"transaction_audit_uid"`
}

// UpdateCommand carries the inputs required to update an existing DataSource.
// Only Name and ConnectionSpecification may be changed; the DataSourceType
// is immutable once a data source is created.
type UpdateCommand struct {
	// Uid identifies the data source to update (DS_UID).
	Uid uuid.UUID `json:"uid"`

	// Name is the new unique name for the data source.
	Name string `json:"name"`

	// ConnectionSpecification is the new connection details JSON. It must
	// still conform to the DataSourceType's ConnectionSchema.
	ConnectionSpecification json.RawMessage `json:"connection_specification"`

	// Username is the JWT subject claim of the caller, written to the audit entry.
	Username string `json:"username"`

	// CorrelationID groups all audit entries produced by the same HTTP request.
	CorrelationID string `json:"correlation_id"`
}

// CreateCommand carries the inputs required to create a new DataSource.
// The service validates ConnectionSpecification against the DataSourceType's
// schema before persisting.
type CreateCommand struct {
	// DataSourceTypeUid identifies the DataSourceType whose ConnectionSchema
	// the ConnectionSpecification will be validated against.
	DataSourceTypeUid uuid.UUID `json:"data_source_type_uid"`

	// Name is the unique human-readable identifier for the new data source.
	Name string `json:"name"`

	// ConnectionSpecification is the JSON object describing how to connect.
	// It must satisfy the referenced DataSourceType's ConnectionSchema.
	ConnectionSpecification json.RawMessage `json:"connection_specification"`

	// Username is the JWT subject claim of the caller, written to the audit
	// entry that is created atomically with the data source row.
	Username string `json:"username"`

	// CorrelationID groups all audit entries produced by the same HTTP request.
	CorrelationID string `json:"correlation_id"`
}
