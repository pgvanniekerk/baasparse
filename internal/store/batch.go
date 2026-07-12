package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"
)

// This file implements output consolidation (TS 07 §7.3.2, BR-DST-005): N input
// files are concatenated into ONE output object per destination, and destinations
// deliver INDEPENDENTLY so a revenue-assurance outage cannot stall the billing
// feed.
//
// Independence is what forces the durable state here. If destinations could only
// succeed or fail together, a batch would need no identity — it would just be
// retried whole. But because billing may publish while revenue assurance fails,
// the next attempt MUST know that billing already has these records, or it would
// deliver them twice. So a batch is:
//
//   FROZEN    — its member set is pinned at formation (BT/BF rows) and hashed into
//               an identity, so a retry resumes the same batch instead of forming
//               a new one that overlaps it.
//   PER-DEST  — each destination's progress is its own DL_DELIVERY row, keyed by
//               (DS_UID, batch identity). A retry re-delivers only where there is
//               no DELIVERED row.
//   GATED     — the contributing files reach DONE only when EVERY destination has
//               delivered (BR-COL-009). Until then the batch stays OPEN and its
//               files are excluded from any other batch.

// ErrBatchOverlap means a proposed batch shares a file with one that is already
// open. Forming it anyway would deliver the shared file's records twice.
var ErrBatchOverlap = errors.New("proposed batch overlaps an open batch")

// OpenBatch is a batch awaiting completion, with the deliveries already made.
type OpenBatch struct {
	Batch
	// Delivered is the set of DS_UIDs that have already published this batch and
	// must NOT be written again.
	Delivered map[int64]bool
}

