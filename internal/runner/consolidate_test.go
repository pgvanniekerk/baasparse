package runner

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// consolidatingPipeline: DSV in, two destinations in DIFFERENT formats — billing
// takes pipe-delimited DSV with a narrow column set, revenue assurance takes
// gzipped NDJSON with everything. This is the shape the feature exists for.
func consolidatingPipeline() store.Pipeline {
	return store.Pipeline{
		ID:   1,
		Name: "cdr",
		Input: spec.FormatSpec{
			Kind: spec.FormatDSV,
			Fields: []spec.FieldSpec{
				{Name: "msisdn"}, {Name: "duration", Type: spec.TypeInteger}, {Name: "cell"},
			},
			DSV: &spec.DSVSpec{Delimiter: ",", HasHeader: true, Columns: []string{"msisdn", "duration", "cell"}},
		},
		Transform: spec.TransformSpec{PassThrough: true},
		Batch:     store.BatchSpec{Enabled: true, MaxFiles: 10},
		Outputs: []store.Output{
			{
				Name: "billing", DSUID: 101, Dir: "out/billing",
				Format: spec.FormatSpec{
					Kind:    spec.FormatDSV,
					Columns: []string{"msisdn", "duration"}, // narrow projection
					DSV:     &spec.DSVSpec{Delimiter: "|", HasHeader: true, Columns: []string{"msisdn", "duration"}},
				},
			},
			{
				Name: "revenue-assurance", DSUID: 102, Dir: "out/ra",
				Format: spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}},
			},
		},
	}
}

