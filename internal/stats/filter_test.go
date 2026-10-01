package stats

import (
	"net/url"
	"testing"
	"time"
)

func TestFilter(t *testing.T) {
	n := now()
	f := ParseFilter(url.Values{}, n)
	if f.To.Format(time.DateOnly) != "2026-09-30" || f.From.Format(time.DateOnly) != "2026-09-01" || len(f.Days()) != 30 {
		t.Fatalf("default range %v..%v", f.From, f.To)
	}
	f = ParseFilter(url.Values{"from": {"2026-09-30"}, "to": {"2026-09-01"}, "locale": {"en"}, "release": {"v1"}, "internal": {"1"}, "path": {"/x"}}, n)
	if !f.From.Before(f.To) || f.Locale != "en" || !f.IncludeInternal || *f.release() != "v1" || *f.path() != "/x" {
		t.Fatalf("swapped range and options %+v", f)
	}
	if ParseFilter(url.Values{"locale": {"fr"}}, n).Locale != "" {
		t.Fatal("unknown locale ignored")
	}
	if got := ParseFilter(url.Values{"from": {"2000-01-01"}}, n); got.From.Year() != 2023 {
		t.Fatalf("range capped at three years: %v", got.From)
	}
	q, _ := url.ParseQuery(f.Query("path", "", "locale", "de"))
	if q.Get("path") != "" || q.Get("locale") != "de" || q.Get("internal") != "1" || q.Get("release") != "v1" {
		t.Fatalf("query %v", q)
	}
	if f.End().Sub(f.To) != 24*time.Hour {
		t.Fatal("end is the next midnight")
	}
	// DST: the last Sunday of October has 25 hours in Amsterdam.
	dst := ParseFilter(url.Values{"from": {"2026-10-24"}, "to": {"2026-10-26"}}, n)
	if len(dst.Days()) != 3 || dst.Days()[2].Hour() != 0 {
		t.Fatal("days stay on midnight across DST")
	}
}
