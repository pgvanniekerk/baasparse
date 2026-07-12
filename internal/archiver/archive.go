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
	"fmt"
	"io"
	"time"

	"github.com/pgvanniekerk/baasparse/internal/storage"
)

// fileEntry is one original object selected for archival.
type fileEntry struct {
	Name    string    // base name (manifest + archive member name)
	Key     string    // storage key on the source store
	Size    int64     // size in bytes (from the directory listing)
	ModTime time.Time // modification time (archive member metadata)
}

// ManifestFile is one file's record in the archive manifest.
type ManifestFile struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// Manifest is the sidecar written next to an archive object. It records each
// member's checksum and the archive object's own sha256 (computed over the final
// compressed bytes), so an operator can validate an offloaded archive
// independently of the database.
type Manifest struct {
	Archive     string         `json:"archive"`
	Pipeline    string         `json:"pipeline"`
	Compression string         `json:"compression"`
	CreatedAt   time.Time      `json:"createdAt"`
	SHA256      string         `json:"sha256"` // sha256 of the archive object
	SizeBytes   int64          `json:"sizeBytes"`
	Files       []ManifestFile `json:"files"`
}

// byteCounter counts bytes written (to size the archive object without buffering
// it in memory).
type byteCounter struct{ n int64 }

func (c *byteCounter) Write(p []byte) (int, error) {
	c.n += int64(len(p))
	return len(p), nil
}

// buildAndOffload streams the selected files into a single compressed archive
// and writes it to the destination store, computing per-file sha256 checksums,
// the archive's own sha256, and its byte size in one pass. Memory stays bounded:
// the archive is produced on a pipe that the destination Put consumes.
func buildAndOffload(ctx context.Context, src, dest storage.Store, files []fileEntry, compression, archiveKey string) (Manifest, int64, error) {
	pr, pw := io.Pipe()
	arcHash := sha256.New()
	counter := &byteCounter{}

	type buildRes struct {
		mfiles []ManifestFile
		err    error
	}
	resCh := make(chan buildRes, 1)
	go func() {
		mw := io.MultiWriter(pw, arcHash, counter)
		mfiles, err := writeArchive(ctx, mw, src, files, compression)
		// Propagate any build error to the reader (Put) so it aborts and never
		// commits a partial/corrupt archive object.
		_ = pw.CloseWithError(err)
		resCh <- buildRes{mfiles, err}
	}()

	putErr := dest.Put(ctx, archiveKey, pr, storage.Meta{ContentType: contentTypeFor(compression)})
	// Release the builder goroutine if Put failed early.
	_ = pr.CloseWithError(putErr)
	br := <-resCh

	if br.err != nil {
		return Manifest{}, 0, fmt.Errorf("build: %w", br.err)
	}
	if putErr != nil {
		return Manifest{}, 0, fmt.Errorf("offload: %w", putErr)
	}

	m := Manifest{
		Archive:     archiveKey,
		Compression: compression,
		CreatedAt:   time.Now().UTC(),
		SHA256:      hex.EncodeToString(arcHash.Sum(nil)),
		SizeBytes:   counter.n,
		Files:       br.mfiles,
	}
	return m, counter.n, nil
}

// writeArchive dispatches on the compression selector. "gzip" and "tar_gz" both
// produce a gzip-compressed tar bundle; "zip" produces a zip archive.
func writeArchive(ctx context.Context, w io.Writer, src storage.Store, files []fileEntry, compression string) ([]ManifestFile, error) {
	if compression == "zip" {
		return writeZip(ctx, w, src, files)
	}
	return writeTarGz(ctx, w, src, files)
}

