// Package migrations embeds the SQL migration files into the binary so the
// API can migrate the database on startup without the files on disk.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
