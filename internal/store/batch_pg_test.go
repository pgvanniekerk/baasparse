package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/spec"
)

// These tests run against a REAL Postgres, because the in-memory store cannot
// catch the class of bug that actually shipped here: AbandonBatch's guard query
// composed its parameter in SQL ('bt:' || $1::text), which made pgx infer TEXT for
// an int64 and fail to encode it — so the query errored before it ran and NO batch
// could ever be abandoned. Every mem-store test passed, because Mem reimplements
// the guard in Go. Persistence logic has to be tested against the database.
//
// Set BAASPARSE_TEST_DSN to run; skipped otherwise.
func testPG(t *testing.T) *PG {
	t.Helper()
	dsn := os.Getenv("BAASPARSE_TEST_DSN")
	if dsn == "" {
		t.Skip("BAASPARSE_TEST_DSN not set")
	}
	pg, err := OpenPG(context.Background(), dsn, "baasparse")
	if err != nil {
		t.Fatalf("open pg: %v", err)
	}
	t.Cleanup(pg.Close)
	if err := pg.RegisterInstance(context.Background(), "test-"+t.Name(), "test", "test", ""); err != nil {
		t.Fatalf("register instance: %v", err)
	}
	return pg
}

// seedPipeline creates a throwaway pipeline and returns its id + srcUID.
func seedPipeline(t *testing.T, pg *PG, name string) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	fs := spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}}
	name = fmt.Sprintf("%s-%d", name, time.Now().UnixNano())
	id, err := pg.CreatePipeline(ctx, Pipeline{
		Name:    name,
		Enabled: false,
		Input:   fs,
		Output:  fs,
		Source:  Source{Backend: "posix", InputDir: name + "/input", DoneDir: name + "/done"},
		Outputs: []Output{{Name: "d1", Format: fs}},
	})
	if err != nil {
		t.Fatalf("create pipeline: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pg.pool.Exec(ctx, `DELETE FROM BF_BATCH_FILE WHERE BF_BT_UID IN (SELECT BT_UID FROM BT_BATCH WHERE BT_PL_UID=$1)`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM BT_BATCH WHERE BT_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM DL_DELIVERY WHERE DL_PLV_UID IN (SELECT PLV_UID FROM PLV_PIPELINE_VERSION WHERE PLV_PL_UID=$1)`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM SRC_SOURCE WHERE SRC_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM PLV_PIPELINE_VERSION WHERE PLV_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM PL_PIPELINE WHERE PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM DS_DESTINATION WHERE DS_NAME LIKE $1`, name+"%")
	})
	p, err := pg.GetPipeline(ctx, id)
	if err != nil {
		t.Fatalf("get pipeline: %v", err)
	}
	return id, p.SrcUID
}

