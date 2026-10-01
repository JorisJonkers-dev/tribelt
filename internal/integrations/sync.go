package integrations

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

// Sync imports now (Search Performance, Cloudflare counts) or, for IndexNow, submits every Mirror Page.
// Only one run per Integration at a time, across processes.
func (m *Manager) Sync(ctx context.Context, a *Actor, kind Kind) (string, error) {
	r, err := m.resolve(ctx, kind)
	if err != nil {
		return "", err
	}
	if kind == IndexNow {
		return m.notify(ctx, a, r, m.PageURLs(), "every Mirror Page")
	}
	if err := m.runnable(r); err != nil {
		m.event(ctx, kind, "sync", a, err, nil, nil, "")
		m.recordError(ctx, kind, err)
		return "", err
	}
	release, err := m.claim(ctx, kind)
	if err != nil {
		return "", err
	}
	var n int64
	switch kind {
	case Google, Bing:
		n, err = m.syncSearch(ctx, r)
	case Cloudflare:
		n, err = m.syncCloudflare(ctx, r)
	case IndexNow:
	}
	release(err, ptr(m.Now().Add(syncEvery)))
	m.event(ctx, kind, "sync", a, err, &n, nil, plural(int(n), "row", "rows"))
	if err != nil {
		return "", err
	}
	return plural(int(n), "row", "rows") + " imported", nil
}

func (m *Manager) runnable(r resolved) error {
	switch {
	case r.source == SourceNone:
		return Fail(CodeNotConfigured)
	case r.err != nil:
		return r.err
	case r.target == "":
		return Fail(CodeNoTarget)
	}
	return nil
}

// claim takes the per-kind run lease; the returned func records the outcome and frees it.
func (m *Manager) claim(ctx context.Context, kind Kind) (func(error, *time.Time), error) {
	if err := m.Q.EnsureIntegration(ctx, string(kind)); err != nil {
		return nil, err
	}
	_, err := m.Q.ClaimSync(ctx, queries.ClaimSyncParams{Now: m.Now(), Kind: string(kind), StaleBefore: m.Now().Add(-staleClaim)})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, Fail(CodeBusy)
	}
	if err != nil {
		return nil, err
	}
	return func(runErr error, next *time.Time) {
		var msg, code *string
		if runErr != nil {
			c := CodeOf(runErr)
			msg, code = ptr(c.Message()), ptr(string(c))
		}
		if err := m.Q.FinishSync(context.WithoutCancel(ctx), queries.FinishSyncParams{
			Ok: runErr == nil, LastError: msg, LastErrorCode: code, NextSyncAt: next, Kind: string(kind),
		}); err != nil {
			m.Log.Error("integration status not recorded", "kind", kind, "error", err)
		}
	}, nil
}

