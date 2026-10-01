package content

import (
	"html/template"
	"path"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
)

type pageView struct {
	Lang, Title, Description, Keywords, Canonical string
	Path                                          string
	Noindex                                       bool
	Alternates                                    []altLink
	MarkdownURL                                   string
	OGType, OGLocale, OGImage, OGImageAlt         string
	OGAltLocales                                  []string
	SiteName, HomeHref, CSS, Beacon, Hit, Auth    string
	Logo, LogoFooter                              string
	Fonts                                         []string
	JSONLD                                        template.JS
	UI                                            UI
	Languages                                     []langLink
	Nav                                           []navLink
	Breadcrumbs                                   []crumb
	Type, H1, Theme                               string
	Image                                         *imageView
	Hero                                          *imageView
	Help                                          helpView
	Body                                          template.HTML
	Product                                       *Product
	FAQ                                           []faqView
	CTA                                           *CTA
	Footer                                        footerView
}

type altLink struct{ Hreflang, URL string }

type langLink struct {
	Code, Hreflang, Href string
	Current              bool
}

type navLink struct {
	Label, Href string
	Current     bool
	// Slot places the link: main (header bar), util (top row) or contact (the pill button).
	Slot string
}

// helpView is the "how can we help" box that links to the Official Page for contact.
type helpView struct{ Title, Label, Href string }

type crumb struct{ Name, Href string }

type imageView struct {
	Src, Srcset, Alt, Credit string
	Width, Height            int
}

// heroSizes is the rendered width of a hero image, shared by the <img> and its preload.
const heroSizes = "(min-width: 62em) calc(100vw - 60px), calc(100vw - 32px)"

// PreloadAttrs are the responsive attributes of the hero preload link. html/template would treat
// imagesrcset as one URL and escape its spaces, so the pair is built here.
func (v *imageView) PreloadAttrs() template.HTMLAttr {
	if v.Srcset == "" {
		return ""
	}
	return template.HTMLAttr(`imagesrcset="` + template.HTMLEscapeString(v.Srcset) + `" imagesizes="` + heroSizes + `"`) //nolint:gosec // escaped above; sizes is a constant
}

type faqView struct {
	Q string
	A template.HTML
}

type footerView struct {
	OfficialLabel, OfficialHref, SiteURL, SiteLabel, PrivacyHref, PrivacyLabel string
	ProductsLabel                                                              string
	Products                                                                   []navLink
	Org                                                                        Org
}

func (b *builder) common(locale string) pageView {
	l := b.c.Site.Locales[locale]
	home := b.c.Home(locale)
	v := pageView{
		Lang: l.Hreflang, SiteName: b.c.Site.Org.Name, HomeHref: home.Path, CSS: b.css, Beacon: b.beacon, Hit: HitPlaceholder,
		Auth: AuthPlaceholder, Logo: b.versioned("/static/logo-header.svg"), LogoFooter: b.versioned("/static/logo-footer.svg"),
		Fonts: b.fonts, UI: uiStrings(locale), OGType: "website", OGLocale: ogLocale(locale),
		Footer: footerView{
			OfficialLabel: l.Footer.OfficialLabel, SiteURL: b.c.Site.Official, Org: b.c.Site.Org,
			SiteLabel: or(l.Footer.OfficialSiteLabel, "tribelt.nl"), PrivacyLabel: or(l.Footer.PrivacyLabel, uiStrings(locale).Privacy),
		},
	}
	for _, p := range b.c.Pages {
		if p.Locale == locale && p.Type == "privacy" {
			v.Footer.PrivacyHref = p.Path
		}
	}
	for _, n := range l.Nav {
		if t := b.c.InLocale(n.ID, locale); t != nil {
			v.Nav = append(v.Nav, navLink{Label: n.Label, Href: t.Path, Slot: navSlot(n.ID)})
			if t.ID == "metalen-transportbanden" {
				v.Footer.ProductsLabel = n.Label
			}
		}
	}
	for _, p := range b.c.Pages {
		if p.Locale == locale && p.Type == "product" {
			v.Footer.Products = append(v.Footer.Products, navLink{Label: p.H1, Href: p.Path})
		}
	}
	for _, code := range LocaleOrder() {
		ll := b.c.Site.Locales[code]
		v.Languages = append(v.Languages, langLink{Code: code, Hreflang: ll.Hreflang, Href: b.c.Home(code).Path, Current: code == locale})
	}
	return v
}

