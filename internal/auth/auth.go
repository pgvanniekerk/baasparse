// Package auth provides password hashing and session-token handling for the
// built-in user system (BR-USR-002/004/007). Passwords are stored salted and
// hashed (BR-USR-004); session tokens are random and only their SHA-256 hash is
// persisted (BR-USR-007, SES_TOKEN_HASH).
//
// The alpha uses PBKDF2-HMAC-SHA256 (stdlib only, no external dependency). The
// TS specifies argon2id (BR-USR-004); this is a drop-in-replaceable stand-in —
// the PHC-style encoding records the algorithm so a future argon2id hash
// verifies alongside.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash"
	"strconv"
	"strings"
)

const (
	pbkdf2Iters = 210000
	saltLen     = 16
	keyLen      = 32
)

// HashPassword returns a PHC-style encoding: pbkdf2$sha256$<iter>$<salt>$<hash>.
func HashPassword(password string) (string, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	dk := pbkdf2SHA256([]byte(password), salt, pbkdf2Iters, keyLen)
	b64 := base64.RawStdEncoding.EncodeToString
	return fmt.Sprintf("pbkdf2$sha256$%d$%s$%s", pbkdf2Iters, b64(salt), b64(dk)), nil
}

// VerifyPassword reports whether password matches the stored PHC encoding. It
// returns false for placeholders or malformed encodings rather than erroring.
func VerifyPassword(password, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 5 || parts[0] != "pbkdf2" || parts[1] != "sha256" {
		return false
	}
	iters, err := strconv.Atoi(parts[2])
	if err != nil || iters <= 0 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false
	}
	got := pbkdf2SHA256([]byte(password), salt, iters, len(want))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// GenerateSessionToken returns a URL-safe random token (the cookie value).
func GenerateSessionToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// HashToken returns the SHA-256 of a token — what gets persisted (never the raw token).
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// pbkdf2SHA256 is a minimal PBKDF2-HMAC-SHA256 (RFC 2898), stdlib-only.
func pbkdf2SHA256(password, salt []byte, iter, keyLen int) []byte {
	h := func() hash.Hash { return sha256.New() }
	hLen := sha256.Size
	numBlocks := (keyLen + hLen - 1) / hLen
	var dk []byte
	block := make([]byte, 4)
	for i := 1; i <= numBlocks; i++ {
		binary.BigEndian.PutUint32(block, uint32(i))
		u := hmacSum(h, password, append(append([]byte{}, salt...), block...))
		t := append([]byte{}, u...)
		for j := 1; j < iter; j++ {
			u = hmacSum(h, password, u)
			for k := range t {
				t[k] ^= u[k]
			}
		}
		dk = append(dk, t...)
	}
	return dk[:keyLen]
}

func hmacSum(h func() hash.Hash, key, data []byte) []byte {
	mac := hmac.New(h, key)
	mac.Write(data)
	return mac.Sum(nil)
}
