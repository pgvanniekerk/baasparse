package transformeddata

import (
	"context"

	"github.com/google/uuid"
)

// Service is the application port for the transformeddata module.
//
// Create validates the referenced collection request and collector, and that
// the collector matches the collection request's collector, then persists the
// record inside a managed database transaction. The read methods back the HTTP
// handlers that expose transformed data; callers never manage transactions
// themselves. The concrete implementation lives in
// internal/adapters/transformeddata.
type Service interface {
	// Create validates that the referenced collection request and collector
	// exist (wrapped not-found otherwise) and that the collector matches the
	// collection request's collector (ErrCollectorMismatch), then atomically
	// inserts the record and returns the persisted row (with the
	// database-assigned Uid and TimestampTz).
	Create(ctx context.Context, cmd CreateCommand) (TransformedData, error)

	// GetByUid returns the TransformedData with the given primary key.
	// Returns a wrapped ErrNotFound if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (TransformedData, error)

	// GetByCRUid returns all transformed-data records parsed from the given
	// collection request UID, ordered chronologically. Returns an empty slice
	// when none exist.
	GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]TransformedData, error)

	// GetByCUid returns all transformed-data records for the given collector
	// UID, ordered chronologically. Returns an empty slice when none exist.
	GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]TransformedData, error)
}
