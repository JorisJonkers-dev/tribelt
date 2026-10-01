# Local accounts with server-side sessions, roles re-read from auth-api

Signing in to /stats creates a local Account (keyed by auth-api issuer and subject) and a server-side Session in Postgres; the browser only holds a sealed session id. auth-api remains the only way to sign in, and it stays the source of truth for roles: tribelt keeps the session's refresh token (sealed like integration credentials, ADR-0005) and trades it for fresh tokens whenever the roles it holds are more than 15 seconds old, and always right before a change. A permission granted or withdrawn in auth-api therefore applies within seconds, and withdrawing access ends the tribelt sessions on the next request.

We chose this over the earlier design, which sealed the ID token's roles into the cookie at sign-in: that froze admin status for the cookie's lifetime, so promoting someone in auth-api never reached tribelt until they signed in again. A push from auth-api (webhook or back-channel logout) would be instant, but needs auth-api changes and still a pull fallback; refreshing on read costs one token call per active viewer per 15 seconds, which is nothing at this scale.

Signing out of tribelt deletes the tribelt session and revokes its refresh token; it deliberately does not call auth-api's end-session endpoint, so the viewer stays signed in to auth-api and every other app.

## Consequences

- auth-api rotates refresh tokens (`reuseRefreshTokens=false`), so a refresh runs under a row lock and only when the stored `roles_checked_at` still matches what the request loaded; concurrent requests never spend the same token twice, and app and database clocks never get compared.
- If auth-api is unreachable, reading /stats continues for up to five minutes on the last known roles; every change needs fresh roles and is refused meanwhile.
- Rotating SESSION_KEY makes stored refresh tokens unreadable, which signs everyone out; they sign in again.
