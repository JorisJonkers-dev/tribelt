//go:build integration

package release

import (
	"context"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/JorisJonkers-dev/tribelt/internal/content"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/pgtest"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

func store(t *testing.T) (*pgxpool.Pool, *queries.Queries) {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool, queries.New(pool)
}

func fixture(t *testing.T) *content.Content {
	t.Helper()
	c, err := content.Load(os.DirFS("../content/testdata/content"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// retitled syncs the fixture as 0.2.0, then syncs a release that changes only the home page's title
// as 0.3.0, and returns what each sync dated the pages.
func retitled(t *testing.T, q *queries.Queries, c *content.Content) (first, second map[string]time.Time) {
	t.Helper()
	ctx := context.Background()
	first, err := Sync(ctx, q, c, "0.2.0")
	if err != nil || len(first) != len(c.Pages) {
		t.Fatalf("first sync: %d %v", len(first), err)
	}
	time.Sleep(20 * time.Millisecond)
	c.Site.Release = content.Release{Label: "v2-new-title", Note: "retitle home", Tags: []string{"title-test"}}
	c.ByPath["/"].Title = "A new title for the home page"
	if second, err = Sync(ctx, q, c, "0.3.0"); err != nil {
		t.Fatal(err)
	}
	return first, second
}

func TestAPageWhoseTextChangedGetsANewLastMod(t *testing.T) {
	_, q := store(t)
	first, second := retitled(t, q, fixture(t))
	if !second["/"].After(first["/"]) {
		t.Fatalf("lastmod %v then %v", first["/"], second["/"])
	}
}

func TestAPageWhoseTextDidNotChangeKeepsItsLastMod(t *testing.T) {
	_, q := store(t)
	first, second := retitled(t, q, fixture(t))
	if p := "/metalen-transportbanden"; !second[p].Equal(first[p]) {
		t.Fatalf("lastmod %v then %v", first[p], second[p])
	}
}

func TestEachReleaseStoresEveryPage(t *testing.T) {
	_, q := store(t)
	c := fixture(t)
	retitled(t, q, c)
	rels, _ := q.ListReleases(context.Background())
	pages, _ := q.ListPages(context.Background())
	if len(rels) != 2 || len(pages) != 2*len(c.Pages) {
		t.Fatalf("releases=%d pages=%d", len(rels), len(pages))
	}
}

func TestSyncingAReleaseAgainOnlyAddsTheNewAppVersionOnce(t *testing.T) {
	ctx := context.Background()
	_, q := store(t)
	c := fixture(t)
	retitled(t, q, c)
	for range 2 {
		if _, err := Sync(ctx, q, c, "0.4.0"); err != nil {
			t.Fatal(err)
		}
	}
	rels, _ := q.ListReleases(ctx)
	v1, v2 := rels[0], rels[1]
	if v1.AppVersion != "0.2.0" || !slices.Equal(v1.AppVersions, []string{"0.2.0"}) || len(v1.Tags) != 0 {
		t.Fatalf("v1 %+v", v1)
	}
	if v2.AppVersion != "0.3.0" || !slices.Equal(v2.AppVersions, []string{"0.3.0", "0.4.0"}) || !slices.Equal(v2.Tags, []string{"title-test"}) {
		t.Fatalf("v2 %+v", v2)
	}
	tags, _ := q.ReleaseTags(ctx)
	versions, _ := q.AppVersions(ctx)
	if !slices.Equal(tags, []string{"title-test"}) || !slices.Equal(versions, []string{"0.2.0", "0.3.0", "0.4.0"}) {
		t.Fatalf("tags %v versions %v", tags, versions)
	}
}

func TestBeforeAnyReleaseThereIsNoPreviousOne(t *testing.T) {
	_, q := store(t)
	if label, hashes, err := Previous(context.Background(), q); label != "" || hashes != nil || err != nil {
		t.Fatalf("Previous = %q %v %v", label, hashes, err)
	}
}

func TestThePreviousReleaseIsTheLastSyncedWithItsPageHashes(t *testing.T) {
	ctx := context.Background()
	_, q := store(t)
	c := fixture(t)
	if _, err := Sync(ctx, q, c, "0.4.0"); err != nil {
		t.Fatal(err)
	}
	label, hashes, err := Previous(ctx, q)
	if err != nil || label != c.Site.Release.Label || len(hashes) != len(c.Pages) || hashes["/"] != Hashes(c)["/"] {
		t.Fatalf("Previous = %q %d %v", label, len(hashes), err)
	}
}

func TestAPreviousReleaseThatCannotBeReadIsAnError(t *testing.T) {
	pool, q := store(t)
	pool.Close()
	if _, _, err := Previous(context.Background(), q); err == nil {
		t.Fatal("no error")
	}
}
