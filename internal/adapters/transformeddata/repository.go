package transformeddata

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
	module "github.com/pgvanniekerk/baasparse/internal/module/transformeddata"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so every
// database call is automatically observed by OTel metrics.
type repository struct {
	// pool is used for read operations (GetByUid, GetByCRUid, GetByCUid).
	// Create extracts a pgx.Tx from the context via db.TxFromContext instead,
	// leaving the commit boundary to the caller.
	pool *pgxpool.Pool
}

// Create inserts a new transformed-data record using the pgx.Tx in ctx and
// returns the persisted record with the database-generated Uid and TimestampTz
// populated. Returns db.ErrNoTransaction if no transaction is present.
func (r repository) Create(ctx context.Context, td module.TransformedData) (module.TransformedData, error) {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return module.TransformedData{}, db.ErrNoTransaction
	}

	// TD_DATA is a BYTEA column; pgx sends the []byte using the binary codec.
	// TD_UID and TD_TIMESTAMPTZ are assigned by the database and returned here.
	err := tx.QueryRow(ctx, sqlCreate,
		td.CollectionRequestUid,
		td.CollectorUid,
		td.Data,
	).Scan(&td.Uid, &td.TimestampTz)
	if err != nil {
		return module.TransformedData{}, fmt.Errorf("transformeddata: create: %w", err)
	}
	return td, nil
}

func (r repository) GetByUid(ctx context.Context, uid uuid.UUID) (module.TransformedData, error) {
	td, err := scanRow(r.pool.QueryRow(ctx, sqlGetByUid, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.TransformedData{}, fmt.Errorf("%w: %s", module.ErrNotFound, uid)
	}
	if err != nil {
		return module.TransformedData{}, fmt.Errorf("transformeddata: get by uid: %w", err)
	}
	return td, nil
}

func (r repository) GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]module.TransformedData, error) {
	rows, err := r.pool.Query(ctx, sqlGetByCRUid, collectionRequestUid)
	if err != nil {
		return nil, fmt.Errorf("transformeddata: get by collection request uid: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]module.TransformedData, error) {
	rows, err := r.pool.Query(ctx, sqlGetByCUid, collectorUid)
	if err != nil {
		return nil, fmt.Errorf("transformeddata: get by collector uid: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

// scanRow scans a single TD_TRANSFORMED_DATA row from any pgx.Row (returned by
// either pool.QueryRow for single-row lookups or rows.Scan for multi-row
// iteration). TD_DATA is a BYTEA scanned into a []byte. Column order must match
// the SELECT list in sql.go.
func scanRow(row pgx.Row) (module.TransformedData, error) {
	var td module.TransformedData
	if err := row.Scan(
		&td.Uid,
		&td.CollectionRequestUid,
		&td.CollectorUid,
		&td.Data,
		&td.TimestampTz,
	); err != nil {
		return module.TransformedData{}, err
	}
	return td, nil
}

// scanRows maps a full pgx.Rows result set into a slice of TransformedData by
// calling scanRow for each row. rows.Err() is checked after iteration to
// surface any error that terminated the stream early.
func scanRows(rows pgx.Rows) ([]module.TransformedData, error) {
	var results []module.TransformedData
	for rows.Next() {
		td, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("transformeddata: scan row: %w", err)
		}
		results = append(results, td)
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
	getByUid   telemetry.MethodInstruments
	getByCRUid telemetry.MethodInstruments
	getByCUid  telemetry.MethodInstruments
}

func (t teleRepository) Create(ctx context.Context, td module.TransformedData) (module.TransformedData, error) {
	t.create.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.Create(ctx, td)
	t.create.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.create.Failures.Add(ctx, 1)
	} else {
		t.create.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetByUid(ctx context.Context, uid uuid.UUID) (module.TransformedData, error) {
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

func (t teleRepository) GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]module.TransformedData, error) {
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

func (t teleRepository) GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]module.TransformedData, error) {
	t.getByCUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByCUid(ctx, collectorUid)
	t.getByCUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByCUid.Failures.Add(ctx, 1)
	} else {
		t.getByCUid.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// Repository is the exported, telemetry-instrumented adapter for the
// transformeddata module. It embeds teleRepository so all four module.Repository
// methods are promoted and the struct satisfies the interface directly.
// Construct with NewRepository; do not instantiate directly.
type Repository struct {
	teleRepository
}

// NewRepository builds a telemetry-instrumented pgx repository.
//
// pool is the pgx connection pool shared across the application.
// meter is the OTel Meter used to register per-method call counters and
// duration histograms under the "baasparse.transformeddata.repository.*"
// namespace.
//
// Returns an error if any OTel instrument fails to register, which typically
// indicates a meter provider misconfiguration.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.transformeddata.repository."

	create, err := telemetry.NewMethodInstruments(meter, prefix+"create")
	if err != nil {
		return Repository{}, err
	}
	getByUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_uid")
	if err != nil {
		return Repository{}, err
	}
	getByCRUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_cr_uid")
	if err != nil {
		return Repository{}, err
	}
	getByCUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_c_uid")
	if err != nil {
		return Repository{}, err
	}

	return Repository{
		teleRepository: teleRepository{
			repo:       repository{pool: pool},
			create:     create,
			getByUid:   getByUid,
			getByCRUid: getByCRUid,
			getByCUid:  getByCUid,
		},
	}, nil
}
