// Package oidc signs Stats Viewers in against auth-api (authorization code + PKCE, state and nonce)
// and keeps them in an encrypted host-only session cookie.
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
	"time"

	gooidc "github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/session"
)

// Cookie names; the __Host- prefix pins them to this host, path / and HTTPS.
const (
	SessionCookie = "__Host-tribelt_session"
	flowCookie    = "__Host-tribelt_oidc"
	sessionTTL    = 8 * time.Hour
	flowTTL       = 10 * time.Minute
)

// Roles that may read the stats.
func allowedRoles() []string { return []string{"SERVICE_TRIBELT", "ROLE_ADMIN"} }

// Viewer is a signed-in Stats Viewer.
type Viewer struct {
	Sub     string `json:"sub"`
	Name    string `json:"name"`
	IDToken string `json:"idt,omitempty"`
}

// Gate decides who may see /stats and whether a request is an Internal Hit.
type Gate interface {
	Viewer(r *http.Request) (Viewer, bool)
	Require(next http.Handler) http.Handler
	Routes(mux *http.ServeMux)
}

// Config is the OIDC client registration.
type Config struct {
	Issuer, ClientID, ClientSecret, RedirectURL string
	// PostLogoutURL must match the registered post_logout_redirect_uri exactly.
	PostLogoutURL string
}

// Auth is the real Gate.
type Auth struct {
	oauth      oauth2.Config
	verifier   *gooidc.IDTokenVerifier
	endSession string
	postLogout string
	codec      *session.Codec
	log        *slog.Logger
	now        func() time.Time
}

// New discovers the issuer and prepares the client.
func New(ctx context.Context, cfg Config, codec *session.Codec, log *slog.Logger) (*Auth, error) {
	provider, err := gooidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc: discovery: %w", err)
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = provider.Claims(&meta)
	return &Auth{
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL,
			Endpoint: provider.Endpoint(), Scopes: []string{gooidc.ScopeOpenID, "profile", "email"},
		},
		verifier:   provider.Verifier(&gooidc.Config{ClientID: cfg.ClientID}),
		endSession: meta.EndSession,
		postLogout: cfg.PostLogoutURL,
		codec:      codec,
		log:        log,
		now:        time.Now,
	}, nil
}

// Routes mounts login, callback and logout.
func (a *Auth) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", a.login)
	mux.HandleFunc("GET /auth/callback", a.callback)
	mux.HandleFunc("POST /auth/logout", a.logout)
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
	if !slices.ContainsFunc(cl.Roles, func(r string) bool { return slices.Contains(allowedRoles(), r) }) {
		a.log.Warn("stats access denied", "sub", cl.Sub, "roles", cl.Roles)
		page(w, http.StatusForbidden, "Access denied", "This account may not view the statistics.")
		return
	}
	v := Viewer{Sub: cl.Sub, Name: firstNonEmpty(cl.PreferredUsername, cl.Name, cl.Email, cl.Sub), IDToken: rawID}
	sealed, err := a.codec.Seal(SessionCookie, v, a.now(), sessionTTL)
	if err != nil {
		a.fail(w, http.StatusInternalServerError, "seal session", err)
		return
	}
	setCookie(w, SessionCookie, sealed, sessionTTL)
	a.log.Info("stats viewer signed in", "sub", cl.Sub)
	http.Redirect(w, r, f.Next, http.StatusFound)
}

func (a *Auth) logout(w http.ResponseWriter, r *http.Request) {
	v, _ := a.Viewer(r)
	clearCookie(w, SessionCookie)
	target := "/"
	if a.endSession != "" {
		q := url.Values{"client_id": {a.oauth.ClientID}, "post_logout_redirect_uri": {a.postLogout}}
		if v.IDToken != "" {
			q.Set("id_token_hint", v.IDToken)
		}
		target = a.endSession + "?" + q.Encode()
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// Viewer returns the signed-in Stats Viewer, if any.
func (a *Auth) Viewer(r *http.Request) (Viewer, bool) {
	c, err := r.Cookie(SessionCookie)
	if err != nil {
		return Viewer{}, false
	}
	var v Viewer
	if a.codec.Open(SessionCookie, c.Value, a.now(), &v) != nil {
		return Viewer{}, false
	}
	return v, true
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

// DevBypass is a Gate that treats every request to /stats as a local developer. Never in production.
type DevBypass struct{}

// Viewer reports the developer only on stats pages, so public Hits stay non-internal.
func (DevBypass) Viewer(r *http.Request) (Viewer, bool) {
	if strings.HasPrefix(r.URL.Path, "/stats") {
		return Viewer{Sub: "dev", Name: "developer"}, true
	}
	return Viewer{}, false
}

// Require lets everything through.
func (DevBypass) Require(next http.Handler) http.Handler { return next }

// Routes mounts a login that goes straight to the stats and a logout that returns home.
func (DevBypass) Routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /auth/login", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, safeNext(r.URL.Query().Get("next")), http.StatusFound) //nolint:gosec // safeNext only returns /stats paths
	})
	mux.HandleFunc("POST /auth/logout", func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/", http.StatusSeeOther) })
}
