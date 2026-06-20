package collector

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	"github.com/pgvanniekerk/baasparse/internal/db"
	module "github.com/pgvanniekerk/baasparse/internal/module/collector"
	"github.com/pgvanniekerk/baasparse/internal/module/datasource"
	"github.com/pgvanniekerk/baasparse/internal/module/datasourcetype"
	"github.com/pgvanniekerk/baasparse/internal/module/pipeline"
	"github.com/pgvanniekerk/baasparse/internal/module/transactionaudit"
	"github.com/pgvanniekerk/baasparse/internal/module/transformer"
	"github.com/pgvanniekerk/baasparse/internal/util/jsonschema"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── inner service (business logic) ───────────────────────────────────────────

// service contains the core application logic. It expects a pgx.Tx to already
// be present in the context for mutating operations; the txService wrapper is
// responsible for providing it. Read operations use the pool directly via the
// repositories.
//
// Reference validation and schema validation are performed against the other
// modules' ports: the data source's type supplies the collection schema, and
// the optional transformer supplies the transformation schema.
type service struct {
	repo               module.Repository
	pipelineRepo       pipeline.Repository
	dataSourceRepo     datasource.Repository
	dataSourceTypeRepo datasourcetype.Repository
	transformerRepo    transformer.Repository
	auditRepo          transactionaudit.Repository
}

// validateReferencesAndSpecs enforces the transformer/specification pairing and
// validates the collection and transformation specifications against their
// schemas. The data source's type provides the collection schema; the optional
// transformer provides the transformation schema. It performs read-only lookups
// (via the pool) of committed data, so it is safe to call inside a transaction.
func (s service) validateReferencesAndSpecs(
	ctx context.Context,
	dataSourceUid uuid.UUID,
	collectionSpec json.RawMessage,
	transformerUid *uuid.UUID,
	transformationSpec json.RawMessage,
) error {
	// A transformation specification must be present if and only if a
	// transformer is set (C_TRANSFORMATION_SPECIFICATION is NULL when C_T_UID
	// is NULL).
	hasTransformer := transformerUid != nil
	hasTransformationSpec := len(transformationSpec) > 0
	if hasTransformer != hasTransformationSpec {
		return module.ErrTransformationSpecMismatch
	}

	// Resolve the data source and its type to obtain the collection schema.
	ds, err := s.dataSourceRepo.GetByUid(ctx, dataSourceUid)
	if err != nil {
		return fmt.Errorf("collector: fetch data source: %w", err)
	}
	dst, err := s.dataSourceTypeRepo.GetByUid(ctx, ds.DataSourceTypeUid)
	if err != nil {
		return fmt.Errorf("collector: fetch data source type: %w", err)
	}
	if err := jsonschema.Validate(dst.CollectionSchema, collectionSpec); err != nil {
		return fmt.Errorf("%w: %v", module.ErrInvalidCollectionSpec, err)
	}

	// When a transformer is set, validate the transformation specification
	// against its schema (and confirm the transformer exists).
	if hasTransformer {
		t, err := s.transformerRepo.GetByUid(ctx, *transformerUid)
		if err != nil {
			return fmt.Errorf("collector: fetch transformer: %w", err)
		}
		if err := jsonschema.Validate(t.TransformationSchema, transformationSpec); err != nil {
			return fmt.Errorf("%w: %v", module.ErrInvalidTransformationSpec, err)
		}
	}
	return nil
}

