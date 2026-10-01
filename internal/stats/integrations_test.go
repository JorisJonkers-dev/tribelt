package stats

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/JorisJonkers-dev/tribelt/internal/integrations"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

const testBingKey = "bing-test-key-aaaaaaaaaaaa"

// integrationsMux mounts the stats routes behind a stand-in for oidc.Auth: the X-Role header picks
// the viewer (admin, grantee or none), and Require redirects anonymous requests to sign-in.
func integrationsMux(t *testing.T) (*http.ServeMux, seeded) {
	t.Helper()
	s := seed(t)
	bing := http.NewServeMux()
	bing.HandleFunc("GET /webmaster/api.svc/json/GetUserSites", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != testBingKey {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"d":[{"Url":"https://tribelt.jorisjonkers.dev/","IsVerified":true}]}`)
	})
	srv := httptest.NewServer(bing)
	t.Cleanup(srv.Close)
	sealer, err := integrations.NewSealer("a-session-key-of-at-least-32-characters")
	if err != nil {
		t.Fatal(err)
	}
	s.svc.Integrations = &integrations.Manager{
		Q: s.q, Sealer: sealer, Endpoints: integrations.Endpoints{Bing: srv.URL, Google: srv.URL, IndexNow: srv.URL, Cloudflare: srv.URL},
		HTTP: srv.Client(), BaseURL: "https://tribelt.jorisjonkers.dev", Launch: at(5), Log: slog.New(slog.DiscardHandler), Now: now,
		PageURLs: func() []string { return nil },
	}
	s.svc.Actor = func(r *http.Request) integrations.Actor {
		switch r.Header.Get("X-Role") {
		case "admin":
			return integrations.Actor{Sub: "admin-sub", Name: "admin", Admin: true}
		case "grantee":
			return integrations.Actor{Sub: "grantee-sub", Name: "grantee"}
		}
		return integrations.Actor{}
	}
	s.svc.Session = func(r *http.Request) string {
		if role := r.Header.Get("X-Role"); role != "" {
			return "session-of-" + role + r.Header.Get("X-Session")
		}
		return ""
	}
	s.svc.CSRFKey = []byte("csrf-test-key")
	mux := http.NewServeMux()
	s.svc.Routes(mux, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Role") == "" {
				http.Redirect(w, r, "/auth/login", http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
		})
	})
	return mux, s
}

func do(mux *http.ServeMux, role, method, target string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, target, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if role != "" {
		req.Header.Set("X-Role", role)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

var csrfField = regexp.MustCompile(`name="csrf" value="([^"]+)"`)

func csrfOf(t *testing.T, mux *http.ServeMux) string {
	t.Helper()
	m := csrfField.FindStringSubmatch(do(mux, "admin", "GET", "/stats/integrations", nil).Body.String())
	if m == nil {
		t.Fatal("an admin gets a CSRF token")
	}
	return m[1]
}

func actions() []string {
	return []string{
		"google/connect", "google/select", "bing/connect", "bing/test", "bing/sync", "bing/remove", "bing/enable", "bing/disable",
		"indexnow/generate", "indexnow/notify", "indexnow/test", "cloudflare/connect", "cloudflare/sync",
	}
}

