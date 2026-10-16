package stats

import (
	"testing"
)

func TestASessionsDeviceIsNamedForItsBrowserAndSystem(t *testing.T) {
	for ua, want := range map[string]string{
		"Mozilla/5.0 (Windows NT 10.0) Chrome/140 Edg/140":    "Edge on Windows",
		"Mozilla/5.0 (X11; Linux x86_64; rv:130) Firefox/130": "Firefox on Linux",
		"Mozilla/5.0 (Linux; Android 14) Chrome/140":          "Chrome on Android",
		"curl/8": "Browser",
	} {
		t.Run(want, func(t *testing.T) {
			if got := device(ua); got != want {
				t.Fatalf("device(%q) = %q", ua, got)
			}
		})
	}
}
