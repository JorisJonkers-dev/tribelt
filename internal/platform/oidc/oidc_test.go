package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/session"
)

// fakeIssuer is a minimal OIDC provider: discovery, JWKS, a PKCE-checking token endpoint with
// rotating refresh tokens like auth-api's (reuseRefreshTokens=false), and token revocation.
type fakeIssuer struct {
	srv  *httptest.Server
	key  *rsa.PrivateKey
	mu   sync.Mutex
	next map[string]any // claims for the next id token, at sign-in and at every refresh
	// challenge seen at authorize time, checked at the token endpoint
	challenge string
	nonce     string
	audience  string
	// refresh is the one refresh token currently valid; rotation replaces it.
	refresh   string
	rotations int
	refreshes int
	down      bool
	revoked   []string
}

func newFakeIssuer(t *testing.T) *fakeIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeIssuer{key: key, audience: "tribelt"}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/authorize", "token_endpoint": f.srv.URL + "/token",
			"jwks_uri": f.srv.URL + "/jwks", "end_session_endpoint": f.srv.URL + "/api/connect/logout",
			"revocation_endpoint":                   f.srv.URL + "/revoke",
			"id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"},
			"subject_types_supported": []string{"public"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/revoke", func(_ http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		f.revoked = append(f.revoked, r.PostForm.Get("token"))
		f.mu.Unlock()
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		f.mu.Lock()
		defer f.mu.Unlock()
		invalid := func() {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
		}
		claims := map[string]any{
			"iss": f.srv.URL, "aud": f.audience, "sub": "user-1", "exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(), "preferred_username": "joris",
		}
		switch r.PostForm.Get("grant_type") {
		case "refresh_token":
			f.refreshes++
			if f.down {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			if r.PostForm.Get("refresh_token") != f.refresh {
				invalid()
				return
			}
		default:
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if r.PostForm.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
				invalid()
				return
			}
			claims["nonce"] = f.nonce
		}
		for k, v := range f.next {
			claims[k] = v
		}
		f.rotations++
		f.refresh = fmt.Sprintf("rt-%d", f.rotations)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at", "token_type": "Bearer", "expires_in": 900, "refresh_token": f.refresh, "id_token": f.sign(t, claims),
		})
	})
	return f
}

func (f *fakeIssuer) set(fn func(f *fakeIssuer)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeIssuer) sign(t *testing.T, claims map[string]any) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: f.key}, (&jose.SignerOptions{}).WithHeader("kid", "k1").WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// clock is a settable test clock shared by Auth and MemStore.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newAuth(t *testing.T, f *fakeIssuer) (*Auth, *http.ServeMux, *MemStore, *clock) {
	t.Helper()
	codec, _ := session.NewCodec(strings.Repeat("s", 64))
	store := NewMemStore()
	clk := &clock{t: time.Now()}
	store.Now = clk.now
	a, err := New(context.Background(), Config{
		Issuer: f.srv.URL, ClientID: "tribelt", ClientSecret: "secret", RedirectURL: "https://mirror.test/auth/callback",
	}, codec, store, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	a.now = clk.now
	mux := http.NewServeMux()
	a.Routes(mux)
	mux.Handle("GET /stats", a.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := a.Viewer(r)
		_, _ = io.WriteString(w, "hello "+v.Name)
	})))
	return a, mux, store, clk
}

