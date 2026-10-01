package stats

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

// SourceTile is one search engine's totals.
type SourceTile struct {
	Name, Clicks, Impressions, CTR, Position string
}

// SearchPage is one Mirror Page's Search Performance.
type SearchPage struct {
	queries.SearchByPathRow
	Title string
}

// SearchView is the Search view.
type SearchView struct {
	Sources             SearchSources
	Tiles               []SourceTile
	Impressions, Clicks Bars
	XTicks              []string
	Queries             []queries.TopQueriesRow
	Pages               []SearchPage
	SVGHref, File       string
}

func sourceName(s string) string {
	if s == "bing" {
		return "Bing"
	}
	return "Google"
}

func (s *Service) searchDays(ctx context.Context, f Filter) ([]time.Time, []DaySeries, error) {
	rows, err := s.Q.SearchDaily(ctx, queries.SearchDailyParams{FromDay: f.Start(), ToDay: f.End()})
	if err != nil {
		return nil, nil, err
	}
	days, idx := dayIndex(f)
	impr, clicks := make([]int64, len(days)), make([]int64, len(days))
	for _, r := range rows {
		if i, ok := idx[r.Day.Format(time.DateOnly)]; ok {
			impr[i], clicks[i] = r.Impressions, r.Clicks
		}
	}
	return days, []DaySeries{{Label: "Impressions", Color: colBlue, Values: impr}, {Label: "Clicks", Color: colGreen, Values: clicks}}, nil
}

// SearchPerformance gathers the Search view.
func (s *Service) SearchPerformance(ctx context.Context, f Filter) (*SearchView, error) {
	v := &SearchView{
		Sources: s.searchSources(ctx), SVGHref: "/stats/chart/search.svg?" + f.Query(),
		File: "search-" + f.From.Format(time.DateOnly) + "-" + f.To.Format(time.DateOnly),
	}
	window := queries.SearchTotalsParams{FromDay: f.Start(), ToDay: f.End()}
	totals, err := s.Q.SearchTotals(ctx, window)
	if err != nil {
		return nil, err
	}
	for _, t := range totals {
		ctr := 0.0
		if t.Impressions > 0 {
			ctr = float64(t.Clicks) * 100 / float64(t.Impressions)
		}
		v.Tiles = append(v.Tiles, SourceTile{
			Name: sourceName(t.Source), Clicks: num(t.Clicks), Impressions: num(t.Impressions),
			CTR: fmt.Sprintf("%.1f%%", ctr), Position: one(t.AvgPosition),
		})
	}
	days, series, err := s.searchDays(ctx, f)
	if err != nil {
		return nil, err
	}
	v.Impressions, v.Clicks, v.XTicks = dayBars(days, series[0], 160), dayBars(days, series[1], 160), tickDays(days)
	if v.Queries, err = s.Q.TopQueries(ctx, queries.TopQueriesParams(window)); err != nil {
		return nil, err
	}
	pages, err := s.Q.SearchByPath(ctx, queries.SearchByPathParams{FromDay: f.Start(), ToDay: f.End()})
	if err != nil {
		return nil, err
	}
	sort.Slice(pages, func(i, j int) bool {
		if pages[i].Impressions != pages[j].Impressions {
			return pages[i].Impressions > pages[j].Impressions
		}
		return pages[i].Path < pages[j].Path
	})
	for _, p := range pages {
		v.Pages = append(v.Pages, SearchPage{SearchByPathRow: p, Title: s.title(p.Path)})
	}
	return v, nil
}
