package stats

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// Palette of the approved dark design; charts carry it in attributes so exports keep it.
const (
	colBg     = "#0F1113"
	colPanel  = "#171A1D"
	colGrid   = "#23272B"
	colAxis   = "#3A4046"
	colInk    = "#ECEDEE"
	colMuted  = "#9BA1A6"
	colGreen  = "#4CC39B"
	colBlue   = "#5B8DEF"
	colOrange = "#F07A3E"
	colSand   = "#F6C08A"
	colGrey   = "#6B7178"
)

// KindColor gives every Visitor Kind its design colour.
func KindColor(k string) string {
	switch visits.Kind(k) {
	case visits.KindHuman:
		return colGreen
	case visits.KindHumanUnconfirmed:
		return "#2E7A64"
	case visits.KindSearchCrawler:
		return colBlue
	case visits.KindAICrawler:
		return colOrange
	case visits.KindAIFetcher:
		return colSand
	case visits.KindSEOTool:
		return "#A58BD6"
	case visits.KindOtherBot:
	}
	return colGrey
}

// KindDot is the Tailwind class of a Visitor Kind's legend swatch.
func KindDot(k string) string {
	switch visits.Kind(k) {
	case visits.KindHuman:
		return "bg-k-human"
	case visits.KindHumanUnconfirmed:
		return "bg-k-unconfirmed"
	case visits.KindSearchCrawler:
		return "bg-k-search"
	case visits.KindAICrawler:
		return "bg-k-aicrawler"
	case visits.KindAIFetcher:
		return "bg-k-aifetcher"
	case visits.KindSEOTool:
		return "bg-k-seo"
	case visits.KindOtherBot:
	}
	return "bg-k-other"
}

// KindLabel is the reporting name of a Visitor Kind.
func KindLabel(k string) string {
	switch visits.Kind(k) {
	case visits.KindHuman:
		return "Human"
	case visits.KindHumanUnconfirmed:
		return "Human (unconfirmed)"
	case visits.KindSearchCrawler:
		return "Search crawler"
	case visits.KindAICrawler:
		return "AI crawler"
	case visits.KindAIFetcher:
		return "AI fetcher"
	case visits.KindSEOTool:
		return "SEO tool"
	case visits.KindOtherBot:
		return "Other bot"
	}
	return k
}

func botKinds() []visits.Kind {
	return []visits.Kind{visits.KindAICrawler, visits.KindSearchCrawler, visits.KindAIFetcher, visits.KindSEOTool, visits.KindOtherBot}
}

// ChannelLabel is the reporting name of an Arrival Channel.
func ChannelLabel(c string) string {
	switch visits.Channel(c) {
	case visits.ChannelSearch:
		return "Search"
	case visits.ChannelAIChat:
		return "AI chat"
	case visits.ChannelSocial:
		return "Social"
	case visits.ChannelCampaign:
		return "Campaign"
	case visits.ChannelReferral:
		return "Referral"
	case visits.ChannelDirect:
		return "Direct"
	case visits.ChannelInternal:
		return "Internal"
	}
	return c
}

// ChannelColor highlights search and AI chat; every other channel is green.
func ChannelColor(c string) string {
	switch visits.Channel(c) {
	case visits.ChannelSearch:
		return colBlue
	case visits.ChannelAIChat:
		return colOrange
	case visits.ChannelSocial, visits.ChannelCampaign, visits.ChannelReferral, visits.ChannelDirect, visits.ChannelInternal:
	}
	return colGreen
}

func channelOrder() []visits.Channel {
	return []visits.Channel{
		visits.ChannelSearch, visits.ChannelAIChat, visits.ChannelDirect, visits.ChannelReferral, visits.ChannelSocial,
		visits.ChannelCampaign, visits.ChannelInternal,
	}
}

// ResourceLabel is the reporting name of a resource class.
func ResourceLabel(r string) string {
	switch visits.Resource(r) {
	case visits.ResourcePage:
		return "Pages (HTML)"
	case visits.ResourceMarkdown:
		return "Markdown twins"
	case visits.ResourceRobots:
		return "robots.txt"
	case visits.ResourceLLMs:
		return "llms.txt"
	case visits.ResourceLLMsFull:
		return "llms-full.txt"
	case visits.ResourceSitemap:
		return "sitemap.xml"
	case visits.ResourceImage:
		return "Images"
	case visits.ResourceRedirect:
		return "Redirects (301)"
	case visits.ResourceNotFound:
		return "Not found (404)"
	case visits.ResourceOutbound:
		return "Outbound Clicks"
	}
	return r
}

// num formats a count with thousands separators, as 1,284.
func num(n int64) string {
	s := strconv.FormatInt(n, 10)
	for i := len(s) - 3; i > 0 && s[i-1] != '-'; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

// pct is part of whole as a rounded percentage, "0%" for an empty whole.
func pct(part, whole int64) int {
	if whole <= 0 {
		return 0
	}
	return int(math.Round(float64(part) * 100 / float64(whole)))
}

// delta compares a figure with the previous period: "+38%", "−4%", "new" or "0%".
func delta(cur, prev float64) (text string, up bool) {
	switch {
	case prev == 0 && cur == 0:
		return "0%", true
	case prev == 0:
		return "new", true
	}
	d := math.Round((cur - prev) / prev * 100)
	switch {
	case d == 0:
		return "0%", cur >= prev
	case d < 0:
		return "−" + strconv.FormatFloat(-d, 'f', 0, 64) + "%", false
	}
	return "+" + strconv.FormatFloat(d, 'f', 0, 64) + "%", true
}

// minSec formats milliseconds as m:ss.
func minSec(ms int64) string {
	s := (ms + 500) / 1000
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

// ago is a short relative time, as "38 min ago" or "2 days ago".
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return strconv.Itoa(int(d/time.Minute)) + " min ago"
	case d < 24*time.Hour:
		return strconv.Itoa(int(d/time.Hour)) + " h ago"
	case d < 48*time.Hour:
		return "yesterday"
	}
	return strconv.Itoa(int(d/(24*time.Hour))) + " days ago"
}

// initials of a display name, at most two letters.
func initials(name string) string {
	var out []rune
	for _, w := range strings.Fields(name) {
		out = append(out, []rune(strings.ToUpper(w))[0])
		if len(out) == 2 {
			break
		}
	}
	if len(out) == 1 && len([]rune(name)) > 1 {
		out = []rune(strings.ToUpper(string([]rune(strings.TrimSpace(name))[:2])))
	}
	return string(out)
}

// shortURL drops the scheme and www. of an Outbound Click target.
func shortURL(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	return strings.TrimPrefix(u, "www.")
}

func itoa(n int) string { return strconv.Itoa(n) }
