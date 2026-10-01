-- Integrations an admin connects in /stats (ADR-0005). Credentials are sealed with AES-256-GCM under a key
-- derived from SESSION_KEY; key_id names that key so a rotation is detectable. A row without a credential
-- carries the sync status of a Vault-managed source. integration_events is the audit log, cloudflare_daily
-- the edge request counts per UTC day.

-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '60s';

CREATE TABLE IF NOT EXISTS integrations (
    kind text PRIMARY KEY CHECK (kind IN ('google', 'bing', 'indexnow', 'cloudflare')),
    credential bytea,
    key_id text,
    fingerprint text,
    account text NOT NULL DEFAULT '',
    target text NOT NULL DEFAULT '',
    enabled boolean NOT NULL DEFAULT true,
    meta jsonb NOT NULL DEFAULT '{}',
    created_by_sub text,
    created_by_name text,
    created_at timestamptz,
    updated_by_sub text,
    updated_by_name text,
    updated_at timestamptz,
    last_sync_at timestamptz,
    last_success_at timestamptz,
    last_error text,
    last_error_code text,
    next_sync_at timestamptz,
    running_since timestamptz,
    backfilled_at timestamptz,
    CONSTRAINT integrations_sealed_check CHECK (
        (credential IS NULL) = (key_id IS NULL) AND (credential IS NULL) = (fingerprint IS NULL)
    )
);

CREATE TABLE IF NOT EXISTS integration_events (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind text NOT NULL CHECK (kind IN ('google', 'bing', 'indexnow', 'cloudflare')),
    action text NOT NULL CHECK (
        action IN ('connect', 'replace', 'select', 'remove', 'enable', 'disable', 'test', 'sync', 'notify')
    ),
    ok boolean NOT NULL,
    code text NOT NULL DEFAULT '',
    detail text NOT NULL DEFAULT '',
    count bigint,
    http_status bigint,
    actor_sub text,
    actor_name text NOT NULL DEFAULT 'scheduler',
    at timestamptz NOT NULL DEFAULT now()
);

-- squawk-ignore require-concurrent-index-creation -- table is created in this migration and is empty
CREATE INDEX IF NOT EXISTS integration_events_kind_at_idx ON integration_events (kind, at DESC);

CREATE TABLE IF NOT EXISTS cloudflare_daily (
    host text NOT NULL,
    day date NOT NULL,
    dimension text NOT NULL CHECK (dimension IN ('total', 'user_agent', 'path', 'status', 'bot_category')),
    value text NOT NULL,
    requests bigint NOT NULL,
    imported_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (host, day, dimension, value)
);
