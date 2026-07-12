package store

import (
	"context"
	"fmt"
	"sort"
)

// In-memory batch/delivery state, mirroring the Postgres semantics closely enough
// to test the invariants that matter: a batch is frozen by identity, a delivery
// reserves its sequence once and reuses it on retry, and the files commit only
// when every destination has delivered.

type memBatch struct {
	uid      int64
	pipeline int64
	srcUID   int64
	identity string
	status   string
	files    []BatchFile
	attempts int
}

type memDelivery struct {
	uid        int64
	dsUID      int64
	identity   string
	seq        int64
	outputName string
	status     string
	records    int64
	size       int64
	lastError  string
	fileCount  int
	destName   string
}

func (m *Mem) FormBatch(_ context.Context, pipelineID, srcUID int64, files []BatchFile) (OpenBatch, error) {
	if len(files) == 0 {
		return OpenBatch{}, fmt.Errorf("cannot form an empty batch")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	sorted := make([]BatchFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	identity := BatchIdentity(sorted)
	for _, b := range m.batches {
		if b.srcUID == srcUID && b.identity == identity && b.status == "OPEN" {
			return m.openBatchLocked(b), nil
		}
	}
	// Never form a batch overlapping an open one: the shared files' records would be
	// delivered twice (see PG.FormBatch).
	for _, b := range m.batches {
		if b.srcUID != srcUID || b.status != "OPEN" {
			continue
		}
		for _, ex := range b.files {
			for _, f := range sorted {
				if ex.Name == f.Name {
					return OpenBatch{}, fmt.Errorf("%w: %q is already in an open batch", ErrBatchOverlap, f.Name)
				}
			}
		}
	}
	m.seq++
	b := &memBatch{uid: m.seq, pipeline: pipelineID, srcUID: srcUID, identity: identity, status: "OPEN", files: sorted}
	if m.batches == nil {
		m.batches = map[int64]*memBatch{}
	}
	m.batches[b.uid] = b
	return m.openBatchLocked(b), nil
}

func (m *Mem) openBatchLocked(b *memBatch) OpenBatch {
	ob := OpenBatch{
		Batch:     Batch{UID: b.uid, Identity: b.identity, Files: append([]BatchFile(nil), b.files...), Attempts: b.attempts},
		Delivered: map[int64]bool{},
	}
	for _, d := range m.deliveries {
		if d.identity == ob.Batch.DeliveryKey() && d.status == "DELIVERED" {
			ob.Delivered[d.dsUID] = true
		}
	}
	return ob
}

func (m *Mem) ListOpenBatches(_ context.Context, srcUID int64) ([]OpenBatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []OpenBatch
	uids := make([]int64, 0, len(m.batches))
	for uid, b := range m.batches {
		if b.srcUID == srcUID && b.status == "OPEN" {
			uids = append(uids, uid)
		}
	}
	sort.Slice(uids, func(i, j int) bool { return uids[i] < uids[j] })
	for _, uid := range uids {
		out = append(out, m.openBatchLocked(m.batches[uid]))
	}
	return out, nil
}

func (m *Mem) BatchedFileNames(_ context.Context, srcUID int64) (map[string]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]bool{}
	for _, b := range m.batches {
		if b.srcUID == srcUID && b.status == "OPEN" {
			for _, f := range b.files {
				out[f.Name] = true
			}
		}
	}
	return out, nil
}

func (m *Mem) ReserveDelivery(_ context.Context, _ int64, o Output, b Batch, nameFor func(int64) string) (Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.deliveries {
		if d.dsUID == o.DSUID && d.identity == b.DeliveryKey() {
			// Reuse the existing reservation: same sequence, same output name — a
			// retry must not burn a new number.
			return Delivery{UID: d.uid, DSUID: d.dsUID, OutputName: d.outputName, SequenceNo: d.seq, Status: d.status}, nil
		}
	}
	if m.dsSeq == nil {
		m.dsSeq = map[int64]int64{}
	}
	m.dsSeq[o.DSUID]++
	seq := m.dsSeq[o.DSUID]
	m.seq++
	d := &memDelivery{
		uid: m.seq, dsUID: o.DSUID, identity: b.DeliveryKey(), seq: seq,
		outputName: nameFor(seq), status: "PENDING", destName: o.Name, fileCount: len(b.Files),
	}
	m.deliveries = append(m.deliveries, d)
	return Delivery{UID: d.uid, DSUID: d.dsUID, OutputName: d.outputName, SequenceNo: d.seq, Status: d.status}, nil
}

func (m *Mem) MarkDelivered(_ context.Context, d Delivery) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, md := range m.deliveries {
		if md.uid == d.UID {
			md.status = "DELIVERED"
			md.records = d.Records
			md.size = d.SizeBytes
			return nil
		}
	}
	return fmt.Errorf("delivery %d not found", d.UID)
}

