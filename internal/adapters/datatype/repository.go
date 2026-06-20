package datatype

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	module "github.com/pgvanniekerk/baasparse/internal/module/datatype"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so every
// database call is automatically observed by OTel metrics.
// DT_DATA_TYPE is read-only — no transaction handling is needed here.
type repository struct {
	// pool is the pgx connection pool used for all read operations.
	pool *pgxpool.Pool
}

func (r repository) GetByUid(ctx context.Context, uid uuid.UUID) (module.DataType, error) {
	dt, err := scanRow(r.pool.QueryRow(ctx, sqlGetByUid, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.DataType{}, fmt.Errorf("%w: %s", module.ErrNotFound, uid)
	}
	if err != nil {
		return module.DataType{}, fmt.Errorf("datatype: get by uid: %w", err)
	}
	return dt, nil
}

func (r repository) GetByCode(ctx context.Context, code string) (module.DataType, error) {
	dt, err := scanRow(r.pool.QueryRow(ctx, sqlGetByCode, code))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.DataType{}, fmt.Errorf("%w: %s", module.ErrNotFound, code)
	}
	if err != nil {
		return module.DataType{}, fmt.Errorf("datatype: get by code: %w", err)
	}
	return dt, nil
}

func (r repository) GetAll(ctx context.Context) ([]module.DataType, error) {
	rows, err := r.pool.Query(ctx, sqlGetAll)
	if err != nil {
		return nil, fmt.Errorf("datatype: get all: %w", err)
	}
	defer rows.Close()
	result, err := scanRows(rows)
	if err != nil {
		return nil, err
	}
	if len(result) == 0 {
		return nil, module.ErrNotFound
	}
	return result, nil
}

// scanRow scans a single DT_DATA_TYPE row from any pgx.Row (returned by either
// pool.QueryRow for single-row lookups or rows.Scan for multi-row iteration).
// Column order must match the SELECT list in sql.go.
func scanRow(row pgx.Row) (module.DataType, error) {
	var dt module.DataType
	if err := row.Scan(
		&dt.Uid,
		&dt.Code,
	); err != nil {
		return module.DataType{}, err
	}
	return dt, nil
}

// scanRows maps a full pgx.Rows result set into a slice of DataType by calling
// scanRow for each row. rows.Err() is checked after iteration to surface any
// error that terminated the stream early.
func scanRows(rows pgx.Rows) ([]module.DataType, error) {
	var results []module.DataType
	for rows.Next() {
		dt, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("datatype: scan row: %w", err)
		}
		results = append(results, dt)
	}
	return results, rows.Err()
}

// ── telemetry layer ───────────────────────────────────────────────────────────

// teleRepository wraps repository and records OTel metrics on every call.
// Each method increments a calls counter, delegates to the pgx layer, records
// elapsed duration, then increments successes or failures.
type teleRepository struct {
	repo      repository
	getByUid  telemetry.MethodInstruments
	getByCode telemetry.MethodInstruments
	getAll    telemetry.MethodInstruments
}

func (t teleRepository) GetByUid(ctx context.Context, uid uuid.UUID) (module.DataType, error) {
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

func (t teleRepository) GetByCode(ctx context.Context, code string) (module.DataType, error) {
	t.getByCode.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByCode(ctx, code)
	t.getByCode.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByCode.Failures.Add(ctx, 1)
	} else {
		t.getByCode.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetAll(ctx context.Context) ([]module.DataType, error) {
	t.getAll.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetAll(ctx)
	t.getAll.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getAll.Failures.Add(ctx, 1)
	} else {
		t.getAll.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// Repository is the exported, telemetry-instrumented adapter for the datatype
// module. It embeds teleRepository so all three module.Repository methods are
// promoted and the struct satisfies the interface directly.
// Construct with NewRepository; do not instantiate directly.
type Repository struct {
	teleRepository
}

// NewRepository builds a telemetry-instrumented pgx repository.
//
// pool is the pgx connection pool shared across the application.
// meter is the OTel Meter used to register per-method call counters and
// duration histograms under the "baasparse.datatype.repository.*" namespace.
//
// Returns an error if any OTel instrument fails to register, which typically
// indicates a meter provider misconfiguration.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.datatype.repository."

	getByUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_uid")
	if err != nil {
		return Repository{}, err
	}
	getByCode, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_code")
	if err != nil {
		return Repository{}, err
	}
	getAll, err := telemetry.NewMethodInstruments(meter, prefix+"get_all")
	if err != nil {
		return Repository{}, err
	}

	return Repository{
		teleRepository: teleRepository{
			repo:      repository{pool: pool},
			getByUid:  getByUid,
			getByCode: getByCode,
			getAll:    getAll,
		},
	}, nil
}
