package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
)

func events(t *testing.T, r *rig, k Kind) []queries.IntegrationEvent {
	t.Helper()
	ev, err := r.q.ListIntegrationEvents(context.Background(), queries.ListIntegrationEventsParams{Kind: string(k), Lim: 100})
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestVaultSourceAndUIPrecedence(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, Env{})
	sa := serviceAccount(t, r.f.srv.URL+"/token")
	r.m.Env = Env{GSCServiceAccountJSON: string(sa), GSCSiteURL: property}
	if g, b := r.m.SearchSources(ctx); !g || b {
		t.Fatalf("vault google only: %v %v", g, b)
	}
	c := r.card(t, Google)
	if c.Status != "vault" || c.Source != SourceVault || c.Target != property || c.Account != "stats@example.iam.gserviceaccount.com" || c.Shadowed {
		t.Fatalf("vault card %+v", c)
	}
	// A Vault source cannot be removed or re-pointed here.
	if CodeOf(r.m.Remove(ctx, r.admin, Google)) != CodeVaultManaged || CodeOf(r.m.Select(ctx, r.admin, Google, property)) != CodeVaultManaged {
		t.Fatal("vault is read-only")
	}
	msg, err := r.m.Sync(ctx, &r.admin, Google)
	if err != nil || msg != "2 rows imported" {
		t.Fatalf("vault sync: %q %v", msg, err)
	}
	// First run backfills from the launch (later than sixteen months back), the next one looks back ten days.
	r.clock.set(time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC))
	if _, err := r.m.Sync(ctx, nil, Google); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.f.gscStarts, ",") != "2026-09-30,2026-10-10" {
		t.Fatalf("windows %v", r.f.gscStarts)
	}

	// A credential saved in the UI takes precedence.
	if err := r.m.Connect(ctx, r.admin, Google, sa, ""); err != nil {
		t.Fatal(err)
	}
	c = r.card(t, Google)
	if c.Source != SourceUI || !c.Shadowed || c.Status != "setup" || c.Target != "" || c.SavedBy != "joris" || len(c.Fingerprint) != 12 {
		t.Fatalf("ui card %+v", c)
	}
	if !slices.Equal(c.Choices, []string{property}) {
		t.Fatalf("choices list verified properties only: %v", c.Choices)
	}
	if g, _ := r.m.SearchSources(ctx); g {
		t.Fatal("no property picked yet")
	}
	if CodeOf(r.m.Select(ctx, r.admin, Google, "sc-domain:someone-else.test")) != CodeTargetMissing {
		t.Fatal("only a visible property can be picked")
	}
	if err := r.m.Select(ctx, r.admin, Google, property); err != nil {
		t.Fatal(err)
	}
	if c = r.card(t, Google); c.Status != "connected" || c.Target != property || c.Choices != nil {
		t.Fatalf("connected %+v", c)
	}
	if msg, err := r.m.Test(ctx, r.admin, Google); err != nil || msg != "1 property visible" {
		t.Fatalf("test %q %v", msg, err)
	}
	r.f.gscStarts = nil
	if _, err := r.m.Sync(ctx, &r.admin, Google); err != nil || r.f.gscStarts[0] != "2026-09-30" {
		t.Fatalf("a new credential backfills again: %v %v", r.f.gscStarts, err)
	}
	if n := countRows(t, r, "google"); n != 2 {
		t.Fatalf("upserts are idempotent: %d", n)
	}

	// Removing it hands over to Vault again.
	if err := r.m.Remove(ctx, r.admin, Google); err != nil {
		t.Fatal(err)
	}
	if c = r.card(t, Google); c.Source != SourceVault || c.Fingerprint != "" || c.SavedBy != "" {
		t.Fatalf("after remove %+v", c)
	}
	var actions []string
	for _, e := range events(t, r, Google) {
		actions = append(actions, e.Action)
	}
	if strings.Join(actions, ",") != "remove,sync,test,select,connect,sync,sync" {
		t.Fatalf("audit log %v", actions)
	}
	for _, e := range events(t, r, Google) {
		if strings.Contains(e.Detail, "PRIVATE") {
			t.Fatal("no secret in the audit log")
		}
	}
}

