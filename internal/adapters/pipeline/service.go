package pipeline

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	"github.com/pgvanniekerk/baasparse/internal/db"
	module "github.com/pgvanniekerk/baasparse/internal/module/pipeline"
	"github.com/pgvanniekerk/baasparse/internal/module/transactionaudit"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── inner service (business logic) ───────────────────────────────────────────

// service contains the core application logic. It expects a pgx.Tx to already
// be present in the context for mutating operations; the txService wrapper is
// responsible for providing it. Read operations use the pool directly via the
// repository.
type service struct {
	repo      module.Repository
	auditRepo transactionaudit.Repository
}

func (s service) Create(ctx context.Context, cmd module.CreateCommand) (module.Pipeline, error) {
	// Enforce global name uniqueness before writing anything. The UIDX_P_NAME
	// unique index remains the hard guarantee against races.
	exists, err := s.repo.ExistsByName(ctx, cmd.Name)
	if err != nil {
		return module.Pipeline{}, fmt.Errorf("pipeline: check name: %w", err)
	}
	if exists {
		return module.Pipeline{}, fmt.Errorf("%w: %s", module.ErrNameExists, cmd.Name)
	}

	// Record the creation in the audit trail using the tx injected by txService.
	taUID, err := s.auditRepo.CreateTransaction(ctx, transactionaudit.Transaction{
		Username:        cmd.Username,
		TransactionType: "CreatePipeline",
		Data: map[string]any{
			"name": cmd.Name,
		},
		CorrelationID: cmd.CorrelationID,
	})
	if err != nil {
		return module.Pipeline{}, fmt.Errorf("pipeline: create audit: %w", err)
	}

	// Insert the pipeline row (always ACTIVE on creation) using the same tx.
	uid, err := s.repo.Create(ctx, module.Pipeline{
		Name:                cmd.Name,
		Description:         cmd.Description,
		Status:              module.StatusActive,
		TransactionAuditUid: taUID,
	})
	if err != nil {
		return module.Pipeline{}, fmt.Errorf("pipeline: create: %w", err)
	}

	// Assemble the persisted record from known inputs. We deliberately do not
	// re-read via the repository here: read methods use the connection pool,
	// which would not observe the row inserted on this still-uncommitted tx.
	return module.Pipeline{
		Uid:                 uid,
		Name:                cmd.Name,
		Description:         cmd.Description,
		Status:              module.StatusActive,
		TransactionAuditUid: taUID,
	}, nil
}

func (s service) Update(ctx context.Context, cmd module.UpdateCommand) (module.Pipeline, error) {
	// Reject unknown status values before touching the database.
	if !cmd.Status.Valid() {
		return module.Pipeline{}, fmt.Errorf("%w: %s", module.ErrInvalidStatus, cmd.Status)
	}

	// Ensure the pipeline exists and learn its current name and audit row.
	// This read targets committed data, so the pool is correct here.
	existing, err := s.repo.GetByUid(ctx, cmd.Uid)
	if err != nil {
		return module.Pipeline{}, fmt.Errorf("pipeline: fetch existing: %w", err)
	}

	// If the name is changing, enforce uniqueness against other pipelines.
	if cmd.Name != existing.Name {
		exists, err := s.repo.ExistsByName(ctx, cmd.Name)
		if err != nil {
			return module.Pipeline{}, fmt.Errorf("pipeline: check name: %w", err)
		}
		if exists {
			return module.Pipeline{}, fmt.Errorf("%w: %s", module.ErrNameExists, cmd.Name)
		}
	}

	// Record the update in the audit trail using the tx injected by txService.
	_, err = s.auditRepo.CreateTransaction(ctx, transactionaudit.Transaction{
		Username:        cmd.Username,
		TransactionType: "UpdatePipeline",
		Data: map[string]any{
			"uid":    cmd.Uid.String(),
			"name":   cmd.Name,
			"status": string(cmd.Status),
		},
		CorrelationID: cmd.CorrelationID,
	})
	if err != nil {
		return module.Pipeline{}, fmt.Errorf("pipeline: create audit: %w", err)
	}

	// Apply the update using the same tx.
	if err := s.repo.Update(ctx, cmd); err != nil {
		return module.Pipeline{}, err
	}

	// Assemble the updated record from known inputs (see Create for rationale).
	// P_TA_UID is not changed by an update, so it retains the creation audit.
	return module.Pipeline{
		Uid:                 cmd.Uid,
		Name:                cmd.Name,
		Description:         cmd.Description,
		Status:              cmd.Status,
		TransactionAuditUid: existing.TransactionAuditUid,
	}, nil
}

func (s service) GetByUid(ctx context.Context, uid uuid.UUID) (module.Pipeline, error) {
	return s.repo.GetByUid(ctx, uid)
}

func (s service) GetByNameLike(ctx context.Context, name string) ([]module.Pipeline, error) {
	// Implement a "contains" search by wrapping the term in LIKE wildcards.
	return s.repo.GetByNameLike(ctx, "%"+name+"%")
}

