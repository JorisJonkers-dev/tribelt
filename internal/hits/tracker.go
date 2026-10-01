package hits

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/JorisJonkers-dev/tribelt/internal/platform/pg/queries"
	"github.com/JorisJonkers-dev/tribelt/internal/visits"
)

// Tracker turns requests into Hits and Outbound Clicks.
type Tracker struct {
	Recorder *Recorder
	Ranges   *visits.Ranges
	Hasher   *visits.DailyHasher
	// Internal reports whether the request comes from a signed-in Stats Viewer.
	Internal func(r *http.Request) bool
	Release  string
	// AppVersion is the build serving the request, stamped on every Hit next to the release label.
	AppVersion string
	Host       string
	// Resolve maps a Mirror Page path to its id and locale.
	Resolve func(path string) (id, locale string, ok bool)
	// SecureCookies is false only for plain-HTTP local development.
	SecureCookies bool
	Now           func() time.Time
	Rand          io.Reader
}

// Info is filled in by the page handler so the Hit knows what was served.
type Info struct {
	HitID  uuid.UUID
	PageID string
	Locale string
	Format string
	// Skip suppresses recording (e.g. a request that is not a public resource after all).
	Skip bool
}

type infoKey struct{}

// WithInfo attaches a fresh Info (with a new Hit id) without recording anything.
func WithInfo(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), infoKey{}, &Info{HitID: newID(), Format: "html"})))
	})
}

// InfoFrom returns the Info of a tracked request, or a throwaway one.
func InfoFrom(ctx context.Context) *Info {
	if i, ok := ctx.Value(infoKey{}).(*Info); ok {
		return i
	}
	return &Info{}
}

type identity struct {
	ip       netip.Addr
	ua       string
	cls      visits.Classification
	visitor  *string
	daily    string
	internal bool
	country  *string
}

func (t *Tracker) identify(w http.ResponseWriter, r *http.Request, now time.Time, mayIssue bool) identity {
	id := identity{ip: ClientIP(r), ua: visits.Clean(r.UserAgent(), 1024)}
	id.cls = visits.Classify(id.ua, id.ip, t.Ranges)
	id.daily = t.Hasher.Hash(now, id.ip, id.ua)
	id.internal = t.Internal != nil && t.Internal(r)
	if c := strings.ToUpper(visits.Clean(r.Header.Get("CF-IPCountry"), 2)); len(c) == 2 {
		id.country = &c
	}
	if !visits.WantsVisitorID(id.cls.Kind, visits.OptedOut(r.Header.Get("Sec-GPC"), r.Header.Get("DNT"))) {
		return id
	}
	if c, err := r.Cookie(visits.VisitorCookie); err == nil && visits.ValidVisitorID(c.Value) {
		v := c.Value
		id.visitor = &v
		return id
	}
	if !mayIssue {
		return id
	}
	if v, err := visits.NewVisitorID(t.Rand); err == nil {
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure is off only for plain-HTTP local development
			Name: visits.VisitorCookie, Value: v, Path: "/", MaxAge: int(visits.VisitorCookieMaxAge.Seconds()),
			Secure: t.SecureCookies, HttpOnly: true, SameSite: http.SameSiteLaxMode,
		})
		id.visitor = &v
	}
	return id
}

// ClientIP is the Cloudflare-reported client address, else the TCP peer. It is never stored.
func ClientIP(r *http.Request) netip.Addr {
	if ip, err := netip.ParseAddr(strings.TrimSpace(r.Header.Get("CF-Connecting-IP"))); err == nil {
		return ip.Unmap()
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip, _ := netip.ParseAddr(host)
	return ip.Unmap()
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// Middleware records one Hit per request after the handler has answered.
func (t *Tracker) Middleware(next http.Handler) http.Handler { return t.track(next, "html", true) }

// Images records image requests as Hits of format img; they never issue a Visitor ID, so cached
// responses carry no Set-Cookie.
func (t *Tracker) Images(next http.Handler) http.Handler { return t.track(next, "img", false) }

func (t *Tracker) track(next http.Handler, format string, mayIssue bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := t.Now()
		info := &Info{HitID: newID(), Format: format}
		id := t.identify(w, r, now, mayIssue)
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r.WithContext(context.WithValue(r.Context(), infoKey{}, info)))
		if sw.status == 0 {
			sw.status = http.StatusOK
		}
		res, ok := visits.ClassifyResource(r.URL.Path, info.Format, sw.status)
		if info.Skip || !ok {
			return
		}
		t.Recorder.Enqueue(Op{Hit: t.hit(r, now, info, id, sw.status, res)})
	})
}