func countRows(t *testing.T, r *rig, source string) int {
	t.Helper()
	var n int
	if err := r.pool.QueryRow(context.Background(), `SELECT count(*) FROM search_performance WHERE source = $1`, source).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestBingAndErrors(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, Env{BingAPIKey: "vault-bing-key-aaaaaaaaaa", BingSiteURL: bingSite})
	// The Vault key is wrong, so a test fails with a generic code that shows on the card.
	if _, err := r.m.Test(ctx, r.admin, Bing); CodeOf(err) != CodeAuth {
		t.Fatalf("bad vault key: %v", err)
	}
	if c := r.card(t, Bing); c.Status != "error" || c.ErrorCode != "auth_failed" || c.Error != CodeAuth.Message() {
		t.Fatalf("error card %+v", c)
	}
	for _, bad := range []string{"", "short", "with spaces in the key"} {
		if CodeOf(r.m.Connect(ctx, r.admin, Bing, []byte(bad), "")) != CodeInvalid {
			t.Fatalf("invalid key %q accepted", bad)
		}
	}
	if err := r.m.Connect(ctx, r.admin, Bing, []byte("  "+bingKey+"\n"), ""); err != nil {
		t.Fatal(err)
	}
	if err := r.m.Select(ctx, r.admin, Bing, bingSite); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Test(ctx, r.admin, Bing); err != nil {
		t.Fatal(err)
	}
	if c := r.card(t, Bing); c.Status != "connected" || c.Error != "" || !c.Shadowed {
		t.Fatalf("a passing test clears the error: %+v", c)
	}
	if msg, err := r.m.Sync(ctx, &r.admin, Bing); err != nil || msg != "2 rows imported" {
		t.Fatalf("bing sync %q %v", msg, err)
	}
	if c := r.card(t, Bing); c.Rows != "2 Search Performance rows" || c.LastSync == "" || c.NextSync == "" {
		t.Fatalf("bing card %+v", c)
	}
	// A run already in progress (another process) is not doubled.
	if _, err := r.q.ClaimSync(ctx, queries.ClaimSyncParams{Now: r.clock.now(), Kind: "bing", StaleBefore: r.clock.now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Sync(ctx, &r.admin, Bing); CodeOf(err) != CodeBusy {
		t.Fatalf("concurrent run: %v", err)
	}
	if !r.card(t, Bing).Running {
		t.Fatal("the card shows the running sync")
	}
	// Paused integrations are skipped by the schedule.
	if err := r.m.SetEnabled(ctx, r.admin, Bing, false); err != nil {
		t.Fatal(err)
	}
	if _, b := r.m.SearchSources(ctx); b || r.card(t, Bing).Status != "paused" {
		t.Fatal("paused")
	}
	if err := r.m.SetEnabled(ctx, r.admin, Bing, true); err != nil {
		t.Fatal(err)
	}
	if err := r.m.Remove(ctx, r.admin, Cloudflare); CodeOf(err) != CodeNotConfigured {
		t.Fatalf("nothing to remove: %v", err)
	}
	if CodeOf(r.m.Select(ctx, r.admin, Cloudflare, "x")) != CodeInvalid || CodeOf(r.m.Select(ctx, r.admin, Google, "x")) != CodeNotConfigured {
		t.Fatal("select")
	}
	if _, err := r.m.Sync(ctx, nil, Google); CodeOf(err) != CodeNotConfigured {
		t.Fatalf("unconfigured sync: %v", err)
	}
	if _, err := r.m.Test(ctx, r.admin, Google); CodeOf(err) != CodeNotConfigured {
		t.Fatalf("unconfigured test: %v", err)
	}
}

func TestKeyRotationAndNoStorage(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, Env{})
	if err := r.m.Connect(ctx, r.admin, Bing, []byte(bingKey), ""); err != nil {
		t.Fatal(err)
	}
	r.m.Sealer = sealer(t, otherKey)
	if c := r.card(t, Bing); c.Status != "error" || c.ErrorCode != string(CodeKeyChanged) || c.Choices != nil {
		t.Fatalf("rotated SESSION_KEY %+v", c)
	}
	if _, err := r.m.Sync(ctx, nil, Bing); CodeOf(err) != CodeKeyChanged {
		t.Fatalf("sync after rotation: %v", err)
	}
	r.m.Sealer = nil
	if CodeOf(r.m.Connect(ctx, r.admin, Bing, []byte(bingKey), "")) != CodeNoStorage {
		t.Fatal("no SESSION_KEY, no saving")
	}
	v, err := r.m.View(ctx, true, "")
	if err != nil || v.Storage || v.Cards[1].ErrorCode != string(CodeNoStorage) {
		t.Fatalf("no storage %+v %v", v, err)
	}
	if CodeOf(r.m.Connect(ctx, r.admin, Bing, make([]byte, MaxCredential+1), "")) != CodeNoStorage {
		t.Fatal("storage is checked first")
	}
	r.m.Sealer = sealer(t, testKey)
	if CodeOf(r.m.Connect(ctx, r.admin, Bing, make([]byte, MaxCredential+1), "")) != CodeTooLarge {
		t.Fatal("too large")
	}
	if CodeOf(r.m.Connect(ctx, r.admin, Google, []byte(`{"type":"user"}`), "")) != CodeNotServiceAccount {
		t.Fatal("not a service account")
	}
	if CodeOf(r.m.Connect(ctx, r.admin, IndexNow, []byte("no"), "")) != CodeInvalid {
		t.Fatal("bad indexnow key")
	}
}

func TestViewerSeesNoAuditOrChoices(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, Env{})
	if err := r.m.Connect(ctx, r.admin, Bing, []byte(bingKey), ""); err != nil {
		t.Fatal(err)
	}
	v, err := r.m.View(ctx, false, Bing)
	if err != nil {
		t.Fatal(err)
	}
	if v.Admin || v.Cards[1].Choices != nil || v.Cards[1].Events != nil || v.Cards[1].Fingerprint == "" {
		t.Fatalf("viewer card %+v", v.Cards[1])
	}
}

