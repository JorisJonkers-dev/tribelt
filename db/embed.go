// Package db embeds the SQL migrations so the binary can prepare its own database.
package db

import "embed"

// Migrations holds the goose migrations.
//
//go:embed migrations/*.sql
var Migrations embed.FS
