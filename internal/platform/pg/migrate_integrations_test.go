package pg_test

import (
	"context"
	"database/sql"
	"io/fs"
	"testing"

	"github.com/pressly/goose/v3"

	"github.com/JorisJonkers-dev/tribelt/db"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
)

// TestIntegrationsMigration applies 00004 on top of a database holding data, then checks the
// constraints that keep a credential sealed and the audit log closed to unknown actions.
func TestIntegrationsMigration(t *testing.T) {
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
	if _, err := provider.UpTo(ctx, 3); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `INSERT INTO search_performance (source, day, page, path, query, clicks, impressions, ctr, position)
		VALUES ('google', '2026-10-01', 'https://x/', '/', 'q', 1, 2, 0.5, 3)`); err != nil {
		t.Fatal(err)
	}
	if err := pg.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	if err := pg.Migrate(ctx, url); err != nil {
		t.Fatalf("re-running is a no-op: %v", err)
	}
	var version int64
	if err := conn.QueryRowContext(ctx, `SELECT max(version_id) FROM goose_db_version`).Scan(&version); err != nil || version != 4 {
		t.Fatalf("version %d %v", version, err)
	}
	ok := func(q string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	refused := func(q string) {
		t.Helper()
		if _, err := conn.ExecContext(ctx, q); err == nil {
			t.Fatalf("accepted: %s", q)
		}
	}
	ok(`INSERT INTO integrations (kind) VALUES ('bing')`)
	ok(`INSERT INTO integrations (kind, credential, key_id, fingerprint) VALUES ('google', '\x00', 'v1-abcd', 'abcdef012345')`)
	refused(`INSERT INTO integrations (kind) VALUES ('vault')`)
	refused(`INSERT INTO integrations (kind, credential) VALUES ('cloudflare', '\x00')`)
	refused(`UPDATE integrations SET key_id = NULL WHERE kind = 'google'`)
	ok(`INSERT INTO integration_events (kind, action, ok) VALUES ('indexnow', 'notify', true)`)
	refused(`INSERT INTO integration_events (kind, action, ok) VALUES ('indexnow', 'exfiltrate', true)`)
	ok(`INSERT INTO cloudflare_daily (host, day, dimension, value, requests) VALUES ('h', '2026-10-01', 'user_agent', 'GPTBot', 3)`)
	refused(`INSERT INTO cloudflare_daily (host, day, dimension, value, requests) VALUES ('h', '2026-10-01', 'cookie', 'x', 3)`)
	var enabled bool
	var meta string
	if err := conn.QueryRowContext(ctx, `SELECT enabled, meta::text FROM integrations WHERE kind = 'bing'`).Scan(&enabled, &meta); err != nil || !enabled || meta != "{}" {
		t.Fatalf("defaults: %v %q %v", enabled, meta, err)
	}
	var n int
	if err := conn.QueryRowContext(ctx, `SELECT count(*) FROM search_performance`).Scan(&n); err != nil || n != 1 {
		t.Fatal("existing Search Performance survives")
	}
}
