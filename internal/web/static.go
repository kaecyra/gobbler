package web

import "embed"

// staticFS holds the vendored HTMX and the stylesheet. Nothing is loaded from
// a third-party origin at runtime (ADR-00002).
//
//go:embed static
var staticFS embed.FS
