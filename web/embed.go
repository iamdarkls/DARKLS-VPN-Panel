// Package web supplies the self-contained interface embedded in the binary.
package web

import "embed"

// Files excludes Go source and is served through the panel's explicit allowlist.
//
//go:embed index.html js css
var Files embed.FS
