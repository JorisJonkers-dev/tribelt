package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEveryAnswerCarriesTheBrowserHardeningHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	Secure(false, http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	h := rec.Header()
	if h.Get("Content-Security-Policy") == "" || h.Get("X-Content-Type-Options") != "nosniff" ||
		h.Get("Permissions-Policy") != "camera=(), microphone=(), geolocation=(), interest-cohort=()" || h.Get("X-Frame-Options") != "DENY" {
		t.Fatalf("headers %v", h)
	}
}

func TestStrictTransportSecurityIsSentOnlyWhenAsked(t *testing.T) {
	for _, hsts := range []bool{true, false} {
		t.Run(fmt.Sprint(hsts), func(t *testing.T) {
			rec := httptest.NewRecorder()
			Secure(hsts, http.NotFoundHandler()).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
			if (rec.Header().Get("Strict-Transport-Security") != "") != hsts {
				t.Fatalf("Strict-Transport-Security = %q", rec.Header().Get("Strict-Transport-Security"))
			}
		})
	}
}

func TestAPanickingHandlerAnswersAPlain500(t *testing.T) {
	rec := httptest.NewRecorder()
	Recover(slog.New(slog.DiscardHandler), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic("x") })).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 500 || rec.Body.String() != "Internal server error\n" {
		t.Fatalf("%d %q", rec.Code, rec.Body)
	}
}

func TestAnAbortedHandlerStillAborts(t *testing.T) {
	defer func() {
		if err, _ := recover().(error); !errors.Is(err, http.ErrAbortHandler) {
			t.Fatal("ErrAbortHandler did not propagate")
		}
	}()
	Recover(slog.New(slog.DiscardHandler), http.HandlerFunc(func(http.ResponseWriter, *http.Request) { panic(http.ErrAbortHandler) })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
}

// probe answers GET path on the health routes, with readiness failing with ready.
func probe(path string, ready error) int {
	mux := http.NewServeMux()
	Health(mux, func(context.Context) error { return ready })
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec.Code
}

func TestAHealthyServiceIsAliveAndReady(t *testing.T) {
	if probe("/healthz", nil) != 200 || probe("/readyz", nil) != 200 {
		t.Fatal("not healthy")
	}
}

func TestLivenessStaysUpWhileReadinessFails(t *testing.T) {
	down := errors.New("down")
	if probe("/healthz", down) != 200 || probe("/readyz", down) != 503 {
		t.Fatal("liveness followed readiness")
	}
}