func writeTarGz(ctx context.Context, w io.Writer, src storage.Store, files []fileEntry) ([]ManifestFile, error) {
	gz := gzip.NewWriter(w)
	tw := tar.NewWriter(gz)
	var mfiles []ManifestFile
	for _, f := range files {
		mf, err := copyMember(ctx, src, f, func(size int64) (io.Writer, error) {
			mod := f.ModTime
			if mod.IsZero() {
				mod = time.Now()
			}
			hdr := &tar.Header{Name: f.Name, Mode: 0o644, Size: size, ModTime: mod, Typeflag: tar.TypeReg}
			if err := tw.WriteHeader(hdr); err != nil {
				return nil, err
			}
			return tw, nil
		})
		if err != nil {
			return nil, err
		}
		mfiles = append(mfiles, mf)
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("close gzip: %w", err)
	}
	return mfiles, nil
}

func writeZip(ctx context.Context, w io.Writer, src storage.Store, files []fileEntry) ([]ManifestFile, error) {
	zw := zip.NewWriter(w)
	var mfiles []ManifestFile
	for _, f := range files {
		// copyMember hashes the source content via a TeeReader before it is
		// deflated, so the manifest records the pre-compression sha256.
		mf, err := copyMember(ctx, src, f, func(int64) (io.Writer, error) {
			mod := f.ModTime
			if mod.IsZero() {
				mod = time.Now()
			}
			hdr := &zip.FileHeader{Name: f.Name, Method: zip.Deflate, Modified: mod}
			return zw.CreateHeader(hdr)
		})
		if err != nil {
			return nil, err
		}
		mfiles = append(mfiles, mf)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("close zip: %w", err)
	}
	return mfiles, nil
}

// copyMember streams one source object into the archive member writer produced
// by open(size), hashing the content as it goes. It returns the member's
// manifest entry (name, observed size, sha256).
func copyMember(ctx context.Context, src storage.Store, f fileEntry, open func(size int64) (io.Writer, error)) (ManifestFile, error) {
	rc, err := src.Open(ctx, f.Key)
	if err != nil {
		return ManifestFile{}, fmt.Errorf("open %q: %w", f.Key, err)
	}
	defer rc.Close()

	dst, err := open(f.Size)
	if err != nil {
		return ManifestFile{}, fmt.Errorf("archive header %q: %w", f.Name, err)
	}
	h := sha256.New()
	n, err := io.Copy(dst, io.TeeReader(rc, h))
	if err != nil {
		return ManifestFile{}, fmt.Errorf("copy %q: %w", f.Name, err)
	}
	// tar declared f.Size up front; a size drift would corrupt the stream.
	if f.Size >= 0 && n != f.Size {
		return ManifestFile{}, fmt.Errorf("size drift for %q: listed %d, read %d", f.Name, f.Size, n)
	}
	return ManifestFile{Name: f.Name, Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}, nil
}

// verify confirms the archive object is intact on the destination. The primary
// check is the Stat size; if the backend does not report a size, it falls back
// to a readback sha256 comparison.
func verify(ctx context.Context, dest storage.Store, key string, wantSize int64, wantSHA string) error {
	st, err := dest.Stat(ctx, key)
	if err != nil {
		return fmt.Errorf("stat: %w", err)
	}
	if st.Size == wantSize {
		return nil
	}
	if st.Size > 0 {
		return fmt.Errorf("size mismatch: wrote %d, destination reports %d", wantSize, st.Size)
	}
	// Backend did not report a usable size — read the object back and compare.
	rc, err := dest.Open(ctx, key)
	if err != nil {
		return fmt.Errorf("readback open: %w", err)
	}
	defer rc.Close()
	h := sha256.New()
	if _, err := io.Copy(h, rc); err != nil {
		return fmt.Errorf("readback: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != wantSHA {
		return fmt.Errorf("checksum mismatch: wrote %s, readback %s", wantSHA, got)
	}
	return nil
}

// putManifest writes the manifest sidecar next to the archive object.
func putManifest(ctx context.Context, dest storage.Store, archiveKey string, m Manifest) error {
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return dest.Put(ctx, archiveKey+".manifest.json", bytes.NewReader(body), storage.Meta{ContentType: "application/json"})
}

func contentTypeFor(compression string) string {
	if compression == "zip" {
		return "application/zip"
	}
	return "application/gzip"
}
