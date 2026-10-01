package integrations

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

const (
	bingKey  = "bing-test-key-aaaaaaaaaaaa"
	cfToken  = "cloudflare-test-token-aaaaaaaa"
	cfZone   = "0123456789abcdef0123456789abcdef"
	property = "sc-domain:jorisjonkers.dev"
	bingSite = "https://tribelt.jorisjonkers.dev/"
)

// fake is every provider at once: Google (token, sites, searchAnalytics), Bing, IndexNow and Cloudflare.
type fake struct {
	srv *httptest.Server
	mu  sync.Mutex
	// What the providers saw.
	gscStarts []string
	submitted []map[string]any
	queries   []string
	// Behaviour switches.
	googleDown   atomic.Bool
	indexNowCode atomic.Int32
	cfNoSettings atomic.Bool
	cfInactive   atomic.Bool
	cfZoneName   atomic.Value
}

func serviceAccount(t *testing.T, tokenURL string) []byte {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(key)
	raw, _ := json.Marshal(map[string]string{
		"type": "service_account", "client_email": "stats@example.iam.gserviceaccount.com", "private_key_id": "k1",
		"private_key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "token_uri": tokenURL,
	})
	return raw
}

func write(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.WriteString(w, body)
}

func newFake(t *testing.T) *fake {
	t.Helper()
	f := &fake{}
	f.cfZoneName.Store("jorisjonkers.dev")
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("assertion") == "" {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		write(w, `{"access_token":"tok","token_type":"Bearer","expires_in":3600}`)
	})
	google := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer tok" || f.googleDown.Load() {
				http.Error(w, `{"error":"denied"}`, http.StatusForbidden)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /webmasters/v3/sites", google(func(w http.ResponseWriter, _ *http.Request) {
		write(w, `{"siteEntry":[{"siteUrl":"`+property+`","permissionLevel":"siteRestrictedUser"},{"siteUrl":"https://unverified.test/","permissionLevel":"siteUnverifiedUser"}]}`)
	}))
	mux.HandleFunc("POST /webmasters/v3/sites/{site}/searchAnalytics/query", google(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			StartDate  string   `json:"startDate"`
			Dimensions []string `json:"dimensions"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.PathValue("site") != property || strings.Join(body.Dimensions, ",") != "date,page,query" {
			http.Error(w, "bad", 400)
			return
		}
		f.mu.Lock()
		f.gscStarts = append(f.gscStarts, body.StartDate)
		f.mu.Unlock()
		write(w, `{"rows":[
			{"keys":["2026-10-02","https://tribelt.jorisjonkers.dev/sectoren","metalen transportbanden"],"clicks":3,"impressions":120,"ctr":0.025,"position":7.5},
			{"keys":["2026-10-03","https://tribelt.jorisjonkers.dev/","tribelt"],"clicks":1,"impressions":10,"ctr":0.1,"position":1.2},
			{"keys":["bad-date","x","y"]},{"keys":["2026-10-03"]}]}`)
	}))
	bing := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("apikey") != bingKey {
				http.Error(w, "denied", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /webmaster/api.svc/json/GetUserSites", bing(func(w http.ResponseWriter, _ *http.Request) {
		write(w, `{"d":[{"Url":"`+bingSite+`","IsVerified":true},{"Url":"https://pending.test/","IsVerified":false}]}`)
	}))
	mux.HandleFunc("GET /webmaster/api.svc/json/GetPageStats", bing(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("siteUrl") != bingSite {
			http.Error(w, "unknown site", 400)
			return
		}
		write(w, `{"d":[{"Query":"https://tribelt.jorisjonkers.dev/sectoren"},{"Query":"https://tribelt.jorisjonkers.dev/sectoren"},{"Query":"https://tribelt.jorisjonkers.dev/broken"}]}`)
	}))
	mux.HandleFunc("GET /webmaster/api.svc/json/GetPageQueryStats", bing(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") != "https://tribelt.jorisjonkers.dev/sectoren" {
			write(w, `{"d":[]}`)
			return
		}
		write(w, `{"d":[
			{"Query":"transportband","Clicks":2,"Impressions":8,"AvgImpressionPosition":4.5,"Date":"/Date(1790985600000-0700)/"},
			{"Query":"old","Clicks":1,"Impressions":1,"Date":"/Date(1000000000000)/"},
			{"Query":"none","Date":"/Date(1790985600000)/"},
			{"Query":"nodate","Date":"soon"}]}`)
	}))
	mux.HandleFunc("POST /indexnow", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.submitted = append(f.submitted, body)
		f.mu.Unlock()
		code := int(f.indexNowCode.Load())
		if code == 0 {
			code = http.StatusAccepted
		}
		w.WriteHeader(code)
	})
	cf := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+cfToken {
				http.Error(w, `{"success":false}`, http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	mux.HandleFunc("GET /client/v4/user/tokens/verify", cf(func(w http.ResponseWriter, _ *http.Request) {
		status := "active"
		if f.cfInactive.Load() {
			status = "disabled"
		}
		write(w, `{"success":true,"result":{"id":"t1","status":"`+status+`"}}`)
	}))
	mux.HandleFunc("GET /client/v4/zones/{id}", cf(func(w http.ResponseWriter, r *http.Request) {
		if r.PathValue("id") != cfZone {
			http.Error(w, `{"success":false}`, http.StatusNotFound)
			return
		}
		write(w, `{"success":true,"result":{"id":"`+cfZone+`","name":"`+f.cfZoneName.Load().(string)+`","plan":{"name":"Free Website"}}}`)
	}))
	mux.HandleFunc("POST /client/v4/graphql", cf(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.queries = append(f.queries, body.Query)
		f.mu.Unlock()
		if body.Variables["zone"] != cfZone {
			write(w, `{"data":{"viewer":{"zones":[]}},"errors":null}`)
			return
		}
		switch {
		case strings.Contains(body.Query, "settings"):
			if f.cfNoSettings.Load() {
				write(w, `{"data":null,"errors":[{"message":"settings unavailable"}]}`)
				return
			}
			write(w, `{"data":{"viewer":{"zones":[{"settings":{"httpRequestsAdaptiveGroups":{"enabled":true,"maxDuration":86400,"notOlderThan":691200,"maxPageSize":10000,
				"availableFields":["count","dimensions.date","dimensions.userAgent","dimensions.clientRequestPath","dimensions.edgeResponseStatus"]}}}]}}}`)
		case strings.Contains(body.Query, "verifiedBotCategory"):
			write(w, `{"data":null,"errors":[{"message":"zone does not have access to the field verifiedBotCategory"}]}`)
		case strings.Contains(body.Query, "userAgent"):
			write(w, `{"data":{"viewer":{"zones":[{"httpRequestsAdaptiveGroups":[
				{"count":40,"dimensions":{"userAgent":"Mozilla/5.0 (compatible; GPTBot/1.2; +https://openai.com/gptbot)"}},
				{"count":5,"dimensions":{"userAgent":"Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; ClaudeBot/1.0)"}},
				{"count":100,"dimensions":{"userAgent":"Mozilla/5.0 Chrome/140.0 Safari/537.36"}}]}]}}}`)
		case strings.Contains(body.Query, "clientRequestPath"):
			write(w, `{"data":{"viewer":{"zones":[{"httpRequestsAdaptiveGroups":[
				{"count":90,"dimensions":{"clientRequestPath":"/sectoren"}},{"count":50,"dimensions":{"clientRequestPath":"/static/site.css"}},
				{"count":5,"dimensions":{"clientRequestPath":"/b"}}]}]}}}`)
		case strings.Contains(body.Query, "edgeResponseStatus"):
			write(w, `{"data":{"viewer":{"zones":[{"httpRequestsAdaptiveGroups":[{"count":100,"dimensions":{"edgeResponseStatus":200}},{"count":45,"dimensions":{"edgeResponseStatus":403}}]}]}}}`)
		default:
			write(w, `{"data":{"viewer":{"zones":[{"httpRequestsAdaptiveGroups":[{"count":145,"dimensions":{"date":"x"}}]}]}}}`)
		}
	}))
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fake) endpoints() Endpoints {
	return Endpoints{Google: f.srv.URL, Bing: f.srv.URL, IndexNow: f.srv.URL + "/indexnow", Cloudflare: f.srv.URL}
}

// clock is a settable time source.
type clock struct{ t atomic.Pointer[time.Time] }

func (c *clock) now() time.Time  { return *c.t.Load() }
func (c *clock) set(t time.Time) { c.t.Store(&t) }

type rig struct {
	m     *Manager
	f     *fake
	q     *queries.Queries
	pool  *pgxpool.Pool
	clock *clock
	admin Actor
}

func newRig(t *testing.T, env Env) *rig {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	f := newFake(t)
	c := &clock{}
	c.set(time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC))
	q := queries.New(pool)
	m := &Manager{
		Q: q, Sealer: sealer(t, testKey), Env: env, Endpoints: f.endpoints(), HTTP: f.srv.Client(),
		BaseURL: "https://tribelt.jorisjonkers.dev", Launch: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
		PageURLs: func() []string {
			return []string{"https://tribelt.jorisjonkers.dev/", "https://tribelt.jorisjonkers.dev/sectoren", "https://elsewhere.test/x"}
		},
		Log: slog.New(slog.DiscardHandler), Now: c.now,
	}
	return &rig{m: m, f: f, q: q, pool: pool, clock: c, admin: Actor{Sub: "sub-1", Name: "joris", Admin: true}}
}

func (r *rig) card(t *testing.T, k Kind) Card {
	t.Helper()
	v, err := r.m.View(context.Background(), true, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range v.Cards {
		if c.Kind == k {
			return c
		}
	}
	t.Fatalf("no card %s", k)
	return Card{}
}
