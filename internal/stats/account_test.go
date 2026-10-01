package stats

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/oidc"
)

func TestAccountPage(t *testing.T) {
	_, s := integrationsMux(t)
	when := time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)
	s.svc.Account = func(r *http.Request) (oidc.Account, []oidc.Session, bool) {
		role := r.Header.Get("X-Role")
		if role == "" {
			return oidc.Account{}, nil, false
		}
		acc := oidc.Account{ID: 7, Sub: role + "-sub", Name: role, Roles: []string{"SERVICE_TRIBELT"}, Admin: role == "admin", CreatedAt: when, LastLoginAt: when}
		current := oidc.Session{ID: "session-of-" + role, UserAgent: "Mozilla/5.0 (Macintosh; Mac OS X) Chrome/140", CreatedAt: when, LastSeenAt: when, ExpiresAt: when.Add(7 * 24 * time.Hour)}
		other := oidc.Session{ID: "phone", UserAgent: "Mozilla/5.0 (iPhone) Safari/604", CreatedAt: when, LastSeenAt: when, ExpiresAt: when}
		if role == "admin" {
			return acc, []oidc.Session{current, other}, true
		}
		return acc, []oidc.Session{current}, true
	}
	mux := http.NewServeMux()
	s.svc.Routes(mux, func(next http.Handler) http.Handler { return next })
	inline := regexp.MustCompile(`\sstyle=|<script>|<script [^>]*>[^<]|\son[a-z]+=`)

	rec := do(mux, "admin", "GET", "/stats/account", nil)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || inline.MatchString(body) || strings.Contains(body, "ZgotmplZ") {
		t.Fatalf("admin account page: %d", rec.Code)
	}
	for _, want := range []string{"Administrator", "admin-sub", "Chrome on macOS", "Safari on iPhone", "This session", `action="/auth/logout"`, `action="/auth/logout-all"`, "tribelt sessions only"} {
		if !strings.Contains(body, want) {
			t.Fatalf("account page lacks %q", want)
		}
	}
	if strings.Count(body, "This session") != 1 {
		t.Fatal("only the current session is marked")
	}
	if !strings.Contains(body, `href="/stats/account"`) {
		t.Fatal("the header links to the account page")
	}

	rec = do(mux, "grantee", "GET", "/stats/account", nil)
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, "Stats Viewer") || strings.Contains(body, "/auth/logout-all") {
		t.Fatalf("a single-session viewer gets no sign-out-everywhere button: %d", rec.Code)
	}

	if rec := do(mux, "", "GET", "/stats/account", nil); rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/auth/login") {
		t.Fatalf("anonymous: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func TestDevice(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 (Windows NT 10.0) Chrome/140 Edg/140":    "Edge on Windows",
		"Mozilla/5.0 (X11; Linux x86_64; rv:130) Firefox/130": "Firefox on Linux",
		"Mozilla/5.0 (Linux; Android 14) Chrome/140":          "Chrome on Android",
		"curl/8": "Browser",
	} {
		if got := device(ua); got != want {
			t.Fatalf("device(%q) = %q, want %q", ua, got, want)
		}
	}
}
