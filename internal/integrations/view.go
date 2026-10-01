package integrations

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// Card is one Integration on the Integrations page. It never carries a secret.
type Card struct {
	Kind                 Kind
	Title, Status, Label string // Status: connected, error, none, vault, setup, paused
	Source               Source
	// Shadowed is a UI credential overriding a Vault-managed one.
	Shadowed, VaultSet, Enabled, Running   bool
	EnvVars                                []string
	Account, Fingerprint, Target           string
	SavedBy, SavedAt, ChangedBy, ChangedAt string
	LastSync, LastSuccess, NextSync        string
	Error, ErrorCode                       string
	Rows                                   string
	// Choices are the properties or sites the credential can see, for an admin to pick from.
	Choices    []string
	ChoicesErr string
	Events     []Event
	// IndexNow
	KeyURL, LastSubmission string
	// Cloudflare
	Zone, Plan string
	Dims       []Dim
	Window     string
}

// Dim is one Cloudflare breakdown and whether the zone's plan offers it.
type Dim struct {
	Label, Field string
	Available    bool
	Known        bool
}

// Event is one audit log row as shown.
type Event struct {
	At, Action, Actor, Detail, Code, Status string
	OK                                      bool
}

// View is the Integrations page.
type View struct {
	Cards   []Card
	Admin   bool
	Storage bool
	Host    string
	Compare *Compare
}

// CompareDay is one UTC day of edge requests against origin Hits.
type CompareDay struct {
	Day                          string
	Edge, EdgeComparable, Origin int64
	EdgeAI, OriginAI             int64
	HasComparable, HasAI         bool
}

// CompareBot is one AI agent seen at the edge, at the origin, or both.
type CompareBot struct {
	Name         string
	Edge, Origin int64
	// Missing is how many edge requests never reached the origin as a Hit.
	Missing int64
}

// Compare is the edge-versus-origin panel.
type Compare struct {
	Days             []CompareDay
	Bots             []CompareBot
	Blocked          int64
	HasUA, HasStatus bool
	Range            string
}

// Title is the provider name shown for a kind.
func Title(k Kind) string {
	switch k {
	case Google:
		return "Google Search Console"
	case Bing:
		return "Bing Webmaster Tools"
	case IndexNow:
		return "IndexNow"
	case Cloudflare:
		return "Cloudflare analytics"
	}
	return string(k)
}

func envVars(k Kind) []string {
	switch k {
	case Google:
		return []string{"GSC_SERVICE_ACCOUNT_JSON", "GSC_SITE_URL"}
	case Bing:
		return []string{"BING_API_KEY", "BING_SITE_URL"}
	case IndexNow, Cloudflare:
	}
	return nil
}

func amsterdam() *time.Location {
	loc, _ := time.LoadLocation("Europe/Amsterdam")
	return loc
}

func stamp(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.In(amsterdam()).Format("2 Jan 2006 15:04")
}

// View gathers the page. Only an admin gets the audit log and the live list of properties to pick.
func (m *Manager) View(ctx context.Context, admin bool, pick Kind) (*View, error) {
	v := &View{Admin: admin, Storage: m.Sealer != nil, Host: m.Host()}
	counts, err := m.counts(ctx)
	if err != nil {
		return nil, err
	}
	for _, k := range Kinds() {
		r, err := m.resolve(ctx, k)
		if err != nil {
			return nil, err
		}
		c := m.card(r)
		c.Rows = counts[k]
		if admin {
			m.choices(ctx, r, pick, &c)
			if c.Events, err = m.events(ctx, k); err != nil {
				return nil, err
			}
		}
		v.Cards = append(v.Cards, c)
	}
	if v.Compare, err = m.compare(ctx); err != nil {
		return nil, err
	}
	return v, nil
}

// counts is what each Integration has stored, as shown on its card.
func (m *Manager) counts(ctx context.Context) (map[Kind]string, error) {
	out := map[Kind]string{}
	rows, err := m.Q.SearchRowCounts(ctx)
	if err != nil {
		return nil, err
	}
	n := map[string]int64{}
	for _, r := range rows {
		n[r.Source] = r.N
	}
	out[Google] = plural(int(n[string(Google)]), "Search Performance row", "Search Performance rows")
	out[Bing] = plural(int(n[string(Bing)]), "Search Performance row", "Search Performance rows")
	cf, err := m.Q.CloudflareRowCount(ctx, m.Host())
	if err != nil {
		return nil, err
	}
	out[Cloudflare] = plural(int(cf), "edge count row", "edge count rows")
	urls, err := m.Q.NotifiedURLCount(ctx)
	if err != nil {
		return nil, err
	}
	out[IndexNow] = plural(int(urls), "URL submitted", "URLs submitted")
	return out, nil
}

