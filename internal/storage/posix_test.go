package storage

import (
	"bytes"
	"context"
	"io"
	"testing"
)

func TestPosixRoundTrip(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st, err := New(ctx, Config{Backend: BackendPOSIX, Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if st.Backend() != BackendPOSIX {
		t.Fatalf("backend=%s", st.Backend())
	}

	// Put two files under an "input" prefix.
	if err := st.Put(ctx, "input/a.csv", bytes.NewBufferString("hello"), Meta{}); err != nil {
		t.Fatal(err)
	}
	if err := st.Put(ctx, "input/b.csv", bytes.NewBufferString("world"), Meta{}); err != nil {
		t.Fatal(err)
	}

	// List the prefix.
	ents, err := st.List(ctx, "input")
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != 2 {
		t.Fatalf("want 2 entries, got %d (%v)", len(ents), ents)
	}
	if ents[0].Key != "input/a.csv" && ents[0].Key != "input/b.csv" {
		t.Fatalf("unexpected key %q", ents[0].Key)
	}

	// Open + read.
	rc, err := st.Open(ctx, "input/a.csv")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	rc.Close()
	if string(b) != "hello" {
		t.Fatalf("read %q", b)
	}

	// Stat.
	e, err := st.Stat(ctx, "input/a.csv")
	if err != nil || e.Size != 5 {
		t.Fatalf("stat: %v size=%d", err, e.Size)
	}

	// Move to done.
	if err := st.Move(ctx, "input/a.csv", "done/a.csv"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Stat(ctx, "input/a.csv"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after move, got %v", err)
	}
	if _, err := st.Stat(ctx, "done/a.csv"); err != nil {
		t.Fatalf("moved file missing: %v", err)
	}

	// Delete is idempotent.
	if err := st.Delete(ctx, "done/a.csv"); err != nil {
		t.Fatal(err)
	}
	if err := st.Delete(ctx, "done/a.csv"); err != nil {
		t.Fatalf("delete of absent key should be nil, got %v", err)
	}

	// Missing key → ErrNotFound.
	if _, err := st.Open(ctx, "nope.csv"); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
