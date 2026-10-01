package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

// fakeIssuer is a minimal OIDC provider: discovery, JWKS and a PKCE-checking token endpoint.
type fakeIssuer struct {
	srv  *httptest.Server
	key  *rsa.PrivateKey
	mu   sync.Mutex
	next map[string]any // claims for the next id token
	// challenge seen at authorize time, checked at the token endpoint
	challenge string
	nonce     string
	audience  string
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
			"id_token_signing_alg_values_supported": []string{"RS256"}, "response_types_supported": []string{"code"},
			"subject_types_supported": []string{"public"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.PostForm.Get("code") != "good-code" || base64.RawURLEncoding.EncodeToString(sum[:]) != f.challenge {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		claims := map[string]any{
			"iss": f.srv.URL, "aud": f.audience, "sub": "user-1", "exp": time.Now().Add(time.Hour).Unix(),
			"iat": time.Now().Unix(), "nonce": f.nonce, "preferred_username": "joris",
		}
		for k, v := range f.next {
			claims[k] = v
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600, "id_token": f.sign(t, claims)})
	})
	return f
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

func newAuth(t *testing.T, f *fakeIssuer) (*Auth, *http.ServeMux) {
	t.Helper()
	codec, _ := session.NewCodec(strings.Repeat("s", 64))
	a, err := New(context.Background(), Config{
		Issuer: f.srv.URL, ClientID: "tribelt", ClientSecret: "secret", RedirectURL: "https://mirror.test/auth/callback",
		PostLogoutURL: "https://mirror.test/",
	}, codec, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	a.Routes(mux)
	mux.Handle("GET /stats", a.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v, _ := a.Viewer(r)
		_, _ = io.WriteString(w, "hello "+v.Name)
	})))
	return a, mux
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
	f.mu.Lock()
	f.challenge, f.nonce = q.Get("code_challenge"), q.Get("nonce")
	f.mu.Unlock()
	flowC = cookie(rec, flowCookie)
	if flowC == nil || !flowC.Secure || !flowC.HttpOnly || flowC.Path != "/" || flowC.Domain != "" {
		t.Fatalf("flow cookie %+v", flowC)
	}
	return q.Get("state"), flowC
}

func TestLoginCallbackRoles(t *testing.T) {
	cases := []struct {
		name   string
		claims map[string]any
		status int
	}{
		{"service permission", map[string]any{"roles": []string{"ROLE_USER", "SERVICE_TRIBELT"}}, http.StatusFound},
		{"admin", map[string]any{"roles": []string{"ROLE_ADMIN"}}, http.StatusFound},
		{"missing roles", map[string]any{}, http.StatusForbidden},
		{"other service", map[string]any{"roles": []string{"SERVICE_NOTES"}}, http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeIssuer(t)
			f.next = tc.claims
			_, mux := newAuth(t, f)
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
			out := do(mux, "POST", "/auth/logout", sess)
			loc, _ := url.Parse(out.Header().Get("Location"))
			if out.Code != http.StatusSeeOther || loc.Path != "/api/connect/logout" || loc.Query().Get("post_logout_redirect_uri") != "https://mirror.test/" || loc.Query().Get("id_token_hint") == "" {
				t.Fatalf("logout redirect %d %s", out.Code, loc)
			}
			if c := cookie(out, SessionCookie); c == nil || c.MaxAge >= 0 {
				t.Fatal("logout must clear the session")
			}
		})
	}
}

func TestCallbackRejects(t *testing.T) {
	f := newFakeIssuer(t)
	f.next = map[string]any{"roles": []string{"ROLE_ADMIN"}}
	_, mux := newAuth(t, f)

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
	f.mu.Lock()
	f.nonce = "replayed"
	f.mu.Unlock()
	if rec := do(mux, "GET", "/auth/callback?code=good-code&state="+state, flowC); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad nonce: %d", rec.Code)
	}

	state, flowC = login(t, f, mux)
	f.audience = "someone-else"
	if rec := do(mux, "GET", "/auth/callback?code=good-code&state="+state, flowC); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong audience: %d", rec.Code)
	}
}

func TestRequireRedirectsAnonymous(t *testing.T) {
	f := newFakeIssuer(t)
	a, mux := newAuth(t, f)
	rec := do(mux, "GET", "/stats")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/auth/login?next=%2Fstats" {
		t.Fatalf("%d %s", rec.Code, rec.Header().Get("Location"))
	}
	junk := &http.Cookie{Name: SessionCookie, Value: "junk"}
	if _, ok := a.Viewer(httptest.NewRequest("GET", "/", nil)); ok {
		t.Fatal("no cookie, no viewer")
	}
	req := httptest.NewRequest("GET", "/", nil)
	req.AddCookie(junk)
	if _, ok := a.Viewer(req); ok {
		t.Fatal("junk cookie, no viewer")
	}
	if out := do(mux, "POST", "/auth/logout"); out.Code != http.StatusSeeOther || strings.Contains(out.Header().Get("Location"), "id_token_hint") {
		t.Fatalf("anonymous logout %s", out.Header().Get("Location"))
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
	if _, err := New(context.Background(), Config{Issuer: srv.URL}, codec, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("discovery failure must surface")
	}
}

func TestDevBypass(t *testing.T) {
	var g Gate = DevBypass{}
	if _, ok := g.Viewer(httptest.NewRequest("GET", "/stats", nil)); !ok {
		t.Fatal("dev viewer on stats")
	}
	if _, ok := g.Viewer(httptest.NewRequest("GET", "/", nil)); ok {
		t.Fatal("public pages stay non-internal")
	}
	mux := http.NewServeMux()
	g.Routes(mux)
	mux.Handle("/stats", g.Require(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })))
	if do(mux, "GET", "/stats").Code != 204 || do(mux, "POST", "/auth/logout").Code != http.StatusSeeOther {
		t.Fatal("bypass routes")
	}
	if rec := do(mux, "GET", "/auth/login?next=//evil.example"); rec.Code != http.StatusFound || rec.Header().Get("Location") != "/stats" {
		t.Fatalf("bypass login goes to the stats: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}
