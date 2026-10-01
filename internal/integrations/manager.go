package integrations

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

// Source is where an Integration's credential comes from.
type Source string

const (
	SourceNone  Source = ""
	SourceUI    Source = "ui"
	SourceVault Source = "vault"
)

// Env is the Vault-managed credentials, read-only. Empty strings are unset.
type Env struct {
	GSCServiceAccountJSON, GSCSiteURL string
	BingAPIKey, BingSiteURL           string
}

func (e Env) lookup(k Kind) (secret, target string) {
	switch k {
	case Google:
		return e.GSCServiceAccountJSON, e.GSCSiteURL
	case Bing:
		return e.BingAPIKey, e.BingSiteURL
	case IndexNow, Cloudflare:
	}
	return "", ""
}

// Endpoints are the provider base URLs; tests point them at fakes.
type Endpoints struct {
	Google, Bing, IndexNow, Cloudflare string
}

// DefaultEndpoints are the real providers.
func DefaultEndpoints() Endpoints {
	return Endpoints{
		Google: "https://www.googleapis.com", Bing: "https://ssl.bing.com",
		IndexNow: "https://api.indexnow.org/indexnow", Cloudflare: "https://api.cloudflare.com",
	}
}

// Actor is who changes or runs an Integration; nil is the scheduler.
type Actor struct {
	Sub, Name string
	Admin     bool
}

// Meta is the non-secret state an Integration keeps between runs.
type Meta struct {
	Zone        string   `json:"zone,omitempty"`
	Plan        string   `json:"plan,omitempty"`
	Caps        *Caps    `json:"caps,omitempty"`
	Unavailable []string `json:"unavailable,omitempty"`
	LastCount   int64    `json:"last_count,omitempty"`
	LastStatus  int      `json:"last_status,omitempty"`
}

// Search Performance windows: how far back each API reads, and how many days a daily run re-imports
// (search data settles over a few days).
const (
	lookbackDays = 10
	syncEvery    = 24 * time.Hour
	staleClaim   = 30 * time.Minute
)

// Manager owns every Integration: credentials, tests, syncs, IndexNow submissions and the schedule.
type Manager struct {
	Q         *queries.Queries
	Sealer    *Sealer // nil: SESSION_KEY unset, credentials cannot be saved in the UI
	Env       Env
	Endpoints Endpoints
	HTTP      *http.Client
	// BaseURL is the public origin, e.g. https://tribelt.jorisjonkers.dev.
	BaseURL string
	// Launch is the first day the site was public; backfills never reach further.
	Launch time.Time
	// PageURLs lists the absolute URL of every Mirror Page in the running Content Release.
	PageURLs func() []string
	Log      *slog.Logger
	Now      func() time.Time

	once sync.Once
	kick chan struct{}
	key  atomic.Pointer[keyEntry]
}

type keyEntry struct {
	key   string
	until time.Time
}

func (m *Manager) kicks() chan struct{} {
	m.once.Do(func() { m.kick = make(chan struct{}, 1) })
	return m.kick
}

// Host is the public host every IndexNow URL and Cloudflare count is about.
func (m *Manager) Host() string {
	u, _ := url.Parse(m.BaseURL)
	return u.Hostname()
}

type resolved struct {
	kind   Kind
	row    queries.Integration
	hasRow bool
	source Source
	secret []byte
	target string
	err    error // a saved credential that does not open
	meta   Meta
}

func (r resolved) enabled() bool { return !r.hasRow || r.row.Enabled }

func (r resolved) ready() bool {
	return r.source != SourceNone && r.err == nil && r.enabled() && (r.target != "" || r.kind == IndexNow)
}

func (m *Manager) resolve(ctx context.Context, kind Kind) (resolved, error) {
	r := resolved{kind: kind}
	row, err := m.Q.GetIntegration(ctx, string(kind))
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return r, err
	default:
		r.row, r.hasRow = row, true
		_ = json.Unmarshal(row.Meta, &r.meta)
	}
	if r.hasRow && row.Credential != nil {
		r.source, r.target = SourceUI, row.Target
		if m.Sealer == nil {
			r.err = Fail(CodeNoStorage)
			return r, nil
		}
		secret, err := m.Sealer.Open(kind, deref(row.KeyID), row.Credential)
		if err != nil {
			r.err = &CodedError{Code: CodeOf(err), Err: err}
			return r, nil
		}
		r.secret = secret
		return r, nil
	}
	if secret, target := m.Env.lookup(kind); secret != "" {
		r.source, r.secret, r.target = SourceVault, []byte(secret), target
	}
	return r, nil
}

