// Package httpx holds the HTTP plumbing shared by every route: security headers, panic recovery and
// health endpoints.
package httpx

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/JorisJonkers-dev/go-commons/secure"
)

// contentSecurityPolicy is tribelt's: no page loads anything from elsewhere.
const contentSecurityPolicy = "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'"

// Secure sets the security headers every response carries: go-commons' fixed set, tribelt's own
// content security policy and permissions policy, and HSTS when the site is served over HTTPS.
func Secure(hsts bool, next http.Handler) http.Handler {
	policy := secure.Policy{
		ContentSecurityPolicy: contentSecurityPolicy,
		Permissions:           "camera=(), microphone=(), geolocation=(), interest-cohort=()",
	}
	if hsts {
		policy.HSTS = "max-age=31536000"
	}
	// Cannot fail: the policy names a content security policy.
	wrap, _ := secure.Headers(policy)
	return wrap(next)
}

// Recover turns a panic into a generic 500 and logs the detail.
func Recover(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler { //nolint:errorlint // sentinel compared by identity, as net/http does
					panic(v)
				}
				log.Error("panic", "path", r.URL.Path, "panic", v)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Health mounts /healthz (process is up) and /readyz (content rendered, database reachable).
func Health(mux *http.ServeMux, ping func(ctx context.Context) error) {
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write([]byte("ok\n"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := ping(ctx); err != nil {
			http.Error(w, "not ready\n", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ready\n"))
	})
}
