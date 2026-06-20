package datasourcetype

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel/metric"

	module "github.com/pgvanniekerk/baasparse/internal/module/datasourcetype"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so every
// database call is automatically observed by OTel metrics.
// DST_TYPE is read-only — no transaction handling is needed here.
type repository struct {
	// pool is the pgx connection pool used for all read operations.
	pool *pgxpool.Pool
}

func (r repository) GetByUid(ctx context.Context, uid uuid.UUID) (module.DataSourceType, error) {
	dst, err := scanRow(r.pool.QueryRow(ctx, sqlGetByUid, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.DataSourceType{}, fmt.Errorf("%w: %s", module.ErrNotFound, uid)
	}
	if err != nil {
		return module.DataSourceType{}, fmt.Errorf("datasourcetype: get by uid: %w", err)
	}
	return dst, nil
}

func (r repository) GetByName(ctx context.Context, name string) (module.DataSourceType, error) {
	dst, err := scanRow(r.pool.QueryRow(ctx, sqlGetByName, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.DataSourceType{}, fmt.Errorf("%w: %s", module.ErrNotFound, name)
	}
	if err != nil {
		return module.DataSourceType{}, fmt.Errorf("datasourcetype: get by name: %w", err)
	}
	return dst, nil
}

func (r repository) GetAll(ctx context.Context) ([]module.DataSourceType, error) {
	rows, err := r.pool.Query(ctx, sqlGetAll)
	if err != nil {
		return nil, fmt.Errorf("datasourcetype: get all: %w", err)
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

// scanRow scans a single DST_TYPE row from any pgx.Row (returned by either
// pool.QueryRow for single-row lookups or rows.Scan for multi-row iteration).
// The three JSONB schema columns are scanned into json.RawMessage so they are
// preserved verbatim and can be passed directly to a JSON Schema validator
// without a round-trip through map[string]any.
func scanRow(row pgx.Row) (module.DataSourceType, error) {
	var dst module.DataSourceType
	var connSchema, collSchema, distSchema json.RawMessage
	// Column order must match the SELECT list in sql.go.
	if err := row.Scan(
		&dst.Uid,
		&dst.Name,
		&connSchema,
		&collSchema,
		&distSchema,
	); err != nil {
		return module.DataSourceType{}, err
	}
	dst.ConnectionSchema = connSchema
	dst.CollectionSchema = collSchema
	dst.DistributionSchema = distSchema
	return dst, nil
}

// scanRows maps a full pgx.Rows result set into a slice of DataSourceType
// by calling scanRow for each row. rows.Err() is checked after iteration to
// surface any error that terminated the stream early.
func scanRows(rows pgx.Rows) ([]module.DataSourceType, error) {
	var results []module.DataSourceType
	for rows.Next() {
		dst, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("datasourcetype: scan row: %w", err)
		}
		results = append(results, dst)
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
	getByName telemetry.MethodInstruments
	getAll    telemetry.MethodInstruments
}

func (t teleRepository) GetByUid(ctx context.Context, uid uuid.UUID) (module.DataSourceType, error) {
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

func (t teleRepository) GetByName(ctx context.Context, name string) (module.DataSourceType, error) {
	t.getByName.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByName(ctx, name)
	t.getByName.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByName.Failures.Add(ctx, 1)
	} else {
		t.getByName.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetAll(ctx context.Context) ([]module.DataSourceType, error) {
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

// Repository is the exported, telemetry-instrumented adapter for the
// datasourcetype module. It embeds teleRepository so all three module.Repository
// methods are promoted and the struct satisfies the interface directly.
// Construct with NewRepository; do not instantiate directly.
type Repository struct {
	teleRepository
}

// NewRepository builds a telemetry-instrumented pgx repository.
//
// pool is the pgx connection pool shared across the application.
// meter is the OTel Meter used to register per-method call counters and
// duration histograms under the "baasparse.datasourcetype.repository.*" namespace.
//
// Returns an error if any OTel instrument fails to register, which typically
// indicates a meter provider misconfiguration.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.datasourcetype.repository."

	getByUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_uid")
	if err != nil {
		return Repository{}, err
	}
	getByName, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_name")
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
			getByName: getByName,
			getAll:    getAll,
		},
	}, nil
}
