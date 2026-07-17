// Package web embeds the static frontend assets into the binary so the Go
// server can serve the single-page app alongside the API (same origin).
package web

import "embed"

// Files holds the frontend: index.html plus the static/ and terms/ directories
// (app.js, styles.css, the vendored keycloak-js adapter, and T&C pages).
//
//go:embed index.html error.html static terms/v1.html
var Files embed.FS
