package fetcher

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/pgvanniekerk/baasparse/internal/store"
)

func testKey(t *testing.T) ssh.PublicKey {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	pk, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatalf("ssh pubkey: %v", err)
	}
	return pk
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestNormalizeKeyRoundTrip(t *testing.T) {
	pk := testKey(t)
	marshaled := marshalKey(pk)
	authLine := string(ssh.MarshalAuthorizedKey(pk)) // "algo base64\n" (+ maybe comment)

	got1, err := normalizeKey(marshaled)
	if err != nil {
		t.Fatalf("normalize marshaled: %v", err)
	}
	got2, err := normalizeKey(authLine + " someone@host")
	if err != nil {
		t.Fatalf("normalize authorized line: %v", err)
	}
	if got1 != marshaled || got2 != marshaled {
		t.Fatalf("normalize mismatch: got1=%q got2=%q want=%q", got1, got2, marshaled)
	}
}

func TestHostKeyTOFU(t *testing.T) {
	hks := &hostKeyStore{mem: map[string]string{}, log: quietLogger()} // in-memory only (path == "")
	v := hostKeyVerifier{store: hks, log: quietLogger(), pipeline: "p"}
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 22}

	first := testKey(t)
	if err := v.callback("host:22", addr, first); err != nil {
		t.Fatalf("first-use should be trusted: %v", err)
	}
	// Same key again -> ok.
	if err := v.callback("host:22", addr, first); err != nil {
		t.Fatalf("known key should verify: %v", err)
	}
	// Different key for same host -> refused (MITM).
	if err := v.callback("host:22", addr, testKey(t)); err == nil {
		t.Fatalf("changed host key must be refused")
	}
	// Different host -> new TOFU accept.
	if err := v.callback("other:22", addr, testKey(t)); err != nil {
		t.Fatalf("new host should be trusted: %v", err)
	}
}

func TestHostKeyPinned(t *testing.T) {
	hks := &hostKeyStore{mem: map[string]string{}, log: quietLogger()}
	pinned := testKey(t)
	v := hostKeyVerifier{pinned: marshalKey(pinned), store: hks, log: quietLogger(), pipeline: "p"}
	addr := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 22}

	if err := v.callback("host:22", addr, pinned); err != nil {
		t.Fatalf("matching pinned key should verify: %v", err)
	}
	if err := v.callback("host:22", addr, testKey(t)); err == nil {
		t.Fatalf("non-matching key against a pin must be refused")
	}
}

func TestReschedule(t *testing.T) {
	f := New(store.NewMem(), nil, quietLogger())
	p := store.Pipeline{ID: 7}
	p.Source.PollSeconds = 10

	// success -> due ~poll from now, fails reset
	f.fails[7] = 3
	f.reschedule(p, nil)
	if f.fails[7] != 0 {
		t.Fatalf("success should reset fails, got %d", f.fails[7])
	}
	if d := time.Until(f.nextDue[7]); d < 5*time.Second || d > 11*time.Second {
		t.Fatalf("success next-due out of range: %v", d)
	}

	// failure -> backoff grows on consecutive failures
	f.reschedule(p, io.EOF)
	d1 := time.Until(f.nextDue[7])
	f.reschedule(p, io.EOF)
	d2 := time.Until(f.nextDue[7])
	if d2 <= d1 {
		t.Fatalf("consecutive failures should back off further: d1=%v d2=%v", d1, d2)
	}
}

func TestDefaultPollUsedWhenUnset(t *testing.T) {
	f := New(store.NewMem(), nil, quietLogger())
	p := store.Pipeline{ID: 3} // PollSeconds == 0
	f.reschedule(p, nil)
	if d := time.Until(f.nextDue[3]); d < (defaultPollSeconds-2)*time.Second {
		t.Fatalf("default poll not applied: %v", d)
	}
}
