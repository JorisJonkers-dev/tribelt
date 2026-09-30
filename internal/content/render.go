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

// Resource is one pre-rendered public response.
type Resource struct {
	Path        string
	PageID      string
	Locale      string
	Format      string // html | md | txt | xml
	ContentType string
	// Body is the whole response; for HTML it is split around the Hit placeholder into Head and Tail.
	Body       []byte
	Head, Tail []byte
	ETag       string
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
	css    string
	beacon string
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
	head, tail, ok := bytes.Cut(buf.Bytes(), []byte(HitPlaceholder))
	if !ok {
		return nil, fmt.Errorf("template %s lacks the hit placeholder", name)
	}
	return &Resource{Path: p, PageID: id, Locale: locale, Format: "html", ContentType: "text/html; charset=utf-8", Head: head, Tail: tail}, nil
}

func (b *builder) loadAssets() error {
	err := fs.WalkDir(b.opts.Static, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := fs.ReadFile(b.opts.Static, p)
		if err != nil {
			return err
		}
		b.assets["/static/"+strings.TrimPrefix(p, "static/")] = newAsset(p, raw)
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
		b.assets["/images/"+name] = newAsset(name, raw)
		if strings.HasSuffix(name, ".webp") {
			if cfg, err := webp.DecodeConfig(bytes.NewReader(raw)); err == nil {
				b.images[name] = imageInfo{cfg.Width, cfg.Height}
			}
		}
	}
	b.css = b.versioned("/static/site.css")
	b.beacon = b.versioned("/static/beacon.js")
	return nil
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
