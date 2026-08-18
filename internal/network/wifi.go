package network

import (
	"errors"
	"sort"

	"github.com/godbus/dbus/v5"
)

// NetworkManager Wi-Fi security flags (NM_802_11_AP_FLAGS / NM_802_11_AP_SEC).
const (
	apFlagPrivacy   = 0x1
	secKeyMgmtPSK   = 0x100
	secKeyMgmt8021X = 0x200
	secKeyMgmtSAE   = 0x400 // WPA3
	secKeyMgmtOWE   = 0x800

	settingsPath = "/org/freedesktop/NetworkManager/Settings"
	settingsIf   = "org.freedesktop.NetworkManager.Settings"

	wifiModeAP = 3 // NM_802_11_MODE_AP — excluded from station management
)

// WiFiAP is a scanned access point (deduplicated per SSID, strongest kept).
type WiFiAP struct {
	SSID     string
	Strength int    // 0-100
	Security string // Open, WEP, WPA, WPA2, WPA3, 802.1X
	Secured  bool
	Band     string // "2.4 GHz", "5 GHz", "6 GHz"
	Channel  int
	Active   bool // currently connected
	Saved    bool // a saved profile exists
}

// WiFiStations returns the names of Wi-Fi station (non-AP) interfaces, i.e. the
// ones we can scan with and join networks on (excludes hotspot/uap0).
func (s *Service) WiFiStations() ([]string, error) {
	conn, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	var devPaths []dbus.ObjectPath
	if err := conn.Object(nmDest, nmPath).Call(nmIface+".GetDevices", 0).Store(&devPaths); err != nil {
		return nil, err
	}
	var out []string
	for _, dp := range devPaths {
		if dtype, _ := getUint32(conn, dp, devIface, "DeviceType"); dtype != 2 {
			continue
		}
		if mode, _ := getUint32(conn, dp, wirelessIfc, "Mode"); mode == wifiModeAP {
			continue
		}
		if name, err := getString(conn, dp, devIface, "Interface"); err == nil && name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out, nil
}

// WiFiScan asks NetworkManager to rescan on the given Wi-Fi interface.
func (s *Service) WiFiScan(iface string) error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	dev, err := s.deviceByName(conn, iface)
	if err != nil {
		return err
	}
	return conn.Object(nmDest, dev).Call(wirelessIfc+".RequestScan", 0, map[string]dbus.Variant{}).Err
}

// WiFiNetworks lists visible access points on the interface, plus the active SSID.
func (s *Service) WiFiNetworks(iface string) ([]WiFiAP, string, error) {
	conn, err := s.connect()
	if err != nil {
		return nil, "", err
	}
	defer conn.Close()
	dev, err := s.deviceByName(conn, iface)
	if err != nil {
		return nil, "", err
	}

	activeAP, _ := getObjectPath(conn, dev, wirelessIfc, "ActiveAccessPoint")
	saved := s.savedSSIDs(conn)

	var aps []dbus.ObjectPath
	if err := conn.Object(nmDest, dev).Call(wirelessIfc+".GetAllAccessPoints", 0).Store(&aps); err != nil {
		return nil, "", err
	}

	bySSID := map[string]WiFiAP{}
	var active string
	for _, ap := range aps {
		ssid := apSSID(conn, ap)
		if ssid == "" {
			continue // hidden
		}
		strength := int(apByte(conn, ap, "Strength"))
		freq, _ := getUint32(conn, ap, apIface, "Frequency")
		flags, _ := getUint32(conn, ap, apIface, "Flags")
		wpa, _ := getUint32(conn, ap, apIface, "WpaFlags")
		rsn, _ := getUint32(conn, ap, apIface, "RsnFlags")
		sec, secured := apSecurity(flags, wpa, rsn)
		band, ch := bandChannel(int(freq))
		w := WiFiAP{
			SSID: ssid, Strength: strength, Security: sec, Secured: secured,
			Band: band, Channel: ch, Active: ap == activeAP, Saved: saved[ssid],
		}
		if w.Active {
			active = ssid
		}
		if cur, ok := bySSID[ssid]; !ok || w.Strength > cur.Strength {
			w.Active = w.Active || cur.Active
			bySSID[ssid] = w
		} else if w.Active {
			cur.Active = true
			bySSID[ssid] = cur
		}
	}
	out := make([]WiFiAP, 0, len(bySSID))
	for _, w := range bySSID {
		out = append(out, w)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Active != out[j].Active {
			return out[i].Active
		}
		return out[i].Strength > out[j].Strength
	})
	return out, active, nil
}

// WiFiConnect creates and activates a connection to an SSID (auto IPv4/IPv6).
func (s *Service) WiFiConnect(iface, ssid, password string) error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	dev, err := s.deviceByName(conn, iface)
	if err != nil {
		return err
	}

	// Determine security + specific AP from the current scan.
	apPath, secured, keymgmt := s.matchAP(conn, dev, ssid)
	if secured && keymgmt == "" {
		return errors.New("this network's security type isn't supported yet")
	}

	settings := map[string]map[string]dbus.Variant{
		"connection": {
			"type": dbus.MakeVariant("802-11-wireless"),
			"id":   dbus.MakeVariant(ssid),
		},
		"802-11-wireless": {
			"ssid": dbus.MakeVariant([]byte(ssid)),
			"mode": dbus.MakeVariant("infrastructure"),
		},
		"ipv4": {"method": dbus.MakeVariant("auto")},
		"ipv6": {"method": dbus.MakeVariant("auto")},
	}
	if secured {
		settings["802-11-wireless-security"] = map[string]dbus.Variant{
			"key-mgmt": dbus.MakeVariant(keymgmt),
			"psk":      dbus.MakeVariant(password),
		}
	}
	specific := dbus.ObjectPath("/")
	if apPath != "" {
		specific = apPath
	}
	var newConn, active dbus.ObjectPath
	return conn.Object(nmDest, nmPath).
		Call(nmIface+".AddAndActivateConnection", 0, settings, dev, specific).
		Store(&newConn, &active)
}

