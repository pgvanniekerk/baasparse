package distributionrequest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	"github.com/pgvanniekerk/baasparse/internal/db"
	module "github.com/pgvanniekerk/baasparse/internal/module/distributionrequest"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so every
// database call is automatically observed by OTel metrics.
type repository struct {
	// pool is used for read operations (GetByUid, GetByDUid, GetByCRUid,
	// GetByTDUid). Create and Update extract a pgx.Tx from the context via
	// db.TxFromContext instead, leaving the commit boundary to the caller.
	pool *pgxpool.Pool
}

// Create inserts a new distribution request using the pgx.Tx in ctx and returns
// the persisted record with the database-generated Uid and TimestampTz
// populated. Returns db.ErrNoTransaction if no transaction is present.
func (r repository) Create(ctx context.Context, dr module.DistributionRequest) (module.DistributionRequest, error) {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return module.DistributionRequest{}, db.ErrNoTransaction
	}

	// DR_TD_UID may be NULL (pass-through); DR_UID and DR_TIMESTAMPTZ are
	// assigned by the database and returned here.
	err := tx.QueryRow(ctx, sqlCreate,
		dr.DistributorUid,
		dr.CollectionRequestUid,
		nullableUUID(dr.TransformedDataUid),
		string(dr.Status),
	).Scan(&dr.Uid, &dr.TimestampTz)
	if err != nil {
		return module.DistributionRequest{}, fmt.Errorf("distributionrequest: create: %w", err)
	}
	return dr, nil
}

// Update records the outcome (status, content, failure reason) for an existing
// row using the pgx.Tx in ctx. Returns module.ErrNotFound if no row matches.
func (r repository) Update(ctx context.Context, cmd module.UpdateCommand) error {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return db.ErrNoTransaction
	}

	var uid uuid.UUID
	err := tx.QueryRow(ctx, sqlUpdate,
		string(cmd.Status),
		nullableBytes(cmd.Content),
		cmd.FailureReason,
		cmd.Uid,
	).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", module.ErrNotFound, cmd.Uid)
	}
	if err != nil {
		return fmt.Errorf("distributionrequest: update: %w", err)
	}
	return nil
}

