// Package containers manages Docker containers via the engine socket
// (/var/run/docker.sock).
//
// IMPLEMENTATION NOTE: we talk to the Docker Engine HTTP API directly over the
// unix socket with a tiny stdlib net/http client (see client.go), NOT the
// docker/docker SDK. That SDK pulls in a very large dependency tree; for a
// read-only listing on constrained hardware a ~150-line client is far lighter.
// Revisit only if we need broad API coverage.
//
// STATUS: List(), Available(), Logs() (Phase 1) and Start/Stop/Restart()
// (Phase 2, see actions.go) are implemented. Controls are CSRF-protected +
// audited in the HTTP layer.
//
// GUARDRAILS: the gateway-manager container refuses to stop/restart itself
// (IsSelf; enforced in the handler). No image building/pulling in MVP.
//
// SECURITY: the Docker socket is root-equivalent on the host — the biggest
// attack surface. See backlog "Docker socket proxy". docs/ARCHITECTURE.md §5, §8.3.
package containers
