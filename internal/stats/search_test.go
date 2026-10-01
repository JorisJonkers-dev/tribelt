package stats

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

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

// fakeGoogle serves an OAuth token endpoint and the searchAnalytics API.
func fakeGoogle(t *testing.T, fail *atomic.Bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.PostForm.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" || r.PostForm.Get("assertion") == "" {
			http.Error(w, "bad grant", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("POST /webmasters/v3/sites/{site}/searchAnalytics/query", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" || r.PathValue("site") != "sc-domain:jorisjonkers.dev" || fail.Load() {
			http.Error(w, `{"error":"denied"}`, http.StatusForbidden)
			return
		}
		var body struct {
			Dimensions []string `json:"dimensions"`
			StartDate  string   `json:"startDate"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if strings.Join(body.Dimensions, ",") != "date,page,query" || body.StartDate == "" {
			http.Error(w, "bad body", 400)
			return
		}
		_, _ = io.WriteString(w, `{"rows":[
			{"keys":["2026-09-28","https://tribelt.jorisjonkers.dev/sectoren","metalen transportbanden"],"clicks":3,"impressions":120,"ctr":0.025,"position":7.5},
			{"keys":["2026-09-29","https://tribelt.jorisjonkers.dev/","tribelt"],"clicks":1,"impressions":10,"ctr":0.1,"position":1.2},
			{"keys":["bad-date","x","y"],"clicks":1},
			{"keys":["2026-09-29"]}]}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func fakeBing(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /webmaster/api.svc/json/GetPageStats", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apikey") != "bing-key" || r.URL.Query().Get("siteUrl") != "https://tribelt.jorisjonkers.dev/" {
			http.Error(w, "denied", http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"d":[{"Query":"https://tribelt.jorisjonkers.dev/sectoren","Clicks":1,"Impressions":9},{"Query":"https://tribelt.jorisjonkers.dev/sectoren"},{"Query":"https://tribelt.jorisjonkers.dev/broken"}]}`)
	})
	mux.HandleFunc("GET /webmaster/api.svc/json/GetPageQueryStats", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Query().Get("page") {
		case "https://tribelt.jorisjonkers.dev/sectoren":
			_, _ = io.WriteString(w, `{"d":[
				{"Query":"transportband","Clicks":2,"Impressions":8,"AvgImpressionPosition":4.5,"Date":"/Date(1790985600000-0700)/"},
				{"Query":"old","Clicks":1,"Impressions":1,"AvgImpressionPosition":9,"Date":"/Date(1000000000000)/"},
				{"Query":"none","Clicks":0,"Impressions":0,"AvgImpressionPosition":0,"Date":"/Date(1790985600000)/"},
				{"Query":"nodate","Clicks":1,"Impressions":1,"Date":"soon"}]}`)
		default:
			_, _ = io.WriteString(w, `{"d":[]}`)
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func sources(t *testing.T) ([]Source, *atomic.Bool) {
	t.Helper()
	fail := &atomic.Bool{}
	g := fakeGoogle(t, fail)
	gsc, err := NewGSC(context.Background(), serviceAccount(t, g.URL+"/token"), "sc-domain:jorisjonkers.dev")
	if err != nil {
		t.Fatal(err)
	}
	gsc.BaseURL = g.URL
	b := fakeBing(t)
	return []Source{gsc, &Bing{HTTP: http.DefaultClient, BaseURL: b.URL, SiteURL: "https://tribelt.jorisjonkers.dev/", APIKey: "bing-key"}}, fail
}

func TestSourcesFetch(t *testing.T) {
	srcs, _ := sources(t)
	from, to := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC)
	g, err := srcs[0].Fetch(context.Background(), from, to)
	if err != nil || len(g) != 2 || g[0].Path != "/sectoren" || g[0].Impressions != 120 || g[0].Source != "google" || g[1].Path != "/" {
		t.Fatalf("gsc %+v %v", g, err)
	}
	b, err := srcs[1].Fetch(context.Background(), from, to)
	if err != nil || len(b) != 2 || b[0].Query != "transportband" || b[0].Ctr != 0.25 || b[0].Day.Format(time.DateOnly) != "2026-10-03" || b[0].Path != "/sectoren" || b[1].Ctr != 0 {
		t.Fatalf("bing rows %+v %v", b, err)
	}
	if srcs[0].Name() != "google" || srcs[1].Name() != "bing" {
		t.Fatal("names")
	}
}

func TestBingDate(t *testing.T) {
	d, ok := parseBingDate("/Date(1790985600000-0700)/")
	if !ok || d.Format(time.DateOnly) != "2026-10-03" {
		t.Fatalf("%v %v", d, ok)
	}
	if _, ok := parseBingDate("soon"); ok {
		t.Fatal("bad date")
	}
	if pathOf("https://x.test") != "/" || pathOf("::") != "/" || pathOf("https://x.test/a") != "/a" {
		t.Fatal("pathOf")
	}
}

type memStore struct {
	mu   sync.Mutex
	rows []queries.UpsertSearchPerformanceParams
	fail bool
}

func (m *memStore) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.rows)
}

func (m *memStore) UpsertSearchPerformance(_ context.Context, r queries.UpsertSearchPerformanceParams) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail {
		return errors.New("db down")
	}
	m.rows = append(m.rows, r)
	return nil
}

func TestImporter(t *testing.T) {
	srcs, fail := sources(t)
	store := &memStore{}
	im := &Importer{
		Sources: srcs, Store: store, Log: slog.New(slog.DiscardHandler), Lookback: 10,
		Now: func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) },
	}
	if err := im.ImportOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(store.rows) != 4 {
		t.Fatalf("rows %d: %+v", len(store.rows), store.rows)
	}
	fail.Store(true)
	if err := im.ImportOnce(context.Background()); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("a failing source is reported: %v", err)
	}
	fail.Store(false)
	store.fail = true
	if err := im.ImportOnce(context.Background()); err == nil {
		t.Fatal("store failure is reported")
	}
	store.fail = false
	store.rows = nil
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { im.Run(ctx, time.Millisecond, time.Hour); close(done) }()
	waitUntil(t, func() bool { return store.count() == 4 })
	cancel()
	<-done
	fail.Store(true)
	ctx, cancel = context.WithCancel(context.Background())
	done = make(chan struct{})
	go func() { im.Run(ctx, time.Millisecond, time.Hour); close(done) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	for deadline := time.Now().Add(5 * time.Second); !cond(); time.Sleep(5 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
	}
}

func TestImportIntoPostgres(t *testing.T) {
	s := seed(t)
	srcs, _ := sources(t)
	im := &Importer{
		Sources: srcs, Store: s.q, Log: slog.New(slog.DiscardHandler), Lookback: 10,
		Now: func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) },
	}
	for range 2 {
		if err := im.ImportOnce(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	rows, err := s.q.ListSearchPerformance(context.Background())
	if err != nil || len(rows) != 3+4 {
		t.Fatalf("upserts are idempotent: %d %v", len(rows), err)
	}
}

func TestNewGSCErrors(t *testing.T) {
	if _, err := NewGSC(context.Background(), []byte("{"), "x"); err == nil {
		t.Fatal("bad json")
	}
	g := &GSC{HTTP: http.DefaultClient, BaseURL: "http://127.0.0.1:1", SiteURL: "x"}
	if _, err := g.Fetch(context.Background(), time.Now(), time.Now()); err == nil {
		t.Fatal("unreachable")
	}
	b := &Bing{HTTP: http.DefaultClient, BaseURL: "http://127.0.0.1:1"}
	if _, err := b.Fetch(context.Background(), time.Now(), time.Now()); err == nil {
		t.Fatal("unreachable")
	}
	if err := doJSON(context.Background(), http.DefaultClient, "GET", "::", nil, nil); err == nil {
		t.Fatal("bad url")
	}
}

func TestFilter(t *testing.T) {
	n := now()
	f := ParseFilter(url.Values{}, n)
	if f.To.Format(time.DateOnly) != "2026-09-30" || f.From.Format(time.DateOnly) != "2026-09-01" || len(f.Days()) != 30 {
		t.Fatalf("default range %v..%v", f.From, f.To)
	}
	f = ParseFilter(url.Values{"from": {"2026-09-30"}, "to": {"2026-09-01"}, "locale": {"en"}, "release": {"v1"}, "internal": {"1"}, "path": {"/x"}}, n)
	if !f.From.Before(f.To) || f.Locale != "en" || !f.IncludeInternal || *f.release() != "v1" || *f.path() != "/x" {
		t.Fatalf("swapped range and options %+v", f)
	}
	if ParseFilter(url.Values{"locale": {"fr"}}, n).Locale != "" {
		t.Fatal("unknown locale ignored")
	}
	if got := ParseFilter(url.Values{"from": {"2000-01-01"}}, n); got.From.Year() != 2023 {
		t.Fatalf("range capped at three years: %v", got.From)
	}
	q, _ := url.ParseQuery(f.Query("path", "", "locale", "de"))
	if q.Get("path") != "" || q.Get("locale") != "de" || q.Get("internal") != "1" || q.Get("release") != "v1" {
		t.Fatalf("query %v", q)
	}
	if f.End().Sub(f.To) != 24*time.Hour {
		t.Fatal("end is the next midnight")
	}
	// DST: the last Sunday of October has 25 hours in Amsterdam.
	dst := ParseFilter(url.Values{"from": {"2026-10-24"}, "to": {"2026-10-26"}}, n)
	if len(dst.Days()) != 3 || dst.Days()[2].Hour() != 0 {
		t.Fatal("days stay on midnight across DST")
	}
}