// choices lists the properties or sites a UI credential can see, while none is picked or on request.
func (m *Manager) choices(ctx context.Context, r resolved, pick Kind, c *Card) {
	if (r.kind != Google && r.kind != Bing) || r.source != SourceUI || r.err != nil || (r.target != "" && pick != r.kind) {
		return
	}
	sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	sites, err := m.sites(sctx, r)
	if err != nil {
		c.ChoicesErr = CodeOf(err).Message()
		m.Log.Warn("integration sites unreadable", "kind", r.kind, "code", CodeOf(err), "error", err)
		return
	}
	c.Choices = sites
}

func (m *Manager) card(r resolved) Card {
	k := r.kind
	secret, _ := m.Env.lookup(k)
	c := Card{
		Kind: k, Title: Title(k), Source: r.source, Target: r.target, EnvVars: envVars(k), Enabled: r.enabled(),
		VaultSet: secret != "",
	}
	c.Shadowed = r.source == SourceUI && c.VaultSet
	if r.hasRow {
		m.rowFacts(r.row, &c)
	}
	if r.source == SourceVault {
		c.Account = ""
		if sa, err := ParseServiceAccount(r.secret); k == Google && err == nil {
			c.Account = sa.ClientEmail
		}
	}
	if r.err != nil {
		code := CodeOf(r.err)
		c.ErrorCode, c.Error = string(code), code.Message()
	}
	c.Status, c.Label = status(r, c)
	switch k {
	case IndexNow:
		m.indexNowFacts(r, &c)
	case Cloudflare:
		cloudflareFacts(r, &c)
	case Google, Bing:
	}
	return c
}

func (m *Manager) rowFacts(row queries.Integration, c *Card) {
	c.Account, c.Fingerprint = row.Account, deref(row.Fingerprint)
	c.SavedBy, c.SavedAt = deref(row.CreatedByName), stamp(row.CreatedAt)
	c.ChangedBy, c.ChangedAt = deref(row.UpdatedByName), stamp(row.UpdatedAt)
	c.LastSync, c.LastSuccess, c.NextSync = stamp(row.LastSyncAt), stamp(row.LastSuccessAt), stamp(row.NextSyncAt)
	c.Running = row.RunningSince != nil && row.RunningSince.After(m.Now().Add(-staleClaim))
	if row.LastErrorCode != nil {
		c.ErrorCode, c.Error = *row.LastErrorCode, deref(row.LastError)
	}
}

func (m *Manager) indexNowFacts(r resolved, c *Card) {
	if r.source == SourceUI && r.err == nil {
		c.KeyURL = m.keyLocation(string(r.secret))
	}
	if r.meta.LastStatus != 0 {
		c.LastSubmission = fmt.Sprintf("%s · HTTP %d", plural(int(r.meta.LastCount), "URL", "URLs"), r.meta.LastStatus)
	}
}

func cloudflareFacts(r resolved, c *Card) {
	c.Zone, c.Plan = r.meta.Zone, r.meta.Plan
	known := r.meta.Caps != nil || r.meta.Unavailable != nil || (r.hasRow && r.row.LastSuccessAt != nil)
	for _, d := range Dimensions()[1:] {
		c.Dims = append(c.Dims, Dim{Label: d.Label, Field: d.Field, Available: !slices.Contains(r.meta.Unavailable, d.Name), Known: known})
	}
	if r.meta.Caps != nil {
		c.Window = fmt.Sprintf("reads %d days back, up to %d rows per query", r.meta.Caps.Window()/(24*time.Hour), r.meta.Caps.MaxPageSize)
	}
}

func status(r resolved, c Card) (string, string) {
	switch {
	case r.source == SourceNone:
		return "none", "Not configured"
	case c.Error != "":
		return "error", "Error"
	case !c.Enabled:
		return "paused", "Paused"
	case r.target == "" && r.kind == Google:
		return "setup", "Pick a property"
	case r.target == "" && r.kind == Bing:
		return "setup", "Pick a site"
	case r.source == SourceVault:
		return "vault", "Managed by Vault"
	}
	return "connected", "Connected"
}

