package content

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"html/template"
	"regexp"
	"slices"
	"strings"
	"time"
)

type obj = map[string]any

func (b *builder) orgLD() obj {
	o := b.c.Site.Org
	org := obj{
		"@type": "Organization", "@id": b.abs("/#organization"), "name": o.Name, "legalName": o.LegalName, "url": o.URL,
		"sameAs": append([]string{b.c.Site.Official}, o.SameAs...),
	}
	if o.Logo != "" {
		org["logo"] = b.abs("/" + o.Logo)
	}
	if o.Address.Street != "" {
		org["address"] = obj{
			"@type": "PostalAddress", "streetAddress": o.Address.Street, "postalCode": o.Address.PostalCode,
			"addressLocality": o.Address.Locality, "addressCountry": o.Address.Country,
		}
	}
	for k, v := range map[string]string{"telephone": o.Telephone, "email": o.Email, "foundingDate": o.FoundingDate} {
		if v != "" {
			org[k] = v
		}
	}
	if o.ParentOrganization != "" {
		org["parentOrganization"] = obj{"@type": "Organization", "name": o.ParentOrganization}
	}
	return org
}

func (b *builder) jsonLD(p *Page, crumbs []crumb) (template.JS, error) {
	lang := b.c.Site.Locales[p.Locale].Hreflang
	url := b.abs(p.Path)
	website := obj{
		"@type": "WebSite", "@id": b.abs("/#website"), "url": b.abs(b.c.Home(p.Locale).Path), "name": b.c.Site.Org.Name,
		"inLanguage": lang, "publisher": obj{"@id": b.abs("/#organization")},
	}
	page := obj{
		"@type": "WebPage", "@id": url + "#webpage", "url": url, "name": p.Title, "description": p.Description, "inLanguage": lang,
		"isPartOf": obj{"@id": b.abs("/#website")}, "about": obj{"@id": b.abs("/#organization")},
		"isBasedOn": p.Official, "breadcrumb": obj{"@id": url + "#breadcrumb"},
	}
	if len(p.FAQ) > 0 {
		page["@type"] = []string{"WebPage", "FAQPage"}
		qs := make([]obj, 0, len(p.FAQ))
		for _, f := range p.FAQ {
			qs = append(qs, obj{"@type": "Question", "name": f.Q, "acceptedAnswer": obj{"@type": "Answer", "text": PlainBody(f.A)}})
		}
		page["mainEntity"] = qs
	}
	if p.Image != nil {
		page["primaryImageOfPage"] = obj{"@type": "ImageObject", "url": b.imageURL(p.Image), "caption": p.Image.Alt}
	}
	graph := []obj{b.orgLD(), website, page, b.breadcrumbLD(p, url, crumbs)}
	if p.Product != nil {
		graph = append(graph, b.productLD(p, url))
	}
	raw, err := json.Marshal(obj{"@context": "https://schema.org", "@graph": graph})
	if err != nil {
		return "", err
	}
	return template.JS(raw), nil //nolint:gosec // json.Marshal escapes <, > and & for script context
}

func (b *builder) imageURL(img *Image) string {
	return b.abs("/images/" + strings.TrimPrefix(img.Src, "images/"))
}

func (b *builder) breadcrumbLD(p *Page, url string, crumbs []crumb) obj {
	if len(crumbs) == 0 {
		crumbs = []crumb{{Name: p.H1}}
	}
	items := make([]obj, 0, len(crumbs))
	for i, c := range crumbs {
		item := obj{"@type": "ListItem", "position": i + 1, "name": c.Name, "item": url}
		if c.Href != "" {
			item["item"] = b.abs(c.Href)
		}
		items = append(items, item)
	}
	return obj{"@type": "BreadcrumbList", "@id": url + "#breadcrumb", "itemListElement": items}
}

func (b *builder) productLD(p *Page, url string) obj {
	prod := obj{
		"@type": "Product", "@id": url + "#product", "name": p.Product.Name, "description": p.Description, "url": url,
		"brand": obj{"@type": "Brand", "name": b.c.Site.Org.Name}, "manufacturer": obj{"@id": b.abs("/#organization")},
	}
	if p.Product.Category != "" {
		prod["category"] = p.Product.Category
	}
	if len(p.Product.Materials) > 0 {
		prod["material"] = strings.Join(p.Product.Materials, ", ")
	}
	if p.Image != nil {
		prod["image"] = b.imageURL(p.Image)
	}
	props := make([]obj, 0, len(p.Product.Specs))
	for _, s := range p.Product.Specs {
		prop := obj{"@type": "PropertyValue", "name": s.Name, "value": s.Value}
		if s.Unit != "" {
			prop["unitText"] = s.Unit
		}
		props = append(props, prop)
	}
	if len(props) > 0 {
		prod["additionalProperty"] = props
	}
	return prod
}

