package transformer

import (
	"context"

	"github.com/google/uuid"
)

// Repository is the read-only persistence port for the transformer module.
// Both methods return ErrNotFound when no matching row exists, allowing callers
// to distinguish "not found" from other database errors using
// errors.Is(err, transformer.ErrNotFound).
type Repository interface {
	// GetByUid returns the Transformer with the given primary key.
	// Returns a wrapped ErrNotFound containing the UID string if absent.
	GetByUid(ctx context.Context, uid uuid.UUID) (Transformer, error)

	// GetByFromDtUidAndToDtUid returns the single Transformer that converts
	// from the given source data type (fromDtUid) to the given target data
	// type (toDtUid). The (from, to) pair is unique, so at most one row
	// matches. Returns a wrapped ErrNotFound containing both UIDs if absent.
	GetByFromDtUidAndToDtUid(ctx context.Context, fromDtUid, toDtUid uuid.UUID) (Transformer, error)
}
