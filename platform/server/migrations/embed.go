// Package migrations embeds the numbered SQL schema files so the server
// binary can apply them without the repo checkout at runtime.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
