package transformeddata

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	"github.com/pgvanniekerk/baasparse/internal/db"
	"github.com/pgvanniekerk/baasparse/internal/module/collectionrequest"
	"github.com/pgvanniekerk/baasparse/internal/module/collector"
	module "github.com/pgvanniekerk/baasparse/internal/module/transformeddata"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── inner service (business logic) ───────────────────────────────────────────

// service contains the core application logic. It validates the referenced
// collection request and collector and expects a pgx.Tx to already be present
// in the context for Create; the txService wrapper provides it. Read operations
// use the pool directly via the repository.
type service struct {
	repo                  module.Repository
	collectionRequestRepo collectionrequest.Repository
	collectorRepo         collector.Repository
}

func (s service) Create(ctx context.Context, cmd module.CreateCommand) (module.TransformedData, error) {
	// Validate the referenced collection request exists and learn its collector.
	cr, err := s.collectionRequestRepo.GetByUid(ctx, cmd.CollectionRequestUid)
	if err != nil {
		return module.TransformedData{}, fmt.Errorf("transformeddata: fetch collection request: %w", err)
	}

	// Validate the referenced collector exists.
	if _, err := s.collectorRepo.GetByUid(ctx, cmd.CollectorUid); err != nil {
		return module.TransformedData{}, fmt.Errorf("transformeddata: fetch collector: %w", err)
	}

	// The transformed record's collector must be the collection request's
	// collector (TD_C_UID == CR_C_UID).
	if cmd.CollectorUid != cr.CollectorUid {
		return module.TransformedData{}, fmt.Errorf("%w: collection request %s was collected by %s", module.ErrCollectorMismatch, cmd.CollectionRequestUid, cr.CollectorUid)
	}

	return s.repo.Create(ctx, module.TransformedData{
		CollectionRequestUid: cmd.CollectionRequestUid,
		CollectorUid:         cmd.CollectorUid,
		Data:                 cmd.Data,
	})
}

func (s service) GetByUid(ctx context.Context, uid uuid.UUID) (module.TransformedData, error) {
	return s.repo.GetByUid(ctx, uid)
}

func (s service) GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]module.TransformedData, error) {
	return s.repo.GetByCRUid(ctx, collectionRequestUid)
}

func (s service) GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]module.TransformedData, error) {
	return s.repo.GetByCUid(ctx, collectorUid)
}

// ── txService (transaction wrapper) ──────────────────────────────────────────

// txService mirrors the teleRepository pattern but for transaction management:
// it begins a pgx.Tx before delegating to the inner service Create, then
// commits on success or rolls back on any error. Read methods pass straight
// through.
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
func (t txService) Create(ctx context.Context, cmd module.CreateCommand) (module.TransformedData, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return module.TransformedData{}, fmt.Errorf("transformeddata: begin transaction: %w", err)
	}

	result, err := t.svc.Create(db.WithTx(ctx, tx), cmd)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.TransformedData{}, fmt.Errorf("transformeddata: rollback failed (%w) after: %w", rbErr, err)
		}
		return module.TransformedData{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.TransformedData{}, fmt.Errorf("transformeddata: rollback failed (%w) after commit error: %w", rbErr, err)
		}
		return module.TransformedData{}, fmt.Errorf("transformeddata: commit: %w", err)
	}
	return result, nil
}

// Read methods do not require a transaction; they delegate directly.

func (t txService) GetByUid(ctx context.Context, uid uuid.UUID) (module.TransformedData, error) {
	return t.svc.GetByUid(ctx, uid)
}

func (t txService) GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]module.TransformedData, error) {
	return t.svc.GetByCRUid(ctx, collectionRequestUid)
}

func (t txService) GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]module.TransformedData, error) {
	return t.svc.GetByCUid(ctx, collectorUid)
}

// ── telemetry wrapper ─────────────────────────────────────────────────────────

// teleService is the outermost layer. It records OTel metrics for every call
// then delegates to txService, which manages transaction lifecycle.
//
// Full stack: teleService → txService → service
type teleService struct {
	svc        txService
	create     telemetry.MethodInstruments
	getByUid   telemetry.MethodInstruments
	getByCRUid telemetry.MethodInstruments
	getByCUid  telemetry.MethodInstruments
}

func (t teleService) Create(ctx context.Context, cmd module.CreateCommand) (module.TransformedData, error) {
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

func (t teleService) GetByUid(ctx context.Context, uid uuid.UUID) (module.TransformedData, error) {
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

func (t teleService) GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]module.TransformedData, error) {
	t.getByCRUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.GetByCRUid(ctx, collectionRequestUid)
	t.getByCRUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByCRUid.Failures.Add(ctx, 1)
	} else {
		t.getByCRUid.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleService) GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]module.TransformedData, error) {
	t.getByCUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.GetByCUid(ctx, collectorUid)
	t.getByCUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByCUid.Failures.Add(ctx, 1)
	} else {
		t.getByCUid.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// ServiceImpl is the exported, fully wired transformeddata service adapter.
// It embeds teleService, which delegates to txService, which delegates to
// service. Construct with NewService; do not instantiate directly.
type ServiceImpl struct {
	teleService
}

// NewService builds a fully wired transformeddata service.
//
//   - pool is the pgx connection pool; txService uses it to begin transactions.
//   - repo is the TransformedData repository (typically
//     adapters/transformeddata.Repository).
//   - collectionRequestRepo validates the referenced collection request exists
//     and supplies its collector for the consistency check.
//   - collectorRepo validates the referenced collector exists.
//   - meter is the OTel Meter used to register per-method service metrics under
//     the "baasparse.transformeddata.service.*" namespace.
//
// Returns an error if any OTel instrument fails to register.
func NewService(
	pool db.TxBeginner,
	repo module.Repository,
	collectionRequestRepo collectionrequest.Repository,
	collectorRepo collector.Repository,
	meter metric.Meter,
) (ServiceImpl, error) {
	const prefix = "baasparse.transformeddata.service."

	create, err := telemetry.NewMethodInstruments(meter, prefix+"create")
	if err != nil {
		return ServiceImpl{}, err
	}
	getByUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_uid")
	if err != nil {
		return ServiceImpl{}, err
	}
	getByCRUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_cr_uid")
	if err != nil {
		return ServiceImpl{}, err
	}
	getByCUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_c_uid")
	if err != nil {
		return ServiceImpl{}, err
	}

	return ServiceImpl{
		teleService: teleService{
			svc: txService{
				svc: service{
					repo:                  repo,
					collectionRequestRepo: collectionRequestRepo,
					collectorRepo:         collectorRepo,
				},
				pool: pool,
			},
			create:     create,
			getByUid:   getByUid,
			getByCRUid: getByCRUid,
			getByCUid:  getByCUid,
		},
	}, nil
}
