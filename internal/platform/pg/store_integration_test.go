//go:build integration

package pg_test

import (
	"context"
	"testing"
	"testing/fstest"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
)

func TestMigratingAnUpToDateDatabaseAgainChangesNothing(t *testing.T) {
	if err := pg.Migrate(context.Background(), pgtest.URL(t)); err != nil {
		t.Fatal(err)
	}
}

func TestAnOpenStoreAnswersAndHasTheMigratedSchema(t *testing.T) {
	ctx := context.Background()
	s, err := pg.Open(ctx, pgtest.URL(t))
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

func TestAFailingMigrationFailsTheMigrate(t *testing.T) {
	bad := fstest.MapFS{"migrations/00099_x.sql": {Data: []byte("-- +goose Up\nSELECT nope;\n")}}
	if err := pg.MigrateFS(context.Background(), pgtest.URL(t), bad); err == nil {
		t.Fatal("migrated")
	}
}

func TestADirectoryWithoutMigrationsIsRefused(t *testing.T) {
	if err := pg.MigrateFS(context.Background(), pgtest.URL(t), fstest.MapFS{"migrations/readme.txt": {Data: []byte("x")}}); err == nil {
		t.Fatal("migrated")
	}
}
