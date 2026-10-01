// Package tribelt embeds the content directory so one binary carries a whole Content Release, and the
// release-please manifest so the binary knows its App Version without build arguments.
package tribelt

import (
	"embed"
	"encoding/json"
)

// Content is content/ at build time.
//
//go:embed content
var Content embed.FS

//go:embed .release-please-manifest.json
var manifest []byte

// Version is the App Version release-please last cut, or "dev" when the manifest is unreadable.
func Version() string { return versionOf(manifest) }

func versionOf(raw []byte) string {
	var m map[string]string
	if json.Unmarshal(raw, &m) != nil || m["."] == "" {
		return "dev"
	}
	return m["."]
}
