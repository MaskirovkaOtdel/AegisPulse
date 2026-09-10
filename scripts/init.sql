-- AegisPulse PostgreSQL Schema & Initial Seed Data

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

-- Seed Default API Keys (Free, Pro, Enterprise, Compromised)
INSERT INTO api_keys (id, key_hash, encrypted_key, tier, is_active, rate_frozen)
VALUES 
    ('key_free_01', encode(sha256('aegis_live_free_key_1001'::bytea), 'hex'), 'enc_aegis_live_free_key_1001', 'free', true, false),
    ('key_pro_01', encode(sha256('aegis_live_pro_key_2002'::bytea), 'hex'), 'enc_aegis_live_pro_key_2002', 'pro', true, false),
    ('key_enterprise_01', encode(sha256('aegis_live_enterprise_key_3003'::bytea), 'hex'), 'enc_aegis_live_enterprise_key_3003', 'enterprise', true, false),
    ('key_compromised_99', encode(sha256('aegis_live_compromised_key_9999'::bytea), 'hex'), 'enc_aegis_live_compromised_key_9999', 'enterprise', true, false)
ON CONFLICT (id) DO NOTHING;

-- Seed Webhook Subscriptions
INSERT INTO webhook_subscriptions (id, api_key_id, url, sink_type, encrypted_secret, is_active)
VALUES
    ('sub_gen_01', 'key_pro_01', 'http://webhook-receiver:8083/webhook/generic', 'generic', 'secret_generic_sig', true),
    ('sub_disc_01', 'key_enterprise_01', 'http://webhook-receiver:8083/webhook/discord', 'discord', 'secret_discord_sig', true),
    ('sub_slack_01', 'key_enterprise_01', 'http://webhook-receiver:8083/webhook/slack', 'slack', 'secret_slack_sig', true)
ON CONFLICT (id) DO NOTHING;
