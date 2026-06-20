package distributor

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the distributor module.
//
// Create and Update require a pgx.Tx in the context (via internal/db.WithTx)
// because the distributor row and its corresponding audit entry must be written
// atomically. The txService wrapper manages this automatically; callers should
// not set up transactions themselves.
//
// Read methods (GetByUid, GetByPUid, GetAll, ExistsByPUidAndName) use the
// connection pool directly and do not require a transaction in the context.
type Repository interface {
	// Create inserts a new distributor row and returns the generated UID.
	// The context must carry a pgx.Tx; returns db.ErrNoTransaction otherwise.
	Create(ctx context.Context, d Distributor) (uuid.UUID, error)

	// Update changes the data source, name, specifications, transformer, and
	// status of an existing distributor. The context must carry a pgx.Tx.
	// Returns a wrapped ErrNotFound if no row with the given UID exists.
	Update(ctx context.Context, cmd UpdateCommand) error

	// GetByUid returns the Distributor with the given primary key.
	// Returns a wrapped ErrNotFound containing the UID string if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (Distributor, error)

	// GetByPUid returns all distributors for the given pipeline UID, ordered by
	// name. A pipeline may have many distributors, so this returns a slice
	// (empty when the pipeline has none).
	GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]Distributor, error)

	// GetAll returns all distributors ordered by pipeline UID then name. When
	// status is non-nil, only distributors with that status are returned.
	// Returns an empty slice when nothing matches.
	GetAll(ctx context.Context, status *Status) ([]Distributor, error)

	// ExistsByPUidAndName reports whether a distributor with the given name
	// already exists within the given pipeline. Used by the service to enforce
	// per-pipeline name uniqueness (UIDX_D_P_UID_NAME) before an insert or
	// rename.
	ExistsByPUidAndName(ctx context.Context, pipelineUid uuid.UUID, name string) (bool, error)
}
