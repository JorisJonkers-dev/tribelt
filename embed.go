// Package tribelt embeds the content directory so one binary carries a whole Content Release.
package tribelt

import "embed"

// Content is content/ at build time.
//
//go:embed content
var Content embed.FS
