package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"mime"
	"path"
	"strings"
	"time"

	"golang.org/x/image/webp"
)

// HitPlaceholder marks where the per-request Hit id goes in pre-rendered HTML.
const HitPlaceholder = "__TRIBELT_HIT__"

// AuthPlaceholder marks where the per-request sign-in or stats button goes in pre-rendered HTML.
const AuthPlaceholder = "__TRIBELT_AUTH__"

// Resource is one pre-rendered public response.
type Resource struct {
	Path        string
	PageID      string
	Locale      string
	Format      string // html | md | txt | xml
	ContentType string
	// Body is the whole response; for HTML it is split around the Hit and auth placeholders into
	// Head, Mid and Tail, with the two prepared buttons for the auth slot.
	Body                []byte
	Head, Mid, Tail     []byte
	SignedOut, SignedIn []byte
	ETag                string
}

// Parts are the pieces of an HTML response: the page with this request's Hit id and header button.
func (r *Resource) Parts(hit string, signedIn bool) [][]byte {
	button := r.SignedOut
	if signedIn {
		button = r.SignedIn
	}
	return [][]byte{r.Head, []byte(hit), r.Mid, button, r.Tail}
}

// Asset is a static file served with a long cache lifetime.
type Asset struct {
	ContentType string
	Body        []byte
	ETag        string
}

// Built is every public resource, rendered once at startup.
type Built struct {
	Content   *Content
	HTML      map[string]*Resource // page path -> HTML
	Markdown  map[string]*Resource // page path -> Markdown twin
	Files     map[string]*Resource // /robots.txt, /sitemap.xml, /llms.txt, /llms-full.txt
	NotFound  map[string]*Resource // locale -> 404 page
	Assets    map[string]*Asset    // /static/... and /images/...
	BaseURL   string
	GoAllowed func(string) bool
}

// Options are the inputs Build needs besides the content itself.
type Options struct {
	Templates fs.FS // web/templates
	Static    fs.FS // web/static
	Images    fs.FS // content/images
	// LastMod returns when a page's current text was first published; zero means unknown.
	LastMod func(p *Page) time.Time
	Now     time.Time
}

type imageInfo struct{ width, height int }

type builder struct {
	c      *Content
	opts   Options
	tmpl   *template.Template
	assets map[string]*Asset
	images map[string]imageInfo
	design map[string]designImage
	fonts  []string
	css    string
	beacon string
	notice string
}

// Build renders every page, twin, agent file and 404 page into memory.
func Build(c *Content, opts Options) (*Built, error) {
	b := &builder{c: c, opts: opts, assets: map[string]*Asset{}, images: map[string]imageInfo{}}
	if err := b.loadAssets(); err != nil {
		return nil, err
	}
	tmpl, err := template.New("public").Funcs(template.FuncMap{"join": strings.Join}).ParseFS(opts.Templates, "public.html")
	if err != nil {
		return nil, fmt.Errorf("content: templates: %w", err)
	}
	b.tmpl = tmpl
	out := &Built{
		Content: c, HTML: map[string]*Resource{}, Markdown: map[string]*Resource{}, Files: map[string]*Resource{},
		NotFound: map[string]*Resource{}, Assets: b.assets, BaseURL: c.Site.BaseURL, GoAllowed: isOfficialURL,
	}
	md := markdown()
	for _, p := range c.Pages {
		view, err := b.pageView(md, p)
		if err != nil {
			return nil, fmt.Errorf("content: %s: %w", p.File, err)
		}
		res, err := b.html("page", view, p.Path, p.ID, p.Locale)
		if err != nil {
			return nil, fmt.Errorf("content: %s: %w", p.File, err)
		}
		out.HTML[p.Path] = res
		out.Markdown[p.Path] = textResource(p.Path, p.ID, p.Locale, "md", "text/markdown; charset=utf-8", []byte(b.markdownTwin(p)))
	}
	for _, l := range LocaleOrder() {
		res, err := b.html("notfound", b.notFoundView(l), "", "", l)
		if err != nil {
			return nil, fmt.Errorf("content: 404 %s: %w", l, err)
		}
		out.NotFound[l] = res
	}
	out.Files["/robots.txt"] = textResource("/robots.txt", "", "", "txt", "text/plain; charset=utf-8", []byte(b.robots()))
	out.Files["/llms.txt"] = textResource("/llms.txt", "", "", "txt", "text/plain; charset=utf-8", []byte(b.llms()))
	out.Files["/llms-full.txt"] = textResource("/llms-full.txt", "", "", "txt", "text/plain; charset=utf-8", []byte(b.llmsFull()))
	sm, err := b.sitemap()
	if err != nil {
		return nil, err
	}
	out.Files["/sitemap.xml"] = textResource("/sitemap.xml", "", "", "xml", "application/xml; charset=utf-8", sm)
	return out, nil
}

func textResource(p, id, locale, format, ctype string, body []byte) *Resource {
	return &Resource{Path: p, PageID: id, Locale: locale, Format: format, ContentType: ctype, Body: body, ETag: etag(body)}
}

