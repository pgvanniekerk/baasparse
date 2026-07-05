// Package storage is the data-plane's file-access abstraction (TS 16 §16.2). A
// Store is bound to a backend (posix / sftp / s3) and a root (base directory or
// bucket); callers address content by key. This lets the same pipeline run over
// a shared POSIX filesystem (on-prem) or S3 object storage with pod-local
// scratch (Kubernetes) — coordination stays in PostgreSQL, so exactly-once holds
// regardless of backend (BR-STO-001..004). Content is streamed, never held in
// PostgreSQL (BR-NFR-009).
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Backend identifiers.
const (
	BackendPOSIX = "posix"
	BackendS3    = "s3"
)

// ErrNotFound is returned when a key does not exist.
var ErrNotFound = errors.New("storage: not found")

// Entry describes one listed object/file.
type Entry struct {
	Key     string
	Size    int64
	ModTime time.Time
	ETag    string
}

// Meta carries optional write metadata.
type Meta struct {
	ContentType string
}

// Store is the backend-agnostic file interface. Keys use forward slashes and are
// relative to the store's root. All writes are atomic (BR-STO-004).
type Store interface {
	// Backend returns the backend identifier (posix|s3).
	Backend() string
	// List returns the entries under a key prefix (a "directory" of files),
	// excluding sub-"directory" markers. Used for detection (BR-COL-012).
	List(ctx context.Context, prefix string) ([]Entry, error)
	// Open streams the object at key (ErrNotFound if absent).
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	// Put writes body to key atomically (temp-then-rename on posix; atomic
	// PutObject/multipart on s3), replacing any existing object.
	Put(ctx context.Context, key string, body io.Reader, m Meta) error
	// Stat returns the entry for key (ErrNotFound if absent).
	Stat(ctx context.Context, key string) (Entry, error)
	// Move relocates srcKey to dstKey within the same store (rename on posix;
	// server-side copy + delete on s3).
	Move(ctx context.Context, srcKey, dstKey string) error
	// Delete removes key (no error if absent).
	Delete(ctx context.Context, key string) error
	// Close releases backend resources.
	Close() error
}

// Config selects and configures a backend.
type Config struct {
	Backend string // posix | s3

	// posix
	Root string // base directory; "" means keys are used as filesystem paths as-is

	// s3
	Endpoint  string // host:port (e.g. s3.amazonaws.com, minio:9000)
	Region    string
	Bucket    string // the store root for s3
	AccessKey string
	SecretKey string
	UseSSL    bool
	CreateBucket bool // create the bucket if missing (dev/MinIO convenience)
}

// New builds the Store for a Config.
func New(ctx context.Context, cfg Config) (Store, error) {
	switch cfg.Backend {
	case "", BackendPOSIX:
		return newPOSIX(cfg.Root), nil
	case BackendS3:
		return newS3(ctx, cfg)
	default:
		return nil, fmt.Errorf("storage: unsupported backend %q", cfg.Backend)
	}
}
