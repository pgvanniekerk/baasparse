package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strconv"
	"time"
)

// keepMarker is the sentinel object written into a "directory" so it materialises
// on backends where empty directories are not otherwise represented (POSIX needs
// a real dir for an operator to drop files into; on S3 it is a harmless no-op
// key). It is dot-prefixed so both backends' List skip it (posix.go / s3.go).
const keepMarker = ".keep"

// Relocate moves srcKey in src to dstKey in dst, spanning backends. When src and
// dst are the same store it uses the backend-native Move (posix rename / s3
// server-side copy+delete). When they differ it streams the object across
// (dst.Put ← src.Open) and only deletes the source after the destination write
// succeeds, so a failed transfer never loses the original. This is the primitive
// that lets a pipeline read from one datasource and land done/output files on a
// different one (Store.Move is same-store only).
func Relocate(ctx context.Context, dst Store, dstKey string, src Store, srcKey string) error {
	if dst == src {
		return dst.Move(ctx, srcKey, dstKey)
	}
	rc, err := src.Open(ctx, srcKey)
	if err != nil {
		return fmt.Errorf("relocate open %s: %w", srcKey, err)
	}
	if err := dst.Put(ctx, dstKey, rc, Meta{}); err != nil {
		rc.Close()
		return fmt.Errorf("relocate put %s: %w", dstKey, err)
	}
	if err := rc.Close(); err != nil {
		return fmt.Errorf("relocate close %s: %w", srcKey, err)
	}
	if err := src.Delete(ctx, srcKey); err != nil {
		return fmt.Errorf("relocate delete source %s: %w", srcKey, err)
	}
	return nil
}

// EnsureTree makes each of the given directory keys exist in the store by writing
// a dot-prefixed keep-marker into it. On POSIX this creates the real directory
// (so an operator can drop input files immediately); on S3 directories are
// virtual, so it is an effectively free no-op key. Empty/duplicate keys are
// skipped. Used when a filesystem-backed pipeline is created (TS 04 file
// lifecycle areas).
func EnsureTree(ctx context.Context, s Store, dirs ...string) error {
	seen := make(map[string]struct{}, len(dirs))
	for _, d := range dirs {
		if d == "" {
			continue
		}
		if _, ok := seen[d]; ok {
			continue
		}
		seen[d] = struct{}{}
		key := path.Join(d, keepMarker)
		if err := s.Put(ctx, key, bytes.NewReader(nil), Meta{}); err != nil {
			return fmt.Errorf("ensure dir %q: %w", d, err)
		}
	}
	return nil
}

// TestConnection verifies a storage config is reachable and usable — the datasource
// "Test connection" check. For posix it round-trips a probe object under Root
// (proving mkdir+write+read+delete). For s3 with a bucket it round-trips a probe
// object in that bucket; for s3 WITHOUT a bucket (a connection-only datasource,
// where the bucket is chosen per-pipeline) it validates the endpoint + credentials
// by listing buckets. Returns nil on success or a descriptive error.
func TestConnection(ctx context.Context, cfg Config) error {
	switch cfg.Backend {
	case "", BackendPOSIX:
		return Probe(ctx, newPOSIX(cfg.Root))
	case BackendS3:
		if cfg.Bucket == "" {
			return s3Ping(ctx, cfg)
		}
		s, err := newS3(ctx, cfg)
		if err != nil {
			return err
		}
		return Probe(ctx, s)
	default:
		return fmt.Errorf("unsupported backend %q", cfg.Backend)
	}
}

// Probe verifies that a store is reachable and read/writable by round-tripping a
// throwaway object: write → read back → verify bytes → delete, plus a List of the
// root to prove read access. It powers the datasource "Test connection" button
// and exercises the same code paths the data plane uses (mkdir+atomic write on
// posix, PutObject/GetObject on s3), so a green probe means real pipelines will
// work. The probe key lives under a dot-prefixed prefix so it is invisible to
// List-based detection even if the delete fails.
func Probe(ctx context.Context, s Store) error {
	if _, err := s.List(ctx, ""); err != nil {
		return fmt.Errorf("list: %w", err)
	}
	key := ".baasparse-probe/probe-" + strconv.FormatInt(time.Now().UnixNano(), 10)
	want := []byte("baasparse probe")
	if err := s.Put(ctx, key, bytes.NewReader(want), Meta{ContentType: "text/plain"}); err != nil {
		return fmt.Errorf("write: %w", err)
	}
	// Best-effort cleanup regardless of the read outcome.
	defer func() { _ = s.Delete(ctx, key) }()
	rc, err := s.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("read back: %w", err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		return fmt.Errorf("read back: %w", err)
	}
	if !bytes.Equal(got, want) {
		return fmt.Errorf("read back mismatch: wrote %d bytes, read %d", len(want), len(got))
	}
	return nil
}