// TestIntegrationsAuthorization is the matrix: every route for an admin, a grantee and an anonymous request.
func TestIntegrationsAuthorization(t *testing.T) {
	mux, _ := integrationsMux(t)
	inline := regexp.MustCompile(`\sstyle=|<script>|<script [^>]*>[^<]|\son[a-z]+=`)
	for role, want := range map[string]int{"": http.StatusFound, "grantee": http.StatusOK, "admin": http.StatusOK} {
		rec := do(mux, role, "GET", "/stats/integrations", nil)
		body := rec.Body.String()
		if rec.Code != want {
			t.Fatalf("GET as %q: %d", role, rec.Code)
		}
		if role == "" {
			continue
		}
		if strings.Contains(body, "ZgotmplZ") || inline.MatchString(body) || rec.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%s: escaping or inline style/script under the CSP", role)
		}
		forms := strings.Count(body, `method="post" action="/stats/integrations/`)
		if role == "grantee" && (forms != 0 || strings.Contains(body, `name="csrf"`) || !strings.Contains(body, "Read-only") || !strings.Contains(body, "Not configured")) {
			t.Fatalf("a grantee sees no controls: %d forms", forms)
		}
		if role == "admin" && (forms < 4 || strings.Contains(body, "Read-only")) {
			t.Fatalf("an admin sees the controls: %d forms", forms)
		}
	}
	adminToken := csrfOf(t, mux)
	for _, a := range actions() {
		target := "/stats/integrations/" + a
		if rec := do(mux, "", "POST", target, url.Values{"csrf": {adminToken}}); rec.Code != http.StatusFound {
			t.Errorf("anonymous %s: %d", a, rec.Code)
		}
		if rec := do(mux, "grantee", "POST", target, url.Values{"csrf": {adminToken}}); rec.Code != http.StatusForbidden {
			t.Errorf("grantee %s: %d", a, rec.Code)
		}
		if rec := do(mux, "admin", "POST", target, url.Values{}); rec.Code != http.StatusForbidden {
			t.Errorf("admin without CSRF %s: %d", a, rec.Code)
		}
		if rec := do(mux, "admin", "POST", target, url.Values{"csrf": {adminToken}}); rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
			t.Errorf("admin %s: %d", a, rec.Code)
		}
	}
	for _, bad := range []string{"/stats/integrations/vault/test", "/stats/integrations/bing/explode", "/stats/integrations/bing/generate"} {
		if rec := do(mux, "admin", "POST", bad, url.Values{"csrf": {adminToken}}); rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	if rec := do(mux, "admin", "GET", "/stats/integrations/bing/test", nil); rec.Code == http.StatusSeeOther {
		t.Fatal("GET never changes anything")
	}
}

