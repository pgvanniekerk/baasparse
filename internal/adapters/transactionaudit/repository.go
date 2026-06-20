package transactionaudit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
	"go.opentelemetry.io/otel/metric"

	"github.com/pgvanniekerk/baasparse/internal/db"
	domain "github.com/pgvanniekerk/baasparse/internal/module/transactionaudit"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so that
// every database call is automatically observed by OTel metrics.
type repository struct {
	// pool is the pgx connection pool. Read methods call pool.Query /
	// pool.QueryRow directly. Write methods must extract a pgx.Tx from the
	// context via db.TxFromContext instead.
	pool *pgxpool.Pool
}

// CreateTransaction inserts an audit row using the pgx.Tx extracted from ctx.
// It intentionally refuses to fall back to the pool: an audit row that commits
// independently of the business operation it describes would be misleading.
func (r repository) CreateTransaction(ctx context.Context, ta domain.Transaction) (uuid.UUID, error) {
	// Enforce the transactional contract — callers must use db.WithTx.
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return uuid.Nil, db.ErrNoTransaction
	}

	// TA_DATA is a JSONB column. We marshal to []byte first and cast to
	// json.RawMessage so pgx sends it using the JSON codec rather than BYTEA.
	dataBytes, err := json.Marshal(ta.Data)
	if err != nil {
		return uuid.Nil, fmt.Errorf("transactionaudit: marshal data: %w", err)
	}

	var uid uuid.UUID
	var ts time.Time
	err = tx.QueryRow(ctx, sqlCreate,
		ta.Username,
		ta.TransactionType,
		json.RawMessage(dataBytes),
		ta.CorrelationID,
	).Scan(&uid, &ts)
	if err != nil {
		return uuid.Nil, fmt.Errorf("transactionaudit: create: %w", err)
	}

	return uid, nil
}

func (r repository) GetByUser(ctx context.Context, username string, offset, count uint8, fromTimestamp, toTimestamp time.Time) ([]domain.Transaction, error) {
	rows, err := r.pool.Query(ctx, sqlGetByUser, username, fromTimestamp, toTimestamp, count, offset)
	if err != nil {
		return nil, fmt.Errorf("transactionaudit: get by user: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) GetByTransactionType(ctx context.Context, transactionType string, offset, count uint8, fromTimestamp, toTimestamp time.Time) ([]domain.Transaction, error) {
	rows, err := r.pool.Query(ctx, sqlGetByTransactionType, transactionType, fromTimestamp, toTimestamp, count, offset)
	if err != nil {
		return nil, fmt.Errorf("transactionaudit: get by transaction type: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) GetByCorrelationID(ctx context.Context, correlationID string, offset, count uint8) ([]domain.Transaction, error) {
	rows, err := r.pool.Query(ctx, sqlGetByCorrelationID, correlationID, count, offset)
	if err != nil {
		return nil, fmt.Errorf("transactionaudit: get by correlation id: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// scanRows iterates a pgx.Rows result set and maps each row to a
// domain.Transaction. TA_DATA is a JSONB column — it is scanned into a
// json.RawMessage first and then unmarshalled into map[string]any so the
// caller receives a fully-populated struct rather than raw bytes.
func scanRows(rows pgx.Rows) ([]domain.Transaction, error) {
	var results []domain.Transaction
	for rows.Next() {
		var t domain.Transaction
		var rawData json.RawMessage
		if err := rows.Scan(
			&t.TransactionUid,
			&t.TimestampTz,
			&t.Username,
			&t.TransactionType,
			&t.CorrelationID,
			&rawData,
		); err != nil {
			return nil, fmt.Errorf("transactionaudit: scan row: %w", err)
		}
		if err := json.Unmarshal(rawData, &t.Data); err != nil {
			return nil, fmt.Errorf("transactionaudit: unmarshal data: %w", err)
		}
		results = append(results, t)
	}
	// rows.Err() surfaces any error that terminated iteration early (e.g. a
	// network failure mid-stream) that would not have been caught inside the loop.
	return results, rows.Err()
}

// ── telemetry layer ───────────────────────────────────────────────────────────

// teleRepository wraps repository and records OTel metrics on every call.
// Each method increments a calls counter before delegating, records the
// elapsed duration after the call returns, and increments either successes or
// failures depending on whether err is nil.
type teleRepository struct {
	repo                 repository
	createTransaction    telemetry.MethodInstruments
	getByUser            telemetry.MethodInstruments
	getByTransactionType telemetry.MethodInstruments
	getByCorrelationID   telemetry.MethodInstruments
}

func (t teleRepository) CreateTransaction(ctx context.Context, ta domain.Transaction) (uuid.UUID, error) {
	t.createTransaction.Calls.Add(ctx, 1)
	start := time.Now()
	uid, err := t.repo.CreateTransaction(ctx, ta)
	t.createTransaction.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.createTransaction.Failures.Add(ctx, 1)
	} else {
		t.createTransaction.Successes.Add(ctx, 1)
	}
	return uid, err
}

func (t teleRepository) GetByUser(ctx context.Context, username string, offset, count uint8, fromTimestamp, toTimestamp time.Time) ([]domain.Transaction, error) {
	t.getByUser.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByUser(ctx, username, offset, count, fromTimestamp, toTimestamp)
	t.getByUser.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByUser.Failures.Add(ctx, 1)
	} else {
		t.getByUser.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetByTransactionType(ctx context.Context, transactionType string, offset, count uint8, fromTimestamp, toTimestamp time.Time) ([]domain.Transaction, error) {
	t.getByTransactionType.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByTransactionType(ctx, transactionType, offset, count, fromTimestamp, toTimestamp)
	t.getByTransactionType.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByTransactionType.Failures.Add(ctx, 1)
	} else {
		t.getByTransactionType.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetByCorrelationID(ctx context.Context, correlationID string, offset, count uint8) ([]domain.Transaction, error) {
	t.getByCorrelationID.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByCorrelationID(ctx, correlationID, offset, count)
	t.getByCorrelationID.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByCorrelationID.Failures.Add(ctx, 1)
	} else {
		t.getByCorrelationID.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// Repository is the exported, telemetry-instrumented adapter for the
// transactionaudit module. It embeds teleRepository so all four module.Repository
// methods are promoted and the struct satisfies the interface directly.
// Construct with NewRepository; do not instantiate directly.
type Repository struct {
	teleRepository
}

// NewRepository builds a telemetry-instrumented pgx repository.
//
// pool is the pgx connection pool shared across the application.
// meter is the OTel Meter (obtained from the global provider or a pipeline-
// specific one) used to register per-method call counters and duration
// histograms under the "baasparse.transactionaudit.repository.*" namespace.
//
// Returns an error if any OTel instrument fails to register, which typically
// indicates a meter provider misconfiguration.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.transactionaudit.repository."

	createTransaction, err := telemetry.NewMethodInstruments(meter, prefix+"create_transaction")
	if err != nil {
		return Repository{}, err
	}
	getByUser, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_user")
	if err != nil {
		return Repository{}, err
	}
	getByTransactionType, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_transaction_type")
	if err != nil {
		return Repository{}, err
	}
	getByCorrelationID, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_correlation_id")
	if err != nil {
		return Repository{}, err
	}

	return Repository{
		teleRepository: teleRepository{
			repo:                 repository{pool: pool},
			createTransaction:    createTransaction,
			getByUser:            getByUser,
			getByTransactionType: getByTransactionType,
			getByCorrelationID:   getByCorrelationID,
		},
	}, nil
}
