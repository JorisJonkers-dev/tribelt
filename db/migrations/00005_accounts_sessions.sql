-- Local Stats Viewer accounts and server-side sessions (ADR-0006). An account is created on the first
-- auth-api sign-in and keyed by issuer + sub; auth-api stays the only way to sign in. A session holds the
-- sealed refresh token tribelt uses to re-read the account's roles from auth-api, so a role change there
-- reaches tribelt within seconds and signing out here ends only the tribelt session.

-- +goose Up
SET lock_timeout = '5s';
SET statement_timeout = '60s';

CREATE TABLE IF NOT EXISTS accounts (
    id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    issuer text NOT NULL,
    sub text NOT NULL,
    name text NOT NULL DEFAULT '',
    email text NOT NULL DEFAULT '',
    roles text[] NOT NULL DEFAULT '{}',
    admin boolean NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz NOT NULL DEFAULT now(),
    roles_checked_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (issuer, sub)
);

CREATE TABLE IF NOT EXISTS sessions (
    id text PRIMARY KEY,
    account_id bigint NOT NULL REFERENCES accounts (id) ON DELETE CASCADE,
    refresh_token bytea NOT NULL,
    key_id text NOT NULL,
    user_agent text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    roles_checked_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL
);

-- squawk-ignore require-concurrent-index-creation -- table is created in this migration and is empty
CREATE INDEX IF NOT EXISTS sessions_account_idx ON sessions (account_id);

