package tribelt

import (
	"os"
	"strings"
	"testing"
)

func TestVersionComesFromTheManifest(t *testing.T) {
	raw, err := os.ReadFile(".release-please-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	if v := Version(); v == "dev" || !strings.Contains(string(raw), `"`+v+`"`) {
		t.Fatalf("Version() = %q, manifest %s", v, raw)
	}
	for raw, want := range map[string]string{`{".": "1.2.3"}`: "1.2.3", `{}`: "dev", `nope`: "dev", `{".": ""}`: "dev"} {
		if got := versionOf([]byte(raw)); got != want {
			t.Errorf("versionOf(%s) = %q, want %q", raw, got, want)
		}
	}
}
