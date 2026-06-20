package distributionrequest

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	"github.com/pgvanniekerk/baasparse/internal/db"
	"github.com/pgvanniekerk/baasparse/internal/module/collectionrequest"
	module "github.com/pgvanniekerk/baasparse/internal/module/distributionrequest"
	"github.com/pgvanniekerk/baasparse/internal/module/distributor"
	"github.com/pgvanniekerk/baasparse/internal/module/transformeddata"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── inner service (business logic) ───────────────────────────────────────────

// service contains the core application logic. It validates the referenced
// distributor, collection request, and optional transformed data, and expects a
// pgx.Tx to already be present in the context for Create and Update; the
// txService wrapper provides it. Read operations use the pool directly via the
// repository.
type service struct {
	repo                  module.Repository
	distributorRepo       distributor.Repository
	collectionRequestRepo collectionrequest.Repository
	transformedDataRepo   transformeddata.Repository
}

func (s service) Create(ctx context.Context, cmd module.CreateCommand) (module.DistributionRequest, error) {
	// Validate the referenced distributor exists.
	if _, err := s.distributorRepo.GetByUid(ctx, cmd.DistributorUid); err != nil {
		return module.DistributionRequest{}, fmt.Errorf("distributionrequest: fetch distributor: %w", err)
	}

	// Validate the referenced collection request exists.
	if _, err := s.collectionRequestRepo.GetByUid(ctx, cmd.CollectionRequestUid); err != nil {
		return module.DistributionRequest{}, fmt.Errorf("distributionrequest: fetch collection request: %w", err)
	}

	// When transformed data is referenced, validate it exists and derives from
	// the same collection request (TD_CR_UID == DR_CR_UID).
	if cmd.TransformedDataUid != nil {
		td, err := s.transformedDataRepo.GetByUid(ctx, *cmd.TransformedDataUid)
		if err != nil {
			return module.DistributionRequest{}, fmt.Errorf("distributionrequest: fetch transformed data: %w", err)
		}
		if td.CollectionRequestUid != cmd.CollectionRequestUid {
			return module.DistributionRequest{}, fmt.Errorf("%w: transformed data %s derives from collection request %s", module.ErrTransformedDataMismatch, *cmd.TransformedDataUid, td.CollectionRequestUid)
		}
	}

	// Insert as PENDING (no content or failure reason yet).
	return s.repo.Create(ctx, module.DistributionRequest{
		DistributorUid:       cmd.DistributorUid,
		CollectionRequestUid: cmd.CollectionRequestUid,
		TransformedDataUid:   cmd.TransformedDataUid,
		Status:               module.StatusPending,
	})
}

func (s service) Update(ctx context.Context, cmd module.UpdateCommand) (module.DistributionRequest, error) {
	// Reject unknown status values before touching the database.
	if !cmd.Status.Valid() {
		return module.DistributionRequest{}, fmt.Errorf("%w: %s", module.ErrInvalidStatus, cmd.Status)
	}

	// A failure reason must be present if and only if the status is FAILED
	// (DR_FAILURE_REASON is NULL otherwise).
	hasReason := cmd.FailureReason != nil
	if (cmd.Status == module.StatusFailed) != hasReason {
		return module.DistributionRequest{}, module.ErrFailureReasonMismatch
	}

	// Ensure the request exists; preserve its immutable fields for the return.
	// This read targets committed data, so the pool is correct here.
	existing, err := s.repo.GetByUid(ctx, cmd.Uid)
	if err != nil {
		return module.DistributionRequest{}, fmt.Errorf("distributionrequest: fetch existing: %w", err)
	}

	if err := s.repo.Update(ctx, cmd); err != nil {
		return module.DistributionRequest{}, err
	}

	// Assemble the updated record from the existing immutable fields and the
	// command's outcome. Empty content normalises to nil to match the stored
	// NULL (see nullableBytes in the repository).
	existing.Status = cmd.Status
	existing.FailureReason = cmd.FailureReason
	if len(cmd.Content) == 0 {
		existing.Content = nil
	} else {
		existing.Content = cmd.Content
	}
	return existing, nil
}

func (s service) GetByUid(ctx context.Context, uid uuid.UUID) (module.DistributionRequest, error) {
	return s.repo.GetByUid(ctx, uid)
}

func (s service) GetByDUid(ctx context.Context, distributorUid uuid.UUID) ([]module.DistributionRequest, error) {
	return s.repo.GetByDUid(ctx, distributorUid)
}

func (s service) GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]module.DistributionRequest, error) {
	return s.repo.GetByCRUid(ctx, collectionRequestUid)
}

func (s service) GetByTDUid(ctx context.Context, transformedDataUid uuid.UUID) ([]module.DistributionRequest, error) {
	return s.repo.GetByTDUid(ctx, transformedDataUid)
}

// ── txService (transaction wrapper) ──────────────────────────────────────────

// txService mirrors the teleRepository pattern but for transaction management:
// it begins a pgx.Tx before delegating to the inner service Create/Update, then
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
func (t txService) Create(ctx context.Context, cmd module.CreateCommand) (module.DistributionRequest, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return module.DistributionRequest{}, fmt.Errorf("distributionrequest: begin transaction: %w", err)
	}

	result, err := t.svc.Create(db.WithTx(ctx, tx), cmd)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.DistributionRequest{}, fmt.Errorf("distributionrequest: rollback failed (%w) after: %w", rbErr, err)
		}
		return module.DistributionRequest{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.DistributionRequest{}, fmt.Errorf("distributionrequest: rollback failed (%w) after commit error: %w", rbErr, err)
		}
		return module.DistributionRequest{}, fmt.Errorf("distributionrequest: commit: %w", err)
	}
	return result, nil
}

