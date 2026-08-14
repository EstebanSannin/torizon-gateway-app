// Package network manages device networking via NetworkManager over the system
// D-Bus bus. The host stays authoritative; this package reads and applies
// connection settings, it never keeps a second copy of the config.
//
// IMPLEMENTED (Phase 2, read-only): Available() + Interfaces() — Ethernet/Wi-Fi
// devices with state, MAC, method (DHCP/manual), IPv4/gateway/DNS, active
// profile, Wi-Fi SSID. See networkmanager.go / dbus_helpers.go.
//
// STILL ROADMAP (write): WiFiScan, Apply(ConnectionChange), Confirm(token),
// SetHostname. SAFETY — anti-lockout: any change to the interface the operator
// is connected through MUST use the confirm-or-revert flow (apply → countdown →
// auto-revert if not re-confirmed from the browser). docs/ARCHITECTURE.md §8.2.
//
// Implementation: github.com/godbus/dbus/v5 talking to org.freedesktop.NetworkManager.
package network
