-- name: ListIntegrations :many
SELECT * FROM integrations ORDER BY kind;

-- name: GetIntegration :one
SELECT * FROM integrations WHERE kind = @kind;

-- name: EnsureIntegration :exec
INSERT INTO integrations (kind) VALUES (@kind) ON CONFLICT (kind) DO NOTHING;

-- name: SaveCredential :exec
-- A new credential restarts the sync state, so the next run backfills.
INSERT INTO integrations (
    kind, credential, key_id, fingerprint, account, target, enabled, meta,
    created_by_sub, created_by_name, created_at, updated_by_sub, updated_by_name, updated_at, next_sync_at
)
VALUES (
    @kind, @credential, @key_id::text, @fingerprint::text, @account, @target, true, '{}',
    @actor_sub::text, @actor_name::text, now(), @actor_sub::text, @actor_name::text, now(), now()
)
ON CONFLICT (kind) DO UPDATE
SET credential = excluded.credential, key_id = excluded.key_id, fingerprint = excluded.fingerprint,
    account = excluded.account, target = excluded.target, enabled = true, meta = '{}',
    created_by_sub = CASE WHEN integrations.credential IS NULL THEN excluded.created_by_sub ELSE integrations.created_by_sub END,
    created_by_name = CASE WHEN integrations.credential IS NULL THEN excluded.created_by_name ELSE integrations.created_by_name END,
    created_at = CASE WHEN integrations.credential IS NULL THEN excluded.created_at ELSE integrations.created_at END,
    updated_by_sub = excluded.updated_by_sub, updated_by_name = excluded.updated_by_name, updated_at = now(),
    last_sync_at = NULL, last_success_at = NULL, last_error = NULL, last_error_code = NULL,
    next_sync_at = now(), backfilled_at = NULL;

-- name: SetTarget :exec
INSERT INTO integrations (kind, target, updated_by_sub, updated_by_name, updated_at, next_sync_at)
VALUES (@kind, @target, @actor_sub::text, @actor_name::text, now(), now())
ON CONFLICT (kind) DO UPDATE
SET target = excluded.target, updated_by_sub = excluded.updated_by_sub, updated_by_name = excluded.updated_by_name,
    updated_at = now(), next_sync_at = now(), backfilled_at = NULL, last_error = NULL, last_error_code = NULL;

-- name: SetEnabled :exec
UPDATE integrations
SET enabled = @enabled, updated_by_sub = @actor_sub::text, updated_by_name = @actor_name::text, updated_at = now()
WHERE kind = @kind;

-- name: SetMeta :exec
UPDATE integrations SET meta = @meta WHERE kind = @kind;

-- name: ClearCredential :exec
-- Removing a credential resets the row to a bare status row, so a Vault source starts clean.
UPDATE integrations
SET credential = NULL, key_id = NULL, fingerprint = NULL, account = '', target = '', enabled = true, meta = '{}',
    created_by_sub = NULL, created_by_name = NULL, created_at = NULL,
    updated_by_sub = @actor_sub::text, updated_by_name = @actor_name::text, updated_at = now(),
    last_sync_at = NULL, last_success_at = NULL, last_error = NULL, last_error_code = NULL,
    next_sync_at = now(), running_since = NULL, backfilled_at = NULL
WHERE kind = @kind;

-- name: ClaimSync :one
-- One sync per kind at a time, across processes; a claim older than stale_before is abandoned.
UPDATE integrations SET running_since = @now::timestamptz
WHERE kind = @kind AND (running_since IS NULL OR running_since < @stale_before::timestamptz)
RETURNING kind;

-- name: FinishSync :exec
UPDATE integrations
SET running_since = NULL, last_sync_at = now(),
    last_success_at = CASE WHEN @ok::boolean THEN now() ELSE last_success_at END,
    last_error = sqlc.narg(last_error), last_error_code = sqlc.narg(last_error_code),
    next_sync_at = sqlc.narg(next_sync_at)::timestamptz,
    backfilled_at = CASE WHEN @ok::boolean AND backfilled_at IS NULL THEN now() ELSE backfilled_at END
WHERE kind = @kind;

-- name: RecordError :exec
UPDATE integrations SET last_error = @last_error, last_error_code = @last_error_code WHERE kind = @kind;

-- name: InsertIntegrationEvent :exec
INSERT INTO integration_events (kind, action, ok, code, detail, count, http_status, actor_sub, actor_name)
VALUES (@kind, @action, @ok, @code, @detail, sqlc.narg(count), sqlc.narg(http_status), sqlc.narg(actor_sub), @actor_name::text);

-- name: ListIntegrationEvents :many
SELECT * FROM integration_events WHERE kind = @kind ORDER BY at DESC, id DESC LIMIT @lim;

-- name: SearchRowCounts :many
SELECT source, count(*) AS n FROM search_performance GROUP BY source;

-- name: NotifiedURLCount :one
SELECT COALESCE(sum(count), 0)::bigint FROM integration_events WHERE kind = 'indexnow' AND action = 'notify' AND ok;

-- name: UpsertCloudflareDaily :exec
INSERT INTO cloudflare_daily (host, day, dimension, value, requests, imported_at)
VALUES (@host, @day, @dimension, @value, @requests, now())
ON CONFLICT (host, day, dimension, value) DO UPDATE SET requests = excluded.requests, imported_at = now();

-- name: CloudflareRowCount :one
SELECT count(*) FROM cloudflare_daily WHERE host = @host;

-- name: CloudflareDaily :many
SELECT day, dimension, value, requests FROM cloudflare_daily
WHERE host = @host AND day >= @from_day AND day < @to_day
ORDER BY day, dimension, requests DESC;

-- name: HitsPerUTCDay :many
SELECT (ts AT TIME ZONE 'UTC')::date AS day, count(*) AS hits,
       count(*) FILTER (WHERE visitor_kind IN ('ai-crawler', 'ai-fetcher')) AS ai_hits
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts
GROUP BY 1
ORDER BY 1;

-- name: OriginAIBots :many
SELECT COALESCE(bot_name, '')::text AS bot_name, count(*) AS hits
FROM hits
WHERE ts >= @from_ts AND ts < @to_ts AND visitor_kind IN ('ai-crawler', 'ai-fetcher')
GROUP BY 1
ORDER BY 2 DESC;
