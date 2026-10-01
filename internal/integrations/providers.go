package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// IndexNowClient submits URL batches to an IndexNow endpoint.
type IndexNowClient struct {
	HTTP     *http.Client
	Endpoint string // https://api.indexnow.org/indexnow
}

// BatchSize is the protocol's maximum URLs per request.
const BatchSize = 10000

// Submit posts one batch and returns the HTTP status; 200 and 202 are accepted.
func (c *IndexNowClient) Submit(ctx context.Context, host, key, keyLocation string, urls []string) (int, error) {
	body, _ := json.Marshal(map[string]any{"host": host, "key": key, "keyLocation": keyLocation, "urlList": urls})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint, bytes.NewReader(body))
	if err != nil {
		return 0, &CodedError{Code: CodeInternal, Err: err}
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, redact(err)
	}
	defer func() { _ = resp.Body.Close() }()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusAccepted {
		return resp.StatusCode, &CodedError{Code: statusCode(resp.StatusCode), Status: resp.StatusCode, Err: fmt.Errorf("status %d: %s", resp.StatusCode, snippet)}
	}
	return resp.StatusCode, nil
}

// Batches keeps the URLs on host (the protocol refuses others), drops duplicates and splits them
// into batches of at most size.
func Batches(urls []string, host string, size int) [][]string {
	seen := map[string]bool{}
	var keep []string
	for _, raw := range urls {
		u, err := url.Parse(raw)
		if err != nil || !strings.EqualFold(u.Hostname(), host) || (u.Scheme != "https" && u.Scheme != "http") || seen[raw] {
			continue
		}
		seen[raw] = true
		keep = append(keep, raw)
	}
	var out [][]string
	for len(keep) > 0 {
		n := min(size, len(keep))
		out = append(out, keep[:n])
		keep = keep[n:]
	}
	return out
}

// CloudflareClient reads the Cloudflare API with an API token (Zone → Analytics: Read is enough).
type CloudflareClient struct {
	HTTP    *http.Client
	BaseURL string // https://api.cloudflare.com
	Token   string
}

type cfEnvelope struct {
	Success bool            `json:"success"`
	Result  json.RawMessage `json:"result"`
}

func (c *CloudflareClient) rest(ctx context.Context, path string, out any) error {
	var env cfEnvelope
	if err := doJSONAuth(ctx, c.HTTP, http.MethodGet, c.BaseURL+"/client/v4"+path, c.Token, nil, &env); err != nil {
		return err
	}
	if !env.Success {
		return &CodedError{Code: CodeBadResponse, Err: fmt.Errorf("%s: success=false", path)}
	}
	if err := json.Unmarshal(env.Result, out); err != nil {
		return &CodedError{Code: CodeBadResponse, Err: err}
	}
	return nil
}

// Verify checks the token is active (/user/tokens/verify).
func (c *CloudflareClient) Verify(ctx context.Context) error {
	var r struct {
		Status string `json:"status"`
	}
	if err := c.rest(ctx, "/user/tokens/verify", &r); err != nil {
		return err
	}
	if r.Status != "active" {
		return &CodedError{Code: CodeAuth, Err: fmt.Errorf("token status %q", r.Status)}
	}
	return nil
}

// Zone is what the mirror shows of a zone.
type Zone struct {
	Name string
	Plan string
}

// Zone reads one zone, which proves the token may see it.
func (c *CloudflareClient) Zone(ctx context.Context, id string) (Zone, error) {
	var r struct {
		Name string `json:"name"`
		Plan struct {
			Name string `json:"name"`
		} `json:"plan"`
	}
	if err := c.rest(ctx, "/zones/"+url.PathEscape(id), &r); err != nil {
		return Zone{}, err
	}
	return Zone{Name: r.Name, Plan: r.Plan.Name}, nil
}

// Caps is what the zone's plan allows on httpRequestsAdaptiveGroups (the GraphQL settings node).
type Caps struct {
	Enabled      bool     `json:"enabled"`
	MaxDuration  int64    `json:"maxDuration"`
	NotOlderThan int64    `json:"notOlderThan"`
	MaxPageSize  int64    `json:"maxPageSize"`
	Fields       []string `json:"availableFields"`
}

// Has reports whether a field is available; an unknown field list allows everything, and the
// import then learns from the API's answer.
func (c Caps) Has(field string) bool {
	if len(c.Fields) == 0 {
		return true
	}
	for _, f := range c.Fields {
		if f == field {
			return true
		}
		if strings.HasSuffix(f, field) {
			b := f[len(f)-len(field)-1]
			if !isAlnum(b) {
				return true
			}
		}
	}
	return false
}

