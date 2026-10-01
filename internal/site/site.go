// Package site serves the pre-rendered public resources: pages, Markdown twins, agent files, 404s.
package site

import (
	"mime"
	"net/http"
	"strconv"
	"strings"

	"github.com/JorisJonkers-dev/tribelt/internal/content"
	"github.com/JorisJonkers-dev/tribelt/internal/hits"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// Handler serves everything in a content.Built.
type Handler struct {
	Built *content.Built
	// Viewer reports a signed-in Stats Viewer, whose header button then links to /stats; nil means never.
	Viewer func(r *http.Request) bool
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	info := hits.InfoFrom(r.Context())
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		info.Skip = true
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	p := r.URL.Path
	info.Locale = content.LocaleOf(p)
	if p != "/" && strings.HasSuffix(p, "/") {
		if to := "/" + strings.Trim(p, "/"); !strings.HasPrefix(to, "/\\") {
			redirect(w, r, to)
			return
		}
	}
	if to, ok := h.Built.Content.Redirects[p]; ok {
		redirect(w, r, to)
		return
	}
	if res, ok := h.Built.Files[p]; ok {
		info.Format, info.Locale = res.Format, ""
		h.serveText(w, r, res)
		return
	}
	if base, ok := strings.CutSuffix(p, ".md"); ok {
		if base == "/index" {
			base = "/"
		}
		if res, ok := h.Built.Markdown[base]; ok {
			info.PageID, info.Locale, info.Format = res.PageID, res.Locale, "md"
			h.serveText(w, r, res)
			return
		}
	}
	if res, ok := h.Built.HTML[p]; ok {
		info.PageID, info.Locale = res.PageID, res.Locale
		w.Header().Add("Vary", "Accept")
		if WantsMarkdown(r.Header.Get("Accept")) {
			info.Format = "md"
			h.serveText(w, r, h.Built.Markdown[p])
			return
		}
		h.serveHTML(w, r, res, info.HitID.String(), http.StatusOK)
		return
	}
	h.serveHTML(w, r, h.Built.NotFound[info.Locale], info.HitID.String(), http.StatusNotFound)
}

func (h *Handler) signedIn(r *http.Request) bool { return h.Viewer != nil && h.Viewer(r) }

func redirect(w http.ResponseWriter, r *http.Request, to string) {
	if r.URL.RawQuery != "" {
		to += "?" + r.URL.RawQuery
	}
	http.Redirect(w, r, to, http.StatusMovedPermanently) //nolint:gosec // target is a same-site path from content or the request path
}

// serveHTML writes a pre-rendered page with this request's Hit id and header button swapped in. The
// test-site bar and pill go only to people browsing; crawlers, agents and curl get the page without.
func (h *Handler) serveHTML(w http.ResponseWriter, r *http.Request, res *content.Resource, hitID string, status int) {
	signedIn := h.signedIn(r)
	parts := res.Parts(hitID, signedIn, visits.Browsing(r.UserAgent(), r.Header.Get("Sec-Fetch-Dest")))
	size := 0
	for _, p := range parts {
		size += len(p)
	}
	hd := w.Header()
	hd.Set("Content-Type", res.ContentType)
	hd.Add("Vary", "User-Agent, Sec-Fetch-Dest")
	hd.Set("Cache-Control", "no-cache")
	if signedIn {
		hd.Set("Cache-Control", "private, no-cache")
	}
	hd.Set("Content-Length", strconv.Itoa(size))
	if status == http.StatusNotFound {
		hd.Set("X-Robots-Tag", "noindex")
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	for _, p := range parts {
		_, _ = w.Write(p) //nolint:gosec // pre-rendered page; the request only picks segments and a generated Hit id
	}
}

func (h *Handler) serveText(w http.ResponseWriter, r *http.Request, res *content.Resource) {
	hd := w.Header()
	hd.Set("Content-Type", res.ContentType)
	hd.Set("Cache-Control", "no-cache")
	hd.Set("ETag", res.ETag)
	if res.Format == "md" {
		hd.Set("X-Robots-Tag", "noindex")
		hd.Set("Link", `<`+h.Built.BaseURL+res.Path+`>; rel="canonical"`)
	}
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, res.ETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	hd.Set("Content-Length", strconv.Itoa(len(res.Body)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(res.Body)
}

// WantsMarkdown reports whether the Accept header prefers text/markdown over text/html.
func WantsMarkdown(accept string) bool {
	md, html := -1.0, -1.0
	for _, part := range strings.Split(accept, ",") {
		mt, params, err := mime.ParseMediaType(strings.TrimSpace(part))
		if err != nil {
			continue
		}
		q := 1.0
		if v, ok := params["q"]; ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		switch mt {
		case "text/markdown", "text/x-markdown":
			md = max(md, q)
		case "text/html", "application/xhtml+xml":
			html = max(html, q)
		}
	}
	return md > 0 && md >= html
}

// Assets serves /static and /images with long-lived caching.
func Assets(b *content.Built) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a, ok := b.Assets[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		hd := w.Header()
		hd.Set("Content-Type", a.ContentType)
		hd.Set("ETag", a.ETag)
		if r.URL.Query().Get("v") != "" {
			hd.Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			hd.Set("Cache-Control", "public, max-age=86400")
		}
		if strings.Contains(r.Header.Get("If-None-Match"), a.ETag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		hd.Set("Content-Length", strconv.Itoa(len(a.Body)))
		if r.Method == http.MethodHead {
			return
		}
		_, _ = w.Write(a.Body)
	})
}
