# Torizon Web Gateway (working name: Gateway Manager)

An on-device, web-based management application for Toradex modules running **Torizon OS** — starting with the **Verdin** family for the **Toradex Zinnia** gateway product, but designed to be **hardware-agnostic** across Torizon.

From a browser on the local network, an operator can:

- 📊 **Inspect** the board — model, serial, OS version, CPU/RAM/storage/temperature.
- 🌐 **Configure networking** — Ethernet, WiFi, static/DHCP, DNS (via NetworkManager), with anti-lockout safeguards.
- 📦 **Manage containers** — list, status, logs, start/stop/restart (via the Docker engine).
- ⬇️ **Apply offline updates** — signed Torizon Secure Offline Updates (Lockbox), from USB or upload.

## Design at a glance

- **Single Go binary** with the UI embedded (`//go:embed`) — no separate web server, no Node runtime.
- **Build-less frontend** — server-rendered HTML + HTMX + Alpine.js + SSE, all vendored. Works fully offline.
- **Torizon-native host access** — NetworkManager over D-Bus, Docker over its socket, updates via aktualizr offline. No blanket `--privileged`.
- **Product-grade security** — local accounts (argon2id), first-boot setup, HTTPS (self-signed on first boot) + mDNS discovery (`zinnia.local`), CSRF, audit log.
- **Ships as a Torizon app** (docker-compose), baked into the OS image via TorizonCore Builder.

## Documentation

- 📐 [Architecture & Specification](docs/ARCHITECTURE.md) — the full design, privilege model, feature specs, roadmap, and open decisions.

## Status

**Draft — architecture phase.** See [open decisions](docs/ARCHITECTURE.md#16-open-decisions-need-your-confirmation) before implementation starts.
