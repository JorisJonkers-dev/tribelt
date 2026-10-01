package content

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

	"github.com/yuin/goldmark/ast"

	"github.com/JorisJonkers-dev/tribelt/web"
)

// These gates run against the real content/ and the tribelt.nl crawl.

const (
	realContent = "../../content"
	crawlDir    = "../../crawl"
	// VerbatimRun is the longest run of consecutive words a Mirror Page may share with tribelt.nl.
	VerbatimRun = 25
)

type crawlPage struct {
	Code  int      `json:"code"`
	Title string   `json:"title"`
	Text  []string `json:"text"`
}

func loadCrawl(t *testing.T) map[string]crawlPage {
	t.Helper()
	raw, err := os.ReadFile(crawlDir + "/pages.json")
	if err != nil {
		t.Fatal(err)
	}
	var pages map[string]crawlPage
	if err := json.Unmarshal(raw, &pages); err != nil {
		t.Fatal(err)
	}
	return pages
}

var (
	realOnce    sync.Once
	realC       *Content
	realB       *Built
	errRealLoad error
)

func loadReal(t *testing.T) (*Content, *Built) {
	t.Helper()
	realOnce.Do(func() {
		realC, errRealLoad = Load(os.DirFS(realContent))
		if errRealLoad != nil {
			return
		}
		tmpl, _ := fs.Sub(web.Templates, "templates")
		static, _ := fs.Sub(web.Static, "static")
		realB, errRealLoad = Build(realC, Options{Templates: tmpl, Static: static, Images: os.DirFS(realContent + "/images"), Now: time.Now()})
	})
	if errRealLoad != nil {
		t.Fatalf("real content does not load:\n%v", errRealLoad)
	}
	return realC, realB
}

func missingRoutes(c *Content, routes map[string]crawlPage) []string {
	var missing []string
	for route := range routes {
		if _, ok := c.Resolve(route); !ok {
			missing = append(missing, route)
		}
	}
	sort.Strings(missing)
	return missing
}

// TestRouteManifest: every official route must exist as a Mirror Page or a redirect to one.
func TestRouteManifest(t *testing.T) {
	c, _ := loadReal(t)
	routes := loadCrawl(t)
	if missing := missingRoutes(c, routes); len(missing) > 0 {
		t.Fatalf("%d of %d official routes have no Mirror Page or redirect:\n  %s", len(missing), len(routes), strings.Join(missing, "\n  "))
	}
}

var (
	h1Tag     = regexp.MustCompile(`<h1[\s>]`)
	imgTag    = regexp.MustCompile(`<img\b[^>]*>`)
	altAttr   = regexp.MustCompile(`\balt="([^"]*)"`)
	ldScript  = regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`)
	canonTag  = regexp.MustCompile(`<link rel="canonical" href="([^"]+)">`)
	hreflang  = regexp.MustCompile(`<link rel="alternate" hreflang="([^"]+)" href="([^"]+)">`)
	hrefAttr  = regexp.MustCompile(`\bhref="(/[^"]*)"`)
	titleTag  = regexp.MustCompile(`<title>([^<]*)</title>`)
	robotsTag = regexp.MustCompile(`<meta name="robots" content="noindex`)
)

// html is a page as a signed-out visitor receives it, with "HIT" as the Hit id.
func html(r *Resource) string { return string(bytes.Join(r.Parts("HIT", false, true), nil)) }

