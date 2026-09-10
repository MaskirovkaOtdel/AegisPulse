package storage

import (
	"context"
	"errors"
	"testing"

	"aegispulse/internal/crypto"
)

func TestPostgresRepo_KeysAndEnvelopeEncryption(t *testing.T) {
	ctx := context.Background()
	cipher, err := crypto.NewEnvelopeCipher("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatalf("cipher init error: %v", err)
	}

	repo, err := NewPostgresRepo(ctx, "", cipher, nil)
	if err != nil {
		t.Fatalf("repo init error: %v", err)
	}
	defer repo.Close()

	// 1. Retrieve valid key
	rawKey := "aegis_live_free_key_1001"
	rec, err := repo.GetAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("expected key to be found: %v", err)
	}
	if rec.Tier != "free" {
		t.Fatalf("expected free tier, got %s", rec.Tier)
	}
	if rec.ID != "key_free_01" {
		t.Fatalf("expected id key_free_01, got %s", rec.ID)
	}

	// 2. Verify envelope encryption: decrypt EncryptedKey
	decrypted, err := cipher.Decrypt(rec.EncryptedKey)
	if err != nil {
		t.Fatalf("failed to decrypt encrypted_key: %v", err)
	}
	if string(decrypted) != rawKey {
		t.Fatalf("expected decrypted key to match %q, got %q", rawKey, string(decrypted))
	}

	// 3. Nonexistent key
	_, err = repo.GetAPIKey(ctx, "nonexistent_key_9999")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound, got %v", err)
	}
}

func TestPostgresRepo_FreezeAPIKey(t *testing.T) {
	ctx := context.Background()
	cipher, _ := crypto.NewEnvelopeCipher("0123456789abcdef0123456789abcdef")
	repo, _ := NewPostgresRepo(ctx, "", cipher, nil)
	defer repo.Close()

	// 1. Freeze by ID
	err := repo.FreezeAPIKey(ctx, "key_compromised_99")
	if err != nil {
		t.Fatalf("expected freeze by id to succeed: %v", err)
	}

	// Verify key is now frozen
	_, err = repo.GetAPIKey(ctx, "aegis_live_compromised_key_9999")
	if !errors.Is(err, ErrKeyFrozen) {
		t.Fatalf("expected ErrKeyFrozen for compromised key, got %v", err)
	}

	// 2. Freeze by raw key
	rawKey := "aegis_live_pro_key_2002"
	err = repo.FreezeAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("expected freeze by raw key to succeed: %v", err)
	}

	_, err = repo.GetAPIKey(ctx, rawKey)
	if !errors.Is(err, ErrKeyFrozen) {
		t.Fatalf("expected ErrKeyFrozen for pro key, got %v", err)
	}

	// 3. Freeze nonexistent key
	err = repo.FreezeAPIKey(ctx, "completely_unknown_id")
	if !errors.Is(err, ErrKeyNotFound) {
		t.Fatalf("expected ErrKeyNotFound for unknown key, got %v", err)
	}
}

func TestPostgresRepo_AuditEventsAndWebhooks(t *testing.T) {
	ctx := context.Background()
	cipher, _ := crypto.NewEnvelopeCipher("0123456789abcdef0123456789abcdef")
	repo, _ := NewPostgresRepo(ctx, "", cipher, nil)
	defer repo.Close()

	// 1. Audit log
	err := repo.CreateAuditLog(ctx, "TEST_EVENT", `{"test":true}`, "sig123")
	if err != nil {
		t.Fatalf("CreateAuditLog failed: %v", err)
	}

	// 2. Record billing overage
	err = repo.RecordBillingOverage(ctx, "aegis_live_free_key_1001", "2026-09", 10, 0.05)
	if err != nil {
		t.Fatalf("RecordBillingOverage failed: %v", err)
	}

	// 3. Webhooks
	whs, err := repo.GetWebhooks(ctx)
	if err != nil {
		t.Fatalf("GetWebhooks failed: %v", err)
	}
	if len(whs) < 3 {
		t.Fatalf("expected at least 3 seeded webhooks, got %d", len(whs))
	}
}
