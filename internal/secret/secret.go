// Package secret provides symmetric encryption for the credential fields stored
// inside a pipeline's configuration document (S3 secret keys, SFTP passwords and
// private keys). Ciphertext is AES-256-GCM, base64-encoded, so it is safe to keep
// inside the PLV_STAGE_GRAPH JSONB document alongside the rest of the pipeline
// config (HARD CONSTRAINT: no new SQL columns).
//
// Key resolution (in precedence order):
//   - env BAASPARSE_SECRET_KEY — a base64/hex 32-byte key, or any passphrase
//     (SHA-256 derived to 32 bytes). Authoritative in containers.
//   - ~/.baasparse/secret.key — a persisted, auto-generated 32-byte key (base64).
//     Generated on first use when the env var is unset and a home directory
//     exists. In a container with no home and no env var, encryption of a
//     non-empty secret fails loudly rather than silently using a weak key.
//
// Empty strings pass through unchanged (an unset secret stays unset), so callers
// can encrypt/decrypt optional fields unconditionally.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/pgvanniekerk/baasparse/internal/settings"
)

// EnvKey is the environment variable holding the master key/passphrase.
const EnvKey = "BAASPARSE_SECRET_KEY"

// keyFileName is the persisted key under the baasparse config directory.
const keyFileName = "secret.key"

var (
	once      sync.Once
	cachedKey []byte
	cachedErr error

	// override lets tests inject a fixed key without touching env/home.
	overrideMu  sync.RWMutex
	overrideKey []byte
)

// SetKeyForTest overrides the resolved key process-wide (test helper). Passing a
// nil key clears the override. It also resets the one-time resolver so a
// subsequent real resolution can run.
func SetKeyForTest(key []byte) {
	overrideMu.Lock()
	overrideKey = key
	overrideMu.Unlock()
	once = sync.Once{}
}

// resolveKey returns the 32-byte AES key, resolving and caching it once.
func resolveKey() ([]byte, error) {
	overrideMu.RLock()
	ov := overrideKey
	overrideMu.RUnlock()
	if ov != nil {
		if len(ov) != 32 {
			return nil, fmt.Errorf("secret: override key must be 32 bytes, got %d", len(ov))
		}
		return ov, nil
	}
	once.Do(func() { cachedKey, cachedErr = loadKey() })
	return cachedKey, cachedErr
}

func loadKey() ([]byte, error) {
	if v := os.Getenv(EnvKey); v != "" {
		return deriveKey(v), nil
	}
	// No env var — fall back to a persisted local key. Requires a home directory.
	dir, err := settings.Dir()
	if err != nil {
		return nil, fmt.Errorf("secret: no %s set and no home directory for a persisted key: %w", EnvKey, err)
	}
	path := filepath.Join(dir, keyFileName)
	if data, rerr := os.ReadFile(path); rerr == nil {
		key, derr := base64.StdEncoding.DecodeString(string(bytesTrim(data)))
		if derr != nil || len(key) != 32 {
			return nil, fmt.Errorf("secret: corrupt key file %s", path)
		}
		return key, nil
	} else if !os.IsNotExist(rerr) {
		return nil, fmt.Errorf("secret: read key file %s: %w", path, rerr)
	}
	// Generate and persist a fresh key.
	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("secret: generate key: %w", err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secret: create key dir: %w", err)
	}
	enc := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(path, []byte(enc), 0o600); err != nil {
		return nil, fmt.Errorf("secret: persist key %s: %w", path, err)
	}
	return key, nil
}

// deriveKey turns the env value into a 32-byte key: a base64 or hex encoding of
// exactly 32 bytes is used verbatim; anything else is SHA-256 hashed.
func deriveKey(v string) []byte {
	if b, err := base64.StdEncoding.DecodeString(v); err == nil && len(b) == 32 {
		return b
	}
	if b, err := hex.DecodeString(v); err == nil && len(b) == 32 {
		return b
	}
	sum := sha256.Sum256([]byte(v))
	return sum[:]
}

// Encrypt returns base64(nonce||AES-GCM ciphertext) for plain. An empty string
// returns an empty string (an unset secret stays unset).
func Encrypt(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	key, err := resolveKey()
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secret: nonce: %w", err)
	}
	ct := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

// Decrypt reverses Encrypt. An empty string returns an empty string.
func Decrypt(cipherText string) (string, error) {
	if cipherText == "" {
		return "", nil
	}
	key, err := resolveKey()
	if err != nil {
		return "", err
	}
	gcm, err := newGCM(key)
	if err != nil {
		return "", err
	}
	raw, err := base64.StdEncoding.DecodeString(cipherText)
	if err != nil {
		return "", fmt.Errorf("secret: decode ciphertext: %w", err)
	}
	ns := gcm.NonceSize()
	if len(raw) < ns {
		return "", errors.New("secret: ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("secret: decrypt: %w", err)
	}
	return string(plain), nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secret: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

// bytesTrim strips trailing whitespace/newlines from a key file's contents.
func bytesTrim(b []byte) []byte {
	i := len(b)
	for i > 0 {
		c := b[i-1]
		if c == '\n' || c == '\r' || c == ' ' || c == '\t' {
			i--
			continue
		}
		break
	}
	return b[:i]
}
