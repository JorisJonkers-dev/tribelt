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

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// DB is what the raw exports stream from.
type DB interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Service renders the stats views.
type Service struct {
	Q       Querier
	DB      DB
	Log     *slog.Logger
	Now     func() time.Time
	Release string
	// Title returns the current title of a Mirror Page path.
	Title func(path string) string
	// ViewerName returns the signed-in Stats Viewer's display name.
	ViewerName    func(r *http.Request) string
	SearchEnabled bool
	CSS, ChartJS  string
	tmpl          *template.Template
}

// Init parses the stats templates.
func (s *Service) Init(templates fs.FS) error {
	t, err := template.ParseFS(templates, "stats.html")
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
	Title, View, Release, Viewer, CSS, ChartJS string
	Filter                                     Filter
	Locales                                    []string
	Releases                                   []queries.Release
	SearchEnabled                              bool
	Data                                       any
	charts                                     map[string]string
}

// Num formats a count with thin grouping.
func (page) Num(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// Seconds formats milliseconds as seconds.
func (page) Seconds(ms int64) string { return strconv.FormatFloat(float64(ms)/1000, 'f', 1, 64) }

// SumOutbound totals Outbound Clicks.
func (page) SumOutbound(rows []queries.OutboundTotalsRow) int64 {
	var n int64
	for _, r := range rows {
		n += r.Clicks
	}
	return n
}

type chartView struct {
	SVG           template.HTML
	SVGHref, Name string
}

// Chart embeds a rendered chart with its download links.
func (p page) Chart(name string) chartView {
	return chartView{
		SVG:     template.HTML(p.charts[name]), //nolint:gosec // built by LineChart/BarChart, which escape all text
		SVGHref: "/stats/chart/" + name + ".svg?" + p.Filter.Query(),
		Name:    name + "-" + p.Filter.From.Format(time.DateOnly) + "-" + p.Filter.To.Format(time.DateOnly),
	}
}

func (s *Service) load(ctx context.Context, view string, f Filter) (any, map[string]string, error) {
	charts := map[string]string{}
	switch view {
	case "overview":
		o, err := s.Overview(ctx, f)
		if err != nil {
			return nil, nil, err
		}
		charts["timeline"] = kindTimeline("Hits per day by Visitor Kind", o.Days, o.ByKind, o.Markers, visits.Kinds())
		var bars []Bar
		for _, c := range o.Channels {
			bars = append(bars, Bar{Label: c.Channel, Value: c.Hits, Color: KindColor(c.Channel)})
		}
		charts["channels"] = BarChart("Human page views by Arrival Channel", bars)
		return o, charts, nil
	case "pages":
		p, err := s.Pages(ctx, f)
		return p, charts, err
	case "page":
		d, err := s.Page(ctx, f)
		if err != nil {
			return nil, nil, err
		}
		charts["page"] = kindTimeline("Hits per day: "+f.Path, d.Days, d.ByKind, d.Markers, visits.Kinds())
		return d, charts, nil
	case "releases":
		r, err := s.Releases(ctx, f)
		if err != nil {
			return nil, nil, err
		}
		days, by, err := s.daily(ctx, f)
		if err != nil {
			return nil, nil, err
		}
		m, _, err := s.markers(ctx)
		if err != nil {
			return nil, nil, err
		}
		charts["timeline"] = kindTimeline("Hits per day with Content Releases", days, by, m, visits.Kinds())
		return r, charts, nil
	default:
		a, err := s.Agents(ctx, f)
		if err != nil {
			return nil, nil, err
		}
		charts["agents"] = kindTimeline("Crawler and agent Hits per day", a.Days, a.ByKind, a.Markers,
			[]visits.Kind{visits.KindSearchCrawler, visits.KindAICrawler, visits.KindAIFetcher, visits.KindSEOTool, visits.KindOtherBot})
		return a, charts, nil
	}
}

func kindTimeline(title string, days []time.Time, by map[string][]int64, markers []Marker, kinds []visits.Kind) string {
	var series []Series
	for _, k := range kinds {
		series = append(series, Series{Name: string(k), Color: KindColor(string(k)), Values: by[string(k)]})
	}
	return LineChart(title, days, series, markers)
}

func viewTitle(view string) string {
	switch view {
	case "pages":
		return "Per page"
	case "page":
		return "Page detail"
	case "releases":
		return "Release compare"
	case "agents":
		return "AI & crawlers"
	default:
		return "Overview"
	}
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
		data, charts, err := s.load(r.Context(), name, f)
		if err != nil {
			s.fail(w, err)
			return
		}
		rels, err := s.Q.ListReleases(r.Context())
		if err != nil {
			s.fail(w, err)
			return
		}
		p := page{
			Title: viewTitle(name), View: name, Release: s.Release, CSS: s.CSS, ChartJS: s.ChartJS, Filter: f,
			Locales: []string{"nl", "en", "de"}, Releases: rels, SearchEnabled: s.SearchEnabled, Data: data, charts: charts,
		}
		if s.ViewerName != nil {
			p.Viewer = s.ViewerName(r)
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

func (s *Service) chartSVG(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSuffix(r.PathValue("name"), ".svg")
	f := ParseFilter(r.URL.Query(), s.Now())
	view := map[string]string{"timeline": "overview", "channels": "overview", "page": "page", "agents": "agents"}[name]
	if view == "" || (view == "page" && f.Path == "") {
		http.NotFound(w, r)
		return
	}
	_, charts, err := s.load(r.Context(), view, f)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.svg"`, name, f.To.Format(time.DateOnly)))
	_, _ = w.Write([]byte(charts[name])) //nolint:gosec // charts escape every text node
}

func (s *Service) fail(w http.ResponseWriter, err error) {
	s.Log.Error("stats view failed", "error", err)
	http.Error(w, "Something went wrong loading the statistics.", http.StatusInternalServerError)
}
