package content

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode"

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

func html(r *Resource) string { return string(r.Head) + "HIT" + string(r.Tail) }

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
	case strings.HasPrefix(href, "/go?"), target == "/stats":
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
