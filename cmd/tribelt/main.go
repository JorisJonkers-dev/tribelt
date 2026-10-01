// Command tribelt serves the Tribelt mirror: `tribelt serve` (default) or `tribelt migrate`.
package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/JorisJonkers-dev/tribelt"
	"github.com/JorisJonkers-dev/tribelt/internal/content"
	"github.com/JorisJonkers-dev/tribelt/internal/hits"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/config"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/httpx"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/oidc"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg"
	"github.com/JorisJonkers-dev/tribelt/internal/platform/session"
	"github.com/JorisJonkers-dev/tribelt/internal/release"
	"github.com/JorisJonkers-dev/tribelt/internal/site"
	"github.com/JorisJonkers-dev/tribelt/internal/stats"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
	"github.com/JorisJonkers-dev/tribelt/web"
)

// version is set at build time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(os.Args[1:], logger); err != nil {
		logger.Error("tribelt stopped", "error", err)
		os.Exit(1)
	}
}

func run(args []string, logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd := "serve"
	if len(args) > 0 {
		cmd = args[0]
	}
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	switch cmd {
	case "migrate":
		return migrate(ctx, cfg, logger)
	case "serve":
		return serve(ctx, cfg, logger)
	default:
		return fmt.Errorf("unknown command %q (want serve or migrate)", cmd)
	}
}

