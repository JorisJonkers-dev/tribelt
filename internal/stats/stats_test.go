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
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
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
		label, version string
		ago            int
		tags           []string
	}{{"v1-baseline", "0.1.0", 10, []string{}}, {"v2-longtail", "0.2.0", 3, []string{"longtail-keywords"}}} {
		if _, err := q.UpsertRelease(ctx, queries.UpsertReleaseParams{Label: r.label, Note: "note " + r.label, ContentHash: "h", AppVersion: r.version, Tags: r.tags}); err != nil {
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
		version := map[string]string{"v1-baseline": "0.1.0", "v2-longtail": "0.2.0"}[rel]
		h := queries.InsertHitParams{
			ID: uuid.New(), Ts: at(ago), Path: path, PageID: strp("sectoren"), Locale: strp("nl"), ReleaseLabel: rel, AppVersion: version,
			Format: format, Status: 200, VisitorKind: kind, UserAgent: "Mozilla/5.0 Chrome/140.0 Safari/537.36", DailyHash: "d" + path,
		}
		if mutate != nil {
			mutate(&h)
		}
		res, _ := visits.ClassifyResource(h.Path, h.Format, int(h.Status))
		h.Resource = string(res)
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
	noPage := func(h *queries.InsertHitParams) { h.PageID, h.Locale = nil, nil }
	add(2, "v2-longtail", "/sectoren.md", "ai-crawler", "md", bot("GPTBot", true))
	add(1, "v2-longtail", "/llms.txt", "ai-crawler", "txt", func(h *queries.InsertHitParams) { bot("ClaudeBot", true)(h); noPage(h) })
	add(1, "v2-longtail", "/sectoren", "ai-fetcher", "md", bot("ChatGPT-User", true))
	add(5, "v1-baseline", "/sectoren", "search-crawler", "html", bot("Googlebot", true))
	add(5, "v1-baseline", "/sitemap.xml", "other-bot", "xml", func(h *queries.InsertHitParams) { bot("Googlebot", false)(h); noPage(h) })
	add(3, "v2-longtail", "/wp-login.php", "other-bot", "html", func(h *queries.InsertHitParams) {
		h.BotName, h.Status = strp("curl"), 404
		noPage(h)
	})
	add(3, "v2-longtail", "/images/sectoren-960.webp", "human-unconfirmed", "img", noPage)
	add(40, "v1-baseline", "/sectoren", "human", "html", human("v-old", "search", "bing.com"))

	for _, c := range []struct {
		ago      int
		rel, who string
		internal bool
	}{{4, "v1-baseline", "human", false}, {1, "v2-longtail", "human-unconfirmed", false}, {1, "v2-longtail", "human", true}} {
		err := q.InsertOutboundClick(ctx, queries.InsertOutboundClickParams{
			ID: uuid.New(), Ts: at(c.ago), FromPath: "/sectoren", PageID: strp("sectoren"), Locale: strp("nl"), ReleaseLabel: c.rel,
			Target: "https://www.tribelt.nl/contact", VisitorKind: c.who, VisitorID: strp("v-a"), DailyHash: "d", Internal: c.internal,
			AppVersion: "0.2.0",
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range []queries.UpsertSearchPerformanceParams{
		{Source: "google", Day: at(8), Page: "https://mirror.test/sectoren", Path: "/sectoren", Query: "metalen transportbanden", Clicks: 2, Impressions: 40, Ctr: 0.05, Position: 8},
		{Source: "google", Day: at(2), Page: "https://mirror.test/sectoren", Path: "/sectoren", Query: "transportband sectoren", Clicks: 1, Impressions: 10, Ctr: 0.1, Position: 3},
		{Source: "bing", Day: at(2), Page: "https://mirror.test/sectoren", Path: "/sectoren", Query: "tribelt", Clicks: 0, Impressions: 5, Ctr: 0, Position: 2},
	} {
		if err := q.UpsertSearchPerformance(ctx, s); err != nil {
			t.Fatal(err)
		}
	}
	svc := &Service{
		Q: q, DB: pool, Log: slog.New(slog.DiscardHandler), Now: now, Release: "v2-longtail", AppVersion: "0.2.0", PageCount: 124,
		Title:      func(p string) string { return map[string]string{"/sectoren": "Sectoren | Tribelt"}[p] },
		ViewerName: func(*http.Request) string { return "joris jonkers" }, CSS: "/static/stats.css", ChartJS: "/static/chart.js",
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

func kpiByLabel(t *testing.T, kpis []KPI) map[string]KPI {
	t.Helper()
	out := map[string]KPI{}
	for _, k := range kpis {
		out[k.Label] = k
	}
	return out
}

func TestOverview(t *testing.T) {
	s := seed(t)
	o, err := s.svc.Overview(context.Background(), week())
	if err != nil {
		t.Fatal(err)
	}
	k := kpiByLabel(t, o.KPIs)
	// Visitors v-a and v-b plus one cookie-less Daily Visitor; nothing in the previous ten days.
	if h := k["Human visitors"]; h.Value != "3" || h.Delta != "new" || h.Hint != "1 Returning Visitors" || h.Spark.Line == "" {
		t.Fatalf("human visitors %+v", h)
	}
	if k["AI crawler hits"].Value != "2" || k["AI crawler hits"].Hint != "100% verified against IP ranges" {
		t.Fatalf("ai crawler %+v", k["AI crawler hits"])
	}
	if k["AI fetches"].Value != "1" || k["AI fetches"].Hint != "ChatGPT-User" || k["Search crawler hits"].Hint != "Googlebot" {
		t.Fatalf("hints %+v %+v", k["AI fetches"], k["Search crawler hits"])
	}
	if out := k["Outbound Clicks"]; out.Value != "2" || out.Hint != "67% of Human visitors" {
		t.Fatalf("outbound %+v", out)
	}
	fetched := map[string]FetchRow{}
	for _, f := range o.Fetched {
		fetched[f.Resource] = f
	}
	if len(o.Fetched) != len(visits.Resources()) {
		t.Fatalf("one row per resource class: %d", len(o.Fetched))
	}
	for res, want := range map[string]string{"page": "5", "markdown": "2", "llms": "1", "sitemap": "1", "image": "1", "not_found": "1", "outbound": "2", "robots": "0"} {
		if fetched[res].Total != want {
			t.Errorf("%s: %s, want %s", res, fetched[res].Total, want)
		}
	}
	if p := fetched["page"]; p.Top != "Browsers" || p.TopShare != "80% of requests" || p.Example != "/sectoren" || len(p.Segs) != 3 {
		t.Fatalf("page row %+v", p)
	}
	if out := fetched["outbound"]; out.Example != "/go → tribelt.nl/contact" || out.TopShare != "100% of clicks" {
		t.Fatalf("outbound row %+v", out)
	}
	if fetched["robots"].Segs != nil || fetched["robots"].Top != "—" {
		t.Fatal("empty resource")
	}
	arrive := map[string]BarRow{}
	for _, a := range o.Arrive {
		arrive[a.Key] = a
	}
	if len(o.Arrive) != 7 || arrive["search"].Share != "25%" || arrive["social"].N != 0 || arrive["search"].W != 100 {
		t.Fatalf("arrive %+v", o.Arrive)
	}
	if o.Bots[0].Kind != "ai-crawler" || o.Bots[0].Verified != "100%" || o.Bots[0].Pages != "1" || o.Bots[4].Hits != "2" {
		t.Fatalf("bots %+v", o.Bots)
	}
	if len(o.Pages) != 1 || o.PageTotal != 1 || len(o.Search.Totals) != 2 || len(o.Search.Queries) != 3 {
		t.Fatalf("pages %d search %+v", len(o.Pages), o.Search)
	}
	if o.Timeline.Total != 10 || len(o.Timeline.Marks) != 1 || len(o.Timeline.Chips) != 7 || len(o.Timeline.Columns) != 10 {
		t.Fatalf("timeline total=%d marks=%d", o.Timeline.Total, len(o.Timeline.Marks))
	}
}

func TestOverviewFilters(t *testing.T) {
	s := seed(t)
	total := func(f Filter) int64 {
		o, err := s.svc.Overview(context.Background(), f)
		if err != nil {
			t.Fatal(err)
		}
		return o.Timeline.Total
	}
	f := week()
	f.IncludeInternal = true
	if total(f) != 11 {
		t.Fatal("include-internal counts the internal hit")
	}
	for name, mutate := range map[string]func(*Filter){
		"release": func(f *Filter) { f.Release = "v1-baseline" },
		"version": func(f *Filter) { f.Version = "0.1.0" },
	} {
		f := week()
		mutate(&f)
		if got := total(f); got != 4 {
			t.Errorf("%s filter: %d", name, got)
		}
	}
	f = week()
	f.Tag = "longtail-keywords"
	if got := total(f); got != 6 {
		t.Fatalf("tag filter: %d", got)
	}
	f = week()
	f.Locale = "de"
	if total(f) != 0 {
		t.Fatal("locale filter")
	}
	f = week()
	f.Arrive, f.Hide, f.Scale = "first", []string{"human"}, "share"
	o, err := s.svc.Overview(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if o.Arrive[0].Key != "search" || o.Arrive[0].N != 1 || len(o.Timeline.Areas) != 6 || !o.Timeline.Chips[0].Off {
		t.Fatalf("first touch %+v, areas %d", o.Arrive, len(o.Timeline.Areas))
	}
}

func TestPagesAndDetail(t *testing.T) {
	s := seed(t)
	l, err := s.svc.Pages(context.Background(), week(), 0)
	if err != nil || len(l.Rows) != 1 {
		t.Fatalf("rows=%v err=%v", l, err)
	}
	r := l.Rows[0]
	// The Markdown twin /sectoren.md counts towards /sectoren.
	if r.Path != "/sectoren" || r.Title != "Sectoren | Tribelt" || r.Humans != 4 || r.Markdown != 2 || r.Crawlers != 2 || r.AI != 1 || r.Engaged != "0:08" || r.HumanOutbound != 2 || r.Impr != 55 || r.Clicks != 3 {
		t.Fatalf("page row %+v", r)
	}
	f := week()
	f.Q = "nothing-matches"
	if l, _ = s.svc.Pages(context.Background(), f, 0); len(l.Rows) != 0 {
		t.Fatal("q filters the list")
	}
	f = week()
	f.Path = "/sectoren"
	d, err := s.svc.Page(context.Background(), f)
	if err != nil || len(d.History) != 2 || !d.History[1].Changed || len(d.Referrers) == 0 || len(d.Bots) != 3 || len(d.Queries) != 3 || len(d.KPIs) != 4 {
		t.Fatalf("detail %+v %v", d, err)
	}
	if d.Timeline.Total != 7 {
		t.Fatalf("page timeline %d", d.Timeline.Total)
	}
}

func TestReleases(t *testing.T) {
	s := seed(t)
	f := ParseFilter(url.Values{"from": {at(60).Format(time.DateOnly)}}, now())
	v, err := s.svc.Releases(context.Background(), f)
	if err != nil || len(v.Rows) != 2 {
		t.Fatalf("%v %v", v, err)
	}
	v1, v2 := v.Rows[0], v.Rows[1]
	if v1.ReleaseLabel != "v1-baseline" || v1.Days != 4 || v1.HumanOut != 1 || v1.Impr != 40 || v1.FromSearch != 2 || v1.Versions() != "v0.1.0" {
		t.Fatalf("v1 %+v", v1)
	}
	if v2.ReleaseLabel != "v2-longtail" || v2.Impr != 15 || v2.AIPerDay != 1 || v2.Outbound != 1 || v2.Markdown != 2 || v2.Llms != 1 || v2.Release.Tags[0] != "longtail-keywords" {
		t.Fatalf("v2 %+v", v2)
	}
	c := v.Compare
	if c.A.ReleaseLabel != "v1-baseline" || c.B.ReleaseLabel != "v2-longtail" || len(c.Metrics) != 14 || len(c.Changes) != 1 || c.Unchanged != 0 {
		t.Fatalf("compare %+v", c)
	}
	if ch := c.Changes[0]; ch.TitleA != "Title v1-baseline" || ch.TitleB != "Title v2-longtail" || ch.Added || ch.Removed {
		t.Fatalf("change %+v", ch)
	}
	f.A, f.B = "v2-longtail", "v2-longtail"
	v, _ = s.svc.Releases(context.Background(), f)
	if len(v.Compare.Changes) != 0 || v.Compare.Unchanged != 1 {
		t.Fatal("a release compared with itself changes nothing")
	}
	f.A, f.B = "unknown", "v1-baseline"
	if v, _ = s.svc.Releases(context.Background(), f); v.Compare.A.ReleaseLabel != "v1-baseline" {
		t.Fatal("unknown labels fall back to the previous release")
	}
}

func TestAgents(t *testing.T) {
	s := seed(t)
	a, err := s.svc.Agents(context.Background(), week())
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Bots) != 6 {
		t.Fatalf("bots %d", len(a.Bots))
	}
	by := map[string]BotRow{}
	for _, b := range a.Bots {
		by[b.Name] = b
	}
	if c := by["ClaudeBot"]; !c.ReadLLMs || c.Operator != "Anthropic" || c.VerifiedTone != "ok" || c.MixText != "llms 100%" {
		t.Fatalf("claudebot %+v", c)
	}
	if g := by[`"Googlebot" (spoofed)`]; !g.Spoofed || g.VerifiedText != "0%" || g.Operator != "unknown" {
		t.Fatalf("spoofed %+v", g)
	}
	if c := by["curl"]; c.VerifiedText != "no ranges" || c.VerifiedTone != "none" {
		t.Fatalf("curl %+v", c)
	}
	files := map[string]AgentFile{}
	for _, f := range a.Files {
		files[f.Name] = f
	}
	if files["llms.txt"].Hits != "1" || files[".md twins"].Hint != "2 of 124 pages fetched as Markdown" || files["robots.txt"].Hint != "Fetched by 0 distinct agents" {
		t.Fatalf("files %+v", files)
	}
	if a.Formats[0].Pct != "20%" || a.Formats[1].Pct != "40%" || len(a.Fetchers) != 1 || a.Fetchers[0].Format != "md" {
		t.Fatalf("formats %+v fetchers %+v", a.Formats, a.Fetchers)
	}
	f := week()
	f.Kind = "other"
	if a, _ = s.svc.Agents(context.Background(), f); len(a.Bots) != 2 || !a.Kinds[4].On {
		t.Fatalf("kind filter: %d", len(a.Bots))
	}
}

func TestSearchView(t *testing.T) {
	s := seed(t)
	v, err := s.svc.SearchPerformance(context.Background(), week())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Tiles) != 2 || v.Tiles[0].Name != "Bing" || v.Tiles[1].CTR != "6.0%" || len(v.Pages) != 1 || v.Pages[0].Title != "Sectoren | Tribelt" {
		t.Fatalf("search %+v", v)
	}
	if v.Impressions.Total != "55" || v.Clicks.Total != "3" || len(v.Impressions.Cols) != 10 {
		t.Fatalf("bars %+v", v.Impressions)
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
	inline := regexp.MustCompile(`\sstyle=|<script>|<script [^>]*>[^<]|\son[a-z]+=`)
	escaped := regexp.MustCompile(`href="[^"]*\?[^"]*%(3[dD]|26)`)
	for target, want := range map[string][]string{
		"/stats" + q: {"What was fetched", "Who reads the mirror", "Returning Visitors", "Top Mirror Pages"},
		"/stats/" + q + "&hide=human&scale=share": {"How people arrive", `data-off`},
		"/stats/pages" + q:                        {"Sectoren | Tribelt", "Last 10 days"},
		"/stats/page" + q + "&path=%2Fsectoren":   {"This page per Content Release", "Hits per day on this page"},
		"/stats/releases" + q:                     {"Compare two Content Releases", "longtail-keywords", "v0.1.0"},
		"/stats/agents" + q:                       {"ChatGPT-User", "read llms.txt", `&#34;Googlebot&#34; (spoofed)`},
		"/stats/search" + q:                       {"Top queries", "Mirror Pages in search"},
	} {
		rec := get(t, s, target)
		body := rec.Body.String()
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: %d", target, rec.Code)
		}
		for _, w := range append(want, "joris jonkers", "JJ", "v0.2.0 · v2-longtail", "Include my own visits") {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %q", target, w)
			}
		}
		if strings.Contains(body, "ZgotmplZ") || inline.MatchString(body) {
			t.Errorf("%s: template escaping or inline style/script under the CSP: %v", target, inline.FindString(body))
		}
		if bad := escaped.FindString(body); bad != "" {
			t.Errorf("%s: a link's query string is escaped and loses the filter: %s", target, bad)
		}
	}
	if rec := get(t, s, "/stats/page"+q); rec.Code != http.StatusFound {
		t.Fatal("page view without path redirects to the page list")
	}
	empty := get(t, s, "/stats/search?from=2026-06-01&to=2026-06-10").Body.String()
	if !strings.Contains(empty, "Search Performance is not connected yet") || !strings.Contains(empty, "GSC_SERVICE_ACCOUNT_JSON") {
		t.Fatal("search empty state")
	}
	s.svc.Search = SearchSources{Google: true}
	if !strings.Contains(get(t, s, "/stats/search?from=2026-06-01&to=2026-06-10").Body.String(), "No Search Performance data for this range yet") {
		t.Fatal("connected but empty")
	}
}

func TestCSVExports(t *testing.T) {
	s := seed(t)
	q := "?" + week().Query()
	for file, header := range map[string]string{
		"overview": "day,human", "pages": "path,page_id", "releases": "release,note,tags,app_version", "agents": "bot_name,operator",
		"hits": "id,ts,path", "page": "day,human", "search": "query,source",
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
	if strings.Count(hitsCSV, "\n") != 12 || !strings.Contains(hitsCSV, ",resource,app_version") || !strings.Contains(hitsCSV, ",image,0.2.0") {
		t.Fatalf("raw hits without internal: %d lines", strings.Count(hitsCSV, "\n"))
	}
	if get(t, s, "/stats/nope.csv").Code != 404 || get(t, s, "/stats/nope").Code != 404 {
		t.Fatal("unknown export")
	}
}

func TestChartSVG(t *testing.T) {
	s := seed(t)
	q := "?" + week().Query()
	for _, name := range []string{"timeline", "releases", "agents", "channels", "search"} {
		rec := get(t, s, "/stats/chart/"+name+".svg"+q)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/svg+xml" || !strings.HasPrefix(rec.Body.String(), "<svg") || !strings.HasSuffix(rec.Body.String(), "</svg>") {
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
	var hits, visitors, releases, pages, perf, clicks, images int
	var ua, tags, version string
	err = db.QueryRow(`SELECT (SELECT count(*) FROM hits), (SELECT count(DISTINCT visitor) FROM hits), (SELECT count(*) FROM releases),
		(SELECT count(*) FROM pages), (SELECT count(*) FROM search_performance), (SELECT count(*) FROM outbound_clicks),
		(SELECT user_agent FROM hits WHERE visitor_kind = 'human' LIMIT 1), (SELECT count(*) FROM hits WHERE resource = 'image'),
		(SELECT tags FROM releases WHERE label = 'v2-longtail'), (SELECT app_version FROM outbound_clicks LIMIT 1)`).Scan(&hits, &visitors, &releases, &pages, &perf, &clicks, &ua, &images, &tags, &version)
	if err != nil {
		t.Fatal(err)
	}
	if hits != 13 || visitors != 4 || releases != 2 || pages != 2 || perf != 3 || clicks != 3 || ua != "Chrome" || images != 1 || tags != "longtail-keywords" || version != "0.2.0" {
		t.Fatalf("hits=%d visitors=%d releases=%d pages=%d perf=%d clicks=%d ua=%q images=%d tags=%q", hits, visitors, releases, pages, perf, clicks, ua, images, tags)
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
