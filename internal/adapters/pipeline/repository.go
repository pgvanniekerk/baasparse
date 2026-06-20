package pipeline

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
	module "github.com/pgvanniekerk/baasparse/internal/module/pipeline"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── pgx layer ────────────────────────────────────────────────────────────────

// repository is the raw pgx implementation of module.Repository.
// It is unexported; all external access goes through teleRepository so every
// database call is automatically observed by OTel metrics.
type repository struct {
	// pool is used for read operations (GetByUid, GetByNameLike, GetAll,
	// ExistsByName). Write operations (Create, Update) extract a pgx.Tx from
	// the context via db.TxFromContext instead.
	pool *pgxpool.Pool
}

// Create inserts a new pipeline row using the pgx.Tx in ctx.
// Returns db.ErrNoTransaction if no transaction is present — the txService
// wrapper in the service layer is responsible for providing it.
func (r repository) Create(ctx context.Context, p module.Pipeline) (uuid.UUID, error) {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return uuid.Nil, db.ErrNoTransaction
	}

	var uid uuid.UUID
	err := tx.QueryRow(ctx, sqlCreate,
		p.Name,
		p.Description,
		string(p.Status),
		p.TransactionAuditUid,
	).Scan(&uid)
	if err != nil {
		return uuid.Nil, fmt.Errorf("pipeline: create: %w", err)
	}
	return uid, nil
}

// Update modifies P_NAME, P_DESCRIPTION, and P_STATUS for an existing row using
// the pgx.Tx in ctx. Returns module.ErrNotFound if no row matches.
func (r repository) Update(ctx context.Context, cmd module.UpdateCommand) error {
	tx, ok := db.TxFromContext(ctx)
	if !ok {
		return db.ErrNoTransaction
	}

	var uid uuid.UUID
	err := tx.QueryRow(ctx, sqlUpdate,
		cmd.Name,
		cmd.Description,
		string(cmd.Status),
		cmd.Uid,
	).Scan(&uid)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", module.ErrNotFound, cmd.Uid)
	}
	if err != nil {
		return fmt.Errorf("pipeline: update: %w", err)
	}
	return nil
}

func (r repository) GetByUid(ctx context.Context, uid uuid.UUID) (module.Pipeline, error) {
	p, err := scanRow(r.pool.QueryRow(ctx, sqlGetByUid, uid))
	if errors.Is(err, pgx.ErrNoRows) {
		return module.Pipeline{}, fmt.Errorf("%w: %s", module.ErrNotFound, uid)
	}
	if err != nil {
		return module.Pipeline{}, fmt.Errorf("pipeline: get by uid: %w", err)
	}
	return p, nil
}

func (r repository) GetByNameLike(ctx context.Context, pattern string) ([]module.Pipeline, error) {
	rows, err := r.pool.Query(ctx, sqlGetByNameLike, pattern)
	if err != nil {
		return nil, fmt.Errorf("pipeline: get by name like: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) GetAll(ctx context.Context, status *module.Status) ([]module.Pipeline, error) {
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
		return nil, fmt.Errorf("pipeline: get all: %w", err)
	}
	defer rows.Close()
	return scanRows(rows)
}

func (r repository) ExistsByName(ctx context.Context, name string) (bool, error) {
	var exists bool
	if err := r.pool.QueryRow(ctx, sqlExistsByName, name).Scan(&exists); err != nil {
		return false, fmt.Errorf("pipeline: exists by name: %w", err)
	}
	return exists, nil
}

// scanRow scans a single P_PIPELINE row from any pgx.Row (returned by either
// pool.QueryRow for single-row lookups or rows.Scan for multi-row iteration).
// P_DESCRIPTION is nullable and scanned into a *string (nil when NULL). P_STATUS
// is scanned into a string first and then converted to module.Status. Column
// order must match the SELECT list in sql.go.
func scanRow(row pgx.Row) (module.Pipeline, error) {
	var p module.Pipeline
	var status string
	if err := row.Scan(
		&p.Uid,
		&p.Name,
		&p.Description,
		&status,
		&p.TransactionAuditUid,
	); err != nil {
		return module.Pipeline{}, err
	}
	p.Status = module.Status(status)
	return p, nil
}

// scanRows maps a full pgx.Rows result set into a slice of Pipeline by calling
// scanRow for each row. rows.Err() is checked after iteration to surface any
// error that terminated the stream early.
func scanRows(rows pgx.Rows) ([]module.Pipeline, error) {
	var results []module.Pipeline
	for rows.Next() {
		p, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("pipeline: scan row: %w", err)
		}
		results = append(results, p)
	}
	return results, rows.Err()
}

// ── telemetry layer ───────────────────────────────────────────────────────────

// teleRepository wraps repository and records OTel metrics on every call.
// Each method increments a calls counter, delegates to the pgx layer, records
// elapsed duration, then increments successes or failures.
type teleRepository struct {
	repo          repository
	create        telemetry.MethodInstruments
	update        telemetry.MethodInstruments
	getByUid      telemetry.MethodInstruments
	getByNameLike telemetry.MethodInstruments
	getAll        telemetry.MethodInstruments
	existsByName  telemetry.MethodInstruments
}

func (t teleRepository) Create(ctx context.Context, p module.Pipeline) (uuid.UUID, error) {
	t.create.Calls.Add(ctx, 1)
	start := time.Now()
	uid, err := t.repo.Create(ctx, p)
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

func (t teleRepository) GetByUid(ctx context.Context, uid uuid.UUID) (module.Pipeline, error) {
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

func (t teleRepository) GetByNameLike(ctx context.Context, pattern string) ([]module.Pipeline, error) {
	t.getByNameLike.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.GetByNameLike(ctx, pattern)
	t.getByNameLike.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByNameLike.Failures.Add(ctx, 1)
	} else {
		t.getByNameLike.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleRepository) GetAll(ctx context.Context, status *module.Status) ([]module.Pipeline, error) {
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

func (t teleRepository) ExistsByName(ctx context.Context, name string) (bool, error) {
	t.existsByName.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.repo.ExistsByName(ctx, name)
	t.existsByName.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.existsByName.Failures.Add(ctx, 1)
	} else {
		t.existsByName.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// Repository is the exported, telemetry-instrumented adapter for the pipeline
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
// duration histograms under the "baasparse.pipeline.repository.*" namespace.
//
// Returns an error if any OTel instrument fails to register, which typically
// indicates a meter provider misconfiguration.
func NewRepository(pool *pgxpool.Pool, meter metric.Meter) (Repository, error) {
	const prefix = "baasparse.pipeline.repository."

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
	getByNameLike, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_name_like")
	if err != nil {
		return Repository{}, err
	}
	getAll, err := telemetry.NewMethodInstruments(meter, prefix+"get_all")
	if err != nil {
		return Repository{}, err
	}
	existsByName, err := telemetry.NewMethodInstruments(meter, prefix+"exists_by_name")
	if err != nil {
		return Repository{}, err
	}

	return Repository{
		teleRepository: teleRepository{
			repo:          repository{pool: pool},
			create:        create,
			update:        update,
			getByUid:      getByUid,
			getByNameLike: getByNameLike,
			getAll:        getAll,
			existsByName:  existsByName,
		},
	}, nil
}
