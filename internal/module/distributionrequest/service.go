package distributionrequest

import (
	"context"

	"github.com/google/uuid"
)

// Service is the application port for the distributionrequest module.
//
// Create validates the referenced distributor, collection request, and optional
// transformed data, then persists a PENDING request. Update validates the
// status and failure-reason pairing, then records the delivery outcome. Both
// mutating methods run inside a managed transaction. The read methods back the
// HTTP handlers that expose distribution requests; callers never manage
// transactions themselves. The concrete implementation lives in
// internal/adapters/distributionrequest.
type Service interface {
	// Create validates that the referenced distributor and collection request
	// exist (wrapped not-found otherwise), and that any referenced transformed
	// data exists and belongs to the collection request (ErrTransformedDataMismatch),
	// then atomically inserts a StatusPending request and returns the persisted
	// row (with the database-assigned Uid and TimestampTz).
	Create(ctx context.Context, cmd CreateCommand) (DistributionRequest, error)

	// Update records the outcome of a distribution request. Returns
	// ErrInvalidStatus for an unrecognised status, ErrFailureReasonMismatch if
	// the failure reason is not present exactly when the status is StatusFailed,
	// or a wrapped ErrNotFound if no request with the given UID exists.
	Update(ctx context.Context, cmd UpdateCommand) (DistributionRequest, error)

	// GetByUid returns the DistributionRequest with the given primary key.
	// Returns a wrapped ErrNotFound if absent.
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
