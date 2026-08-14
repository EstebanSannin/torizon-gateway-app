// Package web embeds all UI assets (templates + static files) into the binary
// so the whole application ships as a single artifact with no external web
// server and no CDN dependency. Edit a template/CSS and rebuild — no JS build step.
package web

import "embed"

// Templates holds server-rendered HTML (full pages + HTMX fragments).
//
//go:embed templates
var Templates embed.FS

// Static holds CSS, brand SVGs, and vendored JS (htmx, alpine) + fonts.
//
//go:embed static
var Static embed.FS
