package content

import (
	"html/template"
	"path"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
)

type pageView struct {
	Lang, Title, Description, Keywords, Canonical string
	Noindex                                       bool
	Alternates                                    []altLink
	MarkdownURL                                   string
	OGType, OGLocale, OGImage, OGImageAlt         string
	OGAltLocales                                  []string
	SiteName, HomeHref, CSS, Beacon, Hit          string
	JSONLD                                        template.JS
	UI                                            UI
	Languages                                     []langLink
	Nav                                           []navLink
	Breadcrumbs                                   []crumb
	Type, H1                                      string
	Image                                         *imageView
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
}

type crumb struct{ Name, Href string }

type imageView struct {
	Src, Srcset, Alt, Credit string
	Width, Height            int
}

type faqView struct {
	Q string
	A template.HTML
}

type footerView struct {
	Note, OfficialLabel, OfficialHref, SiteURL, SiteLabel, PrivacyHref, PrivacyLabel string
}

func (b *builder) common(locale string) pageView {
	l := b.c.Site.Locales[locale]
	home := b.c.Home(locale)
	v := pageView{
		Lang: l.Hreflang, SiteName: b.c.Site.Org.Name, HomeHref: home.Path, CSS: b.css, Beacon: b.beacon, Hit: HitPlaceholder,
		UI: uiStrings(locale), OGType: "website", OGLocale: ogLocale(locale),
		Footer: footerView{
			Note: l.Footer.Note, OfficialLabel: l.Footer.OfficialLabel, SiteURL: b.c.Site.Official,
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
			v.Nav = append(v.Nav, navLink{Label: n.Label, Href: t.Path})
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
		v.Image = b.imageView(p.Image)
		v.OGImage, v.OGImageAlt = b.abs(v.Image.Src), p.Image.Alt
	}
	body, err := renderMarkdown(md, p.Body, p.Path)
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
	return v
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

func (b *builder) imageView(img *Image) *imageView {
	name := strings.TrimPrefix(img.Src, "images/")
	v := &imageView{Src: "/images/" + name, Alt: img.Alt, Credit: img.Credit}
	info, ok := b.images[name]
	if ok {
		v.Width, v.Height = info.width, info.height
	}
	base := strings.TrimSuffix(name, path.Ext(name))
	var set []string
	for _, w := range []int{480, 960} {
		variant := base + "-" + strconv.Itoa(w) + path.Ext(name)
		if b.c.Images[variant] {
			set = append(set, "/images/"+variant+" "+strconv.Itoa(w)+"w")
		}
	}
	if len(set) > 0 && ok {
		set = append(set, v.Src+" "+strconv.Itoa(info.width)+"w")
		v.Srcset = strings.Join(set, ", ")
	}
	return v
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
