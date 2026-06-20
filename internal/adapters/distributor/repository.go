package distributor

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
	module "github.com/pgvanniekerk/baasparse/internal/module/distributor"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so every
// database call is automatically observed by OTel metrics.
type repository struct {
	// pool is used for read operations (GetByUid, GetByPUid, GetAll,
	// ExistsByPUidAndName). Write operations (Create, Update) extract a pgx.Tx
	// from the context via db.TxFromContext instead.
	pool *pgxpool.Pool
}

// Create inserts a new distributor row using the pgx.Tx in ctx.
// Returns db.ErrNoTransaction if no transaction is present — the txService
// wrapper in the service layer is responsible for providing it.
func (r repository) Create(ctx context.Context, d module.Distributor) (uuid.UUID, error) {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return uuid.Nil, db.ErrNoTransaction
	}

	var uid uuid.UUID
	err := tx.QueryRow(ctx, sqlCreate,
		d.PipelineUid,
		d.DataSourceUid,
		d.Name,
		d.DistributionSpecification,
		nullableUUID(d.TransformerUid),
		nullableJSON(d.TransformationSpecification),
		string(d.Status),
		d.TransactionAuditUid,
	).Scan(&uid)
	if err != nil {
		return uuid.Nil, fmt.Errorf("distributor: create: %w", err)
	}
	return uid, nil
}

// Update modifies the data source, name, specifications, transformer, and
// status for an existing row using the pgx.Tx in ctx. Returns module.ErrNotFound
// if no row matches.
func (r repository) Update(ctx context.Context, cmd module.UpdateCommand) error {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return db.ErrNoTransaction
	}

	var uid uuid.UUID
	err := tx.QueryRow(ctx, sqlUpdate,
		cmd.DataSourceUid,
		cmd.Name,
		cmd.DistributionSpecification,
		nullableUUID(cmd.TransformerUid),
		nullableJSON(cmd.TransformationSpecification),
		string(cmd.Status),
		cmd.Uid,
	).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", module.ErrNotFound, cmd.Uid)
	}
	if err != nil {
		return fmt.Errorf("distributor: update: %w", err)
	}
	return nil
}

func (r repository) GetByUid(ctx context.Context, uid uuid.UUID) (module.Distributor, error) {
	d, err := scanRow(r.pool.QueryRow(ctx, sqlGetByUid, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.Distributor{}, fmt.Errorf("%w: %s", module.ErrNotFound, uid)
	}
	if err != nil {
		return module.Distributor{}, fmt.Errorf("distributor: get by uid: %w", err)
	}
	return d, nil
}

func (r repository) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]module.Distributor, error) {
	rows, err := r.pool.Query(ctx, sqlGetByPUid, pipelineUid)
	if err != nil {
		return nil, fmt.Errorf("distributor: get by pipeline uid: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) GetAll(ctx context.Context, status *module.Status) ([]module.Distributor, error) {
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
		return nil, fmt.Errorf("distributor: get all: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) ExistsByPUidAndName(ctx context.Context, pipelineUid uuid.UUID, name string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, sqlExistsByPUidAndName, pipelineUid, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("distributor: exists by pipeline uid and name: %w", err)
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

// scanRow scans a single D_DISTRIBUTOR row from any pgx.Row (returned by either
// pool.QueryRow for single-row lookups or rows.Scan for multi-row iteration).
// The two JSONB columns are scanned into json.RawMessage (nil when NULL).
// D_T_UID is nullable and scanned via uuid.NullUUID into an optional *uuid.UUID.
// D_STATUS is scanned into a string first and then converted to module.Status.
// Column order must match the SELECT list in sql.go.
func scanRow(row pgx.Row) (module.Distributor, error) {
	var d module.Distributor
	var (
		distributionSpec json.RawMessage
		transformSpec    json.RawMessage
		transformerUID   uuid.NullUUID
		status           string
	)
	if err := row.Scan(
		&d.Uid,
		&d.PipelineUid,
		&d.DataSourceUid,
		&d.Name,
		&distributionSpec,
		&transformerUID,
		&transformSpec,
		&status,
		&d.TransactionAuditUid,
	); err != nil {
		return module.Distributor{}, err
	}
	d.DistributionSpecification = distributionSpec
	if transformerUID.Valid {
		id := transformerUID.UUID
		d.TransformerUid = &id
	}
	d.TransformationSpecification = transformSpec
	d.Status = module.Status(status)
	return d, nil
}

// scanRows maps a full pgx.Rows result set into a slice of Distributor by
// calling scanRow for each row. rows.Err() is checked after iteration to
// surface any error that terminated the stream early.
func scanRows(rows pgx.Rows) ([]module.Distributor, error) {
	var results []module.Distributor
	for rows.Next() {
		d, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("distributor: scan row: %w", err)
		}
		results = append(results, d)
	}
	return results, rows.Err()
}

// ── telemetry layer ───────────────────────────────────────────────────────────

// teleRepository wraps repository and records OTel metrics on every call.
// Each method increments a calls counter, delegates to the pgx layer, records
// elapsed duration, then increments successes or failures.
type teleRepository struct {
	repo                repository
	create              telemetry.MethodInstruments
	update              telemetry.MethodInstruments
	getByUid            telemetry.MethodInstruments
	getByPUid           telemetry.MethodInstruments
	getAll              telemetry.MethodInstruments
	existsByPUidAndName telemetry.MethodInstruments
}

func (t teleRepository) Create(ctx context.Context, d module.Distributor) (uuid.UUID, error) {
	t.create.Calls.Add(ctx, 1)
	start := time.Now()
	uid, err := t.repo.Create(ctx, d)
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

func (t teleRepository) GetByUid(ctx context.Context, uid uuid.UUID) (module.Distributor, error) {
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

func (t teleRepository) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]module.Distributor, error) {
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

func (t teleRepository) GetAll(ctx context.Context, status *module.Status) ([]module.Distributor, error) {
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

func (t teleRepository) ExistsByPUidAndName(ctx context.Context, pipelineUid uuid.UUID, name string) (bool, error) {
	t.existsByPUidAndName.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.ExistsByPUidAndName(ctx, pipelineUid, name)
	t.existsByPUidAndName.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.existsByPUidAndName.Failures.Add(ctx, 1)
	} else {
		t.existsByPUidAndName.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// Repository is the exported, telemetry-instrumented adapter for the distributor
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
// duration histograms under the "baasparse.distributor.repository.*" namespace.
//
// Returns an error if any OTel instrument fails to register, which typically
// indicates a meter provider misconfiguration.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.distributor.repository."

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
	existsByPUidAndName, err := telemetry.NewMethodInstruments(meter, prefix+"exists_by_p_uid_and_name")
	if err != nil {
		return Repository{}, err
	}

	return Repository{
		teleRepository: teleRepository{
			repo:                repository{pool: pool},
			create:              create,
			update:              update,
			getByUid:            getByUid,
			getByPUid:           getByPUid,
			getAll:              getAll,
			existsByPUidAndName: existsByPUidAndName,
		},
	}, nil
}
