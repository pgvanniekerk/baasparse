package archiver

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// seedSource writes the given done files (and one completion marker) into a posix
// source store rooted at dir, and returns the store.
func seedSource(t *testing.T, dir string, files map[string]string) storage.Store {
	t.Helper()
	ctx := context.Background()
	sg, err := storage.New(ctx, storage.Config{Backend: storage.BackendPOSIX, Root: dir})
	if err != nil {
		t.Fatalf("source store: %v", err)
	}
	for name, body := range files {
		if err := sg.Put(ctx, "done/"+name, bytes.NewReader([]byte(body)), storage.Meta{}); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
	}
	// A completion marker under the .markers sub-area — must survive archival.
	if err := sg.Put(ctx, "done/.markers/1.json", bytes.NewReader([]byte(`{"fileUid":1}`)), storage.Meta{}); err != nil {
		t.Fatalf("put marker: %v", err)
	}
	return sg
}

func newArchiver(st store.Store, sg storage.Store) *Archiver {
	a := New(st, sg, testLogger())
	return a
}

func TestArchivePipeline_TarGz(t *testing.T) {
	ctx := context.Background()
	srcDir := t.TempDir()
	destDir := t.TempDir()

	contents := map[string]string{
		"a.json": `{"x":1}`,
		"b.json": `{"y":2}` + "\n" + `{"y":3}`,
	}
	srcStore := seedSource(t, srcDir, contents)
	defer srcStore.Close()

	mem := store.NewMem()
	old := time.Now().Add(-48 * time.Hour)
	for name, body := range contents {
		uid, _ := mem.NextFileUID(ctx)
		if err := mem.RecordProcessedFile(ctx, store.ProcessedFile{
			FileUID: uid, PipelineID: 1, Name: name, Size: int64(len(body)),
			Status: "DONE", OutputName: name, CollectedOn: old,
		}); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	p := store.Pipeline{
		ID: 1, Name: "Billing Feed", Enabled: true,
		Source: store.Source{Backend: storage.BackendPOSIX, Root: srcDir, DoneDir: "done"},
		Archive: &store.Archive{
			Enabled: true, AgeDays: 1, Compression: "tar_gz", ScheduleSeconds: 60,
			Dest: store.Dest{Backend: storage.BackendPOSIX, Root: destDir},
		},
	}

	a := newArchiver(mem, srcStore)
	defer a.closeStores()
	if err := a.archivePipeline(ctx, p); err != nil {
		t.Fatalf("archivePipeline: %v", err)
	}

	// 1. Archive + manifest exist on the destination.
	arc := findOne(t, destDir, ".tar.gz")
	manifestPath := arc + ".manifest.json"
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}

	// 2. Manifest checksums match the source content, and the archive sha matches
	//    the actual archive bytes.
	var m Manifest
	readJSON(t, manifestPath, &m)
	if len(m.Files) != 2 {
		t.Fatalf("manifest files = %d, want 2", len(m.Files))
	}
	for _, mf := range m.Files {
		want := sha256Hex(contents[mf.Name])
		if mf.SHA256 != want {
			t.Errorf("manifest sha for %s = %s, want %s", mf.Name, mf.SHA256, want)
		}
	}
	arcBytes, _ := os.ReadFile(arc)
	if got := hex.EncodeToString(sha256sum(arcBytes)); got != m.SHA256 {
		t.Errorf("archive sha = %s, manifest sha = %s", got, m.SHA256)
	}
	if m.SizeBytes != int64(len(arcBytes)) {
		t.Errorf("manifest size = %d, actual = %d", m.SizeBytes, len(arcBytes))
	}

	// 3. Archive extracts back to the original content.
	extracted := extractTarGz(t, arcBytes)
	for name, body := range contents {
		if extracted[name] != body {
			t.Errorf("extracted %s = %q, want %q", name, extracted[name], body)
		}
	}

	// 4. Originals pruned, completion marker preserved.
	for name := range contents {
		if _, err := os.Stat(filepath.Join(srcDir, "done", name)); !os.IsNotExist(err) {
			t.Errorf("done/%s not pruned (err=%v)", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(srcDir, "done", ".markers", "1.json")); err != nil {
		t.Errorf("completion marker was removed: %v", err)
	}

	// 5. Processed files flipped to ARCHIVED.
	pfs, _ := mem.ListProcessedFiles(ctx, 0)
	for _, pf := range pfs {
		if pf.Status != "ARCHIVED" {
			t.Errorf("PF %s status = %s, want ARCHIVED", pf.Name, pf.Status)
		}
	}

	// 6. A second pass is a no-op (nothing left DONE) and must not error.
	if err := a.archivePipeline(ctx, p); err != nil {
		t.Fatalf("second archivePipeline: %v", err)
	}
}

func TestArchivePipeline_Zip(t *testing.T) {
	ctx := context.Background()
	srcDir := t.TempDir()
	destDir := t.TempDir()
	contents := map[string]string{"rec.csv": "a,b,c\n1,2,3\n"}
	srcStore := seedSource(t, srcDir, contents)
	defer srcStore.Close()

	mem := store.NewMem()
	uid, _ := mem.NextFileUID(ctx)
	_ = mem.RecordProcessedFile(ctx, store.ProcessedFile{
		FileUID: uid, PipelineID: 1, Name: "rec.csv", Size: int64(len(contents["rec.csv"])),
		Status: "DONE", OutputName: "rec.csv", CollectedOn: time.Now().Add(-time.Hour),
	})

	p := store.Pipeline{
		ID: 1, Name: "z", Enabled: true,
		Source:  store.Source{Backend: storage.BackendPOSIX, Root: srcDir, DoneDir: "done"},
		Archive: &store.Archive{Enabled: true, AgeDays: 0, Compression: "zip", Dest: store.Dest{Backend: storage.BackendPOSIX, Root: destDir}},
	}
	a := newArchiver(mem, srcStore)
	defer a.closeStores()
	if err := a.archivePipeline(ctx, p); err != nil {
		t.Fatalf("archivePipeline: %v", err)
	}

	arc := findOne(t, destDir, ".zip")
	arcBytes, _ := os.ReadFile(arc)
	zr, err := zip.NewReader(bytes.NewReader(arcBytes), int64(len(arcBytes)))
	if err != nil {
		t.Fatalf("zip open: %v", err)
	}
	if len(zr.File) != 1 || zr.File[0].Name != "rec.csv" {
		t.Fatalf("unexpected zip members: %+v", zr.File)
	}
	rc, _ := zr.File[0].Open()
	got, _ := io.ReadAll(rc)
	rc.Close()
	if string(got) != contents["rec.csv"] {
		t.Errorf("zip content = %q, want %q", got, contents["rec.csv"])
	}
}

// TestArchivePipeline_SkipUnconfiguredDest ensures an unconfigured destination is
// skipped without error and without pruning.
func TestArchivePipeline_SkipUnconfiguredDest(t *testing.T) {
	ctx := context.Background()
	srcDir := t.TempDir()
	srcStore := seedSource(t, srcDir, map[string]string{"a.json": "{}"})
	defer srcStore.Close()

	mem := store.NewMem()
	uid, _ := mem.NextFileUID(ctx)
	_ = mem.RecordProcessedFile(ctx, store.ProcessedFile{
		FileUID: uid, PipelineID: 1, Name: "a.json", Status: "DONE", CollectedOn: time.Now().Add(-time.Hour),
	})
	p := store.Pipeline{
		ID: 1, Name: "x", Enabled: true,
		Source:  store.Source{Backend: storage.BackendPOSIX, Root: srcDir, DoneDir: "done"},
		Archive: &store.Archive{Enabled: true, AgeDays: 0, Dest: store.Dest{}}, // no Root/bucket
	}
	a := newArchiver(mem, srcStore)
	defer a.closeStores()
	if err := a.archivePipeline(ctx, p); err != nil {
		t.Fatalf("archivePipeline: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "done", "a.json")); err != nil {
		t.Errorf("original should not be pruned when dest unconfigured: %v", err)
	}
	pfs, _ := mem.ListProcessedFiles(ctx, 0)
	if pfs[0].Status != "DONE" {
		t.Errorf("status = %s, want DONE (unchanged)", pfs[0].Status)
	}
}

func TestDue(t *testing.T) {
	a := New(store.NewMem(), nil, testLogger())
	p := store.Pipeline{ID: 7, Archive: &store.Archive{ScheduleSeconds: 100}}
	now := time.Now()
	if !a.due(p, now) {
		t.Fatal("first check should be due")
	}
	a.lastRun[7] = now
	if a.due(p, now.Add(50*time.Second)) {
		t.Fatal("should not be due before schedule elapses")
	}
	if !a.due(p, now.Add(100*time.Second)) {
		t.Fatal("should be due once schedule elapses")
	}
}

// --- helpers ---

func findOne(t *testing.T, dir, suffix string) string {
	t.Helper()
	var found string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && filepath.Ext(p) != ".json" && hasSuffix(p, suffix) {
			found = p
		}
		return nil
	})
	if err != nil || found == "" {
		t.Fatalf("no %s archive under %s (err=%v)", suffix, dir, err)
	}
	return found
}

func hasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
}

func sha256sum(b []byte) []byte { h := sha256.Sum256(b); return h[:] }
func sha256Hex(s string) string { return hex.EncodeToString(sha256sum([]byte(s))) }

func extractTarGz(t *testing.T, data []byte) map[string]string {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	tr := tar.NewReader(gz)
	out := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		body, _ := io.ReadAll(tr)
		out[hdr.Name] = string(body)
	}
	return out
}
