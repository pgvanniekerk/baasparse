package secret

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	SetKeyForTest(key)
	defer SetKeyForTest(nil)

	for _, plain := range []string{"", "hunter2", "s3-secret/with+base64=chars", "a very long private key line\nwith newlines"} {
		ct, err := Encrypt(plain)
		if err != nil {
			t.Fatalf("encrypt %q: %v", plain, err)
		}
		if plain == "" {
			if ct != "" {
				t.Fatalf("empty plaintext must encrypt to empty, got %q", ct)
			}
			continue
		}
		if ct == plain {
			t.Fatalf("ciphertext equals plaintext for %q", plain)
		}
		got, err := Decrypt(ct)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if got != plain {
			t.Fatalf("round trip mismatch: got %q want %q", got, plain)
		}
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	k1 := make([]byte, 32)
	SetKeyForTest(k1)
	ct, err := Encrypt("secret")
	if err != nil {
		t.Fatal(err)
	}
	k2 := make([]byte, 32)
	k2[0] = 1
	SetKeyForTest(k2)
	defer SetKeyForTest(nil)
	if _, err := Decrypt(ct); err == nil {
		t.Fatal("expected decrypt with wrong key to fail")
	}
}