func do(mux http.Handler, method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func cookie(rec *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func withCookie(c *http.Cookie) *http.Request {
	req := httptest.NewRequest("GET", "/stats", nil)
	req.AddCookie(c)
	return req
}

// login runs /auth/login and records what the fake issuer's authorize endpoint would see.
func login(t *testing.T, f *fakeIssuer, mux http.Handler) (state string, flowC *http.Cookie) {
	t.Helper()
	rec := do(mux, "GET", "/auth/login?next=/stats%3Frange%3D7d")
	if rec.Code != http.StatusFound {
		t.Fatalf("login status %d", rec.Code)
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	q := loc.Query()
	if !strings.HasPrefix(loc.String(), f.srv.URL+"/authorize") || q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "tribelt" {
		t.Fatalf("authorize redirect %s", loc)
	}
	if !strings.Contains(q.Get("scope"), "openid") || q.Get("nonce") == "" || q.Get("state") == "" {
		t.Fatalf("missing scope/nonce/state: %s", loc)
	}
	f.set(func(f *fakeIssuer) { f.challenge, f.nonce = q.Get("code_challenge"), q.Get("nonce") })
	flowC = cookie(rec, flowCookie)
	if flowC == nil || !flowC.Secure || !flowC.HttpOnly || flowC.Path != "/" || flowC.Domain != "" {
		t.Fatalf("flow cookie %+v", flowC)
	}
	return q.Get("state"), flowC
}

// signIn completes a sign-in and returns the session cookie.
func signIn(t *testing.T, f *fakeIssuer, mux http.Handler) *http.Cookie {
	t.Helper()
	state, flowC := login(t, f, mux)
	rec := do(mux, "GET", "/auth/callback?code=good-code&state="+state, flowC)
	sess := cookie(rec, SessionCookie)
	if rec.Code != http.StatusFound || sess == nil {
		t.Fatalf("sign-in failed: %d %s", rec.Code, rec.Body)
	}
	return sess
}

func waitRevoked(t *testing.T, f *fakeIssuer, token string) {
	t.Helper()
	for range 200 {
		f.mu.Lock()
		for _, r := range f.revoked {
			if r == token {
				f.mu.Unlock()
				return
			}
		}
		f.mu.Unlock()
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("refresh token %q never revoked", token)
}

func TestLoginCallbackRoles(t *testing.T) {
	cases := []struct {
		name   string
		claims map[string]any
		status int
		admin  bool
	}{
		{"service permission", map[string]any{"roles": []string{"ROLE_USER", "SERVICE_TRIBELT"}}, http.StatusFound, false},
		{"admin", map[string]any{"roles": []string{"ROLE_ADMIN"}}, http.StatusFound, true},
		{"missing roles", map[string]any{}, http.StatusForbidden, false},
		{"other service", map[string]any{"roles": []string{"SERVICE_NOTES"}}, http.StatusForbidden, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIssuer(t)
			f.next = tc.claims
			a, mux, store, _ := newAuth(t, f)
			state, flowC := login(t, f, mux)
			rec := do(mux, "GET", "/auth/callback?code=good-code&state="+state, flowC)
			if rec.Code != tc.status {
				t.Fatalf("callback status %d: %s", rec.Code, rec.Body)
			}
			if tc.status != http.StatusFound {
				if cookie(rec, SessionCookie) != nil || strings.Contains(rec.Body.String(), "SERVICE") {
					t.Fatal("denied viewer must get no session and a generic page")
				}
				return
			}
			if rec.Header().Get("Location") != "/stats?range=7d" {
				t.Fatalf("next = %s", rec.Header().Get("Location"))
			}
			sess := cookie(rec, SessionCookie)
			if sess == nil || !sess.Secure || !sess.HttpOnly || sess.SameSite != http.SameSiteLaxMode {
				t.Fatalf("session cookie %+v", sess)
			}
			if got := do(mux, "GET", "/stats", sess); got.Code != 200 || got.Body.String() != "hello joris" {
				t.Fatalf("stats with session: %d %s", got.Code, got.Body)
			}
			v, _ := a.Viewer(withCookie(sess))
			if v.Admin != tc.admin || v.AccountID == 0 {
				t.Fatalf("viewer %+v, want admin %v", v, tc.admin)
			}
			if a.Session(withCookie(sess)) != v.SessionID || v.SessionID == "" || a.Session(httptest.NewRequest("GET", "/stats", nil)) != "" {
				t.Fatal("Session is the server-side session id, only while signed in")
			}
			if strings.Contains(sess.Value, v.SessionID) || store.RefreshToken(v.SessionID) != "rt-1" {
				t.Fatal("the cookie seals the session id; the refresh token stays server-side")
			}
			acc, sessions, ok := a.Account(withCookie(sess))
			if !ok || acc.Sub != "user-1" || acc.Name != "joris" || len(sessions) != 1 {
				t.Fatalf("local account %+v %d", acc, len(sessions))
			}

			out := do(mux, "POST", "/auth/logout", sess)
			if out.Code != http.StatusSeeOther || out.Header().Get("Location") != "/" {
				t.Fatalf("logout must stay on tribelt, got %d %s", out.Code, out.Header().Get("Location"))
			}
			if c := cookie(out, SessionCookie); c == nil || c.MaxAge >= 0 {
				t.Fatal("logout must clear the session cookie")
			}
			if _, ok := a.Viewer(withCookie(sess)); ok {
				t.Fatal("a signed-out session must not come back with the old cookie")
			}
			waitRevoked(t, f, "rt-1")
		})
	}
}

func TestRolesFollowAuthAPI(t *testing.T) {
	f := newFakeIssuer(t)
	f.next = map[string]any{"roles": []string{"ROLE_USER", "SERVICE_TRIBELT"}}
	a, mux, store, clk := newAuth(t, f)
	sess := signIn(t, f, mux)
	if v, _ := a.Viewer(withCookie(sess)); v.Admin {
		t.Fatal("starts as a viewer")
	}

	f.set(func(f *fakeIssuer) { f.next = map[string]any{"roles": []string{"ROLE_ADMIN"}} })
	if v, _ := a.Viewer(withCookie(sess)); v.Admin {
		t.Fatal("inside FreshFor the roles are not re-read")
	}
	if v, ok := a.Fresh(withCookie(sess)); !ok || !v.Admin {
		t.Fatal("Fresh re-reads the roles right away: the promotion applies to a change")
	}
	clk.add(FreshFor + time.Second)
	if v, ok := a.Viewer(withCookie(sess)); !ok || !v.Admin {
		t.Fatal("after FreshFor the promotion is visible on every request")
	}
	id := a.sessionID(withCookie(sess))
	if store.RefreshToken(id) != f.refresh {
		t.Fatal("the rotated refresh token must be stored")
	}

	f.set(func(f *fakeIssuer) { f.next = map[string]any{"roles": []string{"ROLE_USER", "SERVICE_NOTES"}} })
	clk.add(FreshFor + time.Second)
	if _, ok := a.Viewer(withCookie(sess)); ok {
		t.Fatal("removing TRIBELT in auth-api must end the session")
	}
	if _, err := store.Load(context.Background(), id); err == nil {
		t.Fatal("the withdrawn session must be deleted")
	}
}

func TestRevokedRefreshTokenEndsSession(t *testing.T) {
	f := newFakeIssuer(t)
	f.next = map[string]any{"roles": []string{"SERVICE_TRIBELT"}}
	a, mux, _, clk := newAuth(t, f)
	sess := signIn(t, f, mux)
	f.set(func(f *fakeIssuer) { f.refresh = "revoked-elsewhere" })
	clk.add(FreshFor + time.Second)
	if _, ok := a.Viewer(withCookie(sess)); ok {
		t.Fatal("invalid_grant must sign the viewer out")
	}
}

func TestAuthAPIDownGrace(t *testing.T) {
	f := newFakeIssuer(t)
	f.next = map[string]any{"roles": []string{"ROLE_ADMIN"}}
	a, mux, _, clk := newAuth(t, f)
	sess := signIn(t, f, mux)
	f.set(func(f *fakeIssuer) { f.down = true })
	clk.add(time.Minute)
	if _, ok := a.Viewer(withCookie(sess)); !ok {
		t.Fatal("reading continues briefly while auth-api is down")
	}
	if _, ok := a.Fresh(withCookie(sess)); ok {
		t.Fatal("a change needs fresh roles: refused while auth-api is down")
	}
	clk.add(grace)
	if _, ok := a.Viewer(withCookie(sess)); ok {
		t.Fatal("after the grace period an unverifiable session is refused")
	}
	f.set(func(f *fakeIssuer) { f.down = false })
	if _, ok := a.Viewer(withCookie(sess)); !ok {
		t.Fatal("the session recovers once auth-api answers")
	}
}

func TestConcurrentRefreshSpendsTokenOnce(t *testing.T) {
	f := newFakeIssuer(t)
	f.next = map[string]any{"roles": []string{"SERVICE_TRIBELT"}}
	a, mux, _, clk := newAuth(t, f)
	sess := signIn(t, f, mux)
	clk.add(FreshFor + time.Second)
	var wg sync.WaitGroup
	fails := make(chan struct{}, 20)
	for range 20 {
		wg.Go(func() {
			if _, ok := a.Viewer(withCookie(sess)); !ok {
				fails <- struct{}{}
			}
		})
	}
	wg.Wait()
	close(fails)
	if len(fails) != 0 {
		t.Fatalf("%d concurrent requests lost the session", len(fails))
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.refreshes != 1 {
		t.Fatalf("refreshes = %d, want 1: a rotated token must be spent once", f.refreshes)
	}
}

func TestLogoutAllEndsOnlyTribeltSessions(t *testing.T) {
	f := newFakeIssuer(t)
	f.next = map[string]any{"roles": []string{"SERVICE_TRIBELT"}}
	a, mux, _, _ := newAuth(t, f)
	first := signIn(t, f, mux)
	second := signIn(t, f, mux)
	if _, sessions, _ := a.Account(withCookie(first)); len(sessions) != 2 {
		t.Fatalf("two sign-ins, %d sessions", len(sessions))
	}
	out := do(mux, "POST", "/auth/logout-all", second)
	if out.Code != http.StatusSeeOther || out.Header().Get("Location") != "/" {
		t.Fatalf("logout-all %d %s", out.Code, out.Header().Get("Location"))
	}
	for _, c := range []*http.Cookie{first, second} {
		if _, ok := a.Viewer(withCookie(c)); ok {
			t.Fatal("every tribelt session must end")
		}
	}
	waitRevoked(t, f, "rt-1")
	waitRevoked(t, f, "rt-2")
}

func TestCallbackRejects(t *testing.T) {
	f := newFakeIssuer(t)
	f.next = map[string]any{"roles": []string{"ROLE_ADMIN"}}
	_, mux, _, _ := newAuth(t, f)

	state, flowC := login(t, f, mux)
	if rec := do(mux, "GET", "/auth/callback?code=good-code&state=wrong", flowC); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad state: %d", rec.Code)
	}
	if rec := do(mux, "GET", "/auth/callback?code=good-code&state="+state); rec.Code != http.StatusBadRequest {
		t.Fatalf("no flow cookie: %d", rec.Code)
	}
	bad := *flowC
	bad.Value = "garbage"
	if rec := do(mux, "GET", "/auth/callback?code=good-code&state="+state, &bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad flow cookie: %d", rec.Code)
	}
	if rec := do(mux, "GET", "/auth/callback?error=access_denied&state="+state, flowC); rec.Code != http.StatusForbidden {
		t.Fatalf("issuer error: %d", rec.Code)
	}
	if rec := do(mux, "GET", "/auth/callback?code=bad-code&state="+state, flowC); rec.Code != http.StatusBadGateway {
		t.Fatalf("exchange failure: %d", rec.Code)
	}

	state, flowC = login(t, f, mux)
	f.set(func(f *fakeIssuer) { f.nonce = "replayed" })
	if rec := do(mux, "GET", "/auth/callback?code=good-code&state="+state, flowC); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad nonce: %d", rec.Code)
	}

	state, flowC = login(t, f, mux)
	f.set(func(f *fakeIssuer) { f.audience = "someone-else" })
	if rec := do(mux, "GET", "/auth/callback?code=good-code&state="+state, flowC); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong audience: %d", rec.Code)
	}
}

func TestRequireRedirectsAnonymous(t *testing.T) {
	f := newFakeIssuer(t)
	a, mux, _, _ := newAuth(t, f)
	rec := do(mux, "GET", "/stats")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/auth/login?next=%2Fstats" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
	if _, ok := a.Viewer(httptest.NewRequest("GET", "/", nil)); ok {
		t.Fatal("no cookie, no viewer")
	}
	if _, ok := a.Viewer(withCookie(&http.Cookie{Name: SessionCookie, Value: "junk"})); ok {
		t.Fatal("junk cookie, no viewer")
	}
	if out := do(mux, "POST", "/auth/logout"); out.Code != http.StatusSeeOther || out.Header().Get("Location") != "/" {
		t.Fatalf("anonymous logout %s", out.Header().Get("Location"))
	}
	if out := do(mux, "POST", "/auth/logout-all"); out.Code != http.StatusSeeOther {
		t.Fatalf("anonymous logout-all %d", out.Code)
	}
}

func TestSafeNext(t *testing.T) {
	for in, want := range map[string]string{
		"/stats/pages?x=1": "/stats/pages?x=1", "": "/stats", "https://evil.test/stats": "/stats", "//evil.test": "/stats",
		"/": "/stats", "/stats\\evil": "/stats",
	} {
		if got := safeNext(in); got != want {
			t.Fatalf("safeNext(%q) = %q", in, got)
		}
	}
	if firstNonEmpty("", "") != "" {
		t.Fatal("firstNonEmpty")
	}
}

func TestDiscoveryFailure(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	codec, _ := session.NewCodec(strings.Repeat("s", 64))
	if _, err := New(context.Background(), Config{Issuer: srv.URL}, codec, NewMemStore(), slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("discovery failure must surface")
	}
}

func TestDevBypass(t *testing.T) {
	var g Gate = DevBypass{}
	stats := httptest.NewRequest("GET", "/stats", nil)
	if _, ok := g.Viewer(stats); !ok {
		t.Fatal("dev viewer on stats")
	}
	if _, ok := g.Viewer(httptest.NewRequest("GET", "/", nil)); ok {
		t.Fatal("public pages stay non-internal")
	}
	if v, _ := g.Fresh(stats); !v.Admin || g.Session(stats) == "" {
		t.Fatal("the bypass is an admin with a fixed session")
	}
	if acc, sessions, ok := g.Account(stats); !ok || !acc.Admin || len(sessions) != 1 {
		t.Fatal("the bypass has an account page")
	}
	if _, _, ok := g.Account(httptest.NewRequest("GET", "/", nil)); ok {
		t.Fatal("no account off the stats pages")
	}
	ro := DevBypass{ReadOnly: true}
	if v, _ := ro.Viewer(stats); v.Admin || ro.Session(httptest.NewRequest("GET", "/", nil)) != "" {
		t.Fatal("DEV_AUTH_BYPASS=viewer is read-only")
	}
	mux := http.NewServeMux()
	g.Routes(mux)
	mux.Handle("/stats", g.Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })))
	if do(mux, "GET", "/stats").Code != 204 || do(mux, "POST", "/auth/logout").Code != http.StatusSeeOther || do(mux, "POST", "/auth/logout-all").Code != http.StatusSeeOther {
		t.Fatal("bypass routes")
	}
	if rec := do(mux, "GET", "/auth/login?next=//evil.example"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/stats" {
		t.Fatalf("bypass login goes to the stats: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}
