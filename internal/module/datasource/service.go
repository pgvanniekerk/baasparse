package datasource

import (
	"context"

	"github.com/google/uuid"
)

// Service is the application port for the datasource module.
// All mutating methods (Create) run inside a managed database transaction;
// callers never need to begin or commit transactions themselves.
// The concrete implementation lives in internal/adapters/datasource.
type Service interface {
	// Create validates cmd.ConnectionSpecification against the referenced
	// DataSourceType's ConnectionSchema, then atomically inserts the data
	// source and its creation audit entry. Returns ErrInvalidConnectionSpec
	// if validation fails, or a wrapped ErrNotFound if the DataSourceType
	// does not exist.
	Create(ctx context.Context, cmd CreateCommand) (DataSource, error)

	// GetByUid returns the DataSource with the given primary key.
	// Returns a wrapped ErrNotFound if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (DataSource, error)

	// GetByName returns the DataSource with the given unique name.
	// Returns a wrapped ErrNotFound if absent.
	GetByName(ctx context.Context, name string) (DataSource, error)

	// Update validates the new ConnectionSpecification against the DataSourceType's
	// ConnectionSchema, then atomically updates the row and writes an audit entry.
	// Returns ErrInvalidConnectionSpec if validation fails, or a wrapped
	// ErrNotFound if no data source with the given UID exists.
	Update(ctx context.Context, cmd UpdateCommand) (DataSource, error)

	// GetAll returns all data source rows ordered alphabetically by name.
	// Returns ErrNotFound if no rows exist.
	GetAll(ctx context.Context) ([]DataSource, error)
}
