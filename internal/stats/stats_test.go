package stats

import (
	"context"
	"database/sql"
	"encoding/csv"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/web"
)

// Fixed clock: noon on 30 Sept 2026, Amsterdam.
func now() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, amsterdam()) }

func at(daysAgo int) time.Time { return now().AddDate(0, 0, -daysAgo) }

type seeded struct {
	svc  *Service
	pool *pgxpool.Pool
	q    *queries.Queries
}

func strp(s string) *string { return &s }

// seed builds two Content Releases and a small, known set of Hits, clicks and search figures.
func seed(t *testing.T) seeded {
	t.Helper()
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	q := queries.New(pool)
	for _, r := range []struct {
		label string
		ago   int
	}{{"v1-baseline", 10}, {"v2-longtail", 3}} {
		if _, err := q.UpsertRelease(ctx, queries.UpsertReleaseParams{Label: r.label, Note: "note " + r.label, ContentHash: "h"}); err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, `UPDATE releases SET first_seen_at = $1 WHERE label = $2`, at(r.ago), r.label); err != nil {
			t.Fatal(err)
		}
		err := q.UpsertPage(ctx, queries.UpsertPageParams{ReleaseLabel: r.label, Path: "/sectoren", PageID: "sectoren", Locale: "nl", Type: "hub", Title: "Title " + r.label, Description: "d", H1: "h", Keywords: []string{"a"}, WordCount: 100, ContentHash: r.label})
		if err != nil {
			t.Fatal(err)
		}
	}
	add := func(ago int, rel, path, kind, format string, mutate func(*queries.InsertHitParams)) {
		h := queries.InsertHitParams{
			ID: uuid.New(), Ts: at(ago), Path: path, PageID: strp("sectoren"), Locale: strp("nl"), ReleaseLabel: rel, Format: format,
			Status: 200, VisitorKind: kind, UserAgent: "Mozilla/5.0 Chrome/140.0 Safari/537.36", DailyHash: "d" + path,
		}
		if mutate != nil {
			mutate(&h)
		}
		if err := q.InsertHit(ctx, h); err != nil {
			t.Fatal(err)
		}
	}
	human := func(visitor, channel, host string) func(*queries.InsertHitParams) {
		return func(h *queries.InsertHitParams) {
			h.VisitorID, h.ArrivalChannel, h.ReferrerHost = &visitor, &channel, strp(host)
		}
	}
	add(9, "v1-baseline", "/sectoren", "human", "html", human("v-a", "search", "google.nl"))
	add(4, "v1-baseline", "/sectoren", "human", "html", human("v-a", "direct", ""))
	add(2, "v2-longtail", "/sectoren", "human-unconfirmed", "html", human("v-b", "ai-chat", "chatgpt.com"))
	add(2, "v2-longtail", "/sectoren", "human", "html", func(h *queries.InsertHitParams) {
		human("v-c", "social", "linkedin.com")(h)
		h.Internal = true
	})
	confirmed := uuid.New()
	add(1, "v2-longtail", "/sectoren", "human-unconfirmed", "html", func(h *queries.InsertHitParams) {
		h.ID, h.ArrivalChannel = confirmed, strp("referral")
	})
	if n, err := q.ConfirmBeacon(ctx, queries.ConfirmBeaconParams{ID: confirmed, EngagedMs: 8000, NotBefore: at(5)}); err != nil || n != 1 {
		t.Fatalf("beacon: %d %v", n, err)
	}
	bot := func(name string, verified bool) func(*queries.InsertHitParams) {
		return func(h *queries.InsertHitParams) { h.BotName, h.Verified, h.UserAgent = &name, &verified, name+"/1.0" }
	}
	add(2, "v2-longtail", "/sectoren", "ai-crawler", "md", bot("GPTBot", true))
	add(1, "v2-longtail", "/llms.txt", "ai-crawler", "txt", func(h *queries.InsertHitParams) { bot("ClaudeBot", true)(h); h.PageID, h.Locale = nil, nil })
	add(1, "v2-longtail", "/sectoren", "ai-fetcher", "md", bot("ChatGPT-User", true))
	add(5, "v1-baseline", "/sectoren", "search-crawler", "html", bot("Googlebot", true))
	add(5, "v1-baseline", "/sitemap.xml", "other-bot", "xml", func(h *queries.InsertHitParams) { bot("Googlebot", false)(h); h.PageID = nil })
	add(40, "v1-baseline", "/sectoren", "human", "html", human("v-old", "search", "bing.com"))

	for _, c := range []struct {
		ago      int
		rel, who string
		internal bool
	}{{4, "v1-baseline", "human", false}, {1, "v2-longtail", "human-unconfirmed", false}, {1, "v2-longtail", "human", true}} {
		err := q.InsertOutboundClick(ctx, queries.InsertOutboundClickParams{
			ID: uuid.New(), Ts: at(c.ago), FromPath: "/sectoren", PageID: strp("sectoren"), Locale: strp("nl"), ReleaseLabel: c.rel,
			Target: "https://www.tribelt.nl/contact", VisitorKind: c.who, VisitorID: strp("v-a"), DailyHash: "d", Internal: c.internal,
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []PerfRow{
		{Source: "google", Day: at(8), Page: "https://mirror.test/sectoren", Path: "/sectoren", Query: "metalen transportbanden", Clicks: 2, Impressions: 40, Ctr: 0.05, Position: 8},
		{Source: "google", Day: at(2), Page: "https://mirror.test/sectoren", Path: "/sectoren", Query: "transportband sectoren", Clicks: 1, Impressions: 10, Ctr: 0.1, Position: 3},
		{Source: "bing", Day: at(2), Page: "https://mirror.test/sectoren", Path: "/sectoren", Query: "tribelt", Clicks: 0, Impressions: 5, Ctr: 0, Position: 2},
	} {
		if err := q.UpsertSearchPerformance(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{
		Q: q, DB: pool, Log: slog.New(slog.DiscardHandler), Now: now, Release: "v2-longtail",
		Title:      func(p string) string { return map[string]string{"/sectoren": "Sectoren | Tribelt"}[p] },
		ViewerName: func(*http.Request) string { return "joris" }, CSS: "/static/stats.css", ChartJS: "/static/chart.js",
	}
	tmpl, _ := fs.Sub(web.Templates, "templates")
	if err := svc.Init(tmpl); err != nil {
		t.Fatal(err)
	}
	return seeded{svc: svc, pool: pool, q: q}
}

func week() Filter {
	return ParseFilter(url.Values{"from": {at(9).Format(time.DateOnly)}, "to": {at(0).Format(time.DateOnly)}}, now())
}

func TestOverview(t *testing.T) {
	s := seed(t)
	o, err := s.svc.Overview(context.Background(), week())
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]int64{}
	for _, k := range o.Kinds {
		kinds[k.VisitorKind] = k.Hits
	}
	// The internal hit and the 40-day-old hit are excluded.
	if kinds["human"] != 3 || kinds["human-unconfirmed"] != 1 || kinds["ai-crawler"] != 2 || kinds["ai-fetcher"] != 1 || kinds["other-bot"] != 1 {
		t.Fatalf("kinds %v", kinds)
	}
	if o.Visitors.Visitors != 2 || o.Visitors.ReturningVisitors != 1 || o.Visitors.DailyVisitors != 1 {
		t.Fatalf("visitors %+v", o.Visitors)
	}
	ft := map[string]int64{}
	for _, f := range o.FirstTouch {
		ft[f.Channel] = f.Visitors
	}
	if ft["search"] != 1 || ft["ai-chat"] != 1 || len(ft) != 2 {
		t.Fatalf("first touch %v (v-old's first hit is outside the range)", ft)
	}
	if len(o.Markers) != 2 || len(o.Days) != 10 || o.ByKind["human"][0] != 1 {
		t.Fatalf("timeline days=%d markers=%d", len(o.Days), len(o.Markers))
	}
	var clicks int64
	for _, c := range o.Outbound {
		clicks += c.Clicks
	}
	if clicks != 2 || len(o.Search) != 2 || len(o.Queries) != 3 {
		t.Fatalf("clicks=%d search=%v", clicks, o.Search)
	}

	f := week()
	f.IncludeInternal = true
	o, _ = s.svc.Overview(context.Background(), f)
	for _, k := range o.Kinds {
		if k.VisitorKind == "human" && k.Hits != 4 {
			t.Fatal("include-internal counts the internal hit")
		}
	}
	f = week()
	f.Release = "v1-baseline"
	o, _ = s.svc.Overview(context.Background(), f)
	var total int64
	for _, k := range o.Kinds {
		total += k.Hits
	}
	if total != 4 {
		t.Fatalf("release filter: %d", total)
	}
	f = week()
	f.Locale = "de"
	o, _ = s.svc.Overview(context.Background(), f)
	if len(o.Kinds) != 0 {
		t.Fatal("locale filter")
	}
}

func TestPagesAndDetail(t *testing.T) {
	s := seed(t)
	rows, err := s.svc.Pages(context.Background(), week())
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	r := rows[0]
	if r.Path != "/sectoren" || r.Title != "Sectoren | Tribelt" || r.Human != 3 || r.Markdown != 2 || r.AvgEngagedMs != 8000 || r.HumanOutbound != 2 || r.Impr != 55 || r.Clicks != 3 {
		t.Fatalf("page row %+v", r)
	}
	f := week()
	f.Path = "/sectoren"
	d, err := s.svc.Page(context.Background(), f)
	if err != nil || len(d.History) != 2 || len(d.Referrers) == 0 || len(d.Bots) != 3 || len(d.Queries) != 3 {
		t.Fatalf("detail %+v %v", d, err)
	}
}

func TestReleaseCompare(t *testing.T) {
	s := seed(t)
	f := ParseFilter(url.Values{"from": {at(60).Format(time.DateOnly)}}, now())
	rows, err := s.svc.Releases(context.Background(), f)
	if err != nil || len(rows) != 2 {
		t.Fatalf("%v %v", rows, err)
	}
	v1, v2 := rows[0], rows[1]
	if v1.ReleaseLabel != "v1-baseline" || v1.Days != 4 || v1.HumanOut != 1 || v1.Impr != 40 || v1.FromSearch != 2 {
		t.Fatalf("v1 %+v", v1)
	}
	if v2.ReleaseLabel != "v2-longtail" || v2.Impr != 15 || v2.AIPerDay != 1.5 || v2.Outbound != 1 {
		t.Fatalf("v2 %+v", v2)
	}
}

func TestAgents(t *testing.T) {
	s := seed(t)
	a, err := s.svc.Agents(context.Background(), week())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Bots) != 5 || a.Formats["md"] != 2 || a.Formats["txt"] != 1 || a.Formats["xml"] != 1 || a.Formats["html"] != 1 || len(a.Paths) != 2 {
		t.Fatalf("agents %+v", a)
	}
	if SortedKeys(a.Formats)[0] != "html" {
		t.Fatal("sorted keys")
	}
}

func get(t *testing.T, s seeded, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	s.svc.Routes(mux, func(h http.Handler) http.Handler { return h })
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	return rec
}

func TestHTTPViews(t *testing.T) {
	s := seed(t)
	q := "?" + week().Query()
	for target, want := range map[string]string{
		"/stats" + q:                            "Returning Visitors",
		"/stats/" + q:                           "Hits by Visitor Kind",
		"/stats/pages" + q:                      "Sectoren | Tribelt",
		"/stats/page" + q + "&path=%2Fsectoren": "Page per Content Release",
		"/stats/releases" + q:                   "v2-longtail",
		"/stats/agents" + q:                     "ChatGPT-User",
	} {
		rec := get(t, s, target)
		if rec.Code != 200 || !strings.Contains(rec.Body.String(), want) || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d, want %q", target, rec.Code, want)
		}
		if !strings.Contains(rec.Body.String(), "joris") || strings.Contains(rec.Body.String(), "ZgotmplZ") {
			t.Errorf("%s: viewer name or template escaping", target)
		}
	}
	if rec := get(t, s, "/stats/page"+q); rec.Code != http.StatusFound {
		t.Fatal("page view without path redirects to the page list")
	}
}

func TestCSVExports(t *testing.T) {
	s := seed(t)
	q := "?" + week().Query()
	for file, header := range map[string]string{
		"overview": "day,human", "pages": "path,page_id", "releases": "release,note", "agents": "bot_name,visitor_kind",
		"hits": "id,ts,path", "page": "day,human",
	} {
		rec := get(t, s, "/stats/"+file+".csv"+q+"&path=%2Fsectoren")
		if rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), header) || !strings.Contains(rec.Header().Get("Content-Disposition"), file) {
			t.Errorf("%s.csv: %d %q", file, rec.Code, rec.Body.String()[:min(40, rec.Body.Len())])
		}
		if _, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll(); err != nil {
			t.Errorf("%s.csv does not parse: %v", file, err)
		}
	}
	hitsCSV := get(t, s, "/stats/hits.csv"+q).Body.String()
	if strings.Count(hitsCSV, "\n") != 10 {
		t.Fatalf("raw hits without internal: %d lines", strings.Count(hitsCSV, "\n"))
	}
	if get(t, s, "/stats/nope.csv").Code != 404 || get(t, s, "/stats/nope").Code != 404 {
		t.Fatal("unknown export")
	}
}

