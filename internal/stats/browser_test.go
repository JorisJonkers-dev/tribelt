package stats

import (
	"testing"
)

// Fixed clock: noon on 30 Sept 2026, Amsterdam.
func TestAUserAgentIsNamedForItsBrowserFamily(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 Chrome/140 Safari/537.36 Edg/140": "Edge", "Mozilla/5.0 Firefox/142": "Firefox", "Mozilla/5.0 Version/18 Safari/604.1": "Safari",
		"curl/8": "other", "Mozilla/5.0 Chrome/1 Safari/1 OPR/1": "Opera",
	} {
		t.Run(ua, func(t *testing.T) {
			if got := BrowserFamily(ua); got != want {
				t.Fatalf("BrowserFamily = %s, want %s", got, want)
			}
		})
	}
}