var relLink = regexp.MustCompile(`\]\((/[^)\s]*)\)`)

// markdownTwin is the page as Markdown for agents: front matter facts, body, specs and FAQ.
func (b *builder) markdownTwin(p *Page) string {
	ui := uiStrings(p.Locale)
	var s strings.Builder
	fmt.Fprintf(&s, "# %s\n\n> %s\n\n", p.H1, p.Description)
	fmt.Fprintf(&s, "- URL: %s\n- %s: %s\n- %s\n\n", b.abs(p.Path), ui.Source, p.Official, b.c.Site.Locales[p.Locale].Footer.Note)
	s.WriteString(relLink.ReplaceAllString(p.Body, "]("+b.c.Site.BaseURL+"$1)"))
	s.WriteString("\n")
	if p.Product != nil && (len(p.Product.Specs) > 0 || len(p.Product.Materials) > 0) {
		fmt.Fprintf(&s, "\n## %s\n\n| %s | %s |\n| --- | --- |\n", ui.Specs, ui.Property, ui.Value)
		for _, sp := range p.Product.Specs {
			fmt.Fprintf(&s, "| %s | %s |\n", sp.Name, strings.TrimSpace(sp.Value+" "+sp.Unit))
		}
		if len(p.Product.Materials) > 0 {
			fmt.Fprintf(&s, "| %s | %s |\n", ui.Materials, strings.Join(p.Product.Materials, ", "))
		}
	}
	if len(p.FAQ) > 0 {
		fmt.Fprintf(&s, "\n## %s\n", ui.FAQ)
		for _, f := range p.FAQ {
			fmt.Fprintf(&s, "\n### %s\n\n%s\n", f.Q, f.A)
		}
	}
	if p.CTA != nil {
		fmt.Fprintf(&s, "\n[%s](%s)\n", p.CTA.Label, p.CTA.Href)
	}
	return s.String()
}

// RobotsAgents are the crawlers robots.txt names explicitly; all are allowed.
func RobotsAgents() []string {
	return []string{
		"Googlebot", "Google-Extended", "Bingbot", "Applebot", "Applebot-Extended", "DuckDuckBot", "GPTBot", "OAI-SearchBot",
		"ChatGPT-User", "ClaudeBot", "Claude-User", "Claude-SearchBot", "PerplexityBot", "Perplexity-User", "CCBot",
		"Amazonbot", "meta-externalagent", "Bytespider", "cohere-ai", "MistralAI-User", "DuckAssistBot", "YandexBot",
	}
}

func disallowed() []string { return []string{"/stats", "/auth", "/go", "/b"} }

func (b *builder) robots() string {
	var s strings.Builder
	s.WriteString("# Student test site mirroring tribelt.nl for a university digital strategy project.\n")
	s.WriteString("# Every crawler is welcome, including AI crawlers and assistants. Markdown twins: <page>.md, see /llms.txt.\n\n")
	for _, ua := range append(RobotsAgents(), "*") {
		fmt.Fprintf(&s, "User-agent: %s\nAllow: /\n", ua)
		for _, d := range disallowed() {
			fmt.Fprintf(&s, "Disallow: %s\n", d)
		}
		s.WriteString("\n")
	}
	fmt.Fprintf(&s, "Sitemap: %s\n", b.abs("/sitemap.xml"))
	return s.String()
}

func llmsSection(t string) (int, string) {
	switch t {
	case "home", "about", "contact", "careers", "vacancy", "privacy":
		return 0, "Company"
	case "hub", "product":
		return 1, "Products"
	case "sector":
		return 2, "Sectors"
	case "article", "faq":
		return 3, "Knowledge"
	case "case":
		return 4, "Cases"
	default:
		return 5, "Optional"
	}
}

