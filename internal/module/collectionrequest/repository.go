package collectionrequest

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the persistence port for the collectionrequest module.
//
// CR_COLLECTION_REQUEST is an append-only data-plane record: rows are inserted
// at ingestion time and are never updated or deleted.
//
// Create requires a pgx.Tx in the context (via internal/db.WithTx) so the
// caller controls the commit boundary; read methods use the connection pool
// directly and do not require a transaction.
type Repository interface {
	// Create inserts a new collection request and returns the persisted record
	// with the database-generated Uid and TimestampTz populated. The context
	// must carry a pgx.Tx; returns db.ErrNoTransaction otherwise. The caller
	// (the service) supplies cr.HashKey — the repository never derives it. The
	// unique index on (PipelineUid, CollectorUid, HashKey) rejects a duplicate
	// payload with a constraint error.
	Create(ctx context.Context, cr CollectionRequest) (CollectionRequest, error)

	// GetByUid returns the CollectionRequest with the given primary key.
	// Returns a wrapped ErrNotFound containing the UID string if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (CollectionRequest, error)

	// GetByPUid returns all collection requests for the given pipeline UID,
	// ordered chronologically. Returns an empty slice when none exist.
	GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]CollectionRequest, error)

	// GetByCUid returns all collection requests for the given collector UID,
	// ordered chronologically. Returns an empty slice when none exist.
	GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]CollectionRequest, error)

	// ExistsByPUidAndCUidAndHashKey reports whether a payload with the given
	// content hash has already been ingested for the (pipeline, collector)
	// pair. Used to skip duplicate ingestion before insert
	// (UIDX_CR_P_UID_C_UID_HASH_KEY).
	ExistsByPUidAndCUidAndHashKey(ctx context.Context, pipelineUid, collectorUid uuid.UUID, hashKey string) (bool, error)
}
