package hits

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

const (
	chrome = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/140.0.0.0 Safari/537.36"
	gptbot = "Mozilla/5.0 AppleWebKit/537.36 (KHTML, like Gecko; compatible; GPTBot/1.2; +https://openai.com/gptbot)"
)

// memWriter keeps what the recorder writes.
type memWriter struct {
	mu   sync.Mutex
	ops  []Op
	fail error
	// unmatched beacons are reported back this many times
	unmatchedTimes int
}

func (m *memWriter) Write(_ context.Context, ops []Op) ([]Op, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.fail != nil {
		return nil, m.fail
	}
	var unmatched []Op
	for _, op := range ops {
		if op.Beacon != nil && m.unmatchedTimes > 0 {
			m.unmatchedTimes--
			unmatched = append(unmatched, op)
			continue
		}
		m.ops = append(m.ops, op)
	}
	return unmatched, nil
}

func (m *memWriter) snapshot() []Op {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Op(nil), m.ops...)
}

func newTracker(t *testing.T) *Tracker {
	t.Helper()
	ranges, err := visits.BundledRanges()
	if err != nil {
		t.Fatal(err)
	}
	rec := NewRecorder(&memWriter{}, slog.New(slog.DiscardHandler), 100)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	return &Tracker{
		Recorder: rec, Ranges: ranges, Hasher: visits.NewDailyHasher([]byte("k")), Release: "v1-test", AppVersion: "0.3.0", Host: "mirror.test",
		SecureCookies: true, Now: func() time.Time { return now }, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 1024)),
		Internal: func(r *http.Request) bool { return r.Header.Get("X-Test-Internal") == "1" },
		Resolve: func(p string) (string, string, bool) {
			if p == "/sectoren" {
				return "sectoren", "nl", true
			}
			return "", "", false
		},
	}
}

func page(w http.ResponseWriter, r *http.Request) {
	info := InfoFrom(r.Context())
	info.PageID, info.Locale = "sectoren", "nl"
	_, _ = io.WriteString(w, "ok "+info.HitID.String())
}

func request(t *Tracker, target string, header map[string]string, cookies ...*http.Cookie) (*httptest.ResponseRecorder, Op) {
	req := httptest.NewRequest("GET", target, nil)
	req.RemoteAddr = "192.0.2.1:1234"
	for k, v := range header {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	t.Middleware(http.HandlerFunc(page)).ServeHTTP(rec, req)
	var op Op
	select {
	case op = <-t.Recorder.ch:
	default:
	}
	return rec, op
}

func visitorCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == visits.VisitorCookie {
			return c
		}
	}
	return nil
}

func TestHumanHitGetsVisitorCookie(t *testing.T) {
	tr := newTracker(t)
	rec, op := request(tr, "/sectoren?utm_source=newsletter&utm_medium=email", map[string]string{
		"User-Agent": chrome, "Referer": "https://www.google.nl/search?q=x", "CF-Connecting-IP": "198.51.100.4", "CF-IPCountry": "nl",
	})
	c := visitorCookie(rec)
	if c == nil || !c.HttpOnly || !c.Secure || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" || c.MaxAge != 395*24*3600 {
		t.Fatalf("visitor cookie %+v", c)
	}
	h := op.Hit
	if h == nil || h.VisitorKind != "human-unconfirmed" || *h.ArrivalChannel != "campaign" || *h.ReferrerHost != "google.nl" || *h.UtmSource != "newsletter" {
		t.Fatalf("hit %+v", h)
	}
	if *h.VisitorID != c.Value || *h.Country != "NL" || h.Status != 200 || h.Format != "html" || *h.PageID != "sectoren" || h.ReleaseLabel != "v1-test" {
		t.Fatalf("hit %+v", h)
	}
	if h.Verified != nil || h.BotName != nil || h.Internal || len(h.DailyHash) != 32 {
		t.Fatalf("hit %+v", h)
	}
	if !strings.Contains(rec.Body.String(), h.ID.String()) {
		t.Fatal("handler sees the same hit id")
	}
	if strings.Contains(h.UserAgent, "198.51.100.4") || strings.Contains(h.DailyHash, "198") {
		t.Fatal("raw IP is never stored")
	}
}

