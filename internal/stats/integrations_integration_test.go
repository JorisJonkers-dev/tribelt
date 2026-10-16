//go:build integration

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
	"slices"
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
var inlineCode = regexp.MustCompile(`\sstyle=|<script>|<script [^>]*>[^<]|\son[a-z]+=`)

func TestAnAnonymousVisitorIsSentToSignInFromTheIntegrationsPage(t *testing.T) {
	mux, _ := integrationsMux(t)
	if rec := do(mux, "", "GET", "/stats/integrations", nil); rec.Code != http.StatusFound {
		t.Fatalf("GET = %d", rec.Code)
	}
}

func TestTheIntegrationsPageKeepsToTheCSPAndIsNeverCached(t *testing.T) {
	for _, role := range []string{"grantee", "admin"} {
		t.Run(role, func(t *testing.T) {
			mux, _ := integrationsMux(t)
			rec := do(mux, role, "GET", "/stats/integrations", nil)
			body := rec.Body.String()
			if rec.Code != http.StatusOK || strings.Contains(body, "ZgotmplZ") || inlineCode.MatchString(body) || rec.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("GET = %d, cache %q", rec.Code, rec.Header().Get("Cache-Control"))
			}
		})
	}
}

func TestAStatsViewerSeesTheIntegrationsReadOnly(t *testing.T) {
	mux, _ := integrationsMux(t)
	body := do(mux, "grantee", "GET", "/stats/integrations", nil).Body.String()
	forms := strings.Count(body, `method="post" action="/stats/integrations/`)
	if forms != 0 || strings.Contains(body, `name="csrf"`) || !strings.Contains(body, "Read-only") || !strings.Contains(body, "Not configured") {
		t.Fatalf("%d forms", forms)
	}
}

func TestAnAdminGetsTheIntegrationControls(t *testing.T) {
	mux, _ := integrationsMux(t)
	body := do(mux, "admin", "GET", "/stats/integrations", nil).Body.String()
	if forms := strings.Count(body, `method="post" action="/stats/integrations/`); forms < 4 || strings.Contains(body, "Read-only") {
		t.Fatalf("%d forms", forms)
	}
}

// eachAction posts to every integration action as role, with form, and wants status.
func eachAction(t *testing.T, role string, form func(token string) url.Values, want ...int) {
	t.Helper()
	for _, a := range actions() {
		t.Run(a, func(t *testing.T) {
			mux, _ := integrationsMux(t)
			rec := do(mux, role, "POST", "/stats/integrations/"+a, form(csrfOf(t, mux)))
			if !slices.Contains(want, rec.Code) {
				t.Fatalf("POST = %d, want %v", rec.Code, want)
			}
		})
	}
}

func withToken(token string) url.Values { return url.Values{"csrf": {token}} }

func TestAnAnonymousChangeIsSentToSignIn(t *testing.T) {
	eachAction(t, "", withToken, http.StatusFound)
}

func TestAStatsViewerCannotChangeAnIntegration(t *testing.T) {
	eachAction(t, "grantee", withToken, http.StatusForbidden)
}

func TestAnAdminChangeWithoutTheFormTokenIsRefused(t *testing.T) {
	eachAction(t, "admin", func(string) url.Values { return url.Values{} }, http.StatusForbidden)
}

func TestAnAdminChangeWithTheFormTokenGoesThrough(t *testing.T) {
	eachAction(t, "admin", withToken, http.StatusSeeOther, http.StatusOK)
}

func TestAnUnknownIntegrationOrActionIsNotFound(t *testing.T) {
	for _, bad := range []string{"/stats/integrations/vault/test", "/stats/integrations/bing/explode", "/stats/integrations/bing/generate"} {
		t.Run(bad, func(t *testing.T) {
			mux, _ := integrationsMux(t)
			if rec := do(mux, "admin", "POST", bad, withToken(csrfOf(t, mux))); rec.Code != http.StatusNotFound {
				t.Fatalf("POST = %d", rec.Code)
			}
		})
	}
}

