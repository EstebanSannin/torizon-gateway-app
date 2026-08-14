package network

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

// getProp reads a single D-Bus property and returns its underlying Go value.
func getProp(conn *dbus.Conn, path dbus.ObjectPath, iface, prop string) (interface{}, error) {
	v, err := conn.Object(nmDest, path).GetProperty(iface + "." + prop)
	if err != nil {
		return nil, err
	}
	return v.Value(), nil
}

func getString(conn *dbus.Conn, path dbus.ObjectPath, iface, prop string) (string, error) {
	v, err := getProp(conn, path, iface, prop)
	if err != nil {
		return "", err
	}
	s, _ := v.(string)
	return s, nil
}

func getUint32(conn *dbus.Conn, path dbus.ObjectPath, iface, prop string) (uint32, error) {
	v, err := getProp(conn, path, iface, prop)
	if err != nil {
		return 0, err
	}
	n, _ := v.(uint32)
	return n, nil
}

func getObjectPath(conn *dbus.Conn, path dbus.ObjectPath, iface, prop string) (dbus.ObjectPath, error) {
	v, err := getProp(conn, path, iface, prop)
	if err != nil {
		return "", err
	}
	op, _ := v.(dbus.ObjectPath)
	return op, nil
}

// ip4Addresses reads AddressData ([{address, prefix}, ...]) as "a.b.c.d/nn".
func ip4Addresses(conn *dbus.Conn, path dbus.ObjectPath) []string {
	v, err := getProp(conn, path, ip4Iface, "AddressData")
	if err != nil {
		return nil
	}
	data, ok := v.([]map[string]dbus.Variant)
	if !ok {
		return nil
	}
	var out []string
	for _, m := range data {
		addr, _ := m["address"].Value().(string)
		prefix, _ := m["prefix"].Value().(uint32)
		if addr != "" {
			out = append(out, fmt.Sprintf("%s/%d", addr, prefix))
		}
	}
	return out
}

// ip4Nameservers reads DNS servers, preferring the modern NameserverData
// property and falling back to the legacy Nameservers (uint32) array.
func ip4Nameservers(conn *dbus.Conn, path dbus.ObjectPath) []string {
	if v, err := getProp(conn, path, ip4Iface, "NameserverData"); err == nil {
		if data, ok := v.([]map[string]dbus.Variant); ok {
			var out []string
			for _, m := range data {
				if a, _ := m["address"].Value().(string); a != "" {
					out = append(out, a)
				}
			}
			if len(out) > 0 {
				return out
			}
		}
	}
	if v, err := getProp(conn, path, ip4Iface, "Nameservers"); err == nil {
		if arr, ok := v.([]uint32); ok {
			var out []string
			for _, n := range arr {
				out = append(out, uint32ToIP(n))
			}
			return out
		}
	}
	return nil
}

// uint32ToIP renders a NetworkManager-encoded (network byte order) IPv4 address.
func uint32ToIP(n uint32) string {
	return fmt.Sprintf("%d.%d.%d.%d", byte(n), byte(n>>8), byte(n>>16), byte(n>>24))
}

func deviceTypeName(t uint32) string {
	switch t {
	case 1:
		return "Ethernet"
	case 2:
		return "Wi-Fi"
	default:
		return "Other"
	}
}

// deviceStateName maps NM_DEVICE_STATE to a short label.
func deviceStateName(s uint32) string {
	switch {
	case s >= 120:
		return "Failed"
	case s >= 110:
		return "Disconnecting"
	case s >= 100:
		return "Connected"
	case s == 30:
		return "Disconnected"
	case s <= 20:
		return "Unavailable"
	default:
		return "Connecting"
	}
}