func (m *Manager) events(ctx context.Context, k Kind) ([]Event, error) {
	rows, err := m.Q.ListIntegrationEvents(ctx, queries.ListIntegrationEventsParams{Kind: string(k), Lim: 8})
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(rows))
	for _, e := range rows {
		ev := Event{At: stamp(&e.At), Action: e.Action, Actor: e.ActorName, Detail: e.Detail, OK: e.Ok}
		if e.Code != "" {
			ev.Code = e.Code
		}
		if e.HttpStatus != nil && *e.HttpStatus != 0 {
			ev.Status = "HTTP " + strconv.FormatInt(*e.HttpStatus, 10)
		}
		if e.Count != nil && e.Action == "notify" {
			ev.Detail = plural(int(*e.Count), "URL", "URLs") + " · " + e.Detail
		}
		out = append(out, ev)
	}
	return out, nil
}

// compare builds the edge-versus-origin panel over the last seven complete UTC days with edge data.
func (m *Manager) compare(ctx context.Context) (*Compare, error) {
	to := day(m.Now())
	from := to.AddDate(0, 0, -7)
	rows, err := m.Q.CloudflareDaily(ctx, queries.CloudflareDailyParams{Host: m.Host(), FromDay: from, ToDay: to})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	origin, err := m.Q.HitsPerUTCDay(ctx, queries.HitsPerUTCDayParams{FromTs: from, ToTs: to})
	if err != nil {
		return nil, err
	}
	bots, err := m.Q.OriginAIBots(ctx, queries.OriginAIBotsParams{FromTs: from, ToTs: to})
	if err != nil {
		return nil, err
	}
	c := &Compare{Range: from.Format("2 Jan") + " – " + to.AddDate(0, 0, -1).Format("2 Jan 2006") + " (UTC days)"}
	f := &fold{days: map[string]*CompareDay{}, bots: map[string]int64{}}
	for _, r := range rows {
		f.edge(c, r)
	}
	for _, o := range origin {
		d := f.day(o.Day)
		d.Origin, d.OriginAI = o.Hits, o.AiHits
	}
	keys := make([]string, 0, len(f.days))
	for k := range f.days {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c.Days = append(c.Days, *f.days[k])
	}
	c.Bots = botRows(f.bots, bots, c.HasUA)
	return c, nil
}

// fold accumulates edge rows per day and per AI agent.
type fold struct {
	days map[string]*CompareDay
	bots map[string]int64
}

func (f *fold) day(d time.Time) *CompareDay {
	key := d.Format(time.DateOnly)
	if f.days[key] == nil {
		f.days[key] = &CompareDay{Day: d.Format("Mon 2 Jan")}
	}
	return f.days[key]
}

func (f *fold) edge(c *Compare, r queries.CloudflareDailyRow) {
	d := f.day(r.Day)
	switch r.Dimension {
	case "total":
		d.Edge += r.Requests
	case "path":
		d.HasComparable = true
		if !strings.HasPrefix(r.Value, "/static/") && r.Value != "/b" {
			d.EdgeComparable += r.Requests
		}
	case "user_agent":
		c.HasUA, d.HasAI = true, true
		if name, kind, ok := visits.ClaimedBot(r.Value); ok && (kind == visits.KindAICrawler || kind == visits.KindAIFetcher) {
			d.EdgeAI += r.Requests
			f.bots[name] += r.Requests
		}
	case "status":
		c.HasStatus = true
		if r.Value == "403" {
			c.Blocked += r.Requests
		}
	}
}

func botRows(edge map[string]int64, origin []queries.OriginAIBotsRow, hasUA bool) []CompareBot {
	at := map[string]int64{}
	for _, b := range origin {
		at[b.BotName] = b.Hits
	}
	var out []CompareBot
	for name, n := range edge {
		out = append(out, CompareBot{Name: name, Edge: n, Origin: at[name], Missing: max(0, n-at[name])})
	}
	for name, n := range at {
		if _, ok := edge[name]; !ok && hasUA {
			out = append(out, CompareBot{Name: name, Origin: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Edge != out[j].Edge {
			return out[i].Edge > out[j].Edge
		}
		return out[i].Name < out[j].Name
	})
	return out
}
