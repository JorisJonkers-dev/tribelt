package stats

import (
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

func i64(n int64) string   { return strconv.FormatInt(n, 10) }
func f64(f float64) string { return strconv.FormatFloat(f, 'f', 3, 64) }
func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func boolp(p *bool) string {
	if p == nil {
		return ""
	}
	return strconv.FormatBool(*p)
}

func (s *Service) csv(w http.ResponseWriter, r *http.Request) {
	name, ok := strings.CutSuffix(r.PathValue("file"), ".csv")
	if !ok {
		http.NotFound(w, r)
		return
	}
	f := ParseFilter(r.URL.Query(), s.Now())
	var rows [][]string
	var err error
	switch name {
	case "overview", "page":
		if name == "overview" {
			f.Path = ""
		}
		rows, err = s.dailyCSV(r.Context(), f)
	case "pages":
		rows, err = s.pagesCSV(r.Context(), f)
	case "releases":
		rows, err = s.releasesCSV(r.Context(), f)
	case "agents":
		rows, err = s.agentsCSV(r.Context(), f)
	case "search":
		rows, err = s.searchCSV(r.Context(), f)
	case "hits":
		s.hitsCSV(w, r, f)
		return
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		s.fail(w, err)
		return
	}
	writeCSV(w, name+"-"+f.From.Format(time.DateOnly)+"-"+f.To.Format(time.DateOnly), rows)
}

func writeCSV(w http.ResponseWriter, name string, rows [][]string) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.csv"`, name))
	cw := csv.NewWriter(w)
	_ = cw.WriteAll(rows)
}

