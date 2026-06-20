package transformer

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

	module "github.com/pgvanniekerk/baasparse/internal/module/transformer"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so every
// database call is automatically observed by OTel metrics.
// T_TRANSFORMER is read-only — no transaction handling is needed here.
type repository struct {
	// pool is the pgx connection pool used for all read operations.
	pool *pgxpool.Pool
}

func (r repository) GetByUid(ctx context.Context, uid uuid.UUID) (module.Transformer, error) {
	t, err := scanRow(r.pool.QueryRow(ctx, sqlGetByUid, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.Transformer{}, fmt.Errorf("%w: %s", module.ErrNotFound, uid)
	}
	if err != nil {
		return module.Transformer{}, fmt.Errorf("transformer: get by uid: %w", err)
	}
	return t, nil
}

func (r repository) GetByFromDtUidAndToDtUid(ctx context.Context, fromDtUid, toDtUid uuid.UUID) (module.Transformer, error) {
	t, err := scanRow(r.pool.QueryRow(ctx, sqlGetByFromAndTo, fromDtUid, toDtUid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.Transformer{}, fmt.Errorf("%w: from=%s to=%s", module.ErrNotFound, fromDtUid, toDtUid)
	}
	if err != nil {
		return module.Transformer{}, fmt.Errorf("transformer: get by from/to data type: %w", err)
	}
	return t, nil
}

// scanRow scans a single T_TRANSFORMER row from a pgx.Row.
// T_TRANSFORMATION_SCHEMA is a JSONB column scanned into json.RawMessage so it
// is preserved verbatim and can be passed directly to a JSON Schema validator.
// T_DESCRIPTION is nullable and scanned into a *string (nil when NULL).
// Column order must match the SELECT list in sql.go.
func scanRow(row pgx.Row) (module.Transformer, error) {
	var t module.Transformer
	var schema json.RawMessage
	if err := row.Scan(
		&t.Uid,
		&t.FromDataTypeUid,
		&t.ToDataTypeUid,
		&schema,
		&t.Description,
	); err != nil {
		return module.Transformer{}, err
	}
	t.TransformationSchema = schema
	return t, nil
}

// ── telemetry layer ───────────────────────────────────────────────────────────

// teleRepository wraps repository and records OTel metrics on every call.
// Each method increments a calls counter, delegates to the pgx layer, records
// elapsed duration, then increments successes or failures.
type teleRepository struct {
	repo                     repository
	getByUid                 telemetry.MethodInstruments
	getByFromDtUidAndToDtUid telemetry.MethodInstruments
}

func (t teleRepository) GetByUid(ctx context.Context, uid uuid.UUID) (module.Transformer, error) {
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

func (t teleRepository) GetByFromDtUidAndToDtUid(ctx context.Context, fromDtUid, toDtUid uuid.UUID) (module.Transformer, error) {
	t.getByFromDtUidAndToDtUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByFromDtUidAndToDtUid(ctx, fromDtUid, toDtUid)
	t.getByFromDtUidAndToDtUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByFromDtUidAndToDtUid.Failures.Add(ctx, 1)
	} else {
		t.getByFromDtUidAndToDtUid.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// Repository is the exported, telemetry-instrumented adapter for the transformer
// module. It embeds teleRepository so both module.Repository methods are
// promoted and the struct satisfies the interface directly.
// Construct with NewRepository; do not instantiate directly.
type Repository struct {
	teleRepository
}

// NewRepository builds a telemetry-instrumented pgx repository.
//
// pool is the pgx connection pool shared across the application.
// meter is the OTel Meter used to register per-method call counters and
// duration histograms under the "baasparse.transformer.repository.*" namespace.
//
// Returns an error if any OTel instrument fails to register, which typically
// indicates a meter provider misconfiguration.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.transformer.repository."

	getByUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_uid")
	if err != nil {
		return Repository{}, err
	}
	getByFromDtUidAndToDtUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_from_dt_uid_and_to_dt_uid")
	if err != nil {
		return Repository{}, err
	}

	return Repository{
		teleRepository: teleRepository{
			repo:                     repository{pool: pool},
			getByUid:                 getByUid,
			getByFromDtUidAndToDtUid: getByFromDtUidAndToDtUid,
		},
	}, nil
}
