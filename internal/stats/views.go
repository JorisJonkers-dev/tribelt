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
	DailyHumanVisitors(ctx context.Context, arg queries.DailyHumanVisitorsParams) ([]queries.DailyHumanVisitorsRow, error)
	VisitorSummary(ctx context.Context, arg queries.VisitorSummaryParams) (queries.VisitorSummaryRow, error)
	FirstTouchChannels(ctx context.Context, arg queries.FirstTouchChannelsParams) ([]queries.FirstTouchChannelsRow, error)
	OutboundTotals(ctx context.Context, arg queries.OutboundTotalsParams) ([]queries.OutboundTotalsRow, error)
	DailyOutbound(ctx context.Context, arg queries.DailyOutboundParams) ([]queries.DailyOutboundRow, error)
	OutboundBreakdown(ctx context.Context, arg queries.OutboundBreakdownParams) ([]queries.OutboundBreakdownRow, error)
	PageTable(ctx context.Context, arg queries.PageTableParams) ([]queries.PageTableRow, error)
	DailyPathHumans(ctx context.Context, arg queries.DailyPathHumansParams) ([]queries.DailyPathHumansRow, error)
	OutboundByPage(ctx context.Context, arg queries.OutboundByPageParams) ([]queries.OutboundByPageRow, error)
	PageReferrers(ctx context.Context, arg queries.PageReferrersParams) ([]queries.PageReferrersRow, error)
	BotTable(ctx context.Context, arg queries.BotTableParams) ([]queries.BotTableRow, error)
	BotKinds(ctx context.Context, arg queries.BotKindsParams) ([]queries.BotKindsRow, error)
	ResourceKinds(ctx context.Context, arg queries.ResourceKindsParams) ([]queries.ResourceKindsRow, error)
	ResourceTopClients(ctx context.Context, arg queries.ResourceTopClientsParams) ([]queries.ResourceTopClientsRow, error)
	ResourceTopPaths(ctx context.Context, arg queries.ResourceTopPathsParams) ([]queries.ResourceTopPathsRow, error)
	AgentFileClients(ctx context.Context, arg queries.AgentFileClientsParams) ([]queries.AgentFileClientsRow, error)
	MarkdownPages(ctx context.Context, arg queries.MarkdownPagesParams) (int64, error)
	AIFetcherPaths(ctx context.Context, arg queries.AIFetcherPathsParams) ([]queries.AIFetcherPathsRow, error)
	ReleaseTotals(ctx context.Context, arg queries.ReleaseTotalsParams) ([]queries.ReleaseTotalsRow, error)
	OutboundByRelease(ctx context.Context, arg queries.OutboundByReleaseParams) ([]queries.OutboundByReleaseRow, error)
	SearchTotals(ctx context.Context, arg queries.SearchTotalsParams) ([]queries.SearchTotalsRow, error)
	SearchDaily(ctx context.Context, arg queries.SearchDailyParams) ([]queries.SearchDailyRow, error)
	SearchByPath(ctx context.Context, arg queries.SearchByPathParams) ([]queries.SearchByPathRow, error)
	TopQueries(ctx context.Context, arg queries.TopQueriesParams) ([]queries.TopQueriesRow, error)
	ListReleases(ctx context.Context) ([]queries.Release, error)
	ListPages(ctx context.Context) ([]queries.Page, error)
	ReleaseTags(ctx context.Context) ([]string, error)
	AppVersions(ctx context.Context) ([]string, error)
}

// SearchSources says which Search Performance imports have credentials.
type SearchSources struct {
	Google, Bing bool
	// Managed means the Integrations page exists to connect them.
	Managed bool
}

// Any reports whether at least one import runs.
func (s SearchSources) Any() bool { return s.Google || s.Bing }

// searchSources is live when Integrations are mounted: a source saved in the UI counts at once.
func (s *Service) searchSources(ctx context.Context) SearchSources {
	if s.Integrations == nil {
		return s.Search
	}
	g, b := s.Integrations.SearchSources(ctx)
	return SearchSources{Google: g, Bing: b, Managed: true}
}

func (s *Service) markers(ctx context.Context) ([]Marker, []queries.Release, error) {
	rels, err := s.Q.ListReleases(ctx)
	if err != nil {
		return nil, nil, err
	}
	out := make([]Marker, len(rels))
	for i, r := range rels {
		out[i] = Marker{Day: day(r.FirstSeenAt), Label: r.Label, Version: r.AppVersion, Note: r.Note}
	}
	return out, rels, nil
}

// dayIndex maps each day of f to its position.
func dayIndex(f Filter) ([]time.Time, map[string]int) {
	days := f.Days()
	idx := make(map[string]int, len(days))
	for i, d := range days {
		idx[d.Format(time.DateOnly)] = i
	}
	return days, idx
}

func (s *Service) daily(ctx context.Context, f Filter) ([]time.Time, map[string][]int64, error) {
	p := f.params()
	rows, err := s.Q.DailyKinds(ctx, queries.DailyKindsParams{
		FromTs: p.FromTs, ToTs: p.ToTs, Locale: p.Locale, Release: p.Release, Version: p.Version, Tag: p.Tag,
		IncludeInternal: p.IncludeInternal, Path: f.path(),
	})
	if err != nil {
		return nil, nil, err
	}
	days, idx := dayIndex(f)
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

// kindTimeline is the stacked timeline of the given kinds with release markers.
func (s *Service) kindTimeline(ctx context.Context, name string, f Filter, kinds []visits.Kind) (Timeline, stackData, error) {
	days, by, err := s.daily(ctx, f)
	if err != nil {
		return Timeline{}, stackData{}, err
	}
	m, _, err := s.markers(ctx)
	if err != nil {
		return Timeline{}, stackData{}, err
	}
	d := stackData{days: days, by: by, kinds: kinds, hidden: f.Hidden, markers: m}
	return timeline(name, d, f), d, nil
}

func sum(v []int64) int64 {
	var n int64
	for _, x := range v {
		n += x
	}
	return n
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