// migrate retries while the database comes up (compose, pod start).
func migrate(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	var err error
	for attempt := range 30 {
		if err = pg.Migrate(ctx, cfg.DatabaseURL); err == nil {
			logger.Info("migrations applied")
			return nil
		}
		logger.Warn("migrate: database not ready", "attempt", attempt+1, "error", err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return err
}

func contentFS(cfg config.Config) (fs.FS, error) {
	if cfg.ContentDir != "" {
		return os.DirFS(cfg.ContentDir), nil
	}
	return fs.Sub(tribelt.Content, "content")
}

func sub(fsys fs.FS, dir string) fs.FS {
	s, _ := fs.Sub(fsys, dir)
	return s
}

func serve(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	cfs, err := contentFS(cfg)
	if err != nil {
		return err
	}
	c, err := content.Load(cfs)
	if err != nil {
		return err
	}
	logger.Info("content loaded", "release", c.Site.Release.Label, "pages", len(c.Pages), "hash", c.Hash, "version", version)
	if cfg.AutoMigrate {
		if err := migrate(ctx, cfg, logger); err != nil {
			return err
		}
	}
	store, err := pg.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer store.Close()
	lastMod, err := release.Sync(ctx, store.Q(), c)
	if err != nil {
		return err
	}
	built, err := content.Build(c, content.Options{
		Templates: sub(web.Templates, "templates"), Static: sub(web.Static, "static"), Images: sub(cfs, "images"),
		LastMod: func(p *content.Page) time.Time { return lastMod[p.Path] }, Now: time.Now(),
	})
	if err != nil {
		return err
	}
	gate, statsOn, err := newGate(ctx, cfg, logger)
	if err != nil {
		return err
	}
	recorder := hits.NewRecorder(hits.PGWriter{DB: store.Pool(), Log: logger}, logger, 10000)
	tracker, err := newTracker(cfg, c, recorder, gate, statsOn)
	if err != nil {
		return err
	}
	mux := http.NewServeMux()
	httpx.Health(mux, func(context.Context) error { return nil })
	assets := site.Assets(built)
	mux.Handle("GET /static/", assets)
	mux.Handle("GET /images/", assets)
	mux.HandleFunc("POST /b", tracker.Beacon)
	mux.HandleFunc("GET /go", tracker.Go(built.GoAllowed))
	pages := &site.Handler{Built: built}
	if statsOn {
		pages.Viewer = func(r *http.Request) bool { _, ok := gate.Viewer(r); return ok }
	}
	mux.Handle("/", tracker.Middleware(pages))
	searchOn := startSearchImport(ctx, cfg, store, logger)
	if statsOn {
		if err := mountStats(mux, gate, store, c, built, searchOn, logger); err != nil {
			return err
		}
	} else {
		logger.Warn("stats disabled: set OIDC_* or DEV_AUTH_BYPASS=1 (non-production only)")
	}
	return listen(ctx, cfg, httpx.Recover(logger, httpx.Secure(cfg.HTTPS(), mux)), recorder, logger)
}

func newTracker(cfg config.Config, c *content.Content, rec *hits.Recorder, gate oidc.Gate, statsOn bool) (*hits.Tracker, error) {
	ranges, err := visits.BundledRanges()
	if err != nil {
		return nil, err
	}
	t := &hits.Tracker{
		Recorder: rec, Ranges: ranges, Hasher: visits.NewDailyHasher([]byte(cfg.VisitorHMACKey)),
		Release: c.Site.Release.Label, Host: cfg.Host(), SecureCookies: cfg.HTTPS(), Now: time.Now, Rand: hits.DefaultRand(),
		Resolve: func(p string) (string, string, bool) {
			page, ok := c.ByPath[p]
			if !ok {
				return "", "", false
			}
			return page.ID, page.Locale, true
		},
	}
	if statsOn {
		t.Internal = func(r *http.Request) bool { _, ok := gate.Viewer(r); return ok }
	}
	return t, nil
}

func mountStats(mux *http.ServeMux, gate oidc.Gate, store *pg.Store, c *content.Content, built *content.Built, searchOn bool, logger *slog.Logger) error {
	svc := &stats.Service{
		Q: store.Q(), DB: store.Pool(), Log: logger, Now: time.Now, Release: c.Site.Release.Label,
		Title: func(p string) string {
			if page, ok := c.ByPath[p]; ok {
				return page.Title
			}
			return ""
		},
		ViewerName:    func(r *http.Request) string { v, _ := gate.Viewer(r); return v.Name },
		SearchEnabled: searchOn, CSS: built.AssetURL("/static/stats.css"), ChartJS: built.AssetURL("/static/chart.js"),
	}
	if err := svc.Init(sub(web.Templates, "templates")); err != nil {
		return err
	}
	gate.Routes(mux)
	svc.Routes(mux, gate.Require)
	return nil
}

// listen serves until ctx ends, then drains the hit buffer.
func listen(ctx context.Context, cfg config.Config, h http.Handler, recorder *hits.Recorder, logger *slog.Logger) error {
	recCtx, stopRecorder := context.WithCancel(context.WithoutCancel(ctx))
	recDone := make(chan struct{})
	go func() { recorder.Run(recCtx); close(recDone) }()
	srv := &http.Server{
		Addr: cfg.Addr, Handler: h,
		ReadHeaderTimeout: 10 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 5 * time.Minute, IdleTimeout: 2 * time.Minute,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Info("serving", "addr", cfg.Addr, "base_url", cfg.BaseURL)
	var err error
	select {
	case err = <-errc:
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		err = srv.Shutdown(shutdownCtx)
		cancel()
	}
	stopRecorder()
	<-recDone
	logger.Info("stopped", "hits_written", recorder.Written(), "hits_dropped", recorder.Dropped())
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// newGate picks OIDC, the development bypass, or no stats at all (enabled=false).
func newGate(ctx context.Context, cfg config.Config, logger *slog.Logger) (gate oidc.Gate, enabled bool, err error) {
	switch {
	case cfg.OIDCConfigured():
		codec, err := session.NewCodec(cfg.SessionKey)
		if err != nil {
			return nil, false, err
		}
		a, err := oidc.New(ctx, oidc.Config{
			Issuer: cfg.OIDCIssuer, ClientID: cfg.OIDCClientID, ClientSecret: cfg.OIDCClientSecret,
			RedirectURL: cfg.OIDCRedirectURL, PostLogoutURL: cfg.BaseURL + "/",
		}, codec, logger)
		return a, err == nil, err
	case cfg.DevAuthBypass && !cfg.Production:
		logger.Warn("DEV_AUTH_BYPASS=1: /stats is open to anyone who can reach this process")
		return oidc.DevBypass{}, true, nil
	default:
		return oidc.DevBypass{}, false, nil
	}
}

func startSearchImport(ctx context.Context, cfg config.Config, store *pg.Store, logger *slog.Logger) bool {
	var sources []stats.Source
	if cfg.GSCServiceAccountJSON != "" && cfg.GSCSiteURL != "" {
		gsc, err := stats.NewGSC(ctx, []byte(cfg.GSCServiceAccountJSON), cfg.GSCSiteURL)
		if err != nil {
			logger.Error("search console import disabled", "error", err)
		} else {
			sources = append(sources, gsc)
		}
	}
	if cfg.BingAPIKey != "" && cfg.BingSiteURL != "" {
		sources = append(sources, &stats.Bing{HTTP: &http.Client{Timeout: time.Minute}, BaseURL: "https://ssl.bing.com", SiteURL: cfg.BingSiteURL, APIKey: cfg.BingAPIKey})
	}
	if len(sources) == 0 {
		logger.Info("search performance import disabled: no credentials")
		return false
	}
	im := &stats.Importer{Sources: sources, Store: store.Q(), Log: logger, Now: time.Now, Lookback: 10}
	go im.Run(ctx, time.Minute, 24*time.Hour)
	return true
}