func ldTypes(t *testing.T, path, doc string) []string {
	t.Helper()
	m := ldScript.FindStringSubmatch(doc)
	if m == nil {
		t.Errorf("%s: no JSON-LD", path)
		return nil
	}
	var ld struct {
		Graph []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal([]byte(m[1]), &ld); err != nil {
		t.Errorf("%s: JSON-LD does not parse: %v", path, err)
		return nil
	}
	var types []string
	for _, n := range ld.Graph {
		switch v := n["@type"].(type) {
		case string:
			types = append(types, v)
		case []any:
			for _, x := range v {
				types = append(types, x.(string))
			}
		}
	}
	return types
}

// TestSEOLint checks every rendered Mirror Page.
func TestSEOLint(t *testing.T) {
	c, b := loadReal(t)
	crawl := loadCrawl(t)
	for _, p := range c.Pages {
		doc := html(b.HTML[p.Path])
		if n := len(h1Tag.FindAllString(doc, -1)); n != 1 {
			t.Errorf("%s: %d <h1> elements, want 1", p.Path, n)
		}
		for _, img := range imgTag.FindAllString(doc, -1) {
			if m := altAttr.FindStringSubmatch(img); m == nil || strings.TrimSpace(m[1]) == "" {
				t.Errorf("%s: image without alt text: %s", p.Path, img)
			}
		}
		if m := canonTag.FindStringSubmatch(doc); m == nil || m[1] != c.Site.BaseURL+p.Path {
			t.Errorf("%s: canonical %v, want self", p.Path, m)
		}
		if m := titleTag.FindStringSubmatch(doc); m == nil || m[1] == "" {
			t.Errorf("%s: no <title>", p.Path)
		}
		if official, ok := crawl[p.Path]; ok && strings.EqualFold(strings.TrimSpace(official.Title), strings.TrimSpace(p.Title)) {
			t.Errorf("%s: title equals the official title (ADR-0001)", p.Path)
		}
		if p.Noindex != robotsTag.MatchString(doc) {
			t.Errorf("%s: robots meta does not match noindex=%v", p.Path, p.Noindex)
		}
		types := ldTypes(t, p.Path, doc)
		for _, want := range []string{"Organization", "WebSite", "WebPage", "BreadcrumbList"} {
			if !slices.Contains(types, want) {
				t.Errorf("%s: JSON-LD lacks %s", p.Path, want)
			}
		}
		if p.Product != nil && !slices.Contains(types, "Product") {
			t.Errorf("%s: product page without Product JSON-LD", p.Path)
		}
		if len(p.FAQ) > 0 && !slices.Contains(types, "FAQPage") {
			t.Errorf("%s: FAQ without FAQPage JSON-LD", p.Path)
		}
		for _, m := range hrefAttr.FindAllStringSubmatch(doc, -1) {
			checkInternalHref(t, c, b, p.Path, m[1])
		}
	}
}

func checkInternalHref(t *testing.T, c *Content, b *Built, from, href string) {
	t.Helper()
	target, _, _ := strings.Cut(href, "#")
	target, _, _ = strings.Cut(target, "?")
	switch {
	case strings.HasPrefix(href, "/go?"), target == "/stats", target == "/auth/login":
		return
	case b.Files[target] != nil:
		return
	case strings.HasPrefix(target, "/static/"), strings.HasPrefix(target, "/images/"):
		if b.Assets[target] == nil {
			t.Errorf("%s: asset %s missing", from, href)
		}
		return
	}
	if base, ok := strings.CutSuffix(target, ".md"); ok {
		if base == "/index" {
			base = "/"
		}
		if b.Markdown[base] == nil {
			t.Errorf("%s: twin %s missing", from, href)
		}
		return
	}
	if _, ok := c.Resolve(target); !ok {
		t.Errorf("%s: link %s does not resolve", from, href)
	}
}

type smURL struct {
	Loc   string `xml:"loc"`
	Links []struct {
		Hreflang string `xml:"hreflang,attr"`
		Href     string `xml:"href,attr"`
	} `xml:"link"`
}

// TestSitemapHreflangConsistency: sitemap holds exactly the indexable pages, alternates are reciprocal
// and match each page's <link rel=alternate> tags.
func TestSitemapHreflangConsistency(t *testing.T) {
	c, b := loadReal(t)
	var set struct {
		URLs []smURL `xml:"url"`
	}
	if err := xml.Unmarshal(b.Files["/sitemap.xml"].Body, &set); err != nil {
		t.Fatal(err)
	}
	alts := map[string]map[string]string{}
	for _, u := range set.URLs {
		m := map[string]string{}
		for _, l := range u.Links {
			m[l.Hreflang] = l.Href
		}
		alts[u.Loc] = m
	}
	for _, p := range c.Pages {
		loc := c.Site.BaseURL + p.Path
		sm, inSitemap := alts[loc]
		if inSitemap == p.Noindex {
			t.Errorf("%s: in sitemap=%v but noindex=%v", p.Path, inSitemap, p.Noindex)
			continue
		}
		if p.Noindex {
			continue
		}
		page := map[string]string{}
		for _, m := range hreflang.FindAllStringSubmatch(html(b.HTML[p.Path]), -1) {
			page[m[1]] = m[2]
		}
		if len(page) != len(sm) {
			t.Errorf("%s: page hreflang %v != sitemap %v", p.Path, page, sm)
		}
		for lang, href := range sm {
			if page[lang] != href {
				t.Errorf("%s: hreflang %s page=%q sitemap=%q", p.Path, lang, page[lang], href)
			}
			if back := alts[href]; len(back) > 0 && back[langOf(c, p)] != loc && lang != "x-default" {
				t.Errorf("%s: alternate %s does not link back", p.Path, href)
			}
		}
		if len(sm) > 0 && sm["x-default"] == "" && c.InLocale(p.ID, "nl") != nil {
			t.Errorf("%s: group with a Dutch page lacks x-default", p.Path)
		}
	}
}

func langOf(c *Content, p *Page) string { return c.Site.Locales[p.Locale].Hreflang }

func words(s string) []string {
	return strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// verbatimFindings reports every page passage sharing n consecutive words with an official paragraph.
func verbatimFindings(pages []*Page, official []string, n int) []string {
	shingles := map[string]string{}
	for _, para := range official {
		w := words(para)
		for i := 0; i+n <= len(w); i++ {
			shingles[strings.Join(w[i:i+n], " ")] = para
		}
	}
	var out []string
	for _, p := range pages {
		text := []string{p.Description, PlainBody(p.Body)}
		for _, f := range p.FAQ {
			text = append(text, f.Q+" "+PlainBody(f.A))
		}
		for _, block := range text {
			for _, para := range strings.Split(block, "\n") {
				w := words(para)
				for i := 0; i+n <= len(w); i++ {
					if src, ok := shingles[strings.Join(w[i:i+n], " ")]; ok {
						out = append(out, fmt.Sprintf("%s: %d+ words copied verbatim: %q (official: %.120q)", p.File, n, strings.Join(w[i:i+n], " "), src))
						break
					}
				}
			}
		}
	}
	return out
}

func officialTexts(t *testing.T) []string {
	t.Helper()
	var official []string
	for _, p := range loadCrawl(t) {
		official = append(official, p.Text...)
	}
	raw, err := os.ReadFile(crawlDir + "/texts.json")
	if err != nil {
		t.Fatal(err)
	}
	var texts map[string][]string
	if err := json.Unmarshal(raw, &texts); err != nil {
		t.Fatal(err)
	}
	for _, ps := range texts {
		official = append(official, ps...)
	}
	return official
}

// TestNoVerbatimCopy: no Mirror Page shares a run of VerbatimRun consecutive words with any crawled
// official paragraph.
func TestNoVerbatimCopy(t *testing.T) {
	c, _ := loadReal(t)
	for _, f := range verbatimFindings(c.Pages, officialTexts(t), VerbatimRun) {
		t.Error(f)
	}
}

var testBar = regexp.MustCompile(`(?s)<aside class="testbar" aria-label="[^"]+">.*?<a href="(/go\?[^"]+)" rel="nofollow">`)

// TestTestBarOnEveryPage: every page and 404 says it is a test site and links to its Official Page.
func TestTestBarOnEveryPage(t *testing.T) {
	c, b := loadReal(t)
	check := func(name, doc, want string) {
		t.Helper()
		m := testBar.FindStringSubmatch(doc)
		if m == nil {
			t.Errorf("%s: no test-site bar with an official link", name)
			return
		}
		if to := strings.ReplaceAll(m[1], "&amp;", "&"); !strings.Contains(to, "to="+url.QueryEscape(want)) {
			t.Errorf("%s: bar links to %s, want %s", name, to, want)
		}
	}
	for _, p := range c.Pages {
		check(p.Path, html(b.HTML[p.Path]), p.Official)
	}
	for l, res := range b.NotFound {
		check("404 "+l, html(res), c.Site.Official+c.Home(l).Path)
	}
}

var (
	noticePill   = regexp.MustCompile(`(?s)<aside class="notice" aria-label="[^"]+">\s*<p>[^<]+<a href="(/go\?[^"]+)" rel="nofollow">[^<]+</a></p>\s*<button class="notice-close" type="button" data-notice-close aria-label="([^"]+)">`)
	noticeScript = regexp.MustCompile(`(?s)<head>.*<script src="(/static/notice\.js\?v=[0-9a-f]+)"></script>.*</head>`)
)

// TestNoticeOnEveryPage: every page and 404 carries the dismissible pill, linking to its Official Page,
// and loads the script that shows it from <head>, before the body paints.
func TestNoticeOnEveryPage(t *testing.T) {
	c, b := loadReal(t)
	check := func(name, doc, want string) {
		t.Helper()
		m := noticePill.FindStringSubmatch(doc)
		if m == nil {
			t.Errorf("%s: no notice pill with an official link and a labelled close button", name)
			return
		}
		if to := strings.ReplaceAll(m[1], "&amp;", "&"); !strings.Contains(to, "to="+url.QueryEscape(want)) {
			t.Errorf("%s: pill links to %s, want %s", name, to, want)
		}
		s := noticeScript.FindStringSubmatch(doc)
		if s == nil || b.Assets[strings.Split(s[1], "?")[0]] == nil {
			t.Errorf("%s: notice script missing from <head>", name)
		}
	}
	for _, p := range c.Pages {
		check(p.Path, html(b.HTML[p.Path]), p.Official)
	}
	for l, res := range b.NotFound {
		check("404 "+l, html(res), c.Site.Official+c.Home(l).Path)
	}
	labels := map[string]bool{}
	for _, l := range LocaleOrder() {
		if m := noticePill.FindStringSubmatch(html(b.NotFound[l])); m != nil {
			labels[m[2]] = true
		}
	}
	if len(labels) != len(LocaleOrder()) {
		t.Errorf("close button label is not localized: %v", labels)
	}
}

// The gates above must be able to fail: prove it on the fixture.
func TestGatesCanFail(t *testing.T) {
	c := loadFixture(t)
	if missing := missingRoutes(c, loadCrawl(t)); len(missing) < 100 {
		t.Fatalf("fixture should miss most official routes, missed %d", len(missing))
	}
	copied := &Page{File: "x.md", Body: "Intro.\n\nWij zeggen: " + strings.Repeat("een twee drie vier vijf ", 5) + "klaar."}
	official := []string{"Hier staat " + strings.Repeat("een twee drie vier vijf ", 5)}
	if got := verbatimFindings([]*Page{copied}, official, VerbatimRun); len(got) != 1 {
		t.Fatalf("25-word copy must be found: %v", got)
	}
	short := &Page{File: "y.md", Body: strings.Repeat("een twee drie vier vijf ", 4) + "zes"}
	if got := verbatimFindings([]*Page{short}, official, VerbatimRun); len(got) != 0 {
		t.Fatalf("24 shared words are allowed: %v", got)
	}
}

var tags = regexp.MustCompile(`<[^>]*>`)

// TestLayoutKeepsText: cards, bands and tabs only regroup a body; every word stays, in order.
func TestLayoutKeepsText(t *testing.T) {
	c, b := loadReal(t)
	static, _ := fs.Sub(web.Static, "static")
	bl := &builder{c: c, opts: Options{Static: static, Images: os.DirFS(realContent + "/images")}, assets: map[string]*Asset{}, images: map[string]imageInfo{}}
	if err := bl.loadAssets(); err != nil {
		t.Fatal(err)
	}
	md := markdown()
	for _, p := range c.Pages {
		plain, err := renderMarkdown(md, p.Body, p.Path)
		if err != nil {
			t.Fatal(err)
		}
		shaped, err := renderWith(md, p.Body, p.Path, func(doc ast.Node, src []byte) { bl.layout(doc, src, p) })
		if err != nil {
			t.Fatal(err)
		}
		if a, z := strings.Join(words(tags.ReplaceAllString(plain, " ")), " "), strings.Join(words(tags.ReplaceAllString(shaped, " ")), " "); a != z {
			t.Errorf("%s: layout changed the text", p.Path)
		}
	}
	for path, want := range map[string]string{
		"/transportbanden/draadogenbanden": `<div class="tabs"><section class="tab"><h2 id="specificaties"><a href="#specificaties">Specificaties</a></h2>`,
		"/metalen-transportbanden":         `<div class="cards cards-product"><article class="card">`,
		"/sectoren":                        `<div class="cards cards-sector"><article class="card">`,
		"/":                                `<ul class="cards cards-product">`,
		"/en/knowledge-center":             `<ul class="cards cards-post">`,
	} {
		if doc := html(b.HTML[path]); !strings.Contains(doc, want) {
			t.Errorf("%s lacks %s", path, want)
		}
	}
}
