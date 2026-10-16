//go:build integration

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

// upgradedFromV3 is a database that held data at migration 00003 and was then migrated to the
// latest schema, which adds the integrations tables in 00004.
func upgradedFromV3(t *testing.T) *sql.DB {
	conn, _ := upgradedFromV3At(t)
	return conn
}

// upgradedFromV3At is upgradedFromV3, with the database's URL.
func upgradedFromV3At(t *testing.T) (*sql.DB, string) {
	t.Helper()
	ctx := context.Background()
	url := pgtest.EmptyURL(t)
	conn, err := sql.Open("pgx", url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
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
	return conn, url
}

func accepts(t *testing.T, conn *sql.DB, q string) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), q); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func refuses(t *testing.T, conn *sql.DB, q string) {
	t.Helper()
	if _, err := conn.ExecContext(context.Background(), q); err == nil {
		t.Fatalf("accepted: %s", q)
	}
}

func TestUpgradingPastTheIntegrationsMigrationKeepsSearchPerformance(t *testing.T) {
	var n int
	if err := upgradedFromV3(t).QueryRowContext(context.Background(), `SELECT count(*) FROM search_performance`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("rows = %d, %v", n, err)
	}
}

func TestRerunningTheMigrationsAfterTheUpgradeChangesNothing(t *testing.T) {
	conn, url := upgradedFromV3At(t)
	var before, after int64
	_ = conn.QueryRowContext(context.Background(), `SELECT max(version_id) FROM goose_db_version`).Scan(&before)
	if err := pg.Migrate(context.Background(), url); err != nil {
		t.Fatal(err)
	}
	if err := conn.QueryRowContext(context.Background(), `SELECT max(version_id) FROM goose_db_version`).Scan(&after); err != nil || before < 4 || after != before {
		t.Fatalf("version %d then %d, %v", before, after, err)
	}
}

func TestANewIntegrationIsEnabledWithNoMetadata(t *testing.T) {
	conn := upgradedFromV3(t)
	accepts(t, conn, `INSERT INTO integrations (kind) VALUES ('bing')`)
	var enabled bool
	var meta string
	if err := conn.QueryRowContext(context.Background(), `SELECT enabled, meta::text FROM integrations WHERE kind = 'bing'`).Scan(&enabled, &meta); err != nil || !enabled || meta != "{}" {
		t.Fatalf("enabled %v, meta %q, %v", enabled, meta, err)
	}
}

func TestAnIntegrationOfAnUnknownKindIsRefused(t *testing.T) {
	refuses(t, upgradedFromV3(t), `INSERT INTO integrations (kind) VALUES ('vault')`)
}

func TestACredentialIsStoredOnlyWithTheKeyThatSealedIt(t *testing.T) {
	conn := upgradedFromV3(t)
	accepts(t, conn, `INSERT INTO integrations (kind, credential, key_id, fingerprint) VALUES ('google', '\x00', 'v1-abcd', 'abcdef012345')`)
	refuses(t, conn, `INSERT INTO integrations (kind, credential) VALUES ('cloudflare', '\x00')`)
	refuses(t, conn, `UPDATE integrations SET key_id = NULL WHERE kind = 'google'`)
}

func TestTheIntegrationLogTakesOnlyKnownActions(t *testing.T) {
	conn := upgradedFromV3(t)
	accepts(t, conn, `INSERT INTO integration_events (kind, action, ok) VALUES ('indexnow', 'notify', true)`)
	refuses(t, conn, `INSERT INTO integration_events (kind, action, ok) VALUES ('indexnow', 'exfiltrate', true)`)
}

func TestCloudflareDailyCountsTakeOnlyKnownDimensions(t *testing.T) {
	conn := upgradedFromV3(t)
	accepts(t, conn, `INSERT INTO cloudflare_daily (host, day, dimension, value, requests) VALUES ('h', '2026-10-01', 'user_agent', 'GPTBot', 3)`)
	refuses(t, conn, `INSERT INTO cloudflare_daily (host, day, dimension, value, requests) VALUES ('h', '2026-10-01', 'cookie', 'x', 3)`)
}
