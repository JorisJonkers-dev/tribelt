// Package accounts stores local Stats Viewer accounts and server-side sessions in Postgres (ADR-0006).
// Refresh tokens are sealed with the same SESSION_KEY-derived AES-256-GCM key as integration credentials.
package accounts

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JorisJonkers-dev/tribelt/internal/integrations"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/oidc"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

// sealKind binds sealed refresh tokens to their use, so a blob cannot pass for a credential.
const sealKind = integrations.Kind("oidc_refresh")

// Store implements oidc.Store.
type Store struct {
	Pool   *pgxpool.Pool
	Sealer *integrations.Sealer
}

var _ oidc.Store = (*Store)(nil)

// SignIn creates or updates the local account and opens a session for it.
func (s *Store) SignIn(ctx context.Context, issuer string, id oidc.Identity, refreshToken, userAgent string, expires time.Time) (string, error) {
	blob, err := s.Sealer.Seal(sealKind, []byte(refreshToken))
	if err != nil {
		return "", err
	}
	sid := oidc.NewSessionID()
	err = pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		q := queries.New(tx)
		acc, err := q.UpsertAccount(ctx, queries.UpsertAccountParams{
			Issuer: issuer, Sub: id.Sub, Name: id.Name, Email: id.Email, Roles: nonNil(id.Roles), Admin: oidc.IsAdmin(id.Roles),
		})
		if err != nil {
			return err
		}
		return q.CreateSession(ctx, queries.CreateSessionParams{
			ID: sid, AccountID: acc.ID, RefreshToken: blob, KeyID: s.Sealer.KeyID(), UserAgent: truncate(userAgent, 300), ExpiresAt: expires,
		})
	})
	if err != nil {
		return "", fmt.Errorf("accounts: sign in: %w", err)
	}
	return sid, nil
}

// Load returns an unexpired session with its account.
func (s *Store) Load(ctx context.Context, sessionID string) (oidc.Session, error) {
	row, err := queries.New(s.Pool).GetSession(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return oidc.Session{}, oidc.ErrNoSession
	}
	if err != nil {
		return oidc.Session{}, err
	}
	_ = queries.New(s.Pool).TouchSession(ctx, sessionID)
	return toSession(row.Session, row.Account), nil
}

// Refresh re-reads the roles under a row lock, so concurrent requests never spend the same rotated token.
func (s *Store) Refresh(ctx context.Context, sessionID string, seen time.Time, fn oidc.RefreshFunc) (oidc.Session, error) {
	var out oidc.Session
	gone := false
	err := pgx.BeginFunc(ctx, s.Pool, func(tx pgx.Tx) error {
		q := queries.New(tx)
		sess, err := q.LockSession(ctx, sessionID)
		if errors.Is(err, pgx.ErrNoRows) {
			return oidc.ErrNoSession
		}
		if err != nil {
			return err
		}
		// A changed roles_checked_at means another request refreshed while this one waited for the lock.
		if sess.RolesCheckedAt.Equal(seen) {
			if gone, err = s.refreshLocked(ctx, q, sess, fn); err != nil || gone {
				return err
			}
		}
		row, err := q.GetSession(ctx, sessionID)
		if err != nil {
			return err
		}
		out = toSession(row.Session, row.Account)
		return nil
	})
	if gone {
		return oidc.Session{}, oidc.ErrSessionGone
	}
	if err != nil {
		return oidc.Session{}, err
	}
	return out, nil
}

// refreshLocked spends the session's refresh token; gone means the session was deleted instead.
func (s *Store) refreshLocked(ctx context.Context, q *queries.Queries, sess queries.Session, fn oidc.RefreshFunc) (gone bool, err error) {
	plain, err := s.Sealer.Open(sealKind, sess.KeyID, sess.RefreshToken)
	if err != nil {
		return true, q.DeleteSession(ctx, sess.ID)
	}
	next, id, err := fn(ctx, string(plain))
	if errors.Is(err, oidc.ErrSessionGone) {
		return true, q.DeleteSession(ctx, sess.ID)
	}
	if err != nil {
		return false, err
	}
	blob, err := s.Sealer.Seal(sealKind, []byte(next))
	if err != nil {
		return false, err
	}
	if err := q.SaveSessionRefresh(ctx, queries.SaveSessionRefreshParams{ID: sess.ID, RefreshToken: blob, KeyID: s.Sealer.KeyID()}); err != nil {
		return false, err
	}
	return false, q.UpdateAccountRoles(ctx, queries.UpdateAccountRolesParams{
		ID: sess.AccountID, Name: id.Name, Email: id.Email, Roles: nonNil(id.Roles), Admin: oidc.IsAdmin(id.Roles),
	})
}

// SignOut deletes the session and returns its refresh token for revocation.
func (s *Store) SignOut(ctx context.Context, sessionID string) (string, error) {
	q := queries.New(s.Pool)
	sess, err := q.LockSession(ctx, sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", oidc.ErrNoSession
	}
	if err != nil {
		return "", err
	}
	if err := q.DeleteSession(ctx, sessionID); err != nil {
		return "", err
	}
	// An unreadable token (SESSION_KEY rotated) cannot be revoked; the session is gone regardless.
	if plain, openErr := s.Sealer.Open(sealKind, sess.KeyID, sess.RefreshToken); openErr == nil {
		return string(plain), nil
	}
	return "", nil
}

// SignOutAccount deletes every session of the account and returns their refresh tokens.
func (s *Store) SignOutAccount(ctx context.Context, accountID int64) ([]string, error) {
	q := queries.New(s.Pool)
	all, err := q.ListAccountSessions(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if err := q.DeleteAccountSessions(ctx, accountID); err != nil {
		return nil, err
	}
	tokens := make([]string, 0, len(all))
	for _, sess := range all {
		if plain, err := s.Sealer.Open(sealKind, sess.KeyID, sess.RefreshToken); err == nil {
			tokens = append(tokens, string(plain))
		}
	}
	return tokens, nil
}

// Sessions lists the account's open sessions, most recently used first.
func (s *Store) Sessions(ctx context.Context, accountID int64) ([]oidc.Session, error) {
	all, err := queries.New(s.Pool).ListAccountSessions(ctx, accountID)
	if err != nil {
		return nil, err
	}
	out := make([]oidc.Session, 0, len(all))
	for _, sess := range all {
		out = append(out, toSession(sess, queries.Account{}))
	}
	return out, nil
}

// Prune deletes expired sessions.
func (s *Store) Prune(ctx context.Context) error {
	return queries.New(s.Pool).DeleteExpiredSessions(ctx)
}

func toSession(s queries.Session, a queries.Account) oidc.Session {
	return oidc.Session{
		ID: s.ID, UserAgent: s.UserAgent, CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt,
		RolesCheckedAt: s.RolesCheckedAt, ExpiresAt: s.ExpiresAt,
		Account: oidc.Account{
			ID: a.ID, Sub: a.Sub, Name: a.Name, Email: a.Email, Roles: a.Roles, Admin: a.Admin,
			CreatedAt: a.CreatedAt, LastLoginAt: a.LastLoginAt,
		},
	}
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
