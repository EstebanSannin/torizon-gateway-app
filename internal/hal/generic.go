package hal

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// generic reads standard Linux interfaces (/proc, /sys, os-release). Used as a
// fallback so the app runs on non-Toradex hardware and in dev containers.
type generic struct{}

func newGeneric() BoardInfo { return &generic{} }

func (g *generic) Kind() string { return "generic" }

func (g *generic) Model() string {
	if v := firstLine("/proc/device-tree/model"); v != "" {
		return strings.TrimRight(v, "\x00")
	}
	if v := firstLine("/sys/devices/virtual/dmi/id/product_name"); v != "" {
		return v
	}
	return "Unknown device"
}

func (g *generic) SerialNumber() string {
	return strings.TrimRight(firstLine("/sys/devices/virtual/dmi/id/product_serial"), "\x00")
}

func (g *generic) OSVersion() string {
	m := parseKeyVals("/etc/os-release")
	if v := m["PRETTY_NAME"]; v != "" {
		return v
	}
	return m["NAME"]
}

func (g *generic) KernelVersion() string {
	return firstLine("/proc/sys/kernel/osrelease")
}

func (g *generic) Metrics() (Metrics, error) {
	m := Metrics{}
	m.UptimeSeconds = readUptime()
	m.CPULoad1 = readLoad1()
	m.MemTotalBytes, m.MemUsedBytes = readMem()
	m.SoCTempCelsius = readTemp()
	return m, nil
}

// ---- helpers (stdlib only) ----

func firstLine(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	if s.Scan() {
		return strings.TrimSpace(s.Text())
	}
	return ""
}

func parseKeyVals(path string) map[string]string {
	out := map[string]string{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := s.Text()
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
	}
	return out
}

func readUptime() int64 {
	f := firstLine("/proc/uptime")
	if f == "" {
		return 0
	}
	fields := strings.Fields(f)
	sec, _ := strconv.ParseFloat(fields[0], 64)
	return int64(sec)
}

func readLoad1() float64 {
	f := firstLine("/proc/loadavg")
	if f == "" {
		return 0
	}
	fields := strings.Fields(f)
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

func readMem() (total, used uint64) {
	m := map[string]uint64{}
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, v, ok := strings.Cut(s.Text(), ":")
		if !ok {
			continue
		}
		fields := strings.Fields(v)
		if len(fields) == 0 {
			continue
		}
		kb, _ := strconv.ParseUint(fields[0], 10, 64)
		m[k] = kb * 1024
	}
	total = m["MemTotal"]
	avail := m["MemAvailable"]
	if total >= avail {
		used = total - avail
	}
	return total, used
}

// readTemp returns the first thermal zone reading in Celsius, 0 if none.
func readTemp() float64 {
	b, err := os.ReadFile("/sys/class/thermal/thermal_zone0/temp")
	if err != nil {
		return 0
	}
	milli, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return float64(milli) / 1000.0
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}