func (b *builder) pageView(md goldmark.Markdown, p *Page) (pageView, error) {
	v := b.common(p.Locale)
	v.Title, v.Description, v.Keywords, v.Noindex = p.Title, p.Description, strings.Join(p.Keywords, ", "), p.Noindex
	v.Canonical = b.abs(p.Path)
	v.MarkdownURL = TwinPath(p.Path)
	v.Type, v.H1, v.Product = p.Type, p.H1, p.Product
	v.Footer.OfficialHref = GoLink(p.Official, p.Path)
	v.Path, v.Help = p.Path, b.help(p.Locale, p.Path)
	switch p.Type {
	case "article", "news", "case":
		v.OGType = "article"
	case "product":
		v.OGType = "product"
	}
	v.Alternates, v.OGAltLocales = b.alternates(p)
	for i := range v.Languages {
		if a := alternateIn(p, v.Languages[i].Code); a != nil {
			v.Languages[i].Href = a.Path
		}
	}
	for i := range v.Nav {
		v.Nav[i].Current = v.Nav[i].Href == p.Path
	}
	v.Breadcrumbs = b.breadcrumbs(p)
	if p.Image != nil {
		v.Image = b.pageImage(p)
		v.OGImage, v.OGImageAlt = b.abs(v.Image.Src), p.Image.Alt
	}
	v.Hero = b.pageImage(p)
	if p.Type == "sector" || p.Type == "news" || p.ID == "sectoren" {
		v.Theme = "blue"
	}
	body, err := renderWith(md, p.Body, p.Path, func(doc ast.Node, src []byte) { b.layout(doc, src, p) })
	if err != nil {
		return v, err
	}
	v.Body = template.HTML(body) //nolint:gosec // rendered from repository Markdown by goldmark, raw HTML disabled
	for _, f := range p.FAQ {
		a, err := renderMarkdown(md, f.A, p.Path)
		if err != nil {
			return v, err
		}
		v.FAQ = append(v.FAQ, faqView{Q: f.Q, A: template.HTML(a)}) //nolint:gosec // as above
	}
	if p.CTA != nil {
		v.CTA = &CTA{Label: p.CTA.Label, Href: GoLink(p.CTA.Href, p.Path)}
	}
	ld, err := b.jsonLD(p, v.Breadcrumbs)
	if err != nil {
		return v, err
	}
	v.JSONLD = ld
	return v, nil
}

// alternates are the hreflang links of an indexable page, matching the sitemap.
func (b *builder) alternates(p *Page) ([]altLink, []string) {
	alts := b.indexedAlternates(p)
	if p.Noindex || len(alts) < 2 {
		return nil, nil
	}
	var links []altLink
	var og []string
	for _, a := range alts {
		links = append(links, altLink{Hreflang: b.c.Site.Locales[a.Locale].Hreflang, URL: b.abs(a.Path)})
		if a.Locale == "nl" {
			links = append(links, altLink{Hreflang: "x-default", URL: b.abs(a.Path)})
		}
		if a != p {
			og = append(og, ogLocale(a.Locale))
		}
	}
	return links, og
}

func (b *builder) notFoundView(locale string) pageView {
	v := b.common(locale)
	ui := v.UI
	v.Title, v.H1, v.Description, v.Noindex = ui.NotFoundTitle+" | "+b.c.Site.Org.Name, ui.NotFoundTitle, ui.NotFoundText, true
	v.Footer.OfficialHref = GoLink(b.c.Site.Official+b.c.Home(locale).Path, b.c.Home(locale).Path)
	v.Help = b.help(locale, b.c.Home(locale).Path)
	v.Type = "notfound"
	return v
}

// navSlot places the pages Tribelt keeps in its top row there, and contact in the pill button.
func navSlot(id string) string {
	switch id {
	case "werken-bij-tribelt", "nieuws", "veelgestelde-vragen":
		return "util"
	case "contact":
		return "contact"
	}
	return "main"
}

// help links the "how can we help" box to the Official Page for contact, as an Outbound Click.
func (b *builder) help(locale, from string) helpView {
	ui := uiStrings(locale)
	to := b.c.Site.Official + "/contact"
	if c := b.c.InLocale("contact", locale); c != nil {
		to = c.Official
	}
	return helpView{Title: ui.HelpTitle, Label: ui.HelpLink, Href: GoLink(to, from)}
}

func alternateIn(p *Page, locale string) *Page {
	for _, a := range p.Alternates {
		if a.Locale == locale {
			return a
		}
	}
	return nil
}

// breadcrumbs walks up the path, keeping ancestors that resolve to a page.
func (b *builder) breadcrumbs(p *Page) []crumb {
	home := b.c.Home(p.Locale)
	if p == home {
		return nil
	}
	crumbs := []crumb{{Name: uiStrings(p.Locale).Home, Href: home.Path}}
	var middle []crumb
	for dir := path.Dir(p.Path); dir != "/" && dir != home.Path && dir != "."; dir = path.Dir(dir) {
		if anc, ok := b.c.Resolve(dir); ok && anc != home && anc != p {
			middle = append([]crumb{{Name: anc.H1, Href: anc.Path}}, middle...)
		}
	}
	crumbs = append(crumbs, middle...)
	return append(crumbs, crumb{Name: p.H1})
}

// TwinPath is the Markdown twin URL of a page path.
func TwinPath(p string) string {
	if p == "/" {
		return "/index.md"
	}
	return p + ".md"
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