func (s service) Create(ctx context.Context, cmd module.CreateCommand) (module.Collector, error) {
	// Validate the pipeline exists and does not already have a collector
	// (C_P_UID is unique — one collector per pipeline).
	if _, err := s.pipelineRepo.GetByUid(ctx, cmd.PipelineUid); err != nil {
		return module.Collector{}, fmt.Errorf("collector: fetch pipeline: %w", err)
	}
	exists, err := s.repo.ExistsByPUid(ctx, cmd.PipelineUid)
	if err != nil {
		return module.Collector{}, fmt.Errorf("collector: check pipeline: %w", err)
	}
	if exists {
		return module.Collector{}, fmt.Errorf("%w: %s", module.ErrCollectorExists, cmd.PipelineUid)
	}

	// Validate the data source / transformer references and their specs.
	if err := s.validateReferencesAndSpecs(ctx, cmd.DataSourceUid, cmd.CollectionSpecification, cmd.TransformerUid, cmd.TransformationSpecification); err != nil {
		return module.Collector{}, err
	}

	// Record the creation in the audit trail using the tx injected by txService.
	taUID, err := s.auditRepo.CreateTransaction(ctx, transactionaudit.Transaction{
		Username:        cmd.Username,
		TransactionType: "CreateCollector",
		Data: map[string]any{
			"pipeline_uid":    cmd.PipelineUid.String(),
			"data_source_uid": cmd.DataSourceUid.String(),
		},
		CorrelationID: cmd.CorrelationID,
	})
	if err != nil {
		return module.Collector{}, fmt.Errorf("collector: create audit: %w", err)
	}

	// Insert the collector row (always ACTIVE on creation) using the same tx.
	c := module.Collector{
		PipelineUid:                 cmd.PipelineUid,
		DataSourceUid:               cmd.DataSourceUid,
		CollectionSpecification:     cmd.CollectionSpecification,
		TransformerUid:              cmd.TransformerUid,
		TransformationSpecification: cmd.TransformationSpecification,
		Status:                      module.StatusActive,
		TransactionAuditUid:         taUID,
	}
	uid, err := s.repo.Create(ctx, c)
	if err != nil {
		return module.Collector{}, fmt.Errorf("collector: create: %w", err)
	}

	// Assemble the persisted record from known inputs. We deliberately do not
	// re-read via the repository here: read methods use the connection pool,
	// which would not observe the row inserted on this still-uncommitted tx.
	c.Uid = uid
	return c, nil
}

func (s service) Update(ctx context.Context, cmd module.UpdateCommand) (module.Collector, error) {
	// Reject unknown status values before touching the database.
	if !cmd.Status.Valid() {
		return module.Collector{}, fmt.Errorf("%w: %s", module.ErrInvalidStatus, cmd.Status)
	}

	// Ensure the collector exists; preserve its immutable pipeline + audit links.
	// This read targets committed data, so the pool is correct here.
	existing, err := s.repo.GetByUid(ctx, cmd.Uid)
	if err != nil {
		return module.Collector{}, fmt.Errorf("collector: fetch existing: %w", err)
	}

	// Validate the (possibly changed) data source / transformer references and
	// their specs.
	if err := s.validateReferencesAndSpecs(ctx, cmd.DataSourceUid, cmd.CollectionSpecification, cmd.TransformerUid, cmd.TransformationSpecification); err != nil {
		return module.Collector{}, err
	}

	// Record the update in the audit trail using the tx injected by txService.
	_, err = s.auditRepo.CreateTransaction(ctx, transactionaudit.Transaction{
		Username:        cmd.Username,
		TransactionType: "UpdateCollector",
		Data: map[string]any{
			"uid":             cmd.Uid.String(),
			"data_source_uid": cmd.DataSourceUid.String(),
			"status":          string(cmd.Status),
		},
		CorrelationID: cmd.CorrelationID,
	})
	if err != nil {
		return module.Collector{}, fmt.Errorf("collector: create audit: %w", err)
	}

	// Apply the update using the same tx.
	if err := s.repo.Update(ctx, cmd); err != nil {
		return module.Collector{}, err
	}

	// Assemble the updated record from known inputs (see Create for rationale).
	// C_P_UID and C_TA_UID are immutable, so they retain the existing values.
	return module.Collector{
		Uid:                         cmd.Uid,
		PipelineUid:                 existing.PipelineUid,
		DataSourceUid:               cmd.DataSourceUid,
		CollectionSpecification:     cmd.CollectionSpecification,
		TransformerUid:              cmd.TransformerUid,
		TransformationSpecification: cmd.TransformationSpecification,
		Status:                      cmd.Status,
		TransactionAuditUid:         existing.TransactionAuditUid,
	}, nil
}

func (s service) GetByUid(ctx context.Context, uid uuid.UUID) (module.Collector, error) {
	return s.repo.GetByUid(ctx, uid)
}

func (s service) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) (module.Collector, error) {
	return s.repo.GetByPUid(ctx, pipelineUid)
}

