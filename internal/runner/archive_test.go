package runner

import (
	"archive/tar"
	"bytes"
	"compress/gzip" // stdlib: independently verifies the klauspost-written output
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pgvanniekerk/baasparse/internal/container"
	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

func writeTarGz(t *testing.T, path string, members map[string]string) {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	// deterministic order
	names := make([]string, 0, len(members))
	for n := range members {
		names = append(names, n)
	}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, n := range names {
		body := members[n]
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	tw.Close()
	gz.Close()
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func archivePipeline() store.Pipeline {
	return store.Pipeline{
		Name: "arc",
		Input: spec.FormatSpec{
			Kind: spec.FormatDSV, Container: "targz", MemberGlob: "*.csv",
			Fields: []spec.FieldSpec{{Name: "a", Type: spec.TypeInteger}, {Name: "b"}},
			DSV:    &spec.DSVSpec{Delimiter: ",", HasHeader: true, Columns: []string{"a", "b"}},
		},
		Transform: spec.TransformSpec{PassThrough: true},
		Output:    spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}, Compress: "gzip"},
	}
}

// TestProcessFileTarGzToGz drives the full phase-1 path: a tar.gz of CSV members
// in, one gzip-compressed NDJSON out — verified by decompressing with the STDLIB
// gzip reader and counting/parsing records.
func TestProcessFileTarGzToGz(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "in"), 0o755)
	members := map[string]string{
		"m1.csv":    "a,b\n1,one\n2,two\n",
		"m2.csv":    "a,b\n3,three\n",
		"skip.json": `{"not":"matched"}`,
		"m3.csv":    "a,b\n4,four\n5,five\n6,six\n",
	}
	writeTarGz(t, filepath.Join(root, "in", "batch.tar.gz"), members)

	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r := New(store.NewMem(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	pf, err := r.ProcessFile(context.Background(), archivePipeline(), sg, sg, "in/batch.tar.gz", "out", "done", nil)
	if err != nil {
		t.Fatalf("ProcessFile: %v", err)
	}
	if pf.RecordsIn != 6 || pf.RecordsOut != 6 {
		t.Fatalf("records in/out = %d/%d, want 6/6", pf.RecordsIn, pf.RecordsOut)
	}
	if !strings.HasSuffix(pf.OutputName, ".ndjson.gz") {
		t.Fatalf("output name = %q, want .ndjson.gz suffix", pf.OutputName)
	}

	f, err := os.Open(filepath.Join(root, "out", pf.OutputName))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("output is not valid gzip: %v", err)
	}
	body, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("gunzip: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	if len(lines) != 6 {
		t.Fatalf("output lines = %d, want 6\n%s", len(lines), body)
	}
	if lines[0] != `{"a":1,"b":"one"}` || lines[5] != `{"a":6,"b":"six"}` {
		t.Fatalf("content order/values wrong:\n%s", body)
	}
}