// WiFiDisconnect deactivates the interface's active connection.
func (s *Service) WiFiDisconnect(iface string) error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	dev, err := s.deviceByName(conn, iface)
	if err != nil {
		return err
	}
	active, _ := getObjectPath(conn, dev, devIface, "ActiveConnection")
	if active == "" || active == "/" {
		return nil
	}
	return conn.Object(nmDest, nmPath).Call(nmIface+".DeactivateConnection", 0, active).Err
}

// WiFiSaved returns the SSIDs of saved Wi-Fi connection profiles.
func (s *Service) WiFiSaved() ([]string, error) {
	conn, err := s.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	seen := map[string]bool{}
	var out []string
	for _, c := range s.listConnections(conn) {
		if ssid := connSSID(conn, c); ssid != "" && !seen[ssid] {
			seen[ssid] = true
			out = append(out, ssid)
		}
	}
	sort.Strings(out)
	return out, nil
}

// WiFiForget deletes the saved profile(s) for an SSID.
func (s *Service) WiFiForget(ssid string) error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	for _, c := range s.listConnections(conn) {
		if connSSID(conn, c) == ssid {
			return conn.Object(nmDest, c).Call(settingsConnIf+".Delete", 0).Err
		}
	}
	return nil
}

// ---- helpers ----

func (s *Service) matchAP(conn *dbus.Conn, dev dbus.ObjectPath, ssid string) (path dbus.ObjectPath, secured bool, keymgmt string) {
	var aps []dbus.ObjectPath
	conn.Object(nmDest, dev).Call(wirelessIfc+".GetAllAccessPoints", 0).Store(&aps)
	best := -1
	for _, ap := range aps {
		if apSSID(conn, ap) != ssid {
			continue
		}
		st := int(apByte(conn, ap, "Strength"))
		if st > best {
			best = st
			flags, _ := getUint32(conn, ap, apIface, "Flags")
			wpa, _ := getUint32(conn, ap, apIface, "WpaFlags")
			rsn, _ := getUint32(conn, ap, apIface, "RsnFlags")
			_, secured = apSecurity(flags, wpa, rsn)
			keymgmt = secKeyMgmt(rsn, wpa)
			path = ap
		}
	}
	return path, secured, keymgmt
}

func (s *Service) savedSSIDs(conn *dbus.Conn) map[string]bool {
	out := map[string]bool{}
	for _, c := range s.listConnections(conn) {
		if ssid := connSSID(conn, c); ssid != "" {
			out[ssid] = true
		}
	}
	return out
}

func (s *Service) listConnections(conn *dbus.Conn) []dbus.ObjectPath {
	var conns []dbus.ObjectPath
	conn.Object(nmDest, settingsPath).Call(settingsIf+".ListConnections", 0).Store(&conns)
	return conns
}

// connSSID returns the SSID of a Wi-Fi connection profile, or "".
func connSSID(conn *dbus.Conn, c dbus.ObjectPath) string {
	var settings map[string]map[string]dbus.Variant
	if err := conn.Object(nmDest, c).Call(settingsConnIf+".GetSettings", 0).Store(&settings); err != nil {
		return ""
	}
	w, ok := settings["802-11-wireless"]
	if !ok {
		return ""
	}
	if b, ok := w["ssid"].Value().([]byte); ok {
		return string(b)
	}
	return ""
}

func apSSID(conn *dbus.Conn, ap dbus.ObjectPath) string {
	v, err := getProp(conn, ap, apIface, "Ssid")
	if err != nil {
		return ""
	}
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return ""
}

func apByte(conn *dbus.Conn, ap dbus.ObjectPath, prop string) byte {
	v, err := getProp(conn, ap, apIface, prop)
	if err != nil {
		return 0
	}
	if b, ok := v.(byte); ok {
		return b
	}
	return 0
}

// apSecurity returns a human security label and whether a key is required.
func apSecurity(flags, wpa, rsn uint32) (string, bool) {
	switch {
	case rsn&secKeyMgmtSAE != 0:
		return "WPA3", true
	case rsn&secKeyMgmtPSK != 0:
		return "WPA2", true
	case wpa&secKeyMgmtPSK != 0:
		return "WPA", true
	case rsn&secKeyMgmt8021X != 0 || wpa&secKeyMgmt8021X != 0:
		return "802.1X", true
	case rsn&secKeyMgmtOWE != 0:
		return "OWE", false
	case flags&apFlagPrivacy != 0:
		return "WEP", true
	default:
		return "Open", false
	}
}

// secKeyMgmt picks the NM key-mgmt string for connecting ("" if unsupported).
func secKeyMgmt(rsn, wpa uint32) string {
	switch {
	case rsn&secKeyMgmtSAE != 0:
		return "sae"
	case rsn&secKeyMgmtPSK != 0 || wpa&secKeyMgmtPSK != 0:
		return "wpa-psk"
	default:
		return "" // enterprise/WEP not supported here
	}
}

func bandChannel(freq int) (string, int) {
	switch {
	case freq == 2484:
		return "2.4 GHz", 14
	case freq >= 2412 && freq <= 2472:
		return "2.4 GHz", (freq - 2407) / 5
	case freq >= 5000 && freq < 5900:
		return "5 GHz", (freq - 5000) / 5
	case freq >= 5925:
		return "6 GHz", (freq - 5950) / 5
	default:
		return "—", 0
	}
}