func (s *Service) dailyCSV(ctx context.Context, f Filter) ([][]string, error) {
	days, by, err := s.daily(ctx, f)
	if err != nil {
		return nil, err
	}
	header := []string{"day"}
	for _, k := range visits.Kinds() {
		header = append(header, string(k))
	}
	rows := [][]string{header}
	for i, d := range days {
		row := []string{d.Format(time.DateOnly)}
		for _, k := range visits.Kinds() {
			row = append(row, i64(by[string(k)][i]))
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (s *Service) pagesCSV(ctx context.Context, f Filter) ([][]string, error) {
	pages, err := s.pageRows(ctx, f)
	if err != nil {
		return nil, err
	}
	rows := [][]string{{"path", "page_id", "locale", "title", "human", "human_unconfirmed", "search_crawler", "ai_crawler", "ai_fetcher", "other", "markdown", "avg_engaged_ms", "outbound", "human_outbound", "impressions", "clicks", "avg_position"}}
	for _, p := range pages {
		rows = append(rows, []string{p.Path, p.PageID, p.Locale, p.Title, i64(p.Human), i64(p.HumanUnconfirmed), i64(p.SearchCrawler), i64(p.AiCrawler), i64(p.AiFetcher), i64(p.Other), i64(p.Markdown), i64(p.AvgEngagedMs), i64(p.Outbound), i64(p.HumanOutbound), i64(p.Impr), i64(p.Clicks), f64(p.Position)})
	}
	return rows, nil
}

func (s *Service) releasesCSV(ctx context.Context, f Filter) ([][]string, error) {
	rels, err := s.releaseRows(ctx, f)
	if err != nil {
		return nil, err
	}
	rows := [][]string{{"release", "note", "tags", "app_version", "app_versions", "first_seen", "days", "human", "human_unconfirmed", "search_crawler", "ai_crawler", "ai_fetcher", "other", "from_search", "from_ai_chat", "markdown", "llms", "visitors", "outbound", "human_outbound", "impressions", "clicks", "avg_position", "humans_per_day", "search_crawler_per_day", "ai_per_day"}}
	for _, r := range rels {
		rel := r.Release
		rows = append(rows, []string{r.ReleaseLabel, rel.Note, TagsText(rel.Tags), rel.AppVersion, TagsText(rel.AppVersions), rel.FirstSeenAt.Format(time.RFC3339), i64(r.Days), i64(r.Human), i64(r.HumanUnconfirmed), i64(r.SearchCrawler), i64(r.AiCrawler), i64(r.AiFetcher), i64(r.Other), i64(r.FromSearch), i64(r.FromAiChat), i64(r.Markdown), i64(r.Llms), i64(r.Visitors), i64(r.Outbound), i64(r.HumanOut), i64(r.Impr), i64(r.Clicks), f64(r.Position), f64(r.HumansPerDay), f64(r.CrawlersPerDay), f64(r.AIPerDay)})
	}
	return rows, nil
}

func (s *Service) agentsCSV(ctx context.Context, f Filter) ([][]string, error) {
	a, err := s.Agents(ctx, f)
	if err != nil {
		return nil, err
	}
	rows := [][]string{{"bot_name", "operator", "visitor_kind", "hits", "verified_hits", "checked_hits", "pages", "html", "markdown", "robots", "llms", "sitemap", "other", "last_seen"}}
	for _, b := range a.Bots {
		rows = append(rows, []string{b.BotName, visits.Operator(b.BotName), b.VisitorKind, i64(b.Hits), i64(b.Verified), i64(b.Checked), i64(b.Pages), i64(b.Page), i64(b.Markdown), i64(b.Robots), i64(b.Llms), i64(b.Sitemap), i64(b.Other), b.LastSeen.Format(time.RFC3339)})
	}
	return rows, nil
}

func (s *Service) searchCSV(ctx context.Context, f Filter) ([][]string, error) {
	q, err := s.Q.TopQueries(ctx, queries.TopQueriesParams{FromDay: f.Start(), ToDay: f.End()})
	if err != nil {
		return nil, err
	}
	rows := [][]string{{"query", "source", "clicks", "impressions", "avg_position"}}
	for _, r := range q {
		rows = append(rows, []string{r.Query, r.Source, i64(r.Clicks), i64(r.Impressions), f64(r.AvgPosition)})
	}
	return rows, nil
}

// HitColumns is the raw hit export header, in table order.
func HitColumns() []string {
	return []string{
		"id", "ts", "path", "page_id", "locale", "release_label", "format", "status", "visitor_kind", "bot_name", "verified",
		"arrival_channel", "referrer_host", "referrer_name", "utm_source", "utm_medium", "utm_campaign", "utm_term", "utm_content",
		"country", "user_agent", "visitor_id", "daily_hash", "internal", "beacon_confirmed", "engaged_ms", "resource", "app_version",
	}
}

// StreamHits reads hits in [from, to) oldest first without loading them all.
func StreamHits(ctx context.Context, db DB, from, to time.Time, fn func(queries.Hit) error) error {
	rows, err := db.Query(ctx, `SELECT `+strings.Join(HitColumns(), ", ")+` FROM hits WHERE ts >= $1 AND ts < $2 ORDER BY ts`, from, to)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		h, err := pgx.RowToStructByPos[queries.Hit](rows)
		if err != nil {
			return err
		}
		if err := fn(h); err != nil {
			return err
		}
	}
	return rows.Err()
}

func hitRecord(h queries.Hit) []string {
	engaged := ""
	if h.EngagedMs != nil {
		engaged = i64(*h.EngagedMs)
	}
	return []string{
		h.ID.String(), h.Ts.UTC().Format(time.RFC3339Nano), h.Path, str(h.PageID), str(h.Locale), h.ReleaseLabel, h.Format,
		i64(h.Status), h.VisitorKind, str(h.BotName), boolp(h.Verified), str(h.ArrivalChannel), str(h.ReferrerHost), str(h.ReferrerName),
		str(h.UtmSource), str(h.UtmMedium), str(h.UtmCampaign), str(h.UtmTerm), str(h.UtmContent), str(h.Country), h.UserAgent,
		str(h.VisitorID), h.DailyHash, strconv.FormatBool(h.Internal), strconv.FormatBool(h.BeaconConfirmed), engaged, h.Resource, h.AppVersion,
	}
}

func (s *Service) hitsCSV(w http.ResponseWriter, r *http.Request, f Filter) {
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="hits-%s-%s.csv"`, f.From.Format(time.DateOnly), f.To.Format(time.DateOnly)))
	cw := csv.NewWriter(w)
	_ = cw.Write(HitColumns())
	err := StreamHits(r.Context(), s.DB, f.Start(), f.End(), func(h queries.Hit) error {
		if !f.IncludeInternal && h.Internal {
			return nil
		}
		return cw.Write(hitRecord(h))
	})
	cw.Flush()
	if err != nil {
		s.Log.Error("hits export failed", "error", err)
	}
}
