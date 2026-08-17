package sysinfo

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Peripherals reads connected hardware from sysfs (pure Go — no lsusb/lsblk/ip
// dependency, keeping the image small). USB and block devices are host-global
// in /sys even from a bridged container; CAN interfaces need host networking.
type Peripherals struct {
	Sysfs    string // sysfs root, e.g. "/sys"
	HostRoot string // host filesystem root ("/" native, "/host" in a container)
}

// NewPeripherals builds a reader over the given sysfs and host-filesystem roots.
// HostRoot is where the host's "/" is visible (for the mount table and statfs);
// "/" natively, "/host" when the host is bind-mounted into a container.
func NewPeripherals(sysfs, hostRoot string) *Peripherals {
	if sysfs == "" {
		sysfs = "/sys"
	}
	if hostRoot == "" {
		hostRoot = "/"
	}
	return &Peripherals{Sysfs: sysfs, HostRoot: hostRoot}
}

// USBDevice is a connected USB device (not an interface or root hub).
type USBDevice struct {
	Name         string // sysfs name, e.g. "1-1.4"
	VendorID     string
	ProductID    string
	Manufacturer string
	Product      string
	Serial       string
	Class        string // human class name
	Speed        string // Mbps as reported by sysfs
	IsHub        bool
}

// USB lists connected USB devices from /sys/bus/usb/devices.
func (p *Peripherals) USB() []USBDevice {
	base := filepath.Join(p.Sysfs, "bus/usb/devices")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []USBDevice
	for _, e := range entries {
		name := e.Name()
		// Skip interfaces (contain ':') and root hubs ("usbN").
		if strings.Contains(name, ":") || strings.HasPrefix(name, "usb") {
			continue
		}
		dir := filepath.Join(base, name)
		vid := readTrim(filepath.Join(dir, "idVendor"))
		if vid == "" {
			continue // not an actual device node
		}
		class := readTrim(filepath.Join(dir, "bDeviceClass"))
		d := USBDevice{
			Name:         name,
			VendorID:     vid,
			ProductID:    readTrim(filepath.Join(dir, "idProduct")),
			Manufacturer: readTrim(filepath.Join(dir, "manufacturer")),
			Product:      readTrim(filepath.Join(dir, "product")),
			Serial:       readTrim(filepath.Join(dir, "serial")),
			Class:        usbClassName(class),
			Speed:        readTrim(filepath.Join(dir, "speed")),
			IsHub:        class == "09",
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// BlockDevice is a whole-disk block device with its partitions.
type BlockDevice struct {
	Name       string
	SizeBytes  uint64
	Removable  bool
	Transport  string // "USB", "MMC", "NVMe", "virtual", "other"
	Model      string
	Partitions []Partition
}

// Partition is a partition (or a whole-disk filesystem) with mount/usage info.
type Partition struct {
	Name       string
	SizeBytes  uint64
	Mountpoint string
	FSType     string
	UsedBytes  uint64  // filesystem used (mounted only)
	TotalBytes uint64  // filesystem capacity (mounted only)
	UsedPct    float64 // mounted only
}

// Block lists whole block devices and their partitions, with mount points and
// filesystem usage for mounted ones. Removable USB media are flagged.
func (p *Peripherals) Block() []BlockDevice {
	base := filepath.Join(p.Sysfs, "block")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	mounts := readMounts(p.HostRoot)
	var out []BlockDevice
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue // pseudo-devices, noise
		}
		dir := filepath.Join(base, name)
		sectors, _ := strconv.ParseUint(readTrim(filepath.Join(dir, "size")), 10, 64)
		link, _ := os.Readlink(dir)
		bd := BlockDevice{
			Name:      name,
			SizeBytes: sectors * 512,
			Removable: readTrim(filepath.Join(dir, "removable")) == "1",
			Transport: transportOf(name, link),
			Model:     readTrim(filepath.Join(dir, "device/model")),
		}
		bd.Partitions = p.partitionsOf(dir, name, mounts)
		// Whole-disk filesystem (no partition table) — represent as one entry.
		if len(bd.Partitions) == 0 {
			if m, ok := mounts[readTrim(filepath.Join(dir, "dev"))]; ok {
				bd.Partitions = []Partition{fillUsage(Partition{
					Name: name, SizeBytes: bd.SizeBytes, Mountpoint: m.point, FSType: m.fstype,
				}, p.HostRoot)}
			}
		}
		out = append(out, bd)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// partitionsOf enumerates a disk's partitions (sysfs subdirs with a "partition"
// file), attaching mount point + filesystem usage. Partitions are matched to
// mounts by major:minor (robust to /dev/disk/by-label symlinks).
func (p *Peripherals) partitionsOf(diskDir, disk string, mounts map[string]mount) []Partition {
	entries, err := os.ReadDir(diskDir)
	if err != nil {
		return nil
	}
	var parts []Partition
	for _, e := range entries {
		pn := e.Name()
		if !strings.HasPrefix(pn, disk) {
			continue
		}
		pdir := filepath.Join(diskDir, pn)
		if !fileExists(filepath.Join(pdir, "partition")) {
			continue
		}
		sectors, _ := strconv.ParseUint(readTrim(filepath.Join(pdir, "size")), 10, 64)
		part := Partition{Name: pn, SizeBytes: sectors * 512}
		if m, ok := mounts[readTrim(filepath.Join(pdir, "dev"))]; ok {
			part.Mountpoint = m.point
			part.FSType = m.fstype
			part = fillUsage(part, p.HostRoot)
		}
		parts = append(parts, part)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Name < parts[j].Name })
	return parts
}

// fillUsage adds filesystem usage (statfs) for a mounted partition. The
// mountpoint is a host path, so it is statfs'd through the host root.
func fillUsage(part Partition, hostRoot string) Partition {
	if part.Mountpoint == "" {
		return part
	}
	if d, err := DiskUsage(filepath.Join(hostRoot, part.Mountpoint)); err == nil {
		part.UsedBytes, part.TotalBytes, part.UsedPct = d.UsedBytes, d.TotalBytes, d.UsedPct
	}
	return part
}

// CANInterface is a CAN bus network interface.
type CANInterface struct {
	Name  string
	State string // "up" / "down"
}

// CAN lists CAN interfaces from /sys/class/net/can*.
func (p *Peripherals) CAN() []CANInterface {
	base := filepath.Join(p.Sysfs, "class/net")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []CANInterface
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "can") {
			continue
		}
		out = append(out, CANInterface{
			Name:  name,
			State: readTrim(filepath.Join(base, name, "operstate")),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SerialPort is a serial/UART device.
type SerialPort struct {
	Name string
	Kind string // "USB serial", "CDC ACM", "UART", "USB gadget"
}

// Serial lists meaningful serial ports from /sys/class/tty (skips the many
// unused ttyS* 8250 stubs).
func (p *Peripherals) Serial() []SerialPort {
	base := filepath.Join(p.Sysfs, "class/tty")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []SerialPort
	for _, e := range entries {
		n := e.Name()
		var kind string
		switch {
		case strings.HasPrefix(n, "ttyUSB"):
			kind = "USB serial"
		case strings.HasPrefix(n, "ttyACM"):
			kind = "CDC ACM"
		case strings.HasPrefix(n, "ttymxc"):
			kind = "UART"
		case strings.HasPrefix(n, "ttyGS"):
			kind = "USB gadget"
		default:
			continue
		}
		out = append(out, SerialPort{Name: n, Kind: kind})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Bus is an I²C/GPIO controller entry with a human detail string.
type Bus struct {
	Name   string
	Detail string
}

// I2C lists I²C buses from /sys/class/i2c-dev.
func (p *Peripherals) I2C() []Bus {
	base := filepath.Join(p.Sysfs, "class/i2c-dev")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []Bus
	for _, e := range entries {
		name := readTrim(filepath.Join(base, e.Name(), "name"))
		if name == "" {
			name = readTrim(filepath.Join(base, e.Name(), "device/name"))
		}
		out = append(out, Bus{Name: e.Name(), Detail: name})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// SPI lists SPI device nodes (spidevX.Y).
func (p *Peripherals) SPI() []string {
	base := filepath.Join(p.Sysfs, "class/spidev")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

// GPIO lists GPIO controllers from /sys/class/gpio (label + line count).
func (p *Peripherals) GPIO() []Bus {
	base := filepath.Join(p.Sysfs, "class/gpio")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	var out []Bus
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "gpiochip") {
			continue
		}
		label := readTrim(filepath.Join(base, e.Name(), "label"))
		n := readTrim(filepath.Join(base, e.Name(), "ngpio"))
		detail := label
		if n != "" {
			detail = strings.TrimSpace(label + " · " + n + " lines")
		}
		out = append(out, Bus{Name: e.Name(), Detail: detail})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ---- helpers ----

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func transportOf(name, sysfsLink string) string {
	switch {
	case strings.Contains(sysfsLink, "/usb"):
		return "USB"
	case strings.HasPrefix(name, "mmcblk"):
		return "MMC"
	case strings.HasPrefix(name, "nvme"):
		return "NVMe"
	case strings.HasPrefix(name, "zram"):
		return "virtual"
	default:
		return "other"
	}
}

type mount struct {
	point  string
	fstype string
}

// readMounts maps a device's "major:minor" → mount info, read from the host
// mount table (/proc/1/mountinfo under hostRoot). Keying by major:minor is
// robust to /dev/disk/by-* symlinks. When a device is mounted in several places
// (e.g. OSTree bind mounts), the whole-device mount (root "/") is preferred.
func readMounts(hostRoot string) map[string]mount {
	out := map[string]mount{}
	b, err := os.ReadFile(filepath.Join(hostRoot, "proc/1/mountinfo"))
	if err != nil {
		if b, err = os.ReadFile(filepath.Join(hostRoot, "proc/self/mountinfo")); err != nil {
			return out
		}
	}
	for _, line := range strings.Split(string(b), "\n") {
		sep := strings.Index(line, " - ")
		if sep < 0 {
			continue
		}
		left := strings.Fields(line[:sep])    // id parent maj:min root mountpoint opts...
		right := strings.Fields(line[sep+3:]) // fstype source superopts
		if len(left) < 5 || len(right) < 1 {
			continue
		}
		majmin, root, point, fstype := left[2], left[3], unescapeMount(left[4]), right[0]
		// Prefer the whole-device mount (root "/") over subtree bind mounts.
		if _, seen := out[majmin]; seen && root != "/" {
			continue
		}
		out[majmin] = mount{point: point, fstype: fstype}
	}
	return out
}

// unescapeMount decodes the octal escapes mountinfo uses for space/tab/newline.
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	r := strings.NewReplacer(`\040`, " ", `\011`, "\t", `\012`, "\n", `\134`, `\`)
	return r.Replace(s)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// usbClassName maps a bDeviceClass hex code to a short human label.
func usbClassName(hex string) string {
	switch strings.ToLower(hex) {
	case "00":
		return "per-interface"
	case "02":
		return "communications"
	case "03":
		return "HID"
	case "08":
		return "mass storage"
	case "09":
		return "hub"
	case "0a":
		return "CDC data"
	case "0e":
		return "video"
	case "e0":
		return "wireless"
	case "ff":
		return "vendor-specific"
	case "":
		return ""
	default:
		return "class " + hex
	}
}
