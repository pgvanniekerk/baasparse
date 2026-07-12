package fetcher

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"

	"github.com/pgvanniekerk/baasparse/internal/settings"
)

// knownHostsFile is the local trust-on-first-use host-key store, kept under the
// baasparse config directory. It records the first key seen for each host so a
// later man-in-the-middle presenting a different key is refused.
const knownHostsFile = "fetcher_known_hosts"

// hostKeyStore persists first-seen SFTP host keys. Because the store exposes no
// pipeline-update method for the fetcher to write the captured key back into the
// Source document, TOFU state is kept here: a local file when a home directory is
// available, and an in-memory map otherwise (e.g. a container with no home), which
// still gives within-process consistency.
type hostKeyStore struct {
	mu   sync.Mutex
	path string // "" => in-memory only
	mem  map[string]string
	log  *slog.Logger
}

func newHostKeyStore(log *slog.Logger) *hostKeyStore {
	s := &hostKeyStore{mem: map[string]string{}, log: log.With("component", "fetcher")}
	if dir, err := settings.Dir(); err == nil {
		s.path = filepath.Join(dir, knownHostsFile)
	} else {
		s.log.Warn("no home directory for host-key store; using in-memory TOFU", "function", "newHostKeyStore", "err", err.Error())
	}
	return s
}

// lookup returns the remembered authorized-key line for hostport, if any.
func (s *hostKeyStore) lookup(hostport string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.mem[hostport]; ok {
		return v, true
	}
	if s.path == "" {
		return "", false
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		host, key, ok := strings.Cut(line, " ")
		if !ok || host != hostport {
			continue
		}
		key = strings.TrimSpace(key)
		s.mem[hostport] = key
		return key, true
	}
	return "", false
}

// remember records the first-seen key for hostport (memory + file when available).
func (s *hostKeyStore) remember(hostport, authorizedKey string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mem[hostport] = authorizedKey
	if s.path == "" {
		return
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		s.log.Warn("persist host key: mkdir failed", "function", "remember", "err", err.Error())
		return
	}
	line := hostport + " " + authorizedKey + "\n"
	fh, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		s.log.Warn("persist host key: open failed", "function", "remember", "err", err.Error())
		return
	}
	defer fh.Close()
	if _, err := fh.WriteString(line); err != nil {
		s.log.Warn("persist host key: write failed", "function", "remember", "err", err.Error())
	}
}

// hostKeyVerifier is an ssh host-key callback source: it pins to a configured key
// when present, otherwise trusts-on-first-use via the local store.
type hostKeyVerifier struct {
	pinned   string // configured Source.Remote.HostKey (authorized-key form), if any
	store    *hostKeyStore
	log      *slog.Logger
	pipeline string
}

// callback implements ssh.HostKeyCallback.
func (v hostKeyVerifier) callback(hostname string, remote net.Addr, key ssh.PublicKey) error {
	presented := marshalKey(key)
	hostport := hostname // ssh passes host:port

	if v.pinned != "" {
		want, err := normalizeKey(v.pinned)
		if err != nil {
			return fmt.Errorf("sftp: bad configured host key: %w", err)
		}
		if want != presented {
			return fmt.Errorf("sftp: host key mismatch for %s (pinned key does not match server)", hostport)
		}
		return nil
	}

	if remembered, ok := v.store.lookup(hostport); ok {
		if remembered != presented {
			return fmt.Errorf("sftp: host key mismatch for %s (known key changed — possible MITM)", hostport)
		}
		return nil
	}
	// Trust on first use.
	v.store.remember(hostport, presented)
	v.log.Info("trusted new sftp host key (TOFU)", "function", "hostKeyCallback", "pipeline", v.pipeline, "host", hostport)
	return nil
}

// marshalKey renders a public key as a stable "algo base64" line.
func marshalKey(key ssh.PublicKey) string {
	return key.Type() + " " + base64.StdEncoding.EncodeToString(key.Marshal())
}

// normalizeKey parses a configured host-key string (either an authorized_keys /
// known_hosts line "algo base64 [comment]" or our "algo base64" form) into the
// canonical "algo base64" comparison form.
func normalizeKey(s string) (string, error) {
	s = strings.TrimSpace(s)
	pk, _, _, _, err := ssh.ParseAuthorizedKey([]byte(s))
	if err == nil {
		return marshalKey(pk), nil
	}
	// Tolerate a bare "algo base64" pair that ParseAuthorizedKey rejected.
	fields := strings.Fields(s)
	if len(fields) >= 2 {
		if raw, derr := base64.StdEncoding.DecodeString(fields[1]); derr == nil {
			if pk, perr := ssh.ParsePublicKey(raw); perr == nil {
				return marshalKey(pk), nil
			}
			return fields[0] + " " + base64.StdEncoding.EncodeToString(bytes.TrimSpace(raw)), nil
		}
	}
	return "", fmt.Errorf("unrecognized host key format")
}
