package release

import (
	"context"
	"errors"
	"os"
	"slices"
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
	first, err := Sync(ctx, q, c, "0.2.0")
	if err != nil || len(first) != len(c.Pages) {
		t.Fatalf("first sync: %d %v", len(first), err)
	}
	time.Sleep(20 * time.Millisecond)

	// A new release where one page changed and the rest did not.
	c.Site.Release = content.Release{Label: "v2-new-title", Note: "retitle home", Tags: []string{"title-test"}}
	c.ByPath["/"].Title = "A new title for the home page"
	second, err := Sync(ctx, q, c, "0.3.0")
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
	// Re-running the same release is idempotent; a new App Version serving it is appended once.
	for range 2 {
		if _, err := Sync(ctx, q, c, "0.4.0"); err != nil {
			t.Fatal(err)
		}
	}
	rels, _ = q.ListReleases(ctx)
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
		if _, err := Sync(context.Background(), failing{at: at}, c, "0.1.0"); !errors.Is(err, errBoom) {
			t.Fatalf("%s: %v", at, err)
		}
	}
}

func TestPreviousRelease(t *testing.T) {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, pgtest.URL(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := queries.New(pool)
	if label, hashes, err := Previous(ctx, q); label != "" || hashes != nil || err != nil {
		t.Fatalf("no release yet: %q %v %v", label, hashes, err)
	}
	c, err := content.Load(os.DirFS("../content/testdata/content"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Sync(ctx, q, c, "0.4.0"); err != nil {
		t.Fatal(err)
	}
	label, hashes, err := Previous(ctx, q)
	if err != nil || label != c.Site.Release.Label || len(hashes) != len(c.Pages) || hashes["/"] != Hashes(c)["/"] {
		t.Fatalf("previous %q %d %v", label, len(hashes), err)
	}
	pool.Close()
	if _, _, err := Previous(ctx, q); err == nil {
		t.Fatal("a database error surfaces")
	}
}
