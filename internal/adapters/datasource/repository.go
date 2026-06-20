package datasource

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
	module "github.com/pgvanniekerk/baasparse/internal/module/datasource"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so every
// database call is automatically observed by OTel metrics.
type repository struct {
	// pool is used for read operations. Write operations (Create) extract a
	// pgx.Tx from the context via db.TxFromContext instead.
	pool *pgxpool.Pool
}

// Create inserts a new data source row using the pgx.Tx in ctx.
// Returns db.ErrNoTransaction if no transaction is present — the txService
// wrapper in the module layer is responsible for providing it.
func (r repository) Create(ctx context.Context, ds module.DataSource) (uuid.UUID, error) {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return uuid.Nil, db.ErrNoTransaction
	}

	// DS_CONNECTION_SPECIFICATION is JSONB. Passing json.RawMessage ensures pgx
	// sends the bytes using the JSON codec rather than treating them as BYTEA.
	var uid uuid.UUID
	err := tx.QueryRow(ctx, sqlCreate,
		ds.DataSourceTypeUid,
		ds.Name,
		ds.ConnectionSpecification,
		ds.TransactionAuditUid,
	).Scan(&uid)
	if err != nil {
		return uuid.Nil, fmt.Errorf("datasource: create: %w", err)
	}
	return uid, nil
}

func (r repository) GetByUid(ctx context.Context, uid uuid.UUID) (module.DataSource, error) {
	ds, err := scanRow(r.pool.QueryRow(ctx, sqlGetByUid, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.DataSource{}, fmt.Errorf("%w: %s", module.ErrNotFound, uid)
	}
	if err != nil {
		return module.DataSource{}, fmt.Errorf("datasource: get by uid: %w", err)
	}
	return ds, nil
}

func (r repository) GetByName(ctx context.Context, name string) (module.DataSource, error) {
	ds, err := scanRow(r.pool.QueryRow(ctx, sqlGetByName, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.DataSource{}, fmt.Errorf("%w: %s", module.ErrNotFound, name)
	}
	if err != nil {
		return module.DataSource{}, fmt.Errorf("datasource: get by name: %w", err)
	}
	return ds, nil
}

// Update modifies DS_NAME and DS_CONNECTION_SPECIFICATION for an existing row
// using the pgx.Tx in ctx. Returns module.ErrNotFound if no row matches.
func (r repository) Update(ctx context.Context, cmd module.UpdateCommand) error {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return db.ErrNoTransaction
	}

	// DS_CONNECTION_SPECIFICATION is JSONB. Passing json.RawMessage ensures pgx
	// sends the bytes using the JSON codec rather than treating them as BYTEA.
	var uid uuid.UUID
	err := tx.QueryRow(ctx, sqlUpdate,
		cmd.Name,
		cmd.ConnectionSpecification,
		cmd.Uid,
	).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", module.ErrNotFound, cmd.Uid)
	}
	if err != nil {
		return fmt.Errorf("datasource: update: %w", err)
	}
	return nil
}

func (r repository) GetAll(ctx context.Context) ([]module.DataSource, error) {
	rows, err := r.pool.Query(ctx, sqlGetAll)
	if err != nil {
		return nil, fmt.Errorf("datasource: get all: %w", err)
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

// scanRow scans a single DS_DATA_SOURCE row. Column order must match sql.go.
// DS_CONNECTION_SPECIFICATION is scanned into json.RawMessage to preserve the
// JSON bytes verbatim.
func scanRow(row pgx.Row) (module.DataSource, error) {
	var ds module.DataSource
	var spec json.RawMessage
	if err := row.Scan(
		&ds.Uid,
		&ds.DataSourceTypeUid,
		&ds.Name,
		&spec,
		&ds.TransactionAuditUid,
	); err != nil {
		return module.DataSource{}, err
	}
	ds.ConnectionSpecification = spec
	return ds, nil
}

// scanRows iterates a pgx.Rows result set into a slice of module.DataSource.
// rows.Err() is checked after iteration to surface stream errors.
func scanRows(rows pgx.Rows) ([]module.DataSource, error) {
	var results []module.DataSource
	for rows.Next() {
		ds, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("datasource: scan row: %w", err)
		}
		results = append(results, ds)
	}
	return results, rows.Err()
}

// ── telemetry layer ───────────────────────────────────────────────────────────

// teleRepository wraps repository and records OTel metrics on every call.
// Each method increments a calls counter, delegates to the pgx layer, records
// elapsed duration, then increments successes or failures.
type teleRepository struct {
	repo      repository
	create    telemetry.MethodInstruments
	update    telemetry.MethodInstruments
	getByUid  telemetry.MethodInstruments
	getByName telemetry.MethodInstruments
	getAll    telemetry.MethodInstruments
}

func (t teleRepository) Create(ctx context.Context, ds module.DataSource) (uuid.UUID, error) {
	t.create.Calls.Add(ctx, 1)
	start := time.Now()
	uid, err := t.repo.Create(ctx, ds)
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

func (t teleRepository) GetByUid(ctx context.Context, uid uuid.UUID) (module.DataSource, error) {
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

func (t teleRepository) GetByName(ctx context.Context, name string) (module.DataSource, error) {
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

func (t teleRepository) GetAll(ctx context.Context) ([]module.DataSource, error) {
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

// Repository is the exported, telemetry-instrumented adapter for the datasource
// module. It embeds teleRepository so all four module.Repository methods are
// promoted. Construct with NewRepository; do not instantiate directly.
type Repository struct {
	teleRepository
}

// NewRepository builds a telemetry-instrumented pgx repository.
//
// pool is the pgx connection pool shared across the application.
// meter is the OTel Meter used to register per-method call counters and
// duration histograms under the "baasparse.datasource.repository.*" namespace.
//
// Returns an error if any OTel instrument fails to register.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.datasource.repository."

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
			create:    create,
			update:    update,
			getByUid:  getByUid,
			getByName: getByName,
			getAll:    getAll,
		},
	}, nil
}
