// Package store is the persistence layer for app-owned state only: user
// accounts, sessions, app settings, the audit log, and the uploaded TLS cert.
// It never duplicates host config (network/containers) — the host is
// authoritative for those.
//
// ROADMAP (Phase 0). Backend: SQLite via modernc.org/sqlite (pure Go, no cgo),
// stored on the persistent volume (GATEWAY_DATA_DIR). Small, transactional.
//
// Planned surface:
//
//	type Store interface {
//	    Users() UserRepo
//	    Sessions() SessionRepo
//	    Settings() SettingsRepo
//	    Audit() AuditRepo         // append-only: who/what/when for every state change
//	    Close() error
//	}
//
// Every state-changing action elsewhere in the app writes an audit record here. §11.
package store
