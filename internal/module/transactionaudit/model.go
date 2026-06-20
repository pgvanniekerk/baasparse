// Package transactionaudit defines the domain model and persistence port for
// the TA_TRANSACTION_AUDIT table. It records an immutable audit trail of every
// mutating control-plane operation performed by an authenticated caller.
//
// Rules:
//   - Audit rows are never updated or deleted.
//   - Username is always the JWT subject claim of the authenticated caller.
//   - CorrelationID groups all audit rows produced by a single logical request.
package transactionaudit

import (
	"time"

	"github.com/google/uuid"
)

// Transaction is a single immutable row in TA_TRANSACTION_AUDIT.
// It captures who performed an action, what kind of action it was, a
// structured payload describing the change, and a correlation ID that ties
// together all audit rows written during the same logical HTTP request.
type Transaction struct {
	// TransactionUid is the database-generated primary key (TA_UID).
	TransactionUid uuid.UUID `json:"transaction_uid"`

	// TimestampTz is the database-assigned timestamp of the event (TA_TIMESTAMPTZ).
	TimestampTz time.Time `json:"timestamp_tz"`

	// Username is the JWT subject claim of the authenticated caller (TA_USERNAME).
	// It is always required — see the module package doc.
	Username string `json:"username"`

	// TransactionType is a machine-readable category for the operation, e.g.
	// "CreateDataSource" or "UpdatePipeline" (TA_TRANSACTION_TYPE).
	TransactionType string `json:"transaction_type"`

	// CorrelationID links all audit rows produced by a single logical operation,
	// typically one inbound HTTP request (TA_CORRELATION_ID).
	CorrelationID string `json:"correlation_id"`

	// Data is the structured JSON payload describing what changed (TA_DATA).
	// Its exact shape is defined per TransactionType and enforced by the service
	// layer, not the database.
	Data map[string]any `json:"data"`
}
