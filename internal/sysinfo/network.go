package sysinfo

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DefaultIface returns the interface carrying the default route (the primary
// uplink), from /proc/net/route. Empty if none. With host networking the
// container's /proc/net reflects the host, so this works there too.
func DefaultIface() string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for i, line := range strings.Split(string(b), "\n") {
		if i == 0 {
			continue // header
		}
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == "00000000" { // destination 0.0.0.0
			return f[0]
		}
	}
	return ""
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
