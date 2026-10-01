package stats

import (
	"fmt"
	"html"
	"math"
	"strings"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// Plot space of the in-page charts; the SVGs stretch to their box (preserveAspectRatio none) and the
// axis labels are HTML, so text never distorts.
const plotW, plotH = 1000.0, 280.0

// Marker is a Content Release on a timeline.
type Marker struct {
	Day                  time.Time
	Label, Version, Note string
}

// Area is one band of a stacked chart.
type Area struct{ D, Color, Title string }

// Column is one day's column: a hover target on timelines, a bar on day charts.
type Column struct {
	X, W, Y, H float64
	Title      string
}

// Mark is a Content Release inside the plotted range.
type Mark struct {
	X                         float64
	Label, Version, Note, Day string
}

// Chip is one legend toggle of a timeline.
type Chip struct {
	Kind, Label, Total, Href, Dot string
	Off                           bool
}

// Timeline is a server-rendered stacked chart by Visitor Kind with HTML axes and legend chips.
type Timeline struct {
	Name, Title    string
	Sub            string
	Areas          []Area
	Columns        []Column
	Marks          []Mark
	YTicks, XTicks []string
	Chips          []Chip
	SVGHref, File  string
	Total          int64
}

// stackData is what a stacked chart is drawn from.
type stackData struct {
	days    []time.Time
	by      map[string][]int64
	kinds   []visits.Kind
	hidden  func(string) bool
	markers []Marker
}

func (d stackData) visible() []visits.Kind {
	var out []visits.Kind
	for _, k := range d.kinds {
		if d.hidden == nil || !d.hidden(string(k)) {
			out = append(out, k)
		}
	}
	return out
}

// niceTop rounds a maximum up to four even ticks of 1, 2, 2.5 or 5 times a power of ten.
func niceTop(v int64) int64 {
	for p := int64(1); ; p *= 10 {
		for _, m := range []int64{10, 20, 25, 50} {
			if m == 25 && p == 1 {
				continue
			}
			if step := m * p / 10; step*4 >= v {
				return step * 4
			}
		}
	}
}

// xs places n days across the plot; a single day spans the whole width.
func xs(n int) []float64 {
	if n == 1 {
		return []float64{0, plotW}
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = plotW * float64(i) / float64(n-1)
	}
	return out
}

func spread[T any](v []T) []T {
	if len(v) == 1 {
		return []T{v[0], v[0]}
	}
	return v
}

// geometry stacks the visible kinds and returns each band's top and bottom and the y maximum.
func (d stackData) geometry() (tops [][]int64, top int64) {
	sum := make([]int64, len(d.days))
	for _, k := range d.visible() {
		row := make([]int64, len(d.days))
		for i := range d.days {
			sum[i] += d.by[string(k)][i]
			row[i] = sum[i]
		}
		tops = append(tops, row)
	}
	var m int64
	for _, v := range sum {
		m = max(m, v)
	}
	return tops, niceTop(m)
}

func (d stackData) areas() ([]Area, int64) {
	tops, top := d.geometry()
	x := xs(len(d.days))
	y := func(v int64) float64 { return plotH - float64(v)/float64(top)*plotH }
	var out []Area
	for i, k := range d.visible() {
		upper := spread(tops[i])
		lower := make([]int64, len(upper))
		if i > 0 {
			lower = spread(tops[i-1])
		}
		var b strings.Builder
		for j := range upper {
			fmt.Fprintf(&b, "%s%.1f,%.1f ", map[bool]string{true: "M", false: "L"}[j == 0], x[j], y(upper[j]))
		}
		for j := len(lower) - 1; j >= 0; j-- {
			fmt.Fprintf(&b, "L%.1f,%.1f ", x[j], y(lower[j]))
		}
		b.WriteString("Z")
		var total int64
		for _, v := range d.by[string(k)] {
			total += v
		}
		out = append(out, Area{D: b.String(), Color: KindColor(string(k)), Title: KindLabel(string(k)) + ": " + num(total) + " Hits"})
	}
	return out, top
}

func (d stackData) columns() []Column {
	n := len(d.days)
	w := plotW / float64(max(n, 1))
	out := make([]Column, n)
	for i, day := range d.days {
		parts := []string{day.Format("Mon 2 Jan")}
		for _, k := range d.visible() {
			if v := d.by[string(k)][i]; v > 0 {
				parts = append(parts, KindLabel(string(k))+" "+num(v))
			}
		}
		out[i] = Column{X: float64(i) * w, W: w, H: plotH, Title: strings.Join(parts, " · ")}
	}
	return out
}

func (d stackData) marks() []Mark {
	idx := map[string]int{}
	for i, day := range d.days {
		idx[day.Format(time.DateOnly)] = i
	}
	x := xs(len(d.days))
	var out []Mark
	for _, m := range d.markers {
		if i, ok := idx[m.Day.Format(time.DateOnly)]; ok {
			out = append(out, Mark{X: x[i], Label: m.Label, Version: m.Version, Note: m.Note, Day: m.Day.Format("2 Jan")})
		}
	}
	return out
}

// tickDays picks at most five evenly spaced days for the x axis.
func tickDays(days []time.Time) []string {
	n := len(days)
	if n <= 5 {
		out := make([]string, n)
		for i, d := range days {
			out[i] = d.Format("2 Jan")
		}
		return out
	}
	out := make([]string, 5)
	for k := range 5 {
		out[k] = days[int(math.Round(float64(k*(n-1))/4))].Format("2 Jan")
	}
	return out
}

func yTicks(top int64) []string {
	return []string{num(top), num(top * 3 / 4), num(top / 2), num(top / 4), "0"}
}

func timelineText(name string) (title, sub string) {
	switch name {
	case "page":
		return "Hits per day on this page", "Split by Visitor Kind. Dashed lines mark Content Releases."
	case "releases":
		return "Hits per day with Content Releases", "Every Hit except images, split by Visitor Kind. Dashed lines mark when each Content Release went live."
	case "agents":
		return "Machine Hits per day", "Crawlers and agents by Visitor Kind. Dashed lines mark Content Releases."
	}
	return "Who reads the mirror, per day", "Every request except images, split by Visitor Kind. Dashed lines mark Content Releases."
}

// timeline builds the in-page chart; f supplies the chip links.
func timeline(name string, d stackData, f Filter) Timeline {
	areas, top := d.areas()
	title, sub := timelineText(name)
	t := Timeline{
		Name: name, Title: title, Sub: sub, Areas: areas, Columns: d.columns(), Marks: d.marks(), YTicks: yTicks(top), XTicks: tickDays(d.days),
		SVGHref: "/stats/chart/" + name + ".svg?" + f.Query(),
		File:    name + "-" + f.From.Format(time.DateOnly) + "-" + f.To.Format(time.DateOnly),
	}
	for _, k := range d.kinds {
		var total int64
		for _, v := range d.by[string(k)] {
			total += v
		}
		t.Total += total
		t.Chips = append(t.Chips, Chip{
			Kind: string(k), Label: KindLabel(string(k)), Total: num(total), Dot: KindDot(string(k)),
			Off: f.Hidden(string(k)), Href: "?" + f.Query("hide", f.ToggleHide(string(k))),
		})
	}
	return t
}

// Spark is a sparkline's line and filled area in a w by h box.
type Spark struct{ Line, Area string }

func spark(values []int64, w, h float64) Spark {
	values = spread(values)
	if len(values) == 0 {
		values = []int64{0, 0}
	}
	var top int64 = 1
	for _, v := range values {
		top = max(top, v)
	}
	step := w / float64(len(values)-1)
	pts := make([]string, len(values))
	for i, v := range values {
		pts[i] = fmt.Sprintf("%.1f,%.1f", float64(i)*step, h-2-float64(v)/float64(top)*(h-6))
	}
	line := "M" + strings.Join(pts, " L")
	return Spark{Line: line, Area: fmt.Sprintf("M0,%.0f L%s L%.0f,%.0f Z", h, strings.Join(pts, " L"), w, h)}
}

// Seg is one coloured part of a stacked bar in a 1000-wide box.
type Seg struct {
	X, W         float64
	Color, Title string
}

type part struct {
	label, color string
	n            int64
}

// segments lays parts side by side; scale shrinks the whole bar (1 is full width).
func segments(parts []part, scale float64) []Seg {
	var total int64
	for _, p := range parts {
		total += p.n
	}
	if total == 0 {
		return nil
	}
	var out []Seg
	x := 0.0
	for _, p := range parts {
		if p.n == 0 {
			continue
		}
		w := plotW * scale * float64(p.n) / float64(total)
		out = append(out, Seg{X: x, W: max(w-2, 1), Color: p.color, Title: fmt.Sprintf("%s: %s (%d%%)", p.label, num(p.n), pct(p.n, total))})
		x += w
	}
	return out
}

// svgOpen starts a standalone export SVG on the dark panel colour.
func svgOpen(b *strings.Builder, w, h int, title string) {
	fmt.Fprintf(b, `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" width="%d" height="%d" role="img" aria-label="%s" font-family="IBM Plex Sans, system-ui, sans-serif" font-size="12">`,
		w, h, w, h, html.EscapeString(title))
	fmt.Fprintf(b, `<title>%s</title><rect width="%d" height="%d" fill="%s"/>`, html.EscapeString(title), w, h, colPanel)
	fmt.Fprintf(b, `<text x="24" y="30" font-size="16" font-weight="600" fill="%s">%s</text>`, colInk, html.EscapeString(title))
}

// TimelineSVG renders a stacked timeline as a self-contained SVG for download and PNG export.
func TimelineSVG(title string, d stackData) string {
	const left, top0, w, h = 64.0, 92.0, 1088, 420
	var b strings.Builder
	svgOpen(&b, w, h, title)
	lx := 24
	for _, k := range d.visible() {
		fmt.Fprintf(&b, `<rect x="%d" y="48" width="10" height="10" rx="2" fill="%s"/><text x="%d" y="57" fill="%s">%s</text>`,
			lx, KindColor(string(k)), lx+16, colInk, html.EscapeString(KindLabel(string(k))))
		lx += 30 + 7*len(KindLabel(string(k)))
	}
	areas, top := d.areas()
	ticks := yTicks(top)
	for i, t := range ticks {
		y := top0 + plotH*float64(i)/4
		stroke := colGrid
		if i == 4 {
			stroke = colAxis
		}
		fmt.Fprintf(&b, `<line x1="%.0f" x2="%.0f" y1="%.1f" y2="%.1f" stroke="%s"/><text x="%.0f" y="%.1f" text-anchor="end" fill="%s" font-family="IBM Plex Mono, monospace" font-size="11">%s</text>`,
			left, left+plotW, y, y, stroke, left-8, y+4, colMuted, t)
	}
	fmt.Fprintf(&b, `<g transform="translate(%.0f %.0f)">`, left, top0)
	for _, a := range areas {
		fmt.Fprintf(&b, `<path d="%s" fill="%s" fill-opacity="0.9"><title>%s</title></path>`, a.D, a.Color, html.EscapeString(a.Title))
	}
	for _, m := range d.marks() {
		anchor, dx := "start", 4.0
		if m.X > plotW/2 {
			anchor, dx = "end", -4
		}
		fmt.Fprintf(&b, `<line x1="%.1f" x2="%.1f" y1="0" y2="%.0f" stroke="%s" stroke-dasharray="4 4"/><text x="%.1f" y="12" text-anchor="%s" fill="%s" font-family="IBM Plex Mono, monospace" font-size="11">%s</text>`,
			m.X, m.X, plotH, colInk, m.X+dx, anchor, colInk, html.EscapeString(m.Label))
	}
	b.WriteString(`</g>`)
	labels := tickDays(d.days)
	for i, l := range labels {
		x := left
		if len(labels) > 1 {
			x = left + plotW*float64(i)/float64(len(labels)-1)
		}
		anchor := "middle"
		switch {
		case i == 0:
			anchor = "start"
		case i == len(labels)-1:
			anchor = "end"
		}
		fmt.Fprintf(&b, `<text x="%.1f" y="%.0f" text-anchor="%s" fill="%s" font-family="IBM Plex Mono, monospace" font-size="11">%s</text>`,
			x, top0+plotH+22, anchor, colMuted, l)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// BarRow is one horizontal bar of a ranked list.
type BarRow struct {
	Key, Label, Value, Share, Color string
	N                               int64
	W                               float64 // percentage of the longest bar
}

func barRows(rows []BarRow) []BarRow {
	var top, total int64
	for _, r := range rows {
		top = max(top, r.N)
		total += r.N
	}
	for i := range rows {
		rows[i].Value = num(rows[i].N)
		rows[i].Share = fmt.Sprintf("%d%%", pct(rows[i].N, total))
		if top > 0 {
			rows[i].W = float64(rows[i].N) * 100 / float64(top)
		}
	}
	return rows
}

// BarsSVG renders a ranked list of bars as a self-contained SVG.
func BarsSVG(title string, rows []BarRow) string {
	const rowH, labelW, w = 30, 160, 1088
	h := 64 + rowH*max(len(rows), 1) + 16
	var b strings.Builder
	svgOpen(&b, w, h, title)
	plot := float64(w - labelW - 120)
	for i, r := range rows {
		y := 56 + i*rowH
		fmt.Fprintf(&b, `<text x="%d" y="%d" text-anchor="end" fill="%s">%s</text><rect x="%d" y="%d" width="%.1f" height="10" rx="5" fill="#262B30"/><rect x="%d" y="%d" width="%.1f" height="10" rx="5" fill="%s"/><text x="%.1f" y="%d" fill="%s" font-family="IBM Plex Mono, monospace" font-size="11">%s · %s</text>`,
			labelW-12, y+14, colInk, html.EscapeString(r.Label), labelW, y+5, plot, labelW, y+5, plot*r.W/100, r.Color,
			float64(labelW)+plot+12, y+14, colMuted, r.Value, r.Share)
	}
	b.WriteString(`</svg>`)
	return b.String()
}

// DaySeries is one daily figure drawn as columns.
type DaySeries struct {
	Label, Color string
	Values       []int64
}

// Bars is a per-day column chart in the plot space.
type Bars struct {
	Label, Total string
	Color        string
	Cols         []Column
	YTicks       []string
}

func dayBars(days []time.Time, s DaySeries, h float64) Bars {
	var top, total int64
	for _, v := range s.Values {
		top = max(top, v)
		total += v
	}
	top = niceTop(top)
	w := plotW / float64(max(len(days), 1))
	out := Bars{Label: s.Label, Total: num(total), Color: s.Color, YTicks: yTicks(top)}
	for i, d := range days {
		v := s.Values[i]
		bh := h * float64(v) / float64(top)
		out.Cols = append(out.Cols, Column{
			X: float64(i)*w + w*0.15, W: max(w*0.7, 1), Y: h - bh, H: bh,
			Title: d.Format("Mon 2 Jan") + ": " + num(v) + " " + strings.ToLower(s.Label),
		})
	}
	return out
}

// DaySVG renders daily series as stacked panels of columns, for download and PNG export.
func DaySVG(title string, days []time.Time, series []DaySeries) string {
	const left, w, panelH = 64.0, 1088, 150.0
	h := int(70 + (panelH+50)*float64(len(series)))
	var b strings.Builder
	svgOpen(&b, w, h, title)
	cw := plotW / float64(max(len(days), 1))
	for p, s := range series {
		y0 := 70 + float64(p)*(panelH+50)
		var top int64
		for _, v := range s.Values {
			top = max(top, v)
		}
		top = niceTop(top)
		fmt.Fprintf(&b, `<text x="%.0f" y="%.0f" fill="%s" font-weight="600">%s</text>`, left, y0, colInk, html.EscapeString(s.Label))
		fmt.Fprintf(&b, `<line x1="%.0f" x2="%.0f" y1="%.0f" y2="%.0f" stroke="%s"/><text x="%.0f" y="%.0f" text-anchor="end" fill="%s" font-size="11">%s</text>`,
			left, left+plotW, y0+12+panelH, y0+12+panelH, colAxis, left-8, y0+16, colMuted, num(top))
		for i, v := range s.Values {
			bh := panelH * float64(v) / float64(top)
			fmt.Fprintf(&b, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"><title>%s: %s</title></rect>`,
				left+float64(i)*cw+cw*0.15, y0+12+panelH-bh, max(cw*0.7, 1), bh, s.Color, days[i].Format("2 Jan"), num(v))
		}
	}
	b.WriteString(`</svg>`)
	return b.String()
}
