package stats

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// ExportSources is what the SQLite export reads.
type ExportSources interface {
	ListReleases(ctx context.Context) ([]queries.Release, error)
	ListPages(ctx context.Context) ([]queries.Page, error)
	ListSearchPerformance(ctx context.Context) ([]queries.SearchPerformance, error)
	ListOutboundClicks(ctx context.Context) ([]queries.OutboundClick, error)
}

func exportSchema() []string {
	return []string{
		`CREATE TABLE releases (label TEXT PRIMARY KEY, note TEXT, first_seen_at TEXT, last_seen_at TEXT, app_version TEXT, app_versions TEXT, tags TEXT)`,
		`CREATE TABLE pages (release_label TEXT, path TEXT, page_id TEXT, locale TEXT, type TEXT, title TEXT, description TEXT, h1 TEXT, keywords TEXT, word_count INTEGER, content_hash TEXT, PRIMARY KEY (release_label, path))`,
		`CREATE TABLE hits (id TEXT PRIMARY KEY, ts TEXT, day TEXT, path TEXT, page_id TEXT, locale TEXT, release_label TEXT, format TEXT, status INTEGER, visitor_kind TEXT, bot_name TEXT, verified INTEGER, arrival_channel TEXT, referrer_host TEXT, referrer_name TEXT, utm_source TEXT, utm_medium TEXT, utm_campaign TEXT, utm_term TEXT, utm_content TEXT, country TEXT, user_agent TEXT, visitor TEXT, daily_visitor TEXT, internal INTEGER, beacon_confirmed INTEGER, engaged_ms INTEGER, resource TEXT, app_version TEXT)`,
		`CREATE TABLE outbound_clicks (id TEXT PRIMARY KEY, ts TEXT, from_path TEXT, page_id TEXT, locale TEXT, release_label TEXT, target TEXT, visitor_kind TEXT, bot_name TEXT, verified INTEGER, visitor TEXT, daily_visitor TEXT, internal INTEGER, country TEXT, app_version TEXT)`,
		`CREATE TABLE search_performance (source TEXT, day TEXT, page TEXT, path TEXT, query TEXT, clicks INTEGER, impressions INTEGER, ctr REAL, position REAL, PRIMARY KEY (source, day, page, query))`,
		`CREATE INDEX hits_day ON hits (day)`,
		`CREATE VIEW about AS SELECT 'Anonymised export of the Tribelt mirror. Visitor IDs and Daily Visitor hashes are replaced by per-export pseudonyms; human user-agents are reduced to the browser family.' AS note`,
	}
}

// pseudonyms replaces identifiers with numbers that are only stable inside one export.
type pseudonyms struct {
	prefix string
	seen   map[string]string
}

func (p *pseudonyms) of(v *string) any {
	if v == nil || *v == "" {
		return nil
	}
	if p.seen == nil {
		p.seen = map[string]string{}
	}
	if s, ok := p.seen[*v]; ok {
		return s
	}
	s := p.prefix + strconv.Itoa(len(p.seen)+1)
	p.seen[*v] = s
	return s
}

// BrowserFamily keeps only the browser name of a human user-agent.
func BrowserFamily(ua string) string {
	for _, b := range []struct{ token, name string }{
		{"Edg/", "Edge"},
		{"OPR/", "Opera"},
		{"SamsungBrowser/", "Samsung Internet"},
		{"Firefox/", "Firefox"},
		{"CriOS/", "Chrome"},
		{"Chrome/", "Chrome"},
		{"Safari/", "Safari"},
		{"Trident/", "Internet Explorer"},
	} {
		if strings.Contains(ua, b.token) {
			return b.name
		}
	}
	return "other"
}

