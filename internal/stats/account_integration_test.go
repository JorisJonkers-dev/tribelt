//go:build integration

package stats

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/oidc"
)

// accountMux serves the account page, with the request's X-Role as the signed-in account: an admin
// with two sessions, any other role with one, and no role signed out.
func accountMux(t *testing.T) *http.ServeMux {
	t.Helper()
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
	return mux
}

func TestTheAccountPageKeepsToTheCSP(t *testing.T) {
	rec := do(accountMux(t), "admin", "GET", "/stats/account", nil)
	inline := regexp.MustCompile(`\sstyle=|<script>|<script [^>]*>[^<]|\son[a-z]+=`)
	if body := rec.Body.String(); rec.Code != http.StatusOK || inline.MatchString(body) || strings.Contains(body, "ZgotmplZ") {
		t.Fatalf("GET = %d", rec.Code)
	}
}

func TestAnAdminSeesTheirRoleAndEverySessionWithSignOutsForBoth(t *testing.T) {
	body := do(accountMux(t), "admin", "GET", "/stats/account", nil).Body.String()
	for _, want := range []string{"Administrator", "admin-sub", "Chrome on macOS", "Safari on iPhone", "This session", `action="/auth/logout"`, `action="/auth/logout-all"`, "tribelt sessions only"} {
		if !strings.Contains(body, want) {
			t.Errorf("account page lacks %q", want)
		}
	}
}

func TestOnlyTheCurrentSessionIsMarked(t *testing.T) {
	if n := strings.Count(do(accountMux(t), "admin", "GET", "/stats/account", nil).Body.String(), "This session"); n != 1 {
		t.Fatalf("marked %d", n)
	}
}

func TestTheStatsHeaderLinksToTheAccountPage(t *testing.T) {
	if !strings.Contains(do(accountMux(t), "admin", "GET", "/stats/account", nil).Body.String(), `href="/stats/account"`) {
		t.Fatal("no link")
	}
}

func TestAViewerWithOneSessionGetsNoSignOutEverywhere(t *testing.T) {
	rec := do(accountMux(t), "grantee", "GET", "/stats/account", nil)
	if body := rec.Body.String(); rec.Code != http.StatusOK || !strings.Contains(body, "Stats Viewer") || strings.Contains(body, "/auth/logout-all") {
		t.Fatalf("GET = %d", rec.Code)
	}
}

func TestAnAnonymousVisitorIsSentToSignInFromTheAccountPage(t *testing.T) {
	if rec := do(accountMux(t), "", "GET", "/stats/account", nil); rec.Code != http.StatusFound || !strings.HasPrefix(rec.Header().Get("Location"), "/auth/login") {
		t.Fatalf("GET = %d %s", rec.Code, rec.Header().Get("Location"))
	}
}
