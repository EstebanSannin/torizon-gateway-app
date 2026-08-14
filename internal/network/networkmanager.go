package network

import (
	"fmt"
	"sort"

	"github.com/godbus/dbus/v5"
)

// NetworkManager D-Bus constants.
const (
	nmDest       = "org.freedesktop.NetworkManager"
	nmPath       = "/org/freedesktop/NetworkManager"
	nmIface      = "org.freedesktop.NetworkManager"
	devIface     = "org.freedesktop.NetworkManager.Device"
	wiredIface   = "org.freedesktop.NetworkManager.Device.Wired"
	wirelessIfc  = "org.freedesktop.NetworkManager.Device.Wireless"
	ip4Iface     = "org.freedesktop.NetworkManager.IP4Config"
	activeConnIf = "org.freedesktop.NetworkManager.Connection.Active"
	apIface      = "org.freedesktop.NetworkManager.AccessPoint"
)

// Service reads host networking state from NetworkManager over the system bus.
// Read-only (Phase 2). Mutations (confirm-or-revert) come next.
type Service struct {
	busAddr string
}

// New builds a Service that dials the given system D-Bus socket path.
func New(socketPath string) *Service {
	return &Service{busAddr: "unix:path=" + socketPath}
}

// Iface is the read-only view of one network device/connection.
type Iface struct {
	Name         string
	Type         string // Ethernet, Wi-Fi, Other
	State        string // Connected, Disconnected, Unavailable, Connecting
	MAC          string
	Method       string // DHCP, Manual, —
	IPv4         []string
	Gateway      string
	DNS          []string
	ConnectionID string
	SSID         string // Wi-Fi only
}

// Available reports whether NetworkManager is reachable.
func (s *Service) Available() bool {
	conn, err := s.connect()
	if err != nil {
		return false
	}
	defer conn.Close()
	_, err = getProp(conn, nmPath, nmIface, "Version")
	return err == nil
}

// Interfaces returns the Ethernet and Wi-Fi devices with their current config.
func (s *Service) Interfaces() ([]Iface, error) {
	conn, err := s.connect()
	if err != nil {
		return nil, fmt.Errorf("networkmanager unreachable: %w", err)
	}
	defer conn.Close()

	var devPaths []dbus.ObjectPath
	if err := conn.Object(nmDest, nmPath).
		Call(nmIface+".GetDevices", 0).Store(&devPaths); err != nil {
		return nil, err
	}

	var out []Iface
	for _, dp := range devPaths {
		dtype, _ := getUint32(conn, dp, devIface, "DeviceType")
		// 1 = Ethernet, 2 = Wi-Fi. Skip everything else (loopback, bridges, ...).
		if dtype != 1 && dtype != 2 {
			continue
		}
		out = append(out, s.readDevice(conn, dp, dtype))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (s *Service) readDevice(conn *dbus.Conn, dp dbus.ObjectPath, dtype uint32) Iface {
	name, _ := getString(conn, dp, devIface, "Interface")
	state, _ := getUint32(conn, dp, devIface, "State")
	iface := Iface{
		Name:  name,
		Type:  deviceTypeName(dtype),
		State: deviceStateName(state),
	}

	// MAC + Wi-Fi SSID depend on the medium.
	if dtype == 1 {
		iface.MAC, _ = getString(conn, dp, wiredIface, "HwAddress")
	} else {
		iface.MAC, _ = getString(conn, dp, wirelessIfc, "HwAddress")
		iface.SSID = s.activeSSID(conn, dp)
	}

	// DHCP vs manual: presence of a Dhcp4Config object implies DHCP is in use.
	if dhcp, _ := getObjectPath(conn, dp, devIface, "Dhcp4Config"); dhcp != "" && dhcp != "/" {
		iface.Method = "DHCP"
	} else if state >= 100 {
		iface.Method = "Manual"
	} else {
		iface.Method = "—"
	}

	// IPv4 addresses, gateway, DNS from the active IP4Config.
	if ip4, _ := getObjectPath(conn, dp, devIface, "Ip4Config"); ip4 != "" && ip4 != "/" {
		iface.IPv4 = ip4Addresses(conn, ip4)
		iface.Gateway, _ = getString(conn, ip4, ip4Iface, "Gateway")
		iface.DNS = ip4Nameservers(conn, ip4)
	}

	// Active connection profile name.
	if ac, _ := getObjectPath(conn, dp, devIface, "ActiveConnection"); ac != "" && ac != "/" {
		iface.ConnectionID, _ = getString(conn, ac, activeConnIf, "Id")
	}
	return iface
}

// activeSSID reads the SSID of the Wi-Fi device's active access point.
func (s *Service) activeSSID(conn *dbus.Conn, dp dbus.ObjectPath) string {
	ap, _ := getObjectPath(conn, dp, wirelessIfc, "ActiveAccessPoint")
	if ap == "" || ap == "/" {
		return ""
	}
	v, err := getProp(conn, ap, apIface, "Ssid")
	if err != nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return ""
}

func (s *Service) connect() (*dbus.Conn, error) {
	conn, err := dbus.Dial(s.busAddr)
	if err != nil {
		return nil, err
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
