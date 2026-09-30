package stats

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/oauth2/google"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

// PerfRow is one Search Performance figure: one page, one query, one day, one source.
type PerfRow = queries.UpsertSearchPerformanceParams

// Source fetches Search Performance for a date range.
type Source interface {
	Name() string
	Fetch(ctx context.Context, from, to time.Time) ([]PerfRow, error)
}

// GSC reads the Search Console searchAnalytics API with a service account.
type GSC struct {
	HTTP    *http.Client // authorised client
	BaseURL string       // https://www.googleapis.com
	SiteURL string       // property, e.g. sc-domain:jorisjonkers.dev or https://tribelt.jorisjonkers.dev/
}

// NewGSC builds an authorised client from service account JSON.
func NewGSC(ctx context.Context, serviceAccountJSON []byte, siteURL string) (*GSC, error) {
	cfg, err := google.JWTConfigFromJSON(serviceAccountJSON, "https://www.googleapis.com/auth/webmasters.readonly")
	if err != nil {
		return nil, fmt.Errorf("gsc: service account: %w", err)
	}
	return &GSC{HTTP: cfg.Client(ctx), BaseURL: "https://www.googleapis.com", SiteURL: siteURL}, nil
}

// Name implements Source.
func (*GSC) Name() string { return "google" }

// Fetch implements Source, paging through every row.
func (g *GSC) Fetch(ctx context.Context, from, to time.Time) ([]PerfRow, error) {
	endpoint := g.BaseURL + "/webmasters/v3/sites/" + url.PathEscape(g.SiteURL) + "/searchAnalytics/query"
	const limit = 25000
	var out []PerfRow
	for start := 0; ; start += limit {
		body, _ := json.Marshal(map[string]any{
			"startDate": from.Format(time.DateOnly), "endDate": to.Format(time.DateOnly),
			"dimensions": []string{"date", "page", "query"}, "rowLimit": limit, "startRow": start, "dataState": "all",
		})
		var resp struct {
			Rows []struct {
				Keys        []string `json:"keys"`
				Clicks      float64  `json:"clicks"`
				Impressions float64  `json:"impressions"`
				CTR         float64  `json:"ctr"`
				Position    float64  `json:"position"`
			} `json:"rows"`
		}
		if err := doJSON(ctx, g.HTTP, http.MethodPost, endpoint, body, &resp); err != nil {
			return nil, fmt.Errorf("gsc: %w", err)
		}
		for _, r := range resp.Rows {
			if len(r.Keys) != 3 {
				continue
			}
			d, err := time.Parse(time.DateOnly, r.Keys[0])
			if err != nil {
				continue
			}
			out = append(out, PerfRow{
				Source: "google", Day: d, Page: r.Keys[1], Path: pathOf(r.Keys[1]), Query: r.Keys[2],
				Clicks: int64(r.Clicks), Impressions: int64(r.Impressions), Ctr: r.CTR, Position: r.Position,
			})
		}
		if len(resp.Rows) < limit {
			return out, nil
		}
	}
}

// Bing reads the Bing Webmaster API with an API key: pages first, then each page's queries.
type Bing struct {
	HTTP    *http.Client
	BaseURL string // https://ssl.bing.com
	SiteURL string
	APIKey  string
}

// Name implements Source.
func (*Bing) Name() string { return "bing" }

type bingStat struct {
	Query                 string  `json:"Query"`
	Clicks                int64   `json:"Clicks"`
	Impressions           int64   `json:"Impressions"`
	AvgImpressionPosition float64 `json:"AvgImpressionPosition"`
	Date                  string  `json:"Date"`
}

var bingDate = regexp.MustCompile(`/Date\((-?\d+)`)

func parseBingDate(s string) (time.Time, bool) {
	m := bingDate.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, false
	}
	ms, _ := strconv.ParseInt(m[1], 10, 64)
	t := time.UnixMilli(ms).UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), true
}

func (b *Bing) call(ctx context.Context, method string, params url.Values) ([]bingStat, error) {
	params.Set("siteUrl", b.SiteURL)
	params.Set("apikey", b.APIKey)
	var resp struct {
		D []bingStat `json:"d"`
	}
	if err := doJSON(ctx, b.HTTP, http.MethodGet, b.BaseURL+"/webmaster/api.svc/json/"+method+"?"+params.Encode(), nil, &resp); err != nil {
		return nil, fmt.Errorf("bing %s: %w", method, err)
	}
	return resp.D, nil
}

// Fetch implements Source.
func (b *Bing) Fetch(ctx context.Context, from, to time.Time) ([]PerfRow, error) {
	pages, err := b.call(ctx, "GetPageStats", url.Values{})
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []PerfRow
	for _, p := range pages {
		if seen[p.Query] {
			continue
		}
		seen[p.Query] = true
		stats, err := b.call(ctx, "GetPageQueryStats", url.Values{"page": {p.Query}})
		if err != nil {
			return nil, err
		}
		for _, s := range stats {
			d, ok := parseBingDate(s.Date)
			if !ok || d.Before(from) || d.After(to) {
				continue
			}
			var ctr float64
			if s.Impressions > 0 {
				ctr = float64(s.Clicks) / float64(s.Impressions)
			}
			out = append(out, PerfRow{
				Source: "bing", Day: d, Page: p.Query, Path: pathOf(p.Query), Query: s.Query,
				Clicks: s.Clicks, Impressions: s.Impressions, Ctr: ctr, Position: s.AvgImpressionPosition,
			})
		}
	}
	return out, nil
}

func pathOf(page string) string {
	u, err := url.Parse(page)
	if err != nil || u.Path == "" {
		return "/"
	}
	return u.Path
}

func doJSON(ctx context.Context, client *http.Client, method, endpoint string, body []byte, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("status %d: %s", resp.StatusCode, snippet)
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(out)
}

// Upserter stores Search Performance rows.
type Upserter interface {
	UpsertSearchPerformance(ctx context.Context, arg queries.UpsertSearchPerformanceParams) error
}

// Importer pulls every Source once a day in-process.
type Importer struct {
	Sources []Source
	Store   Upserter
	Log     *slog.Logger
	Now     func() time.Time
	// Lookback is how many days each run re-imports; search data settles over a few days.
	Lookback int
}

// ImportOnce fetches and stores the last Lookback days from every source.
func (im *Importer) ImportOnce(ctx context.Context) error {
	to := im.Now().UTC()
	from := to.AddDate(0, 0, -im.Lookback)
	var errs []error
	for _, src := range im.Sources {
		rows, err := src.Fetch(ctx, from, to)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		for _, r := range rows {
			if err := im.Store.UpsertSearchPerformance(ctx, r); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", src.Name(), err))
				break
			}
		}
		im.Log.Info("search performance imported", "source", src.Name(), "rows", len(rows))
	}
	return errors.Join(errs...)
}

// Run imports after firstDelay and then every interval until ctx ends.
func (im *Importer) Run(ctx context.Context, firstDelay, interval time.Duration) {
	timer := time.NewTimer(firstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := im.ImportOnce(ctx); err != nil {
				im.Log.Error("search performance import failed", "error", err)
			}
			timer.Reset(interval)
		}
	}
}
