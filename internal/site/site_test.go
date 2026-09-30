package site

import (
	"context"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/JorisJonkers-dev/tribelt/internal/content"
	"github.com/JorisJonkers-dev/tribelt/internal/hits"
	"github.com/JorisJonkers-dev/tribelt/web"
)

const fixture = "../content/testdata/content"

func built(t *testing.T) *content.Built {
	t.Helper()
	c, err := content.Load(os.DirFS(fixture))
	if err != nil {
		t.Fatal(err)
	}
	tmpl, _ := fs.Sub(web.Templates, "templates")
	static, _ := fs.Sub(web.Static, "static")
	b, err := content.Build(c, content.Options{Templates: tmpl, Static: static, Images: os.DirFS(fixture + "/images"), Now: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// serve runs the handler the way the tracking middleware would, returning the recorded Info.
func serve(h http.Handler, method, target string, header map[string]string) (*httptest.ResponseRecorder, *hits.Info) {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	var info *hits.Info
	rec := httptest.NewRecorder()
	capture := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info = hits.InfoFrom(r.Context())
		h.ServeHTTP(w, r)
	})
	hits.WithInfo(capture).ServeHTTP(rec, req)
	return rec, info
}

func TestPages(t *testing.T) {
	h := &Handler{Built: built(t)}
	cases := []struct {
		name, method, target string
		header               map[string]string
		status               int
		ctype, location      string
		format, pageID, loc  string
		skip                 bool
		body                 string
	}{
		{"html page", "GET", "/transportbanden/draadogenbanden", nil, 200, "text/html; charset=utf-8", "", "html", "transportbanden--draadogenbanden", "nl", false, "<h1>Draadogenbanden</h1>"},
		{"home", "GET", "/", nil, 200, "text/html; charset=utf-8", "", "html", "home", "nl", false, `data-hit="`},
		{"markdown twin", "GET", "/en/conveyor-belts/eye-link-belts.md", nil, 200, "text/markdown; charset=utf-8", "", "md", "transportbanden--draadogenbanden", "en", false, "# Eye-link belts"},
		{"home twin", "GET", "/index.md", nil, 200, "text/markdown; charset=utf-8", "", "md", "home", "nl", false, "# Metalen transportbanden op maat"},
		{"accept markdown", "GET", "/de", map[string]string{"Accept": "text/markdown"}, 200, "text/markdown; charset=utf-8", "", "md", "home", "de", false, "# Metallförderbänder nach Maß"},
		{"accept html wins", "GET", "/de", map[string]string{"Accept": "text/html,text/markdown;q=0.9"}, 200, "text/html; charset=utf-8", "", "html", "home", "de", false, ""},
		{"robots", "GET", "/robots.txt", nil, 200, "text/plain; charset=utf-8", "", "txt", "", "", false, "User-agent: GPTBot"},
		{"llms", "GET", "/llms.txt", nil, 200, "text/plain; charset=utf-8", "", "txt", "", "", false, "# Tribelt"},
		{"sitemap", "GET", "/sitemap.xml", nil, 200, "application/xml; charset=utf-8", "", "xml", "", "", false, "<urlset"},
		{"official 301", "GET", "/transportbanden?utm_source=x", nil, 301, "text/html; charset=utf-8", "/metalen-transportbanden?utm_source=x", "html", "", "nl", false, ""},
		{"trailing slash", "GET", "/en/", nil, 301, "text/html; charset=utf-8", "/en", "html", "", "en", false, ""},
		{"trailing slash keeps hyphen", "GET", "/metalen-transportbanden/", nil, 301, "text/html; charset=utf-8", "/metalen-transportbanden", "html", "", "nl", false, ""},
		{"backslash stays 404", "GET", "/%5Cevil.example/", nil, 404, "text/html; charset=utf-8", "", "html", "", "nl", false, ""},
		{"404 nl", "GET", "/bestaat-niet", nil, 404, "text/html; charset=utf-8", "", "html", "", "nl", false, "Pagina niet gevonden"},
		{"404 de", "GET", "/de/fehlt", nil, 404, "text/html; charset=utf-8", "", "html", "", "de", false, "Seite nicht gefunden"},
		{"unknown twin", "GET", "/fehlt.md", nil, 404, "text/html; charset=utf-8", "", "html", "", "nl", false, ""},
		{"head", "HEAD", "/", nil, 200, "text/html; charset=utf-8", "", "html", "home", "nl", false, ""},
		{"head twin", "HEAD", "/index.md", nil, 200, "text/markdown; charset=utf-8", "", "md", "home", "nl", false, ""},
		{"post", "POST", "/", nil, 405, "text/plain; charset=utf-8", "", "html", "", "", true, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec, info := serve(h, tc.method, tc.target, tc.header)
			if rec.Code != tc.status || rec.Header().Get("Content-Type") != tc.ctype || rec.Header().Get("Location") != tc.location {
				t.Fatalf("got %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Header().Get("Location"))
			}
			if info.Format != tc.format || info.PageID != tc.pageID || info.Locale != tc.loc || info.Skip != tc.skip {
				t.Fatalf("info %+v", info)
			}
			if !strings.Contains(rec.Body.String(), tc.body) {
				t.Fatalf("body lacks %q", tc.body)
			}
			if tc.method == "HEAD" && rec.Body.Len() != 0 {
				t.Fatal("HEAD has no body")
			}
		})
	}
}

func TestHitIDInjected(t *testing.T) {
	h := &Handler{Built: built(t)}
	rec, info := serve(h, "GET", "/", nil)
	if !strings.Contains(rec.Body.String(), `data-hit="`+info.HitID.String()+`"`) || info.HitID == uuid.Nil {
		t.Fatal("hit id must be in the page")
	}
	if rec.Header().Get("Cache-Control") != "no-cache" || rec.Header().Get("Vary") != "Accept" {
		t.Fatalf("headers %v", rec.Header())
	}
	if got := rec.Header().Get("Content-Length"); got != "" && got != strconv.Itoa(rec.Body.Len()) {
		t.Fatalf("content length %s vs %d", got, rec.Body.Len())
	}
}

func TestTwinHeadersAndETag(t *testing.T) {
	b := built(t)
	h := &Handler{Built: b}
	rec, _ := serve(h, "GET", "/index.md", nil)
	if rec.Header().Get("Link") != `<https://mirror.test/>; rel="canonical"` || rec.Header().Get("X-Robots-Tag") != "noindex" {
		t.Fatalf("twin headers %v", rec.Header())
	}
	etag := rec.Header().Get("ETag")
	again, _ := serve(h, "GET", "/index.md", map[string]string{"If-None-Match": etag})
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Fatalf("conditional GET: %d", again.Code)
	}
}

