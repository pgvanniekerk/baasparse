package transformeddata

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the transformeddata module.
//
// TD_TRANSFORMED_DATA is an append-only data-plane record: rows are inserted by
// the pipeline engine and are never updated or deleted.
//
// Create requires a pgx.Tx in the context (via internal/db.WithTx) so the
// caller controls the commit boundary; read methods use the connection pool
// directly and do not require a transaction.
type Repository interface {
	// Create inserts a new transformed-data record and returns the persisted
	// record with the database-generated Uid and TimestampTz populated. The
	// context must carry a pgx.Tx; returns db.ErrNoTransaction otherwise.
	Create(ctx context.Context, td TransformedData) (TransformedData, error)

	// GetByUid returns the TransformedData with the given primary key.
	// Returns a wrapped ErrNotFound containing the UID string if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (TransformedData, error)

	// GetByCRUid returns all transformed-data records parsed from the given
	// collection request UID, ordered chronologically. Returns an empty slice
	// when none exist.
	GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]TransformedData, error)

	// GetByCUid returns all transformed-data records for the given collector
	// UID, ordered chronologically. Returns an empty slice when none exist.
	GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]TransformedData, error)
}
