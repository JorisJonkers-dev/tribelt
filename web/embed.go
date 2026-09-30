// Package web embeds the HTML templates and static assets.
package web

import "embed"

// Templates holds the public and stats templates.
//
//go:embed templates/*.html
var Templates embed.FS

// Static holds CSS and the small scripts (beacon, chart PNG export).
//
//go:embed static/*
var Static embed.FS
