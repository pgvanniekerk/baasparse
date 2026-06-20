package collector

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

	"github.com/pgvanniekerk/baasparse/internal/db"
	module "github.com/pgvanniekerk/baasparse/internal/module/collector"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so every
// database call is automatically observed by OTel metrics.
type repository struct {
	// pool is used for read operations (GetByUid, GetByPUid, GetAll,
	// ExistsByPUid). Write operations (Create, Update) extract a pgx.Tx from
	// the context via db.TxFromContext instead.
	pool *pgxpool.Pool
}

// Create inserts a new collector row using the pgx.Tx in ctx.
// Returns db.ErrNoTransaction if no transaction is present — the txService
// wrapper in the service layer is responsible for providing it.
func (r repository) Create(ctx context.Context, c module.Collector) (uuid.UUID, error) {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return uuid.Nil, db.ErrNoTransaction
	}

	var uid uuid.UUID
	err := tx.QueryRow(ctx, sqlCreate,
		c.PipelineUid,
		c.DataSourceUid,
		c.CollectionSpecification,
		nullableUUID(c.TransformerUid),
		nullableJSON(c.TransformationSpecification),
		string(c.Status),
		c.TransactionAuditUid,
	).Scan(&uid)
	if err != nil {
		return uuid.Nil, fmt.Errorf("collector: create: %w", err)
	}
	return uid, nil
}

// Update modifies the data source, specifications, transformer, and status for
// an existing row using the pgx.Tx in ctx. Returns module.ErrNotFound if no row
// matches.
func (r repository) Update(ctx context.Context, cmd module.UpdateCommand) error {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return db.ErrNoTransaction
	}

	var uid uuid.UUID
	err := tx.QueryRow(ctx, sqlUpdate,
		cmd.DataSourceUid,
		cmd.CollectionSpecification,
		nullableUUID(cmd.TransformerUid),
		nullableJSON(cmd.TransformationSpecification),
		string(cmd.Status),
		cmd.Uid,
	).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", module.ErrNotFound, cmd.Uid)
	}
	if err != nil {
		return fmt.Errorf("collector: update: %w", err)
	}
	return nil
}

