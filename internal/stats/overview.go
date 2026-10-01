package stats

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// KPI is one key-figure tile: value, change against the previous equal-length period and a sparkline.
type KPI struct {
	Label, Value, Delta, Hint, Color, Fill string
	Up                                     bool
	Spark                                  Spark
}

// FetchRow is one resource class of "What was fetched", split by Visitor Kind.
type FetchRow struct {
	Resource, Label, Example, Total, Top, TopShare string
	Segs                                           []Seg
}

// BotKindRow is one automated Visitor Kind with how much of it passed the IP check.
type BotKindRow struct {
	Kind, Label, Dot, Hits, Verified, Pages string
	VerifiedW                               int
}

// SearchSummary is the Overview's Search Performance panel.
type SearchSummary struct {
	Sources SearchSources
	Totals  []queries.SearchTotalsRow
	Queries []queries.TopQueriesRow
}

// Overview is the landing view.
type Overview struct {
	KPIs      []KPI
	Timeline  Timeline
	Fetched   []FetchRow
	Arrive    []BarRow
	Bots      []BotKindRow
	Pages     []PageRow
	PageTotal int
	Search    SearchSummary
}

// figures are the KPI inputs of one period.
type figures struct {
	visitors queries.VisitorSummaryRow
	kinds    map[string]int64
	outbound int64
	humanOut int64
}

func (s *Service) figures(ctx context.Context, f Filter) (figures, error) {
	p := f.params()
	var fg figures
	var err error
	if fg.visitors, err = s.Q.VisitorSummary(ctx, queries.VisitorSummaryParams(p)); err != nil {
		return fg, err
	}
	kinds, err := s.Q.KindTotals(ctx, p)
	if err != nil {
		return fg, err
	}
	fg.kinds = map[string]int64{}
	for _, k := range kinds {
		fg.kinds[k.VisitorKind] = k.Hits
	}
	out, err := s.Q.OutboundTotals(ctx, queries.OutboundTotalsParams(p))
	if err != nil {
		return fg, err
	}
	for _, o := range out {
		fg.outbound += o.Clicks
		if visits.Kind(o.VisitorKind).IsHuman() {
			fg.humanOut += o.Clicks
		}
	}
	return fg, nil
}

func (fg figures) humans() int64 { return fg.visitors.Visitors + fg.visitors.DailyVisitors }

func kpi(label string, cur, prev int64, hint, color, fill string, series []int64) KPI {
	d, up := delta(float64(cur), float64(prev))
	return KPI{Label: label, Value: num(cur), Delta: d, Up: up, Hint: hint, Color: color, Fill: fill, Spark: spark(series, 200, 40)}
}