func TestWantsMarkdown(t *testing.T) {
	for accept, want := range map[string]bool{
		"":                                   false,
		"text/markdown":                      true,
		"text/markdown, */*":                 true,
		"text/html":                          false,
		"text/html, text/markdown":           true,
		"text/html;q=1, text/markdown;q=0.5": false,
		"text/markdown;q=0":                  false,
		"text/x-markdown":                    true,
		"text/markdown;q=bad":                true,
		"application/xhtml+xml, text/markdown;q=0.8": false,
		"garbage;;;": false,
	} {
		if WantsMarkdown(accept) != want {
			t.Errorf("WantsMarkdown(%q) != %v", accept, want)
		}
	}
}

func TestAssets(t *testing.T) {
	b := built(t)
	a := Assets(b)
	do := func(method, target, inm string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, nil)
		if inm != "" {
			req.Header.Set("If-None-Match", inm)
		}
		rec := httptest.NewRecorder()
		a.ServeHTTP(rec, req)
		return rec
	}
	rec := do("GET", b.AssetURL("/static/site.css"), "")
	if rec.Code != 200 || rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" || rec.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("versioned css %d %v", rec.Code, rec.Header())
	}
	img := do("GET", "/images/product.webp", "")
	if img.Header().Get("Cache-Control") != "public, max-age=86400" || img.Header().Get("Content-Type") != "image/webp" {
		t.Fatalf("image %v", img.Header())
	}
	if do("GET", "/images/product.webp", img.Header().Get("ETag")).Code != http.StatusNotModified {
		t.Fatal("asset 304")
	}
	if do("HEAD", "/images/product.webp", "").Body.Len() != 0 {
		t.Fatal("HEAD")
	}
	if do("GET", "/images/credits.yml", "").Code != 404 || do("GET", "/static/nope.js", "").Code != 404 {
		t.Fatal("unknown assets 404")
	}
}

func TestInfoWithoutMiddleware(t *testing.T) {
	if hits.InfoFrom(context.Background()) == nil {
		t.Fatal("InfoFrom never returns nil")
	}
}
