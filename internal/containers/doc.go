// Package containers manages Docker containers via the engine socket
// (/var/run/docker.sock) using the official Docker client.
//
// ROADMAP (Phase 1 read-only → Phase 2 controls). Planned surface:
//
//	type Service interface {
//	    List(ctx) ([]Container, error)            // name,image,state,health,ports,stats
//	    Logs(ctx, id, follow) (io.ReadCloser, error) // live tail via SSE
//	    Start(ctx, id) error; Stop(ctx, id) error; Restart(ctx, id) error
//	}
//
// GUARDRAILS: the gateway-manager container must refuse to stop itself; warn
// before stopping other Torizon-critical services. All control actions are
// privileged (auth + audit). No image building/pulling in MVP.
//
// SECURITY: the Docker socket is root-equivalent on the host — this is the
// biggest attack surface. See backlog item "Docker socket proxy". docs/ARCHITECTURE.md §5, §8.3.
package containers
