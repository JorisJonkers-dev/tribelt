package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestDefaults(t *testing.T) {
	c, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if c.Addr != ":8080" || !c.AutoMigrate || c.Production || c.HTTPS() || c.Host() != "localhost" || c.OIDCConfigured() || c.VisitorHMACKey == "" {
		t.Fatalf("%+v", c)
	}
	for v, want := range map[string][2]bool{"1": {true, false}, "viewer": {true, true}, "yes": {false, false}, " ": {false, false}} {
		c, _ := Load(env(map[string]string{"DEV_AUTH_BYPASS": v}))
		if c.DevAuthBypass != want[0] || c.DevAuthViewer != want[1] {
			t.Errorf("DEV_AUTH_BYPASS=%q: %v %v", v, c.DevAuthBypass, c.DevAuthViewer)
		}
	}
}

func TestProductionContract(t *testing.T) {
	full := map[string]string{
		"TRIBELT_ENV": "production", "BASE_URL": "https://tribelt.jorisjonkers.dev/", "OIDC_ISSUER": "https://auth.jorisjonkers.dev",
		"OIDC_CLIENT_ID": "tribelt", "OIDC_CLIENT_SECRET": "s", "OIDC_REDIRECT_URL": "https://tribelt.jorisjonkers.dev/auth/callback",
		"SESSION_KEY": strings.Repeat("a", 64), "VISITOR_HMAC_KEY": strings.Repeat("b", 64), "GSC_SERVICE_ACCOUNT_JSON": "", "BING_API_KEY": "",
	}
	c, err := Load(env(full))
	if err != nil {
		t.Fatal(err)
	}
	if !c.HTTPS() || c.BaseURL != "https://tribelt.jorisjonkers.dev" || c.GSCServiceAccountJSON != "" {
		t.Fatalf("%+v", c)
	}
	for _, mutate := range []func(m map[string]string){
		func(m map[string]string) { m["DEV_AUTH_BYPASS"] = "1" },
		func(m map[string]string) { m["DEV_AUTH_BYPASS"] = "viewer" },
		func(m map[string]string) { m["OIDC_CLIENT_SECRET"] = "" },
		func(m map[string]string) { m["VISITOR_HMAC_KEY"] = "" },
		func(m map[string]string) { m["SESSION_KEY"] = "short" },
		func(m map[string]string) { m["BASE_URL"] = "http://tribelt.jorisjonkers.dev" },
		func(m map[string]string) { m["BASE_URL"] = "::" },
	} {
		m := map[string]string{}
		for k, v := range full {
			m[k] = v
		}
		mutate(m)
		if _, err := Load(env(m)); err == nil {
			t.Fatalf("expected error for %v", m)
		}
	}
}
