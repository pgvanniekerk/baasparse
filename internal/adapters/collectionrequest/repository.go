package collectionrequest

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
	module "github.com/pgvanniekerk/baasparse/internal/module/collectionrequest"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so every
// database call is automatically observed by OTel metrics.
type repository struct {
	// pool is used for read operations (GetByUid, GetByPUid, GetByCUid,
	// ExistsByPUidAndCUidAndHashKey). Create extracts a pgx.Tx from the context
	// via db.TxFromContext instead, leaving the commit boundary to the caller.
	pool *pgxpool.Pool
}

// Create inserts a new collection request using the pgx.Tx in ctx.
// Returns db.ErrNoTransaction if no transaction is present — the caller (e.g.
// the collection engine) is responsible for beginning and committing it.
func (r repository) Create(ctx context.Context, cr module.CollectionRequest) (module.CollectionRequest, error) {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return module.CollectionRequest{}, db.ErrNoTransaction
	}

	// CR_DATA is a BYTEA column; pgx sends the []byte using the binary codec.
	// CR_FILE_NAME is nullable; a nil *string is encoded as SQL NULL.
	// CR_UID and CR_TIMESTAMPTZ are assigned by the database and returned here.
	err := tx.QueryRow(ctx, sqlCreate,
		cr.PipelineUid,
		cr.CollectorUid,
		cr.HashKey,
		cr.FileName,
		cr.Data,
	).Scan(&cr.Uid, &cr.TimestampTz)
	if err != nil {
		return module.CollectionRequest{}, fmt.Errorf("collectionrequest: create: %w", err)
	}
	return cr, nil
}

func (r repository) GetByUid(ctx context.Context, uid uuid.UUID) (module.CollectionRequest, error) {
	cr, err := scanRow(r.pool.QueryRow(ctx, sqlGetByUid, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.CollectionRequest{}, fmt.Errorf("%w: %s", module.ErrNotFound, uid)
	}
	if err != nil {
		return module.CollectionRequest{}, fmt.Errorf("collectionrequest: get by uid: %w", err)
	}
	return cr, nil
}

func (r repository) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]module.CollectionRequest, error) {
	rows, err := r.pool.Query(ctx, sqlGetByPUid, pipelineUid)
	if err != nil {
		return nil, fmt.Errorf("collectionrequest: get by pipeline uid: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]module.CollectionRequest, error) {
	rows, err := r.pool.Query(ctx, sqlGetByCUid, collectorUid)
	if err != nil {
		return nil, fmt.Errorf("collectionrequest: get by collector uid: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) ExistsByPUidAndCUidAndHashKey(ctx context.Context, pipelineUid, collectorUid uuid.UUID, hashKey string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, sqlExistsByPUidAndCUidAndHashKey, pipelineUid, collectorUid, hashKey).Scan(&exists); err != nil {
		return false, fmt.Errorf("collectionrequest: exists by pipeline/collector/hash: %w", err)
	}
	return exists, nil
}

// scanRow scans a single CR_COLLECTION_REQUEST row from any pgx.Row (returned
// by either pool.QueryRow for single-row lookups or rows.Scan for multi-row
// iteration). CR_FILE_NAME is nullable and scanned into a *string (nil when
// NULL); CR_DATA is a BYTEA scanned into a []byte. Column order must match the
// SELECT list in sql.go.
func scanRow(row pgx.Row) (module.CollectionRequest, error) {
	var cr module.CollectionRequest
	if err := row.Scan(
		&cr.Uid,
		&cr.PipelineUid,
		&cr.CollectorUid,
		&cr.HashKey,
		&cr.FileName,
		&cr.Data,
		&cr.TimestampTz,
	); err != nil {
		return module.CollectionRequest{}, err
	}
	return cr, nil
}

// scanRows maps a full pgx.Rows result set into a slice of CollectionRequest by
// calling scanRow for each row. rows.Err() is checked after iteration to
// surface any error that terminated the stream early.
func scanRows(rows pgx.Rows) ([]module.CollectionRequest, error) {
	var results []module.CollectionRequest
	for rows.Next() {
		cr, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("collectionrequest: scan row: %w", err)
		}
		results = append(results, cr)
	}
	return results, rows.Err()
}

// ── telemetry layer ───────────────────────────────────────────────────────────

// teleRepository wraps repository and records OTel metrics on every call.
// Each method increments a calls counter, delegates to the pgx layer, records
// elapsed duration, then increments successes or failures.
type teleRepository struct {
	repo                          repository
	create                        telemetry.MethodInstruments
	getByUid                      telemetry.MethodInstruments
	getByPUid                     telemetry.MethodInstruments
	getByCUid                     telemetry.MethodInstruments
	existsByPUidAndCUidAndHashKey telemetry.MethodInstruments
}

func (t teleRepository) Create(ctx context.Context, cr module.CollectionRequest) (module.CollectionRequest, error) {
	t.create.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.Create(ctx, cr)
	t.create.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.create.Failures.Add(ctx, 1)
	} else {
		t.create.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetByUid(ctx context.Context, uid uuid.UUID) (module.CollectionRequest, error) {
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

func (t teleRepository) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]module.CollectionRequest, error) {
	t.getByPUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByPUid(ctx, pipelineUid)
	t.getByPUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByPUid.Failures.Add(ctx, 1)
	} else {
		t.getByPUid.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]module.CollectionRequest, error) {
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

func (t teleRepository) ExistsByPUidAndCUidAndHashKey(ctx context.Context, pipelineUid, collectorUid uuid.UUID, hashKey string) (bool, error) {
	t.existsByPUidAndCUidAndHashKey.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.ExistsByPUidAndCUidAndHashKey(ctx, pipelineUid, collectorUid, hashKey)
	t.existsByPUidAndCUidAndHashKey.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.existsByPUidAndCUidAndHashKey.Failures.Add(ctx, 1)
	} else {
		t.existsByPUidAndCUidAndHashKey.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// Repository is the exported, telemetry-instrumented adapter for the
// collectionrequest module. It embeds teleRepository so all five
// module.Repository methods are promoted and the struct satisfies the interface
// directly. Construct with NewRepository; do not instantiate directly.
type Repository struct {
	teleRepository
}

// NewRepository builds a telemetry-instrumented pgx repository.
//
// pool is the pgx connection pool shared across the application.
// meter is the OTel Meter used to register per-method call counters and
// duration histograms under the "baasparse.collectionrequest.repository.*"
// namespace.
//
// Returns an error if any OTel instrument fails to register, which typically
// indicates a meter provider misconfiguration.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.collectionrequest.repository."

	create, err := telemetry.NewMethodInstruments(meter, prefix+"create")
	if err != nil {
		return Repository{}, err
	}
	getByUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_uid")
	if err != nil {
		return Repository{}, err
	}
	getByPUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_p_uid")
	if err != nil {
		return Repository{}, err
	}
	getByCUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_c_uid")
	if err != nil {
		return Repository{}, err
	}
	existsByPUidAndCUidAndHashKey, err := telemetry.NewMethodInstruments(meter, prefix+"exists_by_p_uid_and_c_uid_and_hash_key")
	if err != nil {
		return Repository{}, err
	}

	return Repository{
		teleRepository: teleRepository{
			repo:                          repository{pool: pool},
			create:                        create,
			getByUid:                      getByUid,
			getByPUid:                     getByPUid,
			getByCUid:                     getByCUid,
			existsByPUidAndCUidAndHashKey: existsByPUidAndCUidAndHashKey,
		},
	}, nil
}
