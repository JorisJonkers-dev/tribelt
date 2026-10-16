package stats

import (
	"net/url"
	"testing"
	"time"
)

func TestWithoutParametersTheFilterIsTheLastThirtyDays(t *testing.T) {
	f := ParseFilter(url.Values{}, now())
	if f.To.Format(time.DateOnly) != "2026-09-30" || f.From.Format(time.DateOnly) != "2026-09-01" || len(f.Days()) != 30 {
		t.Fatalf("range %v..%v", f.From, f.To)
	}
}

// options is a filter with its range given backwards and every option set.
func options() Filter {
	return ParseFilter(url.Values{"from": {"2026-09-30"}, "to": {"2026-09-01"}, "locale": {"en"}, "release": {"v1"}, "internal": {"1"}, "path": {"/x"}}, now())
}

func TestABackwardsRangeIsTurnedAroundAndTheOptionsKept(t *testing.T) {
	if f := options(); !f.From.Before(f.To) || f.Locale != "en" || !f.IncludeInternal || *f.release() != "v1" || *f.path() != "/x" {
		t.Fatalf("filter %+v", f)
	}
}

func TestAnUnknownLocaleIsIgnored(t *testing.T) {
	if ParseFilter(url.Values{"locale": {"fr"}}, now()).Locale != "" {
		t.Fatal("kept")
	}
}

func TestTheRangeReachesBackAtMostThreeYears(t *testing.T) {
	if got := ParseFilter(url.Values{"from": {"2000-01-01"}}, now()); got.From.Year() != 2023 {
		t.Fatalf("from %v", got.From)
	}
}

func TestAFiltersQueryKeepsItsOptionsWithTheChangesGiven(t *testing.T) {
	q, _ := url.ParseQuery(options().Query("path", "", "locale", "de"))
	if q.Get("path") != "" || q.Get("locale") != "de" || q.Get("internal") != "1" || q.Get("release") != "v1" {
		t.Fatalf("query %v", q)
	}
}

func TestTheRangeEndsAtTheNextMidnight(t *testing.T) {
	if f := options(); f.End().Sub(f.To) != 24*time.Hour {
		t.Fatal(f.End())
	}
}

func TestDaysStayOnMidnightAcrossTheClockChange(t *testing.T) {
	// The last Sunday of October has 25 hours in Amsterdam.
	dst := ParseFilter(url.Values{"from": {"2026-10-24"}, "to": {"2026-10-26"}}, now())
	if len(dst.Days()) != 3 || dst.Days()[2].Hour() != 0 {
		t.Fatal(dst.Days())
	}
}
