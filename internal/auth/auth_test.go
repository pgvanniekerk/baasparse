package auth

import "testing"

func TestPasswordRoundTrip(t *testing.T) {
	h, err := HashPassword("s3cret-pw!")
	if err != nil {
		t.Fatal(err)
	}
	if !VerifyPassword("s3cret-pw!", h) {
		t.Fatal("correct password did not verify")
	}
	if VerifyPassword("wrong", h) {
		t.Fatal("wrong password verified")
	}
	if VerifyPassword("anything", "PLACEHOLDER:reset-on-first-run") {
		t.Fatal("placeholder must never verify")
	}
	if VerifyPassword("x", "garbage$encoding") {
		t.Fatal("malformed encoding must not verify")
	}
}

func TestTokenHashStable(t *testing.T) {
	tok, err := GenerateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(HashToken(tok)) != 32 {
		t.Fatal("token hash should be 32 bytes (sha-256)")
	}
	if string(HashToken(tok)) != string(HashToken(tok)) {
		t.Fatal("token hash not stable")
	}
}