func (s service) GetAll(ctx context.Context, status *module.Status) ([]module.Collector, error) {
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
func (t txService) Create(ctx context.Context, cmd module.CreateCommand) (module.Collector, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return module.Collector{}, fmt.Errorf("collector: begin transaction: %w", err)
	}

	result, err := t.svc.Create(db.WithTx(ctx, tx), cmd)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.Collector{}, fmt.Errorf("collector: rollback failed (%w) after: %w", rbErr, err)
		}
		return module.Collector{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.Collector{}, fmt.Errorf("collector: rollback failed (%w) after commit error: %w", rbErr, err)
		}
		return module.Collector{}, fmt.Errorf("collector: commit: %w", err)
	}
	return result, nil
}

func (t txService) Update(ctx context.Context, cmd module.UpdateCommand) (module.Collector, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return module.Collector{}, fmt.Errorf("collector: begin transaction: %w", err)
	}

	result, err := t.svc.Update(db.WithTx(ctx, tx), cmd)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.Collector{}, fmt.Errorf("collector: rollback failed (%w) after: %w", rbErr, err)
		}
		return module.Collector{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.Collector{}, fmt.Errorf("collector: rollback failed (%w) after commit error: %w", rbErr, err)
		}
		return module.Collector{}, fmt.Errorf("collector: commit: %w", err)
	}
	return result, nil
}

// Read methods do not require a transaction; they delegate directly.

func (t txService) GetByUid(ctx context.Context, uid uuid.UUID) (module.Collector, error) {
	return t.svc.GetByUid(ctx, uid)
}

func (t txService) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) (module.Collector, error) {
	return t.svc.GetByPUid(ctx, pipelineUid)
}

func (t txService) GetAll(ctx context.Context, status *module.Status) ([]module.Collector, error) {
	return t.svc.GetAll(ctx, status)
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
	getByPUid telemetry.MethodInstruments
	getAll    telemetry.MethodInstruments
}

func (t teleService) Create(ctx context.Context, cmd module.CreateCommand) (module.Collector, error) {
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

func (t teleService) Update(ctx context.Context, cmd module.UpdateCommand) (module.Collector, error) {
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

func (t teleService) GetByUid(ctx context.Context, uid uuid.UUID) (module.Collector, error) {
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

func (t teleService) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) (module.Collector, error) {
	t.getByPUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.GetByPUid(ctx, pipelineUid)
	t.getByPUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByPUid.Failures.Add(ctx, 1)
	} else {
		t.getByPUid.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleService) GetAll(ctx context.Context, status *module.Status) ([]module.Collector, error) {
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

// ServiceImpl is the exported, fully wired collector service adapter.
// It embeds teleService, which delegates to txService, which delegates to
// service. Construct with NewService; do not instantiate directly.
type ServiceImpl struct {
	teleService
}

// NewService builds a fully wired collector service.
//
//   - pool is the pgx connection pool; txService uses it to begin transactions.
//   - repo is the Collector repository (typically adapters/collector.Repository).
//   - pipelineRepo validates the referenced pipeline exists.
//   - dataSourceRepo resolves the referenced data source (and its type).
//   - dataSourceTypeRepo supplies the DST_COLLECTION_SCHEMA used to validate the
//     collection specification.
//   - transformerRepo supplies the T_TRANSFORMATION_SCHEMA used to validate the
//     transformation specification.
//   - auditRepo writes audit entries atomically with collector mutations.
//   - meter is the OTel Meter used to register per-method service metrics under
//     the "baasparse.collector.service.*" namespace.
//
// Returns an error if any OTel instrument fails to register.
func NewService(
	pool db.TxBeginner,
	repo module.Repository,
	pipelineRepo pipeline.Repository,
	dataSourceRepo datasource.Repository,
	dataSourceTypeRepo datasourcetype.Repository,
	transformerRepo transformer.Repository,
	auditRepo transactionaudit.Repository,
	meter metric.Meter,
) (ServiceImpl, error) {
	const prefix = "baasparse.collector.service."

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
	getByPUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_p_uid")
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
					repo:               repo,
					pipelineRepo:       pipelineRepo,
					dataSourceRepo:     dataSourceRepo,
					dataSourceTypeRepo: dataSourceTypeRepo,
					transformerRepo:    transformerRepo,
					auditRepo:          auditRepo,
				},
				pool: pool,
			},
			create:    create,
			update:    update,
			getByUid:  getByUid,
			getByPUid: getByPUid,
			getAll:    getAll,
		},
	}, nil
}
