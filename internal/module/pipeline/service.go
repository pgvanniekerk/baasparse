package pipeline

import (
	"context"

	"github.com/google/uuid"
)

// Service is the application port for the pipeline module.
// All mutating methods (Create, Update) run inside a managed database
// transaction; callers never need to begin or commit transactions themselves.
// The concrete implementation lives in internal/adapters/pipeline.
type Service interface {
	// Create inserts a new pipeline (always StatusActive) together with its
	// creation audit entry, atomically. Returns ErrNameExists if a pipeline
	// with the same name already exists.
	Create(ctx context.Context, cmd CreateCommand) (Pipeline, error)

	// Update changes the name, description, and status of an existing pipeline
	// and writes an audit entry, atomically. Returns a wrapped ErrNotFound if
	// no pipeline with the given UID exists, ErrNameExists if the new name is
	// already taken by a different pipeline, or ErrInvalidStatus if the status
	// is not a recognised value.
	Update(ctx context.Context, cmd UpdateCommand) (Pipeline, error)

	// GetByUid returns the Pipeline with the given primary key.
	// Returns a wrapped ErrNotFound if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (Pipeline, error)

	// GetByNameLike returns all pipelines whose name contains the given search
	// term, ordered alphabetically by name. Returns an empty slice when nothing
	// matches.
	GetByNameLike(ctx context.Context, name string) ([]Pipeline, error)

	// GetAll returns all pipelines ordered alphabetically by name. When status
	// is non-nil, only pipelines with that status are returned. Returns an
	// empty slice when nothing matches.
	GetAll(ctx context.Context, status *Status) ([]Pipeline, error)
}