func TestIndexNow(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, Env{})
	site := httptest.NewServer(r.m.KeyFile(http.NotFoundHandler()))
	t.Cleanup(site.Close)
	r.m.BaseURL = site.URL
	host := r.m.Host()
	r.m.PageURLs = func() []string {
		return []string{site.URL + "/", site.URL + "/sectoren", "https://elsewhere.test/x", site.URL + "/"}
	}

	if _, err := r.m.Sync(ctx, &r.admin, IndexNow); CodeOf(err) != CodeNotConfigured {
		t.Fatalf("no key yet: %v", err)
	}
	if err := r.m.GenerateKey(ctx, r.admin); err != nil {
		t.Fatal(err)
	}
	c := r.card(t, IndexNow)
	key := strings.TrimSuffix(strings.TrimPrefix(c.KeyURL, site.URL+"/"), ".txt")
	if c.Status != "connected" || !ValidIndexNowKey(key) {
		t.Fatalf("indexnow card %+v", c)
	}
	resp, err := http.Get(c.KeyURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("key file %d", resp.StatusCode)
	}
	for _, p := range []string{"/" + strings.Repeat("a", 32) + ".txt", "/robots.txt", "/" + key + ".txt/x"} {
		resp, _ := http.Get(site.URL + p)
		_ = resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Errorf("%s served", p)
		}
	}
	if msg, err := r.m.Test(ctx, r.admin, IndexNow); err != nil || msg != "key file served" {
		t.Fatalf("test %q %v", msg, err)
	}
	msg, err := r.m.Sync(ctx, &r.admin, IndexNow)
	if err != nil || msg != "2 URLs submitted (HTTP 202)" {
		t.Fatalf("notify %q %v", msg, err)
	}
	sub := r.f.submitted[0]
	if sub["host"] != host || sub["key"] != key || sub["keyLocation"] != c.KeyURL || len(sub["urlList"].([]any)) != 2 {
		t.Fatalf("submission %+v", sub)
	}
	r.m.NotifyChanged(ctx, "v2-test", []string{site.URL + "/sectoren"})
	if len(r.f.submitted) != 2 || len(r.f.submitted[1]["urlList"].([]any)) != 1 {
		t.Fatalf("release notification %+v", r.f.submitted)
	}
	r.f.indexNowCode.Store(http.StatusForbidden)
	if _, err := r.m.Sync(ctx, &r.admin, IndexNow); CodeOf(err) != CodeForbidden {
		t.Fatalf("rejected key: %v", err)
	}
	ev := events(t, r, IndexNow)
	if ev[0].Action != "notify" || ev[0].Ok || *ev[0].HttpStatus != 403 || *ev[0].Count != 2 {
		t.Fatalf("failed submission logged: %+v", ev[0])
	}
	c = r.card(t, IndexNow)
	if c.Rows != "3 URLs submitted" || c.LastSubmission != "0 URLs · HTTP 403" || c.Status != "error" {
		t.Fatalf("indexnow history %+v", c)
	}
	// A new key replaces the old file at once.
	if err := r.m.GenerateKey(ctx, r.admin); err != nil {
		t.Fatal(err)
	}
	resp, _ = http.Get(site.URL + "/" + key + ".txt")
	_ = resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatal("the old key is no longer served")
	}
	if err := r.m.Remove(ctx, r.admin, IndexNow); err != nil {
		t.Fatal(err)
	}
	r.m.NotifyChanged(ctx, "v3", []string{site.URL + "/"})
	if len(r.f.submitted) != 3 {
		t.Fatal("no key, no notification")
	}
	r.m.BaseURL = "http://127.0.0.1:1"
	if err := r.m.GenerateKey(ctx, r.admin); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Test(ctx, r.admin, IndexNow); CodeOf(err) != CodeNetwork {
		t.Fatalf("unreachable key location: %v", err)
	}
}

