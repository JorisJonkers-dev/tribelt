-- The SQLite export reads these. Each one leaves out the secrets: sealed credentials, key ids,
-- fingerprints, session ids and refresh tokens; and the auth-api subjects and e-mail addresses
-- that would identify a person.

-- name: ExportIntegrations :many
SELECT kind, (credential IS NOT NULL)::boolean AS has_credential, account, target, enabled, meta,
       created_by_name, created_at, updated_by_name, updated_at, last_sync_at, last_success_at,
       last_error, last_error_code, next_sync_at, backfilled_at
FROM integrations ORDER BY kind;

-- name: ExportIntegrationEvents :many
SELECT id, kind, action, ok, code, detail, count, http_status, actor_name, at
FROM integration_events ORDER BY at, id;

-- name: ExportCloudflareDaily :many
SELECT host, day, dimension, value, requests, imported_at
FROM cloudflare_daily ORDER BY host, day, dimension, requests DESC, value;

-- name: ExportAccounts :many
SELECT id, name, roles, admin, created_at, last_login_at
FROM accounts ORDER BY id;