func (t txService) Update(ctx context.Context, cmd module.UpdateCommand) (module.DistributionRequest, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return module.DistributionRequest{}, fmt.Errorf("distributionrequest: begin transaction: %w", err)
	}

	result, err := t.svc.Update(db.WithTx(ctx, tx), cmd)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.DistributionRequest{}, fmt.Errorf("distributionrequest: rollback failed (%w) after: %w", rbErr, err)
		}
		return module.DistributionRequest{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.DistributionRequest{}, fmt.Errorf("distributionrequest: rollback failed (%w) after commit error: %w", rbErr, err)
		}
		return module.DistributionRequest{}, fmt.Errorf("distributionrequest: commit: %w", err)
	}
	return result, nil
}

// Read methods do not require a transaction; they delegate directly.

func (t txService) GetByUid(ctx context.Context, uid uuid.UUID) (module.DistributionRequest, error) {
	return t.svc.GetByUid(ctx, uid)
}

func (t txService) GetByDUid(ctx context.Context, distributorUid uuid.UUID) ([]module.DistributionRequest, error) {
	return t.svc.GetByDUid(ctx, distributorUid)
}

func (t txService) GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]module.DistributionRequest, error) {
	return t.svc.GetByCRUid(ctx, collectionRequestUid)
}

func (t txService) GetByTDUid(ctx context.Context, transformedDataUid uuid.UUID) ([]module.DistributionRequest, error) {
	return t.svc.GetByTDUid(ctx, transformedDataUid)
}

// ── telemetry wrapper ─────────────────────────────────────────────────────────

// teleService is the outermost layer. It records OTel metrics for every call
// then delegates to txService, which manages transaction lifecycle.
//
// Full stack: teleService → txService → service
type teleService struct {
	svc        txService
	create     telemetry.MethodInstruments
	update     telemetry.MethodInstruments
	getByUid   telemetry.MethodInstruments
	getByDUid  telemetry.MethodInstruments
	getByCRUid telemetry.MethodInstruments
	getByTDUid telemetry.MethodInstruments
}

func (t teleService) Create(ctx context.Context, cmd module.CreateCommand) (module.DistributionRequest, error) {
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

func (t teleService) Update(ctx context.Context, cmd module.UpdateCommand) (module.DistributionRequest, error) {
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

func (t teleService) GetByUid(ctx context.Context, uid uuid.UUID) (module.DistributionRequest, error) {
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

func (t teleService) GetByDUid(ctx context.Context, distributorUid uuid.UUID) ([]module.DistributionRequest, error) {
	t.getByDUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.GetByDUid(ctx, distributorUid)
	t.getByDUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByDUid.Failures.Add(ctx, 1)
	} else {
		t.getByDUid.Successes.Add(ctx, 1)
	}
	return result, err
}

func (t teleService) GetByCRUid(ctx context.Context, collectionRequestUid uuid.UUID) ([]module.DistributionRequest, error) {
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

func (t teleService) GetByTDUid(ctx context.Context, transformedDataUid uuid.UUID) ([]module.DistributionRequest, error) {
	t.getByTDUid.Calls.Add(ctx, 1)
	start := time.Now()
	result, err := t.svc.GetByTDUid(ctx, transformedDataUid)
	t.getByTDUid.Duration.Record(ctx, telemetry.MsElapsed(start))
	if err != nil {
		t.getByTDUid.Failures.Add(ctx, 1)
	} else {
		t.getByTDUid.Successes.Add(ctx, 1)
	}
	return result, err
}

// ── exported wrapper ──────────────────────────────────────────────────────────

// ServiceImpl is the exported, fully wired distributionrequest service adapter.
// It embeds teleService, which delegates to txService, which delegates to
// service. Construct with NewService; do not instantiate directly.
type ServiceImpl struct {
	teleService
}

// NewService builds a fully wired distributionrequest service.
//
//   - pool is the pgx connection pool; txService uses it to begin transactions.
//   - repo is the DistributionRequest repository (typically
//     adapters/distributionrequest.Repository).
//   - distributorRepo validates the referenced distributor exists.
//   - collectionRequestRepo validates the referenced collection request exists.
//   - transformedDataRepo validates a referenced transformed record exists and
//     belongs to the collection request.
//   - meter is the OTel Meter used to register per-method service metrics under
//     the "baasparse.distributionrequest.service.*" namespace.
//
// Returns an error if any OTel instrument fails to register.
func NewService(
	pool db.TxBeginner,
	repo module.Repository,
	distributorRepo distributor.Repository,
	collectionRequestRepo collectionrequest.Repository,
	transformedDataRepo transformeddata.Repository,
	meter metric.Meter,
) (ServiceImpl, error) {
	const prefix = "baasparse.distributionrequest.service."

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
	getByDUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_d_uid")
	if err != nil {
		return ServiceImpl{}, err
	}
	getByCRUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_cr_uid")
	if err != nil {
		return ServiceImpl{}, err
	}
	getByTDUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_td_uid")
	if err != nil {
		return ServiceImpl{}, err
	}

	return ServiceImpl{
		teleService: teleService{
			svc: txService{
				svc: service{
					repo:                  repo,
					distributorRepo:       distributorRepo,
					collectionRequestRepo: collectionRequestRepo,
					transformedDataRepo:   transformedDataRepo,
				},
				pool: pool,
			},
			create:     create,
			update:     update,
			getByUid:   getByUid,
			getByDUid:  getByDUid,
			getByCRUid: getByCRUid,
			getByTDUid: getByTDUid,
		},
	}, nil
}