// Window is how far back the plan reads, defaulting to eight days when the settings are unknown.
func (c Caps) Window() time.Duration {
	if c.NotOlderThan <= 0 {
		return 8 * 24 * time.Hour
	}
	return time.Duration(c.NotOlderThan) * time.Second
}

// graphQLError marks a GraphQL error answer, typically a field the plan does not offer.
type graphQLError struct{ messages []string }

func (e *graphQLError) Error() string { return "graphql: " + strings.Join(e.messages, "; ") }

func (c *CloudflareClient) graphql(ctx context.Context, query string, vars map[string]any, out any) error {
	body, _ := json.Marshal(map[string]any{"query": query, "variables": vars})
	var resp struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := doJSONAuth(ctx, c.HTTP, http.MethodPost, c.BaseURL+"/client/v4/graphql", c.Token, body, &resp); err != nil {
		return err
	}
	if len(resp.Errors) > 0 {
		ge := &graphQLError{}
		for _, e := range resp.Errors {
			ge.messages = append(ge.messages, e.Message)
		}
		return &CodedError{Code: CodeForbidden, Err: ge}
	}
	if err := json.Unmarshal(resp.Data, out); err != nil {
		return &CodedError{Code: CodeBadResponse, Err: err}
	}
	return nil
}

const settingsQuery = `query ($zone: string!) { viewer { zones(filter: {zoneTag: $zone}) { settings {
  httpRequestsAdaptiveGroups { enabled maxDuration maxPageSize notOlderThan availableFields } } } } }`

// Settings reads the plan's limits for httpRequestsAdaptiveGroups.
func (c *CloudflareClient) Settings(ctx context.Context, zone string) (Caps, error) {
	var data struct {
		Viewer struct {
			Zones []struct {
				Settings struct {
					Groups Caps `json:"httpRequestsAdaptiveGroups"`
				} `json:"settings"`
			} `json:"zones"`
		} `json:"viewer"`
	}
	if err := c.graphql(ctx, settingsQuery, map[string]any{"zone": zone}, &data); err != nil {
		return Caps{}, err
	}
	if len(data.Viewer.Zones) == 0 {
		return Caps{}, Fail(CodeNotFound)
	}
	return data.Viewer.Zones[0].Settings.Groups, nil
}

// Dimension is one breakdown of the edge counts: its stored name and its GraphQL field.
type Dimension struct{ Name, Field, Label string }

// Dimensions lists every breakdown the import tries, total first.
func Dimensions() []Dimension {
	return []Dimension{
		{"total", "date", "Requests per day"},
		{"user_agent", "userAgent", "User agent"},
		{"path", "clientRequestPath", "Request path"},
		{"status", "edgeResponseStatus", "Edge response status"},
		{"bot_category", "verifiedBotCategory", "Verified bot category"},
	}
}

// Group is one counted value of a dimension.
type Group struct {
	Value    string
	Requests int64
}

// Count reads one UTC day's requests for host, grouped by one dimension.
func (c *CloudflareClient) Count(ctx context.Context, zone, host string, day time.Time, d Dimension, limit int64) ([]Group, error) {
	query := `query ($zone: string!, $filter: ZoneHttpRequestsAdaptiveGroupsFilter_InputObject, $limit: uint64!) {
  viewer { zones(filter: {zoneTag: $zone}) {
    httpRequestsAdaptiveGroups(filter: $filter, limit: $limit, orderBy: [count_DESC]) { count dimensions { ` + d.Field + ` } } } } }`
	vars := map[string]any{
		"zone": zone, "limit": limit,
		"filter": map[string]any{"date": day.Format(time.DateOnly), "clientRequestHTTPHost": host},
	}
	var data struct {
		Viewer struct {
			Zones []struct {
				Groups []struct {
					Count      int64          `json:"count"`
					Dimensions map[string]any `json:"dimensions"`
				} `json:"httpRequestsAdaptiveGroups"`
			} `json:"zones"`
		} `json:"viewer"`
	}
	if err := c.graphql(ctx, query, vars, &data); err != nil {
		return nil, err
	}
	if len(data.Viewer.Zones) == 0 {
		return nil, Fail(CodeNotFound)
	}
	var out []Group
	for _, g := range data.Viewer.Zones[0].Groups {
		v := "all"
		if d.Name != "total" {
			v = fmt.Sprint(g.Dimensions[d.Field])
			if raw, ok := g.Dimensions[d.Field].(float64); ok {
				v = fmt.Sprintf("%.0f", raw)
			}
		}
		out = append(out, Group{Value: visits.Clean(v, 512), Requests: g.Count})
	}
	return out, nil
}

func isAlnum(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
