package container

import (
	"archive/tar"
	"bytes"
	"compress/gzip" // stdlib on the write side, cross-checking the klauspost read side
	"errors"
	"io"
	"strings"
	"testing"
)

type member struct {
	name string
	body string
	typ  byte
}

func buildTarGz(t *testing.T, members []member) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, m := range members {
		typ := m.typ
		if typ == 0 {
			typ = tar.TypeReg
		}
		hdr := &tar.Header{Name: m.name, Mode: 0o644, Size: int64(len(m.body)), Typeflag: typ}
		if typ == tar.TypeSymlink {
			hdr.Size = 0
			hdr.Linkname = "/etc/passwd"
		}
		if typ == tar.TypeDir {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if typ == tar.TypeReg {
			if _, err := tw.Write([]byte(m.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func readAllMembers(t *testing.T, data []byte, glob string, limits Limits) (map[string]string, error) {
	t.Helper()
	tg, err := OpenTarGz(bytes.NewReader(data), glob, limits)
	if err != nil {
		return nil, err
	}
	defer tg.Close()
	out := map[string]string{}
	for tg.Next() {
		b, err := io.ReadAll(tg.Reader())
		if err != nil {
			return out, err
		}
		out[tg.Member()] = string(b)
	}
	return out, tg.Err()
}

func TestTarGzIteration(t *testing.T) {
	data := buildTarGz(t, []member{
		{name: "day1/a.csv", body: "a,b\n1,2\n"},
		{name: "dir/", typ: tar.TypeDir},
		{name: "evil", typ: tar.TypeSymlink},
		{name: "day2/b.csv", body: "a,b\n3,4\n"},
		{name: "notes.txt", body: "ignore me"},
		{name: "../traversal.csv", body: "x,y\n5,6\n"}, // name never used as a path
	})
	got, err := readAllMembers(t, data, "*.csv", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	// base-name matching: a.csv, b.csv, traversal.csv; txt/dir/symlink skipped
	if len(got) != 3 || got["a.csv"] == "" || got["b.csv"] == "" || got["traversal.csv"] == "" {
		t.Fatalf("members = %v", got)
	}

	all, err := readAllMembers(t, data, "", Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 4 { // + notes.txt; still no dir/symlink
		t.Fatalf("unfiltered members = %v", all)
	}
}

func TestTarGzLimits(t *testing.T) {
	data := buildTarGz(t, []member{
		{name: "a.csv", body: strings.Repeat("x", 100)},
		{name: "b.csv", body: strings.Repeat("y", 100)},
		{name: "c.csv", body: strings.Repeat("z", 100)},
	})
	var le *LimitError

	_, err := readAllMembers(t, data, "", Limits{MaxMembers: 2})
	if !errors.As(err, &le) {
		t.Fatalf("MaxMembers: want LimitError, got %v", err)
	}
	_, err = readAllMembers(t, data, "", Limits{MaxMemberBytes: 50})
	if !errors.As(err, &le) {
		t.Fatalf("MaxMemberBytes: want LimitError, got %v", err)
	}
	_, err = readAllMembers(t, data, "", Limits{MaxTotalBytes: 250})
	if !errors.As(err, &le) {
		t.Fatalf("MaxTotalBytes: want LimitError, got %v", err)
	}
	// MaxTotalBytes meters the whole decompressed stream (tar headers + padding
	// + bodies), not just record bytes — that is the surface a bomb actually
	// inflates. 3 members => 3 headers + 3 padded blocks + terminator.
	if _, err = readAllMembers(t, data, "", Limits{MaxMembers: 3, MaxMemberBytes: 100, MaxTotalBytes: 16 << 10}); err != nil {
		t.Fatalf("within limits: %v", err)
	}
}

// TestTarGzSkippedMembersAreMetered locks the decompression-bomb hole found in
// review: bytes of members the glob does NOT select are still decompressed by
// tar.Reader on its way to the next header, so they MUST count toward the total
// guard. Before the fix, an archive whose payload was simply named something the
// pipeline did not select could inflate without bound and never trip a limit.
func TestTarGzSkippedMembersAreMetered(t *testing.T) {
	data := buildTarGz(t, []member{
		{name: "payload.bin", body: strings.Repeat("\x00", 100_000)}, // not matched by *.csv
		{name: "a.csv", body: "a,b\n1,2\n"},
	})
	tg, err := OpenTarGz(bytes.NewReader(data), "*.csv", Limits{MaxTotalBytes: 4096})
	if err != nil {
		t.Fatal(err)
	}
	defer tg.Close()
	for tg.Next() {
		io.Copy(io.Discard, tg.Reader())
	}
	var le *LimitError
	if !errors.As(tg.Err(), &le) {
		t.Fatalf("glob-skipped payload must still meter toward MaxTotalBytes; got err=%v", tg.Err())
	}
	// ...and skipped entries count toward MaxMembers, bounding header-only bombs.
	tg2, _ := OpenTarGz(bytes.NewReader(data), "*.csv", Limits{MaxMembers: 1})
	defer tg2.Close()
	for tg2.Next() {
		io.Copy(io.Discard, tg2.Reader())
	}
	if !errors.As(tg2.Err(), &le) {
		t.Fatalf("skipped entries must count toward MaxMembers; got err=%v", tg2.Err())
	}
}

// TestTarGzTruncated: a tar.gz cut off mid-stream must be an ERROR, not a short
// clean run. Before the trailer drain, tar's end-of-archive marker ended
// iteration without ever validating the gzip CRC32/ISIZE, so a truncated archive
// could complete DONE with records silently missing.
func TestTarGzTruncated(t *testing.T) {
	data := buildTarGz(t, []member{{name: "a.csv", body: strings.Repeat("x,y\n", 5000)}})
	cut := data[:len(data)-8] // drop the gzip trailer
	tg, err := OpenTarGz(bytes.NewReader(cut), "", Limits{})
	if err != nil {
		return // erroring at open is also acceptable
	}
	defer tg.Close()
	for tg.Next() {
		io.Copy(io.Discard, tg.Reader())
	}
	if tg.Err() == nil {
		t.Fatal("truncated archive reported as a clean, complete run")
	}
}

// TestTarGzTotalAccountingWithoutReading ensures a caller that skips member
// content (never reads it) cannot bypass the total-decompressed guard: the
// iterator drains skipped bytes through the accounting reader.
func TestTarGzTotalAccountingWithoutReading(t *testing.T) {
	data := buildTarGz(t, []member{
		{name: "a.csv", body: strings.Repeat("x", 200)},
		{name: "b.csv", body: strings.Repeat("y", 200)},
	})
	tg, err := OpenTarGz(bytes.NewReader(data), "", Limits{MaxTotalBytes: 250})
	if err != nil {
		t.Fatal(err)
	}
	defer tg.Close()
	n := 0
	for tg.Next() {
		n++ // never read the member
	}
	var le *LimitError
	if !errors.As(tg.Err(), &le) {
		t.Fatalf("want LimitError from drained accounting, got %v (members seen %d)", tg.Err(), n)
	}
}

func TestTarGzCorrupt(t *testing.T) {
	if _, err := OpenTarGz(strings.NewReader("not gzip at all"), "", Limits{}); err == nil {
		t.Fatal("corrupt gzip: expected error")
	}
	// valid gzip, corrupt tar
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	gz.Write([]byte("this is not a tar archive, definitely not 512-byte aligned"))
	gz.Close()
	tg, err := OpenTarGz(bytes.NewReader(buf.Bytes()), "", Limits{})
	if err != nil {
		return // acceptable: error at open
	}
	for tg.Next() {
	}
	if tg.Err() == nil {
		t.Fatal("corrupt tar: expected error from iteration")
	}
}

func TestTarGzBadGlob(t *testing.T) {
	if _, err := OpenTarGz(bytes.NewReader(nil), "[", Limits{}); err == nil {
		t.Fatal("invalid glob must error at open")
	}
}
