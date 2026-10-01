// Package stats serves the Stats Viewer pages, CSV/SVG/SQLite exports and the Search Performance import.
package stats

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// Filter is the shared selection of every view: a day range in Amsterdam time, Locale, Content Release,
// App Version, release tag and whether Internal Hits count, plus the small view state links carry.
type Filter struct {
	From, To        time.Time // inclusive Amsterdam days (midnight)
	Today           time.Time
	Locale          string
	Release         string
	Version         string
	Tag             string
	IncludeInternal bool
	Path            string
	// Hide lists Visitor Kinds left out of the timelines.
	Hide []string
	// Scale is "share" for 100% stacked resource bars.
	Scale string
	// Kind narrows the agent table: ai-crawler, ai-fetcher, search-crawler or other.
	Kind string
	// Arrive is "first" for First-touch Channels instead of page views.
	Arrive string
	// Q filters the page list by path or title.
	Q string
	// A and B are the Content Releases compared.
	A, B string
}

func amsterdam() *time.Location {
	loc, _ := time.LoadLocation("Europe/Amsterdam")
	return loc
}

func day(t time.Time) time.Time {
	t = t.In(amsterdam())
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func clipped(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

func oneOf(v string, allowed ...string) string {
	if slices.Contains(allowed, v) {
		return v
	}
	return ""
}

// ParseFilter reads the query; defaults to the last 30 days.
func ParseFilter(q url.Values, now time.Time) Filter {
	f := Filter{
		To: day(now), Today: day(now), Release: clipped(q.Get("release"), 64), Version: clipped(q.Get("version"), 32),
		Tag: clipped(q.Get("tag"), 40), IncludeInternal: q.Get("internal") == "1", Path: clipped(q.Get("path"), 1024),
		Scale: oneOf(q.Get("scale"), "share"), Kind: oneOf(q.Get("kind"), "ai-crawler", "ai-fetcher", "search-crawler", "other"),
		Arrive: oneOf(q.Get("arrive"), "first"), Q: clipped(q.Get("q"), 100), A: clipped(q.Get("a"), 64), B: clipped(q.Get("b"), 64),
		Locale: oneOf(q.Get("locale"), "nl", "en", "de"),
	}
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
	for _, k := range strings.Split(q.Get("hide"), ",") {
		if slices.Contains(visits.Kinds(), visits.Kind(k)) && !slices.Contains(f.Hide, k) {
			f.Hide = append(f.Hide, k)
		}
	}
	return f
}

// Start and End bound the filter as a half-open timestamp range.
func (f Filter) Start() time.Time { return f.From }

// End is midnight after the last day.
func (f Filter) End() time.Time { return f.To.AddDate(0, 0, 1) }

// Len is the number of days in the range.
func (f Filter) Len() int { return len(f.Days()) }

// Days lists every day in the range.
func (f Filter) Days() []time.Time {
	var out []time.Time
	for d := f.From; !d.After(f.To); d = d.AddDate(0, 0, 1) {
		out = append(out, d)
	}
	return out
}

// Previous is the equal-length period just before this one.
func (f Filter) Previous() Filter {
	p := f
	p.To = f.From.AddDate(0, 0, -1)
	p.From = p.To.AddDate(0, 0, 1-f.Len())
	return p
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

// params is the shared filter of every Hit query; each query's generated params convert from it.
func (f Filter) params() queries.KindTotalsParams {
	return queries.KindTotalsParams{
		FromTs: f.Start(), ToTs: f.End(), Locale: f.locale(), Release: f.release(), Version: opt(f.Version), Tag: opt(f.Tag),
		IncludeInternal: f.IncludeInternal,
	}
}

// Hidden reports whether a Visitor Kind is left out of the timelines.
func (f Filter) Hidden(kind string) bool { return slices.Contains(f.Hide, kind) }

// ToggleHide is the hide parameter with kind flipped.
func (f Filter) ToggleHide(kind string) string {
	var out []string
	for _, k := range visits.Kinds() {
		if f.Hidden(string(k)) != (string(k) == kind) {
			out = append(out, string(k))
		}
	}
	return strings.Join(out, ",")
}

// Query renders the filter back into URL parameters, with overrides as key, value pairs ("" removes).
func (f Filter) Query(extra ...string) string {
	q := url.Values{"from": {f.From.Format(time.DateOnly)}, "to": {f.To.Format(time.DateOnly)}}
	for k, v := range map[string]string{
		"locale": f.Locale, "release": f.Release, "version": f.Version, "tag": f.Tag, "path": f.Path, "hide": strings.Join(f.Hide, ","),
		"scale": f.Scale, "kind": f.Kind, "arrive": f.Arrive, "q": f.Q, "a": f.A, "b": f.B,
	} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if f.IncludeInternal {
		q.Set("internal", "1")
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

// Preset names the selected date range: 7d, 30d, 90d or custom.
func (f Filter) Preset() string {
	if f.To.Equal(f.Today) {
		for _, n := range []int{7, 30, 90} {
			if f.From.Equal(f.Today.AddDate(0, 0, 1-n)) {
				return strconv.Itoa(n) + "d"
			}
		}
	}
	return "custom"
}

// PresetQuery is the query of a preset range of days ending today.
func (f Filter) PresetQuery(days int) string {
	return f.Query("from", f.Today.AddDate(0, 0, 1-days).Format(time.DateOnly), "to", f.Today.Format(time.DateOnly))
}

// RangeText is the range as "1 – 30 Oct 2026".
func (f Filter) RangeText() string {
	switch {
	case f.From.Equal(f.To):
		return f.To.Format("2 Jan 2006")
	case f.From.Year() != f.To.Year():
		return f.From.Format("2 Jan 2006") + " – " + f.To.Format("2 Jan 2006")
	case f.From.Month() != f.To.Month():
		return f.From.Format("2 Jan") + " – " + f.To.Format("2 Jan 2006")
	}
	return f.From.Format("2") + " – " + f.To.Format("2 Jan 2006")
}
