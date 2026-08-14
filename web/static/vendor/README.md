# Vendored frontend assets (offline / air-gapped)

The target device has no internet, so **all** frontend dependencies live here, committed, and are embedded into the binary via `//go:embed`. No CDNs.

| File | Purpose | Source |
|------|---------|--------|
| `htmx.min.js` | Dynamic partial updates without hand-written JS | unpkg.com/htmx.org |
| `htmx-ext-sse.js` | Server-Sent Events extension (live metrics/logs) | unpkg.com/htmx-ext-sse |
| `alpine.min.js` | Small local interactivity (modals, toggles) | unpkg.com/alpinejs |
| `inter/` | **TODO** — Inter WOFF2 font files (Light 300, Regular 400, Bold 700) | fonts.google.com/specimen/Inter |

Refresh the JS libs with `make vendor-ui`. For Inter, download the WOFF2 files into `inter/` and add an `@font-face` block in `../css/tokens.css` (or a small `fonts.css`). Until then the UI falls back to `system-ui`.
