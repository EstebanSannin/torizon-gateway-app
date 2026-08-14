// Package network manages device networking via NetworkManager over the system
// D-Bus bus. The host stays authoritative; this package reads and applies
// connection settings, it never keeps a second copy of the config.
//
// ROADMAP (Phase 2). Planned surface:
//
//	type Service interface {
//	    Connections(ctx) ([]Connection, error)   // list devices + connections
//	    WiFiScan(ctx) ([]AccessPoint, error)
//	    Apply(ctx, ConnectionChange) (PendingChange, error) // confirm-or-revert
//	    Confirm(ctx, token) error                 // keep a pending change
//	    Hostname(ctx) (string, error); SetHostname(...)
//	}
//
// SAFETY — anti-lockout: any change to the interface the operator is connected
// through MUST use the confirm-or-revert flow (apply → countdown → auto-revert
// if the operator doesn't re-confirm from the browser). See docs/ARCHITECTURE.md §8.2.
//
// Implementation: github.com/godbus/dbus/v5 talking to org.freedesktop.NetworkManager.
package network
