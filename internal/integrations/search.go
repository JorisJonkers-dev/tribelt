package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

// PerfRow is one Search Performance figure: one page, one query, one day, one source.
type PerfRow = queries.UpsertSearchPerformanceParams

// GSC reads the Search Console API with a service account.
type GSC struct {
	HTTP    *http.Client // authorised client
	BaseURL string       // https://www.googleapis.com
	SiteURL string       // property, e.g. sc-domain:jorisjonkers.dev or https://tribelt.jorisjonkers.dev/
}

// NewGSC builds an authorised client from service account JSON. base carries the HTTP client the
// token exchange uses.
func NewGSC(ctx context.Context, base *http.Client, serviceAccountJSON []byte, baseURL, siteURL string) (*GSC, error) {
	cfg, err := google.JWTConfigFromJSON(serviceAccountJSON, "https://www.googleapis.com/auth/webmasters.readonly")
	if err != nil {
		return nil, &CodedError{Code: CodeIncompleteKey, Err: err}
	}
	ctx = context.WithValue(ctx, oauth2.HTTPClient, base)
	return &GSC{HTTP: cfg.Client(ctx), BaseURL: baseURL, SiteURL: siteURL}, nil
}

// Sites lists the properties the service account can see (sites.list).
func (g *GSC) Sites(ctx context.Context) ([]string, error) {
	var resp struct {
		SiteEntry []struct {
			SiteURL         string `json:"siteUrl"`
			PermissionLevel string `json:"permissionLevel"`
		} `json:"siteEntry"`
	}
	if err := doJSON(ctx, g.HTTP, http.MethodGet, g.BaseURL+"/webmasters/v3/sites", nil, &resp); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.SiteEntry))
	for _, s := range resp.SiteEntry {
		if s.PermissionLevel != "siteUnverifiedUser" {
			out = append(out, s.SiteURL)
		}
	}
	return out, nil
}

// Fetch pages through every searchAnalytics row of [from, to].
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
			return nil, err
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
				Source: string(Google), Day: d, Page: r.Keys[1], Path: pathOf(r.Keys[1]), Query: r.Keys[2],
				Clicks: int64(r.Clicks), Impressions: int64(r.Impressions), Ctr: r.CTR, Position: r.Position,
			})
		}
		if len(resp.Rows) < limit {
			return out, nil
		}
	}
}

// BingClient reads the Bing Webmaster API with an API key: pages first, then each page's queries.
type BingClient struct {
	HTTP    *http.Client
	BaseURL string // https://ssl.bing.com
	SiteURL string
	APIKey  string
}

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

func (b *BingClient) get(ctx context.Context, method string, params url.Values, out any) error {
	params.Set("apikey", b.APIKey)
	return doJSON(ctx, b.HTTP, http.MethodGet, b.BaseURL+"/webmaster/api.svc/json/"+method+"?"+params.Encode(), nil, out)
}

func (b *BingClient) call(ctx context.Context, method string, params url.Values) ([]bingStat, error) {
	params.Set("siteUrl", b.SiteURL)
	var resp struct {
		D []bingStat `json:"d"`
	}
	if err := b.get(ctx, method, params, &resp); err != nil {
		return nil, err
	}
	return resp.D, nil
}

// Sites lists the sites of the key's owner (GetUserSites), verified ones only.
func (b *BingClient) Sites(ctx context.Context) ([]string, error) {
	var resp struct {
		D []struct {
			URL        string `json:"Url"`
			IsVerified bool   `json:"IsVerified"`
		} `json:"d"`
	}
	if err := b.get(ctx, "GetUserSites", url.Values{}, &resp); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(resp.D))
	for _, s := range resp.D {
		if s.IsVerified {
			out = append(out, s.URL)
		}
	}
	return out, nil
}

// Fetch returns every page/query/day figure within [from, to].
func (b *BingClient) Fetch(ctx context.Context, from, to time.Time) ([]PerfRow, error) {
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
				Source: string(Bing), Day: d, Page: p.Query, Path: pathOf(p.Query), Query: s.Query,
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

// doJSON sends one request and decodes a 200 answer; any other status becomes a CodedError whose
// detail (a snippet of the provider's answer) is for the logs only.
func doJSON(ctx context.Context, client *http.Client, method, endpoint string, body []byte, out any) error {
	return doJSONAuth(ctx, client, method, endpoint, "", body, out)
}

func doJSONAuth(ctx context.Context, client *http.Client, method, endpoint, bearer string, body []byte, out any) error {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, rd)
	if err != nil {
		return &CodedError{Code: CodeInternal, Err: fmt.Errorf("%s request", method)}
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		var re *oauth2.RetrieveError
		if errors.As(err, &re) {
			return &CodedError{Code: CodeAuth, Err: fmt.Errorf("token exchange: %s", re.ErrorCode)}
		}
		return redact(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return &CodedError{Code: statusCode(resp.StatusCode), Status: resp.StatusCode, Err: fmt.Errorf("status %d: %s", resp.StatusCode, snippet)}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(out); err != nil {
		return &CodedError{Code: CodeBadResponse, Status: resp.StatusCode, Err: err}
	}
	return nil
}
