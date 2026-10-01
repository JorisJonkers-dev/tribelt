package stats

import (
	"context"
	"sort"
	"strings"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// PageRow is one Mirror Page in the page lists.
type PageRow struct {
	queries.PageTableRow
	Title                   string
	Humans, AI, Crawlers    int64
	Outbound, HumanOutbound int64
	Clicks, Impr            int64
	Position                float64
	OutboundPerHuman        float64
	Engaged                 string
	Spark                   Spark
}

// PageList is the Pages view: every Mirror Page with Hits, busiest with humans first.
type PageList struct {
	Rows  []PageRow
	Total int
}

func (s *Service) pageRows(ctx context.Context, f Filter) ([]PageRow, error) {
	p := f.params()
	rows, err := s.Q.PageTable(ctx, queries.PageTableParams(p))
	if err != nil {
		return nil, err
	}
	out, err := s.Q.OutboundByPage(ctx, queries.OutboundByPageParams(p))
	if err != nil {
		return nil, err
	}
	search, err := s.Q.SearchByPath(ctx, queries.SearchByPathParams{FromDay: f.Start(), ToDay: f.End()})
	if err != nil {
		return nil, err
	}
	outBy := map[string]queries.OutboundByPageRow{}
	for _, o := range out {
		outBy[o.FromPath] = o
	}
	searchBy := map[string]queries.SearchByPathRow{}
	for _, r := range search {
		searchBy[r.Path] = r
	}
	q := strings.ToLower(f.Q)
	res := make([]PageRow, 0, len(rows))
	for _, r := range rows {
		o, sr := outBy[r.Path], searchBy[r.Path]
		row := PageRow{
			PageTableRow: r, Title: s.title(r.Path), Humans: r.Human + r.HumanUnconfirmed, AI: r.AiFetcher,
			Crawlers: r.SearchCrawler + r.AiCrawler, Outbound: o.Clicks, HumanOutbound: o.HumanClicks,
			Clicks: sr.Clicks, Impr: sr.Impressions, Position: sr.AvgPosition, Engaged: minSec(r.AvgEngagedMs),
		}
		if q != "" && !strings.Contains(strings.ToLower(row.Path+" "+row.Title), q) {
			continue
		}
		if row.Humans > 0 {
			row.OutboundPerHuman = float64(o.HumanClicks) / float64(row.Humans)
		}
		res = append(res, row)
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Humans > res[j].Humans })
	return res, nil
}

// Pages gathers the page list; limit 0 means every page.
func (s *Service) Pages(ctx context.Context, f Filter, limit int) (*PageList, error) {
	rows, err := s.pageRows(ctx, f)
	if err != nil {
		return nil, err
	}
	l := &PageList{Total: len(rows), Rows: rows}
	if limit > 0 && len(rows) > limit {
		l.Rows = rows[:limit]
	}
	paths := make([]string, len(l.Rows))
	for i, r := range l.Rows {
		paths[i] = r.Path
	}
	p := f.params()
	daily, err := s.Q.DailyPathHumans(ctx, queries.DailyPathHumansParams{
		FromTs: p.FromTs, ToTs: p.ToTs, Locale: p.Locale, Release: p.Release, Version: p.Version, Tag: p.Tag,
		IncludeInternal: p.IncludeInternal, Paths: paths,
	})
	if err != nil {
		return nil, err
	}
	days, idx := dayIndex(f)
	series := map[string][]int64{}
	for _, d := range daily {
		if series[d.Path] == nil {
			series[d.Path] = make([]int64, len(days))
		}
		series[d.Path][idx[d.Day.Format(time.DateOnly)]] = d.Hits
	}
	for i := range l.Rows {
		v := series[l.Rows[i].Path]
		if v == nil {
			v = make([]int64, len(days))
		}
		l.Rows[i].Spark = spark(v, 120, 28)
	}
	return l, nil
}

// BotRow is one crawler or agent in a page's or the site's agent table.
type BotRow struct {
	queries.BotTableRow
	Name, Operator, Label, Dot, VerifiedText, VerifiedTone, Last, MixText string
	ReadLLMs, Spoofed                                                     bool
	Mix                                                                   []Seg
}

func mixParts(b queries.BotTableRow) []part {
	return []part{
		{"HTML", colBlue, b.Page},
		{".md", colOrange, b.Markdown},
		{"robots", "#A58BD6", b.Robots},
		{"llms", colSand, b.Llms},
		{"sitemap", colGreen, b.Sitemap},
		{"404 / other", colGrey, b.Other},
	}
}

