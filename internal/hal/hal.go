// Package hal is the Hardware Abstraction Layer. All board-specific reads go
// through the BoardInfo interface so the rest of the app stays hardware-agnostic
// across the Torizon-supported range. Detect() picks an implementation at runtime
// by capability probe (never hard-coded per product), so adding a new SoM (e.g.
// the Zinnia gateway) is one new implementation and nothing else changes.
package hal

import "os"

// BoardInfo exposes read-only device identity and health.
type BoardInfo interface {
	// Kind reports which implementation answered ("toradex" or "generic").
	Kind() string

	Model() string        // e.g. "Toradex Verdin iMX8M Plus"
	SerialNumber() string // module serial, "" if unavailable
	OSVersion() string    // Torizon OS version / os-release PRETTY_NAME
	KernelVersion() string

	// Metrics returns a fresh snapshot of live health values.
	Metrics() (Metrics, error)
}

// Metrics is a point-in-time health snapshot pushed to the dashboard via SSE.
type Metrics struct {
	UptimeSeconds  int64
	CPULoad1       float64 // 1-minute load average
	MemTotalBytes  uint64
	MemUsedBytes   uint64
	SoCTempCelsius float64 // 0 if no sensor found
}

// Detect selects the best implementation for the current hardware.
// Probe order: Toradex signature first, generic fallback last.
func Detect() BoardInfo {
	if isToradex() {
		return newToradex()
	}
	return newGeneric()
}

// isToradex probes for the Toradex device-tree signature. Kept deliberately
// simple; refine per validated SoM. Returns false off Toradex hardware.
func isToradex() bool {
	// Toradex modules expose a compatible/vendor string in the device tree.
	// Presence of the Toradex model node is a good-enough capability probe.
	if _, err := os.Stat("/proc/device-tree/model"); err == nil {
		if b, err := os.ReadFile("/proc/device-tree/model"); err == nil {
			return containsFold(string(b), "toradex") || containsFold(string(b), "verdin") ||
				containsFold(string(b), "apalis") || containsFold(string(b), "colibri")
		}
	}
	return false
}