func (b *builder) llms() string {
	o := b.c.Site.Org
	var s strings.Builder
	fmt.Fprintf(&s, "# %s (student mirror of tribelt.nl)\n\n", o.Name)
	fmt.Fprintf(&s, "> %s makes metal conveyor belts (eye-link, open and closed spiral, Tri-flex, other belts) and drives in its own factory in %s, the Netherlands, for food, bakery, recycling, automotive, pharmaceutical and other industries. This site is a student test mirror built with Tribelt's permission; the official site is %s.\n\n", o.LegalName, o.Address.Locality, b.c.Site.Official)
	fmt.Fprintf(&s, "Every page exists in Dutch (default), English and German and is available as Markdown by appending `.md` to its path or sending `Accept: text/markdown`. All pages as one file: %s. Quotes, contact and job applications go through the official site.\n", b.abs("/llms-full.txt"))
	for _, locale := range LocaleOrder() {
		l := b.c.Site.Locales[locale]
		fmt.Fprintf(&s, "\n## %s\n", l.Name)
		var pages []*Page
		for _, p := range b.c.Pages {
			if p.Locale == locale && !p.Noindex {
				pages = append(pages, p)
			}
		}
		slices.SortStableFunc(pages, func(a, c *Page) int {
			sa, _ := llmsSection(a.Type)
			sc, _ := llmsSection(c.Type)
			if sa != sc {
				return sa - sc
			}
			return strings.Compare(a.Path, c.Path)
		})
		last := ""
		for _, p := range pages {
			_, sec := llmsSection(p.Type)
			if sec != last {
				fmt.Fprintf(&s, "\n### %s\n\n", sec)
				last = sec
			}
			fmt.Fprintf(&s, "- [%s](%s): %s\n", p.H1, b.abs(TwinPath(p.Path)), p.Description)
		}
	}
	return s.String()
}

func (b *builder) llmsFull() string {
	var s strings.Builder
	fmt.Fprintf(&s, "# %s: all pages (student mirror of %s)\n", b.c.Site.Org.Name, b.c.Site.Official)
	for _, locale := range LocaleOrder() {
		for _, p := range b.c.Pages {
			if p.Locale == locale && !p.Noindex {
				s.WriteString("\n---\n\n")
				s.WriteString(b.markdownTwin(p))
			}
		}
	}
	return s.String()
}

type urlset struct {
	XMLName xml.Name   `xml:"urlset"`
	NS      string     `xml:"xmlns,attr"`
	XHTML   string     `xml:"xmlns:xhtml,attr"`
	URLs    []sitemapU `xml:"url"`
}

type sitemapU struct {
	Loc     string     `xml:"loc"`
	LastMod string     `xml:"lastmod,omitempty"`
	Links   []xhtmlAlt `xml:"xhtml:link"`
}

type xhtmlAlt struct {
	Rel      string `xml:"rel,attr"`
	Hreflang string `xml:"hreflang,attr"`
	Href     string `xml:"href,attr"`
}

func (b *builder) sitemap() ([]byte, error) {
	set := urlset{NS: "http://www.sitemaps.org/schemas/sitemap/0.9", XHTML: "http://www.w3.org/1999/xhtml"}
	for _, locale := range LocaleOrder() {
		for _, p := range b.c.Pages {
			if p.Locale == locale && !p.Noindex {
				set.URLs = append(set.URLs, b.sitemapURL(p))
			}
		}
	}
	out, err := xml.MarshalIndent(set, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), append(out, '\n')...), nil
}

func (b *builder) sitemapURL(p *Page) sitemapU {
	u := sitemapU{Loc: b.abs(p.Path)}
	if lm := b.lastMod(p); !lm.IsZero() {
		u.LastMod = lm.UTC().Format(time.DateOnly)
	}
	alts := b.indexedAlternates(p)
	if len(alts) < 2 {
		return u
	}
	for _, a := range alts {
		u.Links = append(u.Links, xhtmlAlt{Rel: "alternate", Hreflang: b.c.Site.Locales[a.Locale].Hreflang, Href: b.abs(a.Path)})
		if a.Locale == "nl" {
			u.Links = append(u.Links, xhtmlAlt{Rel: "alternate", Hreflang: "x-default", Href: b.abs(a.Path)})
		}
	}
	return u
}

// indexedAlternates are the hreflang group members that may be indexed.
func (b *builder) indexedAlternates(p *Page) []*Page {
	var alts []*Page
	for _, a := range p.Alternates {
		if !a.Noindex {
			alts = append(alts, a)
		}
	}
	return alts
}

func (b *builder) lastMod(p *Page) time.Time {
	if b.opts.LastMod != nil {
		if t := b.opts.LastMod(p); !t.IsZero() {
			return t
		}
	}
	return b.opts.Now
}
