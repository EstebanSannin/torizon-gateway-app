// Package updates orchestrates Torizon Secure Offline Updates (Lockbox).
//
// ROADMAP (Phase 3). The app accepts a signed Lockbox bundle (USB or web upload),
// stages it into the host offline-update spool, triggers the host update engine
// (aktualizr offline), and streams progress. Signature verification and rollback
// are the HOST's responsibility — we do NOT reimplement signing.
//
// Planned surface:
//
//	type Service interface {
//	    Current(ctx) (Version, error)             // installed OS/app versions
//	    Receive(ctx, io.Reader) (Bundle, error)   // upload/USB → staged bundle
//	    Preview(ctx, Bundle) (Plan, error)        // current vs target summary
//	    Apply(ctx, Bundle) (<-chan Progress, error)
//	}
//
// BACKLOG (research, item #4): pin the exact host trigger interface + spool path
// for aktualizr offline from inside a container on current Torizon OS. Until then
// only Current() (read-only version display) is in scope. docs/ARCHITECTURE.md §8.4.
package updates
