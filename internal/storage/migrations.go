package storage

import (
	"context"
	"fmt"
	"log"
)

const SchemaSQL = `
CREATE TABLE IF NOT EXISTS api_keys (
    id VARCHAR(64) PRIMARY KEY,
    key_hash VARCHAR(64) NOT NULL UNIQUE,
    encrypted_key TEXT NOT NULL,
    tier VARCHAR(32) NOT NULL DEFAULT 'free',
    is_active BOOLEAN NOT NULL DEFAULT true,
    rate_frozen BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash);

CREATE TABLE IF NOT EXISTS webhook_subscriptions (
    id VARCHAR(64) PRIMARY KEY,
    api_key_id VARCHAR(64) REFERENCES api_keys(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    sink_type VARCHAR(32) NOT NULL DEFAULT 'generic',
    encrypted_secret TEXT NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_events (
    id VARCHAR(64) PRIMARY KEY,
    event_type VARCHAR(64) NOT NULL,
    payload TEXT NOT NULL,
    signature TEXT NOT NULL,
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS billing_overages (
    id SERIAL PRIMARY KEY,
    api_key VARCHAR(128) NOT NULL,
    month VARCHAR(7) NOT NULL,
    units BIGINT NOT NULL DEFAULT 0,
    cost NUMERIC(12, 4) NOT NULL DEFAULT 0.0000,
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    UNIQUE (api_key, month)
);
`

func (r *PostgresRepo) RunMigrations(ctx context.Context) error {
	if r.pool == nil {
		return nil
	}

	_, err := r.pool.Exec(ctx, SchemaSQL)
	if err != nil {
		return fmt.Errorf("failed running database migrations: %w", err)
	}

	log.Printf("[STORAGE] Database schema migrations executed successfully.")
	return nil
}
