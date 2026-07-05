package runner

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/spec"
	"github.com/pgvanniekerk/baasparse/internal/storage"
	"github.com/pgvanniekerk/baasparse/internal/store"
)

// failPutStore opens a readable source but fails Put WITHOUT draining the body —
// reproducing the io.Pipe hang the reviewer found (pipeline blocked in pw.Write).
type failPutStore struct{ data []byte }

func (f *failPutStore) Backend() string { return "fake" }
func (f *failPutStore) List(context.Context, string) ([]storage.Entry, error) {
	return nil, nil
}
func (f *failPutStore) Open(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.data)), nil
}
func (f *failPutStore) Put(context.Context, string, io.Reader, storage.Meta) error {
	return errors.New("put failed") // does not read the body
}
func (f *failPutStore) Stat(context.Context, string) (storage.Entry, error) {
	return storage.Entry{Size: int64(len(f.data))}, nil
}
func (f *failPutStore) Move(context.Context, string, string) error  { return nil }
func (f *failPutStore) Delete(context.Context, string) error        { return nil }
func (f *failPutStore) Close() error                                { return nil }

func TestProcessFile_PutFailureDoesNotHang(t *testing.T) {
	r := New(store.NewMem(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	p := store.Pipeline{
		Name:      "t",
		Input:     spec.FormatSpec{Kind: spec.FormatDSV, DSV: &spec.DSVSpec{Delimiter: ",", HasHeader: true}},
		Transform: spec.TransformSpec{PassThrough: true},
		Output:    spec.FormatSpec{Kind: spec.FormatJSON, JSON: &spec.JSONSpec{Mode: "ndjson"}},
	}
	fs := &failPutStore{data: []byte("a,b\n1,2\n3,4\n5,6\n")}

	done := make(chan error, 1)
	go func() {
		_, err := r.ProcessFile(context.Background(), p, fs, "in/f.csv", "out", "done", nil)
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error from the failed Put, got nil")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ProcessFile hung on Put failure (io.Pipe deadlock regression)")
	}
}
