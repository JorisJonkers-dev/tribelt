package stats

import (
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

func TestAChangeIsShownAsASignedPercentOrNew(t *testing.T) {
	for _, c := range []struct {
		cur, prev float64
		want      string
		up        bool
	}{{0, 0, "0%", true}, {5, 0, "new", true}, {138, 100, "+38%", true}, {96, 100, "−4%", false}, {3, 3, "0%", true}} {
		t.Run(c.want, func(t *testing.T) {
			if got, up := delta(c.cur, c.prev); got != c.want || up != c.up {
				t.Fatalf("delta(%v, %v) = %q %v", c.cur, c.prev, got, up)
			}
		})
	}
}

func TestATimeIsShownAsHowLongAgo(t *testing.T) {
	n := now()
	for d, want := range map[time.Duration]string{
		10 * time.Second: "just now", 38 * time.Minute: "38 min ago", 2 * time.Hour: "2 h ago", 30 * time.Hour: "yesterday", 80 * time.Hour: "3 days ago",
	} {
		t.Run(want, func(t *testing.T) {
			if got := ago(n.Add(-d), n); got != want {
				t.Fatalf("ago = %q", got)
			}
		})
	}
}

func TestANameIsShownAsTwoInitials(t *testing.T) {
	for name, want := range map[string]string{"joris jonkers": "JJ", "developer": "DE", "ExtraToast": "EX", "": "", "a b c": "AB"} {
		t.Run(name, func(t *testing.T) {
			if got := initials(name); got != want {
				t.Fatalf("initials = %q", got)
			}
		})
	}
}

func TestNumbersDurationsAndURLsAreShownShort(t *testing.T) {
	if num(1284) != "1,284" || num(-1234567) != "-1,234,567" || minSec(102000) != "1:42" || pct(1, 0) != 0 || shortURL("https://www.tribelt.nl/x") != "tribelt.nl/x" {
		t.Fatal("number formatting")
	}
}

func TestTheAppVersionsAreShownAsARange(t *testing.T) {
	if versionsText(nil) != "unknown" || versionsText([]string{"0.1.0", "0.2.0", "0.3.0"}) != "v0.1.0 → v0.3.0" || Summary("v0.3.0", "v1") != "v0.3.0 · v1" {
		t.Fatal("versions")
	}
}

func TestLabelsAndColours(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range visits.Kinds() {
		if KindLabel(string(k)) == string(k) || KindDot(string(k)) == "" || seen[KindColor(string(k))] {
			t.Errorf("kind %s", k)
		}
		seen[KindColor(string(k))] = true
	}
	for _, c := range visits.Channels() {
		if ChannelLabel(string(c)) == string(c) || ChannelColor(string(c)) == "" {
			t.Errorf("channel %s", c)
		}
	}
	for _, r := range visits.Resources() {
		if ResourceLabel(string(r)) == string(r) {
			t.Errorf("resource %s", r)
		}
	}
	if KindLabel("x") != "x" || ChannelLabel("x") != "x" || ResourceLabel("x") != "x" || KindColor("x") != colGrey || KindDot("x") != "bg-k-other" {
		t.Fatal("unknown values pass through")
	}
	if sourceName("bing") != "Bing" || sourceName("google") != "Google" {
		t.Fatal("source names")
	}
}

func TestAChartsTopIsTheNextRoundNumber(t *testing.T) {
	for v, want := range map[int64]int64{0: 4, 3: 4, 7: 8, 9: 20, 120: 200, 150: 200, 401: 800, 1000: 1000, 4999: 8000} {
		t.Run(strconv.FormatInt(v, 10), func(t *testing.T) {
			if got := niceTop(v); got != want {
				t.Fatalf("niceTop = %d, want %d", got, want)
			}
		})
	}
}

func september() []time.Time {
	return ParseFilter(url.Values{"from": {"2026-09-01"}, "to": {"2026-09-30"}}, now()).Days()
}

func TestAMonthIsLabelledAtFiveDays(t *testing.T) {
	if ticks := tickDays(september()); len(ticks) != 5 || ticks[0] != "1 Sep" || ticks[4] != "30 Sep" {
		t.Fatalf("ticks %v", ticks)
	}
}

func TestAFewDaysAreEachLabelled(t *testing.T) {
	if n := len(tickDays(september()[:3])); n != 3 {
		t.Fatalf("ticks %d", n)
	}
}

// oneDay is a timeline of a single day, with a marker on it and one before the range.
func oneDay() stackData {
	days := september()
	return stackData{
		days: days[:1], by: map[string][]int64{"human": {3}, "ai-crawler": {1}}, kinds: []visits.Kind{visits.KindHuman, visits.KindAICrawler},
		markers: []Marker{{Day: days[0], Label: "v1 & co", Version: "0.1.0"}, {Day: at(400), Label: "gone"}},
	}
}

func TestATimelineEscapesItsTextAndLeavesOutMarkersBeforeIt(t *testing.T) {
	svg := TimelineSVG("t <x>", oneDay())
	if !strings.Contains(svg, "t &lt;x&gt;") || !strings.Contains(svg, "v1 &amp; co") || strings.Contains(svg, "gone") || !strings.HasSuffix(svg, "</svg>") {
		t.Fatal(svg)
	}
}

func TestASingleDaySpansTheWholePlot(t *testing.T) {
	areas, top := oneDay().areas()
	if len(areas) != 2 || top != 4 || !strings.HasPrefix(areas[0].D, "M0.0,") || !strings.Contains(areas[0].D, "L1000.0,") {
		t.Fatalf("areas %+v", areas)
	}
}

func TestAMarkerOnTheRightIsLabelledLeftwards(t *testing.T) {
	days := september()
	late := stackData{
		days: days, kinds: []visits.Kind{visits.KindHuman, visits.KindAICrawler},
		by:      map[string][]int64{"human": make([]int64, 30), "ai-crawler": make([]int64, 30)},
		markers: []Marker{{Day: days[29], Label: "late"}},
	}
	if !strings.Contains(TimelineSVG("late", late), `text-anchor="end"`) {
		t.Fatal("labelled rightwards")
	}
}

func TestAnEmptySparklineStillDrawsALine(t *testing.T) {
	if s := spark(nil, 200, 40); s.Line == "" || !strings.HasSuffix(s.Area, "Z") {
		t.Fatalf("spark %+v", s)
	}
}

func TestPartsWithoutDataMakeNoSegments(t *testing.T) {
	if segments([]part{{"a", "#000", 0}}, 1) != nil {
		t.Fatal("segments")
	}
}

func TestEachPartWithDataGetsASegmentSizedToItsShare(t *testing.T) {
	segs := segments([]part{{"a", "#000", 1}, {"b", "#fff", 0}, {"c", "#111", 3}}, 0.5)
	if len(segs) != 2 || segs[1].X != 125 || !strings.Contains(segs[1].Title, "75%") {
		t.Fatalf("segments %+v", segs)
	}
}

func TestABarChartEscapesItsLabelsAndDrawsWithNoRows(t *testing.T) {
	bars := BarsSVG("b", barRows([]BarRow{{Label: "<search>", N: 5, Color: colBlue}, {Label: "x", N: 0}}))
	if !strings.Contains(bars, "&lt;search&gt;") || !strings.Contains(BarsSVG("none", nil), "</svg>") {
		t.Fatal("bars")
	}
}

func TestADayChartDrawsAColumnPerDayAndSeries(t *testing.T) {
	daySVG := DaySVG("d", september()[:2], []DaySeries{{Label: "Clicks", Color: colGreen, Values: []int64{1, 2}}})
	if strings.Count(daySVG, "<rect") != 3 {
		t.Fatalf("day columns: %s", daySVG)
	}
}

func TestFilterViewState(t *testing.T) {
	n := now()
	f := ParseFilter(url.Values{
		"hide": {"human,bogus,human,ai-crawler"}, "scale": {"share"}, "kind": {"ai-fetcher"}, "arrive": {"first"},
		"q": {" tri-flex "}, "a": {"v1"}, "b": {"v2"}, "version": {"0.2.0"}, "tag": {"faq-schema"},
	}, n)
	if len(f.Hide) != 2 || f.Scale != "share" || f.Kind != "ai-fetcher" || f.Arrive != "first" || f.Q != "tri-flex" || f.Version != "0.2.0" || f.Tag != "faq-schema" {
		t.Fatalf("view state %+v", f)
	}
	if ParseFilter(url.Values{"scale": {"x"}, "kind": {"x"}, "arrive": {"x"}}, n).Scale != "" {
		t.Fatal("unknown view state ignored")
	}
	if f.ToggleHide("human") != "ai-crawler" || f.ToggleHide("ai-fetcher") != "human,ai-crawler,ai-fetcher" || !f.Hidden("human") {
		t.Fatalf("toggle %q", f.ToggleHide("human"))
	}
	q, _ := url.ParseQuery(f.Query())
	for _, k := range []string{"hide", "scale", "kind", "arrive", "q", "a", "b", "version", "tag"} {
		if q.Get(k) == "" {
			t.Errorf("query drops %s", k)
		}
	}
	def := ParseFilter(url.Values{}, n)
	if def.Preset() != "30d" || def.RangeText() != "1 – 30 Sep 2026" {
		t.Fatalf("default preset %s %s", def.Preset(), def.RangeText())
	}
	seven, _ := url.ParseQuery(def.PresetQuery(7))
	if seven.Get("from") != "2026-09-24" || ParseFilter(seven, n).Preset() != "7d" {
		t.Fatal("7d preset")
	}
	if ParseFilter(url.Values{"from": {"2026-07-01"}, "to": {"2026-07-01"}}, n).Preset() != "custom" {
		t.Fatal("custom")
	}
	for raw, want := range map[[2]string]string{
		{"2026-07-01", "2026-07-01"}: "1 Jul 2026", {"2026-07-20", "2026-08-02"}: "20 Jul – 2 Aug 2026", {"2025-12-20", "2026-01-02"}: "20 Dec 2025 – 2 Jan 2026",
	} {
		if got := ParseFilter(url.Values{"from": {raw[0]}, "to": {raw[1]}}, n).RangeText(); got != want {
			t.Errorf("RangeText %v = %q", raw, got)
		}
	}
	p := def.Previous()
	if p.Len() != 30 || !p.To.Equal(def.From.AddDate(0, 0, -1)) {
		t.Fatalf("previous %v..%v", p.From, p.To)
	}
	if SearchSources.Any(SearchSources{}) || !(SearchSources{Bing: true}).Any() {
		t.Fatal("sources")
	}
}
