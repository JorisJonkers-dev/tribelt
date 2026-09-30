package stats

import (
	"context"
	"sort"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// Querier is the subset of generated queries the views read.
type Querier interface {
	KindTotals(ctx context.Context, arg queries.KindTotalsParams) ([]queries.KindTotalsRow, error)
	ChannelTotals(ctx context.Context, arg queries.ChannelTotalsParams) ([]queries.ChannelTotalsRow, error)
	DailyKinds(ctx context.Context, arg queries.DailyKindsParams) ([]queries.DailyKindsRow, error)
	VisitorSummary(ctx context.Context, arg queries.VisitorSummaryParams) (queries.VisitorSummaryRow, error)
	FirstTouchChannels(ctx context.Context, arg queries.FirstTouchChannelsParams) ([]queries.FirstTouchChannelsRow, error)
	OutboundTotals(ctx context.Context, arg queries.OutboundTotalsParams) ([]queries.OutboundTotalsRow, error)
	PageTable(ctx context.Context, arg queries.PageTableParams) ([]queries.PageTableRow, error)
	OutboundByPage(ctx context.Context, arg queries.OutboundByPageParams) ([]queries.OutboundByPageRow, error)
	PageReferrers(ctx context.Context, arg queries.PageReferrersParams) ([]queries.PageReferrersRow, error)
	BotTable(ctx context.Context, arg queries.BotTableParams) ([]queries.BotTableRow, error)
	AgentTopPaths(ctx context.Context, arg queries.AgentTopPathsParams) ([]queries.AgentTopPathsRow, error)
	ReleaseTotals(ctx context.Context, arg queries.ReleaseTotalsParams) ([]queries.ReleaseTotalsRow, error)
	OutboundByRelease(ctx context.Context, arg queries.OutboundByReleaseParams) ([]queries.OutboundByReleaseRow, error)
	SearchTotals(ctx context.Context, arg queries.SearchTotalsParams) ([]queries.SearchTotalsRow, error)
	SearchDaily(ctx context.Context, arg queries.SearchDailyParams) ([]queries.SearchDailyRow, error)
	SearchByPath(ctx context.Context, arg queries.SearchByPathParams) ([]queries.SearchByPathRow, error)
	TopQueries(ctx context.Context, arg queries.TopQueriesParams) ([]queries.TopQueriesRow, error)
	ListReleases(ctx context.Context) ([]queries.Release, error)
	ListPages(ctx context.Context) ([]queries.Page, error)
}

// Overview is the landing view.
type Overview struct {
	Kinds      []queries.KindTotalsRow
	Channels   []queries.ChannelTotalsRow
	FirstTouch []queries.FirstTouchChannelsRow
	Visitors   queries.VisitorSummaryRow
	Outbound   []queries.OutboundTotalsRow
	Search     []queries.SearchTotalsRow
	Queries    []queries.TopQueriesRow
	Days       []time.Time
	ByKind     map[string][]int64
	SearchDays []queries.SearchDailyRow
	Markers    []Marker
}

func (s *Service) markers(ctx context.Context) ([]Marker, []queries.Release, error) {
	rels, err := s.Q.ListReleases(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := make([]Marker, len(rels))
	for i, r := range rels {
		out[i] = Marker{Day: day(r.FirstSeenAt), Label: r.Label}
	}
	return out, rels, nil
}

func (s *Service) daily(ctx context.Context, f Filter) ([]time.Time, map[string][]int64, error) {
	rows, err := s.Q.DailyKinds(ctx, queries.DailyKindsParams{FromTs: f.Start(), ToTs: f.End(), Locale: f.locale(), Release: f.release(), IncludeInternal: f.IncludeInternal, Path: f.path()})
	if err != nil {
		return nil, nil, err
	}
	days := f.Days()
	idx := map[string]int{}
	for i, d := range days {
		idx[d.Format(time.DateOnly)] = i
	}
	by := map[string][]int64{}
	for _, k := range visits.Kinds() {
		by[string(k)] = make([]int64, len(days))
	}
	for _, r := range rows {
		if i, ok := idx[r.Day.Format(time.DateOnly)]; ok {
			by[r.VisitorKind][i] += r.Hits
		}
	}
	return days, by, nil
}

// Overview gathers the landing view.
func (s *Service) Overview(ctx context.Context, f Filter) (*Overview, error) {
	o := &Overview{}
	var err error
	common := queries.KindTotalsParams{FromTs: f.Start(), ToTs: f.End(), Locale: f.locale(), Release: f.release(), IncludeInternal: f.IncludeInternal}
	if o.Kinds, err = s.Q.KindTotals(ctx, common); err != nil {
		return nil, err
	}
	if o.Channels, err = s.Q.ChannelTotals(ctx, queries.ChannelTotalsParams(common)); err != nil {
		return nil, err
	}
	if o.Visitors, err = s.Q.VisitorSummary(ctx, queries.VisitorSummaryParams(common)); err != nil {
		return nil, err
	}
	if o.FirstTouch, err = s.Q.FirstTouchChannels(ctx, queries.FirstTouchChannelsParams{FromTs: f.Start(), ToTs: f.End(), IncludeInternal: f.IncludeInternal}); err != nil {
		return nil, err
	}
	if o.Outbound, err = s.Q.OutboundTotals(ctx, queries.OutboundTotalsParams(common)); err != nil {
		return nil, err
	}
	search := queries.SearchTotalsParams{FromDay: f.Start(), ToDay: f.End()}
	if o.Search, err = s.Q.SearchTotals(ctx, search); err != nil {
		return nil, err
	}
	if o.Queries, err = s.Q.TopQueries(ctx, queries.TopQueriesParams(search)); err != nil {
		return nil, err
	}
	if o.SearchDays, err = s.Q.SearchDaily(ctx, queries.SearchDailyParams(search)); err != nil {
		return nil, err
	}
	if o.Days, o.ByKind, err = s.daily(ctx, f); err != nil {
		return nil, err
	}
	if o.Markers, _, err = s.markers(ctx); err != nil {
		return nil, err
	}
	return o, nil
}

// PageRow is one line of the Per page view.
type PageRow struct {
	queries.PageTableRow
	Title            string
	Outbound         int64
	HumanOutbound    int64
	Clicks, Impr     int64
	Position         float64
	OutboundPerHuman float64
}

// Pages gathers the Per page view.
func (s *Service) Pages(ctx context.Context, f Filter) ([]PageRow, error) {
	common := queries.PageTableParams{FromTs: f.Start(), ToTs: f.End(), Locale: f.locale(), Release: f.release(), IncludeInternal: f.IncludeInternal}
	rows, err := s.Q.PageTable(ctx, common)
	if err != nil {
		return nil, err
	}
	out, err := s.Q.OutboundByPage(ctx, queries.OutboundByPageParams(common))
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
	res := make([]PageRow, len(rows))
	for i, r := range rows {
		o, sr := outBy[r.Path], searchBy[r.Path]
		res[i] = PageRow{PageTableRow: r, Title: s.title(r.Path), Outbound: o.Clicks, HumanOutbound: o.HumanClicks, Clicks: sr.Clicks, Impr: sr.Impressions, Position: sr.AvgPosition}
		if humans := r.Human + r.HumanUnconfirmed; humans > 0 {
			res[i].OutboundPerHuman = float64(o.HumanClicks) / float64(humans)
		}
	}
	return res, nil
}

// PageDetail is one Mirror Page over time.
type PageDetail struct {
	Path, Title string
	Days        []time.Time
	ByKind      map[string][]int64
	Referrers   []queries.PageReferrersRow
	Bots        []queries.BotTableRow
	Search      []queries.SearchTotalsRow
	Queries     []queries.TopQueriesRow
	History     []queries.Page
	Markers     []Marker
}

// Page gathers the page detail view for f.Path.
func (s *Service) Page(ctx context.Context, f Filter) (*PageDetail, error) {
	d := &PageDetail{Path: f.Path, Title: s.title(f.Path)}
	var err error
	if d.Days, d.ByKind, err = s.daily(ctx, f); err != nil {
		return nil, err
	}
	if d.Referrers, err = s.Q.PageReferrers(ctx, queries.PageReferrersParams{FromTs: f.Start(), ToTs: f.End(), Path: f.Path, Release: f.release(), IncludeInternal: f.IncludeInternal}); err != nil {
		return nil, err
	}
	if d.Bots, err = s.Q.BotTable(ctx, queries.BotTableParams{FromTs: f.Start(), ToTs: f.End(), Release: f.release(), Path: f.path()}); err != nil {
		return nil, err
	}
	search := queries.SearchTotalsParams{FromDay: f.Start(), ToDay: f.End(), Path: f.path()}
	if d.Search, err = s.Q.SearchTotals(ctx, search); err != nil {
		return nil, err
	}
	if d.Queries, err = s.Q.TopQueries(ctx, queries.TopQueriesParams(search)); err != nil {
		return nil, err
	}
	pages, err := s.Q.ListPages(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range pages {
		if p.Path == f.Path {
			d.History = append(d.History, p)
		}
	}
	if d.Markers, _, err = s.markers(ctx); err != nil {
		return nil, err
	}
	return d, nil
}

// ReleaseRow compares one Content Release with the others, per active day.
type ReleaseRow struct {
	queries.ReleaseTotalsRow
	Note               string
	FirstSeen          time.Time
	Outbound, HumanOut int64
	Clicks, Impr       int64
	Position           float64
	HumansPerDay       float64
	CrawlersPerDay     float64
	AIPerDay           float64
}

// Releases gathers the Release compare view; search figures use each release's calendar window.
func (s *Service) Releases(ctx context.Context, f Filter) ([]ReleaseRow, error) {
	_, rels, err := s.markers(ctx)
	if err != nil {
		return nil, err
	}
	totals, err := s.Q.ReleaseTotals(ctx, queries.ReleaseTotalsParams{FromTs: f.Start(), ToTs: f.End(), Locale: f.locale(), IncludeInternal: f.IncludeInternal})
	if err != nil {
		return nil, err
	}
	outs, err := s.Q.OutboundByRelease(ctx, queries.OutboundByReleaseParams{FromTs: f.Start(), ToTs: f.End(), Locale: f.locale(), IncludeInternal: f.IncludeInternal})
	if err != nil {
		return nil, err
	}
	totBy := map[string]queries.ReleaseTotalsRow{}
	for _, t := range totals {
		totBy[t.ReleaseLabel] = t
	}
	outBy := map[string]queries.OutboundByReleaseRow{}
	for _, o := range outs {
		outBy[o.ReleaseLabel] = o
	}
	rows := make([]ReleaseRow, 0, len(rels))
	for i, r := range rels {
		end := s.Now()
		if i+1 < len(rels) {
			end = rels[i+1].FirstSeenAt
		}
		search, err := s.Q.SearchTotals(ctx, queries.SearchTotalsParams{FromDay: day(r.FirstSeenAt), ToDay: day(end).AddDate(0, 0, 1)})
		if err != nil {
			return nil, err
		}
		t := totBy[r.Label]
		t.ReleaseLabel = r.Label
		row := ReleaseRow{ReleaseTotalsRow: t, Note: r.Note, FirstSeen: r.FirstSeenAt, Outbound: outBy[r.Label].Clicks, HumanOut: outBy[r.Label].HumanClicks}
		var pos, impr float64
		for _, sr := range search {
			row.Clicks += sr.Clicks
			row.Impr += sr.Impressions
			pos += sr.AvgPosition * float64(sr.Impressions)
			impr += float64(sr.Impressions)
		}
		if impr > 0 {
			row.Position = pos / impr
		}
		if t.Days > 0 {
			d := float64(t.Days)
			row.HumansPerDay = float64(t.Human+t.HumanUnconfirmed) / d
			row.CrawlersPerDay = float64(t.SearchCrawler) / d
			row.AIPerDay = float64(t.AiCrawler+t.AiFetcher) / d
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// Agents is the AI & crawlers view.
type Agents struct {
	Bots    []queries.BotTableRow
	Paths   []queries.AgentTopPathsRow
	Days    []time.Time
	ByKind  map[string][]int64
	Markers []Marker
	Formats map[string]int64
}

// Agents gathers the AI & crawlers view.
func (s *Service) Agents(ctx context.Context, f Filter) (*Agents, error) {
	a := &Agents{Formats: map[string]int64{}}
	var err error
	if a.Bots, err = s.Q.BotTable(ctx, queries.BotTableParams{FromTs: f.Start(), ToTs: f.End(), Locale: f.locale(), Release: f.release()}); err != nil {
		return nil, err
	}
	if a.Paths, err = s.Q.AgentTopPaths(ctx, queries.AgentTopPathsParams{FromTs: f.Start(), ToTs: f.End(), Locale: f.locale(), Release: f.release()}); err != nil {
		return nil, err
	}
	if a.Days, a.ByKind, err = s.daily(ctx, f); err != nil {
		return nil, err
	}
	if a.Markers, _, err = s.markers(ctx); err != nil {
		return nil, err
	}
	for _, b := range a.Bots {
		a.Formats["md"] += b.Markdown
		a.Formats["txt"] += b.Txt
		a.Formats["xml"] += b.Xml
		a.Formats["html"] += b.Hits - b.Markdown - b.Txt - b.Xml
	}
	return a, nil
}

// SortedKeys returns map keys in a stable order.
func SortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
