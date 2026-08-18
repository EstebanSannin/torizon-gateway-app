package sysinfo

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// CPU describes the processor: the SoC/chip identity plus the CPU core and live
// frequency scaling parameters (from /proc/cpuinfo, device-tree and sysfs).
type CPU struct {
	SoC      string // chip/SoC: "i.MX8MP", "AM625", or the x86 model name
	Core     string // CPU core: "Cortex-A53" (ARM), empty on x86
	Arch     string // GOARCH: "arm64", "amd64"
	Cores    int
	MinKHz   int
	MaxKHz   int
	Governor string
}

// CPUInfo reads static processor information.
func CPUInfo(sysfs string) CPU {
	cpu := CPU{Arch: runtime.GOARCH}
	var impl, part, modelName string
	if b, err := os.ReadFile("/proc/cpuinfo"); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			k, v, ok := strings.Cut(line, ":")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			switch k {
			case "processor":
				cpu.Cores++
			case "model name":
				modelName = v
			case "CPU implementer":
				impl = v
			case "CPU part":
				part = v
			}
		}
	}
	switch {
	case modelName != "": // x86: the model name is the chip identity
		cpu.SoC = modelName
	case part != "":
		cpu.Core = armCore(impl, part)
		cpu.SoC = detectSoC(sysfs)
		if cpu.SoC == "" {
			cpu.SoC = cpu.Core // fall back to the core name
		}
	default:
		cpu.SoC = "Unknown CPU"
	}

	cf := filepath.Join(sysfs, "devices/system/cpu/cpu0/cpufreq")
	cpu.MinKHz = atoiSafe(readTrim(filepath.Join(cf, "cpuinfo_min_freq")))
	cpu.MaxKHz = atoiSafe(readTrim(filepath.Join(cf, "cpuinfo_max_freq")))
	cpu.Governor = readTrim(filepath.Join(cf, "scaling_governor"))
	return cpu
}

// CPUCurrentKHz returns the average current frequency across cores (live value).
func CPUCurrentKHz(sysfs string) int {
	matches, _ := filepath.Glob(filepath.Join(sysfs, "devices/system/cpu/cpu[0-9]*/cpufreq/scaling_cur_freq"))
	sum, n := 0, 0
	for _, m := range matches {
		if v := atoiSafe(readTrim(m)); v > 0 {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / n
}

// CPUSample returns cumulative (idle, total) jiffies from the aggregate "cpu"
// line of /proc/stat. Two samples over an interval give utilisation:
//
//	util% = (1 - Δidle/Δtotal) * 100
//
// /proc/stat is system-wide (not PID-namespaced), so this reflects the host CPU.
func CPUSample() (idle, total uint64) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0, 0
	}
	line, _, _ := strings.Cut(string(b), "\n")
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0
	}
	for i, f := range fields[1:] {
		v, _ := strconv.ParseUint(f, 10, 64)
		total += v
		if i == 3 || i == 4 { // idle + iowait
			idle += v
		}
	}
	return idle, total
}

// ThermalLimits returns the SoC temperature thresholds in Celsius: warn (amber)
// from the "passive" trip, alarm (red) from the "critical" trip, and a full-scale
// value for the gauge with headroom above the alarm. Sensible defaults are used
// when the thermal zone exposes no trip points.
func ThermalLimits(sysfs string) (warn, alarm, scale float64) {
	warn, alarm = 85, 100 // defaults
	zone := filepath.Join(sysfs, "class/thermal/thermal_zone0")
	trips, _ := filepath.Glob(filepath.Join(zone, "trip_point_*_type"))
	for _, tp := range trips {
		typ := readTrim(tp)
		milli := atoiSafe(readTrim(strings.TrimSuffix(tp, "_type") + "_temp"))
		if milli <= 0 {
			continue
		}
		switch typ {
		case "passive":
			warn = float64(milli) / 1000
		case "critical":
			alarm = float64(milli) / 1000
		}
	}
	if warn >= alarm {
		warn = alarm - 15
	}
	// Round the scale up to the next 5°C, at least 20°C above the alarm and 125.
	scale = float64((int(alarm+20)/5 + 1) * 5)
	if scale < 125 {
		scale = 125
	}
	return warn, alarm, scale
}

// armCore maps an ARM implementer+part to the core name (e.g. "Cortex-A53").
func armCore(impl, part string) string {
	name := map[string]string{
		"0xd03": "Cortex-A53", "0xd04": "Cortex-A35", "0xd05": "Cortex-A55",
		"0xd07": "Cortex-A57", "0xd08": "Cortex-A72", "0xd09": "Cortex-A73",
		"0xd0a": "Cortex-A75", "0xd0b": "Cortex-A76", "0xd41": "Cortex-A78",
	}[strings.ToLower(part)]
	if name == "" {
		name = "part " + part
	}
	if strings.ToLower(impl) != "0x41" { // non-ARM implementer
		return "impl " + impl + " " + name
	}
	return name
}

// detectSoC identifies the chip. Prefers the kernel's soc0 soc_id (e.g. i.MX
// reports "i.MX8MP"); falls back to the device-tree compatible string.
func detectSoC(sysfs string) string {
	if id := readTrim(filepath.Join(sysfs, "devices/soc0/soc_id")); id != "" &&
		!strings.HasPrefix(strings.ToLower(id), "jep106") {
		return id
	}
	return socFromCompatible()
}

// socFromCompatible parses /proc/device-tree/compatible, whose last entry is the
// SoC-level "vendor,soc" (e.g. "fsl,imx8mp" → i.MX8MP, "ti,am625" → AM625).
func socFromCompatible() string {
	b, err := os.ReadFile("/proc/device-tree/compatible")
	if err != nil {
		return ""
	}
	entries := strings.Split(strings.TrimRight(string(b), "\x00"), "\x00")
	if len(entries) == 0 {
		return ""
	}
	last := entries[len(entries)-1]
	_, soc, ok := strings.Cut(last, ",")
	if !ok {
		return ""
	}
	switch {
	case strings.HasPrefix(soc, "imx"):
		return "i.MX" + strings.ToUpper(soc[3:])
	case strings.HasPrefix(soc, "am"):
		return strings.ToUpper(soc)
	default:
		return strings.ToUpper(soc)
	}
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