func day(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func later(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

// since is where a run starts: a first run backfills to the launch or the API's oldest day, whichever
// is later; later runs re-import the last few days.
func (m *Manager) since(r resolved, oldest time.Time, lookback int) time.Time {
	now := m.Now()
	if r.hasRow && r.row.BackfilledAt != nil {
		return later(day(m.Launch), day(now).AddDate(0, 0, -lookback))
	}
	return later(day(m.Launch), day(oldest))
}

func (m *Manager) syncSearch(ctx context.Context, r resolved) (int64, error) {
	now := m.Now()
	var rows []PerfRow
	var err error
	if r.kind == Google {
		// Search Console keeps sixteen months.
		var g *GSC
		if g, err = NewGSC(ctx, m.HTTP, r.secret, m.Endpoints.Google, r.target); err == nil {
			rows, err = g.Fetch(ctx, m.since(r, now.AddDate(0, -16, 0), lookbackDays), now)
		}
	} else {
		// Bing Webmaster keeps six months.
		b := &BingClient{HTTP: m.HTTP, BaseURL: m.Endpoints.Bing, SiteURL: r.target, APIKey: string(r.secret)}
		rows, err = b.Fetch(ctx, m.since(r, now.AddDate(0, -6, 0), lookbackDays), now)
	}
	if err != nil {
		return 0, err
	}
	for _, row := range rows {
		if err := m.Q.UpsertSearchPerformance(ctx, row); err != nil {
			return 0, err
		}
	}
	return int64(len(rows)), nil
}

// cfRun is one Cloudflare import: the client, the plan's limits and the dimensions found missing.
type cfRun struct {
	m           *Manager
	cf          *CloudflareClient
	zone, host  string
	caps        Caps
	limit       int64
	unavailable map[string]bool
}

// syncCloudflare imports every complete UTC day in the window, one query per day and dimension.
// A dimension the plan does not offer is skipped and listed, so the UI can say which.
func (m *Manager) syncCloudflare(ctx context.Context, r resolved) (int64, error) {
	run := &cfRun{
		m: m, cf: &CloudflareClient{HTTP: m.HTTP, BaseURL: m.Endpoints.Cloudflare, Token: string(r.secret)},
		zone: r.target, host: m.Host(), limit: 10000, unavailable: map[string]bool{},
	}
	meta := r.meta
	if meta.Caps == nil {
		if caps, err := run.cf.Settings(ctx, r.target); err == nil {
			meta.Caps = &caps
		}
	}
	if meta.Caps != nil {
		run.caps = *meta.Caps
	}
	if run.caps.MaxPageSize > 0 {
		run.limit = min(run.limit, run.caps.MaxPageSize)
	}
	yesterday := day(m.Now()).AddDate(0, 0, -1)
	var n int64
	for d := m.since(r, m.Now().Add(-run.caps.Window()).AddDate(0, 0, 1), 3); !d.After(yesterday); d = d.AddDate(0, 0, 1) {
		got, err := run.day(ctx, d)
		n += got
		if err != nil {
			return n, err
		}
	}
	meta.Unavailable = nil
	for _, dim := range Dimensions() {
		if run.unavailable[dim.Name] {
			meta.Unavailable = append(meta.Unavailable, dim.Name)
		}
	}
	m.saveMeta(ctx, Cloudflare, meta)
	return n, nil
}

func (run *cfRun) day(ctx context.Context, d time.Time) (int64, error) {
	var n int64
	for _, dim := range Dimensions() {
		if run.unavailable[dim.Name] {
			continue
		}
		if !run.caps.Has(dim.Field) {
			run.unavailable[dim.Name] = true
			continue
		}
		groups, err := run.cf.Count(ctx, run.zone, run.host, d, dim, run.limit)
		if err != nil {
			if dim.Name != "total" && CodeOf(err) == CodeForbidden {
				run.m.Log.Info("cloudflare dimension unavailable on this plan", "dimension", dim.Field, "error", err)
				run.unavailable[dim.Name] = true
				continue
			}
			return n, err
		}
		for _, g := range groups {
			if err := run.m.Q.UpsertCloudflareDaily(ctx, queries.UpsertCloudflareDailyParams{
				Host: run.host, Day: d, Dimension: dim.Name, Value: g.Value, Requests: g.Requests,
			}); err != nil {
				return n, err
			}
			n++
		}
	}
	return n, nil
}

func (m *Manager) keyLocation(key string) string { return m.BaseURL + "/" + key + ".txt" }

// NotifyChanged submits the Mirror Pages a new Content Release changed, when IndexNow is connected.
func (m *Manager) NotifyChanged(ctx context.Context, release string, urls []string) {
	r, err := m.resolve(ctx, IndexNow)
	if err != nil || !r.ready() || len(urls) == 0 {
		return
	}
	if _, err := m.notify(ctx, nil, r, urls, "Content Release "+release); err != nil {
		m.Log.Warn("indexnow release notification failed", "release", release, "code", CodeOf(err))
	}
}

// notify submits urls in protocol batches and logs each batch.
func (m *Manager) notify(ctx context.Context, a *Actor, r resolved, urls []string, reason string) (string, error) {
	if err := m.runnable(r); err != nil {
		m.event(ctx, IndexNow, "notify", a, err, nil, nil, reason)
		return "", err
	}
	release, err := m.claim(ctx, IndexNow)
	if err != nil {
		return "", err
	}
	client := &IndexNowClient{HTTP: m.HTTP, Endpoint: m.Endpoints.IndexNow}
	key := string(r.secret)
	var sent int64
	var status int
	var runErr error
	for _, batch := range Batches(urls, m.Host(), BatchSize) {
		status, err = client.Submit(ctx, m.Host(), key, m.keyLocation(key), batch)
		count := int64(len(batch))
		m.event(ctx, IndexNow, "notify", a, err, &count, ptr(int64(status)), reason)
		if err != nil {
			runErr = err
			break
		}
		sent += count
	}
	release(runErr, nil)
	meta := r.meta
	meta.LastCount, meta.LastStatus = sent, status
	m.saveMeta(ctx, IndexNow, meta)
	if runErr != nil {
		return "", runErr
	}
	return fmt.Sprintf("%s submitted (HTTP %d)", plural(int(sent), "URL", "URLs"), status), nil
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// KeyFile serves the IndexNow key at /<key>.txt and passes every other request on. The key file is
// protocol plumbing, not content, so it is never a Hit (README "IndexNow").
func (m *Manager) KeyFile(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			name, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/"), ".txt")
			if ok && ValidIndexNowKey(name) {
				if key := m.currentKey(r.Context()); key != "" && subtle.ConstantTimeCompare([]byte(key), []byte(name)) == 1 {
					w.Header().Set("Content-Type", "text/plain; charset=utf-8")
					w.Header().Set("Cache-Control", "public, max-age=300")
					_, _ = w.Write([]byte(key))
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// currentKey caches the IndexNow key for a minute, so key-shaped misses cost no query each.
func (m *Manager) currentKey(ctx context.Context) string {
	now := m.Now()
	if e := m.key.Load(); e != nil && now.Before(e.until) {
		return e.key
	}
	key := ""
	r, err := m.resolve(ctx, IndexNow)
	if err != nil {
		m.Log.Error("indexnow key unreadable", "error", err)
	} else if r.source == SourceUI && r.err == nil {
		key = string(r.secret)
	}
	m.key.Store(&keyEntry{key: key, until: now.Add(time.Minute)})
	return key
}

// ChangedURLs are the absolute URLs of pages whose content hash differs from the previous release,
// including pages added or removed.
func ChangedURLs(baseURL string, before, after map[string]string) []string {
	var out []string
	for path, hash := range after {
		if before[path] != hash {
			out = append(out, baseURL+path)
		}
	}
	for path := range before {
		if _, ok := after[path]; !ok {
			out = append(out, baseURL+path)
		}
	}
	slices.Sort(out)
	return out
}
