package integrations

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestURLsOnTheHostAreBatchedOnceEach(t *testing.T) {
	var urls []string
	for i := range 25001 {
		urls = append(urls, "https://tribelt.jorisjonkers.dev/p"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+"/"+strconv.Itoa(i))
	}
	urls = append(urls, urls[0], "https://other.test/x", "ftp://tribelt.jorisjonkers.dev/x", "://bad")
	if b := Batches(urls, "tribelt.jorisjonkers.dev", BatchSize); len(b) != 3 || len(b[0]) != 10000 || len(b[2]) != 5001 {
		t.Fatalf("batches %d", len(b))
	}
}

func TestNothingToSendMakesNoBatches(t *testing.T) {
	if Batches(nil, "x", 10) != nil {
		t.Fatal("batched")
	}
}

func TestTheChangedURLsAreThoseChangedAddedOrGone(t *testing.T) {
	before := map[string]string{"/": "h1", "/a": "h2", "/gone": "h3"}
	after := map[string]string{"/": "h1", "/a": "h2b", "/new": "h4"}
	got := ChangedURLs("https://x.test", before, after)
	if strings.Join(got, ",") != "https://x.test/a,https://x.test/gone,https://x.test/new" {
		t.Fatalf("%v", got)
	}
}

func TestAFieldIsAvailableByItsLastPathSegment(t *testing.T) {
	c := Caps{Fields: []string{"dimensions.userAgent", "dimensions/clientRequestPath", "count"}}
	if !c.Has("userAgent") || !c.Has("clientRequestPath") || !c.Has("count") || c.Has("verifiedBotCategory") || c.Has("Agent") {
		t.Fatal("field paths")
	}
}

func TestWithoutKnownFieldsEveryFieldIsAvailable(t *testing.T) {
	if !(Caps{}).Has("anything") {
		t.Fatal("refused")
	}
}

func TestTheImportWindowIsEightDaysUnlessThePlanSaysOtherwise(t *testing.T) {
	if (Caps{}).Window() != 8*24*time.Hour || (Caps{NotOlderThan: 86400}).Window() != 24*time.Hour {
		t.Fatal("window")
	}
}
