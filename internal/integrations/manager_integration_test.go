//go:build integration

package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
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

// indexNowSite is a rig whose site serves the IndexNow key file, with its pages on that site.
func indexNowSite(t *testing.T) (*rig, *httptest.Server) {
	t.Helper()
	r := newRig(t, Env{})
	site := httptest.NewServer(r.m.KeyFile(http.NotFoundHandler()))
	t.Cleanup(site.Close)
	r.m.BaseURL = site.URL
	r.m.PageURLs = func() []string {
		return []string{site.URL + "/", site.URL + "/sectoren", "https://elsewhere.test/x", site.URL + "/"}
	}
	return r, site
}

// withIndexNowKey is indexNowSite after generating a key, with the key and its card.
func withIndexNowKey(t *testing.T) (*rig, *httptest.Server, string, Card) {
	t.Helper()
	r, site := indexNowSite(t)
	if err := r.m.GenerateKey(context.Background(), r.admin); err != nil {
		t.Fatal(err)
	}
	c := r.card(t, IndexNow)
	return r, site, strings.TrimSuffix(strings.TrimPrefix(c.KeyURL, site.URL+"/"), ".txt"), c
}

func fetch(t *testing.T, url string) (int, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode, resp.Header.Get("Content-Type")
}

func TestIndexNowCannotSyncBeforeItHasAKey(t *testing.T) {
	r, _ := indexNowSite(t)
	if _, err := r.m.Sync(context.Background(), &r.admin, IndexNow); CodeOf(err) != CodeNotConfigured {
		t.Fatalf("err = %v", err)
	}
}

func TestAGeneratedIndexNowKeyIsServedAsPlainText(t *testing.T) {
	_, _, key, c := withIndexNowKey(t)
	if c.Status != "connected" || !ValidIndexNowKey(key) {
		t.Fatalf("card %+v", c)
	}
	if code, ct := fetch(t, c.KeyURL); code != 200 || ct != "text/plain; charset=utf-8" {
		t.Fatalf("key file %d %q", code, ct)
	}
}

func TestOnlyTheCurrentKeyFileIsServed(t *testing.T) {
	_, site, key, _ := withIndexNowKey(t)
	for _, p := range []string{"/" + strings.Repeat("a", 32) + ".txt", "/robots.txt", "/" + key + ".txt/x"} {
		t.Run(p, func(t *testing.T) {
			if code, _ := fetch(t, site.URL+p); code != 404 {
				t.Fatalf("served %d", code)
			}
		})
	}
}

func TestTestingIndexNowChecksTheKeyFileIsServed(t *testing.T) {
	r, _, _, _ := withIndexNowKey(t)
	if msg, err := r.m.Test(context.Background(), r.admin, IndexNow); err != nil || msg != "key file served" {
		t.Fatalf("test %q %v", msg, err)
	}
}

func TestAnIndexNowSyncSubmitsEachPageOnTheHostOnce(t *testing.T) {
	r, _, key, c := withIndexNowKey(t)
	msg, err := r.m.Sync(context.Background(), &r.admin, IndexNow)
	if err != nil || msg != "2 URLs submitted (HTTP 202)" {
		t.Fatalf("sync %q %v", msg, err)
	}
	sub := r.f.submitted[0]
	if sub["host"] != r.m.Host() || sub["key"] != key || sub["keyLocation"] != c.KeyURL || len(sub["urlList"].([]any)) != 2 {
		t.Fatalf("submission %+v", sub)
	}
}

func TestAReleaseSubmitsItsChangedPagesToIndexNow(t *testing.T) {
	r, site, _, _ := withIndexNowKey(t)
	r.m.NotifyChanged(context.Background(), "v2-test", []string{site.URL + "/sectoren"})
	if len(r.f.submitted) != 1 || len(r.f.submitted[0]["urlList"].([]any)) != 1 {
		t.Fatalf("submitted %+v", r.f.submitted)
	}
}

func TestARefusedIndexNowSubmissionIsLoggedAndShownAsAnError(t *testing.T) {
	ctx := context.Background()
	r, _, _, _ := withIndexNowKey(t)
	if _, err := r.m.Sync(ctx, &r.admin, IndexNow); err != nil {
		t.Fatal(err)
	}
	r.f.indexNowCode.Store(http.StatusForbidden)
	if _, err := r.m.Sync(ctx, &r.admin, IndexNow); CodeOf(err) != CodeForbidden {
		t.Fatalf("err = %v", err)
	}
	ev := events(t, r, IndexNow)
	if ev[0].Action != "notify" || ev[0].Ok || *ev[0].HttpStatus != 403 || *ev[0].Count != 2 {
		t.Fatalf("logged %+v", ev[0])
	}
	if c := r.card(t, IndexNow); c.Rows != "2 URLs submitted" || c.LastSubmission != "0 URLs · HTTP 403" || c.Status != "error" {
		t.Fatalf("card %+v", c)
	}
}