func (m *Mem) MarkDeliveryFailed(_ context.Context, d Delivery, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, md := range m.deliveries {
		if md.uid == d.UID && md.status != "DELIVERED" {
			md.status = "PENDING"
			md.lastError = reason
		}
	}
	return nil
}

func (m *Mem) CloseBatch(ctx context.Context, b Batch, pfs []ProcessedFile, claims []Claim, _ []Delivery, _ map[int64][]Contribution) (bool, error) {
	for _, pf := range pfs {
		if err := m.RecordProcessedFile(ctx, pf); err != nil {
			return false, err
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if bb, ok := m.batches[b.UID]; ok {
		bb.status = "CLOSED"
	}
	return true, nil
}

func (m *Mem) TouchBatchAttempt(_ context.Context, btUID int64, reason string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.batches[btUID]
	if !ok {
		return 0, fmt.Errorf("batch %d not found", btUID)
	}
	b.attempts++
	return b.attempts, nil
}

func (m *Mem) DropBatchFile(_ context.Context, b Batch, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, d := range m.deliveries {
		if d.identity == b.DeliveryKey() && d.status == "DELIVERED" {
			return fmt.Errorf("cannot drop %q: batch already delivered", name)
		}
	}
	bb, ok := m.batches[b.UID]
	if !ok {
		return fmt.Errorf("batch %d not found", b.UID)
	}
	out := bb.files[:0]
	for _, f := range bb.files {
		if f.Name != name {
			out = append(out, f)
		}
	}
	bb.files = out
	return nil
}

func (m *Mem) AbandonBatch(_ context.Context, btUID int64, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.batches[btUID]
	if !ok {
		return nil
	}
	key := Batch{UID: btUID}.DeliveryKey()
	for _, d := range m.deliveries {
		if d.identity == key && d.status == "DELIVERED" {
			return fmt.Errorf("%w (%s)", ErrBatchPartiallyDelivered, d.destName)
		}
	}
	b.status = "ABANDONED"
	return nil
}

func (m *Mem) ListDeliveries(_ context.Context, _ int64, limit int) ([]DeliveryView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []DeliveryView
	for i := len(m.deliveries) - 1; i >= 0 && len(out) < limit; i-- {
		d := m.deliveries[i]
		out = append(out, DeliveryView{
			Destination: d.destName, OutputName: d.outputName, SequenceNo: d.seq,
			Status: d.status, Records: d.records, SizeBytes: d.size, FileCount: d.fileCount, LastError: d.lastError,
		})
	}
	return out, nil
}

// RecordBatchContributions persists each file's record counts once the batch is
// encoded, so a resume after a crash can still record truthful per-file counts.
func (m *Mem) RecordBatchContributions(_ context.Context, btUID int64, cs []Contribution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.batches[btUID]
	if !ok {
		return fmt.Errorf("batch %d not found", btUID)
	}
	for _, c := range cs {
		for i := range b.files {
			if b.files[i].Name == c.FileName {
				b.files[i].RecordsIn = c.RecordsIn
				b.files[i].RecordsOut = c.Records
				b.files[i].Suspended = c.Suspended
			}
		}
	}
	return nil
}

// GetOpenBatchForTest re-reads a batch by identity, including which destinations
// have already delivered it — the state a retry depends on.
func (m *Mem) GetOpenBatchForTest(identity string) (OpenBatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, b := range m.batches {
		if b.identity == identity {
			return m.openBatchLocked(b), nil
		}
	}
	return OpenBatch{}, fmt.Errorf("batch %s not found", identity)
}

// SettledFileNames reports which of these names are already DONE or QUARANTINED.
func (m *Mem) SettledFileNames(_ context.Context, _ int64, names []string) (map[string]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	want := map[string]bool{}
	for _, n := range names {
		want[n] = true
	}
	out := map[string]bool{}
	for _, f := range m.files {
		if want[f.Name] && (f.Status == "DONE" || f.Status == "QUARANTINED") {
			out[f.Name] = true
		}
	}
	return out, nil
}