func botRow(b queries.BotTableRow, now time.Time) BotRow {
	r := BotRow{
		BotTableRow: b, Name: orDefault(b.BotName, "(unnamed)"), Operator: orDefault(visits.Operator(b.BotName), "unknown"),
		Label: KindLabel(b.VisitorKind), Dot: KindDot(b.VisitorKind), Last: ago(b.LastSeen, now), ReadLLMs: b.Llms > 0,
	}
	r.Spoofed = b.VisitorKind == string(visits.KindOtherBot) && b.Checked > 0 && b.Verified == 0
	if r.Spoofed {
		r.Name, r.Operator = `"`+b.BotName+`" (spoofed)`, "unknown"
	}
	switch v := pct(b.Verified, b.Hits); {
	case b.Checked == 0:
		r.VerifiedText, r.VerifiedTone = "no ranges", "none"
	case v >= 90:
		r.VerifiedText, r.VerifiedTone = itoa(v)+"%", "ok"
	case v > 0:
		r.VerifiedText, r.VerifiedTone = itoa(v)+"%", "partial"
	default:
		r.VerifiedText, r.VerifiedTone = "0%", "none"
	}
	parts := mixParts(b)
	r.Mix = segments(parts, 1)
	var texts []string
	for _, p := range parts {
		if p.n > 0 && len(texts) < 3 {
			texts = append(texts, p.label+" "+itoa(pct(p.n, b.Hits))+"%")
		}
	}
	r.MixText = strings.Join(texts, " · ")
	return r
}

// PageDetail is one Mirror Page over time.
type PageDetail struct {
	Path, Title string
	KPIs        []KPI
	Timeline    Timeline
	Referrers   []queries.PageReferrersRow
	Bots        []BotRow
	Search      []queries.SearchTotalsRow
	Queries     []queries.TopQueriesRow
	History     []PageVersion
}

// PageVersion is the page as one Content Release published it.
type PageVersion struct {
	queries.Page
	Release queries.Release
	Changed bool
}

func (s *Service) pageKPIs(ctx context.Context, f Filter, by map[string][]int64) ([]KPI, error) {
	_, prev, err := s.daily(ctx, f.Previous())
	if err != nil {
		return nil, err
	}
	hu, huc := string(visits.KindHuman), string(visits.KindHumanUnconfirmed)
	humans := make([]int64, len(by[hu]))
	for i := range humans {
		humans[i] = by[hu][i] + by[huc][i]
	}
	k := func(label string, kind visits.Kind, color, fill string) KPI {
		return kpi(label, sum(by[string(kind)]), sum(prev[string(kind)]), "", color, fill, by[string(kind)])
	}
	h := kpi("Human page views", sum(humans), sum(prev[hu])+sum(prev[huc]), "", colGreen, "#12352B", humans)
	return []KPI{
		h, k("AI fetches", visits.KindAIFetcher, colOrange, "#3A1F12"), k("AI crawler hits", visits.KindAICrawler, colOrange, "#3A1F12"),
		k("Search crawler hits", visits.KindSearchCrawler, colBlue, "#16233A"),
	}, nil
}

// Page gathers the page detail view for f.Path.
func (s *Service) Page(ctx context.Context, f Filter) (*PageDetail, error) {
	d := &PageDetail{Path: f.Path, Title: s.title(f.Path)}
	tl, data, err := s.kindTimeline(ctx, "page", f, visits.Kinds())
	if err != nil {
		return nil, err
	}
	d.Timeline = tl
	if d.KPIs, err = s.pageKPIs(ctx, f, data.by); err != nil {
		return nil, err
	}
	p := f.params()
	if d.Referrers, err = s.Q.PageReferrers(ctx, queries.PageReferrersParams{
		FromTs: p.FromTs, ToTs: p.ToTs, Locale: p.Locale, Release: p.Release, Version: p.Version, Tag: p.Tag,
		IncludeInternal: p.IncludeInternal, Path: f.Path,
	}); err != nil {
		return nil, err
	}
	bots, err := s.Q.BotTable(ctx, botParams(f))
	if err != nil {
		return nil, err
	}
	for _, b := range bots {
		d.Bots = append(d.Bots, botRow(b, s.Now()))
	}
	search := queries.SearchTotalsParams{FromDay: f.Start(), ToDay: f.End(), Path: f.path()}
	if d.Search, err = s.Q.SearchTotals(ctx, search); err != nil {
		return nil, err
	}
	if d.Queries, err = s.Q.TopQueries(ctx, queries.TopQueriesParams(search)); err != nil {
		return nil, err
	}
	d.History, err = s.history(ctx, f.Path)
	return d, err
}

func (s *Service) history(ctx context.Context, path string) ([]PageVersion, error) {
	_, rels, err := s.markers(ctx)
	if err != nil {
		return nil, err
	}
	pages, err := s.Q.ListPages(ctx)
	if err != nil {
		return nil, err
	}
	byLabel := map[string]queries.Page{}
	for _, p := range pages {
		if p.Path == path {
			byLabel[p.ReleaseLabel] = p
		}
	}
	var out []PageVersion
	prev := ""
	for _, r := range rels {
		p, ok := byLabel[r.Label]
		if !ok {
			continue
		}
		out = append(out, PageVersion{Page: p, Release: r, Changed: prev != "" && prev != p.ContentHash})
		prev = p.ContentHash
	}
	return out, nil
}