// topNames lists up to n bot names of one Visitor Kind, busiest first.
func topNames(bots []queries.BotTableRow, kind visits.Kind, n int) string {
	var names []string
	for _, b := range bots {
		if b.VisitorKind == string(kind) && b.BotName != "" && !slices.Contains(names, b.BotName) && len(names) < n {
			names = append(names, b.BotName)
		}
	}
	return strings.Join(names, ", ")
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func (s *Service) kpis(ctx context.Context, f Filter, by map[string][]int64, bots []queries.BotTableRow, kinds []queries.BotKindsRow) ([]KPI, error) {
	cur, err := s.figures(ctx, f)
	if err != nil {
		return nil, err
	}
	prev, err := s.figures(ctx, f.Previous())
	if err != nil {
		return nil, err
	}
	days, idx := dayIndex(f)
	p := f.params()
	humanDays := make([]int64, len(days))
	hv, err := s.Q.DailyHumanVisitors(ctx, queries.DailyHumanVisitorsParams(p))
	if err != nil {
		return nil, err
	}
	for _, r := range hv {
		humanDays[idx[r.Day.Format(time.DateOnly)]] = r.Visitors
	}
	outDays := make([]int64, len(days))
	od, err := s.Q.DailyOutbound(ctx, queries.DailyOutboundParams(p))
	if err != nil {
		return nil, err
	}
	for _, r := range od {
		outDays[idx[r.Day.Format(time.DateOnly)]] = r.Clicks
	}
	var aiVerified int64
	for _, k := range kinds {
		if k.VisitorKind == string(visits.KindAICrawler) {
			aiVerified = k.Verified
		}
	}
	ai, search, fetch := string(visits.KindAICrawler), string(visits.KindSearchCrawler), string(visits.KindAIFetcher)
	return []KPI{
		kpi("Human visitors", cur.humans(), prev.humans(), num(cur.visitors.ReturningVisitors)+" Returning Visitors", colGreen, "#12352B", humanDays),
		kpi("AI fetches", cur.kinds[fetch], prev.kinds[fetch], orDefault(topNames(bots, visits.KindAIFetcher, 3), "Live answers in ChatGPT, Perplexity, Claude"), colOrange, "#3A1F12", by[fetch]),
		kpi("AI crawler hits", cur.kinds[ai], prev.kinds[ai], fmt.Sprintf("%d%% verified against IP ranges", pct(aiVerified, cur.kinds[ai])), colOrange, "#3A1F12", by[ai]),
		kpi("Search crawler hits", cur.kinds[search], prev.kinds[search], orDefault(topNames(bots, visits.KindSearchCrawler, 3), "None in this range"), colBlue, "#16233A", by[search]),
		kpi("Outbound Clicks", cur.outbound, prev.outbound, fmt.Sprintf("%d%% of Human visitors", pct(cur.humanOut, cur.humans())), colInk, "#262B30", outDays),
	}, nil
}

// fetched builds "What was fetched": one row per resource class, Outbound Clicks from their own table.
func (s *Service) fetched(ctx context.Context, f Filter) ([]FetchRow, error) {
	p := f.params()
	kinds, err := s.Q.ResourceKinds(ctx, queries.ResourceKindsParams(p))
	if err != nil {
		return nil, err
	}
	clients, err := s.Q.ResourceTopClients(ctx, queries.ResourceTopClientsParams(p))
	if err != nil {
		return nil, err
	}
	paths, err := s.Q.ResourceTopPaths(ctx, queries.ResourceTopPathsParams(p))
	if err != nil {
		return nil, err
	}
	outs, err := s.Q.OutboundBreakdown(ctx, queries.OutboundBreakdownParams(p))
	if err != nil {
		return nil, err
	}
	counts := map[string]map[string]int64{}
	add := func(res, kind string, n int64) {
		if counts[res] == nil {
			counts[res] = map[string]int64{}
		}
		counts[res][kind] += n
	}
	for _, k := range kinds {
		add(k.Resource, k.VisitorKind, k.Hits)
	}
	top, example := map[string]int64{}, map[string]string{}
	topName := map[string]string{}
	for _, c := range clients {
		topName[c.Resource], top[c.Resource] = c.Client, c.Hits
	}
	for _, pth := range paths {
		example[pth.Resource] = pth.Path
	}
	outClient, outTarget := map[string]int64{}, map[string]int64{}
	for _, o := range outs {
		add(string(visits.ResourceOutbound), o.VisitorKind, o.Clicks)
		client := o.BotName
		if visits.Kind(o.VisitorKind).IsHuman() || client == "" {
			client = "Browsers"
		}
		outClient[client] += o.Clicks
		outTarget[o.Target] += o.Clicks
	}
	if name, n := busiest(outClient); n > 0 {
		topName["outbound"], top["outbound"] = name, n
		t, _ := busiest(outTarget)
		example["outbound"] = "/go → " + shortURL(t)
	}
	return fetchRows(counts, topName, top, example, f.Scale == "share"), nil
}

func busiest(m map[string]int64) (string, int64) {
	var name string
	var n int64
	for _, k := range SortedKeys(m) {
		if m[k] > n {
			name, n = k, m[k]
		}
	}
	return name, n
}

func fetchRows(counts map[string]map[string]int64, topName map[string]string, top map[string]int64, example map[string]string, share bool) []FetchRow {
	var maxTotal int64
	totals := map[string]int64{}
	for res, byKind := range counts {
		for _, n := range byKind {
			totals[res] += n
		}
		maxTotal = max(maxTotal, totals[res])
	}
	var rows []FetchRow
	for _, r := range visits.Resources() {
		res := string(r)
		total := totals[res]
		row := FetchRow{Resource: res, Label: ResourceLabel(res), Example: orDefault(example[res], "—"), Total: num(total), Top: "—"}
		if total > 0 {
			var parts []part
			for _, k := range visits.Kinds() {
				parts = append(parts, part{label: KindLabel(string(k)), color: KindColor(string(k)), n: counts[res][string(k)]})
			}
			scale := 1.0
			if !share {
				scale = max(float64(total)/float64(maxTotal), 0.02)
			}
			row.Segs = segments(parts, scale)
			noun := "requests"
			if r == visits.ResourceOutbound {
				noun = "clicks"
			}
			row.Top, row.TopShare = topName[res], fmt.Sprintf("%d%% of %s", pct(top[res], total), noun)
		}
		rows = append(rows, row)
	}
	return rows
}

func (s *Service) arrive(ctx context.Context, f Filter) ([]BarRow, error) {
	got := map[string]int64{}
	if f.Arrive == "first" {
		rows, err := s.Q.FirstTouchChannels(ctx, queries.FirstTouchChannelsParams{FromTs: f.Start(), ToTs: f.End(), IncludeInternal: f.IncludeInternal})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			got[r.Channel] = r.Visitors
		}
	} else {
		rows, err := s.Q.ChannelTotals(ctx, queries.ChannelTotalsParams(f.params()))
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			got[r.Channel] = r.Hits
		}
	}
	var out []BarRow
	for _, c := range channelOrder() {
		out = append(out, BarRow{Key: string(c), Label: ChannelLabel(string(c)), Color: ChannelColor(string(c)), N: got[string(c)]})
	}
	return barRows(out), nil
}

