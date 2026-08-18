# CLAUDE.md — Knowledge base for AI-assisted contributors

> **Read this first.** Auto-loaded by Claude Code and similar tools. The durable knowledge base for anyone (human or AI) adding features to the Torizon Gateway app. Keep it current when you change architecture or conventions.

## What this project is

An **on-device web management application** for Toradex modules running **Torizon OS**. First target is the **Verdin** family for the **Toradex Zinnia** gateway product, but it stays **hardware-agnostic** across Torizon. From a browser on the local network an operator can inspect the board, configure networking, manage containers, read logs, browse/edit files, open a shell, and see Torizon Cloud status — no SSH needed.

Full design: [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md). Brand/UI: [`docs/DESIGN-SYSTEM.md`](docs/DESIGN-SYSTEM.md).

## Non-negotiable invariants (don't violate without updating the architecture doc)

1. **Single artifact.** One Go binary with the UI embedded via `//go:embed` (`web/embed.go`). No separate web server, no Node runtime, no frontend build step.
2. **Offline-first / air-gapped.** No CDNs, no external fonts/CSS/JS. Everything vendored under `web/static/vendor/` (htmx, htmx-ext-sse, alpine, **xterm**, **Inter** font) and embedded.
3. **Build-less frontend.** Server-rendered Go templates + **HTMX** (dynamic updates + polling) + **Alpine.js** (local interactivity) + **SSE** (live streams) + **xterm.js** (terminal). No React/SPA, no client build.
4. **Host is authoritative.** Never keep a second copy of network/container config. Read/apply against the host live. The app's SQLite store holds **only** app-owned data (accounts, sessions, audit).
5. **Hardware-agnostic via the HAL.** All board-specific reads go through `internal/hal` (`BoardInfo`), selected by capability probe in `Detect()`. The HAL is **host-root aware** (`SetHostRoot`) so it reads real host files under `/host` in a container or `/` natively.
6. **No blanket `--privileged`.** Grant specific access (mounts, host networking, group_add). Docker socket and system D-Bus are root-equivalent — treat as such.
7. **Security is not optional.** argon2id auth, HTTPS only, CSRF on every state-changing request, an **audit record for every mutation**. No default credentials. Risky "power" features (file writes, terminal) are **off by default** behind env switches.
8. **Use design tokens.** Never hard-code a color/font in a template. Reference `web/static/css/tokens.css`.
9. **Deployment-agnostic (native *or* container).** Torizon runs a single docker-compose and wipes other containers when a customer app deploys, so the product ships **native via Yocto** (systemd service) in production. The SAME binary runs both ways: host paths resolve via `GATEWAY_HOST_ROOT` (`/host` mount vs native `/`), all paths are env-configurable, dependencies are **pure-Go / no cgo** so the Yocto recipe stays trivial. See ARCHITECTURE §13.

## Repository map

```
cmd/gateway-manager/main.go   Entrypoint: config → HAL → store → services → TLS → HTTPS → graceful shutdown
internal/config/              Env-based config with defaults (all GATEWAY_* vars)
internal/hal/                 HAL (toradex/generic), capability probe, host-root-aware path resolution
internal/httpserver/          Router (stdlib ServeMux), template render, TLS, SSE, all handlers
internal/auth/                first-boot setup, argon2id (hash.go), sessions
internal/store/               SQLite (modernc, pure-Go): users, sessions, audit
internal/network/             NetworkManager over D-Bus (godbus): read + IPv4 edit w/ checkpoint confirm-or-revert
internal/containers/          Docker Engine over the socket via a tiny stdlib HTTP client (NOT the docker SDK): list, logs, start/stop/restart
internal/sysinfo/             Pure-Go sysfs/proc readers: disk, peripherals (USB/block/CAN/serial/i2c/spi/gpio), cpu, net counters, default iface
internal/logs/                systemd journal + kernel via journalctl (host binary in the container — see "host-binary exec")
internal/files/               host filesystem browse (read-only, traversal-safe) + edit/upload/delete confined to /etc,/var
internal/terminal/            web SSH shell: x/crypto/ssh to localhost, proxied over a WebSocket (gorilla)
internal/cloud/               Torizon Cloud/OTA status via aktualizr-info (host binary) + process status via /proc scan
internal/updates/             [roadmap] offline Lockbox apply; currently the Updates page shows OS version only
web/embed.go                  //go:embed templates + static
web/templates/                base.html + one file per page ({{define "content"}}) + fragment_*.html (htmx-polled)
web/static/css/               tokens.css (brand) + app.css (components)
web/static/brand/             Official Torizon SVGs + torizon-gateway-logo[-dark].svg (product lockup)
web/static/vendor/            htmx, htmx-ext-sse, alpine, xterm/, inter/ (all committed)
deploy/                       Dockerfile (multi-arch, golang:1.26 → distroless) + docker-compose.yml (reference)
docs/                         ARCHITECTURE.md, DESIGN-SYSTEM.md
```

