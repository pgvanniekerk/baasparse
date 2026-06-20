package datatype

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the read-only persistence port for the datatype module.
// All three methods return ErrNotFound when no matching rows exist, allowing
// callers to distinguish "not found" from other database errors using
// errors.Is(err, datatype.ErrNotFound).
type Repository interface {
	// GetByUid returns the DataType with the given primary key.
	// Returns a wrapped ErrNotFound containing the UID string if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (DataType, error)

	// GetByCode returns the DataType with the given unique code.
	// Returns a wrapped ErrNotFound containing the code string if absent.
	// Prefer this over GetByUid when working with user-supplied configuration,
	// since codes are stable and human-readable (e.g. "JSON").
	GetByCode(ctx context.Context, code string) (DataType, error)

	// GetAll returns all DataType rows ordered alphabetically by code.
	// Returns ErrNotFound if the catalog is empty, which indicates the seed
	// migration (0017_seed_dt_data_types) has not been applied.
	GetAll(ctx context.Context) ([]DataType, error)
}
