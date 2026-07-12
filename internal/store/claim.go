package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// isUniqueViolation reports whether err is a Postgres 23505 unique-violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func isNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// Claim is an owned distributed file claim/lease (FC_FILE_CLAIM, BR-HA-003/004).
type Claim struct {
	UID        int64
	Fence      int64
	StallCount int64
}

// LeaseSeconds is the file-claim lease duration; HeartbeatSeconds how often the
// owner renews it. A lease that is not renewed within LeaseSeconds is takeable
// by another instance (dead-owner recovery).
const (
	LeaseSeconds     = 30
	HeartbeatSeconds = 10
)

// querier is satisfied by both *pgxpool.Pool and pgx.Tx.
type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// AlreadyProcessed reports whether a DONE Processed File already exists for this
// source+name — a re-arrival of an already-processed file (BR-COL-006), or the
// crash/move-failure window where the record committed but the source was not
// yet moved out of the input location.
func (p *PG) AlreadyProcessed(ctx context.Context, srcUID int64, name string) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM PF_PROCESSED_FILE WHERE PF_SRC_UID=$1 AND PF_NAME=$2 AND PF_STATUS='DONE')`,
		srcUID, name).Scan(&exists)
	return exists, err
}

// AlreadyQuarantined reports whether this source+name was already quarantined as
// a poison file (BR-COL-017) — so a failed quarantine-move does not re-arm the
// crash/retry loop.
func (p *PG) AlreadyQuarantined(ctx context.Context, srcUID int64, name string) (bool, error) {
	var exists bool
	err := p.pool.QueryRow(ctx,
		`SELECT EXISTS(SELECT 1 FROM PF_PROCESSED_FILE WHERE PF_SRC_UID=$1 AND PF_NAME=$2 AND PF_STATUS='QUARANTINED')`,
		srcUID, name).Scan(&exists)
	return exists, err
}

// ClaimFile attempts to claim a file exactly once across the cluster (BR-HA-003).
// It first tries a fresh claim (INSERT … ON CONFLICT DO NOTHING on the partial
// unique index of HELD rows); on conflict it takes over only if the existing
// lease has expired (dead owner), bumping the fence and stall count. ok=false
// means another live instance owns it.
func (p *PG) ClaimFile(ctx context.Context, srcUID int64, name string) (Claim, bool, error) {
	if p.insUID == nil {
		return Claim{}, false, fmt.Errorf("instance not registered")
	}
	me := *p.insUID

	// 1. Fresh claim.
	var c Claim
	err := p.pool.QueryRow(ctx, `
		INSERT INTO FC_FILE_CLAIM (FC_SRC_UID, FC_FILE_NAME, FC_INS_UID, FC_STATUS, FC_EXPIRES_ON, FC_CREATED_BY, FC_MODIFIED_BY)
		VALUES ($1,$2,$3,'HELD', now() + make_interval(secs => $4), 'engine', 'engine')
		ON CONFLICT (FC_SRC_UID, FC_FILE_NAME) WHERE FC_STATUS='HELD' DO NOTHING
		RETURNING FC_UID, FC_FENCE, FC_STALL_COUNT`,
		srcUID, name, me, LeaseSeconds).Scan(&c.UID, &c.Fence, &c.StallCount)
	if err == nil {
		return c, true, nil
	}
	if err != pgx.ErrNoRows {
		return Claim{}, false, fmt.Errorf("claim insert: %w", err)
	}

	// 2. Conflict → an existing HELD claim. Take over only if its lease expired.
	err = p.pool.QueryRow(ctx, `
		UPDATE FC_FILE_CLAIM
		   SET FC_INS_UID=$3, FC_FENCE=FC_FENCE+1, FC_ATTEMPT=FC_ATTEMPT+1,
		       FC_EXPIRES_ON = now() + make_interval(secs => $4), FC_HEARTBEAT_ON=now(),
		       FC_STALL_COUNT = FC_STALL_COUNT + CASE WHEN FC_CHECKPOINT_RECORD = 0 THEN 1 ELSE 0 END,
		       FC_MODIFIED_ON=now()
		 WHERE FC_SRC_UID=$1 AND FC_FILE_NAME=$2 AND FC_STATUS='HELD' AND FC_EXPIRES_ON < now()
		RETURNING FC_UID, FC_FENCE, FC_STALL_COUNT`,
		srcUID, name, me, LeaseSeconds).Scan(&c.UID, &c.Fence, &c.StallCount)
	if err == nil {
		return c, true, nil
	}
	if err == pgx.ErrNoRows {
		return Claim{}, false, nil // owned by a live instance
	}
	return Claim{}, false, fmt.Errorf("claim takeover: %w", err)
}

// HeartbeatClaim renews the lease; held=false means the claim was lost (another
// instance took over, or it was released).
func (p *PG) HeartbeatClaim(ctx context.Context, c Claim) (bool, error) {
	if p.insUID == nil {
		return false, nil
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE FC_FILE_CLAIM SET FC_HEARTBEAT_ON=now(), FC_EXPIRES_ON = now() + make_interval(secs => $4), FC_MODIFIED_ON=now()
		 WHERE FC_UID=$1 AND FC_INS_UID=$2 AND FC_FENCE=$3 AND FC_STATUS='HELD'`,
		c.UID, *p.insUID, c.Fence, LeaseSeconds)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