func TestANewIndexNowKeyStopsServingTheOldOne(t *testing.T) {
	r, site, key, _ := withIndexNowKey(t)
	if err := r.m.GenerateKey(context.Background(), r.admin); err != nil {
		t.Fatal(err)
	}
	if code, _ := fetch(t, site.URL+"/"+key+".txt"); code != 404 {
		t.Fatalf("old key served %d", code)
	}
}

func TestWithoutAnIndexNowKeyAReleaseSubmitsNothing(t *testing.T) {
	ctx := context.Background()
	r, site, _, _ := withIndexNowKey(t)
	if err := r.m.Remove(ctx, r.admin, IndexNow); err != nil {
		t.Fatal(err)
	}
	r.m.NotifyChanged(ctx, "v3", []string{site.URL + "/"})
	if len(r.f.submitted) != 0 {
		t.Fatalf("submitted %+v", r.f.submitted)
	}
}

func TestAnUnreachableKeyLocationFailsTheTestAsANetworkError(t *testing.T) {
	ctx := context.Background()
	r, _ := indexNowSite(t)
	r.m.BaseURL = "http://127.0.0.1:1"
	if err := r.m.GenerateKey(ctx, r.admin); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Test(ctx, r.admin, IndexNow); CodeOf(err) != CodeNetwork {
		t.Fatalf("err = %v", err)
	}
}

// cloudflare is a rig connected to the test zone.
func cloudflare(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, Env{})
	if err := r.m.Connect(context.Background(), r.admin, Cloudflare, []byte(cfToken), strings.ToUpper(cfZone)); err != nil {
		t.Fatal(err)
	}
	return r
}

