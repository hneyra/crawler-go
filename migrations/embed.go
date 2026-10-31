// Package migrations = goose sql files, embedded in the binary.
// rules: never edit an applied file. new change = new file with a higher version
// (goose refuses out-of-order). ranges: 00001-09 platform, 00010-19 crawl,
// 00020-29 identity, 00030-39 jobs.
package migrations

import "embed"

// FS holds *.sql.
//
//go:embed *.sql
var FS embed.FS
