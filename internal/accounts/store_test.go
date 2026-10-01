package accounts

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/integrations"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/oidc"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	ctx := context.Background()
	db, err := pg.Open(ctx, pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	sealer, err := integrations.NewSealer(strings.Repeat("k", 64))
	if err != nil {
		t.Fatal(err)
	}
	return &Store{Pool: db.Pool(), Sealer: sealer}
}

func seenOf(t *testing.T, s *Store, sid string) time.Time {
	t.Helper()
	got, err := s.Load(context.Background(), sid)
	if err != nil {
		t.Fatal(err)
	}
	return got.RolesCheckedAt
}

var viewer = oidc.Identity{Sub: "user-1", Name: "joris", Email: "j@example.test", Roles: []string{"SERVICE_TRIBELT"}}

func TestSignInLoadAndSealedToken(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sid, err := s.SignIn(ctx, "https://auth.test", viewer, "rt-secret-1", "Mozilla/5.0 Chrome/140", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.Load(ctx, sid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Account.Sub != "user-1" || got.Account.Name != "joris" || got.Account.Admin || got.Account.ID == 0 {
		t.Fatalf("account %+v", got.Account)
	}
	var blob []byte
	if err := s.Pool.QueryRow(ctx, "SELECT refresh_token FROM sessions WHERE id = $1", sid).Scan(&blob); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("rt-secret-1")) {
		t.Fatal("the refresh token must be sealed at rest")
	}
	again, _ := s.SignIn(ctx, "https://auth.test", oidc.Identity{Sub: "user-1", Name: "joris2", Roles: []string{oidc.AdminRole}}, "rt-x", "", time.Now().Add(time.Hour))
	second, _ := s.Load(ctx, again)
	if second.Account.ID != got.Account.ID || !second.Account.Admin || second.Account.Name != "joris2" {
		t.Fatal("a second sign-in reuses the local account and takes the new roles")
	}
	if _, err := s.SignIn(ctx, "https://other-issuer.test", viewer, "rt-y", "", time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM accounts").Scan(&n)
	if n != 2 {
		t.Fatalf("accounts are keyed by issuer and sub: %d", n)
	}
	if _, err := s.Load(ctx, "missing"); !errors.Is(err, oidc.ErrNoSession) {
		t.Fatalf("missing session: %v", err)
	}
}

func TestRefreshRotatesAndUpdatesRoles(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sid, _ := s.SignIn(ctx, "https://auth.test", viewer, "rt-1", "", time.Now().Add(time.Hour))
	loaded, _ := s.Load(ctx, sid)
	var seen string
	fresh, err := s.Refresh(ctx, sid, loaded.RolesCheckedAt, func(_ context.Context, rt string) (string, oidc.Identity, error) {
		seen = rt
		return "rt-2", oidc.Identity{Sub: "user-1", Name: "joris", Roles: []string{oidc.AdminRole}}, nil
	})
	if err != nil || seen != "rt-1" || !fresh.Account.Admin {
		t.Fatalf("refresh: %v seen=%q admin=%v", err, seen, fresh.Account.Admin)
	}
	tok, _ := s.SignOut(ctx, sid)
	if tok != "rt-2" {
		t.Fatalf("the rotated token must be stored, got %q", tok)
	}
}

func TestConcurrentRefreshCallsAuthAPIOnce(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sid, _ := s.SignIn(ctx, "https://auth.test", viewer, "rt-1", "", time.Now().Add(time.Hour))
	loaded, _ := s.Load(ctx, sid)
	stale := loaded.RolesCheckedAt
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			if _, err := s.Refresh(ctx, sid, stale, func(_ context.Context, rt string) (string, oidc.Identity, error) {
				calls.Add(1)
				time.Sleep(20 * time.Millisecond)
				return rt + "+", viewer, nil
			}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("auth-api called %d times; the row lock must let one request spend the token", calls.Load())
	}
}

func TestRefreshGoneAndKeyChangeEndSession(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	sid, _ := s.SignIn(ctx, "https://auth.test", viewer, "rt-1", "", time.Now().Add(time.Hour))
	gone := func(context.Context, string) (string, oidc.Identity, error) {
		return "", oidc.Identity{}, oidc.ErrSessionGone
	}
	if _, err := s.Refresh(ctx, sid, seenOf(t, s, sid), gone); !errors.Is(err, oidc.ErrSessionGone) {
		t.Fatalf("gone: %v", err)
	}
	if _, err := s.Load(ctx, sid); !errors.Is(err, oidc.ErrNoSession) {
		t.Fatal("a session auth-api ended must be deleted")
	}

	sid, _ = s.SignIn(ctx, "https://auth.test", viewer, "rt-1", "", time.Now().Add(time.Hour))
	other, _ := integrations.NewSealer(strings.Repeat("z", 64))
	rotated := &Store{Pool: s.Pool, Sealer: other}
	never := func(context.Context, string) (string, oidc.Identity, error) {
		t.Fatal("a token sealed under the old key must not reach auth-api")
		return "", oidc.Identity{}, nil
	}
	if _, err := rotated.Refresh(ctx, sid, seenOf(t, s, sid), never); !errors.Is(err, oidc.ErrSessionGone) {
		t.Fatalf("after a SESSION_KEY change: %v", err)
	}

	sid, _ = s.SignIn(ctx, "https://auth.test", viewer, "rt-1", "", time.Now().Add(time.Hour))
	boom := errors.New("auth-api down")
	if _, err := s.Refresh(ctx, sid, seenOf(t, s, sid), func(context.Context, string) (string, oidc.Identity, error) {
		return "", oidc.Identity{}, boom
	}); !errors.Is(err, boom) {
		t.Fatalf("transport errors pass through: %v", err)
	}
	if _, err := s.Load(ctx, sid); err != nil {
		t.Fatal("a transport error keeps the session")
	}
}

func TestSignOutAccountSessionsAndPrune(t *testing.T) {
	ctx := context.Background()
	s := newStore(t)
	a, _ := s.SignIn(ctx, "https://auth.test", viewer, "rt-a", "Firefox", time.Now().Add(time.Hour))
	_, _ = s.SignIn(ctx, "https://auth.test", viewer, "rt-b", "Chrome", time.Now().Add(time.Hour))
	expired, _ := s.SignIn(ctx, "https://auth.test", viewer, "rt-c", "Old", time.Now().Add(-time.Minute))
	got, _ := s.Load(ctx, a)
	list, err := s.Sessions(ctx, got.Account.ID)
	if err != nil || len(list) != 2 {
		t.Fatalf("open sessions %d %v", len(list), err)
	}
	if _, err := s.Load(ctx, expired); !errors.Is(err, oidc.ErrNoSession) {
		t.Fatal("an expired session does not load")
	}
	if err := s.Prune(ctx); err != nil {
		t.Fatal(err)
	}
	tokens, err := s.SignOutAccount(ctx, got.Account.ID)
	if err != nil || len(tokens) != 2 {
		t.Fatalf("sign out everywhere: %v %v", tokens, err)
	}
	if _, err := s.SignOut(ctx, a); !errors.Is(err, oidc.ErrNoSession) {
		t.Fatal("already signed out")
	}
	var n int
	_ = s.Pool.QueryRow(ctx, "SELECT count(*) FROM sessions").Scan(&n)
	if n != 0 {
		t.Fatalf("%d sessions left after prune and sign-out", n)
	}
}
