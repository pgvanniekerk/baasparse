package collectionrequest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/metric"

	"github.com/pgvanniekerk/baasparse/internal/db"
	module "github.com/pgvanniekerk/baasparse/internal/module/collectionrequest"
	"github.com/pgvanniekerk/baasparse/internal/module/collector"
	"github.com/pgvanniekerk/baasparse/internal/module/pipeline"
	"github.com/pgvanniekerk/baasparse/internal/util/telemetry"
)

// ── inner service (business logic) ───────────────────────────────────────────

// service contains the core application logic. It validates the referenced
// pipeline and collector, derives the content hash for each payload, and
// expects a pgx.Tx to already be present in the context for Create; the
// txService wrapper provides it. Read operations use the pool directly via the
// repository.
type service struct {
	repo          module.Repository
	pipelineRepo  pipeline.Repository
	collectorRepo collector.Repository
}

// hashKey derives the value stored in CR_HASH_KEY from the raw payload. This is
// deliberately the service's responsibility (not the repository's): the hash
// defines payload identity for deduplication, which is domain logic. It is a
// hex-encoded SHA-256 digest of the bytes.
func hashKey(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (s service) Create(ctx context.Context, cmd module.CreateCommand) (module.CollectionRequest, error) {
	// Validate the referenced pipeline and collector exist up front rather than
	// relying on a foreign-key violation surfacing from the insert.
	if _, err := s.pipelineRepo.GetByUid(ctx, cmd.PipelineUid); err != nil {
		return module.CollectionRequest{}, fmt.Errorf("collectionrequest: fetch pipeline: %w", err)
	}
	coll, err := s.collectorRepo.GetByUid(ctx, cmd.CollectorUid)
	if err != nil {
		return module.CollectionRequest{}, fmt.Errorf("collectionrequest: fetch collector: %w", err)
	}
	// A collector is bound to exactly one pipeline (C_COLLECTOR.C_P_UID), so the
	// collector named on the request must belong to the request's pipeline.
	if coll.PipelineUid != cmd.PipelineUid {
		return module.CollectionRequest{}, fmt.Errorf("%w: collector %s belongs to pipeline %s", module.ErrCollectorPipelineMismatch, cmd.CollectorUid, coll.PipelineUid)
	}

	key := hashKey(cmd.Data)

	// Skip re-ingestion of an identical payload for the same (pipeline,
	// collector). The unique index remains the hard guarantee against races.
	exists, err := s.repo.ExistsByPUidAndCUidAndHashKey(ctx, cmd.PipelineUid, cmd.CollectorUid, key)
	if err != nil {
		return module.CollectionRequest{}, fmt.Errorf("collectionrequest: check duplicate: %w", err)
	}
	if exists {
		return module.CollectionRequest{}, fmt.Errorf("%w: %s", module.ErrAlreadyExists, key)
	}

	// Persist with the service-computed hash. The repository fills in the
	// database-generated Uid and TimestampTz and returns the full record.
	return s.repo.Create(ctx, module.CollectionRequest{
		PipelineUid:  cmd.PipelineUid,
		CollectorUid: cmd.CollectorUid,
		HashKey:      key,
		FileName:     cmd.FileName,
		Data:         cmd.Data,
	})
}

func (s service) GetByUid(ctx context.Context, uid uuid.UUID) (module.CollectionRequest, error) {
	return s.repo.GetByUid(ctx, uid)
}

func (s service) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]module.CollectionRequest, error) {
	return s.repo.GetByPUid(ctx, pipelineUid)
}

func (s service) GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]module.CollectionRequest, error) {
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
func (t txService) Create(ctx context.Context, cmd module.CreateCommand) (module.CollectionRequest, error) {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return module.CollectionRequest{}, fmt.Errorf("collectionrequest: begin transaction: %w", err)
	}

	result, err := t.svc.Create(db.WithTx(ctx, tx), cmd)
	if err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.CollectionRequest{}, fmt.Errorf("collectionrequest: rollback failed (%w) after: %w", rbErr, err)
		}
		return module.CollectionRequest{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return module.CollectionRequest{}, fmt.Errorf("collectionrequest: rollback failed (%w) after commit error: %w", rbErr, err)
		}
		return module.CollectionRequest{}, fmt.Errorf("collectionrequest: commit: %w", err)
	}
	return result, nil
}

// Read methods do not require a transaction; they delegate directly.

func (t txService) GetByUid(ctx context.Context, uid uuid.UUID) (module.CollectionRequest, error) {
	return t.svc.GetByUid(ctx, uid)
}

func (t txService) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]module.CollectionRequest, error) {
	return t.svc.GetByPUid(ctx, pipelineUid)
}

func (t txService) GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]module.CollectionRequest, error) {
	return t.svc.GetByCUid(ctx, collectorUid)
}

// ── telemetry wrapper ─────────────────────────────────────────────────────────

// teleService is the outermost layer. It records OTel metrics for every call
// then delegates to txService, which manages transaction lifecycle.
//
// Full stack: teleService → txService → service
type teleService struct {
	svc       txService
	create    telemetry.MethodInstruments
	getByUid  telemetry.MethodInstruments
	getByPUid telemetry.MethodInstruments
	getByCUid telemetry.MethodInstruments
}

func (t teleService) Create(ctx context.Context, cmd module.CreateCommand) (module.CollectionRequest, error) {
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

func (t teleService) GetByUid(ctx context.Context, uid uuid.UUID) (module.CollectionRequest, error) {
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

func (t teleService) GetByPUid(ctx context.Context, pipelineUid uuid.UUID) ([]module.CollectionRequest, error) {
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

func (t teleService) GetByCUid(ctx context.Context, collectorUid uuid.UUID) ([]module.CollectionRequest, error) {
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

// ServiceImpl is the exported, fully wired collectionrequest service adapter.
// It embeds teleService, which delegates to txService, which delegates to
// service. Construct with NewService; do not instantiate directly.
type ServiceImpl struct {
	teleService
}

// NewService builds a fully wired collectionrequest service.
//
//   - pool is the pgx connection pool; txService uses it to begin transactions.
//   - repo is the CollectionRequest repository (typically
//     adapters/collectionrequest.Repository).
//   - pipelineRepo and collectorRepo validate that the referenced pipeline and
//     collector exist before insert.
//   - meter is the OTel Meter used to register per-method service metrics under
//     the "baasparse.collectionrequest.service.*" namespace.
//
// Returns an error if any OTel instrument fails to register.
func NewService(
	pool db.TxBeginner,
	repo module.Repository,
	pipelineRepo pipeline.Repository,
	collectorRepo collector.Repository,
	meter metric.Meter,
) (ServiceImpl, error) {
	const prefix = "baasparse.collectionrequest.service."

	create, err := telemetry.NewMethodInstruments(meter, prefix+"create")
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
	getByCUid, err := telemetry.NewMethodInstruments(meter, prefix+"get_by_c_uid")
	if err != nil {
		return ServiceImpl{}, err
	}

	return ServiceImpl{
		teleService: teleService{
			svc: txService{
				svc: service{
					repo:          repo,
					pipelineRepo:  pipelineRepo,
					collectorRepo: collectorRepo,
				},
				pool: pool,
			},
			create:    create,
			getByUid:  getByUid,
			getByPUid: getByPUid,
			getByCUid: getByCUid,
		},
	}, nil
}
