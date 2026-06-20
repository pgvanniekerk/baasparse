package pipeline

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the pipeline module.
//
// Create and Update require a pgx.Tx in the context (via internal/db.WithTx)
// because the pipeline row and its corresponding audit entry must be written
// atomically. The txService wrapper manages this automatically; callers should
// not set up transactions themselves.
//
// Read methods (GetByUid, GetByNameLike, GetAll, ExistsByName) use the
// connection pool directly and do not require a transaction in the context.
type Repository interface {
	// Create inserts a new pipeline row and returns the generated UID.
	// The context must carry a pgx.Tx; returns db.ErrNoTransaction otherwise.
	Create(ctx context.Context, p Pipeline) (uuid.UUID, error)

	// Update changes the Name, Description, and Status of an existing pipeline.
	// The context must carry a pgx.Tx. Returns a wrapped ErrNotFound if no row
	// with the given UID exists.
	Update(ctx context.Context, cmd UpdateCommand) error

	// GetByUid returns the Pipeline with the given primary key.
	// Returns a wrapped ErrNotFound containing the UID string if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (Pipeline, error)

	// GetByNameLike returns all pipelines whose name matches the given SQL LIKE
	// pattern, ordered alphabetically by name. The caller supplies any
	// wildcards. Returns an empty slice when nothing matches.
	GetByNameLike(ctx context.Context, pattern string) ([]Pipeline, error)

	// GetAll returns all pipelines ordered alphabetically by name. When status
	// is non-nil, only pipelines with that status are returned. Returns an
	// empty slice when nothing matches.
	GetAll(ctx context.Context, status *Status) ([]Pipeline, error)

	// ExistsByName reports whether a pipeline with the exact given name already
	// exists. This is an exact match (not a LIKE search), used by the service
	// to enforce P_NAME uniqueness before an insert or rename.
	ExistsByName(ctx context.Context, name string) (bool, error)
}
