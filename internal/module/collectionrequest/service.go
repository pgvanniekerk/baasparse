package collectionrequest

import (
	"context"

	"github.com/google/uuid"
)

// Service is the application port for the collectionrequest module.
//
// Create derives the content hash (CR_HASH_KEY) from the payload before
// persisting — the repository never computes it — and runs inside a managed
// database transaction. The read methods back the HTTP handlers that expose
// collection requests; callers never manage transactions themselves.
// The concrete implementation lives in internal/adapters/collectionrequest.
type Service interface {
	// Create validates that the referenced pipeline and collector exist
	// (returning a wrapped not-found error otherwise) and that the collector
	// belongs to the pipeline (ErrCollectorPipelineMismatch), computes the
	// content hash of cmd.Data, rejects an already-ingested payload for the same
	// (pipeline, collector) with ErrAlreadyExists, then atomically inserts the
	// request and returns the persisted record (with the database-assigned Uid
	// and TimestampTz).
	Create(ctx context.Context, cmd CreateCommand) (CollectionRequest, error)

	// GetByUid returns the CollectionRequest with the given primary key.
	// Returns a wrapped ErrNotFound if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (CollectionRequest, error)

	// GetByPUid returns all collection requests for the given pipeline UID,
	// ordered chronologically. Returns an empty slice when none exist.
	GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]CollectionRequest, error)

	// GetByCUid returns all collection requests for the given collector UID,
	// ordered chronologically. Returns an empty slice when none exist.
	GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]CollectionRequest, error)
}
