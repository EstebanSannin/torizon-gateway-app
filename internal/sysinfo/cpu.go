package sysinfo

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// CPU describes the processor (from /proc/cpuinfo + sysfs cpufreq).
type CPU struct {
	Model    string // e.g. "ARM Cortex-A53" or the x86 model name
	Cores    int
	MinKHz   int
	MaxKHz   int
	Governor string
}

// CPUInfo reads static processor information.
func CPUInfo(sysfs string) CPU {
	var cpu CPU
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
	case modelName != "":
		cpu.Model = modelName // x86
	case part != "":
		cpu.Model = armModel(impl, part)
	default:
		cpu.Model = "Unknown CPU"
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

// armModel maps an ARM implementer+part to a human core name.
func armModel(impl, part string) string {
	vendor := "ARM"
	if strings.ToLower(impl) != "0x41" {
		vendor = "impl " + impl
	}
	name := map[string]string{
		"0xd03": "Cortex-A53", "0xd04": "Cortex-A35", "0xd05": "Cortex-A55",
		"0xd07": "Cortex-A57", "0xd08": "Cortex-A72", "0xd09": "Cortex-A73",
		"0xd0a": "Cortex-A75", "0xd0b": "Cortex-A76", "0xd41": "Cortex-A78",
	}[strings.ToLower(part)]
	if name == "" {
		name = "part " + part
	}
	return vendor + " " + name
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