// cloudflareSynced is a connected, tested and synced Cloudflare rig.
func cloudflareSynced(t *testing.T) *rig {
	t.Helper()
	r := cloudflare(t)
	if _, err := r.m.Test(context.Background(), r.admin, Cloudflare); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Sync(context.Background(), &r.admin, Cloudflare); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestACloudflareZoneIDMustLookLikeOne(t *testing.T) {
	r := newRig(t, Env{})
	if CodeOf(r.m.Connect(context.Background(), r.admin, Cloudflare, []byte(cfToken), "not-a-zone")) != CodeInvalid {
		t.Fatal("accepted")
	}
}

func TestAConnectedZoneIsStoredLowercaseWithItsFieldsNotYetKnown(t *testing.T) {
	if c := cloudflare(t).card(t, Cloudflare); c.Status != "connected" || c.Target != cfZone || c.Dims[0].Known {
		t.Fatalf("card %+v", c)
	}
}

func TestTestingCloudflareLearnsThePlanAndTheFieldsItAllows(t *testing.T) {
	r := cloudflare(t)
	if msg, err := r.m.Test(context.Background(), r.admin, Cloudflare); err != nil || msg != "jorisjonkers.dev · Free Website" {
		t.Fatalf("test %q %v", msg, err)
	}
	c := r.card(t, Cloudflare)
	avail := map[string]bool{}
	for _, d := range c.Dims {
		avail[d.Field] = d.Available
	}
	if c.Plan != "Free Website" || !avail["userAgent"] || !avail["clientRequestPath"] || avail["verifiedBotCategory"] || !strings.Contains(c.Window, "8 days") {
		t.Fatalf("card %+v", c)
	}
}

func TestACloudflareSyncImportsEveryCompleteDaySinceTheLaunch(t *testing.T) {
	r := cloudflare(t)
	if _, err := r.m.Test(context.Background(), r.admin, Cloudflare); err != nil {
		t.Fatal(err)
	}
	// 30 Sept to 4 Oct (today is 5 Oct), four breakdowns of nine rows.
	if msg, err := r.m.Sync(context.Background(), &r.admin, Cloudflare); err != nil || msg != "45 rows imported" {
		t.Fatalf("sync %q %v", msg, err)
	}
}

func TestACloudflareSyncNeverAsksForAFieldThePlanLacks(t *testing.T) {
	r := cloudflareSynced(t)
	for _, q := range r.f.queries {
		if strings.Contains(q, "verifiedBotCategory") {
			t.Fatalf("queried %s", q)
		}
	}
}

func TestEdgeAndOriginRequestsAreComparedPerDayAndPerBot(t *testing.T) {
	ctx := context.Background()
	r := cloudflareSynced(t)
	// GPTBot reached the origin twice, ClaudeBot never.
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
	if d := cmp.Days[3]; d.Edge != 145 || d.EdgeComparable != 90 || d.Origin != 3 || d.EdgeAI != 45 || d.OriginAI != 2 {
		t.Fatalf("3 Oct %+v", d)
	}
	if cmp.Bots[0].Name != "GPTBot" || cmp.Bots[0].Edge != 200 || cmp.Bots[0].Origin != 2 || cmp.Bots[0].Missing != 198 || cmp.Bots[1].Name != "ClaudeBot" {
		t.Fatalf("bots %+v", cmp.Bots)
	}
}

func TestWithoutReadableSettingsTheImportLearnsThePlansFieldsFromItsAnswers(t *testing.T) {
	ctx := context.Background()
	r := cloudflareSynced(t)
	r.f.cfNoSettings.Store(true)
	if _, err := r.pool.Exec(ctx, `UPDATE integrations SET meta = '{}', backfilled_at = NULL WHERE kind = 'cloudflare'`); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Sync(ctx, nil, Cloudflare); err != nil {
		t.Fatal(err)
	}
	if c := r.card(t, Cloudflare); !c.Dims[0].Known || c.Dims[3].Available {
		t.Fatalf("fields %+v", c.Dims)
	}
	if _, err := r.m.Test(ctx, r.admin, Cloudflare); err != nil {
		t.Fatalf("test without settings: %v", err)
	}
}

func TestAZoneThatDoesNotServeTheHostIsReported(t *testing.T) {
	r := cloudflare(t)
	r.f.cfZoneName.Store("other.example")
	if _, err := r.m.Test(context.Background(), r.admin, Cloudflare); CodeOf(err) != CodeTargetMissing {
		t.Fatalf("err = %v", err)
	}
}

func TestAnInactiveCloudflareTokenIsAnAuthError(t *testing.T) {
	r := cloudflare(t)
	r.f.cfInactive.Store(true)
	if _, err := r.m.Test(context.Background(), r.admin, Cloudflare); CodeOf(err) != CodeAuth {
		t.Fatalf("err = %v", err)
	}
}

// unknownZone is a rig connected to a zone the API does not know.
func unknownZone(t *testing.T) *rig {
	t.Helper()
	r := newRig(t, Env{})
	if err := r.m.Connect(context.Background(), r.admin, Cloudflare, []byte(cfToken), strings.Repeat("f", 32)); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestTheCloudflareTokenIsVerifiedBeforeTheZone(t *testing.T) {
	r := unknownZone(t)
	r.f.cfInactive.Store(true)
	if _, err := r.m.Test(context.Background(), r.admin, Cloudflare); CodeOf(err) != CodeAuth {
		t.Fatalf("err = %v", err)
	}
}

func TestAnUnknownZoneIsNotFoundToTestOrSync(t *testing.T) {
	r := unknownZone(t)
	if _, err := r.m.Test(context.Background(), r.admin, Cloudflare); CodeOf(err) != CodeNotFound {
		t.Fatalf("test: %v", err)
	}
	if _, err := r.m.Sync(context.Background(), nil, Cloudflare); CodeOf(err) != CodeNotFound {
		t.Fatalf("sync: %v", err)
	}
}

func TestAWrongCloudflareTokenIsAnAuthError(t *testing.T) {
	r := newRig(t, Env{})
	if err := r.m.Connect(context.Background(), r.admin, Cloudflare, []byte("wrong-token-aaaaaaaaaaaa"), cfZone); err != nil {
		t.Fatal(err)
	}
	if _, err := r.m.Test(context.Background(), r.admin, Cloudflare); CodeOf(err) != CodeAuth {
		t.Fatalf("err = %v", err)
	}
}

func TestADueVaultSourceSyncs(t *testing.T) {
	r := newRig(t, Env{BingAPIKey: bingKey, BingSiteURL: bingSite})
	r.m.RunDue(context.Background())
	if n := countRows(t, r, "bing"); n != 2 {
		t.Fatalf("rows = %d", n)
	}
}

func TestASyncedSourceIsNotDueAgainWithinADay(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, Env{BingAPIKey: bingKey, BingSiteURL: bingSite})
	r.m.RunDue(ctx)
	if _, err := r.pool.Exec(ctx, `DELETE FROM search_performance`); err != nil {
		t.Fatal(err)
	}
	r.m.RunDue(ctx)
	if n := countRows(t, r, "bing"); n != 0 {
		t.Fatalf("rows = %d", n)
	}
}

func TestAKickRunsTheSyncsThatAreDue(t *testing.T) {
	ctx := context.Background()
	r := newRig(t, Env{BingAPIKey: bingKey, BingSiteURL: bingSite})
	r.m.RunDue(ctx)
	if _, err := r.pool.Exec(ctx, `DELETE FROM search_performance`); err != nil {
		t.Fatal(err)
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
			t.Fatal("the kick ran nothing")
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
}
