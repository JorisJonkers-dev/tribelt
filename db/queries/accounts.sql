-- name: UpsertAccount :one
INSERT INTO accounts (issuer, sub, name, email, roles, admin, last_login_at, roles_checked_at)
VALUES (@issuer, @sub, @name, @email, @roles::text[], @admin, now(), now())
ON CONFLICT (issuer, sub) DO UPDATE
SET name = excluded.name, email = excluded.email, roles = excluded.roles, admin = excluded.admin,
    last_login_at = now(), roles_checked_at = now()
RETURNING *;

-- name: UpdateAccountRoles :exec
UPDATE accounts
SET name = @name, email = @email, roles = @roles::text[], admin = @admin, roles_checked_at = clock_timestamp()
WHERE id = @id;

-- name: CreateSession :exec
INSERT INTO sessions (id, account_id, refresh_token, key_id, user_agent, expires_at)
VALUES (@id, @account_id, @refresh_token, @key_id, @user_agent, @expires_at);

-- name: GetSession :one
SELECT sqlc.embed(s), sqlc.embed(a)
FROM sessions s
JOIN accounts a ON a.id = s.account_id
WHERE s.id = @id AND s.expires_at > now();

-- name: LockSession :one
SELECT * FROM sessions WHERE id = @id AND expires_at > now() FOR UPDATE;

-- name: SaveSessionRefresh :exec
UPDATE sessions
SET refresh_token = @refresh_token, key_id = @key_id, roles_checked_at = clock_timestamp(), last_seen_at = clock_timestamp()
WHERE id = @id;

-- name: TouchSession :exec
UPDATE sessions SET last_seen_at = now() WHERE id = @id AND last_seen_at < now() - interval '1 minute';

-- name: DeleteSession :exec
DELETE FROM sessions WHERE id = @id;

-- name: DeleteAccountSessions :exec
DELETE FROM sessions WHERE account_id = @account_id;

-- name: ListAccountSessions :many
SELECT * FROM sessions WHERE account_id = @account_id AND expires_at > now() ORDER BY last_seen_at DESC;

-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= now();