func botKindRows(rows []queries.BotKindsRow) []BotKindRow {
	by := map[string]queries.BotKindsRow{}
	for _, r := range rows {
		by[r.VisitorKind] = r
	}
	var out []BotKindRow
	for _, k := range botKinds() {
		r := by[string(k)]
		v := pct(r.Verified, r.Hits)
		out = append(out, BotKindRow{
			Kind: string(k), Label: KindLabel(string(k)), Dot: KindDot(string(k)), Hits: num(r.Hits),
			Verified: fmt.Sprintf("%d%%", v), VerifiedW: v, Pages: num(r.Pages),
		})
	}
	return out
}

// Overview gathers the landing view.
func (s *Service) Overview(ctx context.Context, f Filter) (*Overview, error) {
	o := &Overview{Search: SearchSummary{Sources: s.searchSources(ctx)}}
	tl, d, err := s.kindTimeline(ctx, "timeline", f, visits.Kinds())
	if err != nil {
		return nil, err
	}
	o.Timeline = tl
	p := f.params()
	bots, err := s.Q.BotTable(ctx, botParams(f))
	if err != nil {
		return nil, err
	}
	kinds, err := s.Q.BotKinds(ctx, queries.BotKindsParams(p))
	if err != nil {
		return nil, err
	}
	o.Bots = botKindRows(kinds)
	if o.KPIs, err = s.kpis(ctx, f, d.by, bots, kinds); err != nil {
		return nil, err
	}
	if o.Fetched, err = s.fetched(ctx, f); err != nil {
		return nil, err
	}
	if o.Arrive, err = s.arrive(ctx, f); err != nil {
		return nil, err
	}
	pages, err := s.Pages(ctx, f, 8)
	if err != nil {
		return nil, err
	}
	o.Pages, o.PageTotal = pages.Rows, pages.Total
	search := queries.SearchTotalsParams{FromDay: f.Start(), ToDay: f.End()}
	if o.Search.Totals, err = s.Q.SearchTotals(ctx, search); err != nil {
		return nil, err
	}
	q, err := s.Q.TopQueries(ctx, queries.TopQueriesParams(search))
	if err != nil {
		return nil, err
	}
	o.Search.Queries = q[:min(len(q), 5)]
	return o, nil
}

func botParams(f Filter) queries.BotTableParams {
	p := f.params()
	return queries.BotTableParams{
		FromTs: p.FromTs, ToTs: p.ToTs, Locale: p.Locale, Release: p.Release, Version: p.Version, Tag: p.Tag,
		IncludeInternal: p.IncludeInternal, Path: f.path(),
	}
}
