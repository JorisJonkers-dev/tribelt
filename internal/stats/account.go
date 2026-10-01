package stats

import (
	"bytes"
	"net/http"
	"strings"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/oidc"
)

// AccountPage is the signed-in viewer's local account and open tribelt sessions.
type AccountPage struct {
	Account  oidc.Account
	Sessions []AccountSession
	FreshFor string
}

// AccountSession is one open tribelt session, as listed on the account page.
type AccountSession struct {
	Device            string
	Started, LastSeen time.Time
	Expires           time.Time
	Current           bool
}

func (s *Service) accountView(w http.ResponseWriter, r *http.Request) {
	acc, sessions, ok := s.Account(r)
	if !ok {
		http.Redirect(w, r, "/auth/login?next=/stats/account", http.StatusFound)
		return
	}
	f := ParseFilter(r.URL.Query(), s.Now())
	f.Path = ""
	p, err := s.chrome(r.Context(), r, "account", f)
	if err != nil {
		s.fail(w, err)
		return
	}
	current := ""
	if s.Session != nil {
		current = s.Session(r)
	}
	page := AccountPage{Account: acc, FreshFor: oidc.FreshFor.String()}
	for _, sess := range sessions {
		page.Sessions = append(page.Sessions, AccountSession{
			Device: device(sess.UserAgent), Started: sess.CreatedAt, LastSeen: sess.LastSeenAt, Expires: sess.ExpiresAt,
			Current: sess.ID == current,
		})
	}
	p.Title = "Account"
	p.Data = page
	var buf bytes.Buffer
	if err := s.tmpl.ExecuteTemplate(&buf, "account", p); err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// device names a browser and platform from a user agent, roughly.
func device(ua string) string {
	browser := "Browser"
	for _, b := range []struct{ token, name string }{
		{"Edg/", "Edge"}, {"Firefox/", "Firefox"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	} {
		if strings.Contains(ua, b.token) {
			browser = b.name
			break
		}
	}
	platform := ""
	for _, p := range []struct{ token, name string }{
		{"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Android", "Android"}, {"Mac OS X", "macOS"}, {"Windows", "Windows"}, {"Linux", "Linux"},
	} {
		if strings.Contains(ua, p.token) {
			platform = p.name
			break
		}
	}
	if platform == "" {
		return browser
	}
	return browser + " on " + platform
}
