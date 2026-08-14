// Package containers manages Docker containers via the engine socket
// (/var/run/docker.sock).
//
// IMPLEMENTATION NOTE: we talk to the Docker Engine HTTP API directly over the
// unix socket with a tiny stdlib net/http client (see client.go), NOT the
// docker/docker SDK. That SDK pulls in a very large dependency tree; for a
// read-only listing on constrained hardware a ~150-line client is far lighter.
// Revisit only if we need broad API coverage.
//
// STATUS: Phase 1 implements read-only List() + Available(). Still ROADMAP:
//
//	Logs(ctx, id, follow) (io.ReadCloser, error) // live tail via SSE
//	Start/Stop/Restart(ctx, id) error            // Phase 2, privileged + audited
//
// GUARDRAILS (Phase 2): the gateway-manager container must refuse to stop
// itself (Container.IsSelf marks it); warn before stopping Torizon-critical
// services. No image building/pulling in MVP.
//
// SECURITY: the Docker socket is root-equivalent on the host — the biggest
// attack surface. See backlog "Docker socket proxy". docs/ARCHITECTURE.md §5, §8.3.
package containers