func (s service) GetAll(ctx context.Context, status *module.Status) ([]module.Pipeline, error) {
	return s.repo.GetAll(ctx, status)
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

// Create begins a transaction, attaches it to ctx via db.WithTx, delegates to
// the inner service, then commits or rolls back.
func (t txService) Create(ctx context.Context, cmd module.CreateCommand) (module.Pipeline, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return module.Pipeline{}, fmt.Errorf("pipeline: begin transaction: %w", err)
	}

	result, err := t.svc.Create(db.WithTx(ctx, tx), cmd)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.Pipeline{}, fmt.Errorf("pipeline: rollback failed (%w) after: %w", rbErr, err)
		}
		return module.Pipeline{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.Pipeline{}, fmt.Errorf("pipeline: rollback failed (%w) after commit error: %w", rbErr, err)
		}
		return module.Pipeline{}, fmt.Errorf("pipeline: commit: %w", err)
	}
	return result, nil
}

func (t txService) Update(ctx context.Context, cmd module.UpdateCommand) (module.Pipeline, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return module.Pipeline{}, fmt.Errorf("pipeline: begin transaction: %w", err)
	}

	result, err := t.svc.Update(db.WithTx(ctx, tx), cmd)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.Pipeline{}, fmt.Errorf("pipeline: rollback failed (%w) after: %w", rbErr, err)
		}
		return module.Pipeline{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.Pipeline{}, fmt.Errorf("pipeline: rollback failed (%w) after commit error: %w", rbErr, err)
		}
		return module.Pipeline{}, fmt.Errorf("pipeline: commit: %w", err)
	}
	return result, nil
}

// Read methods do not require a transaction; they delegate directly.

func (t txService) GetByUid(ctx context.Context, uid uuid.UUID) (module.Pipeline, error) {
	return t.svc.GetByUid(ctx, uid)
}

func (t txService) GetByNameLike(ctx context.Context, name string) ([]module.Pipeline, error) {
	return t.svc.GetByNameLike(ctx, name)
}

func (t txService) GetAll(ctx context.Context, status *module.Status) ([]module.Pipeline, error) {
	return t.svc.GetAll(ctx, status)
}

// ── telemetry wrapper ─────────────────────────────────────────────────────────

// teleService is the outermost layer. It records OTel metrics for every call
// then delegates to txService, which manages transaction lifecycle.
//
// Full stack: teleService → txService → service
type teleService struct {
	svc           txService
	create        telemetry.MethodInstruments
	update        telemetry.MethodInstruments
	getByUid      telemetry.MethodInstruments
	getByNameLike telemetry.MethodInstruments
	getAll        telemetry.MethodInstruments
}

func (t teleService) Create(ctx context.Context, cmd module.CreateCommand) (module.Pipeline, error) {
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

func (t teleService) Update(ctx context.Context, cmd module.UpdateCommand) (module.Pipeline, error) {
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

func (t teleService) GetByUid(ctx context.Context, uid uuid.UUID) (module.Pipeline, error) {
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

func (t teleService) GetByNameLike(ctx context.Context, name string) ([]module.Pipeline, error) {
	t.getByNameLike.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.GetByNameLike(ctx, name)
	t.getByNameLike.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByNameLike.Failures.Add(ctx, 1)
	} else {
		t.getByNameLike.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleService) GetAll(ctx context.Context, status *module.Status) ([]module.Pipeline, error) {
	t.getAll.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.GetAll(ctx, status)
	t.getAll.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getAll.Failures.Add(ctx, 1)
	} else {
		t.getAll.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// ServiceImpl is the exported, fully wired pipeline service adapter.
// It embeds teleService, which delegates to txService, which delegates to
// service. Construct with NewService; do not instantiate directly.
type ServiceImpl struct {
	teleService
}

// NewService builds a fully wired pipeline service.
//
//   - pool is the pgx connection pool; txService uses it to begin transactions.
//   - repo is the Pipeline repository (typically adapters/pipeline.Repository).
//   - auditRepo is the TransactionAudit repository, used to write audit entries
//     atomically with pipeline mutations.
//   - meter is the OTel Meter used to register per-method service metrics under
//     the "baasparse.pipeline.service.*" namespace.
//
// Returns an error if any OTel instrument fails to register.
func NewService(
	pool db.TxBeginner,
	repo module.Repository,
	auditRepo transactionaudit.Repository,
	meter metric.Meter,
) (ServiceImpl, error) {
	const prefix = "baasparse.pipeline.service."

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
	getByNameLike, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_name_like")
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
					repo:      repo,
					auditRepo: auditRepo,
				},
				pool: pool,
			},
			create:        create,
			update:        update,
			getByUid:      getByUid,
			getByNameLike: getByNameLike,
			getAll:        getAll,
		},
	}, nil
}
