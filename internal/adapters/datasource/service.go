package datasource

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	"github.com/pgvanniekerk/baasparse/internal/db"
	module "github.com/pgvanniekerk/baasparse/internal/module/datasource"
	"github.com/pgvanniekerk/baasparse/internal/module/datasourcetype"
	"github.com/pgvanniekerk/baasparse/internal/module/transactionaudit"
	"github.com/pgvanniekerk/baasparse/internal/util/jsonschema"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── inner service (business logic) ───────────────────────────────────────────

// service contains the core application logic. It expects a pgx.Tx to already
// be present in the context for mutating operations; the txService wrapper is
// responsible for providing it. Read operations use the pool directly via the
// repository.
// JSON Schema validation is performed directly via internal/util/jsonschema.
type service struct {
	repo        module.Repository
	dstTypeRepo datasourcetype.Repository
	auditRepo   transactionaudit.Repository
}

func (s service) Create(ctx context.Context, cmd module.CreateCommand) (module.DataSource, error) {
	// Fetch the DataSourceType to obtain its ConnectionSchema for validation.
	// This read is non-transactional — it goes directly to the pool.
	dst, err := s.dstTypeRepo.GetByUid(ctx, cmd.DataSourceTypeUid)
	if err != nil {
		return module.DataSource{}, fmt.Errorf("datasource: fetch type: %w", err)
	}

	// Validate the caller-supplied specification against the type's schema.
	// Fail fast before touching the database if the spec is malformed.
	if err := jsonschema.Validate(dst.ConnectionSchema, cmd.ConnectionSpecification); err != nil {
		return module.DataSource{}, fmt.Errorf("%w: %v", module.ErrInvalidConnectionSpec, err)
	}

	// Create the audit entry using the tx injected by txService.
	taUID, err := s.auditRepo.CreateTransaction(ctx, transactionaudit.Transaction{
		Username:        cmd.Username,
		TransactionType: "CreateDataSource",
		Data: map[string]any{
			"name":                 cmd.Name,
			"data_source_type_uid": cmd.DataSourceTypeUid.String(),
		},
		CorrelationID: cmd.CorrelationID,
	})
	if err != nil {
		return module.DataSource{}, fmt.Errorf("datasource: create audit: %w", err)
	}

	// Insert the data source row using the same tx.
	uid, err := s.repo.Create(ctx, module.DataSource{
		DataSourceTypeUid:       cmd.DataSourceTypeUid,
		Name:                    cmd.Name,
		ConnectionSpecification: cmd.ConnectionSpecification,
		TransactionAuditUid:     taUID,
	})
	if err != nil {
		return module.DataSource{}, fmt.Errorf("datasource: create: %w", err)
	}

	// Return the full persisted record. Still within the same tx, so this
	// read reflects the uncommitted insert.
	return s.repo.GetByUid(ctx, uid)
}

func (s service) Update(ctx context.Context, cmd module.UpdateCommand) (module.DataSource, error) {
	// Fetch the existing record (non-transactional) to obtain its DataSourceTypeUid
	// so we can validate the new spec against the correct JSON Schema.
	existing, err := s.repo.GetByUid(ctx, cmd.Uid)
	if err != nil {
		return module.DataSource{}, fmt.Errorf("datasource: fetch existing: %w", err)
	}

	dst, err := s.dstTypeRepo.GetByUid(ctx, existing.DataSourceTypeUid)
	if err != nil {
		return module.DataSource{}, fmt.Errorf("datasource: fetch type: %w", err)
	}

	if err := jsonschema.Validate(dst.ConnectionSchema, cmd.ConnectionSpecification); err != nil {
		return module.DataSource{}, fmt.Errorf("%w: %v", module.ErrInvalidConnectionSpec, err)
	}

	// Create the audit entry using the tx injected by txService.
	_, err = s.auditRepo.CreateTransaction(ctx, transactionaudit.Transaction{
		Username:        cmd.Username,
		TransactionType: "UpdateDataSource",
		Data: map[string]any{
			"uid":  cmd.Uid.String(),
			"name": cmd.Name,
		},
		CorrelationID: cmd.CorrelationID,
	})
	if err != nil {
		return module.DataSource{}, fmt.Errorf("datasource: create audit: %w", err)
	}

	// Apply the update using the same tx.
	if err := s.repo.Update(ctx, cmd); err != nil {
		return module.DataSource{}, err
	}

	// Return the updated record (still within tx).
	return s.repo.GetByUid(ctx, cmd.Uid)
}

func (s service) GetByUid(ctx context.Context, uid uuid.UUID) (module.DataSource, error) {
	return s.repo.GetByUid(ctx, uid)
}

func (s service) GetByName(ctx context.Context, name string) (module.DataSource, error) {
	return s.repo.GetByName(ctx, name)
}

func (s service) GetAll(ctx context.Context) ([]module.DataSource, error) {
	return s.repo.GetAll(ctx)
}

// ── txService (transaction wrapper) ──────────────────────────────────────────

// txService mirrors the teleRepository pattern but for transaction management:
// it begins a pgx.Tx before delegating to the inner service, then commits on
// success or rolls back on any error. Read methods pass straight through.
//
// Structure:
//
//	service (business logic) → txService (tx lifecycle) → ServiceImpl (exported)
type txService struct {
	svc  service
	pool db.TxBeginner // *pgxpool.Pool satisfies this interface
}

