// Package oidc signs Stats Viewers in against auth-api (authorization code + PKCE, state and nonce) into a
// local account with a server-side session (ADR-0006). auth-api stays the only way to sign in; tribelt
// keeps the refresh token and re-reads the account's roles from auth-api, so a grant or revocation there
// takes effect within seconds, and signing out here ends only the tribelt session.
package oidc

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"html"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/session"
)

// Cookie names; the __Host- prefix pins them to this host, path / and HTTPS.
const (
	SessionCookie = "__Host-tribelt_session"
	flowCookie    = "__Host-tribelt_oidc"
	sessionTTL    = 7 * 24 * time.Hour
	flowTTL       = 10 * time.Minute
	// FreshFor is how old the roles of a session may be before a request re-reads them from auth-api.
	FreshFor = 15 * time.Second
	// grace keeps a session readable while auth-api is unreachable; changes always need fresh roles.
	grace    = 5 * time.Minute
	cacheFor = 3 * time.Second
)

// Roles that may read the stats; only the admin role may change Integrations.
func allowedRoles() []string { return []string{"SERVICE_TRIBELT", AdminRole} }

// AdminRole is auth-api's administrator role.
const AdminRole = "ROLE_ADMIN"

func allowed(roles []string) bool {
	return slices.ContainsFunc(roles, func(r string) bool { return slices.Contains(allowedRoles(), r) })
}

// IsAdmin reports whether the roles include auth-api's administrator role.
func IsAdmin(roles []string) bool { return slices.Contains(roles, AdminRole) }

// Viewer is a signed-in Stats Viewer.
type Viewer struct {
	Sub       string
	Name      string
	Admin     bool
	AccountID int64
	SessionID string
}

// Identity is what auth-api says about an account in a verified token.
type Identity struct {
	Sub, Name, Email string
	Roles            []string
}

// Account is the local record of an auth-api account that signed in to tribelt.
type Account struct {
	ID                     int64
	Sub, Name, Email       string
	Roles                  []string
	Admin                  bool
	CreatedAt, LastLoginAt time.Time
}

// Session is a server-side tribelt session.
type Session struct {
	ID             string
	Account        Account
	UserAgent      string
	CreatedAt      time.Time
	LastSeenAt     time.Time
	RolesCheckedAt time.Time
	ExpiresAt      time.Time
}

// RefreshFunc trades a refresh token for a new one and the account's current Identity.
type RefreshFunc func(ctx context.Context, refreshToken string) (newRefreshToken string, id Identity, err error)

// Store keeps local accounts and server-side sessions.
type Store interface {
	SignIn(ctx context.Context, issuer string, id Identity, refreshToken, userAgent string, expires time.Time) (sessionID string, err error)
	Load(ctx context.Context, sessionID string) (Session, error)
	// Refresh locks the session and, unless another request re-checked its roles since seen (the
	// RolesCheckedAt this request loaded), stores what fn returns. Comparing with seen instead of a clock
	// keeps app and database clock drift out of it. fn returning ErrSessionGone deletes the session.
	Refresh(ctx context.Context, sessionID string, seen time.Time, fn RefreshFunc) (Session, error)
	SignOut(ctx context.Context, sessionID string) (refreshToken string, err error)
	SignOutAccount(ctx context.Context, accountID int64) (refreshTokens []string, err error)
	Sessions(ctx context.Context, accountID int64) ([]Session, error)
}

var (
	// ErrNoSession means the session does not exist or has expired.
	ErrNoSession = errors.New("oidc: no such session")
	// ErrSessionGone means auth-api no longer lets this account in: revoked, disabled or permission removed.
	ErrSessionGone = errors.New("oidc: session ended by auth-api")
)

// Gate decides who may see /stats and whether a request is an Internal Hit.
type Gate interface {
	// Viewer returns the signed-in viewer with roles at most FreshFor old.
	Viewer(r *http.Request) (Viewer, bool)
	// Fresh re-reads the roles from auth-api now; use it before any change.
	Fresh(r *http.Request) (Viewer, bool)
	Require(next http.Handler) http.Handler
	Routes(mux *http.ServeMux)
	// Session identifies the signed-in session, for binding CSRF tokens; empty when signed out.
	Session(r *http.Request) string
	// Account returns the viewer's local account and its open tribelt sessions.
	Account(r *http.Request) (Account, []Session, bool)
}

// Config is the OIDC client registration.
type Config struct {
	Issuer, ClientID, ClientSecret, RedirectURL string
}

// Auth is the real Gate.
type Auth struct {
	oauth    oauth2.Config
	verifier *gooidc.IDTokenVerifier
	issuer   string
	revoke   string
	codec    *session.Codec
	store    Store
	http     *http.Client
	log      *slog.Logger
	now      func() time.Time

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	s  Session
	at time.Time
}