func TestBatches(t *testing.T) {
	var urls []string
	for i := range 25001 {
		urls = append(urls, "https://tribelt.jorisjonkers.dev/p"+strings.Repeat("x", i%3)+string(rune('a'+i%26))+"/"+strconv.Itoa(i))
	}
	urls = append(urls, urls[0], "https://other.test/x", "ftp://tribelt.jorisjonkers.dev/x", "://bad")
	b := Batches(urls, "tribelt.jorisjonkers.dev", BatchSize)
	if len(b) != 3 || len(b[0]) != 10000 || len(b[2]) != 5001 {
		t.Fatalf("batches %d", len(b))
	}
	if Batches(nil, "x", 10) != nil {
		t.Fatal("nothing to send")
	}
}

func TestChangedURLs(t *testing.T) {
	before := map[string]string{"/": "h1", "/a": "h2", "/gone": "h3"}
	after := map[string]string{"/": "h1", "/a": "h2b", "/new": "h4"}
	got := ChangedURLs("https://x.test", before, after)
	if strings.Join(got, ",") != "https://x.test/a,https://x.test/gone,https://x.test/new" {
		t.Fatalf("%v", got)
	}
}

func TestCaps(t *testing.T) {
	c := Caps{Fields: []string{"dimensions.userAgent", "dimensions/clientRequestPath", "count"}}
	if !c.Has("userAgent") || !c.Has("clientRequestPath") || !c.Has("count") || c.Has("verifiedBotCategory") || c.Has("Agent") {
		t.Fatal("field paths")
	}
	if !(Caps{}).Has("anything") || (Caps{}).Window() != 8*24*time.Hour || (Caps{NotOlderThan: 86400}).Window() != 24*time.Hour {
		t.Fatal("defaults")
	}
}