func TestReadingAnActionNeverChangesAnything(t *testing.T) {
	mux, _ := integrationsMux(t)
	if rec := do(mux, "admin", "GET", "/stats/integrations/bing/test", nil); rec.Code == http.StatusSeeOther {
		t.Fatal("GET changed something")
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

// flow is an admin's session on the integrations pages.
type flow struct {
	mux   *http.ServeMux
	s     seeded
	token string
}

func newFlow(t *testing.T) *flow {
	t.Helper()
	mux, s := integrationsMux(t)
	return &flow{mux: mux, s: s, token: csrfOf(t, mux)}
}

func (f *flow) post(target string, form url.Values) *httptest.ResponseRecorder {
	form.Set("csrf", f.token)
	return do(f.mux, "admin", "POST", target, form)
}

// upload connects Google with file uploaded and paste pasted, and returns where it redirected.
func (f *flow) upload(t *testing.T, file []byte, paste string) string {
	t.Helper()
	body, ct := multipartBody(t, map[string]string{"csrf": f.token, "credential": paste}, file)
	req := httptest.NewRequest("POST", "/stats/integrations/google/connect", body)
	req.Header.Set("Content-Type", ct)
	req.Header.Set("X-Role", "admin")
	rec := httptest.NewRecorder()
	f.mux.ServeHTTP(rec, req)
	return rec.Header().Get("Location")
}

// bing is a flow that connected Bing with the test key.
func bing(t *testing.T) (*flow, *httptest.ResponseRecorder) {
	t.Helper()
	f := newFlow(t)
	return f, f.post("/stats/integrations/bing/connect", url.Values{"credential": {testBingKey}})
}

// pickedBing is a flow that connected Bing and picked the mirror's site.
func pickedBing(t *testing.T) *flow {
	t.Helper()
	f, _ := bing(t)
	if loc := f.post("/stats/integrations/bing/select", url.Values{"target": {"https://tribelt.jorisjonkers.dev/"}}).Header().Get("Location"); !strings.Contains(loc, "ok=select") {
		t.Fatalf("select: %s", loc)
	}
	return f
}

func TestAnUnusableUploadedCredentialIsRefused(t *testing.T) {
	for _, tc := range []struct {
		name, paste, err string
		file             []byte
	}{
		{"over 64 KB", "", "err=too_large", bytes.Repeat([]byte("x"), integrations.MaxCredential+1)},
		{"not a service account", "", "err=not_service_account", []byte(`{"type":"authorized_user"}`)},
		{"pasted text that is not JSON", "{not json", "err=invalid_json", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if loc := newFlow(t).upload(t, tc.file, tc.paste); !strings.Contains(loc, tc.err) {
				t.Fatalf("redirected to %s", loc)
			}
		})
	}
}

func TestAnOversizedFormIsRefused(t *testing.T) {
	big := url.Values{"credential": {strings.Repeat("x", 3*integrations.MaxCredential)}}
	if loc := newFlow(t).post("/stats/integrations/bing/connect", big).Header().Get("Location"); !strings.Contains(loc, "err=too_large") {
		t.Fatalf("redirected to %s", loc)
	}
}

func TestConnectingBingLeadsToTheSitePicker(t *testing.T) {
	if _, rec := bing(t); rec.Header().Get("Location") != "/stats/integrations?k=bing&ok=connect&pick=bing#bing" {
		t.Fatalf("redirected to %s", rec.Header().Get("Location"))
	}
}

func TestThePickerListsTheSitesButNeverTheSecret(t *testing.T) {
	f, _ := bing(t)
	page := do(f.mux, "admin", "GET", "/stats/integrations?k=bing&ok=connect&pick=bing", nil).Body.String()
	for _, want := range []string{"Credential saved", `<option value="https://tribelt.jorisjonkers.dev/"`, "Pick a site", "Saved in the UI"} {
		if !strings.Contains(page, want) {
			t.Errorf("picker lacks %q", want)
		}
	}
	if strings.Contains(page, testBingKey) {
		t.Fatal("the secret is rendered")
	}
}

func TestOnlyAListedSiteCanBePicked(t *testing.T) {
	f, _ := bing(t)
	if loc := f.post("/stats/integrations/bing/select", url.Values{"target": {"https://evil.test/"}}).Header().Get("Location"); !strings.Contains(loc, "err=target_missing") {
		t.Fatalf("redirected to %s", loc)
	}
}

func TestAStatsViewerSeesAConnectedSourceButNotItsSecretOrHistory(t *testing.T) {
	f := pickedBing(t)
	grantee := do(f.mux, "grantee", "GET", "/stats/integrations", nil).Body.String()
	if !strings.Contains(grantee, "Connected") || strings.Contains(grantee, testBingKey) || strings.Contains(grantee, "History") {
		t.Fatal(grantee)
	}
}

func TestASourceSavedInTheUICountsAsConnectedOnTheSearchView(t *testing.T) {
	f := pickedBing(t)
	search := do(f.mux, "grantee", "GET", "/stats/search?from=2026-06-01&to=2026-06-10", nil).Body.String()
	if !strings.Contains(search, "No Search Performance data for this range yet") || !strings.Contains(search, `href="/stats/integrations"`) {
		t.Fatal(search)
	}
}

func TestRemovingASourceAsksFirst(t *testing.T) {
	f := pickedBing(t)
	if rec := f.post("/stats/integrations/bing/remove", url.Values{}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Yes, remove it") {
		t.Fatalf("remove = %d", rec.Code)
	}
}

func TestAConfirmedRemovalRemovesTheSource(t *testing.T) {
	f := pickedBing(t)
	if loc := f.post("/stats/integrations/bing/remove", url.Values{"confirm": {"yes"}}).Header().Get("Location"); !strings.Contains(loc, "ok=remove") {
		t.Fatalf("redirected to %s", loc)
	}
}

func TestAnUnknownErrorCodeShowsAFixedText(t *testing.T) {
	f := newFlow(t)
	if !strings.Contains(do(f.mux, "admin", "GET", "/stats/integrations?k=bing&err=%3Cscript%3E", nil).Body.String(), "Something went wrong.") {
		t.Fatal("no fixed text")
	}
}

func TestEachChangeToASourceIsLoggedWithItsActor(t *testing.T) {
	f := pickedBing(t)
	f.post("/stats/integrations/bing/remove", url.Values{"confirm": {"yes"}})
	ev, err := f.s.q.ListIntegrationEvents(context.Background(), queries.ListIntegrationEventsParams{Kind: "bing", Lim: 10})
	if err != nil || len(ev) != 3 || ev[0].Action != "remove" || ev[0].ActorName != "admin" {
		t.Fatalf("audit %+v %v", ev, err)
	}
}
