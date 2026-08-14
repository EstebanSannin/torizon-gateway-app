# CLAUDE.md — Knowledge base for AI-assisted contributors

> **Read this first.** This file is auto-loaded by Claude Code and similar AI tools. It is the durable knowledge base for anyone (human or AI) adding features to the Torizon Gateway app. Keep it up to date when you change architecture or conventions.

## What this project is

An **on-device web management application** for Toradex modules running **Torizon OS**. First target is the **Verdin** family for the **Toradex Zinnia** gateway product, but it must stay **hardware-agnostic** across Torizon. From a browser on the local network an operator can inspect the board, configure networking, manage containers, and apply offline updates — no shell needed.

Full design: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). Brand/UI: [`docs/DESIGN-SYSTEM.md`](docs/DESIGN-SYSTEM.md).

## Non-negotiable invariants (do not violate without updating the architecture doc)

1. **Single artifact.** One Go binary with the **UI embedded** via `//go:embed` (`web/embed.go`). No separate web server, no Node runtime, no build step for the frontend.
2. **Offline-first / air-gapped.** No CDNs, no external fonts/CSS/JS. Everything is **vendored** under `web/static/vendor/` and embedded. `make vendor-ui` fetches htmx/Alpine; Inter fonts go in `web/static/vendor/inter/`.
3. **Build-less frontend.** Server-rendered Go HTML templates + **HTMX** (dynamic updates) + **Alpine.js** (local interactivity) + **SSE** (live data). No React/SPA. No client-side build.
4. **Host is authoritative.** Never keep a second copy of network/container config. Read/apply against the host (NetworkManager, Docker) live. The app's SQLite store holds **only** app-owned data (accounts, settings, audit).
5. **Hardware-agnostic via the HAL.** All board-specific reads go through `internal/hal` (`BoardInfo` interface). Adding a SoM = one new HAL implementation selected by capability probe in `Detect()` — never hard-code per product elsewhere.
6. **No blanket `--privileged`.** Grant only the specific mounts in `deploy/docker-compose.yml`. See the privilege model in ARCHITECTURE §5.
7. **Security is not optional.** Real auth (argon2id), HTTPS only, CSRF on state-changing requests, and an **audit record for every mutation**. No default credentials, ever.
8. **Use design tokens.** Never hard-code a color/font in a template. Reference the CSS variables in `web/static/css/tokens.css`.

## Repository map

```
cmd/gateway-manager/main.go   Entrypoint: config → HAL → TLS → HTTPS server → graceful shutdown
internal/config/              Env-based config with defaults
internal/hal/                 Hardware Abstraction Layer (toradex / generic), capability probe
internal/httpserver/          Router, template rendering, TLS self-signed, SSE. Handlers live here.
internal/auth/                [roadmap] first-boot setup, argon2id, sessions, CSRF
internal/network/             [roadmap] NetworkManager via D-Bus, confirm-or-revert
internal/containers/          [roadmap] Docker socket client
internal/updates/             [roadmap] Torizon Secure Offline Updates (Lockbox)
internal/store/               [roadmap] SQLite (pure-Go): users, settings, audit
web/embed.go                  //go:embed of templates + static
web/templates/                base.html + one file per page ({{define "content"}})
web/static/css/               tokens.css (brand) + app.css (components)
web/static/brand/             Official Torizon SVGs (avatar, logo, dark, tagline)
web/static/vendor/            Vendored htmx, htmx-ext-sse, alpine (+ inter fonts)
deploy/                       Dockerfile (multi-arch) + docker-compose.yml (Torizon app)
docs/                         ARCHITECTURE.md, DESIGN-SYSTEM.md
```

Packages marked **[roadmap]** currently contain a `doc.go` sketching the intended interface. Implement against that sketch and update it as it solidifies.

## Conventions

- **Language:** Go, stdlib-first. The Phase-0 scaffold has **zero external deps** so it builds anywhere. Planned deps are listed (commented) in `go.mod`; add one only with its feature.
- **Routing:** Go 1.22+ `net/http.ServeMux` method+pattern routes (`"GET /network"`). A `chi` migration is fine when middleware grows — noted in ARCHITECTURE §6.
- **Templates:** each page defines `{{define "content"}}`; `render(w, "page.html", data)` composes it with `base.html`. Simple pages can use `renderInline`.
- **Live data:** push via SSE from a handler (`GET /sse/...`) as named events; the template swaps them with `sse-swap="name"`. See `handleMetricsSSE`.
- **Errors:** return them; log at the edges. Don't panic in handlers.
- **Naming/style:** match surrounding code; run `make fmt vet` before committing.

## How to add a feature (recipe)

1. **Model the domain** in its `internal/<domain>` package behind an interface (see the `doc.go` sketch). Keep host access (D-Bus/socket) inside this package.
2. **Wire a route** in `internal/httpserver/server.go` and add a handler (own file, e.g. `handlers_network.go`).
3. **Add a template** `web/templates/<page>.html` with `{{define "content"}}`; use tokens + existing `.card/.grid/.badge/.btn/table` classes.
4. **Live updates?** add a `GET /sse/<domain>` handler and `sse-swap` targets.
5. **Mutations?** require auth, add CSRF, write an **audit** record, and for risky changes add a confirm step (network = confirm-or-revert anti-lockout).
6. **Register the package** in `main.go` if it needs a dependency (store, docker client, etc.).
7. **Tests:** put HAL/host-interaction behind interfaces and test with fixtures under `test/`.
8. **Docs:** update ARCHITECTURE if you changed a decision; update this file if you changed a convention.

## Security rules for contributors

- Treat the **Docker socket** and **system D-Bus** as root-equivalent. Validate all inputs; expose the narrowest host surface. Consider the socket/D-Bus proxy backlog items before GA.
- Every state-changing endpoint: **authenticated + CSRF-protected + audited**.
- **Network changes to the active interface must be revertible** (confirm-or-revert) to avoid locking the operator out.
- Offline updates: rely on the **host** for signature verification and rollback. Do not reimplement signing.

## Build & run

```bash
make vendor-ui   # once: fetch htmx/alpine into web/static/vendor (commit them)
make run         # build + run locally over HTTPS on :8443 (self-signed cert in ./.localdata)
make image-multiarch   # arm64 (Verdin/Zinnia) + amd64 container
```

Note: the target device is offline — the frontend deps must already be vendored and committed.

## Current status & where to look next

Phase-0 scaffold: HTTPS server + first-boot self-signed cert + embedded HTMX dashboard reading the HAL. Network/Containers/Updates pages are placeholders. See the **roadmap and backlog** in [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md#15-roadmap--phasing).
