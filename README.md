# Torizon Web Gateway (working name: Gateway Manager)

An on-device, web-based management application for Toradex modules running **Torizon OS** — first target the **Verdin** family for the **Toradex Zinnia** gateway product, but designed to be **hardware-agnostic** across Torizon.

From a browser on the local network, an operator can manage the device without a shell:

- 📊 **Dashboard** — module, OS, serial, a detailed **kernel** card (release, arch, SMP/PREEMPT, toolchain, binutils, build date from `/proc/version`), **processor** (SoC model/core/arch, live frequency bar/governor), **storage** (partitions, mount points, usage), a detailed **connectivity/WAN** card (address, gateway, DNS, MAC, link speed/duplex, MTU, DHCP/static, multi-homed uplinks); **live health** (CPU utilization, memory, SoC temp as color-zoned **radial gauges**; uptime/load; network as a smooth **area chart**) over a 1s SSE stream; **peripherals** (USB, block/removable media, **CAN** with bitrate/controller-state/errors, serial, I²C/SPI/GPIO).
- 🌐 **Network** — view interfaces and edit IPv4 (DHCP/static, gateway, DNS) via NetworkManager with a **confirm-or-revert** anti-lockout safeguard; **Wi-Fi station management** — pick the interface, scan, click a network to connect (passphrase dialog), see the connected-details panel, disconnect, and forget.
- 🎨 **Light / dark theme** — toggle in the top bar, remembered across sessions.
- 📦 **Containers** — list, live logs, start/stop/restart via the Docker engine.
- 📜 **Logs** — the systemd journal + kernel log, filter by unit, streamed live.
- 📁 **Files** — browse the filesystem; edit text / upload / delete, confined to `/etc` and `/var` (secrets blocked, audited).
- 🖥️ **Terminal** — an in-browser SSH shell for debugging.
- 🔌 **GPIO** — inventory every line across all controllers (with the board's SODIMM pin names, consumers, direction, in-use state), read free lines, and — behind an off-by-default switch — drive/hold outputs with a per-line confirm. Pure-Go GPIO character device, no libgpiod.
- ☁️ **Torizon Cloud** — provisioning status, device identity, update state, subsystems (with the docker-compose app's containers), and whether the aktualizr + remote-access daemons are running.
- ⬇️ **Updates** — aktualizr configuration (online/offline mode, server, install policy), current state, the primary + secondary **ECUs** with per-ECU up-to-date status, a **Check now** button (over D-Bus), an **editable polling interval**, and **offline (Lockbox) update apply** — pick a signed Lockbox from removable media or a path and install it with no server needed (verified on hardware). Web upload of a Lockbox + per-update approval are on the roadmap.

## Design at a glance

- **Single Go binary** with the UI embedded (`//go:embed`) — no separate web server, no Node runtime, **no frontend build step**.
- **Build-less, offline-first frontend** — server-rendered HTML + HTMX + Alpine.js + SSE + xterm.js, all vendored (incl. the Inter font). Works air-gapped.
- **Torizon-native host access** — NetworkManager over D-Bus, Docker over its socket (tiny stdlib client, not the SDK), sysfs/proc reads, systemd journal & `aktualizr-info` via the host binaries. **No blanket `--privileged`.**
- **Pure-Go, no cgo** — `modernc.org/sqlite`, `x/crypto`, `godbus`, `gorilla/websocket` — so the static binary and a future Yocto recipe stay trivial.
- **Product-grade security** — first-boot admin (argon2id), HTTPS (self-signed on first boot), CSRF, audit log; risky features (file writes, terminal) off by default.

## Deployment

- **Dev:** a Torizon container with elevated host access (host networking, `/host` mount, docker/D-Bus sockets). Fast build→load→run loop.
- **Production (planned): native via Yocto** (a bitbake recipe + systemd service). Torizon runs a single docker-compose and wipes other containers when a customer app deploys, so the gateway must run natively to always be present. The same binary runs both ways.

**Container images** — published multi-arch (linux/arm64, linux/arm/v7, linux/amd64) at [`samnite/torizon-gateway`](https://hub.docker.com/r/samnite/torizon-gateway), so the same image runs on any Torizon module.

**Deploy on any Torizon OS device** with the ready-to-use [`docker-compose.yml`](docker-compose.yml) at the repo root:

- **Torizon Cloud:** add `docker-compose.yml` as a package and deploy it to your device or fleet from the platform.
- **Manually on a device:**

  ```bash
  docker compose up -d      # then open https://<device-ip>:8443
  ```

Power features (file writes, web terminal) are **off by default** — enable them via the commented switches in the compose. `deploy/docker-compose.yml` is an annotated reference for custom builds (`${REGISTRY}/${TAG}`).

## Documentation

- 📐 [Architecture & Specification](docs/ARCHITECTURE.md) — design, privilege model, feature specs, data-access patterns, roadmap.
- 🎨 [Design System](docs/DESIGN-SYSTEM.md) — brand tokens, typography, components.
- 🤖 [CLAUDE.md](CLAUDE.md) — knowledge base for AI-assisted contributors.

## Status

**Working prototype — Phases 0–2 plus diagnostics/cloud complete and validated on a Verdin iMX8M Plus (Torizon OS 7.7.0).** Remaining: offline-update apply (Lockbox), the Yocto native build, and hardening (TOTP, BYO cert, rate-limiting, mDNS). See the [roadmap](docs/ARCHITECTURE.md#15-roadmap--phasing).
