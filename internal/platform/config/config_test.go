package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestWithNothingSetTheServiceRunsLocallyOnPort8080(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || !c.AutoMigrate || c.Production || c.HTTPS() || c.Host() != "localhost" || c.OIDCConfigured() || c.VisitorHMACKey == "" {
		t.Fatalf("%+v", c)
	}
}

func TestDevAuthBypassTurnsOnForOneOrViewerOnly(t *testing.T) {
	for v, want := range map[string][2]bool{"1": {true, false}, "viewer": {true, true}, "yes": {false, false}, " ": {false, false}} {
		t.Run(v, func(t *testing.T) {
			c, err := Load(env(map[string]string{"DEV_AUTH_BYPASS": v}))
			if err != nil {
				t.Fatal(err)
			}
			if c.DevAuthBypass != want[0] || c.DevAuthViewer != want[1] {
				t.Fatalf("bypass %v, viewer %v", c.DevAuthBypass, c.DevAuthViewer)
			}
		})
	}
}

// production is every variable a production run needs.
func production() map[string]string {
	return map[string]string{
		"TRIBELT_ENV": "production", "BASE_URL": "https://tribelt.jorisjonkers.dev/", "OIDC_ISSUER": "https://auth.jorisjonkers.dev",
		"OIDC_CLIENT_ID": "tribelt", "OIDC_CLIENT_SECRET": "s", "OIDC_REDIRECT_URL": "https://tribelt.jorisjonkers.dev/auth/callback",
		"SESSION_KEY": strings.Repeat("a", 64), "VISITOR_HMAC_KEY": strings.Repeat("b", 64), "GSC_SERVICE_ACCOUNT_JSON": "", "BING_API_KEY": "",
	}
}

func TestAFullProductionEnvironmentLoadsOverHTTPS(t *testing.T) {
	c, err := Load(env(production()))
	if err != nil {
		t.Fatal(err)
	}
	if !c.HTTPS() || c.BaseURL != "https://tribelt.jorisjonkers.dev" || c.GSCServiceAccountJSON != "" {
		t.Fatalf("%+v", c)
	}
}

func TestProductionRefusesAnUnsafeOrIncompleteEnvironment(t *testing.T) {
	for name, change := range map[string][2]string{
		"auth bypassed":           {"DEV_AUTH_BYPASS", "1"},
		"auth bypassed as viewer": {"DEV_AUTH_BYPASS", "viewer"},
		"no OIDC client secret":   {"OIDC_CLIENT_SECRET", ""},
		"no visitor HMAC key":     {"VISITOR_HMAC_KEY", ""},
		"a short session key":     {"SESSION_KEY", "short"},
		"a base URL over HTTP":    {"BASE_URL", "http://tribelt.jorisjonkers.dev"},
		"a base URL that is not":  {"BASE_URL", "::"},
	} {
		t.Run(name, func(t *testing.T) {
			m := production()
			m[change[0]] = change[1]
			if _, err := Load(env(m)); err == nil {
				t.Fatal("loaded")
			}
		})
	}
}
