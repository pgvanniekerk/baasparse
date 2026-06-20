package datasourcetype

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the read-only persistence port for the datasourcetype module.
// All three methods return ErrNotFound when no matching rows exist, allowing
// callers to distinguish "not found" from other database errors using
// errors.Is(err, datasourcetype.ErrNotFound).
type Repository interface {
	// GetByUid returns the DataSourceType with the given primary key.
	// Returns a wrapped ErrNotFound containing the UID string if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (DataSourceType, error)

	// GetByName returns the DataSourceType with the given unique name.
	// Returns a wrapped ErrNotFound containing the name string if absent.
	// Prefer this over GetByUid when working with user-supplied configuration,
	// since names are stable and human-readable (e.g. "LocalFileSystem").
	GetByName(ctx context.Context, name string) (DataSourceType, error)

	// GetAll returns all DataSourceType rows ordered alphabetically by name.
	// Returns ErrNotFound if the catalog is empty, which indicates the seed
	// migrations (0007_seed_dst_types) have not been applied.
	GetAll(ctx context.Context) ([]DataSourceType, error)
}