func (r repository) GetByUid(ctx context.Context, uid uuid.UUID) (module.Collector, error) {
	c, err := scanRow(r.pool.QueryRow(ctx, sqlGetByUid, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.Collector{}, fmt.Errorf("%w: %s", module.ErrNotFound, uid)
	}
	if err != nil {
		return module.Collector{}, fmt.Errorf("collector: get by uid: %w", err)
	}
	return c, nil
}

func (r repository) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) (module.Collector, error) {
	c, err := scanRow(r.pool.QueryRow(ctx, sqlGetByPUid, pipelineUid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.Collector{}, fmt.Errorf("%w: %s", module.ErrNotFound, pipelineUid)
	}
	if err != nil {
		return module.Collector{}, fmt.Errorf("collector: get by pipeline uid: %w", err)
	}
	return c, nil
}

func (r repository) GetAll(ctx context.Context, status *module.Status) ([]module.Collector, error) {
	var (
		rows pgx.Rows
		err  error
	)
	if status != nil {
		rows, err = r.pool.Query(ctx, sqlGetAllByStatus, string(*status))
	} else {
		rows, err = r.pool.Query(ctx, sqlGetAll)
	}
	if err != nil {
		return nil, fmt.Errorf("collector: get all: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) ExistsByPUid(ctx context.Context, pipelineUid uuid.UUID) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, sqlExistsByPUid, pipelineUid).Scan(&exists); err != nil {
		return false, fmt.Errorf("collector: exists by pipeline uid: %w", err)
	}
	return exists, nil
}

// nullableUUID converts an optional UUID pointer into a query argument that
// encodes as SQL NULL when nil.
func nullableUUID(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

// nullableJSON converts an optional JSON document into a query argument that
// encodes as SQL NULL when empty, so an absent transformation specification
// stores as NULL rather than an empty JSONB value.
func nullableJSON(b json.RawMessage) any {
	if len(b) == 0 {
		return nil
	}
	return b
}

// scanRow scans a single C_COLLECTOR row from any pgx.Row (returned by either
// pool.QueryRow for single-row lookups or rows.Scan for multi-row iteration).
// The two JSONB columns are scanned into json.RawMessage (nil when NULL).
// C_T_UID is nullable and scanned via uuid.NullUUID into an optional *uuid.UUID.
// C_STATUS is scanned into a string first and then converted to module.Status.
// Column order must match the SELECT list in sql.go.
func scanRow(row pgx.Row) (module.Collector, error) {
	var c module.Collector
	var (
		collectionSpec json.RawMessage
		transformSpec  json.RawMessage
		transformerUID uuid.NullUUID
		status         string
	)
	if err := row.Scan(
		&c.Uid,
		&c.PipelineUid,
		&c.DataSourceUid,
		&collectionSpec,
		&transformerUID,
		&transformSpec,
		&status,
		&c.TransactionAuditUid,
	); err != nil {
		return module.Collector{}, err
	}
	c.CollectionSpecification = collectionSpec
	if transformerUID.Valid {
		id := transformerUID.UUID
		c.TransformerUid = &id
	}
	c.TransformationSpecification = transformSpec
	c.Status = module.Status(status)
	return c, nil
}

// scanRows maps a full pgx.Rows result set into a slice of Collector by calling
// scanRow for each row. rows.Err() is checked after iteration to surface any
// error that terminated the stream early.
func scanRows(rows pgx.Rows) ([]module.Collector, error) {
	var results []module.Collector
	for rows.Next() {
		c, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("collector: scan row: %w", err)
		}
		results = append(results, c)
	}
	return results, rows.Err()
}

// ── telemetry layer ───────────────────────────────────────────────────────────

// teleRepository wraps repository and records OTel metrics on every call.
// Each method increments a calls counter, delegates to the pgx layer, records
// elapsed duration, then increments successes or failures.
type teleRepository struct {
	repo         repository
	create       telemetry.MethodInstruments
	update       telemetry.MethodInstruments
	getByUid     telemetry.MethodInstruments
	getByPUid    telemetry.MethodInstruments
	getAll       telemetry.MethodInstruments
	existsByPUid telemetry.MethodInstruments
}

func (t teleRepository) Create(ctx context.Context, c module.Collector) (uuid.UUID, error) {
	t.create.Calls.Add(ctx, 1)
	start := time.Now()
	uid, err := t.repo.Create(ctx, c)
	t.create.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.create.Failures.Add(ctx, 1)
	} else {
		t.create.Successes.Add(ctx, 1)
	}
	return uid, err
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

func (t teleRepository) GetByUid(ctx context.Context, uid uuid.UUID) (module.Collector, error) {
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

func (t teleRepository) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) (module.Collector, error) {
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

func (t teleRepository) GetAll(ctx context.Context, status *module.Status) ([]module.Collector, error) {
	t.getAll.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetAll(ctx, status)
	t.getAll.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getAll.Failures.Add(ctx, 1)
	} else {
		t.getAll.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) ExistsByPUid(ctx context.Context, pipelineUid uuid.UUID) (bool, error) {
	t.existsByPUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.ExistsByPUid(ctx, pipelineUid)
	t.existsByPUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.existsByPUid.Failures.Add(ctx, 1)
	} else {
		t.existsByPUid.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// Repository is the exported, telemetry-instrumented adapter for the collector
// module. It embeds teleRepository so all six module.Repository methods are
// promoted and the struct satisfies the interface directly.
// Construct with NewRepository; do not instantiate directly.
type Repository struct {
	teleRepository
}

// NewRepository builds a telemetry-instrumented pgx repository.
//
// pool is the pgx connection pool shared across the application.
// meter is the OTel Meter used to register per-method call counters and
// duration histograms under the "baasparse.collector.repository.*" namespace.
//
// Returns an error if any OTel instrument fails to register, which typically
// indicates a meter provider misconfiguration.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.collector.repository."

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
	getByPUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_p_uid")
	if err != nil {
		return Repository{}, err
	}
	getAll, err := telemetry.NewMethodInstruments(meter, prefix+"get_all")
	if err != nil {
		return Repository{}, err
	}
	existsByPUid, err := telemetry.NewMethodInstruments(meter, prefix+"exists_by_p_uid")
	if err != nil {
		return Repository{}, err
	}

	return Repository{
		teleRepository: teleRepository{
			repo:         repository{pool: pool},
			create:       create,
			update:       update,
			getByUid:     getByUid,
			getByPUid:    getByPUid,
			getAll:       getAll,
			existsByPUid: existsByPUid,
		},
	}, nil
}