func TestIntegrationsCSRF(t *testing.T) {
	mux, _ := integrationsMux(t)
	token := csrfOf(t, mux)
	if token != csrfOf(t, mux) {
		t.Fatal("the token is stable within a session")
	}
	req := httptest.NewRequest("POST", "/stats/integrations/indexnow/generate", strings.NewReader(url.Values{"csrf": {token}}.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Role", "admin")
	req.Header.Set("X-Session", "-another-login")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "expired") {
		t.Fatalf("a token from another session is refused: %d", rec.Code)
	}
	for _, forged := range []string{"", "x", strings.Repeat("A", len(token))} {
		if rec := do(mux, "admin", "POST", "/stats/integrations/indexnow/generate", url.Values{"csrf": {forged}}); rec.Code != http.StatusForbidden {
			t.Errorf("forged %q: %d", forged, rec.Code)
		}
	}
	rec = do(mux, "admin", "POST", "/stats/integrations/indexnow/generate", url.Values{"csrf": {token}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/stats/integrations?k=indexnow&ok=generate#indexnow" {
		t.Fatalf("generate: %d %s", rec.Code, rec.Header().Get("Location"))
	}
}

func multipartBody(t *testing.T, fields map[string]string, file []byte) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	if file != nil {
		fw, _ := w.CreateFormFile("credential_file", "key.json")
		_, _ = fw.Write(file)
	}
	_ = w.Close()
	return &buf, w.FormDataContentType()
}

func TestIntegrationsFlows(t *testing.T) {
	mux, s := integrationsMux(t)
	token := csrfOf(t, mux)
	post := func(target string, form url.Values) *httptest.ResponseRecorder {
		form.Set("csrf", token)
		return do(mux, "admin", "POST", target, form)
	}
	upload := func(file []byte, paste string) string {
		body, ct := multipartBody(t, map[string]string{"csrf": token, "credential": paste}, file)
		req := httptest.NewRequest("POST", "/stats/integrations/google/connect", body)
		req.Header.Set("Content-Type", ct)
		req.Header.Set("X-Role", "admin")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec.Header().Get("Location")
	}
	if loc := upload(bytes.Repeat([]byte("x"), integrations.MaxCredential+1), ""); !strings.Contains(loc, "err=too_large") {
		t.Fatalf("a file over 64 KB is refused: %s", loc)
	}
	if loc := upload([]byte(`{"type":"authorized_user"}`), ""); !strings.Contains(loc, "err=not_service_account") {
		t.Fatalf("not a service account: %s", loc)
	}
	if loc := upload(nil, "{not json"); !strings.Contains(loc, "err=invalid_json") {
		t.Fatalf("pasted text is validated: %s", loc)
	}
	big := url.Values{"credential": {strings.Repeat("x", 3*integrations.MaxCredential)}}
	if loc := post("/stats/integrations/bing/connect", big).Header().Get("Location"); !strings.Contains(loc, "err=too_large") {
		t.Fatalf("an oversized body is refused: %s", loc)
	}

	// Bing: connect, pick from the listed sites, then remove with a second POST.
	rec := post("/stats/integrations/bing/connect", url.Values{"credential": {testBingKey}})
	if loc := rec.Header().Get("Location"); loc != "/stats/integrations?k=bing&ok=connect&pick=bing#bing" {
		t.Fatalf("connect redirects to the picker: %s", loc)
	}
	page := do(mux, "admin", "GET", "/stats/integrations?k=bing&ok=connect&pick=bing", nil).Body.String()
	for _, want := range []string{"Credential saved", `<option value="https://tribelt.jorisjonkers.dev/"`, "Pick a site", "Saved in the UI"} {
		if !strings.Contains(page, want) {
			t.Errorf("picker page lacks %q", want)
		}
	}
	if strings.Contains(page, testBingKey) {
		t.Fatal("the secret is never rendered back")
	}
	if loc := post("/stats/integrations/bing/select", url.Values{"target": {"https://evil.test/"}}).Header().Get("Location"); !strings.Contains(loc, "err=target_missing") {
		t.Fatalf("only a listed site: %s", loc)
	}
	if loc := post("/stats/integrations/bing/select", url.Values{"target": {"https://tribelt.jorisjonkers.dev/"}}).Header().Get("Location"); !strings.Contains(loc, "ok=select") {
		t.Fatalf("select: %s", loc)
	}
	grantee := do(mux, "grantee", "GET", "/stats/integrations", nil).Body.String()
	if !strings.Contains(grantee, "Connected") || strings.Contains(grantee, testBingKey) || strings.Contains(grantee, "History") {
		t.Fatal("a grantee sees the status, not the history")
	}
	search := do(mux, "grantee", "GET", "/stats/search?from=2026-06-01&to=2026-06-10", nil).Body.String()
	if !strings.Contains(search, "No Search Performance data for this range yet") || !strings.Contains(search, `href="/stats/integrations"`) {
		t.Fatal("a source saved in the UI counts as connected on the Search view")
	}
	rec = post("/stats/integrations/bing/remove", url.Values{})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Yes, remove it") {
		t.Fatalf("removal asks first: %d", rec.Code)
	}
	if loc := post("/stats/integrations/bing/remove", url.Values{"confirm": {"yes"}}).Header().Get("Location"); !strings.Contains(loc, "ok=remove") {
		t.Fatalf("remove: %s", loc)
	}
	if !strings.Contains(do(mux, "admin", "GET", "/stats/integrations?k=bing&err=%3Cscript%3E", nil).Body.String(), "Something went wrong.") {
		t.Fatal("an unknown error code renders a fixed text")
	}
	ev, err := s.q.ListIntegrationEvents(context.Background(), queries.ListIntegrationEventsParams{Kind: "bing", Lim: 10})
	if err != nil || len(ev) != 3 || ev[0].Action != "remove" || ev[0].ActorName != "admin" {
		t.Fatalf("audit %+v %v", ev, err)
	}
}
