package stats

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// ReleaseRow is one Content Release with its figures per active day.
type ReleaseRow struct {
	queries.ReleaseTotalsRow
	Release            queries.Release
	Outbound, HumanOut int64
	Clicks, Impr       int64
	Position           float64
	HumansPerDay       float64
	CrawlersPerDay     float64
	AIPerDay           float64
}

// Versions is the App Versions that served the release, as "v0.2.0 → v0.3.0".
func (r ReleaseRow) Versions() string { return versionsText(r.Release.AppVersions) }

func versionsText(v []string) string {
	if len(v) == 0 {
		return "unknown"
	}
	if len(v) == 1 {
		return "v" + v[0]
	}
	return "v" + v[0] + " → v" + v[len(v)-1]
}

// CompareMetric is one line of the release comparison.
type CompareMetric struct {
	Label, A, B, Delta string
	Good               bool
}

// PageChange is a Mirror Page whose text differs between the compared releases.
type PageChange struct {
	Path, TitleA, TitleB string
	WordsA, WordsB       int64
	Added, Removed       bool
}

// Compare sets two Content Releases side by side.
type Compare struct {
	A, B      *ReleaseRow
	Metrics   []CompareMetric
	Changes   []PageChange
	Unchanged int
}

// ReleasesView is the Releases view.
type ReleasesView struct {
	Rows     []ReleaseRow
	Compare  Compare
	Timeline Timeline
}

func perDay(n, days int64) float64 {
	if days == 0 {
		return 0
	}
	return float64(n) / float64(days)
}

// releaseRows gathers every Content Release; search figures use each release's calendar window.
func (s *Service) releaseRows(ctx context.Context, f Filter) ([]ReleaseRow, error) {
	_, rels, err := s.markers(ctx)
	if err != nil {
		return nil, err
	}
	p := f.params()
	totals, err := s.Q.ReleaseTotals(ctx, queries.ReleaseTotalsParams{FromTs: p.FromTs, ToTs: p.ToTs, Locale: p.Locale, Version: p.Version, Tag: p.Tag, IncludeInternal: p.IncludeInternal})
	if err != nil {
		return nil, err
	}
	outs, err := s.Q.OutboundByRelease(ctx, queries.OutboundByReleaseParams{FromTs: p.FromTs, ToTs: p.ToTs, Locale: p.Locale, Version: p.Version, Tag: p.Tag, IncludeInternal: p.IncludeInternal})
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
		row := ReleaseRow{ReleaseTotalsRow: t, Release: r, Outbound: outBy[r.Label].Clicks, HumanOut: outBy[r.Label].HumanClicks}
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
		row.HumansPerDay = perDay(t.Human+t.HumanUnconfirmed, t.Days)
		row.CrawlersPerDay = perDay(t.SearchCrawler, t.Days)
		row.AIPerDay = perDay(t.AiCrawler+t.AiFetcher, t.Days)
		rows = append(rows, row)
	}
	return rows, nil
}

func one(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }

func metric(label string, a, b float64, format func(float64) string, lowerIsBetter bool) CompareMetric {
	d, up := delta(b, a)
	good := up != lowerIsBetter || d == "0%"
	return CompareMetric{Label: label, A: format(a), B: format(b), Delta: d, Good: good}
}