// New discovers the issuer and prepares the client.
func New(ctx context.Context, cfg Config, codec *session.Codec, store Store, log *slog.Logger) (*Auth, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery: %w", err)
	}
	var meta struct {
		Revocation string `json:"revocation_endpoint"`
	}
	_ = provider.Claims(&meta)
	return &Auth{
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL,
			Endpoint: provider.Endpoint(), Scopes: []string{gooidc.ScopeOpenID, "profile", "email"},
		},
		verifier: provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID}),
		issuer:   cfg.Issuer,
		revoke:   meta.Revocation,
		codec:    codec,
		store:    store,
		http:     &http.Client{Timeout: 10 * time.Second},
		log:      log,
		now:      time.Now,
		cache:    map[string]cached{},
	}, nil
}

// Routes mounts login, callback and the two sign-outs.
func (a *Auth) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("POST /auth/logout", a.logout)
	mux.HandleFunc("POST /auth/logout-all", a.logoutAll)
}

type flow struct {
	State    string `json:"s"`
	Nonce    string `json:"n"`
	Verifier string `json:"v"`
	Next     string `json:"x"`
}

func (a *Auth) login(w http.ResponseWriter, r *http.Request) {
	f := flow{State: random(), Nonce: random(), Verifier: oauth2.GenerateVerifier(), Next: safeNext(r.URL.Query().Get("next"))}
	sealed, err := a.codec.Seal(flowCookie, f, a.now(), flowTTL)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "seal flow cookie", err)
		return
	}
	setCookie(w, flowCookie, sealed, flowTTL)
	http.Redirect(w, r, a.oauth.AuthCodeURL(f.State, gooidc.Nonce(f.Nonce), oauth2.S256ChallengeOption(f.Verifier)), http.StatusFound)
}

type claims struct {
	Sub               string   `json:"sub"`
	Nonce             string   `json:"nonce"`
	Name              string   `json:"name"`
	PreferredUsername string   `json:"preferred_username"`
	Email             string   `json:"email"`
	Roles             []string `json:"roles"`
}

func (c claims) identity() Identity {
	return Identity{Sub: c.Sub, Name: firstNonEmpty(c.PreferredUsername, c.Name, c.Email, c.Sub), Email: c.Email, Roles: c.Roles}
}

func (a *Auth) callback(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(flowCookie)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "callback without flow cookie", err)
		return
	}
	clearCookie(w, flowCookie)
	var f flow
	if err := a.codec.Open(flowCookie, c.Value, a.now(), &f); err != nil {
		a.fail(w, http.StatusBadRequest, "flow cookie", err)
		return
	}
	q := r.URL.Query()
	if e := q.Get("error"); e != "" {
		a.fail(w, http.StatusForbidden, "issuer returned error", errors.New(e+": "+q.Get("error_description")))
		return
	}
	if q.Get("state") == "" || q.Get("state") != f.State {
		a.fail(w, http.StatusBadRequest, "state mismatch", errors.New("state"))
		return
	}
	tok, err := a.oauth.Exchange(r.Context(), q.Get("code"), oauth2.VerifierOption(f.Verifier))
	if err != nil {
		a.fail(w, http.StatusBadGateway, "code exchange", err)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idt, err := a.verifier.Verify(r.Context(), rawID)
	if err != nil {
		a.fail(w, http.StatusBadRequest, "id token verification", err)
		return
	}
	var cl claims
	if err := idt.Claims(&cl); err != nil || cl.Nonce != f.Nonce {
		a.fail(w, http.StatusBadRequest, "nonce mismatch", err)
		return
	}
	if !allowed(cl.Roles) {
		a.log.Warn("stats access denied", "sub", cl.Sub, "roles", cl.Roles)
		page(w, http.StatusForbidden, "Access denied", "This account may not view the statistics.")
		return
	}
	if tok.RefreshToken == "" {
		a.fail(w, http.StatusBadGateway, "no refresh token from the issuer", errors.New("refresh_token missing"))
		return
	}
	id := cl.identity()
	sid, err := a.store.SignIn(r.Context(), a.issuer, id, tok.RefreshToken, r.UserAgent(), a.now().Add(sessionTTL))
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "create session", err)
		return
	}
	sealed, err := a.codec.Seal(SessionCookie, sid, a.now(), sessionTTL)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "seal session", err)
		return
	}
	setCookie(w, SessionCookie, sealed, sessionTTL)
	a.log.Info("stats viewer signed in", "sub", id.Sub, "admin", IsAdmin(id.Roles))
	http.Redirect(w, r, f.Next, http.StatusFound)
}

