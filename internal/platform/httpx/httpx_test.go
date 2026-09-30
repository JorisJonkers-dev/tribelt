package httpx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecureHeaders(t *testing.T) {
	for _, hsts := range []bool{true, false} {
		rec := httptest.NewRecorder()
		Secure(hsts, http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		h := rec.Header()
		if h.Get("Content-Security-Policy") == "" || h.Get("X-Content-Type-Options") != "nosniff" || (h.Get("Strict-Transport-Security") != "") != hsts {
			t.Fatalf("headers %v", h)
		}
	}
}

func TestRecover(t *testing.T) {
	rec := httptest.NewRecorder()
	Recover(slog.New(slog.DiscardHandler), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("x") })).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 || rec.Body.String() != "Internal server error\n" {
		t.Fatalf("%d %q", rec.Code, rec.Body)
	}
	defer func() {
		if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Fatal("ErrAbortHandler propagates")
		}
	}()
	Recover(slog.New(slog.DiscardHandler), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

func TestHealth(t *testing.T) {
	var fail error
	mux := http.NewServeMux()
	Health(mux, func(context.Context) error { return fail })
	get := func(p string) int {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest("GET", p, nil))
		return rec.Code
	}
	if get("/healthz") != 200 || get("/readyz") != 200 {
		t.Fatal("healthy")
	}
	fail = errors.New("down")
	if get("/healthz") != 200 || get("/readyz") != 503 {
		t.Fatal("liveness stays up while readiness fails")
	}
}
