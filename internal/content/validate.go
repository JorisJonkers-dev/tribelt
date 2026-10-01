package content

import (
	"fmt"
	"net/url"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// Title and description bounds (runes).
const (
	MaxTitle       = 60
	MinDescription = 120
	MaxDescription = 160
)

var (
	releaseLabel = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,62}$`)
	idPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// problems collects validation errors with a shared prefix.
type problems struct {
	errs   []error
	prefix string
}

func (p *problems) add(format string, a ...any) {
	p.errs = append(p.errs, fmt.Errorf(p.prefix+format, a...))
}

func (c *Content) validate() []error {
	p := &problems{}
	c.validateSite(p)
	for _, code := range LocaleOrder() {
		c.validateLocale(p, code)
	}
	for _, pg := range c.Pages {
		p.errs = append(p.errs, c.validatePage(pg)...)
	}
	c.validateUnique(p)
	c.validateRedirects(p)
	return p.errs
}

func (c *Content) validateSite(p *problems) {
	s := c.Site
	if !releaseLabel.MatchString(s.Release.Label) {
		p.add("site.yml: release.label %q must be a lowercase slug like v2-longtail-keywords", s.Release.Label)
	}
	if strings.TrimSpace(s.Release.Note) == "" {
		p.add("site.yml: release.note is required")
	}
	validateTags(p, s.Release.Tags)
	if u, err := url.Parse(s.BaseURL); err != nil || u.Scheme == "" || u.Host == "" || strings.HasSuffix(s.BaseURL, "/") {
		p.add("site.yml: baseUrl %q must be an absolute URL without trailing slash", s.BaseURL)
	}
	if !isOfficialURL(s.Official) {
		p.add("site.yml: official %q must be https://www.tribelt.nl", s.Official)
	}
	if s.Org.LegalName == "" || s.Org.Name == "" || s.Org.URL == "" {
		p.add("site.yml: org.legalName, org.name and org.url are required")
	}
	for code := range s.Locales {
		if !slices.Contains(LocaleOrder(), code) {
			p.add("site.yml: unknown locale %s", code)
		}
	}
}

// validateTags checks the optional experiment tags of a Content Release.
func validateTags(p *problems, tags []string) {
	seen := map[string]bool{}
	for _, tag := range tags {
		if !idPattern.MatchString(tag) || len(tag) > 40 {
			p.add("site.yml: release tag %q must be a lowercase slug like faq-schema", tag)
		}
		if seen[tag] {
			p.add("site.yml: release tag %q is listed twice", tag)
		}
		seen[tag] = true
	}
}

func (c *Content) validateLocale(p *problems, code string) {
	l := c.Site.Locales[code]
	if l == nil {
		p.add("site.yml: locale %s is missing", code)
		return
	}
	wantPrefix := "/" + code
	if code == "nl" {
		wantPrefix = ""
	}
	if l.Prefix != wantPrefix {
		p.add("site.yml: locale %s prefix must be %q", code, wantPrefix)
	}
	if l.Hreflang == "" || l.Name == "" || l.Footer.Note == "" || l.Footer.OfficialLabel == "" {
		p.add("site.yml: locale %s needs hreflang, name, footer.note and footer.officialLabel", code)
	}
	for _, n := range l.Nav {
		if n.Label == "" || c.InLocale(n.ID, code) == nil {
			p.add("site.yml: locale %s nav item %q links to unknown page id %q", code, n.Label, n.ID)
		}
	}
	if c.Home(code) == nil {
		p.add("locale %s has no page of type home", code)
	}
}

// validateUnique fails on duplicate paths, per-locale ids, titles and descriptions.
func (c *Content) validateUnique(p *problems) {
	checks := []struct {
		what string
		key  func(*Page) string
	}{
		{"path", func(pg *Page) string { return pg.Path }},
		{"id in its locale", func(pg *Page) string { return pg.Locale + "/" + pg.ID }},
		{"title", func(pg *Page) string { return pg.Title }},
		{"description", func(pg *Page) string { return pg.Description }},
	}
	for _, chk := range checks {
		seen := map[string]string{}
		for _, pg := range c.Pages {
			k := chk.key(pg)
			if prev, dup := seen[k]; dup && k != "" {
				p.add("%s: %s %q already used by %s", pg.File, chk.what, k, prev)
			}
			seen[k] = pg.File
		}
	}
}

func (c *Content) validateRedirects(p *problems) {
	froms := make([]string, 0, len(c.Redirects))
	for from := range c.Redirects {
		froms = append(froms, from)
	}
	sort.Strings(froms)
	for _, from := range froms {
		to := c.Redirects[from]
		if !strings.HasPrefix(from, "/") || !strings.HasPrefix(to, "/") {
			p.add("redirects.yml: %s -> %s: paths must start with /", from, to)
			continue
		}
		if _, clash := c.ByPath[from]; clash {
			p.add("redirects.yml: %s is also a page path", from)
		}
		if _, ok := c.Resolve(to); !ok {
			p.add("redirects.yml: %s -> %s does not reach a page", from, to)
		}
	}
}

func (c *Content) validatePage(pg *Page) []error {
	p := &problems{prefix: pg.File + ": "}
	validateIdentity(p, pg)
	validateMeta(p, pg)
	c.validateBlocks(p, pg)
	c.validateBody(p, pg)
	return p.errs
}

func validateIdentity(p *problems, pg *Page) {
	dir, name := path.Split(strings.TrimPrefix(pg.File, "pages/"))
	switch {
	case !idPattern.MatchString(pg.ID):
		p.add("id %q must be lowercase letters, digits and hyphens", pg.ID)
	case name != pg.ID+".md":
		p.add("file name must be %s.md", pg.ID)
	}
	knownLocale := slices.Contains(LocaleOrder(), pg.Locale)
	switch {
	case !knownLocale:
		p.add("unknown locale %q", pg.Locale)
	case strings.TrimSuffix(dir, "/") != pg.Locale:
		p.add("locale %s must live in pages/%s/", pg.Locale, pg.Locale)
	}
	switch {
	case !strings.HasPrefix(pg.Path, "/") || (pg.Path != "/" && strings.HasSuffix(pg.Path, "/")) || strings.ContainsAny(pg.Path, "?# "):
		p.add("path %q must start with / and have no trailing slash, query or spaces", pg.Path)
	case knownLocale && LocaleOf(pg.Path) != pg.Locale:
		p.add("path %s does not belong to locale %s", pg.Path, pg.Locale)
	}
	if !isOfficialURL(pg.Official) {
		p.add("official %q must be a https://www.tribelt.nl URL", pg.Official)
	} else if u, _ := url.Parse(pg.Official); officialPath(u) != pg.Path {
		p.add("official %s must have the same path as %s (ADR-0002)", pg.Official, pg.Path)
	}
}

func validateMeta(p *problems, pg *Page) {
	if !slices.Contains(PageTypes(), pg.Type) {
		p.add("type %q must be one of %v", pg.Type, PageTypes())
	}
	if n := utf8.RuneCountInString(pg.Title); n == 0 || n > MaxTitle {
		p.add("title must be 1-%d characters (has %d)", MaxTitle, n)
	}
	if n := utf8.RuneCountInString(pg.Description); n < MinDescription || n > MaxDescription {
		p.add("description must be %d-%d characters (has %d)", MinDescription, MaxDescription, n)
	}
	if strings.TrimSpace(pg.H1) == "" {
		p.add("h1 is required")
	}
	for _, f := range pg.FAQ {
		if strings.TrimSpace(f.Q) == "" || strings.TrimSpace(f.A) == "" {
			p.add("faq entries need q and a")
		}
	}
	if pg.CTA != nil && (pg.CTA.Label == "" || !isOfficialURL(pg.CTA.Href)) {
		p.add("cta needs a label and a https://www.tribelt.nl href")
	}
}

func (c *Content) validateBlocks(p *problems, pg *Page) {
	if pg.Image != nil {
		if strings.TrimSpace(pg.Image.Alt) == "" {
			p.add("image.alt is required")
		}
		if imageFile(c, pg.Image.Src) != nil {
			p.add("image %s not found under images/", pg.Image.Src)
		}
	}
	if pg.Product == nil {
		return
	}
	if pg.Type != "product" {
		p.add("product block is only allowed on type product")
	}
	if pg.Product.Name == "" {
		p.add("product.name is required")
	}
	for _, s := range pg.Product.Specs {
		if s.Name == "" || s.Value == "" {
			p.add("product spec needs name and value")
		}
	}
}

// validateBody enforces a single H1 (from front matter), alt text and resolvable internal links.
func (c *Content) validateBody(p *problems, pg *Page) {
	src := []byte(pg.Body)
	doc := markdown().Parser().Parse(text.NewReader(src))
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Heading:
			if n.Level == 1 {
				p.add("body must not contain an H1; it comes from front matter")
			}
		case *ast.Image:
			if plainText(n, src) == "" {
				p.add("image %s has no alt text", n.Destination)
			}
		case *ast.Link:
			c.checkLink(p, string(n.Destination))
		}
		return ast.WalkContinue, nil
	})
}

func (c *Content) checkLink(p *problems, dest string) {
	if !strings.HasPrefix(dest, "/") || strings.HasPrefix(dest, "//") {
		return
	}
	target, _, _ := strings.Cut(dest, "#")
	target, _, _ = strings.Cut(target, "?")
	if strings.HasPrefix(target, "/images/") {
		if !c.Images[strings.TrimPrefix(target, "/images/")] {
			p.add("link %s points to a missing image", dest)
		}
		return
	}
	if target == "/index.md" {
		target = "/"
	}
	if _, ok := c.Resolve(strings.TrimSuffix(target, ".md")); !ok {
		p.add("link %s does not resolve to a Mirror Page", dest)
	}
}

func isOfficialURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && (u.Host == "www.tribelt.nl" || u.Host == "tribelt.nl")
}

func officialPath(u *url.URL) string {
	if u.Path == "" {
		return "/"
	}
	return u.Path
}