// SearchSources reports which Search Performance imports can run.
func (m *Manager) SearchSources(ctx context.Context) (google, bing bool) {
	g, err := m.resolve(ctx, Google)
	if err == nil {
		google = g.ready()
	}
	b, err := m.resolve(ctx, Bing)
	if err == nil {
		bing = b.ready()
	}
	return google, bing
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func ptr[T any](v T) *T { return &v }

func (m *Manager) actorName(a *Actor) (sub *string, name string) {
	if a == nil {
		return nil, "scheduler"
	}
	return ptr(a.Sub), a.Name
}

// event appends to the audit log; a failure to log is itself logged, never fatal.
func (m *Manager) event(ctx context.Context, kind Kind, action string, a *Actor, err error, count, status *int64, detail string) {
	sub, name := m.actorName(a)
	code := ""
	if err != nil {
		code = string(CodeOf(err))
	}
	if e := m.Q.InsertIntegrationEvent(context.WithoutCancel(ctx), queries.InsertIntegrationEventParams{
		Kind: string(kind), Action: action, Ok: err == nil, Code: code, Detail: detail, Count: count, HttpStatus: status,
		ActorSub: sub, ActorName: name,
	}); e != nil {
		m.Log.Error("integration event not recorded", "kind", kind, "action", action, "error", e)
	}
	if err != nil {
		m.Log.Warn("integration "+action+" failed", "kind", kind, "code", CodeOf(err), "error", err)
	}
}

func (m *Manager) recordError(ctx context.Context, kind Kind, err error) {
	var msg, code *string
	if err != nil {
		c := CodeOf(err)
		msg, code = ptr(c.Message()), ptr(string(c))
	}
	_ = m.Q.EnsureIntegration(ctx, string(kind))
	if e := m.Q.RecordError(ctx, queries.RecordErrorParams{LastError: msg, LastErrorCode: code, Kind: string(kind)}); e != nil {
		m.Log.Error("integration status not recorded", "kind", kind, "error", e)
	}
}

// Connect seals and saves a credential entered in the UI. target is the Cloudflare zone id; Google
// and Bing pick theirs afterwards from what the credential can see.
func (m *Manager) Connect(ctx context.Context, a Actor, kind Kind, secret []byte, target string) error {
	if m.Sealer == nil {
		return Fail(CodeNoStorage)
	}
	if len(secret) > MaxCredential {
		return Fail(CodeTooLarge)
	}
	var account string
	switch kind {
	case Google:
		sa, err := ParseServiceAccount(secret)
		if err != nil {
			return err
		}
		account, target = sa.ClientEmail, ""
	case Bing:
		secret = []byte(strings.TrimSpace(string(secret)))
		if !ValidAPIKey(string(secret)) {
			return Fail(CodeInvalid)
		}
		target = ""
	case Cloudflare:
		secret, target = []byte(strings.TrimSpace(string(secret))), strings.ToLower(strings.TrimSpace(target))
		if !ValidAPIKey(string(secret)) || !ValidZoneID(target) {
			return Fail(CodeInvalid)
		}
	case IndexNow:
		if !ValidIndexNowKey(string(secret)) {
			return Fail(CodeInvalid)
		}
		target = m.Host()
	}
	before, err := m.resolve(ctx, kind)
	if err != nil {
		return err
	}
	sealed, err := m.Sealer.Seal(kind, secret)
	if err != nil {
		return err
	}
	if err := m.Q.SaveCredential(ctx, queries.SaveCredentialParams{
		Kind: string(kind), Credential: sealed, KeyID: m.Sealer.KeyID(), Fingerprint: Fingerprint(secret),
		Account: account, Target: target, ActorSub: a.Sub, ActorName: a.Name,
	}); err != nil {
		return err
	}
	action := "connect"
	if before.source == SourceUI {
		action = "replace"
	}
	m.event(ctx, kind, action, &a, nil, nil, nil, "fingerprint "+Fingerprint(secret))
	m.key.Store(nil)
	m.Kick()
	return nil
}

// GenerateKey creates and saves a new IndexNow key.
func (m *Manager) GenerateKey(ctx context.Context, a Actor) error {
	key, err := NewIndexNowKey()
	if err != nil {
		return err
	}
	return m.Connect(ctx, a, IndexNow, []byte(key), "")
}

func (m *Manager) sites(ctx context.Context, r resolved) ([]string, error) {
	switch r.kind {
	case Google:
		g, err := NewGSC(ctx, m.HTTP, r.secret, m.Endpoints.Google, r.target)
		if err != nil {
			return nil, err
		}
		return g.Sites(ctx)
	case Bing:
		return (&BingClient{HTTP: m.HTTP, BaseURL: m.Endpoints.Bing, APIKey: string(r.secret)}).Sites(ctx)
	case IndexNow, Cloudflare:
	}
	return nil, Fail(CodeInvalid)
}

// Select stores the Search Console property or Bing site, which must be one the credential can see.
func (m *Manager) Select(ctx context.Context, a Actor, kind Kind, target string) error {
	if kind != Google && kind != Bing {
		return Fail(CodeInvalid)
	}
	r, err := m.resolve(ctx, kind)
	if err != nil {
		return err
	}
	switch {
	case r.source == SourceVault:
		return Fail(CodeVaultManaged)
	case r.source == SourceNone:
		return Fail(CodeNotConfigured)
	case r.err != nil:
		return r.err
	}
	sites, err := m.sites(ctx, r)
	if err != nil {
		m.event(ctx, kind, "select", &a, err, nil, nil, "")
		return err
	}
	if !slices.Contains(sites, target) {
		return Fail(CodeTargetMissing)
	}
	if err := m.Q.SetTarget(ctx, queries.SetTargetParams{Kind: string(kind), Target: target, ActorSub: a.Sub, ActorName: a.Name}); err != nil {
		return err
	}
	m.event(ctx, kind, "select", &a, nil, nil, nil, target)
	m.Kick()
	return nil
}

// Remove deletes a credential saved in the UI; a Vault-managed one, if any, takes over again.
func (m *Manager) Remove(ctx context.Context, a Actor, kind Kind) error {
	r, err := m.resolve(ctx, kind)
	if err != nil {
		return err
	}
	if r.source != SourceUI {
		if r.source == SourceVault {
			return Fail(CodeVaultManaged)
		}
		return Fail(CodeNotConfigured)
	}
	if err := m.Q.ClearCredential(ctx, queries.ClearCredentialParams{ActorSub: a.Sub, ActorName: a.Name, Kind: string(kind)}); err != nil {
		return err
	}
	m.event(ctx, kind, "remove", &a, nil, nil, nil, "fingerprint "+deref(r.row.Fingerprint))
	m.key.Store(nil)
	return nil
}

// SetEnabled pauses or resumes the scheduled sync of an Integration.
func (m *Manager) SetEnabled(ctx context.Context, a Actor, kind Kind, on bool) error {
	if err := m.Q.EnsureIntegration(ctx, string(kind)); err != nil {
		return err
	}
	if err := m.Q.SetEnabled(ctx, queries.SetEnabledParams{Enabled: on, ActorSub: a.Sub, ActorName: a.Name, Kind: string(kind)}); err != nil {
		return err
	}
	action := "disable"
	if on {
		action = "enable"
	}
	m.event(ctx, kind, action, &a, nil, nil, nil, "")
	return nil
}

func (m *Manager) saveMeta(ctx context.Context, kind Kind, meta Meta) {
	raw, _ := json.Marshal(meta)
	if err := m.Q.SetMeta(ctx, queries.SetMetaParams{Meta: raw, Kind: string(kind)}); err != nil {
		m.Log.Error("integration meta not saved", "kind", kind, "error", err)
	}
}

// Test makes one cheap authenticated call and reports a summary for the UI.
func (m *Manager) Test(ctx context.Context, a Actor, kind Kind) (string, error) {
	r, err := m.resolve(ctx, kind)
	if err != nil {
		return "", err
	}
	msg, err := m.test(ctx, r)
	m.event(ctx, kind, "test", &a, err, nil, nil, msg)
	m.recordError(ctx, kind, err)
	return msg, err
}

func (m *Manager) test(ctx context.Context, r resolved) (string, error) {
	switch {
	case r.source == SourceNone:
		return "", Fail(CodeNotConfigured)
	case r.err != nil:
		return "", r.err
	}
	switch r.kind {
	case Google, Bing:
		sites, err := m.sites(ctx, r)
		if err != nil {
			return "", err
		}
		if r.target != "" && !slices.Contains(sites, r.target) {
			return "", Fail(CodeTargetMissing)
		}
		return plural(len(sites), "property", "properties") + " visible", nil
	case IndexNow:
		return m.testKeyFile(ctx, string(r.secret))
	case Cloudflare:
		return m.testCloudflare(ctx, r)
	}
	return "", Fail(CodeInvalid)
}

func (m *Manager) testKeyFile(ctx context.Context, key string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, m.keyLocation(key), nil)
	if err != nil {
		return "", err
	}
	resp, err := m.HTTP.Do(req)
	if err != nil {
		return "", redact(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body := make([]byte, 256)
	n, _ := resp.Body.Read(body)
	if resp.StatusCode != http.StatusOK || strings.TrimSpace(string(body[:n])) != key {
		return "", &CodedError{Code: CodeKeyMismatch, Status: resp.StatusCode}
	}
	return "key file served", nil
}

func (m *Manager) testCloudflare(ctx context.Context, r resolved) (string, error) {
	cf := &CloudflareClient{HTTP: m.HTTP, BaseURL: m.Endpoints.Cloudflare, Token: string(r.secret)}
	if err := cf.Verify(ctx); err != nil {
		return "", err
	}
	z, err := cf.Zone(ctx, r.target)
	if err != nil {
		return "", err
	}
	host := m.Host()
	if z.Name == "" || (host != z.Name && !strings.HasSuffix(host, "."+z.Name)) {
		return "", &CodedError{Code: CodeTargetMissing, Err: errors.New("zone does not serve the host")}
	}
	meta := r.meta
	meta.Zone, meta.Plan = z.Name, z.Plan
	if caps, err := cf.Settings(ctx, r.target); err == nil {
		meta.Caps = &caps
		meta.Unavailable = nil
		for _, d := range Dimensions() {
			if !caps.Has(d.Field) {
				meta.Unavailable = append(meta.Unavailable, d.Name)
			}
		}
	} else {
		m.Log.Warn("cloudflare settings unreadable; the import learns the plan's fields as it goes", "error", err)
	}
	m.saveMeta(ctx, Cloudflare, meta)
	return z.Name + " · " + orDefault(z.Plan, "plan unknown"), nil
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// Kick asks the scheduler to look for due syncs now.
func (m *Manager) Kick() {
	select {
	case m.kicks() <- struct{}{}:
	default:
	}
}

// Run syncs every due Integration after firstDelay, then on every tick and kick, until ctx ends.
func (m *Manager) Run(ctx context.Context, firstDelay, tick time.Duration) {
	timer := time.NewTimer(firstDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.kicks():
		case <-timer.C:
			timer.Reset(tick)
		}
		m.RunDue(ctx)
	}
}

// RunDue syncs each scheduled Integration whose next sync time has come.
func (m *Manager) RunDue(ctx context.Context) {
	now := m.Now()
	for _, k := range []Kind{Google, Bing, Cloudflare} {
		r, err := m.resolve(ctx, k)
		if err != nil {
			m.Log.Error("integration status unreadable", "kind", k, "error", err)
			continue
		}
		if !r.ready() || (r.hasRow && r.row.NextSyncAt != nil && r.row.NextSyncAt.After(now)) {
			continue
		}
		if _, err := m.Sync(ctx, nil, k); err != nil && CodeOf(err) != CodeBusy {
			m.Log.Error("scheduled sync failed", "kind", k, "code", CodeOf(err))
		}
	}
}
