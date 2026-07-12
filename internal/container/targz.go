// Package container iterates the member files inside an archive object, as a
// layer ABOVE the decoder seam (TS 04 §4.4.8): the runner walks members and
// feeds each one to the pipeline's ordinary format decoder. tar.gz is fully
// streaming — gzip and tar are both sequential formats, so memory stays bounded
// at the gzip window + read buffer regardless of archive size or member count.
//
// Safety (decompression-bomb guards, BR-COL-013/017): member count, per-member
// decompressed bytes and total decompressed bytes are enforced on the fly while
// streaming; a breach aborts with a LimitError so the caller quarantines the
// archive with an ARCHIVE_LIMIT_EXCEEDED reason. Only regular-file members are
// yielded (symlinks/devices/etc. skipped); member names are never used as
// filesystem paths; nested archives are NOT recursed into.
package container

import (
	"archive/tar"
	"errors"
	"fmt"
	"io"
	"path"

	"github.com/klauspost/compress/gzip"
)

// Limits bound a single archive's decompressed size. Zero fields fall back to
// the package defaults (env-configurable at startup, BAASPARSE_ARCHIVE_MAX_*).
// These are bomb guards, not sizing policy — set well above legitimate traffic.
type Limits struct {
	MaxMembers     int64
	MaxMemberBytes int64
	MaxTotalBytes  int64
}

// Package defaults (overridden from settings at startup).
var (
	MaxMembers     int64 = 100_000
	MaxMemberBytes int64 = 32 << 30  // 32 GiB
	MaxTotalBytes  int64 = 256 << 30 // 256 GiB
)

func (l Limits) withDefaults() Limits {
	if l.MaxMembers <= 0 {
		l.MaxMembers = MaxMembers
	}
	if l.MaxMemberBytes <= 0 {
		l.MaxMemberBytes = MaxMemberBytes
	}
	if l.MaxTotalBytes <= 0 {
		l.MaxTotalBytes = MaxTotalBytes
	}
	return l
}

// LimitError marks an archive that exceeded a safety limit — a content problem
// (quarantine with reason), not a transient infrastructure error.
type LimitError struct{ Msg string }

func (e *LimitError) Error() string { return e.Msg }

// TarGz streams the members of a tar.gz archive in order. Not safe for
// concurrent use; the Reader returned for a member is valid only until the next
// Next call (same synchronous-consumption contract as the record decoders).
type TarGz struct {
	gz     *gzip.Reader
	cr     *countingReader // sits under tar: sees EVERY decompressed byte
	tr     *tar.Reader
	glob   string
	limits Limits

	member  string // current member base name
	members int64
	cur     *memberReader
	err     error
}

// countingReader meters the decompressed stream between gzip and tar. Placing
// the total-bytes guard HERE — rather than in the per-member reader — is what
// makes it unbypassable: tar.Reader must decompress a member's body to reach
// the next header, so bytes of members we skip (glob misses, directories,
// non-regular types) and of members the caller abandons early still flow
// through this Read and still count. Tar headers and padding count too.
type countingReader struct {
	r     io.Reader
	total int64
	max   int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.total += int64(n)
		if c.total > c.max {
			return n, &LimitError{Msg: fmt.Sprintf("archive exceeds max total decompressed size (%d bytes)", c.max)}
		}
	}
	return n, err
}

// OpenTarGz starts streaming r as a tar.gz archive. glob filters members by
// base name ("" or "*" accepts every regular file). The reader should already
// be buffered (the runner wraps input in the configured read buffer).
func OpenTarGz(r io.Reader, glob string, limits Limits) (*TarGz, error) {
	if glob != "" { // validate the pattern once up front, not per member
		if _, err := path.Match(glob, "probe"); err != nil {
			return nil, fmt.Errorf("container: invalid member glob %q: %w", glob, err)
		}
	}
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("container: gzip: %w", err)
	}
	lim := limits.withDefaults()
	cr := &countingReader{r: gz, max: lim.MaxTotalBytes}
	return &TarGz{gz: gz, cr: cr, tr: tar.NewReader(cr), glob: glob, limits: lim}, nil
}

// Next advances to the next regular-file member matching the glob. It returns
// false at the end of the archive or on error (check Err).
//
// Every regular-file entry counts against MaxMembers whether or not it matches
// the glob, and its bytes count against MaxTotalBytes whether or not we hand it
// to the caller — an archive cannot dodge the guards by naming its payload
// something the pipeline does not select.
func (t *TarGz) Next() bool {
	if t.err != nil {
		return false
	}
	t.cur = nil
	for {
		hdr, err := t.tr.Next()
		if errors.Is(err, io.EOF) {
			t.finish()
			return false
		}
		if err != nil {
			t.err = t.wrap("tar", err)
			return false
		}
		if hdr.Typeflag != tar.TypeReg {
			continue // dirs, symlinks, devices, hard links: never data
		}
		t.members++
		if t.members > t.limits.MaxMembers {
			t.err = &LimitError{Msg: fmt.Sprintf("archive exceeds max member count (%d)", t.limits.MaxMembers)}
			return false
		}
		name := path.Base(path.Clean(hdr.Name))
		if name == "." || name == ".." || name == "/" || name == "" {
			continue
		}
		if t.glob != "" && t.glob != "*" {
			if ok, _ := path.Match(t.glob, name); !ok {
				continue // body still metered: tar decompresses it to reach the next header
			}
		}
		t.member = name
		t.cur = &memberReader{t: t}
		return true
	}
}

// finish consumes whatever remains of the gzip stream after tar's end-of-archive
// marker. Without this the gzip CRC32/ISIZE trailer is never read, so a
// truncated or corrupted archive would be reported as a clean, complete run
// (records silently lost, file marked DONE). Draining still meters through the
// counting reader, so it cannot itself be used as a bomb.
func (t *TarGz) finish() {
	if _, err := io.Copy(io.Discard, t.cr); err != nil {
		t.err = t.wrap("archive trailer", err)
	}
}

// wrap preserves typed errors (LimitError above all) through the context string
// so the runner's errors.As classification still sees them.
func (t *TarGz) wrap(what string, err error) error {
	var le *LimitError
	if errors.As(err, &le) {
		return err
	}
	return fmt.Errorf("container: %s: %w", what, err)
}

// Member returns the current member's base name.
func (t *TarGz) Member() string { return t.member }

// Reader returns the current member's content stream.
func (t *TarGz) Reader() io.Reader { return t.cur }

// Err returns the terminal error, if any ("" end-of-archive returns nil).
func (t *TarGz) Err() error { return t.err }

// Close releases the gzip reader (the underlying object stream is closed by the
// caller, which owns it).
func (t *TarGz) Close() error { return t.gz.Close() }

// memberReader streams one member's bytes. It enforces only the PER-MEMBER
// limit; the total is metered underneath by countingReader, which also covers
// the bytes tar skips on our behalf.
type memberReader struct {
	t    *TarGz
	read int64
}

func (m *memberReader) Read(p []byte) (int, error) {
	n, err := m.t.tr.Read(p)
	if n > 0 {
		m.read += int64(n)
		if m.read > m.t.limits.MaxMemberBytes {
			return n, &LimitError{Msg: fmt.Sprintf("member %q exceeds max decompressed size (%d bytes)", m.t.member, m.t.limits.MaxMemberBytes)}
		}
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return n, m.t.wrap(fmt.Sprintf("member %q", m.t.member), err)
	}
	return n, err
}
