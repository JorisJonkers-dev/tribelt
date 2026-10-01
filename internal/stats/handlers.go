package stats

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/JorisJonkers-dev/tribelt/internal/integrations"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/oidc"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// DB is what the raw exports stream from.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Service renders the stats views.
type Service struct {
	Q   Querier
	DB  DB
	Log *slog.Logger
	Now func() time.Time
	// Release and AppVersion are what this process serves; the header pill shows both.
	Release, AppVersion string
	// PageCount is the number of Mirror Pages in the running Content Release.
	PageCount int
	// Title returns the current title of a Mirror Page path.
	Title func(path string) string
	// ViewerName returns the signed-in Stats Viewer's display name.
	ViewerName   func(r *http.Request) string
	Search       SearchSources
	CSS, ChartJS string
	// Integrations, when set, adds the Integrations view and makes Search sources live.
	Integrations *integrations.Manager
	// Actor is the signed-in viewer as the Integrations see them (admin or not).
	Actor func(r *http.Request) integrations.Actor
	// Session identifies the session a CSRF token is bound to; CSRFKey signs the tokens.
	Session func(r *http.Request) string
	CSRFKey []byte
	// Account returns the signed-in viewer's local account and open sessions.
	Account func(r *http.Request) (oidc.Account, []oidc.Session, bool)
	tmpl    *template.Template
}

func funcs() template.FuncMap {
	return template.FuncMap{
		"num": num,
		// qs keeps a query string's & and = intact after the ? of an href.
		"qs": func(f Filter, extra ...string) template.URL {
			return template.URL(f.Query(extra...)) //nolint:gosec // url.Values.Encode escapes every value
		},
		"one":       one,
		"two":       func(f float64) string { return strconv.FormatFloat(f, 'f', 2, 64) },
		"f1":        func(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) },
		"kindLabel": KindLabel,
		"kindDot":   KindDot,
		"channel":   ChannelLabel,
		"versions":  versionsText,
		"source":    sourceName,
		"date":      func(t time.Time) string { return t.In(amsterdam()).Format("2 Jan 2006") },
		"heading":   func(title, sub string) struct{ Title, Sub string } { return struct{ Title, Sub string }{title, sub} },
		"pageTable": func(p page, rows []PageRow, full bool) pageTable {
			return pageTable{Filter: p.Filter, Rows: rows, Full: full, Days: p.Filter.Len()}
		},
		"searchPanel":   func(p page, s SearchSummary) searchPanel { return searchPanel{Filter: p.Filter, Search: s} },
		"searchSummary": func(src SearchSources) SearchSummary { return SearchSummary{Sources: src} },
		"side":          func(label string, r *ReleaseRow) releaseSide { return releaseSide{Side: label, Row: r} },
		"kindTitle":     integrations.Title,
		"intCard":       func(p IntegrationsPage, c integrations.Card) intCard { return intCard{Page: p, Card: c} },
	}
}

type pageTable struct {
	Filter Filter
	Rows   []PageRow
	Full   bool
	Days   int
}

type searchPanel struct {
	Filter Filter
	Search SearchSummary
}

type intCard struct {
	Page IntegrationsPage
	Card integrations.Card
}

type releaseSide struct {
	Side string
	Row  *ReleaseRow
}

// Init parses the stats templates.
func (s *Service) Init(templates fs.FS) error {
	t, err := template.New("stats.html").Funcs(funcs()).ParseFS(templates, "stats.html", "stats-integrations.html", "stats-account.html")
	if err != nil {
		return err
	}
	s.tmpl = t
	return nil
}

func (s *Service) title(path string) string {
	if s.Title == nil {
		return ""
	}
	return s.Title(path)
}

// Routes mounts every stats route on mux, wrapped by guard.
func (s *Service) Routes(mux *http.ServeMux, guard func(http.Handler) http.Handler) {
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, guard(noStore(fn))) }
	h("GET /stats", s.view("overview"))
	h("GET /stats/{$}", s.view("overview"))
	h("GET /stats/pages", s.view("pages"))
	h("GET /stats/page", s.view("page"))
	h("GET /stats/releases", s.view("releases"))
	h("GET /stats/agents", s.view("agents"))
	h("GET /stats/search", s.view("search"))
	if s.Integrations != nil {
		h("GET /stats/integrations", s.integrationsView)
		h("POST /stats/integrations/{kind}/{action}", s.integrationsAction)
	}
	if s.Account != nil {
		h("GET /stats/account", s.accountView)
	}
	h("GET /stats/chart/{name}", s.chartSVG)
	h("GET /stats/export.sqlite", s.exportSQLite)
	h("GET /stats/{file}", s.csv)
}

func noStore(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex, nofollow")
		next(w, r)
	})
}

// page is the template data of every view.
type page struct {
	Title, View, Release, AppVersion, Viewer, Initials, CSS, JS string
	Filter                                                      Filter
	Ranges, Locales, Tabs                                       []Tab
	Releases                                                    []queries.Release
	Versions, Tags                                              []string
	Data                                                        any
}

// Pill is the header's "v0.3.0 · v1-baseline".
func (p page) Pill() string { return Summary(p.AppVersion, p.Release) }

