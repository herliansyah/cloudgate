package auth

import (
	"testing"
)

func TestHashAndVerifyMasterPassword(t *testing.T) {
	password := "supersecret123"

	hash, err := HashMasterPassword(password)
	if err != nil {
		t.Fatalf("unexpected error hashing password: %v", err)
	}

	if !VerifyMasterPassword(hash, password) {
		t.Errorf("expected password verification to succeed")
	}

	if VerifyMasterPassword(hash, "wrongpassword") {
		t.Errorf("expected wrong password verification to fail")
	}

	// Short password
	if _, err := HashMasterPassword("12"); err == nil {
		t.Errorf("expected error for password shorter than 4 characters")
	}
}

func TestGenerateSessionToken(t *testing.T) {
	token1, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("unexpected error generating token: %v", err)
	}
	if len(token1) != 64 { // 32 bytes = 64 hex characters
		t.Errorf("expected token length 64, got %d", len(token1))
	}

	token2, err := GenerateSessionToken()
	if err != nil {
		t.Fatalf("unexpected error generating token 2: %v", err)
	}
	if token1 == token2 {
		t.Errorf("expected generated tokens to be unique")
	}
}
