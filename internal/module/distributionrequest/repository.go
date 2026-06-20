package distributionrequest

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the distributionrequest module.
//
// Create and Update require a pgx.Tx in the context (via internal/db.WithTx) so
// the caller controls the commit boundary; read methods use the connection pool
// directly and do not require a transaction.
type Repository interface {
	// Create inserts a new distribution request and returns the persisted
	// record with the database-generated Uid and TimestampTz populated. The
	// context must carry a pgx.Tx; returns db.ErrNoTransaction otherwise.
	Create(ctx context.Context, dr DistributionRequest) (DistributionRequest, error)

	// Update records the outcome of a distribution request, setting its status,
	// content, and failure reason. The context must carry a pgx.Tx. Returns a
	// wrapped ErrNotFound if no row with the given UID exists.
	Update(ctx context.Context, cmd UpdateCommand) error

	// GetByUid returns the DistributionRequest with the given primary key.
	// Returns a wrapped ErrNotFound containing the UID string if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (DistributionRequest, error)

	// GetByDUid returns all distribution requests for the given distributor UID,
	// ordered chronologically. Returns an empty slice when none exist.
	GetByDUid(ctx context.Context, distributorUid uuid.UUID) ([]DistributionRequest, error)

	// GetByCRUid returns all distribution requests for the given collection
	// request UID, ordered chronologically. Returns an empty slice when none
	// exist.
	GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]DistributionRequest, error)

	// GetByTDUid returns all distribution requests for the given transformed
	// data UID, ordered chronologically. Returns an empty slice when none
	// exist.
	GetByTDUid(ctx context.Context, transformedDataUid uuid.UUID) ([]DistributionRequest, error)
}
