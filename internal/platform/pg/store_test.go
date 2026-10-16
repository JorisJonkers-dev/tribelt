package pg_test

import (
	"context"
	"testing"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg"
)

func TestOpeningAMalformedOrUnreachableDatabaseFails(t *testing.T) {
	for _, url := range []string{"postgres://%zz", "postgres://nobody@127.0.0.1:1/x?connect_timeout=1"} {
		t.Run(url, func(t *testing.T) {
			if _, err := pg.Open(context.Background(), url); err == nil {
				t.Fatal("opened")
			}
		})
	}
}

func TestMigratingAMalformedDatabaseURLFails(t *testing.T) {
	if err := pg.Migrate(context.Background(), "postgres://%zz"); err == nil {
		t.Fatal("migrated")
	}
}
