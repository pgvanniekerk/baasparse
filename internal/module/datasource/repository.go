package datasource

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the datasource module.
//
// Create requires a pgx.Tx in the context (via internal/db.WithTx) because
// the data source row and its corresponding audit entry must be inserted
// atomically. The txService wrapper manages this automatically; callers should
// not set up transactions themselves.
//
// Read methods (GetBy*, GetAll) use the connection pool directly and do not
// require a transaction in the context.
type Repository interface {
	// Create inserts a new data source row and returns the generated UID.
	// The context must carry a pgx.Tx; returns db.ErrNoTransaction otherwise.
	Create(ctx context.Context, ds DataSource) (uuid.UUID, error)

	// GetByUid returns the DataSource with the given primary key.
	// Returns a wrapped ErrNotFound containing the UID string if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (DataSource, error)

	// GetByName returns the DataSource with the given unique name.
	// Returns a wrapped ErrNotFound containing the name string if absent.
	GetByName(ctx context.Context, name string) (DataSource, error)

	// Update changes the Name and ConnectionSpecification of an existing data
	// source. The context must carry a pgx.Tx. Returns a wrapped ErrNotFound
	// if no row with the given UID exists.
	Update(ctx context.Context, cmd UpdateCommand) error

	// GetAll returns all data source rows ordered alphabetically by name.
	// Returns ErrNotFound if no rows exist.
	GetAll(ctx context.Context) ([]DataSource, error)
}
