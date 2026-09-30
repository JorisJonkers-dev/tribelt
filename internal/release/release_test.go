package release

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JorisJonkers-dev/tribelt/internal/content"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

func TestSyncTracksWhenPageTextChanged(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := queries.New(pool)
	c, err := content.Load(os.DirFS("../content/testdata/content"))
	if err != nil {
		t.Fatal(err)
	}
	first, err := Sync(ctx, q, c)
	if err != nil || len(first) != len(c.Pages) {
		t.Fatalf("first sync: %d %v", len(first), err)
	}
	time.Sleep(20 * time.Millisecond)

	// A new release where one page changed and the rest did not.
	c.Site.Release = content.Release{Label: "v2-new-title", Note: "retitle home"}
	c.ByPath["/"].Title = "A new title for the home page"
	second, err := Sync(ctx, q, c)
	if err != nil {
		t.Fatal(err)
	}
	if !second["/"].After(first["/"]) {
		t.Fatal("changed page gets a new lastmod")
	}
	if !second["/metalen-transportbanden"].Equal(first["/metalen-transportbanden"]) {
		t.Fatal("unchanged page keeps its lastmod")
	}
	rels, _ := q.ListReleases(ctx)
	pages, _ := q.ListPages(ctx)
	if len(rels) != 2 || len(pages) != 2*len(c.Pages) {
		t.Fatalf("releases=%d pages=%d", len(rels), len(pages))
	}
	// Re-running the same release is idempotent.
	if _, err := Sync(ctx, q, c); err != nil {
		t.Fatal(err)
	}
}

type failing struct {
	Store
	at string
}

var errBoom = errors.New("boom")

func (f failing) UpsertRelease(_ context.Context, _ queries.UpsertReleaseParams) (time.Time, error) {
	if f.at == "release" {
		return time.Time{}, errBoom
	}
	return time.Now(), nil
}

func (f failing) UpsertPage(_ context.Context, _ queries.UpsertPageParams) error {
	if f.at == "page" {
		return errBoom
	}
	return nil
}

func (f failing) PageLastMod(context.Context, string) ([]queries.PageLastModRow, error) {
	return nil, errBoom
}

func TestSyncErrors(t *testing.T) {
	c, _ := content.Load(os.DirFS("../content/testdata/content"))
	for _, at := range []string{"release", "page", "lastmod"} {
		if _, err := Sync(context.Background(), failing{at: at}, c); !errors.Is(err, errBoom) {
			t.Fatalf("%s: %v", at, err)
		}
	}
}