// Create begins a transaction, attaches it to ctx via db.WithTx, delegates
// to the inner service, then commits or rolls back.
func (t txService) Create(ctx context.Context, cmd module.CreateCommand) (module.DataSource, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return module.DataSource{}, fmt.Errorf("datasource: begin transaction: %w", err)
	}

	result, err := t.svc.Create(db.WithTx(ctx, tx), cmd)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.DataSource{}, fmt.Errorf("datasource: rollback failed (%w) after: %w", rbErr, err)
		}
		return module.DataSource{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.DataSource{}, fmt.Errorf("datasource: rollback failed (%w) after commit error: %w", rbErr, err)
		}
		return module.DataSource{}, fmt.Errorf("datasource: commit: %w", err)
	}
	return result, nil
}

func (t txService) Update(ctx context.Context, cmd module.UpdateCommand) (module.DataSource, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return module.DataSource{}, fmt.Errorf("datasource: begin transaction: %w", err)
	}

	result, err := t.svc.Update(db.WithTx(ctx, tx), cmd)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.DataSource{}, fmt.Errorf("datasource: rollback failed (%w) after: %w", rbErr, err)
		}
		return module.DataSource{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.DataSource{}, fmt.Errorf("datasource: rollback failed (%w) after commit error: %w", rbErr, err)
		}
		return module.DataSource{}, fmt.Errorf("datasource: commit: %w", err)
	}
	return result, nil
}

// Read methods do not require a transaction; they delegate directly.

func (t txService) GetByUid(ctx context.Context, uid uuid.UUID) (module.DataSource, error) {
	return t.svc.GetByUid(ctx, uid)
}

func (t txService) GetByName(ctx context.Context, name string) (module.DataSource, error) {
	return t.svc.GetByName(ctx, name)
}

func (t txService) GetAll(ctx context.Context) ([]module.DataSource, error) {
	return t.svc.GetAll(ctx)
}

// ── telemetry wrapper ─────────────────────────────────────────────────────────

// teleService is the outermost layer. It records OTel metrics for every call
// then delegates to txService, which manages transaction lifecycle.
//
// Full stack: teleService → txService → service
type teleService struct {
	svc       txService
	create    telemetry.MethodInstruments
	update    telemetry.MethodInstruments
	getByUid  telemetry.MethodInstruments
	getByName telemetry.MethodInstruments
	getAll    telemetry.MethodInstruments
}

func (t teleService) Create(ctx context.Context, cmd module.CreateCommand) (module.DataSource, error) {
	t.create.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.Create(ctx, cmd)
	t.create.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.create.Failures.Add(ctx, 1)
	} else {
		t.create.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleService) Update(ctx context.Context, cmd module.UpdateCommand) (module.DataSource, error) {
	t.update.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.Update(ctx, cmd)
	t.update.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.update.Failures.Add(ctx, 1)
	} else {
		t.update.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleService) GetByUid(ctx context.Context, uid uuid.UUID) (module.DataSource, error) {
	t.getByUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.GetByUid(ctx, uid)
	t.getByUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByUid.Failures.Add(ctx, 1)
	} else {
		t.getByUid.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleService) GetByName(ctx context.Context, name string) (module.DataSource, error) {
	t.getByName.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.GetByName(ctx, name)
	t.getByName.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByName.Failures.Add(ctx, 1)
	} else {
		t.getByName.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleService) GetAll(ctx context.Context) ([]module.DataSource, error) {
	t.getAll.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.GetAll(ctx)
	t.getAll.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getAll.Failures.Add(ctx, 1)
	} else {
		t.getAll.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// ServiceImpl is the exported, fully wired datasource service adapter.
// It embeds teleService, which delegates to txService, which delegates to
// service. Construct with NewService; do not instantiate directly.
type ServiceImpl struct {
	teleService
}

// NewService builds a fully wired datasource service.
//
//   - pool is the pgx connection pool; txService uses it to begin transactions.
//   - repo is the DataSource repository (typically adapters/datasource.Repository).
//   - dstTypeRepo is the DataSourceType repository, used to fetch the schema for
//     validation.
//   - auditRepo is the TransactionAudit repository, used to write audit entries
//     atomically with data source mutations.
//   - meter is the OTel Meter used to register per-method service metrics under
//     the "baasparse.datasource.service.*" namespace.
//
// JSON Schema validation is performed directly via internal/util/jsonschema.
// Returns an error if any OTel instrument fails to register.
func NewService(
	pool db.TxBeginner,
	repo module.Repository,
	dstTypeRepo datasourcetype.Repository,
	auditRepo transactionaudit.Repository,
	meter metric.Meter,
) (ServiceImpl, error) {
	const prefix = "baasparse.datasource.service."

	create, err := telemetry.NewMethodInstruments(meter, prefix+"create")
	if err != nil {
		return ServiceImpl{}, err
	}
	update, err := telemetry.NewMethodInstruments(meter, prefix+"update")
	if err != nil {
		return ServiceImpl{}, err
	}
	getByUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_uid")
	if err != nil {
		return ServiceImpl{}, err
	}
	getByName, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_name")
	if err != nil {
		return ServiceImpl{}, err
	}
	getAll, err := telemetry.NewMethodInstruments(meter, prefix+"get_all")
	if err != nil {
		return ServiceImpl{}, err
	}

	return ServiceImpl{
		teleService: teleService{
			svc: txService{
				svc: service{
					repo:        repo,
					dstTypeRepo: dstTypeRepo,
					auditRepo:   auditRepo,
				},
				pool: pool,
			},
			create:    create,
			update:    update,
			getByUid:  getByUid,
			getByName: getByName,
			getAll:    getAll,
		},
	}, nil
}