func TestExistingCookieReused(t *testing.T) {
	tr := newTracker(t)
	existing := &http.Cookie{Name: visits.VisitorCookie, Value: "AAAAAAAAAAAAAAAAAAAAAA"}
	rec, op := request(tr, "/sectoren", map[string]string{"User-Agent": chrome}, existing)
	if visitorCookie(rec) != nil || *op.Hit.VisitorID != existing.Value || *op.Hit.ArrivalChannel != "direct" {
		t.Fatal("valid cookie is reused, never extended")
	}
	rec, op = request(tr, "/sectoren", map[string]string{"User-Agent": chrome}, &http.Cookie{Name: visits.VisitorCookie, Value: "forged"})
	if c := visitorCookie(rec); c == nil || *op.Hit.VisitorID != c.Value || c.Value == "forged" {
		t.Fatal("invalid cookie is replaced")
	}
}

func TestNoCookieForBotsGPCAndDNT(t *testing.T) {
	tr := newTracker(t)
	cases := []map[string]string{
		{"User-Agent": chrome, "Sec-GPC": "1"},
		{"User-Agent": chrome, "DNT": "1"},
		{"User-Agent": gptbot, "CF-Connecting-IP": "132.196.86.5"},
		{"User-Agent": "curl/8.7.1"},
	}
	for _, hdr := range cases {
		rec, op := request(tr, "/sectoren", hdr, &http.Cookie{Name: visits.VisitorCookie, Value: "AAAAAAAAAAAAAAAAAAAAAA"})
		if visitorCookie(rec) != nil || op.Hit.VisitorID != nil || op.Hit.DailyHash == "" {
			t.Fatalf("%v: must fall back to the Daily Visitor hash only", hdr)
		}
	}
}

func TestCrawlerHit(t *testing.T) {
	tr := newTracker(t)
	_, op := request(tr, "/sectoren", map[string]string{"User-Agent": gptbot, "CF-Connecting-IP": "132.196.86.5", "Referer": "https://example.com/"})
	h := op.Hit
	if h.VisitorKind != "ai-crawler" || *h.BotName != "GPTBot" || !*h.Verified || h.ArrivalChannel != nil || *h.ReferrerHost != "example.com" {
		t.Fatalf("verified crawler %+v", h)
	}
	_, op = request(tr, "/sectoren", map[string]string{"User-Agent": gptbot, "CF-Connecting-IP": "203.0.113.9"})
	if op.Hit.VisitorKind != "other-bot" || *op.Hit.Verified {
		t.Fatalf("spoofed crawler %+v", op.Hit)
	}
	_, op = request(tr, "/sectoren", map[string]string{"User-Agent": "Mozilla/5.0 (compatible; AhrefsBot/7.0)"})
	if op.Hit.VisitorKind != "seo-tool" || op.Hit.Verified != nil {
		t.Fatalf("unverifiable crawler %+v", op.Hit)
	}
}

func TestInternalAndStatus(t *testing.T) {
	tr := newTracker(t)
	_, op := request(tr, "/sectoren", map[string]string{"User-Agent": chrome, "X-Test-Internal": "1", "Referer": "https://mirror.test/de"})
	if !op.Hit.Internal || *op.Hit.ArrivalChannel != "internal" {
		t.Fatalf("internal %+v", op.Hit)
	}
	req := httptest.NewRequest("GET", "/x", nil)
	rec := httptest.NewRecorder()
	tr.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		InfoFrom(r.Context()).Format = "txt"
		w.WriteHeader(404)
		w.WriteHeader(500)
	})).ServeHTTP(rec, req)
	op = <-tr.Recorder.ch
	if op.Hit.Status != 404 || op.Hit.Format != "txt" || op.Hit.PageID != nil {
		t.Fatalf("status %+v", op.Hit)
	}
	tr.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), req)
	if op = <-tr.Recorder.ch; op.Hit.Status != 200 {
		t.Fatal("no write means 200")
	}
	tr.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { InfoFrom(r.Context()).Skip = true })).ServeHTTP(httptest.NewRecorder(), req)
	if len(tr.Recorder.ch) != 0 {
		t.Fatal("skipped requests are not recorded")
	}
	sw := &statusWriter{ResponseWriter: httptest.NewRecorder()}
	if sw.Unwrap() == nil {
		t.Fatal("unwrap")
	}
}

