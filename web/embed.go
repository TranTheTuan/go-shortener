// Package web embeds the static frontend assets into the binary so the Go
// server can serve the single-page app alongside the API (same origin).
package web

import "embed"

// Files holds the frontend: index.html plus the static/ directory (app.js,
// styles.css, the vendored keycloak-js adapter).
//
//go:embed index.html error.html static
var Files embed.FS