// refresh trades the stored refresh token for the account's current roles.
func (a *Auth) refresh(ctx context.Context, refreshToken string) (string, Identity, error) {
	ctx = context.WithValue(ctx, oauth2.HTTPClient, a.http)
	tok, err := a.oauth.TokenSource(ctx, &oauth2.Token{RefreshToken: refreshToken, Expiry: time.Unix(1, 0)}).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) && (re.ErrorCode == "invalid_grant" || re.Response != nil && re.Response.StatusCode == http.StatusUnauthorized) {
			return "", Identity{}, ErrSessionGone
		}
		return "", Identity{}, err
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		raw = tok.AccessToken
	}
	idt, err := a.verifier.Verify(ctx, raw)
	if err != nil {
		return "", Identity{}, fmt.Errorf("oidc: verify refreshed token: %w", err)
	}
	var cl claims
	if err := idt.Claims(&cl); err != nil {
		return "", Identity{}, err
	}
	if !allowed(cl.Roles) {
		a.log.Info("stats access withdrawn in auth-api", "sub", cl.Sub)
		return "", Identity{}, ErrSessionGone
	}
	next := tok.RefreshToken
	if next == "" {
		next = refreshToken
	}
	return next, cl.identity(), nil
}

// sessionID opens the session cookie.
func (a *Auth) sessionID(r *http.Request) string {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return ""
	}
	var id string
	if a.codec.Open(SessionCookie, c.Value, a.now(), &id) != nil {
		return ""
	}
	return id
}

func (a *Auth) load(ctx context.Context, id string) (Session, error) {
	a.mu.Lock()
	if c, ok := a.cache[id]; ok && a.now().Sub(c.at) < cacheFor {
		a.mu.Unlock()
		return c.s, nil
	}
	a.mu.Unlock()
	s, err := a.store.Load(ctx, id)
	if err != nil {
		return Session{}, err
	}
	a.remember(s)
	return s, nil
}

func (a *Auth) remember(s Session) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(a.cache) >= 1024 {
		for k, c := range a.cache {
			if a.now().Sub(c.at) >= cacheFor {
				delete(a.cache, k)
			}
		}
	}
	a.cache[s.ID] = cached{s: s, at: a.now()}
}

func (a *Auth) forget(id string) {
	a.mu.Lock()
	delete(a.cache, id)
	a.mu.Unlock()
}

// current returns the session with roles no older than maxAge, refreshing them from auth-api when needed.
func (a *Auth) current(r *http.Request, maxAge time.Duration) (Session, bool) {
	id := a.sessionID(r)
	if id == "" {
		return Session{}, false
	}
	ctx := r.Context()
	s, err := a.load(ctx, id)
	if err != nil {
		if !errors.Is(err, ErrNoSession) {
			a.log.Warn("load session", "error", err)
		}
		return Session{}, false
	}
	age := a.now().Sub(s.RolesCheckedAt)
	if age < maxAge {
		return s, true
	}
	fresh, err := a.store.Refresh(ctx, id, s.RolesCheckedAt, a.refresh)
	switch {
	case err == nil:
		a.remember(fresh)
		return fresh, true
	case errors.Is(err, ErrSessionGone), errors.Is(err, ErrNoSession):
		a.forget(id)
		return Session{}, false
	default:
		a.log.Warn("refresh roles from auth-api", "error", err)
		if maxAge > 0 && age <= grace {
			return s, true
		}
		return Session{}, false
	}
}

func viewerOf(s Session) Viewer {
	return Viewer{Sub: s.Account.Sub, Name: s.Account.Name, Admin: s.Account.Admin, AccountID: s.Account.ID, SessionID: s.ID}
}

// Viewer returns the signed-in Stats Viewer, if any.
func (a *Auth) Viewer(r *http.Request) (Viewer, bool) {
	s, ok := a.current(r, FreshFor)
	return viewerOf(s), ok
}

// Fresh returns the viewer with roles read from auth-api for this request.
func (a *Auth) Fresh(r *http.Request) (Viewer, bool) {
	s, ok := a.current(r, 0)
	return viewerOf(s), ok
}

// Session is the id of the signed-in session.
func (a *Auth) Session(r *http.Request) string {
	v, ok := a.Viewer(r)
	if !ok {
		return ""
	}
	return v.SessionID
}

// Account returns the local account and its open sessions.
func (a *Auth) Account(r *http.Request) (Account, []Session, bool) {
	s, ok := a.current(r, FreshFor)
	if !ok {
		return Account{}, nil, false
	}
	all, err := a.store.Sessions(r.Context(), s.Account.ID)
	if err != nil {
		a.log.Warn("list sessions", "error", err)
		return s.Account, []Session{s}, true
	}
	return s.Account, all, true
}