// ReleaseClaim releases a held claim (used for the re-arrival/skip path).
func (p *PG) ReleaseClaim(ctx context.Context, c Claim) error {
	if p.insUID == nil {
		return nil
	}
	_, err := p.pool.Exec(ctx, `
		UPDATE FC_FILE_CLAIM SET FC_STATUS='RELEASED', FC_MODIFIED_ON=now()
		 WHERE FC_UID=$1 AND FC_INS_UID=$2 AND FC_FENCE=$3 AND FC_STATUS='HELD'`,
		c.UID, *p.insUID, c.Fence)
	return err
}

// CompleteClaimed atomically records the Processed File and releases the claim,
// guarded by the fence: if the claim is no longer held by this instance at this
// fence (a takeover happened while we processed), it records nothing and returns
// recorded=false, so the current owner reprocesses (BR-HA-004, no double-record).
func (p *PG) CompleteClaimed(ctx context.Context, pf ProcessedFile, c Claim) (bool, error) {
	if p.insUID == nil {
		return false, fmt.Errorf("instance not registered")
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var insUID, fence int64
	var status string
	if err := tx.QueryRow(ctx,
		`SELECT FC_INS_UID, FC_FENCE, FC_STATUS FROM FC_FILE_CLAIM WHERE FC_UID=$1 FOR UPDATE`,
		c.UID).Scan(&insUID, &fence, &status); err != nil {
		return false, fmt.Errorf("lock claim: %w", err)
	}
	if insUID != *p.insUID || fence != c.Fence || status != "HELD" {
		return false, nil // lost the claim
	}

	srcUID, plvUID, err := resolveSrcPlv(ctx, tx, pf.PipelineID)
	if err != nil {
		return false, err
	}
	pfUID, err := insertPFAndRS(ctx, tx, pf, srcUID, plvUID, p.insUID)
	if err != nil {
		// A concurrent instance already recorded this file (the claim/re-arrival
		// race, caught by UX_PF_SRC_NAME_DONE): treat as "not recorded by us" so
		// the caller discards its output — not a hard error (BR-HA-004).
		if isUniqueViolation(err) {
			return false, nil
		}
		return false, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE FC_FILE_CLAIM SET FC_STATUS='RELEASED', FC_PF_UID=$2, FC_MODIFIED_ON=now() WHERE FC_UID=$1`,
		c.UID, pfUID); err != nil {
		return false, fmt.Errorf("release claim: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// QuarantineFile records a poison file as QUARANTINED (BR-COL-017) and releases
// the claim, fence-guarded and atomic. It appears in reconciliation as an
// unprocessed/quarantined file-level state (BR-REC-008), neither lost nor done.
func (p *PG) QuarantineFile(ctx context.Context, pipelineID, srcUID int64, name string, size int64, reason string, c Claim) (bool, error) {
	if p.insUID == nil {
		return false, fmt.Errorf("instance not registered")
	}
	reasonCode, errorText := splitReason(reason)
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	var insUID, fence int64
	var status string
	if err := tx.QueryRow(ctx,
		`SELECT FC_INS_UID, FC_FENCE, FC_STATUS FROM FC_FILE_CLAIM WHERE FC_UID=$1 FOR UPDATE`,
		c.UID).Scan(&insUID, &fence, &status); err != nil {
		return false, fmt.Errorf("lock claim: %w", err)
	}
	if insUID != *p.insUID || fence != c.Fence || status != "HELD" {
		return false, nil
	}
	_, plvUID, err := resolveSrcPlv(ctx, tx, pipelineID)
	if err != nil {
		return false, err
	}
	var fileUID int64
	if err := tx.QueryRow(ctx,
		`UPDATE SQ_SEQUENCE_ALLOCATOR SET SQ_LAST_VALUE = SQ_LAST_VALUE + 1, SQ_MODIFIED_ON=now() WHERE SQ_NAME='FILE_UID' RETURNING SQ_LAST_VALUE`).
		Scan(&fileUID); err != nil {
		return false, fmt.Errorf("allocate file uid: %w", err)
	}
	var pfUID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO PF_PROCESSED_FILE (PF_FILE_UID, PF_SRC_UID, PF_PLV_UID, PF_NAME, PF_PATH, PF_SIZE_BYTES,
			PF_STATUS, PF_RECON_STATE, PF_ATTEMPT_COUNT, PF_REASON_CODE, PF_ERROR_TEXT, PF_INS_UID, PF_CREATED_BY, PF_MODIFIED_BY)
		VALUES ($1,$2,$3,$4,$4,$5,'QUARANTINED','INDETERMINATE_COUNT',$6,NULLIF($7,''),NULLIF($8,''),$9,'engine','engine') RETURNING PF_UID`,
		fileUID, srcUID, plvUID, name, size, c.StallCount, reasonCode, errorText, p.insUID).Scan(&pfUID); err != nil {
		return false, fmt.Errorf("insert quarantined PF: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO RS_RECONCILIATION_SUMMARY (RS_SCOPE, RS_PF_UID, RS_SRC_UID, RS_BASELINE_KIND, RS_QUARANTINED_COUNT, RS_STATE, RS_CREATED_BY, RS_MODIFIED_BY)
		VALUES ('FILE',$1,$2,'INDETERMINATE',1,'INDETERMINATE','engine','engine')`,
		pfUID, srcUID); err != nil {
		return false, fmt.Errorf("insert quarantine RS: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`UPDATE FC_FILE_CLAIM SET FC_STATUS='RELEASED', FC_PF_UID=$2, FC_MODIFIED_ON=now() WHERE FC_UID=$1`,
		c.UID, pfUID); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}

// RecoverProcessedFile re-creates a Processed File row from an on-disk/object
// completion marker when the database is missing it (e.g. restored behind the
// storage backend) — recovery, not reprocessing (BR-NFR-017). Idempotent: a
// duplicate (the DB already has it) returns recovered=false via the unique index.
func (p *PG) RecoverProcessedFile(ctx context.Context, pf ProcessedFile) (bool, error) {
	srcUID, plvUID, err := resolveSrcPlv(ctx, p.pool, pf.PipelineID)
	if err != nil {
		return false, err
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	if _, err := insertPFAndRS(ctx, tx, pf, srcUID, plvUID, p.insUID); err != nil {
		if isUniqueViolation(err) {
			return false, nil // already present — consistent
		}
		return false, err
	}
	// Advance the file-UID allocator floor to at least this recovered UID, so a
	// DB restored behind storage does not re-issue an already-used file UID and
	// collide on UX_PF_FILE_UID (BR-NFR-017 recovery correctness).
	if _, err := tx.Exec(ctx,
		`UPDATE SQ_SEQUENCE_ALLOCATOR SET SQ_LAST_VALUE = GREATEST(SQ_LAST_VALUE, $1), SQ_MODIFIED_ON=now() WHERE SQ_NAME='FILE_UID'`,
		pf.FileUID); err != nil {
		return false, fmt.Errorf("advance file-uid allocator: %w", err)
	}
	return true, tx.Commit(ctx)
}

// ReconcileOnce runs fn only if this instance wins a cluster-wide advisory lock
// (single-owner startup reconciliation, BR-NFR-017). ran=false means another
// instance holds the lock and is reconciling.
func (p *PG) ReconcileOnce(ctx context.Context, fn func(context.Context) error) (bool, error) {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return false, err
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('baasparse.reconcile'))`).Scan(&got); err != nil {
		return false, err
	}
	if !got {
		return false, nil
	}
	// Release on a context detached from the caller's cancellation, so a SIGTERM
	// during reconciliation still frees the session advisory lock.
	defer func() {
		uctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, _ = conn.Exec(uctx, `SELECT pg_advisory_unlock(hashtext('baasparse.reconcile'))`)
	}()
	return true, fn(ctx)
}

// splitReason separates a quarantine reason into a short UPPER_SNAKE code (the
// leading token before the first ':', as produced by the pipeline/runner error
// convention TS 02 §2.3.1) and the full detail text. An empty reason yields the
// generic PROCESSING_ERROR code.
func splitReason(reason string) (code, text string) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return "PROCESSING_ERROR", ""
	}
	for i := 0; i < len(reason); i++ {
		ch := reason[i]
		if ch == ':' {
			if i > 0 {
				return reason[:i], reason
			}
			break
		}
		if !(ch >= 'A' && ch <= 'Z' || ch == '_') {
			break
		}
	}
	return "PROCESSING_ERROR", reason
}

