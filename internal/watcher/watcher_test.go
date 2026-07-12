package watcher

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/runner"
	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// TestWorkerPoolProcessesAllFilesExactlyOnce drops N files into a posix input
// dir and runs the watcher with a concurrent worker pool, asserting every file
// is processed exactly once (one DONE record each, one output each) and the
// in-flight dedup prevents duplicate same-instance processing across scan ticks.
func TestWorkerPoolProcessesAllFilesExactlyOnce(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"input", "done", "output", "quar"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	const nFiles = 12
	for i := 0; i < nFiles; i++ {
		body := "a,b\n"
		for r := 0; r < 200; r++ {
			body += fmt.Sprintf("%d,x%d\n", r, r)
		}
		if err := os.WriteFile(filepath.Join(root, "input", fmt.Sprintf("f%02d.csv", i)), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	mem := store.NewMem()
	p := store.Pipeline{
		Name: "t", Enabled: true, SrcUID: 1,
		Input:     spec.FormatSpec{Kind: spec.FormatDSV, DSV: &spec.DSVSpec{Delimiter: ",", HasHeader: true, Columns: []string{"a", "b"}}},
		Transform: spec.TransformSpec{PassThrough: true},
		Output:    spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}},
		Source: store.Source{
			Backend: "posix", Root: root,
			InputDir: "input", DoneDir: "done", OutputDir: "output", QuarantineDir: "quar",
		},
	}
	if _, err := mem.CreatePipeline(context.Background(), p); err != nil {
		t.Fatal(err)
	}

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	w := New(mem, sg, runner.New(mem, log), log, "output", "done", 30*time.Millisecond, 4)

	scanCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { w.Run(scanCtx, context.Background()); close(done) }()

	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		files, _ := mem.ListProcessedFiles(context.Background(), 100)
		if len(files) >= nFiles {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done // Run returns only after the pool drains (wg.Wait)

	files, _ := mem.ListProcessedFiles(context.Background(), 100)
	byName := map[string]int{}
	for _, f := range files {
		if f.Status == "DONE" {
			byName[f.Name]++
		}
	}
	if len(byName) != nFiles {
		t.Fatalf("processed %d distinct files, want %d (records: %+v)", len(byName), nFiles, byName)
	}
	for n, c := range byName {
		if c != 1 {
			t.Errorf("file %s recorded %d times, want exactly 1", n, c)
		}
	}
	outs, _ := os.ReadDir(filepath.Join(root, "output"))
	nOut := 0
	for _, e := range outs {
		if filepath.Ext(e.Name()) == ".ndjson" {
			nOut++
		}
	}
	if nOut != nFiles {
		t.Errorf("output files = %d, want %d", nOut, nFiles)
	}
	left, _ := os.ReadDir(filepath.Join(root, "input"))
	if len(left) != 0 {
		t.Errorf("input not drained: %d files left", len(left))
	}
}
