package distributor

import (
	"context"

	"github.com/google/uuid"
)

// Service is the application port for the distributor module.
// All mutating methods (Create, Update) run inside a managed database
// transaction; callers never need to begin or commit transactions themselves.
// The concrete implementation lives in internal/adapters/distributor.
type Service interface {
	// Create validates the referenced pipeline, data source, and (optional)
	// transformer, validates the distribution and transformation specifications
	// against their schemas, enforces per-pipeline name uniqueness, then
	// atomically inserts the distributor (always StatusActive) and its creation
	// audit entry. Returns ErrNameExists if the name is already used within the
	// pipeline, ErrInvalidDistributionSpec or ErrInvalidTransformationSpec on
	// schema-validation failure, or ErrTransformationSpecMismatch if the
	// transformer/specification pairing is invalid.
	Create(ctx context.Context, cmd CreateCommand) (Distributor, error)

	// Update re-validates the references and specifications, then atomically
	// updates the distributor and writes an audit entry. Returns a wrapped
	// ErrNotFound if no distributor with the given UID exists, ErrInvalidStatus
	// for an unrecognised status, ErrNameExists if the new name collides with
	// another distributor in the same pipeline, or the same specification
	// errors as Create. The pipeline link is immutable and cannot be changed.
	Update(ctx context.Context, cmd UpdateCommand) (Distributor, error)

	// GetByUid returns the Distributor with the given primary key.
	// Returns a wrapped ErrNotFound if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (Distributor, error)

	// GetByPUid returns all distributors for the given pipeline UID, ordered by
	// name. Returns an empty slice when the pipeline has none.
	GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]Distributor, error)

	// GetAll returns all distributors ordered by pipeline UID then name. When
	// status is non-nil, only distributors with that status are returned.
	// Returns an empty slice when nothing matches.
	GetAll(ctx context.Context, status *Status) ([]Distributor, error)
}