func nullable[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// BuildSQLite writes the anonymised export into an in-memory SQLite database and returns its bytes.
func BuildSQLite(ctx context.Context, src ExportSources, db DB, now time.Time) ([]byte, error) {
	lite, err := sql.Open("sqlite", "file:export?mode=memory&cache=private")
	if err != nil {
		return nil, err
	}
	defer func() { _ = lite.Close() }()
	lite.SetMaxOpenConns(1)
	conn, err := lite.Conn(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	for _, stmt := range exportSchema() {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			return nil, fmt.Errorf("export schema: %w", err)
		}
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := fillExport(ctx, tx, src, db, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	var out []byte
	err = conn.Raw(func(dc any) error {
		s, ok := dc.(interface{ Serialize() ([]byte, error) })
		if !ok {
			return errors.New("sqlite driver cannot serialize")
		}
		out, err = s.Serialize()
		return err
	})
	return out, err
}

func fillExport(ctx context.Context, tx *sql.Tx, src ExportSources, db DB, now time.Time) error {
	rels, err := src.ListReleases(ctx)
	if err != nil {
		return err
	}
	for _, r := range rels {
		if _, err := tx.ExecContext(ctx, `INSERT INTO releases VALUES (?, ?, ?, ?, ?, ?, ?)`, r.Label, r.Note, r.FirstSeenAt.UTC().Format(time.RFC3339), r.LastSeenAt.UTC().Format(time.RFC3339), r.AppVersion, strings.Join(r.AppVersions, " "), strings.Join(r.Tags, " ")); err != nil {
			return err
		}
	}
	pages, err := src.ListPages(ctx)
	if err != nil {
		return err
	}
	for _, p := range pages {
		if _, err := tx.ExecContext(ctx, `INSERT INTO pages VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, p.ReleaseLabel, p.Path, p.PageID, p.Locale, p.Type, p.Title, p.Description, p.H1, strings.Join(p.Keywords, ", "), p.WordCount, p.ContentHash); err != nil {
			return err
		}
	}
	perf, err := src.ListSearchPerformance(ctx)
	if err != nil {
		return err
	}
	for _, s := range perf {
		if _, err := tx.ExecContext(ctx, `INSERT INTO search_performance VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.Source, s.Day.Format(time.DateOnly), s.Page, s.Path, s.Query, s.Clicks, s.Impressions, s.Ctr, s.Position); err != nil {
			return err
		}
	}
	visitors, daily := &pseudonyms{prefix: "v"}, &pseudonyms{prefix: "d"}
	clicks, err := src.ListOutboundClicks(ctx)
	if err != nil {
		return err
	}
	for _, c := range clicks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbound_clicks VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, c.ID.String(), c.Ts.UTC().Format(time.RFC3339), c.FromPath, nullable(c.PageID), nullable(c.Locale), c.ReleaseLabel, c.Target, c.VisitorKind, nullable(c.BotName), nullable(c.Verified), visitors.of(c.VisitorID), daily.of(&c.DailyHash), c.Internal, nullable(c.Country), c.AppVersion); err != nil {
			return err
		}
	}
	insert, err := tx.PrepareContext(ctx, `INSERT INTO hits VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer func() { _ = insert.Close() }()
	loc := amsterdam()
	return StreamHits(ctx, db, time.Unix(0, 0), now.Add(time.Hour), func(h queries.Hit) error {
		ua := h.UserAgent
		if visits.Kind(h.VisitorKind).IsHuman() {
			ua = BrowserFamily(ua)
		}
		_, err := insert.ExecContext(ctx, h.ID.String(), h.Ts.UTC().Format(time.RFC3339), h.Ts.In(loc).Format(time.DateOnly), h.Path,
			nullable(h.PageID), nullable(h.Locale), h.ReleaseLabel, h.Format, h.Status, h.VisitorKind, nullable(h.BotName), nullable(h.Verified),
			nullable(h.ArrivalChannel), nullable(h.ReferrerHost), nullable(h.ReferrerName), nullable(h.UtmSource), nullable(h.UtmMedium),
			nullable(h.UtmCampaign), nullable(h.UtmTerm), nullable(h.UtmContent), nullable(h.Country), ua, visitors.of(h.VisitorID),
			daily.of(&h.DailyHash), h.Internal, h.BeaconConfirmed, nullable(h.EngagedMs), h.Resource, h.AppVersion)
		return err
	})
}

func (s *Service) exportSQLite(w http.ResponseWriter, r *http.Request) {
	src, ok := s.Q.(ExportSources)
	if !ok {
		s.fail(w, errors.New("export sources unavailable"))
		return
	}
	now := s.Now()
	raw, err := BuildSQLite(r.Context(), src, s.DB, now)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="tribelt-mirror-%s.sqlite"`, now.Format("2006-01-02")))
	w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
	_, _ = w.Write(raw)
}