func (r repository) GetByUid(ctx context.Context, uid uuid.UUID) (module.DistributionRequest, error) {
	dr, err := scanRow(r.pool.QueryRow(ctx, sqlGetByUid, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.DistributionRequest{}, fmt.Errorf("%w: %s", module.ErrNotFound, uid)
	}
	if err != nil {
		return module.DistributionRequest{}, fmt.Errorf("distributionrequest: get by uid: %w", err)
	}
	return dr, nil
}

func (r repository) GetByDUid(ctx context.Context, distributorUid uuid.UUID) ([]module.DistributionRequest, error) {
	rows, err := r.pool.Query(ctx, sqlGetByDUid, distributorUid)
	if err != nil {
		return nil, fmt.Errorf("distributionrequest: get by distributor uid: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]module.DistributionRequest, error) {
	rows, err := r.pool.Query(ctx, sqlGetByCRUid, collectionRequestUid)
	if err != nil {
		return nil, fmt.Errorf("distributionrequest: get by collection request uid: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) GetByTDUid(ctx context.Context, transformedDataUid uuid.UUID) ([]module.DistributionRequest, error) {
	rows, err := r.pool.Query(ctx, sqlGetByTDUid, transformedDataUid)
	if err != nil {
		return nil, fmt.Errorf("distributionrequest: get by transformed data uid: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// nullableUUID converts an optional UUID pointer into a query argument that
// encodes as SQL NULL when nil.
func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

// nullableBytes converts an optional byte payload into a query argument that
// encodes as SQL NULL when empty, so absent content stores as NULL rather than
// an empty BYTEA value.
func nullableBytes(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// scanRow scans a single DR_DISTRIBUTION_REQUEST row from any pgx.Row (returned
// by either pool.QueryRow for single-row lookups or rows.Scan for multi-row
// iteration). DR_TD_UID is nullable and scanned via uuid.NullUUID into an
// optional *uuid.UUID; DR_CONTENT (BYTEA) and DR_FAILURE_REASON are nullable
// (nil when NULL); DR_STATUS is scanned into a string then converted to
// module.Status. Column order must match the SELECT list in sql.go.
func scanRow(row pgx.Row) (module.DistributionRequest, error) {
	var dr module.DistributionRequest
	var (
		transformedDataUID uuid.NullUUID
		status             string
	)
	if err := row.Scan(
		&dr.Uid,
		&dr.DistributorUid,
		&dr.CollectionRequestUid,
		&transformedDataUID,
		&dr.Content,
		&status,
		&dr.FailureReason,
		&dr.TimestampTz,
	); err != nil {
		return module.DistributionRequest{}, err
	}
	if transformedDataUID.Valid {
		id := transformedDataUID.UUID
		dr.TransformedDataUid = &id
	}
	dr.Status = module.Status(status)
	return dr, nil
}

// scanRows maps a full pgx.Rows result set into a slice of DistributionRequest
// by calling scanRow for each row. rows.Err() is checked after iteration to
// surface any error that terminated the stream early.
func scanRows(rows pgx.Rows) ([]module.DistributionRequest, error) {
	var results []module.DistributionRequest
	for rows.Next() {
		dr, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("distributionrequest: scan row: %w", err)
		}
		results = append(results, dr)
	}
	return results, rows.Err()
}

// ── telemetry layer ───────────────────────────────────────────────────────────

// teleRepository wraps repository and records OTel metrics on every call.
// Each method increments a calls counter, delegates to the pgx layer, records
// elapsed duration, then increments successes or failures.
type teleRepository struct {
	repo       repository
	create     telemetry.MethodInstruments
	update     telemetry.MethodInstruments
	getByUid   telemetry.MethodInstruments
	getByDUid  telemetry.MethodInstruments
	getByCRUid telemetry.MethodInstruments
	getByTDUid telemetry.MethodInstruments
}

func (t teleRepository) Create(ctx context.Context, dr module.DistributionRequest) (module.DistributionRequest, error) {
	t.create.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.Create(ctx, dr)
	t.create.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.create.Failures.Add(ctx, 1)
	} else {
		t.create.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) Update(ctx context.Context, cmd module.UpdateCommand) error {
	t.update.Calls.Add(ctx, 1)
	start := time.Now()
	err := t.repo.Update(ctx, cmd)
	t.update.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.update.Failures.Add(ctx, 1)
	} else {
		t.update.Successes.Add(ctx, 1)
	}
	return err
}

func (t teleRepository) GetByUid(ctx context.Context, uid uuid.UUID) (module.DistributionRequest, error) {
	t.getByUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByUid(ctx, uid)
	t.getByUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByUid.Failures.Add(ctx, 1)
	} else {
		t.getByUid.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetByDUid(ctx context.Context, distributorUid uuid.UUID) ([]module.DistributionRequest, error) {
	t.getByDUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByDUid(ctx, distributorUid)
	t.getByDUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByDUid.Failures.Add(ctx, 1)
	} else {
		t.getByDUid.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]module.DistributionRequest, error) {
	t.getByCRUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByCRUid(ctx, collectionRequestUid)
	t.getByCRUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByCRUid.Failures.Add(ctx, 1)
	} else {
		t.getByCRUid.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetByTDUid(ctx context.Context, transformedDataUid uuid.UUID) ([]module.DistributionRequest, error) {
	t.getByTDUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByTDUid(ctx, transformedDataUid)
	t.getByTDUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByTDUid.Failures.Add(ctx, 1)
	} else {
		t.getByTDUid.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// Repository is the exported, telemetry-instrumented adapter for the
// distributionrequest module. It embeds teleRepository so all six
// module.Repository methods are promoted and the struct satisfies the interface
// directly. Construct with NewRepository; do not instantiate directly.
type Repository struct {
	teleRepository
}

// NewRepository builds a telemetry-instrumented pgx repository.
//
// pool is the pgx connection pool shared across the application.
// meter is the OTel Meter used to register per-method call counters and
// duration histograms under the "baasparse.distributionrequest.repository.*"
// namespace.
//
// Returns an error if any OTel instrument fails to register, which typically
// indicates a meter provider misconfiguration.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.distributionrequest.repository."

	create, err := telemetry.NewMethodInstruments(meter, prefix+"create")
	if err != nil {
		return Repository{}, err
	}
	update, err := telemetry.NewMethodInstruments(meter, prefix+"update")
	if err != nil {
		return Repository{}, err
	}
	getByUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_uid")
	if err != nil {
		return Repository{}, err
	}
	getByDUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_d_uid")
	if err != nil {
		return Repository{}, err
	}
	getByCRUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_cr_uid")
	if err != nil {
		return Repository{}, err
	}
	getByTDUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_td_uid")
	if err != nil {
		return Repository{}, err
	}

	return Repository{
		teleRepository: teleRepository{
			repo:       repository{pool: pool},
			create:     create,
			update:     update,
			getByUid:   getByUid,
			getByDUid:  getByDUid,
			getByCRUid: getByCRUid,
			getByTDUid: getByTDUid,
		},
	}, nil
}
