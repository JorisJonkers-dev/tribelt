// Package content loads and validates the Mirror Pages under content/ and pre-renders every public
// resource: HTML pages, Markdown twins, sitemap, robots and llms files.
package content

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Site is content/site.yml.
type Site struct {
	Release  Release            `yaml:"release"`
	BaseURL  string             `yaml:"baseUrl"`
	Official string             `yaml:"official"`
	Org      Org                `yaml:"org"`
	Locales  map[string]*Locale `yaml:"locales"`
}

// Release is the current Content Release.
type Release struct {
	Label string `yaml:"label"`
	Note  string `yaml:"note"`
}

// Org feeds the Organization JSON-LD.
type Org struct {
	LegalName          string   `yaml:"legalName"`
	Name               string   `yaml:"name"`
	URL                string   `yaml:"url"`
	Logo               string   `yaml:"logo"`
	Address            Address  `yaml:"address"`
	Telephone          string   `yaml:"telephone"`
	Email              string   `yaml:"email"`
	FoundingDate       string   `yaml:"foundingDate"`
	ParentOrganization string   `yaml:"parentOrganization"`
	SameAs             []string `yaml:"sameAs"`
}

// Address is a postal address.
type Address struct {
	Street     string `yaml:"street"`
	PostalCode string `yaml:"postalCode"`
	Locality   string `yaml:"locality"`
	Country    string `yaml:"country"`
}

// Locale is one language edition with its navigation and footer.
type Locale struct {
	Code     string    `yaml:"-"`
	Prefix   string    `yaml:"prefix"`
	Hreflang string    `yaml:"hreflang"`
	Name     string    `yaml:"name"`
	Nav      []NavItem `yaml:"nav"`
	Footer   Footer    `yaml:"footer"`
}

// NavItem links to the page with the given id in the same Locale.
type NavItem struct {
	Label string `yaml:"label"`
	ID    string `yaml:"id"`
}

// Footer carries the footer link labels and the student-project note that opens every Markdown twin
// (HTML pages say the same in the test-site bar).
type Footer struct {
	Note              string `yaml:"note"`
	OfficialLabel     string `yaml:"officialLabel"`
	OfficialSiteLabel string `yaml:"officialSiteLabel"`
	PrivacyLabel      string `yaml:"privacyLabel"`
}

// Redirect is one official 301.
type Redirect struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

// Page is one Mirror Page: front matter plus Markdown body.
type Page struct {
	ID          string   `yaml:"id"`
	Locale      string   `yaml:"locale"`
	Path        string   `yaml:"path"`
	Official    string   `yaml:"official"`
	Type        string   `yaml:"type"`
	Title       string   `yaml:"title"`
	Description string   `yaml:"description"`
	Keywords    []string `yaml:"keywords"`
	H1          string   `yaml:"h1"`
	Image       *Image   `yaml:"image"`
	Product     *Product `yaml:"product"`
	FAQ         []FAQ    `yaml:"faq"`
	CTA         *CTA     `yaml:"cta"`
	Noindex     bool     `yaml:"noindex"`

	Body string `yaml:"-" json:"-"`
	File string `yaml:"-" json:"-"`
	// Alternates are the pages sharing this page's id, this page included, in locale order.
	Alternates []*Page `yaml:"-" json:"-"`
}

// Hash identifies the page's published text and metadata.
func (p *Page) Hash() string {
	raw, _ := json.Marshal(p)
	sum := sha256.Sum256(append(raw, p.Body...))
	return hex.EncodeToString(sum[:8])
}

// Image is a page's lead image.
type Image struct {
	Src    string `yaml:"src"`
	Alt    string `yaml:"alt"`
	Credit string `yaml:"credit"`
}

// Product feeds the spec table and Product JSON-LD.
type Product struct {
	Name      string   `yaml:"name"`
	Category  string   `yaml:"category"`
	Specs     []Spec   `yaml:"specs"`
	Materials []string `yaml:"materials"`
}

// Spec is one product property.
type Spec struct {
	Name  string `yaml:"name"`
	Value string `yaml:"value"`
	Unit  string `yaml:"unit"`
}

// FAQ is one question and answer.
type FAQ struct {
	Q string `yaml:"q"`
	A string `yaml:"a"`
}

// CTA is the Outbound Click link to the Official Page.
type CTA struct {
	Label string `yaml:"label"`
	Href  string `yaml:"href"`
}

// Content is everything under content/, validated.
type Content struct {
	Site      Site
	Pages     []*Page
	ByPath    map[string]*Page
	Redirects map[string]string
	// Images lists the files under images/ (relative names).
	Images map[string]bool
	// Hash identifies the exact content state.
	Hash string
}

// LocaleOrder is the fixed order locales appear in alternates and listings.
func LocaleOrder() []string { return []string{"nl", "en", "de"} }

// PageTypes lists the allowed page types.
func PageTypes() []string {
	return []string{"home", "hub", "product", "sector", "article", "case", "news", "about", "contact", "faq", "careers", "vacancy", "privacy"}
}

// Home returns the home page of a locale.
func (c *Content) Home(locale string) *Page {
	for _, p := range c.Pages {
		if p.Locale == locale && p.Type == "home" {
			return p
		}
	}
	return nil
}

// InLocale returns the page with id in the given locale.
func (c *Content) InLocale(id, locale string) *Page {
	for _, p := range c.Pages {
		if p.ID == id && p.Locale == locale {
			return p
		}
	}
	return nil
}

// LocaleOf returns the locale a request path belongs to.
func LocaleOf(path string) string {
	for _, l := range []string{"en", "de"} {
		if path == "/"+l || len(path) > len(l)+1 && path[:len(l)+2] == "/"+l+"/" {
			return l
		}
	}
	return "nl"
}