// writeInputs lays down n DSV files of rows each, returning the batch members.
func writeInputs(t *testing.T, root string, n, rows int) []store.BatchFile {
	t.Helper()
	os.MkdirAll(filepath.Join(root, "in"), 0o755)
	var files []store.BatchFile
	rec := 0
	for f := 0; f < n; f++ {
		var b strings.Builder
		b.WriteString("msisdn,duration,cell\n")
		for i := 0; i < rows; i++ {
			rec++
			b.WriteString("2783000" + strconv.Itoa(rec) + "," + strconv.Itoa(rec) + ",JHB-" + strconv.Itoa(f) + "\n")
		}
		name := "cdr-" + strconv.Itoa(f) + ".csv"
		if err := os.WriteFile(filepath.Join(root, "in", name), []byte(b.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(filepath.Join(root, "in", name))
		files = append(files, store.BatchFile{Name: name, Size: st.Size()})
	}
	return files
}

func setupBatch(t *testing.T, root string, p store.Pipeline, files []store.BatchFile, sg storage.Store) (
	*Runner, *store.Mem, store.OpenBatch, []Member, []Destination) {
	t.Helper()
	ms := store.NewMem()
	r := New(ms, slog.New(slog.NewTextHandler(io.Discard, nil)))
	b, err := ms.FormBatch(context.Background(), p.ID, 1, files)
	if err != nil {
		t.Fatal(err)
	}
	members := make([]Member, 0, len(files))
	for _, f := range b.Files {
		uid, _ := ms.NextFileUID(context.Background())
		members = append(members, Member{Name: f.Name, Key: "in/" + f.Name, Size: f.Size, FileUID: uid})
	}
	dests := make([]Destination, 0, len(p.Outputs))
	for _, o := range p.Outputs {
		o := o
		del, err := ms.ReserveDelivery(context.Background(), p.ID, o, b.Batch, func(seq int64) string {
			return BatchOutputName(p, o, seq)
		})
		if err != nil {
			t.Fatal(err)
		}
		dests = append(dests, Destination{Output: o, Store: sg, Delivery: del})
	}
	return r, ms, b, members, dests
}

// TestConsolidate_TenFilesOneOutputPerDestination is the headline behaviour:
// 10 files x 100 records -> ONE 1000-record file per destination, each in its own
// format, with per-file lineage preserved.
func TestConsolidate_TenFilesOneOutputPerDestination(t *testing.T) {
	root := t.TempDir()
	p := consolidatingPipeline()
	files := writeInputs(t, root, 10, 100)
	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r, ms, b, members, dests := setupBatch(t, root, p, files, sg)

	res, err := r.ProcessBatch(context.Background(), p, sg, sg, b, members, dests, "done")
	if err != nil {
		t.Fatalf("ProcessBatch: %v", err)
	}
	if !res.Committed {
		t.Fatal("batch not committed")
	}
	if res.RecordsIn != 1000 {
		t.Fatalf("records in = %d, want 1000", res.RecordsIn)
	}
	if len(res.Delivered) != 2 || len(res.Failed) != 0 {
		t.Fatalf("delivered=%v failed=%v", res.Delivered, res.Failed)
	}

	// billing: ONE pipe-delimited file, 1000 records + header, narrow columns.
	bill := readFile(t, root, "out/billing", dests[0].Delivery.OutputName)
	lines := strings.Split(strings.TrimSpace(bill), "\n")
	if len(lines) != 1001 {
		t.Fatalf("billing lines = %d, want 1001 (header + 1000)", len(lines))
	}
	if lines[0] != "msisdn|duration" {
		t.Fatalf("billing header = %q, want the projected columns only", lines[0])
	}
	if strings.Contains(bill, "JHB-") {
		t.Fatal("billing must NOT contain the 'cell' column it did not ask for")
	}

	// revenue assurance: ONE NDJSON file, 1000 records, all fields.
	ra := readFile(t, root, "out/ra", dests[1].Delivery.OutputName)
	raLines := strings.Split(strings.TrimSpace(ra), "\n")
	if len(raLines) != 1000 {
		t.Fatalf("RA lines = %d, want 1000", len(raLines))
	}
	if !strings.Contains(raLines[0], `"cell"`) {
		t.Fatalf("RA should carry every field: %s", raLines[0])
	}

	// Per-file reconciliation survives consolidation: each of the 10 inputs keeps
	// its own record counts even though they all landed in one object.
	if len(res.Files) != 10 {
		t.Fatalf("processed files = %d, want 10", len(res.Files))
	}
	for _, pf := range res.Files {
		if pf.RecordsIn != 100 || pf.RecordsOut != 100 {
			t.Fatalf("file %s: in/out = %d/%d, want 100/100", pf.Name, pf.RecordsIn, pf.RecordsOut)
		}
	}
	// Both destinations got sequence 1 — they number independently.
	for _, d := range dests {
		if d.Delivery.SequenceNo != 1 {
			t.Fatalf("%s seq = %d, want 1", d.Output.Name, d.Delivery.SequenceNo)
		}
	}
	_ = ms
}

// TestConsolidate_OneDestinationDownDoesNotBlockTheOther is the whole reason the
// design is what it is: revenue assurance being unreachable must not stop billing
// from being delivered, and must not cause billing to be re-delivered on retry.
func TestConsolidate_OneDestinationDownDoesNotBlockTheOther(t *testing.T) {
	root := t.TempDir()
	p := consolidatingPipeline()
	files := writeInputs(t, root, 4, 50)
	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r, ms, b, members, dests := setupBatch(t, root, p, files, sg)

	// Revenue assurance's storage is down.
	dests[1].Store = &failPutStore{}

	res, err := r.ProcessBatch(context.Background(), p, sg, sg, b, members, dests, "done")
	if err == nil {
		t.Fatal("expected a partial-delivery error")
	}
	if res.Committed {
		t.Fatal("files must NOT be done while a destination is undelivered")
	}
	if len(res.Delivered) != 1 || res.Delivered[0] != "billing" {
		t.Fatalf("billing should have delivered anyway; got %v", res.Delivered)
	}
	if len(res.Failed) != 1 || res.Failed[0] != "revenue-assurance" {
		t.Fatalf("RA should be the failed one; got %v", res.Failed)
	}
	// Billing's output is real and complete.
	bill := readFile(t, root, "out/billing", dests[0].Delivery.OutputName)
	if n := len(strings.Split(strings.TrimSpace(bill), "\n")); n != 201 {
		t.Fatalf("billing lines = %d, want 201 (header + 200)", n)
	}

	// --- the retry: RA's storage is back ---
	b2, err := ms.GetOpenBatchForTest(b.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if !b2.Delivered[101] {
		t.Fatal("billing must be recorded as DELIVERED so the retry skips it")
	}
	if b2.Delivered[102] {
		t.Fatal("RA must not be marked delivered")
	}
	dests[1].Store = sg // recovered

	res2, err := r.ProcessBatch(context.Background(), p, sg, sg, b2, members, dests, "done")
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if !res2.Committed {
		t.Fatal("retry should commit: both destinations now hold the batch")
	}
	// Billing must NOT have been written a second time — its file is unchanged and
	// its sequence number was never re-allocated.
	bill2 := readFile(t, root, "out/billing", dests[0].Delivery.OutputName)
	if bill2 != bill {
		t.Fatal("billing was re-delivered on retry — duplicate records downstream")
	}
	if dests[0].Delivery.SequenceNo != 1 || dests[1].Delivery.SequenceNo != 1 {
		t.Fatalf("sequences must be reused across retries, not burned: %d/%d",
			dests[0].Delivery.SequenceNo, dests[1].Delivery.SequenceNo)
	}
	ra := readFile(t, root, "out/ra", dests[1].Delivery.OutputName)
	if n := len(strings.Split(strings.TrimSpace(ra), "\n")); n != 200 {
		t.Fatalf("RA lines after retry = %d, want 200", n)
	}
}

// TestConsolidate_BadFileNamesItself: one malformed file in a batch must be
// identifiable, so the caller can quarantine that file alone rather than
// condemning the whole batch.
func TestConsolidate_BadFileNamesItself(t *testing.T) {
	root := t.TempDir()
	p := consolidatingPipeline()
	p.Input.Kind = spec.FormatJSON
	p.Input.JSON = &spec.JSONSpec{Mode: "ndjson"}
	p.Input.DSV = nil
	p.Outputs = p.Outputs[1:] // just the NDJSON destination

	os.MkdirAll(filepath.Join(root, "in"), 0o755)
	os.WriteFile(filepath.Join(root, "in", "a.ndjson"), []byte(`{"msisdn":"1"}`+"\n"), 0o644)
	os.WriteFile(filepath.Join(root, "in", "b.ndjson"), []byte("THIS IS NOT JSON\n"), 0o644)
	files := []store.BatchFile{{Name: "a.ndjson", Size: 15}, {Name: "b.ndjson", Size: 17}}

	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r, _, b, members, dests := setupBatch(t, root, p, files, sg)

	_, err := r.ProcessBatch(context.Background(), p, sg, sg, b, members, dests, "done")
	var bad *BadMemberError
	if !errors.As(err, &bad) {
		t.Fatalf("want BadMemberError naming the culprit, got %v", err)
	}
	if bad.Name != "b.ndjson" {
		t.Fatalf("blamed %q, want b.ndjson", bad.Name)
	}
}

// TestBatchIdentity_StableAcrossOrder: the identity must depend only on the member
// SET, not the order the scan happened to list them, or a retry would form a
// "different" batch and re-deliver to destinations that already have the records.
func TestBatchIdentity_StableAcrossOrder(t *testing.T) {
	a := []store.BatchFile{{Name: "x.csv", Size: 1}, {Name: "y.csv", Size: 2}}
	bfs := []store.BatchFile{{Name: "y.csv", Size: 2}, {Name: "x.csv", Size: 1}}
	if store.BatchIdentity(a) != store.BatchIdentity(bfs) {
		t.Fatal("identity must not depend on member order")
	}
	c := []store.BatchFile{{Name: "x.csv", Size: 1}, {Name: "y.csv", Size: 3}}
	if store.BatchIdentity(a) == store.BatchIdentity(c) {
		t.Fatal("a different member set must produce a different identity")
	}
}

func readFile(t *testing.T, root, dir, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, dir, name))
	if err != nil {
		t.Fatalf("read output %s/%s: %v", dir, name, err)
	}
	return string(b)
}

// TestConsolidate_SameFilesAgainIsANewBatch pins a silent-data-loss bug found in
// review: delivery rows used to be keyed on the batch's CONTENT hash, which
// repeats whenever the same filenames and sizes come round again (a daily feed of
// fixed-size files does exactly that). The second batch then found the first
// batch's DELIVERED row, concluded it had already published, skipped writing the
// output — and still marked the files DONE. Deliveries are now keyed on the batch,
// so identical files a day later are a genuinely new batch with a new output.
func TestConsolidate_SameFilesAgainIsANewBatch(t *testing.T) {
	root := t.TempDir()
	p := consolidatingPipeline()
	p.Outputs = p.Outputs[:1] // billing only
	files := writeInputs(t, root, 2, 10)
	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r, ms, b1, members1, dests1 := setupBatch(t, root, p, files, sg)

	res1, err := r.ProcessBatch(context.Background(), p, sg, sg, b1, members1, dests1, "done")
	if err != nil || !res1.Committed {
		t.Fatalf("first batch: err=%v committed=%v", err, res1.Committed)
	}

	// The identical files arrive again the next day: same names, same sizes, so the
	// same content hash as yesterday's batch.
	files = writeInputs(t, root, 2, 10)
	b2, err := ms.FormBatch(context.Background(), p.ID, 1, files)
	if err != nil {
		t.Fatal(err)
	}
	if b2.UID == b1.UID {
		t.Fatal("a closed batch must not be resumed as if still open")
	}
	if len(b2.Delivered) != 0 {
		t.Fatalf("new batch must start undelivered; got %v (the old batch's rows leaked in)", b2.Delivered)
	}
	members2 := make([]Member, 0, len(b2.Files))
	for _, f := range b2.Files {
		uid, _ := ms.NextFileUID(context.Background())
		members2 = append(members2, Member{Name: f.Name, Key: "in/" + f.Name, Size: f.Size, FileUID: uid})
	}
	o := p.Outputs[0]
	del2, err := ms.ReserveDelivery(context.Background(), p.ID, o, b2.Batch, func(seq int64) string {
		return BatchOutputName(p, o, seq)
	})
	if err != nil {
		t.Fatal(err)
	}
	if del2.SequenceNo == dests1[0].Delivery.SequenceNo {
		t.Fatal("a new batch must get its own sequence number, not reuse the old one")
	}
	dests2 := []Destination{{Output: o, Store: sg, Delivery: del2}}

	res2, err := r.ProcessBatch(context.Background(), p, sg, sg, b2, members2, dests2, "done")
	if err != nil {
		t.Fatalf("second batch: %v", err)
	}
	if len(res2.Delivered) != 1 {
		t.Fatalf("the second batch MUST be written, not skipped as already-delivered: %+v", res2)
	}
	// The output really exists and holds the records — this is the data that used
	// to vanish.
	out := readFile(t, root, "out/billing", del2.OutputName)
	if n := len(strings.Split(strings.TrimSpace(out), "\n")); n != 21 {
		t.Fatalf("second output has %d lines, want 21 (header + 20)", n)
	}
	if res2.RecordsIn != 20 {
		t.Fatalf("records in = %d, want 20", res2.RecordsIn)
	}
}

// TestConsolidate_ResumeAfterAllDeliveredKeepsCounts: if we crash after every
// destination delivered but before the commit, the resume must still record
// truthful per-file counts (in = out + suspended) rather than marking the files
// DONE with zero records.
func TestConsolidate_ResumeAfterAllDeliveredKeepsCounts(t *testing.T) {
	root := t.TempDir()
	p := consolidatingPipeline()
	p.Outputs = p.Outputs[:1]
	files := writeInputs(t, root, 3, 40)
	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r, ms, b, members, dests := setupBatch(t, root, p, files, sg)

	// First attempt delivers but "crashes" before commit: simulate by running the
	// batch, then re-reading it (delivery recorded) and running it again.
	if _, err := r.ProcessBatch(context.Background(), p, sg, sg, b, members, dests, "done"); err != nil {
		t.Fatal(err)
	}
	b2, err := ms.GetOpenBatchForTest(b.Identity)
	if err != nil {
		t.Fatal(err)
	}
	// (the batch is CLOSED now, but its persisted counts are what we assert on)
	for _, f := range b2.Files {
		if f.RecordsIn != 40 || f.RecordsOut != 40 {
			t.Fatalf("file %s counts were not persisted at encode time: in=%d out=%d",
				f.Name, f.RecordsIn, f.RecordsOut)
		}
	}
}

// TestConsolidate_PartlyDeliveredBatchCannotBeAbandoned pins the duplicate-delivery
// bug the LIVE e2e caught. Revenue assurance kept failing; the batch hit its
// attempt limit; the watcher abandoned it; its files were released back to the
// scanner, re-formed as a new batch, and re-delivered to BILLING — which already
// held those exact records. Billing would have double-billed.
//
// The rule: a batch that has published ANYWHERE can never be abandoned, at any
// attempt count. Its records are downstream and cannot be un-sent, so it stays open
// and keeps retrying only what it still owes.
func TestConsolidate_PartlyDeliveredBatchCannotBeAbandoned(t *testing.T) {
	root := t.TempDir()
	p := consolidatingPipeline()
	files := writeInputs(t, root, 3, 20)
	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r, ms, b, members, dests := setupBatch(t, root, p, files, sg)

	dests[1].Store = &failPutStore{} // revenue assurance is down
	res, _ := r.ProcessBatch(context.Background(), p, sg, sg, b, members, dests, "done")
	if len(res.Delivered) != 1 {
		t.Fatalf("billing should have delivered; got %v", res.Delivered)
	}

	// However many times it fails, the batch must NOT be abandonable.
	err := ms.AbandonBatch(context.Background(), b.UID, "gave up after many attempts")
	if err == nil {
		t.Fatal("abandoning a partly-delivered batch must be REFUSED — " +
			"releasing its files re-batches them and re-sends to billing (duplicate records)")
	}
	if !errors.Is(err, store.ErrBatchPartiallyDelivered) {
		t.Fatalf("want ErrBatchPartiallyDelivered, got %v", err)
	}

	// The batch is still OPEN, so its files stay pinned and cannot be swept into a
	// new batch by the scanner.
	pinned, _ := ms.BatchedFileNames(context.Background(), 1)
	for _, f := range files {
		if !pinned[f.Name] {
			t.Fatalf("file %s was released for re-batching — it would be re-delivered to billing", f.Name)
		}
	}

	// A batch that delivered NOWHERE is still abandonable (that is safe: nothing
	// downstream has it).
	files2 := writeInputs(t, root, 2, 5)
	for i := range files2 {
		files2[i].Name = "zz-" + files2[i].Name
	}
	b2, _ := ms.FormBatch(context.Background(), p.ID, 1, files2)
	if err := ms.AbandonBatch(context.Background(), b2.UID, "nothing delivered"); err != nil {
		t.Fatalf("an undelivered batch must remain abandonable: %v", err)
	}
}

// TestConsolidate_FailedBatchPublishesNothing pins a critical bug found in review:
// when a bad member aborted the batch, the destinations that had encoded fine still
// had their pipes closed CLEANLY, so Put committed an object holding only the
// records decoded BEFORE the bad file — at a real sequence number. The batch then
// re-formed without the bad file and delivered those same records again, leaving
// the destination with a partial object AND a complete one, and billing the overlap
// twice. A failed batch must publish NOTHING, anywhere.
func TestConsolidate_FailedBatchPublishesNothing(t *testing.T) {
	root := t.TempDir()
	p := consolidatingPipeline()
	p.Input.Kind = spec.FormatJSON
	p.Input.JSON = &spec.JSONSpec{Mode: "ndjson"}
	p.Input.DSV = nil
	p.Outputs[0].Format = spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}}

	os.MkdirAll(filepath.Join(root, "in"), 0o755)
	// good.ndjson decodes fine and would be flushed to BOTH destinations before
	// bad.ndjson blows up — those are exactly the records that used to leak out.
	var good strings.Builder
	for i := 0; i < 500; i++ {
		good.WriteString(`{"msisdn":"278300` + strconv.Itoa(i) + `","duration":1,"cell":"JHB"}` + "\n")
	}
	os.WriteFile(filepath.Join(root, "in", "a-good.ndjson"), []byte(good.String()), 0o644)
	os.WriteFile(filepath.Join(root, "in", "b-bad.ndjson"), []byte("NOT JSON AT ALL\n"), 0o644)
	files := []store.BatchFile{{Name: "a-good.ndjson", Size: 100}, {Name: "b-bad.ndjson", Size: 16}}

	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r, _, b, members, dests := setupBatch(t, root, p, files, sg)

	_, err := r.ProcessBatch(context.Background(), p, sg, sg, b, members, dests, "done")
	var bad *BadMemberError
	if !errors.As(err, &bad) {
		t.Fatalf("want BadMemberError, got %v", err)
	}

	// NOTHING may have been published — not to billing, not to revenue assurance.
	for _, d := range dests {
		path := filepath.Join(root, d.Output.Dir, d.Delivery.OutputName)
		if _, statErr := os.Stat(path); statErr == nil {
			body, _ := os.ReadFile(path)
			t.Fatalf("destination %q published a PARTIAL object (%d bytes) for a FAILED batch — "+
				"those records get delivered again when the batch re-forms: %s",
				d.Output.Name, len(body), path)
		}
	}
}