## Conventions

- **Language:** Go, stdlib-first. Deps are pure-Go/no-cgo (see `go.mod`): `modernc.org/sqlite`, `golang.org/x/crypto`, `github.com/godbus/dbus/v5`, `github.com/gorilla/websocket`. Module targets Go 1.25+; Dockerfile builds on `golang:1.26`. Build static with `CGO_ENABLED=0`.
- **Routing:** stdlib `net/http.ServeMux` with method+pattern routes (`"GET /network"`, `"POST /files/save"`). No chi.
- **Templates:** each page defines `{{define "content"}}`; `render(w, "page.html", data)` composes with `base.html`. Fragments (htmx-polled) define their own name and render via `renderFragment(w, file, define, data)`. `renderInline` for tiny inline pages. Template funcs: `hbytes` (any int → human bytes). Every protected page's data **embeds `layout`** (set via `s.layoutFor`) for the nav/user/CSRF/device chip.
- **Live data:** two patterns — **SSE** for high-frequency streams (`/sse/metrics` value+sparkline fragments at 1s; `/sse/logs/{id}`, `/sse/journal`, `/ws/terminal`), and **htmx polling** for periodic HTML fragments (`hx-get=/fragment/... hx-trigger="load, every Ns"` → peripherals 4s, cloud 15s).
- **Mutations:** require auth (`s.requireAuth`), check CSRF (`checkCSRF`), write an audit record (`s.store.AddAudit`). Risky changes (network) use confirm-or-revert.
- **Errors:** return them; log at edges. Don't panic in handlers.
- **Style:** match surrounding code; run `gofmt -w . && go vet ./...` before committing. Keep it human-readable, clean, light (embedded target).

## Key data-access patterns (how the app reaches host state)

- **sysfs / procfs (pure Go):** peripherals, CPU, disk usage, net counters, mounts, process list. `/sys` and most of `/proc` are host-global (USB/block/CPU) or reflect the host under **host networking** (net, routes). Read via `internal/sysinfo`.
- **NetworkManager:** system D-Bus (`godbus`) — read interfaces, edit IPv4, and **checkpoint** create/rollback for anti-lockout.
- **Docker Engine:** unix socket via a **minimal stdlib `net/http` client** (not the heavy docker SDK) — list/logs/start/stop/restart.
- **systemd journal & `aktualizr-info`:** these are host binaries not present in the distroless container. We run them **via the host dynamic loader** against the host filesystem: `"$hostRoot/lib/ld-*.so" --library-path <host libs incl. /usr/lib/systemd> "$hostRoot/usr/bin/<binary>" ...`, with journal at `/host/run/log/journal` and a generated aktualizr config pointing storage at `/host/var/sota`. Natively these are just `journalctl`/`aktualizr-info`. Requires root + `systemd-journal` group. (`GATEWAY_JOURNALCTL` overrides.) Write the generated config to the **data dir**, not `/tmp` (distroless has no writable `/tmp`).
- **Terminal:** SSH to a **fixed** target (`127.0.0.1:22`, never client-supplied) using the user's board credentials; proxied over a WebSocket to xterm.js.
- **Process status (cloud):** scan `<hostRoot>/proc/*/comm` for `aktualizr*` and `rac` — no systemd/D-Bus dependency.

## Deployment (dev container vs native)

The dev loop runs the **container with elevated host access** (see the on-device `~/gateway/docker-compose.yml` and `deploy/docker-compose.yml`):
- `network_mode: host` — needed so `/sys/class/net` shows host interfaces (CAN), and so the self-signed cert auto-detects the LAN IP.
- `user: "0:0"` (root) + `/:/host:ro` (recursive; whole host FS) + `/etc:/host/etc:rw` + `/var:/host/var:rw` — for the host mount table, statfs, journal, aktualizr-info, and confined file writes.
- `/var/run/docker.sock` + `/run/dbus/system_bus_socket` mounts. `group_add` docker/systemd-journal GIDs if not running as root.
- Feature switches: `GATEWAY_FILES_WRITABLE=1`, `GATEWAY_TERMINAL_ENABLED=1` (both **off** by default).

**Production is native (Yocto systemd service, root)** — direct access, no mounts/ld-exec tricks, and it survives customer app deployments (which wipe the single managed docker-compose). See ARCHITECTURE §13.

