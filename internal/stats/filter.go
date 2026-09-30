// Package stats serves the Stats Viewer pages, CSV/SVG/SQLite exports and the Search Performance import.
package stats

import (
	"net/url"
	"slices"
	"time"
)

// Filter is the shared selection of every view: a day range in Amsterdam time, Locale, release and
// whether Internal Hits count.
type Filter struct {
	From, To        time.Time // inclusive Amsterdam days (midnight)
	Locale          string
	Release         string
	IncludeInternal bool
	Path            string
}

func amsterdam() *time.Location {
	loc, _ := time.LoadLocation("Europe/Amsterdam")
	return loc
}

func day(t time.Time) time.Time {
	t = t.In(amsterdam())
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// ParseFilter reads from, to (YYYY-MM-DD), locale, release, internal=1 and path; defaults to the last 30 days.
func ParseFilter(q url.Values, now time.Time) Filter {
	f := Filter{To: day(now), Release: q.Get("release"), IncludeInternal: q.Get("internal") == "1", Path: q.Get("path")}
	f.From = f.To.AddDate(0, 0, -29)
	if t, err := time.ParseInLocation(time.DateOnly, q.Get("from"), amsterdam()); err == nil {
		f.From = t
	}
	if t, err := time.ParseInLocation(time.DateOnly, q.Get("to"), amsterdam()); err == nil {
		f.To = t
	}
	if f.To.Before(f.From) {
		f.From, f.To = f.To, f.From
	}
	if f.To.Sub(f.From) > 3*366*24*time.Hour {
		f.From = f.To.AddDate(-3, 0, 0)
	}
	if slices.Contains([]string{"nl", "en", "de"}, q.Get("locale")) {
		f.Locale = q.Get("locale")
	}
	return f
}

// Start and End bound the filter as a half-open timestamp range.
func (f Filter) Start() time.Time { return f.From }

// End is midnight after the last day.
func (f Filter) End() time.Time { return f.To.AddDate(0, 0, 1) }

// Days lists every day in the range.
func (f Filter) Days() []time.Time {
	var out []time.Time
	for d := f.From; !d.After(f.To); d = d.AddDate(0, 0, 1) {
		out = append(out, d)
	}
	return out
}

func (f Filter) locale() *string  { return opt(f.Locale) }
func (f Filter) release() *string { return opt(f.Release) }
func (f Filter) path() *string    { return opt(f.Path) }

func opt(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Query renders the filter back into URL parameters, with overrides.
func (f Filter) Query(extra ...string) string {
	q := url.Values{"from": {f.From.Format(time.DateOnly)}, "to": {f.To.Format(time.DateOnly)}}
	if f.Locale != "" {
		q.Set("locale", f.Locale)
	}
	if f.Release != "" {
		q.Set("release", f.Release)
	}
	if f.IncludeInternal {
		q.Set("internal", "1")
	}
	if f.Path != "" {
		q.Set("path", f.Path)
	}
	for i := 0; i+1 < len(extra); i += 2 {
		if extra[i+1] == "" {
			q.Del(extra[i])
		} else {
			q.Set(extra[i], extra[i+1])
		}
	}
	return q.Encode()
}