func (s *Service) load(ctx context.Context, view string, f Filter) (any, error) {
	switch view {
	case "overview":
		return s.Overview(ctx, f)
	case "pages":
		return s.Pages(ctx, f, 0)
	case "page":
		return s.Page(ctx, f)
	case "releases":
		return s.Releases(ctx, f)
	case "search":
		return s.SearchPerformance(ctx, f)
	default:
		return s.Agents(ctx, f)
	}
}

func viewTitle(view string) string {
	return map[string]string{
		"overview": "Overview", "pages": "Pages", "page": "Page detail", "releases": "Releases", "agents": "AI & crawlers", "search": "Search",
		"integrations": "Integrations",
	}[view]
}

func tabs(view string, f Filter, withIntegrations bool) []Tab {
	q := "?" + f.Query("path", "", "q", "", "kind", "", "a", "", "b", "")
	var out []Tab
	for _, t := range []struct{ view, label, href string }{
		{"overview", "Overview", "/stats"},
		{"pages", "Pages", "/stats/pages"},
		{"releases", "Releases", "/stats/releases"},
		{"agents", "AI & crawlers", "/stats/agents"},
		{"search", "Search", "/stats/search"},
		{"integrations", "Integrations", "/stats/integrations"},
	} {
		if t.view == "integrations" && !withIntegrations {
			continue
		}
		on := t.view == view || (t.view == "pages" && view == "page")
		out = append(out, Tab{Label: t.label, Href: t.href + q, On: on})
	}
	return out
}

func (s *Service) chrome(ctx context.Context, r *http.Request, view string, f Filter) (page, error) {
	p := page{
		Title: viewTitle(view), View: view, Release: s.Release, AppVersion: s.AppVersion, CSS: s.CSS, JS: s.ChartJS, Filter: f,
		Tabs: tabs(view, f, s.Integrations != nil),
	}
	for _, n := range []int{7, 30, 90} {
		label := strconv.Itoa(n) + "d"
		p.Ranges = append(p.Ranges, Tab{Label: label, Href: "?" + f.PresetQuery(n), On: f.Preset() == label})
	}
	for _, l := range []string{"", "nl", "en", "de"} {
		p.Locales = append(p.Locales, Tab{Label: strings.ToUpper(orDefault(l, "All")), Href: "?" + f.Query("locale", l), On: f.Locale == l})
	}
	p.Locales[0].Label = "All"
	var err error
	if p.Releases, err = s.Q.ListReleases(ctx); err != nil {
		return p, err
	}
	if p.Versions, err = s.Q.AppVersions(ctx); err != nil {
		return p, err
	}
	if p.Tags, err = s.Q.ReleaseTags(ctx); err != nil {
		return p, err
	}
	if s.ViewerName != nil {
		p.Viewer = s.ViewerName(r)
		p.Initials = initials(p.Viewer)
	}
	return p, nil
}

func (s *Service) view(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f := ParseFilter(r.URL.Query(), s.Now())
		if name == "page" && f.Path == "" {
			http.Redirect(w, r, "/stats/pages?"+f.Query(), http.StatusFound) //nolint:gosec // same-site path
			return
		}
		if name != "page" {
			f.Path = ""
		}
		p, err := s.chrome(r.Context(), r, name, f)
		if err != nil {
			s.fail(w, err)
			return
		}
		if p.Data, err = s.load(r.Context(), name, f); err != nil {
			s.fail(w, err)
			return
		}
		var buf bytes.Buffer
		if err := s.tmpl.ExecuteTemplate(&buf, name, p); err != nil {
			s.fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(buf.Bytes())
	}
}

// chartSVG serves one chart as a standalone SVG, for download and the client-side PNG export.
func (s *Service) chartSVG(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(r.PathValue("name"), ".svg")
	f := ParseFilter(r.URL.Query(), s.Now())
	if name != "page" {
		f.Path = ""
	}
	svg, err := s.chart(r.Context(), name, f)
	if err != nil {
		s.fail(w, err)
		return
	}
	if svg == "" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.svg"`, name, f.To.Format(time.DateOnly)))
	_, _ = w.Write([]byte(svg)) //nolint:gosec // charts escape every text node
}

func (s *Service) chart(ctx context.Context, name string, f Filter) (string, error) {
	kinds := map[string][]visits.Kind{"timeline": visits.Kinds(), "releases": visits.Kinds(), "page": visits.Kinds(), "agents": botKinds()}
	titles := map[string]string{
		"timeline": "Hits per day by Visitor Kind", "releases": "Hits per day with Content Releases", "page": "Hits per day: " + f.Path,
		"agents": "Crawler and agent Hits per day",
	}
	switch {
	case name == "page" && f.Path == "":
		return "", nil
	case kinds[name] != nil:
		_, d, err := s.kindTimeline(ctx, name, f, kinds[name])
		if err != nil {
			return "", err
		}
		return TimelineSVG(titles[name]+" · "+f.RangeText(), d), nil
	case name == "channels":
		rows, err := s.arrive(ctx, f)
		return BarsSVG("Human page views by Arrival Channel · "+f.RangeText(), rows), err
	case name == "search":
		days, series, err := s.searchDays(ctx, f)
		return DaySVG("Search Performance per day · "+f.RangeText(), days, series), err
	}
	return "", nil
}

func (s *Service) fail(w http.ResponseWriter, err error) {
	s.Log.Error("stats view failed", "error", err)
	http.Error(w, "Something went wrong loading the statistics.", http.StatusInternalServerError)
}
