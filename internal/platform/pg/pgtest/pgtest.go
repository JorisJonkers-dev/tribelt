// Package pgtest gives tests a freshly migrated Postgres database each, from one shared container.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg"
)

const template = "tribelt_template"

var (
	once     sync.Once
	adminURL string
	errStart error
)

// URL returns the connection string of a new database migrated to the latest schema.
func URL(t testing.TB) string {
	t.Helper()
	if testing.Short() {
		t.Skip("needs Docker; skipped with -short")
	}
	once.Do(start)
	if errStart != nil {
		t.Fatalf("pgtest: %v", errStart)
	}
	name := "t_" + randomSuffix()
	exec(t, adminURL, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, template))
	return withDatabase(adminURL, name)
}

// EmptyURL returns the connection string of a new database without any migration applied.
func EmptyURL(t testing.TB) string {
	t.Helper()
	URL(t)
	name := "e_" + randomSuffix()
	exec(t, adminURL, "CREATE DATABASE "+name)
	return withDatabase(adminURL, name)
}

func start() {
	ctx := context.Background()
	c, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("postgres"),
		postgres.WithUsername("tribelt"),
		postgres.WithPassword("tribelt"),
		testcontainers.WithWaitStrategy(wait.ForLog("database system is ready to accept connections").WithOccurrence(2).WithStartupTimeout(60*time.Second)),
	)
	if err != nil {
		errStart = err
		return
	}
	adminURL, errStart = c.ConnectionString(ctx, "sslmode=disable")
	if errStart != nil {
		return
	}
	if errStart = execErr(ctx, adminURL, "CREATE DATABASE "+template); errStart != nil {
		return
	}
	errStart = pg.Migrate(ctx, withDatabase(adminURL, template))
}

func exec(t testing.TB, dsn, sql string) {
	t.Helper()
	if err := execErr(context.Background(), dsn, sql); err != nil {
		t.Fatalf("pgtest: %s: %v", sql, err)
	}
}

func execErr(ctx context.Context, dsn, sql string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(ctx) }()
	_, err = conn.Exec(ctx, sql)
	return err
}

func withDatabase(dsn, name string) string {
	u, err := url.Parse(dsn)
	if err != nil {
		panic(err)
	}
	u.Path = "/" + name
	return u.String()
}

func randomSuffix() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