// Require sends anonymous requests to login and back.
func (a *Auth) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := a.Viewer(r); !ok {
			http.Redirect(w, r, "/auth/login?"+url.Values{"next": {r.URL.RequestURI()}}.Encode(), http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logout ends this tribelt session only; the auth-api session stays.
func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	if id := a.sessionID(r); id != "" {
		a.forget(id)
		rt, err := a.store.SignOut(r.Context(), id)
		if err != nil && !errors.Is(err, ErrNoSession) {
			a.log.Warn("sign out", "error", err)
		}
		a.revokeAsync(rt)
	}
	clearCookie(w, SessionCookie)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// logoutAll ends every tribelt session of the signed-in account; auth-api sessions stay.
func (a *Auth) logoutAll(w http.ResponseWriter, r *http.Request) {
	if s, ok := a.current(r, FreshFor); ok {
		tokens, err := a.store.SignOutAccount(r.Context(), s.Account.ID)
		if err != nil {
			a.log.Warn("sign out everywhere", "error", err)
		}
		a.mu.Lock()
		a.cache = map[string]cached{}
		a.mu.Unlock()
		for _, t := range tokens {
			a.revokeAsync(t)
		}
		a.log.Info("stats viewer signed out of every tribelt session", "sub", s.Account.Sub, "sessions", len(tokens))
	}
	clearCookie(w, SessionCookie)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// revokeAsync tells auth-api the refresh token is done; the tribelt session is gone either way.
func (a *Auth) revokeAsync(refreshToken string) {
	if a.revoke == "" || refreshToken == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		form := url.Values{"token": {refreshToken}, "token_type_hint": {"refresh_token"}}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.revoke, strings.NewReader(form.Encode()))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(url.QueryEscape(a.oauth.ClientID), url.QueryEscape(a.oauth.ClientSecret))
		resp, err := a.http.Do(req)
		if err != nil {
			a.log.Warn("revoke refresh token", "error", err)
			return
		}
		_ = resp.Body.Close()
	}()
}

func (a *Auth) fail(w http.ResponseWriter, status int, what string, err error) {
	a.log.Warn("oidc: "+what, "error", err)
	page(w, status, "Sign-in failed", "Please try again from the statistics page.")
}

func page(w http.ResponseWriter, status int, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, "<!doctype html><meta charset=utf-8><meta name=robots content=noindex><title>%s</title><h1>%s</h1><p>%s</p><p><a href=\"/\">Home</a></p>",
		html.EscapeString(title), html.EscapeString(title), html.EscapeString(msg))
}

func setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: int(ttl.Seconds()), Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: "", Path: "/", MaxAge: -1, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func random() string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// NewSessionID returns a random 256-bit session id.
func NewSessionID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

// safeNext only returns to stats pages on this host.
func safeNext(next string) string {
	if strings.HasPrefix(next, "/stats") && !strings.HasPrefix(next, "//") && !strings.Contains(next, "\\") {
		return next
	}
	return "/stats"
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// DevBypass is a Gate that treats every request to /stats as a local developer, an admin unless
// ReadOnly (DEV_AUTH_BYPASS=viewer). Never in production.
type DevBypass struct{ ReadOnly bool }

func (d DevBypass) account() Account {
	if d.ReadOnly {
		return Account{ID: 2, Sub: "dev-viewer", Name: "viewer", Roles: []string{"SERVICE_TRIBELT"}}
	}
	return Account{ID: 1, Sub: "dev", Name: "developer", Roles: []string{AdminRole}, Admin: true}
}

// Viewer reports the developer only on stats pages, so public Hits stay non-internal.
func (d DevBypass) Viewer(r *http.Request) (Viewer, bool) {
	if !strings.HasPrefix(r.URL.Path, "/stats") {
		return Viewer{}, false
	}
	a := d.account()
	return Viewer{Sub: a.Sub, Name: a.Name, Admin: a.Admin, AccountID: a.ID, SessionID: "dev-bypass:" + a.Sub}, true
}

// Fresh is Viewer: the bypass has nothing to refresh.
func (d DevBypass) Fresh(r *http.Request) (Viewer, bool) { return d.Viewer(r) }

// Session is fixed: the bypass has no sessions.
func (d DevBypass) Session(r *http.Request) string {
	v, _ := d.Viewer(r)
	return v.SessionID
}

// Account is the developer with one pretend session.
func (d DevBypass) Account(r *http.Request) (Account, []Session, bool) {
	v, ok := d.Viewer(r)
	if !ok {
		return Account{}, nil, false
	}
	a := d.account()
	return a, []Session{{ID: v.SessionID, Account: a, UserAgent: r.UserAgent()}}, true
}

// Require lets everything through.
func (DevBypass) Require(next http.Handler) http.Handler { return next }

// Routes mounts a login that goes straight to the stats and logouts that return home.
func (DevBypass) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusFound) //nolint:gosec // safeNext only returns /stats paths
	})
	home := func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusSeeOther) }
	mux.HandleFunc("POST /auth/logout", home)
	mux.HandleFunc("POST /auth/logout-all", home)
}