func TestCloudflare(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, Env{})
	if CodeOf(r.m.Connect(ctx, r.admin, Cloudflare, []byte(cfToken), "not-a-zone")) != CodeInvalid {
		t.Fatal("zone id format")
	}
	if err := r.m.Connect(ctx, r.admin, Cloudflare, []byte(cfToken), strings.ToUpper(cfZone)); err != nil {
		t.Fatal(err)
	}
	c := r.card(t, Cloudflare)
	if c.Status != "connected" || c.Target != cfZone || c.Dims[0].Known {
		t.Fatalf("before test %+v", c)
	}
	if msg, err := r.m.Test(ctx, r.admin, Cloudflare); err != nil || msg != "jorisjonkers.dev · Free Website" {
		t.Fatalf("test %q %v", msg, err)
	}
	c = r.card(t, Cloudflare)
	avail := map[string]bool{}
	for _, d := range c.Dims {
		avail[d.Field] = d.Available
	}
	if c.Plan != "Free Website" || !avail["userAgent"] || !avail["clientRequestPath"] || avail["verifiedBotCategory"] || !strings.Contains(c.Window, "8 days") {
		t.Fatalf("plan fields %+v", c)
	}
	// Every complete UTC day since the launch (30 Sept to 4 Oct; today is 5 Oct), four breakdowns of nine rows.
	msg, err := r.m.Sync(ctx, &r.admin, Cloudflare)
	if err != nil || msg != "45 rows imported" {
		t.Fatalf("cloudflare sync %q %v", msg, err)
	}
	for _, q := range r.f.queries {
		if strings.Contains(q, "verifiedBotCategory") {
			t.Fatal("a field the settings omit is never queried")
		}
	}
	// Origin Hits for the comparison: GPTBot reached the origin twice, ClaudeBot never.
	for i, kind := range []string{"ai-crawler", "ai-crawler", "human"} {
		bot := map[bool]*string{true: ptr("GPTBot"), false: nil}[kind == "ai-crawler"]
		_, err := r.pool.Exec(ctx, `INSERT INTO hits (id, ts, path, release_label, format, status, visitor_kind, bot_name, user_agent, daily_hash)
			VALUES (gen_random_uuid(), $1, '/sectoren', 'v1', 'html', 200, $2, $3, 'ua', 'd')`, time.Date(2026, 10, 3, 10, i, 0, 0, time.UTC), kind, bot)
		if err != nil {
			t.Fatal(err)
		}
	}
	v, err := r.m.View(ctx, true, "")
	if err != nil || v.Compare == nil {
		t.Fatalf("compare %v", err)
	}
	cmp := v.Compare
	if len(cmp.Days) != 5 || cmp.Blocked != 225 || !cmp.HasUA || !cmp.HasStatus {
		t.Fatalf("compare %+v", cmp)
	}
	d := cmp.Days[3]
	if d.Edge != 145 || d.EdgeComparable != 90 || d.Origin != 3 || d.EdgeAI != 45 || d.OriginAI != 2 {
		t.Fatalf("3 Oct %+v", d)
	}
	if cmp.Bots[0].Name != "GPTBot" || cmp.Bots[0].Edge != 200 || cmp.Bots[0].Origin != 2 || cmp.Bots[0].Missing != 198 || cmp.Bots[1].Name != "ClaudeBot" {
		t.Fatalf("bots %+v", cmp.Bots)
	}

	// Without readable settings, the import learns the plan's fields from the API's answers.
	r.f.cfNoSettings.Store(true)
	if _, err := r.pool.Exec(ctx, `UPDATE integrations SET meta = '{}', backfilled_at = NULL WHERE kind = 'cloudflare'`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Sync(ctx, nil, Cloudflare); err != nil {
		t.Fatal(err)
	}
	if c = r.card(t, Cloudflare); !c.Dims[0].Known || c.Dims[3].Available {
		t.Fatalf("learned fields %+v", c.Dims)
	}
	if _, err := r.m.Test(ctx, r.admin, Cloudflare); err != nil {
		t.Fatalf("test without settings: %v", err)
	}

	r.f.cfZoneName.Store("other.example")
	if _, err := r.m.Test(ctx, r.admin, Cloudflare); CodeOf(err) != CodeTargetMissing {
		t.Fatalf("a zone that does not serve the host: %v", err)
	}
	r.f.cfInactive.Store(true)
	if _, err := r.m.Test(ctx, r.admin, Cloudflare); CodeOf(err) != CodeAuth {
		t.Fatalf("inactive token: %v", err)
	}
	if err := r.m.Connect(ctx, r.admin, Cloudflare, []byte(cfToken), strings.Repeat("f", 32)); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Test(ctx, r.admin, Cloudflare); CodeOf(err) != CodeAuth {
		t.Fatal("verify runs first")
	}
	r.f.cfInactive.Store(false)
	if _, err := r.m.Test(ctx, r.admin, Cloudflare); CodeOf(err) != CodeNotFound {
		t.Fatalf("unknown zone: %v", err)
	}
	if _, err := r.m.Sync(ctx, nil, Cloudflare); CodeOf(err) != CodeNotFound {
		t.Fatalf("unknown zone sync: %v", err)
	}
	if err := r.m.Connect(ctx, r.admin, Cloudflare, []byte("wrong-token-aaaaaaaaaaaa"), cfZone); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Test(ctx, r.admin, Cloudflare); CodeOf(err) != CodeAuth {
		t.Fatalf("wrong token: %v", err)
	}
}

func TestScheduler(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, Env{BingAPIKey: bingKey, BingSiteURL: bingSite})
	r.m.RunDue(ctx)
	if countRows(t, r, "bing") != 2 {
		t.Fatal("a due Vault source syncs")
	}
	if _, err := r.pool.Exec(ctx, `DELETE FROM search_performance`); err != nil {
		t.Fatal(err)
	}
	r.m.RunDue(ctx)
	if countRows(t, r, "bing") != 0 {
		t.Fatal("not due again within a day")
	}
	r.clock.set(r.clock.now().Add(25 * time.Hour))
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() { r.m.Run(runCtx, time.Hour, time.Hour); close(done) }()
	r.m.Kick()
	r.m.Kick()
	deadline := time.Now().Add(10 * time.Second)
	for countRows(t, r, "bing") != 2 {
		if time.Now().After(deadline) {
			t.Fatal("a kick runs the due syncs")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
}
