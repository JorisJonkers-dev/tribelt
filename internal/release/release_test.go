package release

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/content"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

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

func TestASyncStopsAtTheFirstFailingWrite(t *testing.T) {
	c, _ := content.Load(os.DirFS("../content/testdata/content"))
	for _, at := range []string{"release", "page", "lastmod"} {
		t.Run(at, func(t *testing.T) {
			if _, err := Sync(context.Background(), failing{at: at}, c, "0.1.0"); !errors.Is(err, errBoom) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