func TestClientIP(t *testing.T) {
	cases := []struct{ cf, remote, want string }{
		{"198.51.100.4", "10.0.0.1:1", "198.51.100.4"},
		{"::ffff:198.51.100.4", "10.0.0.1:1", "198.51.100.4"},
		{"", "10.0.0.1:1", "10.0.0.1"},
		{"garbage", "[2001:db8::1]:443", "2001:db8::1"},
		{"", "10.0.0.2", "10.0.0.2"},
	}
	for _, c := range cases {
		req := httptest.NewRequest("GET", "/", nil)
		req.RemoteAddr = c.remote
		if c.cf != "" {
			req.Header.Set("CF-Connecting-IP", c.cf)
		}
		if got := ClientIP(req); got != netip.MustParseAddr(c.want) {
			t.Fatalf("ClientIP(%q, %q) = %s", c.cf, c.remote, got)
		}
	}
}

func TestBeacon(t *testing.T) {
	tr := newTracker(t)
	id := uuid.New()
	post := func(body string) int {
		rec := httptest.NewRecorder()
		tr.Beacon(rec, httptest.NewRequest("POST", "/b", strings.NewReader(body)))
		return rec.Code
	}
	if post(`{"h":"`+id.String()+`","e":99999999999}`) != 204 {
		t.Fatal("beacon answers 204")
	}
	op := <-tr.Recorder.ch
	if op.Beacon.ID != id || op.Beacon.EngagedMs != 6*3600*1000 || !op.Beacon.NotBefore.Equal(tr.Now().Add(-24*time.Hour)) {
		t.Fatalf("beacon %+v", op.Beacon)
	}
	post(`{"h":"` + id.String() + `","e":-5}`)
	if op = <-tr.Recorder.ch; op.Beacon.EngagedMs != 0 {
		t.Fatal("negative engagement clamps to 0")
	}
	for _, bad := range []string{"", "not json", `{"h":"nope"}`, strings.Repeat("x", 5000)} {
		if post(bad) != 204 || len(tr.Recorder.ch) != 0 {
			t.Fatalf("bad beacon %q must be ignored quietly", bad)
		}
	}
}

func TestGo(t *testing.T) {
	tr := newTracker(t)
	allowed := func(s string) bool { return strings.HasPrefix(s, "https://www.tribelt.nl/") }
	h := tr.Go(allowed)
	req := httptest.NewRequest("GET", "/go?to=https%3A%2F%2Fwww.tribelt.nl%2Fcontact&from=%2Fsectoren", nil)
	req.Header.Set("User-Agent", chrome)
	req.AddCookie(&http.Cookie{Name: visits.VisitorCookie, Value: "AAAAAAAAAAAAAAAAAAAAAA"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 302 || rec.Header().Get("Location") != "https://www.tribelt.nl/contact" || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("go %d %v", rec.Code, rec.Header())
	}
	c := (<-tr.Recorder.ch).Click
	if c.Target != "https://www.tribelt.nl/contact" || *c.PageID != "sectoren" || *c.Locale != "nl" || *c.VisitorID != "AAAAAAAAAAAAAAAAAAAAAA" || c.VisitorKind != "human-unconfirmed" {
		t.Fatalf("click %+v", c)
	}
	if visitorCookie(rec) != nil {
		t.Fatal("/go never issues a cookie")
	}
	req = httptest.NewRequest("GET", "/go?to=https%3A%2F%2Fwww.tribelt.nl%2F&from=%2Funknown", nil)
	req.Header.Set("User-Agent", gptbot)
	h.ServeHTTP(httptest.NewRecorder(), req)
	if c = (<-tr.Recorder.ch).Click; c.PageID != nil || c.VisitorKind != "other-bot" || c.Verified == nil {
		t.Fatalf("bot click %+v", c)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/go?to=https%3A%2F%2Fevil.example%2F", nil))
	if rec.Code != 400 || len(tr.Recorder.ch) != 0 {
		t.Fatal("off-site targets are refused and not recorded")
	}
}

func TestRandFailureSkipsCookie(t *testing.T) {
	tr := newTracker(t)
	tr.Rand = bytes.NewReader(nil)
	rec, op := request(tr, "/sectoren", map[string]string{"User-Agent": chrome})
	if visitorCookie(rec) != nil || op.Hit.VisitorID != nil {
		t.Fatal("no entropy, no cookie")
	}
	if DefaultRand() == nil {
		t.Fatal("default rand")
	}
}
