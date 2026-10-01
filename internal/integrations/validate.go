package integrations

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
)

// Kind is one Integration.
type Kind string

const (
	Google     Kind = "google"
	Bing       Kind = "bing"
	IndexNow   Kind = "indexnow"
	Cloudflare Kind = "cloudflare"
)

// Kinds lists every Integration in display order.
func Kinds() []Kind { return []Kind{Google, Bing, IndexNow, Cloudflare} }

// ParseKind accepts only a known kind.
func ParseKind(s string) (Kind, bool) {
	for _, k := range Kinds() {
		if string(k) == s {
			return k, true
		}
	}
	return "", false
}

// MaxCredential bounds every pasted or uploaded credential.
const MaxCredential = 64 << 10

// ServiceAccount is the part of a Google service account key the mirror reads.
type ServiceAccount struct {
	Type        string `json:"type"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
}

// ParseServiceAccount accepts a service account JSON key and nothing else (no OAuth client, no API key).
func ParseServiceAccount(raw []byte) (ServiceAccount, error) {
	var sa ServiceAccount
	if len(raw) == 0 || len(raw) > MaxCredential {
		return sa, Fail(CodeInvalid)
	}
	if err := json.Unmarshal(raw, &sa); err != nil {
		return sa, Fail(CodeInvalidJSON)
	}
	if sa.Type != "service_account" {
		return sa, Fail(CodeNotServiceAccount)
	}
	if !strings.Contains(sa.ClientEmail, "@") || !strings.Contains(sa.PrivateKey, "PRIVATE KEY") {
		return sa, Fail(CodeIncompleteKey)
	}
	return sa, nil
}

var (
	apiKeyPattern  = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]{16,256}$`)
	zonePattern    = regexp.MustCompile(`^[0-9a-f]{32}$`)
	indexNowKeyPat = regexp.MustCompile(`^[A-Za-z0-9-]{8,128}$`)
)

// ValidAPIKey accepts a Bing API key or a Cloudflare API token: one opaque printable token.
func ValidAPIKey(s string) bool { return apiKeyPattern.MatchString(s) }

// ValidZoneID accepts a Cloudflare zone id (32 lowercase hex).
func ValidZoneID(s string) bool { return zonePattern.MatchString(s) }

// ValidIndexNowKey is the protocol's key alphabet and length.
func ValidIndexNowKey(s string) bool { return indexNowKeyPat.MatchString(s) }

// NewIndexNowKey is 32 random hex characters.
func NewIndexNowKey() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