// TestPG_AbandonBatch is the regression for the shipped critical bug: abandoning a
// batch that delivered NOWHERE must actually work, and must release its files.
func TestPG_AbandonBatch(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	id, srcUID := seedPipeline(t, pg, "t-abandon")

	b, err := pg.FormBatch(ctx, id, srcUID, []BatchFile{{Name: "a.csv", Size: 1}, {Name: "b.csv", Size: 2}})
	if err != nil {
		t.Fatalf("form batch: %v", err)
	}
	if err := pg.AbandonBatch(ctx, b.UID, "nothing delivered"); err != nil {
		t.Fatalf("an undelivered batch MUST be abandonable: %v", err)
	}
	var status string
	if err := pg.pool.QueryRow(ctx, `SELECT BT_STATUS FROM BT_BATCH WHERE BT_UID=$1`, b.UID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "ABANDONED" {
		t.Fatalf("status = %q, want ABANDONED", status)
	}
	pinned, err := pg.BatchedFileNames(ctx, srcUID)
	if err != nil {
		t.Fatal(err)
	}
	if len(pinned) != 0 {
		t.Fatalf("an abandoned batch must release its files; still pinned: %v", pinned)
	}
}

// TestPG_AbandonRefusedAfterDelivery: once a destination holds the batch, giving up
// would release the files, re-batch them, and re-send to that destination.
func TestPG_AbandonRefusedAfterDelivery(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	id, srcUID := seedPipeline(t, pg, "t-abandon-refuse")
	p, _ := pg.GetPipeline(ctx, id)
	o := p.Outputs[0]

	b, err := pg.FormBatch(ctx, id, srcUID, []BatchFile{{Name: "x.csv", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := pg.ReserveDelivery(ctx, id, o, b.Batch, func(seq int64) string { return "out-1" })
	if err != nil {
		t.Fatal(err)
	}
	d.Records = 10
	if err := pg.MarkDelivered(ctx, d); err != nil {
		t.Fatal(err)
	}
	err = pg.AbandonBatch(ctx, b.UID, "give up")
	if !errors.Is(err, ErrBatchPartiallyDelivered) {
		t.Fatalf("want ErrBatchPartiallyDelivered, got %v", err)
	}
}

// TestPG_NoOverlappingBatches pins the exactly-once break: two instances proposing
// DIFFERENT but overlapping file sets ({1,2,3} and {1,2,3,4}) hash to different
// identities, so the identity index does not stop them. Both would go OPEN over the
// same files and both would deliver — the shared records go out twice.
func TestPG_NoOverlappingBatches(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	id, srcUID := seedPipeline(t, pg, "t-overlap")

	if _, err := pg.FormBatch(ctx, id, srcUID, []BatchFile{
		{Name: "1.csv", Size: 1}, {Name: "2.csv", Size: 2}, {Name: "3.csv", Size: 3},
	}); err != nil {
		t.Fatalf("first batch: %v", err)
	}
	// A second instance saw one more file land.
	_, err := pg.FormBatch(ctx, id, srcUID, []BatchFile{
		{Name: "1.csv", Size: 1}, {Name: "2.csv", Size: 2}, {Name: "3.csv", Size: 3}, {Name: "4.csv", Size: 4},
	})
	if !errors.Is(err, ErrBatchOverlap) {
		t.Fatalf("an overlapping batch must be refused (its shared files would deliver twice); got %v", err)
	}
	var open int
	if err := pg.pool.QueryRow(ctx,
		`SELECT count(*) FROM BT_BATCH WHERE BT_SRC_UID=$1 AND BT_STATUS='OPEN'`, srcUID).Scan(&open); err != nil {
		t.Fatal(err)
	}
	if open != 1 {
		t.Fatalf("open batches = %d, want exactly 1", open)
	}
}

// TestPG_SequenceReservedOnceAcrossRetries: a retry must reuse its reserved number,
// never burn a new one, or a gap would mean "an attempt failed" rather than "an
// output is missing".
func TestPG_SequenceReservedOnceAcrossRetries(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	id, srcUID := seedPipeline(t, pg, "t-seq")
	p, _ := pg.GetPipeline(ctx, id)
	o := p.Outputs[0]

	b, err := pg.FormBatch(ctx, id, srcUID, []BatchFile{{Name: "s.csv", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	name := func(seq int64) string { return "o.csv" }
	d1, err := pg.ReserveDelivery(ctx, id, o, b.Batch, name)
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.MarkDeliveryFailed(ctx, d1, "destination down"); err != nil {
		t.Fatal(err)
	}
	d2, err := pg.ReserveDelivery(ctx, id, o, b.Batch, name)
	if err != nil {
		t.Fatal(err)
	}
	if d2.SequenceNo != d1.SequenceNo {
		t.Fatalf("retry burned a sequence number: %d then %d", d1.SequenceNo, d2.SequenceNo)
	}
}

// TestPG_PerDestinationTransformRoundTrips pins a bug the live run caught: the
// per-destination transform was added to the model but not to the persisted
// document, so on reload each destination silently fell back to the pipeline-level
// transform. Its COLUMNS still persisted — so it emitted exactly the right keys
// with null values, which looks like data corruption rather than a config bug.
func TestPG_PerDestinationTransformRoundTrips(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()

	in := spec.FormatSpec{Kind: spec.FormatDSV, DSV: &spec.DSVSpec{Delimiter: ",", HasHeader: true}}
	billing := spec.TransformSpec{Fields: []spec.FieldMap{
		{Output: "msisdn", Kind: spec.FieldFromInput, Source: "msisdn"},
		{Output: "duration", Kind: spec.FieldFromInput, Source: "duration"},
	}}
	ra := spec.TransformSpec{Fields: []spec.FieldMap{
		{Output: "msisdn", Kind: spec.FieldFromInput, Source: "msisdn"},
		{Output: "cell", Kind: spec.FieldFromInput, Source: "cell"},
		{Output: "feed", Kind: spec.FieldConst, Const: "RA"},
	}}
	name := fmt.Sprintf("t-shape-%d", time.Now().UnixNano())
	id, err := pg.CreatePipeline(ctx, Pipeline{
		Name: name, Input: in, Source: Source{Backend: "posix", InputDir: name + "/in"},
		Outputs: []Output{
			{Name: "billing", Transform: &billing, Format: spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}}},
			{Name: "revenue-assurance", Transform: &ra, Format: spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}}},
		},
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pg.pool.Exec(ctx, `DELETE FROM SRC_SOURCE WHERE SRC_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM PLV_PIPELINE_VERSION WHERE PLV_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM PL_PIPELINE WHERE PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM DS_DESTINATION WHERE DS_NAME LIKE $1`, name+"%")
	})

	got, err := pg.GetPipeline(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Outputs) != 2 {
		t.Fatalf("outputs = %d", len(got.Outputs))
	}
	b, r := got.Outputs[0], got.Outputs[1]
	if b.Transform == nil || len(b.Transform.Fields) != 2 {
		t.Fatalf("billing transform did not survive the round trip: %+v", b.Transform)
	}
	if r.Transform == nil || len(r.Transform.Fields) != 3 {
		t.Fatalf("revenue assurance transform did not survive the round trip: %+v", r.Transform)
	}
	if r.Transform.Fields[2].Kind != spec.FieldConst || r.Transform.Fields[2].Const != "RA" {
		t.Fatalf("derived field lost: %+v", r.Transform.Fields[2])
	}
	// The two destinations must still carry DIFFERENT shapes after a reload.
	if len(b.Transform.Fields) == len(r.Transform.Fields) {
		t.Fatal("destinations collapsed to the same shape on reload")
	}
}

// TestPG_UpdatePipeline_KeepsDestinationIdentity: editing must not restart a
// surviving destination's output sequence, and must not erase a removed
// destination's delivery history.
func TestPG_UpdatePipeline_KeepsDestinationIdentity(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	in := spec.FormatSpec{Kind: spec.FormatDSV, Fields: []spec.FieldSpec{{Name: "msisdn"}}, DSV: &spec.DSVSpec{Delimiter: ","}}
	pass := spec.TransformSpec{PassThrough: true}
	js := spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}}

	name := fmt.Sprintf("t-edit-%d", time.Now().UnixNano())
	id, err := pg.CreatePipeline(ctx, Pipeline{
		Name: name, Description: "before", Input: in,
		Source: Source{Backend: "posix", InputDir: name + "/in"},
		Outputs: []Output{
			{Name: "billing", Transform: &pass, Format: js},
			{Name: "revenue-assurance", Transform: &pass, Format: js},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pg.pool.Exec(ctx, `DELETE FROM DL_DELIVERY WHERE DL_PLV_UID IN (SELECT PLV_UID FROM PLV_PIPELINE_VERSION WHERE PLV_PL_UID=$1)`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM BF_BATCH_FILE WHERE BF_BT_UID IN (SELECT BT_UID FROM BT_BATCH WHERE BT_PL_UID=$1)`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM BT_BATCH WHERE BT_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM SRC_SOURCE WHERE SRC_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM PLV_PIPELINE_VERSION WHERE PLV_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM PL_PIPELINE WHERE PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM DSQ_DESTINATION_SEQUENCE WHERE DSQ_DS_UID IN (SELECT DS_UID FROM DS_DESTINATION WHERE DS_NAME LIKE $1)`, name+"%")
		_, _ = pg.pool.Exec(ctx, `DELETE FROM DS_DESTINATION WHERE DS_NAME LIKE $1`, name+"%")
	})

	before, _ := pg.GetPipeline(ctx, id)
	billingDS := before.Outputs[0].DSUID
	raDS := before.Outputs[1].DSUID

	// Revenue assurance has delivered — that history must survive its removal.
	b, err := pg.FormBatch(ctx, id, before.SrcUID, []BatchFile{{Name: "a.csv", Size: 1}})
	if err != nil {
		t.Fatal(err)
	}
	d, err := pg.ReserveDelivery(ctx, id, before.Outputs[1], b.Batch, func(seq int64) string { return "ra-1" })
	if err != nil {
		t.Fatal(err)
	}
	d.Records = 5
	if err := pg.MarkDelivered(ctx, d); err != nil {
		t.Fatal(err)
	}
	// Close it so the edit is not refused for being in flight.
	if _, err := pg.pool.Exec(ctx, `UPDATE BT_BATCH SET BT_STATUS='CLOSED' WHERE BT_UID=$1`, b.UID); err != nil {
		t.Fatal(err)
	}

	// Edit: drop revenue assurance, keep billing, add a warehouse, change description.
	edited := before
	edited.Description = "after"
	edited.Outputs = []Output{
		before.Outputs[0],
		{Name: "warehouse", Transform: &pass, Format: js},
	}
	if err := pg.UpdatePipeline(ctx, edited); err != nil {
		t.Fatalf("update: %v", err)
	}

	after, err := pg.GetPipeline(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.Description != "after" {
		t.Fatalf("description = %q", after.Description)
	}
	if len(after.Outputs) != 2 {
		t.Fatalf("outputs = %d", len(after.Outputs))
	}
	// A surviving destination keeps its DS row — and therefore its sequence.
	if after.Outputs[0].Name != "billing" || after.Outputs[0].DSUID != billingDS {
		t.Fatalf("billing lost its identity: %d -> %d (its output numbering would restart at 1)",
			billingDS, after.Outputs[0].DSUID)
	}
	// A new destination gets its own.
	if after.Outputs[1].Name != "warehouse" || after.Outputs[1].DSUID == 0 || after.Outputs[1].DSUID == raDS {
		t.Fatalf("warehouse should have a fresh DS row: %+v", after.Outputs[1])
	}
	// The removed destination's delivery history survives — it really did deliver.
	var n int
	if err := pg.pool.QueryRow(ctx,
		`SELECT count(*) FROM DL_DELIVERY WHERE DL_DS_UID=$1 AND DL_STATUS='DELIVERED'`, raDS).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("removing a destination erased its delivery audit trail (%d rows left)", n)
	}
}

// TestPG_UpdatePipeline_RefusedWhileBatchInFlight: an open batch has already reserved
// deliveries against the destinations as they were. Changing them underneath it can
// leave it pointing at a destination that no longer exists, which it can never
// complete — and if some destinations already published, it cannot be abandoned either.
func TestPG_UpdatePipeline_RefusedWhileBatchInFlight(t *testing.T) {
	pg := testPG(t)
	ctx := context.Background()
	in := spec.FormatSpec{Kind: spec.FormatDSV, Fields: []spec.FieldSpec{{Name: "a"}}, DSV: &spec.DSVSpec{Delimiter: ","}}
	pass := spec.TransformSpec{PassThrough: true}
	js := spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}}
	name := fmt.Sprintf("t-inflight-%d", time.Now().UnixNano())
	id, err := pg.CreatePipeline(ctx, Pipeline{
		Name: name, Input: in, Source: Source{Backend: "posix", InputDir: name + "/in"},
		Outputs: []Output{{Name: "billing", Transform: &pass, Format: js}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pg.pool.Exec(ctx, `DELETE FROM BF_BATCH_FILE WHERE BF_BT_UID IN (SELECT BT_UID FROM BT_BATCH WHERE BT_PL_UID=$1)`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM BT_BATCH WHERE BT_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM SRC_SOURCE WHERE SRC_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM PLV_PIPELINE_VERSION WHERE PLV_PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM PL_PIPELINE WHERE PL_UID=$1`, id)
		_, _ = pg.pool.Exec(ctx, `DELETE FROM DS_DESTINATION WHERE DS_NAME LIKE $1`, name+"%")
	})
	cur, _ := pg.GetPipeline(ctx, id)
	if _, err := pg.FormBatch(ctx, id, cur.SrcUID, []BatchFile{{Name: "x.csv", Size: 1}}); err != nil {
		t.Fatal(err)
	}

	// Structural change -> refused while the batch is open.
	structural := cur
	structural.Outputs = append([]Output{}, cur.Outputs...)
	structural.Outputs[0].Name = "billing-renamed"
	if err := pg.UpdatePipeline(ctx, structural); err == nil {
		t.Fatal("changing destinations while a batch is in flight must be refused")
	}

	// A description-only edit is always allowed — it does not touch the data plane.
	safe := cur
	safe.Description = "just a note"
	if err := pg.UpdatePipeline(ctx, safe); err != nil {
		t.Fatalf("a description-only edit must be allowed mid-flight: %v", err)
	}
}
