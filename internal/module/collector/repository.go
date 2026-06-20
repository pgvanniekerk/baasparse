package collector

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the collector module.
//
// Create and Update require a pgx.Tx in the context (via internal/db.WithTx)
// because the collector row and its corresponding audit entry must be written
// atomically. The txService wrapper manages this automatically; callers should
// not set up transactions themselves.
//
// Read methods (GetByUid, GetByPUid, GetAll, ExistsByPUid) use the connection
// pool directly and do not require a transaction in the context.
type Repository interface {
	// Create inserts a new collector row and returns the generated UID.
	// The context must carry a pgx.Tx; returns db.ErrNoTransaction otherwise.
	Create(ctx context.Context, c Collector) (uuid.UUID, error)

	// Update changes the data source, specifications, transformer, and status
	// of an existing collector. The context must carry a pgx.Tx. Returns a
	// wrapped ErrNotFound if no row with the given UID exists.
	Update(ctx context.Context, cmd UpdateCommand) error

	// GetByUid returns the Collector with the given primary key.
	// Returns a wrapped ErrNotFound containing the UID string if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (Collector, error)

	// GetByPUid returns the single Collector for the given pipeline UID
	// (C_P_UID is unique). Returns a wrapped ErrNotFound containing the UID
	// string if the pipeline has no collector.
	GetByPUid(ctx context.Context, pipelineUid uuid.UUID) (Collector, error)

	// GetAll returns all collectors ordered by pipeline UID. When status is
	// non-nil, only collectors with that status are returned. Returns an empty
	// slice when nothing matches.
	GetAll(ctx context.Context, status *Status) ([]Collector, error)

	// ExistsByPUid reports whether a collector already exists for the given
	// pipeline UID. Used by the service to enforce the 1:1 pipeline:collector
	// relationship before an insert.
	ExistsByPUid(ctx context.Context, pipelineUid uuid.UUID) (bool, error)
}
