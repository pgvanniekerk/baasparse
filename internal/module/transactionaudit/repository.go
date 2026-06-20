package transactionaudit

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Repository is the persistence port for the transactionaudit module.
// Implementations must satisfy two contracts:
//  1. CreateTransaction must execute within the pgx.Tx stored in the context
//     (see internal/db.WithTx) so that the audit row is committed atomically
//     with the business operation it describes.
//  2. Read methods (GetBy*) are non-transactional and use the connection pool
//     directly.
type Repository interface {
	// GetByUser returns audit rows for a specific username within the given
	// time window, ordered newest-first. offset and count drive pagination;
	// both are uint8 (max 255) which is sufficient for control-plane audit UIs.
	GetByUser(ctx context.Context, username string, offset, count uint8, fromTimestamp, toTimestamp time.Time) ([]Transaction, error)

	// GetByTransactionType returns audit rows of a specific type within the
	// given time window, ordered newest-first, with limit/offset pagination.
	GetByTransactionType(ctx context.Context, transactionType string, offset, count uint8, fromTimestamp, toTimestamp time.Time) ([]Transaction, error)

	// GetByCorrelationID returns all audit rows that share the given
	// correlation ID, ordered newest-first, with limit/offset pagination.
	// Useful for reconstructing all side-effects of a single HTTP request.
	GetByCorrelationID(ctx context.Context, correlationID string, offset, count uint8) ([]Transaction, error)

	// CreateTransaction inserts a new immutable audit row and returns its
	// database-generated UID. The context must carry a pgx.Tx (via
	// internal/db.WithTx); returns db.ErrNoTransaction otherwise.
	CreateTransaction(context.Context, Transaction) (uuid.UUID, error)
}
