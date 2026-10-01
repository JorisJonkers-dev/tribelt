package pg_test

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" driver
	"github.com/pressly/goose/v3"

	"github.com/JorisJonkers-dev/tribelt/db"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
)

// TestResourceAndVersionBackfill applies 00001, writes rows the way v0.1.0/v0.2.0 did, then migrates
// to the latest schema and reads back what 00002 backfilled.
func TestResourceAndVersionBackfill(t *testing.T) {
	ctx := context.Background()
	url := pgtest.EmptyURL(t)
	conn, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	migrations, _ := fs.Sub(db.Migrations, "migrations")
	provider, err := goose.NewProvider(goose.DialectPostgres, conn, migrations)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.UpTo(ctx, 1); err != nil {
		t.Fatal(err)
	}
	exec := func(q string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO releases (label, note, content_hash, first_seen_at, last_seen_at) VALUES
		('v1-baseline', 'n', 'h', '2026-09-30 16:00+00', '2026-10-01 12:00+00'),
		('v0-old', 'n', 'h', '2026-09-29 10:00+00', '2026-09-29 11:00+00')`)
	hit := `INSERT INTO hits (id, ts, path, page_id, release_label, format, status, visitor_kind, user_agent, daily_hash) VALUES `
	for _, row := range []string{
		`('00000000-0000-0000-0000-000000000001', '2026-09-30 18:00+00', '/sectoren', 'sectoren', 'v1-baseline', 'html', 200, 'human', 'ua', 'd')`,
		`('00000000-0000-0000-0000-000000000002', '2026-09-30 18:00+00', '/sectoren.md', 'sectoren', 'v1-baseline', 'md', 200, 'ai-crawler', 'ua', 'd')`,
		`('00000000-0000-0000-0000-000000000003', '2026-10-01 10:00+00', '/robots.txt', NULL, 'v1-baseline', 'txt', 200, 'search-crawler', 'ua', 'd')`,
		`('00000000-0000-0000-0000-000000000004', '2026-10-01 10:00+00', '/llms.txt', NULL, 'v1-baseline', 'txt', 304, 'ai-crawler', 'ua', 'd')`,
		`('00000000-0000-0000-0000-000000000005', '2026-10-01 10:00+00', '/llms-full.txt', NULL, 'v1-baseline', 'txt', 200, 'ai-crawler', 'ua', 'd')`,
		`('00000000-0000-0000-0000-000000000006', '2026-10-01 10:00+00', '/sitemap.xml', NULL, 'v1-baseline', 'xml', 200, 'search-crawler', 'ua', 'd')`,
		`('00000000-0000-0000-0000-000000000007', '2026-10-01 10:00+00', '/transportbanden', NULL, 'v1-baseline', 'html', 301, 'search-crawler', 'ua', 'd')`,
		`('00000000-0000-0000-0000-000000000008', '2026-10-01 10:00+00', '/wp-login.php', NULL, 'v1-baseline', 'html', 404, 'other-bot', 'ua', 'd')`,
		`('00000000-0000-0000-0000-000000000009', '2026-10-01 10:00+00', '/nope.md', NULL, 'v1-baseline', 'html', 404, 'other-bot', 'ua', 'd')`,
		`('00000000-0000-0000-0000-00000000000a', '2026-10-01 11:00+00', '/sectoren', 'sectoren', 'v1-baseline', 'html', 200, 'human', 'ua', 'd')`,
	} {
		exec(hit + row)
	}
	exec(`INSERT INTO outbound_clicks (id, ts, from_path, release_label, target, visitor_kind, daily_hash) VALUES
		('00000000-0000-0000-0000-000000000010', '2026-10-01 10:00+00', '/sectoren', 'v1-baseline', 'https://www.tribelt.nl/', 'human', 'd')`)

	if err := pg.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	want := map[string][2]string{
		"1": {"page", "0.1.0"}, "2": {"markdown", "0.1.0"}, "3": {"robots", "0.2.0"}, "4": {"llms", "0.2.0"}, "5": {"llms_full", "0.2.0"},
		"6": {"sitemap", "0.2.0"}, "7": {"redirect", "0.2.0"}, "8": {"not_found", "0.2.0"}, "9": {"not_found", "0.2.0"}, "a": {"page", "0.3.0"},
	}
	rows, err := conn.QueryContext(ctx, `SELECT right(id::text, 1), resource, app_version FROM hits`)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	n := 0
	for rows.Next() {
		var id, res, ver string
		if err := rows.Scan(&id, &res, &ver); err != nil {
			t.Fatal(err)
		}
		if got := [2]string{res, ver}; got != want[id] {
			t.Errorf("hit %s: %v, want %v", id, got, want[id])
		}
		n++
	}
	if n != len(want) || rows.Err() != nil {
		t.Fatalf("%d hits, %v", n, rows.Err())
	}
	var clickVersion, v1, v1All, v0All string
	err = conn.QueryRowContext(ctx, `SELECT (SELECT app_version FROM outbound_clicks),
		(SELECT app_version FROM releases WHERE label = 'v1-baseline'), (SELECT array_to_string(app_versions, ',') FROM releases WHERE label = 'v1-baseline'),
		(SELECT array_to_string(app_versions, ',') FROM releases WHERE label = 'v0-old')`).Scan(&clickVersion, &v1, &v1All, &v0All)
	if err != nil {
		t.Fatal(err)
	}
	if clickVersion != "0.2.0" || v1 != "0.1.0" || v1All != "0.1.0,0.2.0,0.3.0" || v0All != "0.1.0" {
		t.Fatalf("click %s, v1 %s %s, v0 %s", clickVersion, v1, v1All, v0All)
	}
	// The validated checks now refuse what the classifier never produces.
	if _, err := conn.ExecContext(ctx, `UPDATE hits SET resource = 'pdf'`); err == nil {
		t.Fatal("resource check is enforced")
	}
	if _, err := conn.ExecContext(ctx, `UPDATE hits SET format = 'img', resource = 'image' WHERE right(id::text, 1) = '1'`); err != nil {
		t.Fatalf("img is a valid format: %v", err)
	}
}