func TestChartSVG(t *testing.T) {
	s := seed(t)
	q := "?" + week().Query()
	for _, name := range []string{"timeline", "channels", "agents"} {
		rec := get(t, s, "/stats/chart/"+name+".svg"+q)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(rec.Body.String(), "<svg") {
			t.Errorf("%s: %d", name, rec.Code)
		}
	}
	if !strings.Contains(get(t, s, "/stats/chart/timeline.svg"+q).Body.String(), "v2-longtail") {
		t.Fatal("release marker on the timeline")
	}
	if get(t, s, "/stats/chart/page.svg"+q).Code != 404 || get(t, s, "/stats/chart/nope.svg").Code != 404 {
		t.Fatal("unknown chart")
	}
	if get(t, s, "/stats/chart/page.svg"+q+"&path=%2Fsectoren").Code != 200 {
		t.Fatal("page chart")
	}
}

func TestSQLiteExport(t *testing.T) {
	s := seed(t)
	rec := get(t, s, "/stats/export.sqlite")
	if rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), "SQLite format 3") {
		t.Fatalf("export %d", rec.Code)
	}
	path := filepath.Join(t.TempDir(), "x.sqlite")
	if err := os.WriteFile(path, rec.Body.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var hits, visitors, releases, pages, perf, clicks int
	var ua string
	err = db.QueryRow(`SELECT (SELECT count(*) FROM hits), (SELECT count(DISTINCT visitor) FROM hits), (SELECT count(*) FROM releases),
		(SELECT count(*) FROM pages), (SELECT count(*) FROM search_performance), (SELECT count(*) FROM outbound_clicks),
		(SELECT user_agent FROM hits WHERE visitor_kind = 'human' LIMIT 1)`).Scan(&hits, &visitors, &releases, &pages, &perf, &clicks, &ua)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 11 || visitors != 4 || releases != 2 || pages != 2 || perf != 3 || clicks != 3 || ua != "Chrome" {
		t.Fatalf("hits=%d visitors=%d releases=%d pages=%d perf=%d clicks=%d ua=%q", hits, visitors, releases, pages, perf, clicks, ua)
	}
	var leaked int
	_ = db.QueryRow(`SELECT count(*) FROM hits WHERE visitor LIKE 'v-%' OR daily_visitor LIKE 'd/%'`).Scan(&leaked)
	if leaked != 0 {
		t.Fatal("identifiers are pseudonymised")
	}
}

func TestBrowserFamily(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 Chrome/140 Safari/537.36 Edg/140": "Edge", "Mozilla/5.0 Firefox/142": "Firefox", "Mozilla/5.0 Version/18 Safari/604.1": "Safari",
		"curl/8": "other", "Mozilla/5.0 Chrome/1 Safari/1 OPR/1": "Opera",
	} {
		if BrowserFamily(ua) != want {
			t.Errorf("%s -> %s", ua, BrowserFamily(ua))
		}
	}
}
