package stats

import (
	"fmt"
	"html"
	"math"
	"strings"
	"time"
)

// Series is one line of a timeline.
type Series struct {
	Name   string
	Color  string
	Values []int64
}

// Marker is a Content Release on a timeline.
type Marker struct {
	Day   time.Time
	Label string
}

// Bar is one bar of a bar chart.
type Bar struct {
	Label string
	Value int64
	Color string
}

const (
	chartW, chartH       = 820, 280
	padL, padR, padT, pb = 48, 16, 34, 30
	font                 = `font-family="system-ui,-apple-system,Segoe UI,Roboto,sans-serif"`
)

// KindColor gives every Visitor Kind and Arrival Channel a stable, colour-blind-friendly colour.
func KindColor(k string) string {
	switch k {
	case "human", "search":
		return "#0b6e75"
	case "human-unconfirmed", "direct":
		return "#7fb8bd"
	case "search-crawler", "referral":
		return "#3b5bdb"
	case "ai-crawler", "ai-chat":
		return "#c2410c"
	case "ai-fetcher", "social":
		return "#a21caf"
	case "seo-tool", "campaign":
		return "#b08900"
	default:
		return "#6b7280"
	}
}

// niceMax rounds up to 1, 2 or 5 times a power of ten.
func niceMax(v int64) int64 {
	if v <= 4 {
		return 4
	}
	p := math.Pow(10, math.Floor(math.Log10(float64(v))))
	for _, m := range []float64{1, 2, 5, 10} {
		if float64(v) <= m*p {
			return int64(m * p)
		}
	}
	return v
}

// LineChart renders a self-contained SVG timeline with release markers. Styling lives in attributes
// so the same markup exports to PNG.
func LineChart(title string, days []time.Time, series []Series, markers []Marker) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-label="%s" %s font-size="12">`,
		chartW, chartH, chartW, chartH, html.EscapeString(title), font)
	fmt.Fprintf(&b, `<title>%s</title><rect width="%d" height="%d" fill="#ffffff"/>`, html.EscapeString(title), chartW, chartH)
	fmt.Fprintf(&b, `<text x="%d" y="18" font-size="14" font-weight="600" fill="#1c2430">%s</text>`, padL, html.EscapeString(title))
	var top int64
	for _, s := range series {
		for _, v := range s.Values {
			top = max(top, v)
		}
	}
	top = niceMax(top)
	plotW, plotH := float64(chartW-padL-padR), float64(chartH-padT-pb)
	x := func(i int) float64 {
		if len(days) < 2 {
			return float64(padL) + plotW/2
		}
		return float64(padL) + plotW*float64(i)/float64(len(days)-1)
	}
	y := func(v int64) float64 { return float64(padT) + plotH*(1-float64(v)/float64(top)) }
	for i := range 5 {
		v := top * int64(i) / 4
		fmt.Fprintf(&b, `<line x1="%d" x2="%d" y1="%.1f" y2="%.1f" stroke="#e5e7eb"/><text x="%d" y="%.1f" text-anchor="end" fill="#5b6675">%d</text>`,
			padL, chartW-padR, y(v), y(v), padL-6, y(v)+4, v)
	}
	if len(days) > 0 {
		for _, i := range []int{0, len(days) / 2, len(days) - 1} {
			fmt.Fprintf(&b, `<text x="%.1f" y="%d" text-anchor="middle" fill="#5b6675">%s</text>`, x(i), chartH-10, days[i].Format("2 Jan"))
		}
	}
	drawMarkers(&b, days, markers, x)
	for _, s := range series {
		pts := make([]string, len(s.Values))
		for i, v := range s.Values {
			pts[i] = fmt.Sprintf("%.1f,%.1f", x(i), y(v))
		}
		fmt.Fprintf(&b, `<polyline fill="none" stroke="%s" stroke-width="2" points="%s"><title>%s</title></polyline>`, s.Color, strings.Join(pts, " "), html.EscapeString(s.Name))
	}
	lx := padL + 260
	for _, s := range series {
		fmt.Fprintf(&b, `<rect x="%d" y="9" width="10" height="10" fill="%s"/><text x="%d" y="18" fill="#1c2430">%s</text>`, lx, s.Color, lx+14, html.EscapeString(s.Name))
		lx += 24 + 7*len(s.Name)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

func drawMarkers(b *strings.Builder, days []time.Time, markers []Marker, x func(int) float64) {
	idx := map[string]int{}
	for i, d := range days {
		idx[d.Format(time.DateOnly)] = i
	}
	for _, m := range markers {
		i, ok := idx[m.Day.Format(time.DateOnly)]
		if !ok {
			continue
		}
		fmt.Fprintf(b, `<line x1="%.1f" x2="%.1f" y1="%d" y2="%d" stroke="#1c2430" stroke-dasharray="4 3"/><text x="%.1f" y="%d" fill="#1c2430" font-size="11">%s</text>`,
			x(i), x(i), padT-4, chartH-pb, x(i)+3, padT+8, html.EscapeString(m.Label))
	}
}

// BarChart renders horizontal bars with values.
func BarChart(title string, bars []Bar) string {
	rowH, labelW := 24, 150
	h := padT + rowH*max(len(bars), 1) + 10
	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-label="%s" %s font-size="12">`,
		chartW, h, chartW, h, html.EscapeString(title), font)
	fmt.Fprintf(&b, `<title>%s</title><rect width="%d" height="%d" fill="#ffffff"/>`, html.EscapeString(title), chartW, h)
	fmt.Fprintf(&b, `<text x="%d" y="18" font-size="14" font-weight="600" fill="#1c2430">%s</text>`, padL, html.EscapeString(title))
	var top int64 = 1
	for _, bar := range bars {
		top = max(top, bar.Value)
	}
	plotW := float64(chartW - labelW - padR - 60)
	for i, bar := range bars {
		yy := padT + i*rowH
		w := plotW * float64(bar.Value) / float64(top)
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="end" fill="#1c2430">%s</text><rect x="%d" y="%d" width="%.1f" height="%d" fill="%s"/><text x="%.1f" y="%d" fill="#5b6675">%d</text>`,
			labelW-8, yy+15, html.EscapeString(bar.Label), labelW, yy+4, w, rowH-8, bar.Color, float64(labelW)+w+6, yy+15, bar.Value)
	}
	b.WriteString(`</svg>`)
	return b.String()
}
