// Package migrations = goose sql files, embedded in the binary.
// rules: never edit an applied file. new change = new file. ranges per module:
// 00001-00009 platform/identity, 00010-00019 crawl, 00020-00029 jobs.
package migrations

import "embed"

// FS holds *.sql.
//
//go:embed *.sql
var FS embed.FS
