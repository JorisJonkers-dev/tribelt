package content

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Load reads site.yml, redirects.yml, pages/ and images/ from fsys (rooted at the content
// directory) and validates everything. It reports every problem at once.
func Load(fsys fs.FS) (*Content, error) {
	c := &Content{ByPath: map[string]*Page{}, Redirects: map[string]string{}, Images: map[string]bool{}}
	h := sha256.New()
	var errs []error

	raw, err := fs.ReadFile(fsys, "site.yml")
	if err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	h.Write(raw)
	if err := decodeStrict(raw, &c.Site); err != nil {
		return nil, fmt.Errorf("content: site.yml: %w", err)
	}
	for code, l := range c.Site.Locales {
		if l != nil {
			l.Code = code
		}
	}

	redirectErrs, err := c.loadRedirects(fsys, h)
	if err != nil {
		return nil, err
	}
	errs = append(errs, redirectErrs...)
	pageErrs, err := c.loadPages(fsys, h)
	if err != nil {
		return nil, err
	}
	errs = append(errs, pageErrs...)

	_ = fs.WalkDir(fsys, "images", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			c.Images[strings.TrimPrefix(p, "images/")] = true
		}
		return nil
	})

	c.Hash = hex.EncodeToString(h.Sum(nil))[:16]
	c.link()
	errs = append(errs, c.validate()...)
	if len(errs) > 0 {
		return nil, &ValidationError{Problems: errs}
	}
	return c, nil
}

func (c *Content) loadRedirects(fsys fs.FS, h io.Writer) ([]error, error) {
	raw, err := fs.ReadFile(fsys, "redirects.yml")
	if err != nil {
		return nil, nil //nolint:nilerr // redirects.yml is optional
	}
	_, _ = h.Write(raw)
	var list []Redirect
	if err := decodeStrict(raw, &list); err != nil {
		return nil, fmt.Errorf("content: redirects.yml: %w", err)
	}
	var errs []error
	for _, r := range list {
		if _, dup := c.Redirects[r.From]; dup {
			errs = append(errs, fmt.Errorf("redirects.yml: duplicate from %s", r.From))
		}
		c.Redirects[r.From] = r.To
	}
	return errs, nil
}

func (c *Content) loadPages(fsys fs.FS, h io.Writer) ([]error, error) {
	files, _ := fs.Glob(fsys, "pages/*/*.md")
	sort.Strings(files)
	var errs []error
	for _, f := range files {
		raw, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, fmt.Errorf("content: %w", err)
		}
		_, _ = h.Write([]byte(f))
		_, _ = h.Write(raw)
		p, err := parsePage(f, raw)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		c.Pages = append(c.Pages, p)
	}
	return errs, nil
}

// ValidationError lists every content problem found.
type ValidationError struct{ Problems []error }

func (e *ValidationError) Error() string {
	msgs := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		msgs[i] = p.Error()
	}
	return fmt.Sprintf("content: %d problem(s):\n  %s", len(msgs), strings.Join(msgs, "\n  "))
}

func decodeStrict(raw []byte, v any) error {
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	return dec.Decode(v)
}

func parsePage(file string, raw []byte) (*Page, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return nil, fmt.Errorf("%s: missing front matter", file)
	}
	fm, body, ok := strings.Cut(text[4:], "\n---\n")
	if !ok {
		if fm2, found := strings.CutSuffix(text[4:], "\n---"); found {
			fm, body, ok = fm2, "", true
		}
	}
	if !ok {
		return nil, fmt.Errorf("%s: unterminated front matter", file)
	}
	p := &Page{}
	if err := decodeStrict([]byte(fm), p); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}
	p.Body = strings.TrimSpace(body)
	p.File = file
	return p, nil
}

// link groups alternates by id and indexes pages by path.
func (c *Content) link() {
	groups := map[string][]*Page{}
	for _, p := range c.Pages {
		groups[p.ID] = append(groups[p.ID], p)
		c.ByPath[p.Path] = p
	}
	order := LocaleOrder()
	for _, g := range groups {
		slices.SortFunc(g, func(a, b *Page) int { return slices.Index(order, a.Locale) - slices.Index(order, b.Locale) })
		for _, p := range g {
			p.Alternates = g
		}
	}
}

// Resolve follows redirects (at most a few hops) to a page.
func (c *Content) Resolve(p string) (*Page, bool) {
	for range 5 {
		if pg, ok := c.ByPath[p]; ok {
			return pg, true
		}
		to, ok := c.Redirects[p]
		if !ok {
			return nil, false
		}
		p = to
	}
	return nil, false
}

var errNoFile = errors.New("missing file")

func imageFile(c *Content, src string) error {
	name := strings.TrimPrefix(path.Clean(src), "images/")
	if !strings.HasPrefix(src, "images/") || !c.Images[name] {
		return errNoFile
	}
	return nil
}
