package content

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/JorisJonkers-dev/tribelt/web"
)

func loadFixture(t *testing.T) *Content {
	t.Helper()
	c, err := Load(os.DirFS("testdata/content"))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func buildOpts(t *testing.T, images fs.FS) Options {
	t.Helper()
	return Options{
		Templates: sub(t, web.Templates, "templates"), Static: sub(t, web.Static, "static"), Images: images,
		Now: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
	}
}

func buildFixture(t *testing.T) *Built {
	t.Helper()
	b, err := Build(loadFixture(t), buildOpts(t, os.DirFS("testdata/content/images")))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// fixtureFS copies the fixture into memory so a test can break one file.
func fixtureFS(t *testing.T) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	root := os.DirFS("testdata/content")
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(root, p)
		m[p] = &fstest.MapFile{Data: raw}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func edit(m fstest.MapFS, file, old, repl string) {
	m[file] = &fstest.MapFile{Data: []byte(strings.Replace(string(m[file].Data), old, repl, 1))}
}

func appendTo(m fstest.MapFS, file, extra string) {
	m[file] = &fstest.MapFile{Data: append(append([]byte{}, m[file].Data...), extra...)}
}

const product = "pages/nl/transportbanden--draadogenbanden.md"

func TestTheFixtureLoadsEveryPageWithItsRelease(t *testing.T) {
	c := loadFixture(t)
	if len(c.Pages) != 8 || c.Site.Release.Label != "v1-fixture" || len(c.Hash) != 16 {
		t.Fatalf("pages=%d release=%s hash=%s", len(c.Pages), c.Site.Release.Label, c.Hash)
	}
}

func TestAPageListsItsTranslations(t *testing.T) {
	p := loadFixture(t).ByPath["/transportbanden/draadogenbanden"]
	if p == nil || len(p.Alternates) != 2 || p.Alternates[0].Locale != "nl" || p.Alternates[1].Locale != "en" {
		t.Fatalf("alternates: %+v", p)
	}
}

func TestARedirectChainResolvesToItsPage(t *testing.T) {
	if got, ok := loadFixture(t).Resolve("/producten"); !ok || got.Path != "/metalen-transportbanden" {
		t.Fatalf("Resolve = %+v, %v", got, ok)
	}
}

func TestAnUnknownPathDoesNotResolve(t *testing.T) {
	if _, ok := loadFixture(t).Resolve("/missing"); ok {
		t.Fatal("resolved")
	}
}

func TestEachLocaleHasAHomeAndAnUnknownLocaleHasNothing(t *testing.T) {
	c := loadFixture(t)
	if c.Home("de").Path != "/de" || c.InLocale("home", "xx") != nil || c.Home("xx") != nil {
		t.Fatal("lookups")
	}
}

func TestAPathsLocaleIsItsFirstSegmentOrDutch(t *testing.T) {
	for path, want := range map[string]string{"/": "nl", "/en": "en", "/en/x": "en", "/de/x": "de", "/enx": "nl", "/details": "nl"} {
		t.Run(path, func(t *testing.T) {
			if got := LocaleOf(path); got != want {
				t.Fatalf("LocaleOf = %s, want %s", got, want)
			}
		})
	}
}

func TestEachPageHasItsOwnHash(t *testing.T) {
	c := loadFixture(t)
	if c.ByPath["/transportbanden/draadogenbanden"].Hash() == c.ByPath["/en/conveyor-belts/eye-link-belts"].Hash() {
		t.Fatal("two pages share a hash")
	}
}

func TestAWordCountSkipsMarkdownSyntax(t *testing.T) {
	body := "## Kop\n\nEen twee drie.\n\n- vier\n- vijf\n\n| a | b |\n| - | - |\n| zes | zeven |"
	if n := WordCount(body); n != 10 {
		t.Fatalf("word count %d", n)
	}
}

func TestValidationFailsFast(t *testing.T) {
	cases := []struct {
		name  string
		apply func(m fstest.MapFS)
		want  string
	}{
		{"duplicate path", func(m fstest.MapFS) {
			edit(m, "pages/nl/privacyverklaring.md", "path: /privacyverklaring", "path: /metalen-transportbanden")
		}, `path "/metalen-transportbanden" already used`},
		{"duplicate title", func(m fstest.MapFS) {
			edit(m, "pages/nl/privacyverklaring.md", `title: "Privacy op deze studententestsite"`, `title: "Overzicht van alle metalen transportbanden | Tribelt"`)
		}, "title \"Overzicht"},
		{"duplicate description", func(m fstest.MapFS) {
			edit(m, "pages/en/home.md", "Tribelt designs and builds metal conveyor belts and drives for food, recycling and industry in its own factory in Haaksbergen in the east of the Netherlands.",
				"Tribelt ontwerpt en maakt metalen transportbanden en aandrijvingen voor voeding, recycling en industrie, in de eigen fabriek in Haaksbergen gebouwd.")
		}, "description \"Tribelt ontwerpt"},
		{"duplicate id in locale", func(m fstest.MapFS) {
			m["pages/nl/home2.md"] = &fstest.MapFile{Data: []byte(strings.Replace(string(m["pages/nl/home.md"].Data), "id: home", "id: home2", 1))}
			edit(m, "pages/nl/home2.md", "path: /", "path: /home2")
		}, "already used"},
		{"file name", func(m fstest.MapFS) { edit(m, "pages/nl/privacyverklaring.md", "id: privacyverklaring", "id: privacy") }, "file name must be privacy.md"},
		{"missing h1", func(m fstest.MapFS) { edit(m, "pages/nl/home.md", `h1: "Metalen transportbanden op maat"`, `h1: ""`) }, "h1 is required"},
		{"long title", func(m fstest.MapFS) { edit(m, "pages/nl/home.md", `title: "`, `title: "`+strings.Repeat("x", 60)) }, "title must be 1-60"},
		{"description bounds", func(m fstest.MapFS) {
			edit(m, "pages/de/home.md", " im eigenen Werk in Haaksbergen in den Niederlanden.", ".")
		}, "description must be 120-160"},
		{"missing alt", func(m fstest.MapFS) { edit(m, product, `alt: "Draadogenband van roestvrij staal", `, "") }, "image.alt is required"},
		{"missing image file", func(m fstest.MapFS) { edit(m, product, "src: images/product.webp", "src: images/nope.webp") }, "not found under images/"},
		{"alt in body", func(m fstest.MapFS) { edit(m, product, "## Toepassing", "![](/images/product.webp)\n\n## Toepassing") }, "has no alt text"},
		{"h1 in body", func(m fstest.MapFS) { edit(m, product, "## Toepassing", "# Toepassing") }, "must not contain an H1"},
		{"dead internal link", func(m fstest.MapFS) { edit(m, "pages/nl/home.md", "(/producten)", "(/bestaat-niet)") }, "does not resolve"},
		{"dead image link", func(m fstest.MapFS) { edit(m, "pages/nl/home.md", "(/producten)", "(/images/nope.webp)") }, "missing image"},
		{"unknown field", func(m fstest.MapFS) { edit(m, "pages/nl/home.md", "noindex: false", "noindexx: false") }, "field noindexx not found"},
		{"no front matter", func(m fstest.MapFS) { m["pages/nl/home.md"] = &fstest.MapFile{Data: []byte("# hi")} }, "missing front matter"},
		{"unterminated front matter", func(m fstest.MapFS) { m["pages/nl/home.md"] = &fstest.MapFile{Data: []byte("---\nid: home\n")} }, "unterminated"},
		{"wrong locale dir", func(m fstest.MapFS) { edit(m, "pages/de/home.md", "locale: de", "locale: en") }, "must live in pages/en/"},
		{"unknown locale", func(m fstest.MapFS) { edit(m, "pages/de/home.md", "locale: de", "locale: fr") }, `unknown locale "fr"`},
		{"path outside locale", func(m fstest.MapFS) { edit(m, "pages/de/home.md", "path: /de\n", "path: /duits\n") }, "does not belong to locale de"},
		{"trailing slash path", func(m fstest.MapFS) { edit(m, "pages/de/home.md", "path: /de\n", "path: /de/\n") }, "no trailing slash"},
		{"official path differs", func(m fstest.MapFS) {
			edit(m, "pages/de/home.md", "official: https://www.tribelt.nl/de", "official: https://www.tribelt.nl/de/x")
		}, "same path"},
		{"official not tribelt", func(m fstest.MapFS) {
			edit(m, "pages/de/home.md", "official: https://www.tribelt.nl/de", "official: https://evil.example/de")
		}, "must be a https://www.tribelt.nl URL"},
		{"bad id", func(m fstest.MapFS) { edit(m, "pages/de/home.md", "id: home", "id: Home") }, "lowercase letters"},
		{"bad type", func(m fstest.MapFS) { edit(m, "pages/de/home.md", "type: home", "type: blog") }, `type "blog"`},
		{"product off product page", func(m fstest.MapFS) { edit(m, "pages/de/home.md", "type: home", "type: home\nproduct: { name: X }") }, "only allowed on type product"},
		{"product without name", func(m fstest.MapFS) { edit(m, product, "name: Draadogenband", `name: ""`) }, "product.name is required"},
		{"spec without value", func(m fstest.MapFS) { edit(m, product, `value: "40–6500"`, `value: ""`) }, "spec needs name and value"},
		{"faq without answer", func(m fstest.MapFS) { edit(m, "pages/de/home.md", `a: "Ja."`, `a: ""`) }, "faq entries need q and a"},
		{"cta off-site", func(m fstest.MapFS) {
			edit(m, product, `href: "https://www.tribelt.nl/contact"`, `href: "https://evil.example/"`)
		}, "cta needs"},
		{"bad release label", func(m fstest.MapFS) { edit(m, "site.yml", "label: v1-fixture", "label: V1 Fixture") }, "release.label"},
		{"missing release note", func(m fstest.MapFS) { edit(m, "site.yml", `note: "Fixture release for tests"`, `note: ""`) }, "release.note is required"},
		{"bad release tag", func(m fstest.MapFS) {
			edit(m, "site.yml", `note: "Fixture release for tests"`, `note: "n", tags: [FAQ Schema]`)
		}, `release tag "FAQ Schema" must be a lowercase slug`},
		{"duplicate release tag", func(m fstest.MapFS) {
			edit(m, "site.yml", `note: "Fixture release for tests"`, `note: "n", tags: [faq-schema, faq-schema]`)
		}, `release tag "faq-schema" is listed twice`},
		{"unknown release field", func(m fstest.MapFS) { edit(m, "site.yml", `note: "Fixture release for tests"`, `note: "n", tag: x`) }, "field tag not found"},
		{"bad base url", func(m fstest.MapFS) {
			edit(m, "site.yml", "baseUrl: https://mirror.test", "baseUrl: https://mirror.test/")
		}, "baseUrl"},
		{"bad official", func(m fstest.MapFS) {
			edit(m, "site.yml", "official: https://www.tribelt.nl", "official: http://tribelt.nl")
		}, "site.yml: official"},
		{"missing org", func(m fstest.MapFS) { edit(m, "site.yml", "legalName: Tribelt B.V.", `legalName: ""`) }, "org.legalName"},
		{"unknown locale in site", func(m fstest.MapFS) {
			edit(m, "site.yml", "locales:", "locales:\n  fr: { prefix: /fr, hreflang: fr, name: Francais, footer: { note: n, officialLabel: o } }")
		}, "unknown locale fr"},
		{"missing locale", func(m fstest.MapFS) { edit(m, "site.yml", "  de:\n    prefix: /de", "  xx:\n    prefix: /de") }, "locale de is missing"},
		{"bad prefix", func(m fstest.MapFS) { edit(m, "site.yml", "prefix: /en", "prefix: /english") }, "prefix must be"},
		{"missing footer", func(m fstest.MapFS) {
			edit(m, "site.yml", `note: "Student test site, not Tribelt's official site.", `, "")
		}, "footer.note"},
		{"dead nav", func(m fstest.MapFS) { edit(m, "site.yml", "id: transportbanden--draadogenbanden }", "id: nope }") }, "unknown page id"},
		{"no home", func(m fstest.MapFS) { edit(m, "pages/de/home.md", "type: home", "type: about") }, "locale de has no page of type home"},
		{"redirect to nowhere", func(m fstest.MapFS) { edit(m, "redirects.yml", "to: /metalen-transportbanden", "to: /nergens") }, "does not reach a page"},
		{"redirect shadows page", func(m fstest.MapFS) { appendTo(m, "redirects.yml", "- { from: /privacyverklaring, to: / }\n") }, "is also a page path"},
		{"redirect relative", func(m fstest.MapFS) { appendTo(m, "redirects.yml", "- { from: oud, to: / }\n") }, "paths must start with /"},
		{"duplicate redirect", func(m fstest.MapFS) { appendTo(m, "redirects.yml", "- { from: /producten, to: / }\n") }, "duplicate from /producten"},
		{"bad redirects yaml", func(m fstest.MapFS) { m["redirects.yml"] = &fstest.MapFile{Data: []byte("{nope")} }, "redirects.yml"},
		{"bad site yaml", func(m fstest.MapFS) { m["site.yml"] = &fstest.MapFile{Data: []byte("release: [")} }, "site.yml"},
		{"no site", func(m fstest.MapFS) { delete(m, "site.yml") }, "site.yml"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := fixtureFS(t)
			tc.apply(m)
			_, err := Load(m)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadWithoutRedirects(t *testing.T) {
	m := fixtureFS(t)
	delete(m, "redirects.yml")
	edit(m, "pages/nl/home.md", " of de [oude productenpagina](/producten)", "")
	c, err := Load(m)
	if err != nil || len(c.Redirects) != 0 {
		t.Fatalf("redirects.yml is optional: %v", err)
	}
	var ve *ValidationError
	edit(m, "pages/nl/home.md", `h1: "Metalen transportbanden op maat"`, `h1: ""`)
	if _, err := Load(m); !errors.As(err, &ve) || len(ve.Problems) != 1 {
		t.Fatalf("validation error: %v", err)
	}
}

var ldRe = regexp.MustCompile(`(?s)<script type="application/ld\+json">(.*?)</script>`)

// productPage is the built HTML of the fixture's Dutch product page, signed out and as a human.
func productPage(t *testing.T) (*Resource, string) {
	t.Helper()
	res := buildFixture(t).HTML["/transportbanden/draadogenbanden"]
	return res, html(res)
}

func contains(t *testing.T, doc string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(doc, want) {
			t.Errorf("page lacks %s", want)
		}
	}
}

func TestAPageHeadNamesItsCanonicalURLAndEveryTranslation(t *testing.T) {
	_, doc := productPage(t)
	contains(t, doc,
		`<html lang="nl">`,
		`<link rel="canonical" href="https://mirror.test/transportbanden/draadogenbanden">`,
		`<link rel="alternate" hreflang="nl" href="https://mirror.test/transportbanden/draadogenbanden">`,
		`<link rel="alternate" hreflang="x-default" href="https://mirror.test/transportbanden/draadogenbanden">`,
		`<link rel="alternate" hreflang="en" href="https://mirror.test/en/conveyor-belts/eye-link-belts">`,
		`<meta property="og:type" content="product">`,
		`<meta property="og:image" content="https://mirror.test/images/product.webp">`,
		`<link rel="preload" as="image" href="/images/product.webp" imagesrcset="/images/product-480.webp 480w`,
	)
}

func TestAProductPageShowsItsSpecificationsQuestionsAndSizedImages(t *testing.T) {
	_, doc := productPage(t)
	contains(t, doc,
		`<th scope="row">Breedte</th><td>40–6500 mm</td>`,
		`<details>`,
		`srcset="/images/product-480.webp 480w`,
		`width="480" height="320"`,
	)
}

func TestLinksToTheOfficialSiteGoThroughTheRedirectorAsNofollow(t *testing.T) {
	_, doc := productPage(t)
	contains(t, doc,
		`href="/go?from=%2Ftransportbanden%2Fdraadogenbanden&amp;to=https%3A%2F%2Fwww.tribelt.nl%2Fcontact" rel="nofollow">Offerte`,
		`<a href="/go?from=%2Ftransportbanden%2Fdraadogenbanden&amp;to=https%3A%2F%2Fwww.tribelt.nl%2Ftransportbanden%2Fdraadogenbanden" rel="nofollow">Deze pagina op tribelt.nl</a>`,
		`<a href="https://www.tribelt.nl">Officiële site</a>`,
		`<a href="/privacyverklaring">Privacy</a>`,
		`<li><a href="/metalen-transportbanden">Metalen transportbanden</a></li>`,
	)
}

func TestAPageUsesTheMirrorDesign(t *testing.T) {
	_, doc := productPage(t)
	contains(t, doc,
		`data-hit="HIT"`,
		`<a class="btn-pill btn-pill-dark" href="/go?from=%2Ftransportbanden%2Fdraadogenbanden&amp;to=https%3A%2F%2Fwww.tribelt.nl%2Fcontact" rel="nofollow">`,
		`<a class="btn-pill btn-pill-orange" href="/go?from=%2Ftransportbanden%2Fdraadogenbanden&amp;to=https%3A%2F%2Fwww.tribelt.nl%2Fcontact" rel="nofollow">`,
		`<section class="hero hero-split">`,
		`<span class="brand-tag">mirror</span>`,
		`<aside class="notice" aria-label="Melding">`,
		`aria-label="Melding sluiten"`,
	)
}

func TestEveryPageSaysItIsAStudentTestSite(t *testing.T) {
	_, doc := productPage(t)
	contains(t, doc,
		`<aside class="testbar" aria-label="studententestsite">`,
		`Studieproject: dit is een testsite, niet de officiële website van Tribelt.`,
	)
	if strings.Contains(doc, "Studententestsite, niet de officiële site van Tribelt.") {
		t.Error("the footer note is still there beside the test-site bar")
	}
}

func TestAPageOffersAgentsItsTextVersions(t *testing.T) {
	_, doc := productPage(t)
	contains(t, doc,
		`<nav aria-label="Voor agents">`,
		`<li><a href="/llms.txt" type="text/plain">llms.txt</a></li>`,
		`<li><a href="/transportbanden/draadogenbanden.md" type="text/markdown">Markdown-versie</a></li>`,
		`<li><a href="/sitemap.xml" type="application/xml">Sitemap</a></li>`,
	)
}

func TestASignedOutVisitorIsOfferedTheSignIn(t *testing.T) {
	_, doc := productPage(t)
	contains(t, doc, `href="/auth/login?next=/stats" rel="nofollow" aria-label="Inloggen"`)
}

func TestAStatsViewerGetsTheStatsButtonInsteadOfTheSignIn(t *testing.T) {
	res, _ := productPage(t)
	signedIn := string(bytes.Join(res.Parts("HIT", true, true), nil))
	if !strings.Contains(signedIn, `<a class="auth" href="/stats" rel="nofollow" aria-label="Statistieken">`) || strings.Contains(signedIn, "/auth/login") {
		t.Fatal("no stats button, or a sign-in link")
	}
}

func TestNoPlaceholderIsLeftInABuiltPage(t *testing.T) {
	if _, doc := productPage(t); strings.Contains(doc, AuthPlaceholder) || strings.Contains(doc, HitPlaceholder) {
		t.Fatal("a placeholder is left")
	}
}

func TestAProductPageDescribesTheProductAndAnswersInStructuredData(t *testing.T) {
	_, doc := productPage(t)
	var ld struct {
		Graph []map[string]any `json:"@graph"`
	}
	if err := json.Unmarshal([]byte(ldRe.FindStringSubmatch(doc)[1]), &ld); err != nil {
		t.Fatal(err)
	}
	prod := ld.Graph[len(ld.Graph)-1]
	props, _ := prod["additionalProperty"].([]any)
	if prod["@type"] != "Product" || len(props) != 2 || prod["material"] != "1.4301, 1.4404" {
		t.Fatalf("product LD %v", prod)
	}
	faq := ld.Graph[2]["mainEntity"].([]any)[0].(map[string]any)["acceptedAnswer"].(map[string]any)["text"]
	if faq != "Ja, tot hoge temperaturen. Zie ook tribelt.nl." {
		t.Fatalf("FAQ answer is not plain text: %q", faq)
	}
}

func TestABuiltPageKnowsItsPageLocaleAndFormat(t *testing.T) {
	if res, _ := productPage(t); res.PageID != "transportbanden--draadogenbanden" || res.Locale != "nl" || res.Format != "html" {
		t.Fatalf("resource meta %+v", res)
	}
}

func TestBuildAlternatesAndNoindex(t *testing.T) {
	b := buildFixture(t)
	home := html(b.HTML["/de"])
	if !strings.Contains(home, `hreflang="x-default" href="https://mirror.test/"`) || !strings.Contains(home, `<html lang="de-DE">`) {
		t.Fatal("home group has x-default and de-DE")
	}
	vacancy := html(b.HTML["/en/vacancies/cnc-verspaner"])
	if strings.Contains(vacancy, `rel="alternate" hreflang=`) || !strings.Contains(vacancy, `<meta name="robots" content="noindex, follow">`) {
		t.Fatal("noindex page without alternates")
	}
	if strings.Contains(string(b.Files["/sitemap.xml"].Body), "cnc-verspaner") {
		t.Fatal("noindex page stays out of the sitemap")
	}
	if strings.Contains(string(b.Files["/llms.txt"].Body), "cnc-verspaner") {
		t.Fatal("noindex page stays out of llms.txt")
	}
	nf := html(b.NotFound["en"])
	if !strings.Contains(nf, "Page not found") || !strings.Contains(nf, `noindex`) || !strings.Contains(nf, `href="/en"`) {
		t.Fatal("English 404")
	}
}

func TestBuildAgentFiles(t *testing.T) {
	b := buildFixture(t)
	robots := string(b.Files["/robots.txt"].Body)
	for _, want := range []string{
		"User-agent: GPTBot\nAllow: /\nDisallow: /stats\nDisallow: /auth\nDisallow: /go\nDisallow: /b\n",
		"User-agent: ClaudeBot", "User-agent: *", "Sitemap: https://mirror.test/sitemap.xml",
	} {
		if !strings.Contains(robots, want) {
			t.Errorf("robots lacks %q", want)
		}
	}
	llms := string(b.Files["/llms.txt"].Body)
	for _, want := range []string{"# Tribelt (student mirror of tribelt.nl)", "## English", "### Products", "(https://mirror.test/index.md)", "(https://mirror.test/transportbanden/draadogenbanden.md)"} {
		if !strings.Contains(llms, want) {
			t.Errorf("llms.txt lacks %q", want)
		}
	}
	full := string(b.Files["/llms-full.txt"].Body)
	if strings.Count(full, "\n---\n") != 7 || !strings.Contains(full, "# Draadogenbanden") {
		t.Fatalf("llms-full.txt has every indexable page: %d", strings.Count(full, "\n---\n"))
	}
	md := string(b.Markdown["/"].Body)
	if !strings.Contains(md, "[draadogenbanden](https://mirror.test/transportbanden/draadogenbanden)") {
		t.Fatalf("twin links are absolute:\n%s", md)
	}
	twin := string(b.Markdown["/transportbanden/draadogenbanden"].Body)
	for _, want := range []string{"| Breedte | 40–6500 mm |", "### Is een draadogenband geschikt voor ovens?", "[Offerte aanvragen op tribelt.nl](https://www.tribelt.nl/contact)"} {
		if !strings.Contains(twin, want) {
			t.Errorf("twin lacks %q", want)
		}
	}
	sm := string(b.Files["/sitemap.xml"].Body)
	if !strings.Contains(sm, "<lastmod>2026-09-30</lastmod>") || !strings.Contains(sm, `hreflang="de-DE" href="https://mirror.test/de"`) {
		t.Fatal("sitemap lastmod and alternates")
	}
	if b.Assets["/images/product.webp"].ContentType != "image/webp" || b.Assets["/static/beacon.js"] == nil || b.Assets["/images/logo.svg"].ContentType != "image/svg+xml" {
		t.Fatal("assets")
	}
	if !strings.HasPrefix(b.AssetURL("/static/site.css"), "/static/site.css?v=") || b.AssetURL("/nope") != "/nope" {
		t.Fatal("asset URLs")
	}
	if TwinPath("/") != "/index.md" || TwinPath("/en") != "/en.md" {
		t.Fatal("twin paths")
	}
	if !b.GoAllowed("https://tribelt.nl/x") || b.GoAllowed("https://tribelt.nl.evil.example/") || b.GoAllowed("http://www.tribelt.nl/") {
		t.Fatal("outbound allowlist")
	}
}

func TestBuildLastModFromDatabase(t *testing.T) {
	c := loadFixture(t)
	opts := buildOpts(t, os.DirFS("testdata/content/images"))
	opts.LastMod = func(p *Page) time.Time {
		if p.Path == "/" {
			return time.Date(2026, 1, 2, 3, 0, 0, 0, time.UTC)
		}
		return time.Time{}
	}
	b, err := Build(c, opts)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b.Files["/sitemap.xml"].Body), "<loc>https://mirror.test/</loc>\n    <lastmod>2026-01-02</lastmod>") {
		t.Fatal("lastmod from LastMod")
	}
}

func TestABrokenTemplateFailsTheBuild(t *testing.T) {
	c := loadFixture(t)
	for _, tc := range []struct{ name, template, says string }{
		{"no hit placeholder", `{{define "page"}}no placeholder{{end}}{{define "notfound"}}{{end}}`, "placeholder"},
		{"no auth placeholder", `{{define "page"}}{{.Hit}}{{end}}{{define "notfound"}}x{{end}}`, "auth placeholder"},
		{"no auth button", `{{define "page"}}{{.Hit}}{{.Auth}}{{end}}`, "authbutton"},
		{"no 404 placeholder", `{{define "page"}}{{.Hit}}{{.Auth}}{{end}}{{define "authbutton"}}b{{end}}{{define "notfound"}}x{{end}}`, "404"},
		{"an execution error", `{{define "page"}}{{.Nope}}{{end}}`, ""},
		{"a parse error", `{{`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := buildOpts(t, os.DirFS("testdata/content/images"))
			opts.Templates = fstest.MapFS{"public.html": {Data: []byte(tc.template)}}
			if _, err := Build(c, opts); err == nil || !strings.Contains(err.Error(), tc.says) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestAMissingImageFailsTheBuild(t *testing.T) {
	if _, err := Build(loadFixture(t), buildOpts(t, fstest.MapFS{})); err == nil || !strings.Contains(err.Error(), "image") {
		t.Fatalf("err = %v", err)
	}
}

func TestStaticFilesWithoutSiteCSSStillBuild(t *testing.T) {
	opts := buildOpts(t, os.DirFS("testdata/content/images"))
	opts.Static = fstest.MapFS{"x.css": {Data: []byte("a")}}
	if _, err := Build(loadFixture(t), opts); err != nil {
		t.Fatalf("err = %v", err)
	}
}

const designCredit = "- id: home\n  src: img/home.webp\n  alt: {nl: Hal, en: Hall, de: Halle}\n  credit: \"Foto: Tribelt\"\n"

// withDesign builds the fixture with a design hero image, described by credits.
func withDesign(t *testing.T, credits string) (*Built, error) {
	t.Helper()
	webp, err := fs.ReadFile(os.DirFS("testdata/content/images"), "product.webp")
	if err != nil {
		t.Fatal(err)
	}
	opts := buildOpts(t, os.DirFS("testdata/content/images"))
	opts.Static = fstest.MapFS{"site.css": {Data: []byte("a{}")}, "img/home.webp": {Data: webp}, "img/home-480.webp": {Data: webp}, "img/credits.yml": {Data: []byte(credits)}}
	return Build(loadFixture(t), opts)
}

func TestADesignImageIsTheHeroWithItsSizesAndAlt(t *testing.T) {
	b, err := withDesign(t, designCredit)
	if err != nil {
		t.Fatal(err)
	}
	contains(t, html(b.HTML["/en"]),
		`<img class="hero-img" src="/static/img/home.webp" srcset="/static/img/home-480.webp 480w, /static/img/home.webp 480w"`,
		`alt="Hall"`,
	)
}

func TestTheCreditsListIsNotServed(t *testing.T) {
	b, err := withDesign(t, designCredit)
	if err != nil {
		t.Fatal(err)
	}
	if b.Assets["/static/img/credits.yml"] != nil {
		t.Fatal("served")
	}
}

func TestABrokenCreditsListFailsTheBuild(t *testing.T) {
	for name, yml := range map[string]string{
		"missing file": strings.Replace(designCredit, "img/home.webp", "img/nope.webp", 1),
		"missing alt":  strings.Replace(designCredit, "de: Halle", "de: \"\"", 1),
		"unknown key":  designCredit + "  nope: x\n",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := withDesign(t, yml); err == nil || !strings.Contains(err.Error(), "credits.yml") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

// sample is a page with every marker, split into its parts.
func sample(t *testing.T) *Resource {
	t.Helper()
	segs, err := split([]byte("a" + HumanStart + "b" + HitPlaceholder + HumanEnd + "c" + AuthPlaceholder + HumanStart + "d" + HumanEnd))
	if err != nil {
		t.Fatal(err)
	}
	return &Resource{segments: segs, SignedOut: []byte("[out]"), SignedIn: []byte("[in]")}
}

func TestASignedOutHumanGetsTheHumanPartsTheHitAndTheSignIn(t *testing.T) {
	if got := string(bytes.Join(sample(t).Parts("HIT", false, true), nil)); got != "abHITc[out]d" {
		t.Fatalf("Parts = %q", got)
	}
}

func TestASignedInVisitorWhoIsNotHumanGetsNoHumanPartsButTheirButton(t *testing.T) {
	if got := string(bytes.Join(sample(t).Parts("HIT", true, false), nil)); got != "ac[in]" {
		t.Fatalf("Parts = %q", got)
	}
}

func TestAPageWithMissingOrUnpairedMarkersCannotBeSplit(t *testing.T) {
	for _, bad := range []string{
		"no placeholders",
		HitPlaceholder,
		AuthPlaceholder,
		HitPlaceholder + AuthPlaceholder + HumanStart,
		HitPlaceholder + AuthPlaceholder + HumanEnd,
		HitPlaceholder + AuthPlaceholder + HumanStart + HumanStart + HumanEnd,
	} {
		t.Run(bad, func(t *testing.T) {
			if _, err := split([]byte(bad)); err == nil {
				t.Fatal("split")
			}
		})
	}
}
