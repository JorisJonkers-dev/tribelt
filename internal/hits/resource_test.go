package hits

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHitRecordsResourceAndAppVersion(t *testing.T) {
	tr := newTracker(t)
	serve := func(h http.Handler, target string) (*httptest.ResponseRecorder, Op) {
		req := httptest.NewRequest("GET", target, nil)
		req.Header.Set("User-Agent", chrome)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var op Op
		select {
		case op = <-tr.Recorder.ch:
		default:
		}
		return rec, op
	}
	text := func(format string, status int) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			InfoFrom(r.Context()).Format = format
			w.WriteHeader(status)
		})
	}
	cases := []struct {
		target, format string
		status         int
		resource       string
	}{
		{"/sectoren", "html", 200, "page"},
		{"/sectoren.md", "md", 200, "markdown"},
		{"/robots.txt", "txt", 200, "robots"},
		{"/llms.txt", "txt", 304, "llms"},
		{"/llms-full.txt", "txt", 200, "llms_full"},
		{"/sitemap.xml", "xml", 200, "sitemap"},
		{"/transportbanden", "html", 301, "redirect"},
		{"/wp-login.php", "html", 404, "not_found"},
	}
	for _, c := range cases {
		_, op := serve(tr.Middleware(text(c.format, c.status)), c.target)
		if op.Hit == nil || op.Hit.Resource != c.resource || op.Hit.AppVersion != "0.3.0" || op.Hit.ReleaseLabel != "v1-test" {
			t.Errorf("%s: %+v", c.target, op.Hit)
		}
	}
	if _, op := serve(tr.Middleware(text("html", 204)), "/b"); op.Hit != nil {
		t.Fatal("the beacon path is never a Hit")
	}

	img := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("webp")) })
	rec, op := serve(tr.Images(img), "/images/home-960.webp")
	if op.Hit == nil || op.Hit.Resource != "image" || op.Hit.Format != "img" || op.Hit.PageID != nil || op.Hit.VisitorID != nil {
		t.Fatalf("image hit %+v", op.Hit)
	}
	if visitorCookie(rec) != nil {
		t.Fatal("an image response never sets the Visitor ID cookie")
	}
	if _, op = serve(tr.Images(http.NotFoundHandler()), "/images/nope.webp"); op.Hit.Resource != "not_found" {
		t.Fatalf("missing image %+v", op.Hit)
	}
}

func TestOutboundClickRecordsAppVersion(t *testing.T) {
	tr := newTracker(t)
	req := httptest.NewRequest("GET", "/go?to=https%3A%2F%2Fwww.tribelt.nl%2Fcontact&from=%2Fsectoren", nil)
	tr.Go(func(string) bool { return true }).ServeHTTP(httptest.NewRecorder(), req)
	if c := (<-tr.Recorder.ch).Click; c.AppVersion != "0.3.0" || c.ReleaseLabel != "v1-test" {
		t.Fatalf("click %+v", c)
	}
}
