package stats

import (
	"context"
	"fmt"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// AgentFile is one card of the agent-facing files: robots.txt, llms.txt, llms-full.txt, .md twins.
type AgentFile struct {
	Name, Hits, Hint string
	Top              []string
}

// FetcherPath is one page an AI Fetcher read for a live answer.
type FetcherPath struct {
	Path, Format, Hits string
	W                  float64
}

// Share is one format's part of machine fetches.
type Share struct {
	Label, Color, Pct string
	X, W              float64
}

// Agents is the AI & crawlers view.
type Agents struct {
	Files    []AgentFile
	Bots     []BotRow
	Fetchers []FetcherPath
	Formats  []Share
	Timeline Timeline
	Kinds    []Tab
}

// Tab is one option of a segmented link control.
type Tab struct {
	Label, Href string
	On          bool
}

func (s *Service) agentFiles(ctx context.Context, f Filter) ([]AgentFile, error) {
	p := f.params()
	rows, err := s.Q.AgentFileClients(ctx, queries.AgentFileClientsParams(p))
	if err != nil {
		return nil, err
	}
	mdPages, err := s.Q.MarkdownPages(ctx, queries.MarkdownPagesParams(p))
	if err != nil {
		return nil, err
	}
	hits, clients, top := map[string]int64{}, map[string]int{}, map[string][]string{}
	for _, r := range rows {
		hits[r.Resource] += r.Hits
		clients[r.Resource]++
		if len(top[r.Resource]) < 3 {
			top[r.Resource] = append(top[r.Resource], r.Client)
		}
	}
	agents := func(res string) string {
		if clients[res] == 1 {
			return "Fetched by 1 agent"
		}
		return fmt.Sprintf("Fetched by %d distinct agents", clients[res])
	}
	return []AgentFile{
		{Name: "robots.txt", Hits: num(hits["robots"]), Hint: agents("robots"), Top: top["robots"]},
		{Name: "llms.txt", Hits: num(hits["llms"]), Hint: "Company and product summary · " + agents("llms"), Top: top["llms"]},
		{Name: "llms-full.txt", Hits: num(hits["llms_full"]), Hint: fmt.Sprintf("All %d pages as Markdown", s.PageCount), Top: top["llms_full"]},
		{Name: ".md twins", Hits: num(hits["markdown"]), Hint: fmt.Sprintf("%d of %d pages fetched as Markdown", mdPages, s.PageCount), Top: top["markdown"]},
	}, nil
}

func kindMatches(filter, kind string) bool {
	switch filter {
	case "":
		return true
	case "other":
		return kind == string(visits.KindSEOTool) || kind == string(visits.KindOtherBot)
	}
	return filter == kind
}

func formatShares(bots []queries.BotTableRow) []Share {
	var html, md, llms, maps int64
	for _, b := range bots {
		html, md, llms, maps = html+b.Page, md+b.Markdown, llms+b.Llms, maps+b.Robots+b.Sitemap
	}
	total := html + md + llms + maps
	parts := []struct {
		label, color string
		n            int64
	}{{"HTML", colBlue, html}, {".md twin", colOrange, md}, {"llms.txt / llms-full.txt", colSand, llms}, {"sitemap / robots", colGrey, maps}}
	var out []Share
	x := 0.0
	for _, p := range parts {
		w := 0.0
		if total > 0 {
			w = plotW * float64(p.n) / float64(total)
		}
		out = append(out, Share{Label: p.label, Color: p.color, Pct: fmt.Sprintf("%d%%", pct(p.n, total)), X: x, W: w})
		x += w
	}
	return out
}

// Agents gathers the AI & crawlers view.
func (s *Service) Agents(ctx context.Context, f Filter) (*Agents, error) {
	a := &Agents{}
	var err error
	if a.Files, err = s.agentFiles(ctx, f); err != nil {
		return nil, err
	}
	bots, err := s.Q.BotTable(ctx, botParams(f))
	if err != nil {
		return nil, err
	}
	for _, b := range bots {
		if kindMatches(f.Kind, b.VisitorKind) {
			a.Bots = append(a.Bots, botRow(b, s.Now()))
		}
	}
	a.Formats = formatShares(bots)
	for _, k := range []struct{ label, kind string }{{"All", ""}, {"AI crawler", "ai-crawler"}, {"AI fetcher", "ai-fetcher"}, {"Search crawler", "search-crawler"}, {"Other", "other"}} {
		a.Kinds = append(a.Kinds, Tab{Label: k.label, Href: "?" + f.Query("kind", k.kind), On: f.Kind == k.kind})
	}
	paths, err := s.Q.AIFetcherPaths(ctx, queries.AIFetcherPathsParams(f.params()))
	if err != nil {
		return nil, err
	}
	var top int64 = 1
	for _, p := range paths {
		top = max(top, p.Hits)
	}
	for _, p := range paths {
		a.Fetchers = append(a.Fetchers, FetcherPath{Path: p.Path, Format: p.Format, Hits: num(p.Hits), W: float64(p.Hits) * 100 / float64(top)})
	}
	a.Timeline, _, err = s.kindTimeline(ctx, "agents", f, botKinds())
	return a, err
}
