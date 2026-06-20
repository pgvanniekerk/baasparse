package collector

import (
	"context"

	"github.com/google/uuid"
)

// Service is the application port for the collector module.
// All mutating methods (Create, Update) run inside a managed database
// transaction; callers never need to begin or commit transactions themselves.
// The concrete implementation lives in internal/adapters/collector.
type Service interface {
	// Create validates the referenced pipeline, data source, and (optional)
	// transformer, validates the collection and transformation specifications
	// against their schemas, then atomically inserts the collector (always
	// StatusActive) and its creation audit entry. Returns ErrCollectorExists if
	// the pipeline already has a collector, ErrInvalidCollectionSpec or
	// ErrInvalidTransformationSpec on schema-validation failure, or
	// ErrTransformationSpecMismatch if the transformer/specification pairing is
	// invalid.
	Create(ctx context.Context, cmd CreateCommand) (Collector, error)

	// Update re-validates the references and specifications, then atomically
	// updates the collector and writes an audit entry. Returns a wrapped
	// ErrNotFound if no collector with the given UID exists, ErrInvalidStatus
	// for an unrecognised status, or the same specification errors as Create.
	// The pipeline link is immutable and cannot be changed.
	Update(ctx context.Context, cmd UpdateCommand) (Collector, error)

	// GetByUid returns the Collector with the given primary key.
	// Returns a wrapped ErrNotFound if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (Collector, error)

	// GetByPUid returns the single Collector for the given pipeline UID.
	// Returns a wrapped ErrNotFound if the pipeline has no collector.
	GetByPUid(ctx context.Context, pipelineUid uuid.UUID) (Collector, error)

	// GetAll returns all collectors ordered by pipeline UID. When status is
	// non-nil, only collectors with that status are returned. Returns an empty
	// slice when nothing matches.
	GetAll(ctx context.Context, status *Status) ([]Collector, error)
}