### Config env vars
`GATEWAY_DATA_DIR` `GATEWAY_LISTEN_ADDR` `GATEWAY_TLS_CERT` `GATEWAY_TLS_KEY` `GATEWAY_TLS_SANS` `GATEWAY_HOSTNAME` `GATEWAY_DOCKER_SOCKET` `GATEWAY_DBUS_SOCKET` `GATEWAY_SYSFS` `GATEWAY_HOST_ROOT` `GATEWAY_FILES_WRITABLE` `GATEWAY_TERMINAL_ENABLED` `GATEWAY_TERMINAL_SSH_HOST` `GATEWAY_SESSION_TTL` `GATEWAY_DEV_MODE`.

## How to add a feature (recipe)

1. **Model the domain** in `internal/<domain>` behind a small type/interface; keep host access (sysfs/D-Bus/socket/exec) inside it.
2. **Wire a route** in `internal/httpserver/server.go`; add a handler (own file, e.g. `handlers_<domain>.go`). Wrap with `s.requireAuth`.
3. **Add a template** `web/templates/<page>.html` with `{{define "content"}}`; embed `layout`; use tokens + existing components (`.card/.tile/.badge/.btn/table/.kv`). For live sections add a `fragment_<x>.html` + `/fragment/<x>` (htmx poll) or an SSE endpoint.
4. **Mutations?** auth + CSRF + audit; risky changes get confirm-or-revert; gate power features behind an env switch (default off).
5. **Register** the service in `main.go` / `New()` if it needs config.
6. **Build & validate on the Verdin** — no Go on the Mac; see the dev-loop below. On-device testing catches real integration bugs (NM quirks, distroless `/tmp`, embed excluding `_`-prefixed files) that unit tests miss.
7. **Docs:** update ARCHITECTURE if a decision changed; update this file if a convention changed.

## Build & run (dev loop)

**No Go toolchain on the Mac.** Build/test on the **m920x** over SSH, then load the image onto the **Verdin**. Both accessed with `~/.ssh/ota_ce_vm` (see the `dev-resources` memory). Typical loop:
```bash
# sync repo → m920x, then on m920x:
gofmt -w . && go vet ./... && \
docker buildx --builder gwbuilder build --platform linux/arm64 -f deploy/Dockerfile \
  -t torizon-gateway:dev --output type=docker,dest=/tmp/gw-arm64.tar .
# transfer /tmp/gw-arm64.tar → Verdin, then:  docker load && docker compose up -d
```
The Verdin's compose lives at `~/gateway/docker-compose.yml`. Login for the dev device: `admin` / `SuperSecret123` (test account).

## Current status

**Phases 0–2 complete and validated on a Verdin iMX8M Plus (Torizon OS 7.7.0), plus a large "Diagnostics/Cloud" set and a full brand design pass:**
- **Dashboard** — System (module, OS, serial, kernel, **Processor** model/cores/freq/governor, storage w/ partitions+mounts+usage, connectivity), **Live health** (CPU/mem/temp/uptime/network — sparklines, 1s SSE), **Peripherals** (USB, block/removable, CAN, serial, I²C/SPI/GPIO — 4s poll).
- **Network** — read via NM D-Bus; IPv4 edit with confirm-or-revert (NM checkpoints).
- **Containers** — list, live logs (SSE), start/stop/restart (self-guardrail).
- **Logs** — journal + kernel, filter by unit, realtime.
- **Files** — browse read-only; edit/upload/delete confined to /etc,/var (secrets denylist, off by default).
- **Terminal** — in-browser SSH shell (off by default).
- **Torizon Cloud** — provisioning, device, update state, subsystems (expandable containers), aktualizr + remote-access process status.
- **Auth** — first-boot, argon2id, sessions, CSRF, audit.

**Remaining:** offline updates *apply* (Phase 3 — Lockbox), the **Yocto native build** (production deployment), hardening (TOTP, BYO cert, rate-limit), mDNS advertising (so `zinnia.local` resolves), and a parse-once template cache before GA. See ARCHITECTURE §15 backlog.

## Security rules for contributors

- Docker socket and system D-Bus are **root-equivalent**. Validate all inputs; expose the narrowest surface.
- Every state-changing endpoint: **authenticated + CSRF-protected + audited**.
- Network changes to the active interface **must** be revertible (confirm-or-revert).
- File writes are confined to an allowlist (`/etc`,`/var`) with a secrets **read/write denylist**; the terminal SSH target is fixed (no pivot). Keep power features off by default.
- Offline updates: rely on the **host** (`aktualizr`) for signature verification and rollback. Don't reimplement signing.