func compareMetrics(a, b *ReleaseRow) []CompareMetric {
	pd := func(r *ReleaseRow, n int64) float64 { return perDay(n, r.Days) }
	count := func(v float64) string { return num(int64(v)) }
	pos := func(v float64) string {
		if v == 0 {
			return "—"
		}
		return one(v)
	}
	return []CompareMetric{
		metric("Days with Hits", float64(a.Days), float64(b.Days), count, false),
		metric("Human page views per day", a.HumansPerDay, b.HumansPerDay, one, false),
		metric("From search per day", pd(a, a.FromSearch), pd(b, b.FromSearch), one, false),
		metric("From AI chat per day", pd(a, a.FromAiChat), pd(b, b.FromAiChat), one, false),
		metric("Search crawler hits per day", a.CrawlersPerDay, b.CrawlersPerDay, one, false),
		metric("AI crawler hits per day", pd(a, a.AiCrawler), pd(b, b.AiCrawler), one, false),
		metric("AI fetches per day", pd(a, a.AiFetcher), pd(b, b.AiFetcher), one, false),
		metric("Markdown twin fetches per day", pd(a, a.Markdown), pd(b, b.Markdown), one, false),
		metric("llms.txt fetches per day", pd(a, a.Llms), pd(b, b.Llms), one, false),
		metric("Human Outbound Clicks per day", pd(a, a.HumanOut), pd(b, b.HumanOut), one, false),
		metric("Visitors", float64(a.Visitors), float64(b.Visitors), count, false),
		metric("Search impressions", float64(a.Impr), float64(b.Impr), count, false),
		metric("Search clicks", float64(a.Clicks), float64(b.Clicks), count, false),
		metric("Average position", a.Position, b.Position, pos, true),
	}
}

func pageChanges(pages []queries.Page, a, b string) ([]PageChange, int) {
	inA, inB := map[string]queries.Page{}, map[string]queries.Page{}
	var paths []string
	for _, p := range pages {
		if p.ReleaseLabel == a {
			inA[p.Path] = p
		}
		if p.ReleaseLabel == b {
			inB[p.Path] = p
		}
	}
	for path := range inA {
		paths = append(paths, path)
	}
	for path := range inB {
		if _, ok := inA[path]; !ok {
			paths = append(paths, path)
		}
	}
	sort.Strings(paths)
	var out []PageChange
	unchanged := 0
	for _, path := range paths {
		pa, okA := inA[path]
		pb, okB := inB[path]
		if okA && okB && pa.ContentHash == pb.ContentHash {
			unchanged++
			continue
		}
		out = append(out, PageChange{Path: path, TitleA: pa.Title, TitleB: pb.Title, WordsA: pa.WordCount, WordsB: pb.WordCount, Added: !okA, Removed: !okB})
	}
	return out, unchanged
}

func pick(rows []ReleaseRow, label string, fallback int) *ReleaseRow {
	for i := range rows {
		if rows[i].ReleaseLabel == label {
			return &rows[i]
		}
	}
	if fallback >= 0 && fallback < len(rows) {
		return &rows[fallback]
	}
	return nil
}

// Releases gathers the Releases view: every Content Release and two of them compared.
func (s *Service) Releases(ctx context.Context, f Filter) (*ReleasesView, error) {
	rows, err := s.releaseRows(ctx, f)
	if err != nil {
		return nil, err
	}
	v := &ReleasesView{Rows: rows}
	v.Compare.B = pick(rows, f.B, len(rows)-1)
	v.Compare.A = pick(rows, f.A, len(rows)-2)
	if v.Compare.A != nil && v.Compare.B != nil {
		v.Compare.Metrics = compareMetrics(v.Compare.A, v.Compare.B)
		pages, err := s.Q.ListPages(ctx)
		if err != nil {
			return nil, err
		}
		v.Compare.Changes, v.Compare.Unchanged = pageChanges(pages, v.Compare.A.ReleaseLabel, v.Compare.B.ReleaseLabel)
	}
	v.Timeline, _, err = s.kindTimeline(ctx, "releases", f, visits.Kinds())
	return v, err
}

// TagsText joins release tags for CSV.
func TagsText(tags []string) string { return strings.Join(tags, " ") }

// FirstSeen is when the release first went live, as a date.
func (r ReleaseRow) FirstSeen() string {
	return r.Release.FirstSeenAt.In(amsterdam()).Format(time.DateOnly)
}

// Summary is the release label with its first App Version, as shown in the header pill.
func Summary(version, label string) string {
	return fmt.Sprintf("v%s · %s", strings.TrimPrefix(version, "v"), label)
}
