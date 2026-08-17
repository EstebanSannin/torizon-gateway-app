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
	Sysfs string // sysfs root, e.g. "/sys"
}

// NewPeripherals builds a reader rooted at the given sysfs path.
func NewPeripherals(sysfs string) *Peripherals {
	if sysfs == "" {
		sysfs = "/sys"
	}
	return &Peripherals{Sysfs: sysfs}
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

// BlockDevice is a whole-disk block device (partitions excluded).
type BlockDevice struct {
	Name       string
	SizeBytes  uint64
	Removable  bool
	Transport  string // "USB", "MMC", "NVMe", "virtual", "other"
	Model      string
	Mountpoint string // best-effort (from /proc/mounts)
}

// Block lists whole block devices, flagging removable USB media (USB sticks).
func (p *Peripherals) Block() []BlockDevice {
	base := filepath.Join(p.Sysfs, "block")
	entries, err := os.ReadDir(base)
	if err != nil {
		return nil
	}
	mounts := readMounts()
	var out []BlockDevice
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "zram") {
			continue // pseudo-devices, noise
		}
		dir := filepath.Join(base, name)
		sectors, _ := strconv.ParseUint(readTrim(filepath.Join(dir, "size")), 10, 64)
		link, _ := os.Readlink(dir)
		out = append(out, BlockDevice{
			Name:       name,
			SizeBytes:  sectors * 512,
			Removable:  readTrim(filepath.Join(dir, "removable")) == "1",
			Transport:  transportOf(name, link),
			Model:      readTrim(filepath.Join(dir, "device/model")),
			Mountpoint: mounts["/dev/"+name],
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
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

// readMounts maps device path → mountpoint from /proc/mounts (best effort).
func readMounts() map[string]string {
	out := map[string]string{}
	b, err := os.ReadFile("/proc/mounts")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && strings.HasPrefix(f[0], "/dev/") {
			out[f[0]] = f[1]
		}
	}
	return out
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
