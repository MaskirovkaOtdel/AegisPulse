package crypto

import (
	"strings"
	"testing"
	"time"
)

func TestEnvelopeCipher_EncryptDecrypt(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cipher, err := NewEnvelopeCipher(key)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}

	plaintext := "sk_live_enterprise_super_secret_token_12345"
	encrypted, err := cipher.Encrypt([]byte(plaintext))
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	if encrypted == plaintext {
		t.Fatalf("ciphertext must not match plaintext")
	}

	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("decryption failed: %v", err)
	}

	if string(decrypted) != plaintext {
		t.Fatalf("decrypted text %q does not match original %q", string(decrypted), plaintext)
	}
}

func TestEnvelopeCipher_TamperResistance(t *testing.T) {
	key := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	cipher, err := NewEnvelopeCipher(key)
	if err != nil {
		t.Fatalf("failed to create cipher: %v", err)
	}

	plaintext := "sensitive-data"
	encrypted, err := cipher.Encrypt([]byte(plaintext))
	if err != nil {
		t.Fatalf("encryption failed: %v", err)
	}

	// Corrupt ciphertext
	corrupted := encrypted[:len(encrypted)-4] + "AAAA"
	_, err = cipher.Decrypt(corrupted)
	if err == nil {
		t.Fatalf("expected decryption error on corrupted ciphertext, got nil")
	}
}

func TestHMAC_SignAndVerify(t *testing.T) {
	secret := []byte("webhook_secret_key_999")
	payload := []byte(`{"event":"test_event","status":"active"}`)
	now := time.Now().Unix()

	sigHeader := GenerateSignature(payload, secret, now)
	if !strings.HasPrefix(sigHeader, "t=") || !strings.Contains(sigHeader, ",v1=") {
		t.Fatalf("invalid signature format: %s", sigHeader)
	}

	// Successful verification within window
	valid, err := VerifySignature(payload, sigHeader, secret, 60, now)
	if err != nil || !valid {
		t.Fatalf("signature verification failed: %v", err)
	}

	// Tampered payload fails
	tampered := []byte(`{"event":"tampered"}`)
	valid, err = VerifySignature(tampered, sigHeader, secret, 60, now)
	if valid || err == nil {
		t.Fatalf("expected failure for tampered payload, got valid=%v", valid)
	}

	// Expired timestamp fails anti-replay
	futureNow := now + 120 // 120 seconds later, window is 60s
	valid, err = VerifySignature(payload, sigHeader, secret, 60, futureNow)
	if valid || err == nil {
		t.Fatalf("expected expiration error, got valid=%v, err=%v", valid, err)
	}

	// Future clock drift beyond 10s fails
	pastNow := now - 20 // payload was generated 20 seconds in future
	valid, err = VerifySignature(payload, sigHeader, secret, 60, pastNow)
	if valid || err == nil {
		t.Fatalf("expected clock drift error, got valid=%v, err=%v", valid, err)
	}
}