func (t *Tracker) hit(r *http.Request, now time.Time, info *Info, id identity, status int, res visits.Resource) *queries.InsertHitParams {
	arr := visits.ClassifyArrival(r.Referer(), r.URL.Query(), t.Host)
	h := &queries.InsertHitParams{
		ID: info.HitID, Ts: now, Path: visits.Clean(r.URL.Path, 1024), PageID: ptr(info.PageID), Locale: ptr(info.Locale),
		ReleaseLabel: t.Release, AppVersion: t.AppVersion, Resource: string(res),
		Format: info.Format, Status: int64(status), VisitorKind: string(id.cls.Kind),
		BotName: ptr(id.cls.BotName), ReferrerHost: ptr(arr.ReferrerHost), UtmSource: ptr(arr.UTM.Source),
		UtmMedium: ptr(arr.UTM.Medium), UtmCampaign: ptr(arr.UTM.Campaign), UtmTerm: ptr(arr.UTM.Term), UtmContent: ptr(arr.UTM.Content),
		Country: id.country, UserAgent: id.ua, VisitorID: id.visitor, DailyHash: id.daily, Internal: id.internal,
	}
	if id.cls.Kind.IsHuman() {
		ch := string(arr.Channel)
		h.ArrivalChannel, h.ReferrerName = &ch, ptr(arr.ReferrerName)
	}
	h.Verified = verified(id.cls)
	return h
}

// Beacon handles POST /b: {"h": hit id, "e": engaged ms}. It always answers 204.
func (t *Tracker) Beacon(w http.ResponseWriter, r *http.Request) {
	defer w.WriteHeader(http.StatusNoContent)
	var body struct {
		H string `json:"h"`
		E int64  `json:"e"`
	}
	if json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&body) != nil {
		return
	}
	id, err := uuid.Parse(body.H)
	if err != nil {
		return
	}
	engaged := min(max(body.E, 0), int64(6*time.Hour/time.Millisecond))
	t.Recorder.Enqueue(Op{Beacon: &queries.ConfirmBeaconParams{ID: id, EngagedMs: engaged, NotBefore: t.Now().Add(-24 * time.Hour)}})
}

// Go handles GET /go?to=<official url>&from=<mirror path>: record an Outbound Click, then 302.
func (t *Tracker) Go(allowed func(string) bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		to := r.URL.Query().Get("to")
		if !allowed(to) {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		now := t.Now()
		id := t.identify(w, r, now, false)
		from := visits.Clean(r.URL.Query().Get("from"), 1024)
		click := &queries.InsertOutboundClickParams{
			ID: newID(), Ts: now, FromPath: from, ReleaseLabel: t.Release, AppVersion: t.AppVersion, Target: visits.Clean(to, 2048),
			VisitorKind: string(id.cls.Kind), BotName: ptr(id.cls.BotName), VisitorID: id.visitor, DailyHash: id.daily,
			Internal: id.internal, Country: id.country,
		}
		if pageID, locale, ok := t.Resolve(from); ok {
			click.PageID, click.Locale = &pageID, &locale
		}
		click.Verified = verified(id.cls)
		t.Recorder.Enqueue(Op{Click: click})
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Robots-Tag", "noindex")
		http.Redirect(w, r, to, http.StatusFound) //nolint:gosec // to passed the tribelt.nl allowlist above
	}
}

// verified is set only for claimed crawlers whose operator publishes ranges to check against.
func verified(c visits.Classification) *bool {
	if c.Operator == "" {
		return nil
	}
	v := c.Verified
	return &v
}

func newID() uuid.UUID {
	id, err := uuid.NewV7()
	if err != nil {
		return uuid.New()
	}
	return id
}

func ptr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// DefaultRand is the entropy source for Visitor IDs.
func DefaultRand() io.Reader { return rand.Reader }