func etag(b []byte) string {
	s := sha256.Sum256(b)
	return `"` + hex.EncodeToString(s[:8]) + `"`
}

func (b *builder) html(name string, view any, p, id, locale string) (*Resource, error) {
	var buf bytes.Buffer
	if err := b.tmpl.ExecuteTemplate(&buf, name, view); err != nil {
		return nil, err
	}
	head, rest, ok := bytes.Cut(buf.Bytes(), []byte(HitPlaceholder))
	if !ok {
		return nil, fmt.Errorf("template %s lacks the hit placeholder", name)
	}
	mid, tail, ok := bytes.Cut(rest, []byte(AuthPlaceholder))
	if !ok {
		return nil, fmt.Errorf("template %s lacks the auth placeholder", name)
	}
	out, in, err := b.authButtons(locale)
	if err != nil {
		return nil, err
	}
	return &Resource{
		Path: p, PageID: id, Locale: locale, Format: "html", ContentType: "text/html; charset=utf-8",
		Head: head, Mid: mid, Tail: tail, SignedOut: out, SignedIn: in,
	}, nil
}

// authButton is the header link that becomes "Stats" for a signed-in Stats Viewer.
type authButton struct{ Href, Label string }

func (b *builder) authButtons(locale string) (signedOut, signedIn []byte, err error) {
	ui := uiStrings(locale)
	var out, in bytes.Buffer
	if err := b.tmpl.ExecuteTemplate(&out, "authbutton", authButton{Href: "/auth/login?next=/stats", Label: ui.SignIn}); err != nil {
		return nil, nil, err
	}
	if err := b.tmpl.ExecuteTemplate(&in, "authbutton", authButton{Href: "/stats", Label: ui.Stats}); err != nil {
		return nil, nil, err
	}
	return out.Bytes(), in.Bytes(), nil
}

func (b *builder) loadAssets() error {
	err := fs.WalkDir(b.opts.Static, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if path.Ext(p) == ".yml" {
			return nil
		}
		raw, err := fs.ReadFile(b.opts.Static, p)
		if err != nil {
			return err
		}
		b.addAsset("/static/"+strings.TrimPrefix(p, "static/"), raw)
		return nil
	})
	if err != nil {
		return fmt.Errorf("content: static: %w", err)
	}
	for name := range b.c.Images {
		switch path.Ext(name) {
		case ".webp", ".svg", ".png", ".jpg", ".jpeg", ".avif":
		default:
			continue
		}
		raw, err := fs.ReadFile(b.opts.Images, name)
		if err != nil {
			return fmt.Errorf("content: image %s: %w", name, err)
		}
		b.addAsset("/images/"+name, raw)
	}
	b.versionCSSURLs("/static/site.css")
	b.css = b.versioned("/static/site.css")
	b.beacon = b.versioned("/static/beacon.js")
	b.notice = b.versioned("/static/notice.js")
	for _, f := range []string{"/static/fonts/outfit.woff2", "/static/fonts/geist-500.woff2"} {
		if b.assets[f] != nil {
			b.fonts = append(b.fonts, b.versioned(f))
		}
	}
	return b.loadDesign()
}

func (b *builder) addAsset(p string, raw []byte) {
	b.assets[p] = newAsset(p, raw)
	if strings.HasSuffix(p, ".webp") {
		if cfg, err := webp.DecodeConfig(bytes.NewReader(raw)); err == nil {
			b.images[p] = imageInfo{cfg.Width, cfg.Height}
		}
	}
}

// versionCSSURLs points a stylesheet's url(/static/...) references at their cache-busting URLs.
func (b *builder) versionCSSURLs(css string) {
	a, ok := b.assets[css]
	if !ok {
		return
	}
	body := string(a.Body)
	for p := range b.assets {
		if strings.HasPrefix(p, "/static/") && p != css {
			body = strings.ReplaceAll(body, "url("+p+")", "url("+b.versioned(p)+")")
		}
	}
	b.assets[css] = newAsset(css, []byte(body))
}

func newAsset(name string, raw []byte) *Asset {
	ctype := mime.TypeByExtension(path.Ext(name))
	switch path.Ext(name) {
	case ".webp":
		ctype = "image/webp"
	case ".svg":
		ctype = "image/svg+xml"
	case ".js":
		ctype = "text/javascript; charset=utf-8"
	case ".css":
		ctype = "text/css; charset=utf-8"
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	return &Asset{ContentType: ctype, Body: raw, ETag: etag(raw)}
}

// versioned appends a content hash so assets can be cached forever.
func (b *builder) versioned(p string) string {
	a, ok := b.assets[p]
	if !ok {
		return p
	}
	return p + "?v=" + strings.Trim(a.ETag, `"`)
}

// AssetURL is the cache-busting URL of a static asset.
func (b *Built) AssetURL(p string) string {
	a, ok := b.Assets[p]
	if !ok {
		return p
	}
	return p + "?v=" + strings.Trim(a.ETag, `"`)
}

func (b *builder) abs(p string) string { return b.c.Site.BaseURL + p }
