package storage

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"aegispulse/internal/crypto"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrKeyNotFound = errors.New("api key not found")
	ErrKeyFrozen   = errors.New("api key has been frozen by security kill-switch")
	ErrKeyInactive = errors.New("api key is inactive or revoked")
)

type APIKeyRecord struct {
	ID           string
	KeyHash      string
	EncryptedKey string
	Tier         string
	IsActive     bool
	RateFrozen   bool
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type WebhookRecord struct {
	ID              string
	APIKeyID        string
	URL             string
	SinkType        string // "generic", "discord", "slack"
	EncryptedSecret string
	IsActive        bool
	CreatedAt       time.Time
}

type AuditEventRecord struct {
	ID        string
	EventType string
	Payload   string
	Signature string
	CreatedAt time.Time
}

type Repository interface {
	GetAPIKey(ctx context.Context, rawKey string) (*APIKeyRecord, error)
	FreezeAPIKey(ctx context.Context, keyIDOrRawKey string) error
	CreateAuditLog(ctx context.Context, eventType, payload, signature string) error
	RecordBillingOverage(ctx context.Context, apiKey, month string, units int64, cost float64) error
	GetWebhooks(ctx context.Context) ([]WebhookRecord, error)
	Close()
}

type PostgresRepo struct {
	pool        *pgxpool.Pool
	cipher      *crypto.EnvelopeCipher
	redisClient *RedisClient

	// In-memory shadow map for microsecond lookup cache and offline fallback
	mu       sync.RWMutex
	memKeys  map[string]*APIKeyRecord
	webhooks []WebhookRecord
}

func NewPostgresRepo(ctx context.Context, dsn string, cipher *crypto.EnvelopeCipher, redisClient *RedisClient) (*PostgresRepo, error) {
	repo := &PostgresRepo{
		cipher:      cipher,
		redisClient: redisClient,
		memKeys:     make(map[string]*APIKeyRecord),
		webhooks:    make([]WebhookRecord, 0),
	}

	// Pre-seed standard keys in memory for fallback and rapid testing
	repo.seedDefaultKeys()

	if dsn != "" {
		cfg, err := pgxpool.ParseConfig(dsn)
		if err == nil {
			cfg.MaxConns = 20
			cfg.MinConns = 2
			cfg.MaxConnLifetime = 30 * time.Minute
			pool, err := pgxpool.NewWithConfig(ctx, cfg)
			if err == nil {
				if err := pool.Ping(ctx); err == nil {
					repo.pool = pool
					log.Printf("[STORAGE] PostgreSQL connected successfully.")
					_ = repo.RunMigrations(ctx)
					_ = repo.syncFromDB(ctx)
					return repo, nil
				}
				log.Printf("[STORAGE] PostgreSQL ping failed: %v. Running with in-memory fallback.", err)
			} else {
				log.Printf("[STORAGE] PostgreSQL pool init failed: %v. Running with in-memory fallback.", err)
			}
		}
	}

	log.Printf("[STORAGE] Running PostgreSQL repository in standalone/in-memory fallback mode.")
	return repo, nil
}

func (r *PostgresRepo) seedDefaultKeys() {
	keys := []struct {
		id     string
		rawKey string
		tier   string
	}{
		{"key_free_01", "aegis_live_free_key_1001", "free"},
		{"key_pro_01", "aegis_live_pro_key_2002", "pro"},
		{"key_enterprise_01", "aegis_live_enterprise_key_3003", "enterprise"},
		{"key_compromised_99", "aegis_live_compromised_key_9999", "enterprise"},
	}

	for _, k := range keys {
		hash := crypto.HashKey(k.rawKey)
		enc, _ := r.cipher.Encrypt([]byte(k.rawKey))
		r.memKeys[hash] = &APIKeyRecord{
			ID:           k.id,
			KeyHash:      hash,
			EncryptedKey: enc,
			Tier:         k.tier,
			IsActive:     true,
			RateFrozen:   false,
			CreatedAt:    time.Now(),
			UpdatedAt:    time.Now(),
		}
	}

	r.webhooks = []WebhookRecord{
		{
			ID:              "sub_gen_01",
			APIKeyID:        "key_pro_01",
			URL:             "http://webhook-receiver:8083/webhook/generic",
			SinkType:        "generic",
			EncryptedSecret: "secret_generic_sig",
			IsActive:        true,
			CreatedAt:       time.Now(),
		},
		{
			ID:              "sub_disc_01",
			APIKeyID:        "key_enterprise_01",
			URL:             "http://webhook-receiver:8083/webhook/discord",
			SinkType:        "discord",
			EncryptedSecret: "secret_discord_sig",
			IsActive:        true,
			CreatedAt:       time.Now(),
		},
		{
			ID:              "sub_slack_01",
			APIKeyID:        "key_enterprise_01",
			URL:             "http://webhook-receiver:8083/webhook/slack",
			SinkType:        "slack",
			EncryptedSecret: "secret_slack_sig",
			IsActive:        true,
			CreatedAt:       time.Now(),
		},
	}
}

func (r *PostgresRepo) syncFromDB(ctx context.Context) error {
	if r.pool == nil {
		return nil
	}

	rows, err := r.pool.Query(ctx, `SELECT id, key_hash, encrypted_key, tier, is_active, rate_frozen, created_at, updated_at FROM api_keys`)
	if err != nil {
		return err
	}
	defer rows.Close()

	r.mu.Lock()
	defer r.mu.Unlock()

	for rows.Next() {
		var rec APIKeyRecord
		if err := rows.Scan(&rec.ID, &rec.KeyHash, &rec.EncryptedKey, &rec.Tier, &rec.IsActive, &rec.RateFrozen, &rec.CreatedAt, &rec.UpdatedAt); err == nil {
			r.memKeys[rec.KeyHash] = &rec
		}
	}

	// Sync webhook subscriptions
	wRows, err := r.pool.Query(ctx, `SELECT id, api_key_id, url, sink_type, encrypted_secret, is_active, created_at FROM webhook_subscriptions`)
	if err == nil {
		defer wRows.Close()
		var whs []WebhookRecord
		for wRows.Next() {
			var wRec WebhookRecord
			if err := wRows.Scan(&wRec.ID, &wRec.APIKeyID, &wRec.URL, &wRec.SinkType, &wRec.EncryptedSecret, &wRec.IsActive, &wRec.CreatedAt); err == nil {
				whs = append(whs, wRec)
			}
		}
		if len(whs) > 0 {
			r.webhooks = whs
		}
	}

	return nil
}

func (r *PostgresRepo) GetAPIKey(ctx context.Context, rawKey string) (*APIKeyRecord, error) {
	keyHash := crypto.HashKey(rawKey)

	// Check if key is frozen in Redis
	if r.redisClient != nil && (r.redisClient.IsKeyFrozen(ctx, rawKey) || r.redisClient.IsKeyFrozen(ctx, keyHash)) {
		return nil, ErrKeyFrozen
	}

	// Check fast in-memory map first
	r.mu.RLock()
	rec, ok := r.memKeys[keyHash]
	r.mu.RUnlock()

	if ok {
		if rec.RateFrozen {
			return nil, ErrKeyFrozen
		}
		if !rec.IsActive {
			return nil, ErrKeyInactive
		}
		return rec, nil
	}

	if r.pool != nil {
		var dbRec APIKeyRecord
		err := r.pool.QueryRow(ctx,
			`SELECT id, key_hash, encrypted_key, tier, is_active, rate_frozen, created_at, updated_at 
			 FROM api_keys WHERE key_hash = $1`, keyHash).
			Scan(&dbRec.ID, &dbRec.KeyHash, &dbRec.EncryptedKey, &dbRec.Tier, &dbRec.IsActive, &dbRec.RateFrozen, &dbRec.CreatedAt, &dbRec.UpdatedAt)
		if err == nil {
			r.mu.Lock()
			r.memKeys[keyHash] = &dbRec
			r.mu.Unlock()

			if dbRec.RateFrozen {
				return nil, ErrKeyFrozen
			}
			if !dbRec.IsActive {
				return nil, ErrKeyInactive
			}
			return &dbRec, nil
		}
	}

	return nil, ErrKeyNotFound
}

func (r *PostgresRepo) FreezeAPIKey(ctx context.Context, keyIDOrRawKey string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	targetHash := crypto.HashKey(keyIDOrRawKey)

	// Freeze in Redis across distributed gateways
	if r.redisClient != nil {
		_ = r.redisClient.FreezeKey(ctx, keyIDOrRawKey)
		_ = r.redisClient.FreezeKey(ctx, targetHash)
	}

	found := false
	for h, rec := range r.memKeys {
		if h == targetHash || rec.ID == keyIDOrRawKey || rec.EncryptedKey == keyIDOrRawKey {
			rec.RateFrozen = true
			rec.IsActive = false
			rec.UpdatedAt = time.Now()
			found = true
			if r.redisClient != nil {
				_ = r.redisClient.FreezeKey(ctx, rec.ID)
				_ = r.redisClient.FreezeKey(ctx, rec.KeyHash)
			}
			break
		}
	}

	if r.pool != nil {
		tag, err := r.pool.Exec(ctx,
			`UPDATE api_keys SET rate_frozen = true, is_active = false, updated_at = NOW() 
			 WHERE id = $1 OR key_hash = $2`, keyIDOrRawKey, targetHash)
		if err != nil {
			log.Printf("[STORAGE] DB update error during freeze: %v", err)
		} else if tag.RowsAffected() > 0 {
			found = true
		}
	}

	if !found {
		return ErrKeyNotFound
	}
	return nil
}

func (r *PostgresRepo) CreateAuditLog(ctx context.Context, eventType, payload, signature string) error {
	rec := AuditEventRecord{
		ID:        fmt.Sprintf("audit_%d", time.Now().UnixNano()),
		EventType: eventType,
		Payload:   payload,
		Signature: signature,
		CreatedAt: time.Now(),
	}

	if r.pool != nil {
		_, err := r.pool.Exec(ctx,
			`INSERT INTO audit_events (id, event_type, payload, signature, created_at) 
			 VALUES ($1, $2, $3, $4, $5)`,
			rec.ID, rec.EventType, rec.Payload, rec.Signature, rec.CreatedAt)
		if err != nil {
			log.Printf("[STORAGE] Audit log insert error: %v", err)
		}
	}

	log.Printf("[AUDIT] Event: %s | Sig: %s | Payload: %s", eventType, signature, payload)
	return nil
}

func (r *PostgresRepo) RecordBillingOverage(ctx context.Context, apiKey, month string, units int64, cost float64) error {
	if r.pool == nil {
		return nil
	}

	_, err := r.pool.Exec(ctx,
		`INSERT INTO billing_overages (api_key, month, units, cost, updated_at)
		 VALUES ($1, $2, $3, $4, NOW())
		 ON CONFLICT (api_key, month)
		 DO UPDATE SET units = EXCLUDED.units, cost = EXCLUDED.cost, updated_at = NOW()`,
		apiKey, month, units, cost)
	if err != nil {
		log.Printf("[STORAGE] Failed to record billing overage in Postgres: %v", err)
		return err
	}
	return nil
}

func (r *PostgresRepo) GetWebhooks(ctx context.Context) ([]WebhookRecord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]WebhookRecord(nil), r.webhooks...), nil
}

func (r *PostgresRepo) Close() {
	if r.pool != nil {
		r.pool.Close()
	}
}