// resolveSrcPlv finds the published source and pipeline-version for a pipeline.
func resolveSrcPlv(ctx context.Context, q querier, pipelineID int64) (srcUID, plvUID int64, err error) {
	if err = q.QueryRow(ctx,
		`SELECT SRC_UID FROM SRC_SOURCE WHERE SRC_PL_UID=$1 AND SRC_STATUS='PUBLISHED' AND SRC_END_DATE IS NULL LIMIT 1`,
		pipelineID).Scan(&srcUID); err != nil {
		return 0, 0, fmt.Errorf("resolve SRC for pipeline %d: %w", pipelineID, err)
	}
	if err = q.QueryRow(ctx,
		`SELECT PLV_UID FROM PLV_PIPELINE_VERSION WHERE PLV_PL_UID=$1 AND PLV_STATUS='PUBLISHED' AND PLV_END_DATE IS NULL LIMIT 1`,
		pipelineID).Scan(&plvUID); err != nil {
		return 0, 0, fmt.Errorf("resolve PLV for pipeline %d: %w", pipelineID, err)
	}
	return srcUID, plvUID, nil
}

// insertPFAndRS writes the Processed File + Reconciliation Summary rows.
func insertPFAndRS(ctx context.Context, q querier, pf ProcessedFile, srcUID, plvUID int64, insUID *int64) (int64, error) {
	var pfUID int64
	if err := q.QueryRow(ctx, `
		INSERT INTO PF_PROCESSED_FILE (PF_FILE_UID, PF_SRC_UID, PF_PLV_UID, PF_NAME, PF_PATH, PF_SIZE_BYTES,
			PF_STATUS, PF_RECON_STATE, PF_RECORD_COUNT, PF_COMPLETION_MARKER, PF_INS_UID, PF_COMPLETED_ON, PF_CREATED_BY, PF_MODIFIED_BY)
		VALUES ($1,$2,$3,$4,$5,$6,'DONE','BALANCED',$7,$8,$9,now(),'engine','engine') RETURNING PF_UID`,
		pf.FileUID, srcUID, plvUID, pf.Name, pf.Name, pf.Size,
		pf.RecordsIn, pf.OutputName, insUID).Scan(&pfUID); err != nil {
		return 0, fmt.Errorf("insert PF: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO RS_RECONCILIATION_SUMMARY (RS_SCOPE, RS_PF_UID, RS_SRC_UID, RS_BASELINE_KIND, RS_IN_COUNT,
			RS_ROUTED_COUNT, RS_SUSPENDED_COUNT, RS_STATE, RS_CREATED_BY, RS_MODIFIED_BY)
		VALUES ('FILE',$1,$2,'DECODED',$3,$4,$5,'BALANCED','engine','engine')`,
		pfUID, srcUID, pf.RecordsIn, pf.RecordsOut, pf.Suspended); err != nil {
		return 0, fmt.Errorf("insert RS: %w", err)
	}
	return pfUID, nil
}
