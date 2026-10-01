package content

import (
	"os"
	"slices"
	"testing"
)

// TestReleaseTags loads site.yml with each release block fixture in place of the fixture's own.
func TestReleaseTags(t *testing.T) {
	for file, want := range map[string]Release{
		"tags-block.yml": {Label: "v2-longtail-keywords", Note: "Product titles lead with the long-tail term", Tags: []string{"longtail-keywords", "faq-schema"}},
		"tags-flow.yml":  {Label: "v3-faq", Note: "FAQ schema on every product page", Tags: []string{"faq-schema"}},
		"no-tags.yml":    {Label: "v1-baseline", Note: "First Content Release: adapted mirror of tribelt.nl"},
	} {
		t.Run(file, func(t *testing.T) {
			block, err := os.ReadFile("testdata/release/" + file)
			if err != nil {
				t.Fatal(err)
			}
			m := fixtureFS(t)
			edit(m, "site.yml", `release: { label: v1-fixture, note: "Fixture release for tests" }`+"\n", string(block))
			c, err := Load(m)
			if err != nil {
				t.Fatal(err)
			}
			got := c.Site.Release
			if got.Label != want.Label || got.Note != want.Note || !slices.Equal(got.Tags, want.Tags) {
				t.Fatalf("release = %+v, want %+v", got, want)
			}
		})
	}
}