// FormBatch pins a set of files as a batch and returns it. If another instance
// already formed the identical batch (same files), that batch is returned instead
// of a duplicate — the unique index on (SRC_UID, identity) makes the race benign,
// and the loser simply resumes the winner's work.
func (p *PG) FormBatch(ctx context.Context, pipelineID, srcUID int64, files []BatchFile) (OpenBatch, error) {
	if len(files) == 0 {
		return OpenBatch{}, fmt.Errorf("cannot form an empty batch")
	}
	sorted := make([]BatchFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	identity := BatchIdentity(sorted)

	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return OpenBatch{}, err
	}
	defer tx.Rollback(ctx)

	// Serialize batch FORMATION per source.
	//
	// The identity index alone is not enough. It only collides when two instances
	// propose the IDENTICAL file set — but they routinely propose different ones: A
	// sees {1,2,3} and forms a batch while B, listing a moment later, sees {1,2,3,4}.
	// Those hash differently, so both are inserted OPEN over overlapping files, both
	// deliver, and the shared members' records go out TWICE. (The scan's
	// BatchedFileNames check cannot close this: B read the pinned set before A
	// inserted.) The lock plus the re-check below make formation atomic with respect
	// to what is already pinned.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, srcUID); err != nil {
		return OpenBatch{}, fmt.Errorf("lock source for batch formation: %w", err)
	}
	names := make([]string, len(sorted))
	for i, f := range sorted {
		names[i] = f.Name
	}
	var pinned string
	err = tx.QueryRow(ctx, `
		SELECT BF.BF_NAME FROM BF_BATCH_FILE BF
		JOIN BT_BATCH BT ON BT.BT_UID = BF.BF_BT_UID
		WHERE BT.BT_SRC_UID=$1 AND BT.BT_STATUS='OPEN' AND BF.BF_NAME = ANY($2) LIMIT 1`,
		srcUID, names).Scan(&pinned)
	if err == nil {
		// Someone else already owns one of these files. Do not form an overlapping
		// batch; their batch will be resumed on the next scan.
		return OpenBatch{}, fmt.Errorf("%w: %q is already in an open batch", ErrBatchOverlap, pinned)
	}
	if !isNoRows(err) {
		return OpenBatch{}, fmt.Errorf("check pinned files: %w", err)
	}

	_, plvUID, err := resolveSrcPlv(ctx, tx, pipelineID)
	if err != nil {
		return OpenBatch{}, err
	}

	var total int64
	for _, f := range sorted {
		total += f.Size
	}
	var btUID int64
	err = tx.QueryRow(ctx, `
		INSERT INTO BT_BATCH (BT_PL_UID, BT_SRC_UID, BT_PLV_UID, BT_IDENTITY, BT_STATUS, BT_FILE_COUNT, BT_SIZE_BYTES, BT_CREATED_BY, BT_MODIFIED_BY)
		VALUES ($1,$2,$3,$4,'OPEN',$5,$6,'engine','engine') RETURNING BT_UID`,
		pipelineID, srcUID, plvUID, identity, len(sorted), total).Scan(&btUID)
	if err != nil {
		if isUniqueViolation(err) {
			// Another instance formed this exact batch first. Resume it rather than
			// competing: its delivery rows (and reserved sequence numbers) are the
			// authoritative record of what has already been published.
			_ = tx.Rollback(ctx)
			b, gerr := p.GetOpenBatch(ctx, srcUID, identity)
			if gerr != nil {
				// The winner already finished and closed it — nothing to resume.
				return OpenBatch{}, fmt.Errorf("%w: identical batch already completed", ErrBatchOverlap)
			}
			return b, nil
		}
		return OpenBatch{}, fmt.Errorf("insert batch: %w", err)
	}
	for i, f := range sorted {
		if _, err := tx.Exec(ctx, `
			INSERT INTO BF_BATCH_FILE (BF_BT_UID, BF_NAME, BF_SIZE_BYTES, BF_SEQ) VALUES ($1,$2,$3,$4)`,
			btUID, f.Name, f.Size, i); err != nil {
			return OpenBatch{}, fmt.Errorf("insert batch file %q: %w", f.Name, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return OpenBatch{}, err
	}
	return OpenBatch{
		Batch:     Batch{UID: btUID, Identity: identity, Files: sorted},
		Delivered: map[int64]bool{},
	}, nil
}

// GetOpenBatch loads an OPEN batch by identity, with the destinations that have
// already delivered it.
func (p *PG) GetOpenBatch(ctx context.Context, srcUID int64, identity string) (OpenBatch, error) {
	var b OpenBatch
	if err := p.pool.QueryRow(ctx, `
		SELECT BT_UID, BT_IDENTITY, BT_ATTEMPTS FROM BT_BATCH
		WHERE BT_SRC_UID=$1 AND BT_IDENTITY=$2 AND BT_STATUS='OPEN'`,
		srcUID, identity).Scan(&b.UID, &b.Identity, &b.Attempts); err != nil {
		return OpenBatch{}, fmt.Errorf("load open batch: %w", err)
	}
	return p.hydrateBatch(ctx, &b)
}

// ListOpenBatches returns the source's OPEN batches, oldest first — work that a
// previous attempt started and must finish before any new batch is formed.
func (p *PG) ListOpenBatches(ctx context.Context, srcUID int64) ([]OpenBatch, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT BT_UID, BT_IDENTITY, BT_ATTEMPTS FROM BT_BATCH
		WHERE BT_SRC_UID=$1 AND BT_STATUS='OPEN' ORDER BY BT_UID`, srcUID)
	if err != nil {
		return nil, err
	}
	var out []OpenBatch
	for rows.Next() {
		var b OpenBatch
		if err := rows.Scan(&b.UID, &b.Identity, &b.Attempts); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if _, err := p.hydrateBatch(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (p *PG) hydrateBatch(ctx context.Context, b *OpenBatch) (OpenBatch, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT BF_NAME, BF_SIZE_BYTES, BF_RECORDS_IN, BF_RECORDS_OUT, BF_SUSPENDED
		 FROM BF_BATCH_FILE WHERE BF_BT_UID=$1 ORDER BY BF_SEQ`, b.UID)
	if err != nil {
		return OpenBatch{}, err
	}
	b.Files = nil
	for rows.Next() {
		var f BatchFile
		if err := rows.Scan(&f.Name, &f.Size, &f.RecordsIn, &f.RecordsOut, &f.Suspended); err != nil {
			rows.Close()
			return OpenBatch{}, err
		}
		b.Files = append(b.Files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return OpenBatch{}, err
	}

	// Which destinations already have this batch? Those must never be written
	// again — that is the whole point of the durable identity.
	drows, err := p.pool.Query(ctx,
		`SELECT DL_DS_UID FROM DL_DELIVERY WHERE DL_OUTPUT_IDENTITY=$1 AND DL_STATUS='DELIVERED'`, b.Batch.DeliveryKey())
	if err != nil {
		return OpenBatch{}, err
	}
	b.Delivered = map[int64]bool{}
	for drows.Next() {
		var ds int64
		if err := drows.Scan(&ds); err != nil {
			drows.Close()
			return OpenBatch{}, err
		}
		b.Delivered[ds] = true
	}
	drows.Close()
	return *b, drows.Err()
}

// BatchedFileNames returns the names of files already pinned into an OPEN batch
// for this source. The scanner excludes them: a file that belongs to a batch which
// has partially delivered must not be swept into a different batch, or the
// destinations that already published it would receive its records twice.
func (p *PG) BatchedFileNames(ctx context.Context, srcUID int64) (map[string]bool, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT BF.BF_NAME FROM BF_BATCH_FILE BF
		JOIN BT_BATCH BT ON BT.BT_UID = BF.BF_BT_UID
		WHERE BT.BT_SRC_UID=$1 AND BT.BT_STATUS='OPEN'`, srcUID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

// ReserveDelivery gets-or-creates the delivery row for (destination, batch) and
// returns it with its sequence number.
//
// The sequence is allocated ONCE, here, and reused by every retry of this batch.
// Allocating per attempt would burn a number on each failure, and a gap in the
// delivered sequence would then mean "an attempt failed" rather than "an output is
// missing" — which is exactly backwards from what a downstream gap check needs
// (BR-DST-011). A gap can therefore only be produced by an ABANDONED batch.
func (p *PG) ReserveDelivery(ctx context.Context, pipelineID int64, o Output, b Batch, nameFor func(seq int64) string) (Delivery, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return Delivery{}, err
	}
	defer tx.Rollback(ctx)

	var d Delivery
	d.DSUID = o.DSUID
	// Existing reservation (a previous attempt): reuse it verbatim, including the
	// output name, so a retry overwrites the same object rather than orphaning one.
	err = tx.QueryRow(ctx, `
		SELECT DL_UID, COALESCE(DL_SEQUENCE_NO,0), COALESCE(DL_OUTPUT_NAME,''), DL_STATUS
		FROM DL_DELIVERY WHERE DL_DS_UID=$1 AND DL_OUTPUT_IDENTITY=$2 FOR UPDATE`,
		o.DSUID, b.DeliveryKey()).Scan(&d.UID, &d.SequenceNo, &d.OutputName, &d.Status)
	if err == nil {
		if cerr := tx.Commit(ctx); cerr != nil {
			return Delivery{}, cerr
		}
		return d, nil
	}
	if !isNoRows(err) {
		return Delivery{}, fmt.Errorf("load delivery: %w", err)
	}

	_, plvUID, err := resolveSrcPlv(ctx, tx, pipelineID)
	if err != nil {
		return Delivery{}, err
	}
	// Gapless per-destination sequence (DSQ). Upsert so the first delivery to a
	// destination creates its counter.
	var seq int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO DSQ_DESTINATION_SEQUENCE (DSQ_DS_UID, DSQ_LAST_VALUE, DSQ_CREATED_BY, DSQ_MODIFIED_BY)
		VALUES ($1, 1, 'engine', 'engine')
		ON CONFLICT (DSQ_DS_UID) DO UPDATE SET DSQ_LAST_VALUE = DSQ_DESTINATION_SEQUENCE.DSQ_LAST_VALUE + 1, DSQ_MODIFIED_ON = now()
		RETURNING DSQ_LAST_VALUE`, o.DSUID).Scan(&seq); err != nil {
		return Delivery{}, fmt.Errorf("allocate destination sequence: %w", err)
	}
	d.SequenceNo = seq
	d.OutputName = nameFor(seq)
	d.Status = "PENDING"
	if err := tx.QueryRow(ctx, `
		INSERT INTO DL_DELIVERY (DL_DS_UID, DL_PLV_UID, DL_OUTPUT_IDENTITY, DL_KIND, DL_OUTPUT_NAME, DL_SEQUENCE_NO, DL_STATUS, DL_CREATED_BY, DL_MODIFIED_BY)
		VALUES ($1,$2,$3,'ORIGINAL',$4,$5,'PENDING','engine','engine') RETURNING DL_UID`,
		o.DSUID, plvUID, b.DeliveryKey(), d.OutputName, seq).Scan(&d.UID); err != nil {
		if isUniqueViolation(err) {
			// Raced with another instance reserving the same (destination, batch).
			// Re-read theirs — one reservation, one sequence number, no gap.
			_ = tx.Rollback(ctx)
			return p.getDelivery(ctx, o.DSUID, b.DeliveryKey())
		}
		return Delivery{}, fmt.Errorf("insert delivery: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Delivery{}, err
	}
	return d, nil
}

func (p *PG) getDelivery(ctx context.Context, dsUID int64, identity string) (Delivery, error) {
	var d Delivery
	d.DSUID = dsUID
	err := p.pool.QueryRow(ctx, `
		SELECT DL_UID, COALESCE(DL_SEQUENCE_NO,0), COALESCE(DL_OUTPUT_NAME,''), DL_STATUS
		FROM DL_DELIVERY WHERE DL_DS_UID=$1 AND DL_OUTPUT_IDENTITY=$2`,
		dsUID, identity).Scan(&d.UID, &d.SequenceNo, &d.OutputName, &d.Status)
	return d, err
}

// MarkDelivered records that a destination's output is written and visible.
func (p *PG) MarkDelivered(ctx context.Context, d Delivery) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE DL_DELIVERY SET DL_STATUS='DELIVERED', DL_RECORD_COUNT=$2, DL_SIZE_BYTES=$3,
			DL_DELIVERED_ON=now(), DL_MODIFIED_ON=now() WHERE DL_UID=$1`,
		d.UID, d.Records, d.SizeBytes)
	return err
}

// MarkDeliveryFailed records a destination's failure without releasing its
// reserved sequence — the next attempt reuses it.
func (p *PG) MarkDeliveryFailed(ctx context.Context, d Delivery, reason string) error {
	_, err := p.pool.Exec(ctx, `
		UPDATE DL_DELIVERY SET DL_STATUS='PENDING', DL_LAST_ERROR=$2, DL_ATTEMPTS=DL_ATTEMPTS+1, DL_MODIFIED_ON=now()
		WHERE DL_UID=$1 AND DL_STATUS <> 'DELIVERED'`, d.UID, truncate(reason, 500))
	return err
}

// CloseBatch is the commit point: every destination has delivered, so the
// contributing files become DONE and their claims release — atomically, and only
// if we still hold every one of them.
//
// The fence check is a loop over the same guard CompleteClaimed uses for a single
// file: if ANY member's claim was taken over while we worked, we recorded nothing
// and the caller discards. Claims are locked in FC_UID order because two batchers
// holding overlapping claims in opposite orders would deadlock.
func (p *PG) CloseBatch(ctx context.Context, b Batch, pfs []ProcessedFile, claims []Claim, dels []Delivery, contribs map[int64][]Contribution) (bool, error) {
	if p.insUID == nil {
		return false, fmt.Errorf("instance not registered")
	}
	if len(pfs) != len(claims) {
		return false, fmt.Errorf("batch commit: %d files but %d claims", len(pfs), len(claims))
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)

	// Deterministic lock order: two instances whose batches overlap must not
	// deadlock against each other.
	order := make([]int, len(claims))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, c int) bool { return claims[order[a]].UID < claims[order[c]].UID })

	for _, i := range order {
		var insUID, fence int64
		var status string
		if err := tx.QueryRow(ctx,
			`SELECT FC_INS_UID, FC_FENCE, FC_STATUS FROM FC_FILE_CLAIM WHERE FC_UID=$1 FOR UPDATE`,
			claims[i].UID).Scan(&insUID, &fence, &status); err != nil {
			return false, fmt.Errorf("lock claim %d: %w", claims[i].UID, err)
		}
		if insUID != *p.insUID || fence != claims[i].Fence || status != "HELD" {
			return false, nil // lost a member's claim — record nothing, discard
		}
	}

	pfUIDs := make(map[string]int64, len(pfs))
	for _, i := range order {
		pf := pfs[i]
		srcUID, plvUID, err := resolveSrcPlv(ctx, tx, pf.PipelineID)
		if err != nil {
			return false, err
		}
		pfUID, err := insertPFAndRS(ctx, tx, pf, srcUID, plvUID, p.insUID)
		if err != nil {
			if isUniqueViolation(err) {
				return false, nil // another instance recorded a member; discard
			}
			return false, err
		}
		pfUIDs[pf.Name] = pfUID
		if _, err := tx.Exec(ctx,
			`UPDATE FC_FILE_CLAIM SET FC_STATUS='RELEASED', FC_PF_UID=$2, FC_MODIFIED_ON=now() WHERE FC_UID=$1`,
			claims[i].UID, pfUID); err != nil {
			return false, fmt.Errorf("release claim: %w", err)
		}
	}

	// Per-file lineage into each delivery: which input file contributed which
	// records to which output object, and at what offsets. This is what survives
	// consolidation — without it, "where did this file's records go" is
	// unanswerable once ten files merge into one.
	for _, d := range dels {
		for _, c := range contribs[d.DSUID] {
			pfUID, ok := pfUIDs[c.FileName]
			if !ok {
				continue
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO DC_DELIVERY_CONTRIBUTION (DC_DL_UID, DC_PF_UID, DC_RECORD_COUNT, DC_FIRST_RECORD_INDEX, DC_LAST_RECORD_INDEX, DC_CREATED_BY, DC_MODIFIED_BY)
				VALUES ($1,$2,$3,$4,$5,'engine','engine')
				ON CONFLICT (DC_DL_UID, DC_PF_UID) DO NOTHING`,
				d.UID, pfUID, c.Records, c.FirstIndex, c.LastIndex); err != nil {
				return false, fmt.Errorf("insert contribution: %w", err)
			}
		}
	}

	if _, err := tx.Exec(ctx,
		`UPDATE BT_BATCH SET BT_STATUS='CLOSED', BT_MODIFIED_ON=now() WHERE BT_UID=$1`, b.UID); err != nil {
		return false, fmt.Errorf("close batch: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// RecordBatchContributions persists each file's record counts as soon as the batch
// has been encoded. Without this, a batch that delivered to every destination and
// then crashed before committing would, on resume, have no decode to draw counts
// from and would record its files as DONE with zero records — a reconciliation lie
// about data that was in fact delivered.
func (p *PG) RecordBatchContributions(ctx context.Context, btUID int64, cs []Contribution) error {
	if len(cs) == 0 {
		return nil
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for _, c := range cs {
		if _, err := tx.Exec(ctx, `
			UPDATE BF_BATCH_FILE SET BF_RECORDS_IN=$3, BF_RECORDS_OUT=$4, BF_SUSPENDED=$5
			WHERE BF_BT_UID=$1 AND BF_NAME=$2`,
			btUID, c.FileName, c.RecordsIn, c.Records, c.Suspended); err != nil {
			return fmt.Errorf("record contribution %q: %w", c.FileName, err)
		}
	}
	return tx.Commit(ctx)
}

// TouchBatchAttempt records a failed attempt and its cause, so a batch that keeps
// failing is visible rather than silently spinning.
func (p *PG) TouchBatchAttempt(ctx context.Context, btUID int64, reason string) (int, error) {
	var n int
	err := p.pool.QueryRow(ctx, `
		UPDATE BT_BATCH SET BT_ATTEMPTS=BT_ATTEMPTS+1, BT_LAST_ERROR=$2, BT_MODIFIED_ON=now()
		WHERE BT_UID=$1 RETURNING BT_ATTEMPTS`, btUID, truncate(reason, 500)).Scan(&n)
	return n, err
}

// DropBatchFile removes a member from a batch that has not yet delivered anywhere —
// used when one input file turns out to be malformed: it is quarantined on its own
// claim and the batch re-forms without it.
//
// It refuses once any destination has published, because the member set is what
// those destinations were told: mutating it afterwards would corrupt the identity
// that makes the batch resumable.
func (p *PG) DropBatchFile(ctx context.Context, b Batch, name string) error {
	var delivered int
	if err := p.pool.QueryRow(ctx,
		`SELECT count(*) FROM DL_DELIVERY WHERE DL_OUTPUT_IDENTITY=$1 AND DL_STATUS='DELIVERED'`,
		b.DeliveryKey()).Scan(&delivered); err != nil {
		return err
	}
	if delivered > 0 {
		return fmt.Errorf("cannot drop %q: batch %s already delivered to %d destination(s)", name, b.Identity, delivered)
	}
	_, err := p.pool.Exec(ctx, `DELETE FROM BF_BATCH_FILE WHERE BF_BT_UID=$1 AND BF_NAME=$2`, b.UID, name)
	return err
}

// ErrBatchPartiallyDelivered marks an attempt to abandon a batch that has already
// published to at least one destination.
var ErrBatchPartiallyDelivered = errors.New("batch already delivered to a destination; cannot be abandoned")

// AbandonBatch retires a batch that can no longer be completed. Its reserved
// sequence numbers are never reused, so this is the one event that leaves a
// genuine gap in a destination's sequence — which is why it is recorded, not
// silent.
//
// It REFUSES to abandon a batch any destination has already published. Abandoning
// such a batch is not cleanup, it is a DUPLICATE-DELIVERY BUG: the files would be
// released back to the scanner, re-form as a new batch, and be re-sent to a
// destination that already holds them (billing double-bills). A stuck
// partially-delivered batch is an operator problem — the records are downstream and
// cannot be un-sent — so it stays OPEN, keeps its files pinned, and keeps retrying
// only the destinations that still owe it.
func (p *PG) AbandonBatch(ctx context.Context, btUID int64, reason string) error {
	// Build the delivery key in Go and pass it as a plain text parameter. Composing
	// it in SQL ('bt:' || $1::text) makes pgx infer TEXT for the parameter and then
	// fail to encode an int64 into it — the query errors before it ever runs, so the
	// guard never executes and NO batch can ever be abandoned.
	key := Batch{UID: btUID}.DeliveryKey()
	var delivered int
	if err := p.pool.QueryRow(ctx, `
		SELECT count(*) FROM DL_DELIVERY
		WHERE DL_OUTPUT_IDENTITY=$1 AND DL_STATUS='DELIVERED'`, key).Scan(&delivered); err != nil {
		return err
	}
	if delivered > 0 {
		return fmt.Errorf("%w (%d destination(s) already hold it)", ErrBatchPartiallyDelivered, delivered)
	}
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// The reserved-but-never-published sequence numbers of an abandoned batch are
	// burnt: they can't be handed to another batch (a concurrent reservation may
	// already sit above them). Mark them FAILED rather than leaving them PENDING, so
	// the resulting gap in the destination's sequence is EXPLAINED by a row an
	// operator (and a downstream gap check) can find, instead of looking like a
	// silently missing output (BR-DST-011).
	if _, err := tx.Exec(ctx, `
		UPDATE DL_DELIVERY SET DL_STATUS='FAILED', DL_LAST_ERROR=$2, DL_MODIFIED_ON=now()
		WHERE DL_OUTPUT_IDENTITY=$1 AND DL_STATUS <> 'DELIVERED'`, key, truncate(reason, 500)); err != nil {
		return fmt.Errorf("fail deliveries of abandoned batch: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE BT_BATCH SET BT_STATUS='ABANDONED', BT_LAST_ERROR=$2, BT_MODIFIED_ON=now() WHERE BT_UID=$1`,
		btUID, truncate(reason, 500)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ListDeliveries returns a pipeline's recent deliveries per destination, for the UI.
func (p *PG) ListDeliveries(ctx context.Context, pipelineID int64, limit int) ([]DeliveryView, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := p.pool.Query(ctx, `
		SELECT DS.DS_NAME, DL.DL_OUTPUT_NAME, COALESCE(DL.DL_SEQUENCE_NO,0), DL.DL_STATUS,
		       COALESCE(DL.DL_RECORD_COUNT,0), COALESCE(DL.DL_SIZE_BYTES,0), DL.DL_CREATED_ON,
		       COALESCE(DL.DL_LAST_ERROR,''), COALESCE(BT.BT_FILE_COUNT,0)
		FROM DL_DELIVERY DL
		JOIN DS_DESTINATION DS ON DS.DS_UID = DL.DL_DS_UID
		JOIN PLV_PIPELINE_VERSION PLV ON PLV.PLV_UID = DL.DL_PLV_UID
		LEFT JOIN BT_BATCH BT ON 'bt:' || BT.BT_UID::text = DL.DL_OUTPUT_IDENTITY
		WHERE PLV.PLV_PL_UID = $1
		ORDER BY DL.DL_UID DESC LIMIT $2`, pipelineID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeliveryView
	for rows.Next() {
		var v DeliveryView
		if err := rows.Scan(&v.Destination, &v.OutputName, &v.SequenceNo, &v.Status,
			&v.Records, &v.SizeBytes, &v.CreatedOn, &v.LastError, &v.FileCount); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeliveryView is one delivery as shown in the management UI.
type DeliveryView struct {
	Destination string
	OutputName  string
	SequenceNo  int64
	Status      string
	Records     int64
	SizeBytes   int64
	FileCount   int
	CreatedOn   time.Time
	LastError   string
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// SettledFileNames returns the names in this source that are already DONE or
// QUARANTINED — files the scanner must NOT sweep into a batch.
//
// The single-file path checks this per file (AlreadyProcessed / AlreadyQuarantined)
// before claiming. The batch path needs the same guard, in bulk: a file that is
// already DONE but still sitting in the input area — left in place by the
// "leaveMarked" disposition, re-dropped by an operator, or lingering because the
// process died between committing the record and moving the file — would otherwise
// be batched again and its records DELIVERED A SECOND TIME.
func (p *PG) SettledFileNames(ctx context.Context, srcUID int64, names []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(names) == 0 {
		return out, nil
	}
	rows, err := p.pool.Query(ctx, `
		SELECT PF_NAME FROM PF_PROCESSED_FILE
		WHERE PF_SRC_UID=$1 AND PF_NAME = ANY($2) AND PF_STATUS IN ('DONE','QUARANTINED')`,
		srcUID, names)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}