// TestProcessFileArchiveBadMember: a malformed member must quarantine the WHOLE
// archive as a content error, naming the member in the reason.
func TestProcessFileArchiveBadMember(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "in"), 0o755)
	p := archivePipeline()
	p.Input.Kind = spec.FormatJSON
	p.Input.JSON = &spec.JSONSpec{Mode: "ndjson"}
	p.Input.DSV = nil
	p.Input.MemberGlob = "*.ndjson"
	writeTarGz(t, filepath.Join(root, "in", "bad.tar.gz"), map[string]string{
		"ok.ndjson":     `{"a":1}` + "\n",
		"broken.ndjson": "NOT JSON\n",
	})
	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r := New(store.NewMem(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := r.ProcessFile(context.Background(), p, sg, sg, "in/bad.tar.gz", "out", "done", nil)
	var bad *BadFileError
	if !errors.As(err, &bad) {
		t.Fatalf("want BadFileError, got %v", err)
	}
	if !strings.Contains(bad.Reason, `member "broken.ndjson"`) || !strings.HasPrefix(bad.Reason, "DECODE_ERROR") {
		t.Fatalf("reason must name the member with DECODE_ERROR prefix: %q", bad.Reason)
	}
}

// TestProcessFileArchiveLimit: tripping a decompression guard quarantines with
// the ARCHIVE_LIMIT_EXCEEDED reason.
func TestProcessFileArchiveLimit(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "in"), 0o755)
	big := "a,b\n" + strings.Repeat("1,x\n", 5000)
	writeTarGz(t, filepath.Join(root, "in", "big.tar.gz"), map[string]string{"big.csv": big})

	// Shrink the package default for the duration of this test.
	old := container.MaxMemberBytes
	container.MaxMemberBytes = 64
	defer func() { container.MaxMemberBytes = old }()

	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r := New(store.NewMem(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := r.ProcessFile(context.Background(), archivePipeline(), sg, sg, "in/big.tar.gz", "out", "done", nil)
	var bad *BadFileError
	if !errors.As(err, &bad) {
		t.Fatalf("want BadFileError, got %v", err)
	}
	if !strings.HasPrefix(bad.Reason, "ARCHIVE_LIMIT_EXCEEDED") {
		t.Fatalf("reason = %q, want ARCHIVE_LIMIT_EXCEEDED prefix", bad.Reason)
	}
}

// TestProcessFileArchiveLimitNDJSON pins a misclassification found in review: the
// NDJSON path runs on bufio.Scanner, which treats ANY read error as EOF and hands
// the partial line to the parser — so a member breaching the size guard used to be
// reported as a phantom "unterminated string" DECODE_ERROR instead of
// ARCHIVE_LIMIT_EXCEEDED, and whether it did depended on where the read boundary
// landed. The reason code must now come from the stream, not the parser.
func TestProcessFileArchiveLimitNDJSON(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "in"), 0o755)
	p := archivePipeline()
	p.Input.Kind = spec.FormatJSON
	p.Input.JSON = &spec.JSONSpec{Mode: "ndjson"}
	p.Input.DSV = nil
	p.Input.MemberGlob = "*.ndjson"

	var body strings.Builder
	for i := 0; i < 5000; i++ { // every line individually valid JSON
		body.WriteString(`{"a":1,"b":"padding-padding-padding"}` + "\n")
	}
	writeTarGz(t, filepath.Join(root, "in", "big.tar.gz"), map[string]string{"big.ndjson": body.String()})

	old := container.MaxMemberBytes
	container.MaxMemberBytes = 64
	defer func() { container.MaxMemberBytes = old }()

	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r := New(store.NewMem(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := r.ProcessFile(context.Background(), p, sg, sg, "in/big.tar.gz", "out", "done", nil)
	var bad *BadFileError
	if !errors.As(err, &bad) {
		t.Fatalf("want BadFileError, got %v", err)
	}
	if !strings.HasPrefix(bad.Reason, "ARCHIVE_LIMIT_EXCEEDED") {
		t.Fatalf("limit breach must not be masked as a parse error; reason = %q", bad.Reason)
	}
}

// tornReadStore serves n good bytes then fails — a torn object read (S3 reset,
// stalled mount) partway through an otherwise valid file.
type tornReadStore struct {
	data []byte
	n    int
}

func (s *tornReadStore) Backend() string                                       { return "fake" }
func (s *tornReadStore) List(context.Context, string) ([]storage.Entry, error) { return nil, nil }
func (s *tornReadStore) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(io.MultiReader(
		bytes.NewReader(s.data[:s.n]),
		&errReader{err: errors.New("connection reset by peer")},
	)), nil
}
func (s *tornReadStore) Put(_ context.Context, _ string, body io.Reader, _ storage.Meta) error {
	_, err := io.Copy(io.Discard, body)
	return err
}
func (s *tornReadStore) Stat(context.Context, string) (storage.Entry, error) {
	return storage.Entry{Size: int64(len(s.data))}, nil
}
func (s *tornReadStore) Move(context.Context, string, string) error { return nil }
func (s *tornReadStore) Delete(context.Context, string) error       { return nil }
func (s *tornReadStore) Close() error                               { return nil }

type errReader struct{ err error }

func (e *errReader) Read([]byte) (int, error) { return 0, e.err }

// TestProcessFileTornSourceReadIsTransient pins the other half of that bug: when
// the SOURCE stream fails mid-file, the decoder reports the truncated tail as a
// content error. Quarantining on that would condemn a perfectly good file over a
// network blip — it must be retryable instead.
func TestProcessFileTornSourceReadIsTransient(t *testing.T) {
	data := []byte(strings.Repeat(`{"a":1,"b":"x"}`+"\n", 500))
	src := &tornReadStore{data: data, n: 200} // cut mid-record
	p := archivePipeline()
	p.Input.Container = ""
	p.Input.MemberGlob = ""
	p.Input.Kind = spec.FormatJSON
	p.Input.JSON = &spec.JSONSpec{Mode: "ndjson"}
	p.Input.DSV = nil

	r := New(store.NewMem(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	_, err := r.ProcessFile(context.Background(), p, src, src, "in/f.ndjson", "out", "done", nil)
	if err == nil {
		t.Fatal("torn read must fail")
	}
	var bad *BadFileError
	if errors.As(err, &bad) {
		t.Fatalf("torn source read quarantined as content (%q) — must be retried", bad.Reason)
	}
	if !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("error should carry the underlying cause: %v", err)
	}
}

// TestProcessFilePlainGzOutput: compression works for plain (non-archive) input too.
func TestProcessFilePlainGzOutput(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "in"), 0o755)
	os.WriteFile(filepath.Join(root, "in", "f.csv"), []byte("a,b\n1,x\n"), 0o644)
	p := archivePipeline()
	p.Input.Container = ""
	p.Input.MemberGlob = ""
	sg, _ := storage.New(context.Background(), storage.Config{Backend: storage.BackendPOSIX, Root: root})
	r := New(store.NewMem(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	pf, err := r.ProcessFile(context.Background(), p, sg, sg, "in/f.csv", "out", "done", nil)
	if err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(filepath.Join(root, "out", pf.OutputName))
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("not gzip: %v", err)
	}
	b, _ := io.ReadAll(gz)
	if strings.TrimSpace(string(b)) != `{"a":1,"b":"x"}` {
		t.Fatalf("content = %q", b)
	}
}
