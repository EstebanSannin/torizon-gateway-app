package sysinfo

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// DefaultIface returns the interface carrying the primary (lowest-metric)
// default route. Empty if none.
func DefaultIface() string {
	if r := DefaultRoutes(); len(r) > 0 {
		return r[0]
	}
	return ""
}

// DefaultRoutes returns the interfaces carrying a default route, ordered by
// metric (primary/lowest first, deduplicated), from /proc/net/route. With host
// networking the container's /proc/net reflects the host, so this works there.
func DefaultRoutes() []string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return nil
	}
	type route struct {
		iface  string
		metric int
	}
	var routes []route
	for i, line := range strings.Split(string(b), "\n") {
		if i == 0 {
			continue // header
		}
		f := strings.Fields(line)
		if len(f) >= 7 && f[1] == "00000000" { // destination 0.0.0.0
			m, _ := strconv.Atoi(f[6])
			routes = append(routes, route{f[0], m})
		}
	}
	sort.SliceStable(routes, func(i, j int) bool { return routes[i].metric < routes[j].metric })
	var out []string
	seen := map[string]bool{}
	for _, r := range routes {
		if !seen[r.iface] {
			seen[r.iface] = true
			out = append(out, r.iface)
		}
	}
	return out
}

// LinkStatus is the physical link state of an interface (sysfs).
type LinkStatus struct {
	SpeedMbps int
	Duplex    string // "full", "half", or "" if unknown/not applicable
	MTU       int
}

// Link reads the link speed/duplex/MTU for an interface from sysfs. Speed and
// duplex are unavailable (0/"") for wireless and virtual interfaces.
func Link(sysfs, iface string) LinkStatus {
	dir := filepath.Join(sysfs, "class/net", iface)
	ls := LinkStatus{
		SpeedMbps: atoiSafe(readTrim(filepath.Join(dir, "speed"))),
		Duplex:    readTrim(filepath.Join(dir, "duplex")),
		MTU:       atoiSafe(readTrim(filepath.Join(dir, "mtu"))),
	}
	if ls.SpeedMbps < 0 {
		ls.SpeedMbps = 0
	}
	if ls.Duplex == "unknown" {
		ls.Duplex = ""
	}
	return ls
}

// NetCounters returns cumulative rx/tx bytes for an interface from sysfs.
func NetCounters(sysfs, iface string) (rx, tx uint64) {
	if iface == "" {
		return 0, 0
	}
	dir := filepath.Join(sysfs, "class/net", iface, "statistics")
	rx, _ = strconv.ParseUint(readTrim(filepath.Join(dir, "rx_bytes")), 10, 64)
	tx, _ = strconv.ParseUint(readTrim(filepath.Join(dir, "tx_bytes")), 10, 64)
	return rx, tx
}
