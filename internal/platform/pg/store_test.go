package pg_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
)

func TestOpenMigrate(t *testing.T) {
	ctx := context.Background()
	url := pgtest.URL(t)
	if err := pg.Migrate(ctx, url); err != nil {
		t.Fatal("migrations are idempotent:", err)
	}
	s, err := pg.Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Ping(ctx) != nil || s.Pool() == nil || s.Q() == nil {
		t.Fatal("store")
	}
	if _, err := s.Q().ListReleases(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestErrors(t *testing.T) {
	ctx := context.Background()
	if _, err := pg.Open(ctx, "postgres://%zz"); err == nil {
		t.Fatal("bad dsn")
	}
	if _, err := pg.Open(ctx, "postgres://nobody@127.0.0.1:1/x?connect_timeout=1"); err == nil {
		t.Fatal("unreachable")
	}
	if err := pg.Migrate(ctx, "postgres://%zz"); err == nil {
		t.Fatal("bad dsn")
	}
	bad := fstest.MapFS{"migrations/00099_x.sql": {Data: []byte("-- +goose Up\nSELECT nope;\n")}}
	if err := pg.MigrateFS(ctx, pgtest.URL(t), bad); err == nil {
		t.Fatal("failing migration")
	}
	if err := pg.MigrateFS(ctx, pgtest.URL(t), fstest.MapFS{"migrations/readme.txt": {Data: []byte("x")}}); err == nil {
		t.Fatal("no migrations")
	}
}
