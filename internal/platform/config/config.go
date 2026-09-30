// Package config reads the runtime environment contract.
package config

import (
	"errors"
	"net/url"
	"strings"
)

// Config is the whole runtime contract; see README "Environment".
type Config struct {
	Addr          string
	DatabaseURL   string // empty: libpq variables (PGHOST, PGUSER, PGPASSWORD, ...)
	ContentDir    string // empty: the content embedded at build time
	BaseURL       string
	Production    bool
	AutoMigrate   bool
	DevAuthBypass bool

	OIDCIssuer, OIDCClientID, OIDCClientSecret, OIDCRedirectURL string

	SessionKey     string
	VisitorHMACKey string

	GSCServiceAccountJSON, GSCSiteURL string
	BingAPIKey, BingSiteURL           string
}

// OIDCConfigured reports whether every OIDC variable is present.
func (c Config) OIDCConfigured() bool {
	return c.OIDCIssuer != "" && c.OIDCClientID != "" && c.OIDCClientSecret != "" && c.OIDCRedirectURL != ""
}

// Host is the host of BASE_URL.
func (c Config) Host() string {
	u, _ := url.Parse(c.BaseURL)
	return u.Hostname()
}

// HTTPS reports whether the site is served over TLS (cookies get Secure, HSTS is sent).
func (c Config) HTTPS() bool { return strings.HasPrefix(c.BaseURL, "https://") }

// Load reads the environment through getenv. Optional values may arrive as empty strings.
func Load(getenv func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	c := Config{
		Addr:                  get("TRIBELT_ADDR", ":8080"),
		DatabaseURL:           get("DATABASE_URL", ""),
		ContentDir:            get("CONTENT_DIR", ""),
		BaseURL:               strings.TrimSuffix(get("BASE_URL", "http://localhost:8080"), "/"),
		Production:            get("TRIBELT_ENV", "") == "production",
		AutoMigrate:           get("AUTO_MIGRATE", "true") == "true",
		DevAuthBypass:         get("DEV_AUTH_BYPASS", "") == "1",
		OIDCIssuer:            get("OIDC_ISSUER", ""),
		OIDCClientID:          get("OIDC_CLIENT_ID", ""),
		OIDCClientSecret:      get("OIDC_CLIENT_SECRET", ""),
		OIDCRedirectURL:       get("OIDC_REDIRECT_URL", ""),
		SessionKey:            get("SESSION_KEY", ""),
		VisitorHMACKey:        get("VISITOR_HMAC_KEY", ""),
		GSCServiceAccountJSON: get("GSC_SERVICE_ACCOUNT_JSON", ""),
		GSCSiteURL:            get("GSC_SITE_URL", ""),
		BingAPIKey:            get("BING_API_KEY", ""),
		BingSiteURL:           get("BING_SITE_URL", ""),
	}
	var errs []error
	if u, err := url.Parse(c.BaseURL); err != nil || u.Host == "" {
		errs = append(errs, errors.New("BASE_URL must be an absolute URL"))
	}
	if c.Production {
		if c.DevAuthBypass {
			errs = append(errs, errors.New("DEV_AUTH_BYPASS is refused in production"))
		}
		if !c.OIDCConfigured() {
			errs = append(errs, errors.New("production needs OIDC_ISSUER, OIDC_CLIENT_ID, OIDC_CLIENT_SECRET and OIDC_REDIRECT_URL"))
		}
		if c.VisitorHMACKey == "" {
			errs = append(errs, errors.New("production needs VISITOR_HMAC_KEY"))
		}
		if !c.HTTPS() {
			errs = append(errs, errors.New("production needs an https BASE_URL"))
		}
	}
	if c.OIDCConfigured() && len(c.SessionKey) < 32 {
		errs = append(errs, errors.New("SESSION_KEY of at least 32 characters is required with OIDC"))
	}
	if c.VisitorHMACKey == "" {
		c.VisitorHMACKey = "development-only-visitor-key"
	}
	return c, errors.Join(errs...)
}
