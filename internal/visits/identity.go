package visits

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/netip"
	"sync"
	"time"
	_ "time/tzdata" // Europe/Amsterdam must resolve in a distroless image
)

// VisitorCookie is the first-party Visitor ID cookie (ADR-0003).
const VisitorCookie = "tv"

// VisitorCookieMaxAge is 13 months; the cookie is never extended.
const VisitorCookieMaxAge = 395 * 24 * time.Hour

// OptedOut reports whether the client asked not to be tracked through GPC or DNT.
func OptedOut(secGPC, dnt string) bool { return secGPC == "1" || dnt == "1" }

// WantsVisitorID reports whether a Hit of this kind may carry a Visitor ID cookie.
func WantsVisitorID(k Kind, optedOut bool) bool { return k.IsHuman() && !optedOut }

// NewVisitorID returns a random 128-bit identifier.
func NewVisitorID(rand io.Reader) (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ValidVisitorID accepts only identifiers NewVisitorID could have produced.
func ValidVisitorID(s string) bool {
	if len(s) != 22 {
		return false
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	return err == nil && len(b) == 16
}

// DailyHasher computes the Daily Visitor hash. The salt for an Amsterdam day is derived from the
// key and held only in memory, so hashes cannot be linked across days without the key.
type DailyHasher struct {
	key []byte
	loc *time.Location

	mu   sync.Mutex
	day  string
	salt []byte
}

// NewDailyHasher builds a hasher for the Europe/Amsterdam calendar.
func NewDailyHasher(key []byte) *DailyHasher {
	loc, _ := time.LoadLocation("Europe/Amsterdam")
	return &DailyHasher{key: key, loc: loc}
}

// Day is the Amsterdam calendar day of t.
func (h *DailyHasher) Day(t time.Time) string { return t.In(h.loc).Format(time.DateOnly) }

func (h *DailyHasher) saltFor(t time.Time) []byte {
	day := h.Day(t)
	h.mu.Lock()
	defer h.mu.Unlock()
	if day != h.day {
		m := hmac.New(sha256.New, h.key)
		m.Write([]byte("daily-visitor-salt:" + day))
		h.day, h.salt = day, m.Sum(nil)
	}
	return h.salt
}

// Hash returns the Daily Visitor hash of a client address and user-agent at time t.
func (h *DailyHasher) Hash(t time.Time, ip netip.Addr, ua string) string {
	m := hmac.New(sha256.New, h.saltFor(t))
	m.Write([]byte(ip.String()))
	m.Write([]byte{0})
	m.Write([]byte(ua))
	return hex.EncodeToString(m.Sum(nil)[:16])
}
