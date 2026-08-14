package network

import (
	"errors"
	"fmt"
	"net"

	"github.com/godbus/dbus/v5"
)

const (
	settingsConnIf = "org.freedesktop.NetworkManager.Settings.Connection"
)

// IPv4Config is a requested IPv4 configuration for an interface.
type IPv4Config struct {
	Method  string // "auto" (DHCP) or "manual" (static)
	Address string // manual only, e.g. "192.168.1.50"
	Prefix  uint32 // manual only, e.g. 24
	Gateway string // manual only (optional)
	DNS     []string
}

// Validate checks a manual configuration.
func (c IPv4Config) Validate() error {
	if c.Method == "auto" {
		return nil
	}
	if c.Method != "manual" {
		return errors.New("method must be auto or manual")
	}
	if net.ParseIP(c.Address).To4() == nil {
		return fmt.Errorf("invalid IPv4 address %q", c.Address)
	}
	if c.Prefix < 1 || c.Prefix > 32 {
		return errors.New("prefix must be between 1 and 32")
	}
	if c.Gateway != "" && net.ParseIP(c.Gateway).To4() == nil {
		return fmt.Errorf("invalid gateway %q", c.Gateway)
	}
	for _, d := range c.DNS {
		if net.ParseIP(d).To4() == nil {
			return fmt.Errorf("invalid DNS server %q", d)
		}
	}
	return nil
}

// ApplyIPv4 changes an interface's IPv4 settings behind a NetworkManager
// checkpoint that auto-rolls back after rollbackSeconds unless confirmed. It
// returns the checkpoint path — pass it to Confirm (keep) or Rollback (revert
// now). This is the anti-lockout primitive: if the change disconnects the
// operator, NM restores the previous state on its own.
func (s *Service) ApplyIPv4(deviceName string, cfg IPv4Config, rollbackSeconds uint32) (string, error) {
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	conn, err := s.connect()
	if err != nil {
		return "", err
	}
	defer conn.Close()

	devPath, err := s.deviceByName(conn, deviceName)
	if err != nil {
		return "", err
	}
	settingsConn, err := s.activeSettingsConn(conn, devPath)
	if err != nil {
		return "", err
	}

	// 1. Checkpoint the device (auto-rollback after the timeout).
	var cp dbus.ObjectPath
	if err := conn.Object(nmDest, nmPath).
		Call(nmIface+".CheckpointCreate", 0, []dbus.ObjectPath{devPath}, rollbackSeconds, uint32(0)).
		Store(&cp); err != nil {
		return "", fmt.Errorf("checkpoint create (write not permitted?): %w", err)
	}

	// 2. Update the connection profile, then re-activate it on the device.
	if err := s.updateIPv4(conn, settingsConn, cfg); err != nil {
		_ = s.rollback(conn, cp) // undo immediately on failure
		return "", err
	}
	var active dbus.ObjectPath
	if err := conn.Object(nmDest, nmPath).
		Call(nmIface+".ActivateConnection", 0, settingsConn, devPath, dbus.ObjectPath("/")).
		Store(&active); err != nil {
		_ = s.rollback(conn, cp)
		return "", fmt.Errorf("activate: %w", err)
	}
	return string(cp), nil
}

// Confirm keeps a pending change permanent (destroys the checkpoint).
func (s *Service) Confirm(checkpoint string) error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Object(nmDest, nmPath).
		Call(nmIface+".CheckpointDestroy", 0, dbus.ObjectPath(checkpoint)).Err
}

// Rollback reverts a pending change immediately (explicit cancel).
func (s *Service) Rollback(checkpoint string) error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	return s.rollback(conn, dbus.ObjectPath(checkpoint))
}

func (s *Service) rollback(conn *dbus.Conn, cp dbus.ObjectPath) error {
	var res map[dbus.ObjectPath]uint32
	return conn.Object(nmDest, nmPath).
		Call(nmIface+".CheckpointRollback", 0, cp).Store(&res)
}

// updateIPv4 rewrites the connection's ipv4 section and saves it.
func (s *Service) updateIPv4(conn *dbus.Conn, settingsConn dbus.ObjectPath, cfg IPv4Config) error {
	obj := conn.Object(nmDest, settingsConn)
	var settings map[string]map[string]dbus.Variant
	if err := obj.Call(settingsConnIf+".GetSettings", 0).Store(&settings); err != nil {
		return fmt.Errorf("get settings: %w", err)
	}

	ipv4 := map[string]dbus.Variant{}
	if cfg.Method == "auto" {
		ipv4["method"] = dbus.MakeVariant("auto")
	} else {
		ipv4["method"] = dbus.MakeVariant("manual")
		ipv4["address-data"] = dbus.MakeVariant([]map[string]dbus.Variant{{
			"address": dbus.MakeVariant(cfg.Address),
			"prefix":  dbus.MakeVariant(cfg.Prefix),
		}})
		if cfg.Gateway != "" {
			ipv4["gateway"] = dbus.MakeVariant(cfg.Gateway)
		}
		if dns := dnsToUint32(cfg.DNS); len(dns) > 0 {
			ipv4["dns"] = dbus.MakeVariant(dns)
		}
	}
	settings["ipv4"] = ipv4
	// GetSettings returns some legacy properties in formats that Update rejects
	// (notably ipv6.addresses/routes as 'aav' vs 'a(ayuay)'). Strip the
	// deprecated address/route keys so NM regenerates them from the *-data
	// properties, and drop the computed connection timestamp.
	for _, sec := range []string{"ipv4", "ipv6"} {
		if m, ok := settings[sec]; ok {
			delete(m, "addresses")
			delete(m, "routes")
		}
	}
	if connSec, ok := settings["connection"]; ok {
		delete(connSec, "timestamp")
	}

	if err := obj.Call(settingsConnIf+".Update", 0, settings).Err; err != nil {
		return fmt.Errorf("update settings: %w", err)
	}
	return nil
}

// deviceByName resolves a device object path from its interface name.
func (s *Service) deviceByName(conn *dbus.Conn, name string) (dbus.ObjectPath, error) {
	var devs []dbus.ObjectPath
	if err := conn.Object(nmDest, nmPath).Call(nmIface+".GetDevices", 0).Store(&devs); err != nil {
		return "", err
	}
	for _, d := range devs {
		if n, _ := getString(conn, d, devIface, "Interface"); n == name {
			return d, nil
		}
	}
	return "", fmt.Errorf("device %q not found", name)
}

// activeSettingsConn returns the settings-connection path backing a device's
// active connection.
func (s *Service) activeSettingsConn(conn *dbus.Conn, dev dbus.ObjectPath) (dbus.ObjectPath, error) {
	active, err := getObjectPath(conn, dev, devIface, "ActiveConnection")
	if err != nil || active == "" || active == "/" {
		return "", errors.New("interface has no active connection to edit")
	}
	sc, err := getObjectPath(conn, active, activeConnIf, "Connection")
	if err != nil || sc == "" || sc == "/" {
		return "", errors.New("could not resolve connection profile")
	}
	return sc, nil
}

// dnsToUint32 encodes dotted IPv4 DNS servers as NetworkManager expects (uint32
// in network byte order — matching the read side in dbus_helpers.go).
func dnsToUint32(list []string) []uint32 {
	var out []uint32
	for _, s := range list {
		ip := net.ParseIP(s).To4()
		if ip == nil {
			continue
		}
		out = append(out, uint32(ip[0])|uint32(ip[1])<<8|uint32(ip[2])<<16|uint32(ip[3])<<24)
	}
	return out
}
