package sysinfo

import (
	"encoding/binary"
	"syscall"
	"unsafe"
)

// CAN link details come from rtnetlink (RTM_GETLINK): bitrate, controller state,
// bit timing and error counters live only in the netlink IFLA_CAN_* attributes,
// not in sysfs. Read in pure Go — no `ip` binary (absent from distroless) and no
// external dependency. Both supported targets (arm64, amd64) are little-endian.
var nativeEndian = binary.LittleEndian

// netlink IFLA / nested attribute types (linux/if_link.h, linux/can/netlink.h).
const (
	iflaIfname    = 3
	iflaLinkinfo  = 18
	infoKind      = 1
	infoData      = 2
	infoXstats    = 3
	canBitTiming  = 1
	canClock      = 3
	canState      = 4
	canCtrlMode   = 5
	canRestartMs  = 6
	canBerrCount  = 8
	canDataBittim = 9
	ctrlModeFD    = 0x20

	nlaTypeMask = 0x3fff // strip NLA_F_NESTED / NLA_F_NET_BYTEORDER
)

// canNL holds the netlink-sourced fields for one CAN interface.
type canNL struct {
	State        string
	Bitrate      int
	SamplePoint  float64
	FD           bool
	DataBitrate  int
	ClockHz      int
	RestartMs    int
	BusErrTx     int
	BusErrRx     int
	BusOff       int
	ErrorWarning int
	ErrorPassive int
	ArbLost      int
	Restarts     int
	BusErrors    int
	found        bool
}

var canStateNames = []string{
	"ERROR-ACTIVE", "ERROR-WARNING", "ERROR-PASSIVE", "BUS-OFF", "STOPPED", "SLEEPING",
}

// canDetails dumps all links via rtnetlink and returns CAN attributes keyed by
// interface name. Returns an empty map on any error (caller falls back to sysfs).
func canDetails() map[string]canNL {
	out := map[string]canNL{}
	buf, err := syscall.NetlinkRIB(syscall.RTM_GETLINK, syscall.AF_UNSPEC)
	if err != nil {
		return out
	}
	msgs, err := syscall.ParseNetlinkMessage(buf)
	if err != nil {
		return out
	}
	hdr := int(unsafe.Sizeof(syscall.IfInfomsg{}))
	for _, m := range msgs {
		if m.Header.Type != syscall.RTM_NEWLINK || len(m.Data) < hdr {
			continue
		}
		attrs := parseAttrs(m.Data[hdr:])
		name := cstr(attrs[iflaIfname])
		link, ok := attrs[iflaLinkinfo]
		if name == "" || !ok {
			continue
		}
		info := parseAttrs(link)
		if cstr(info[infoKind]) != "can" {
			continue
		}
		out[name] = parseCAN(info[infoData], info[infoXstats])
	}
	return out
}

func parseCAN(data, xstats []byte) canNL {
	c := canNL{found: true}
	a := parseAttrs(data)
	if bt := a[canBitTiming]; len(bt) >= 8 { // struct can_bittiming
		c.Bitrate = int(nativeEndian.Uint32(bt[0:4]))
		c.SamplePoint = float64(nativeEndian.Uint32(bt[4:8])) / 1000 // permille
	}
	if dbt := a[canDataBittim]; len(dbt) >= 4 {
		c.DataBitrate = int(nativeEndian.Uint32(dbt[0:4]))
	}
	if st := a[canState]; len(st) >= 4 {
		if i := int(nativeEndian.Uint32(st)); i >= 0 && i < len(canStateNames) {
			c.State = canStateNames[i]
		}
	}
	if cm := a[canCtrlMode]; len(cm) >= 8 { // struct can_ctrlmode { mask, flags }
		c.FD = nativeEndian.Uint32(cm[4:8])&ctrlModeFD != 0
	}
	if cl := a[canClock]; len(cl) >= 4 {
		c.ClockHz = int(nativeEndian.Uint32(cl[0:4]))
	}
	if rm := a[canRestartMs]; len(rm) >= 4 {
		c.RestartMs = int(nativeEndian.Uint32(rm))
	}
	if be := a[canBerrCount]; len(be) >= 4 { // struct can_berr_counter { txerr, rxerr u16 }
		c.BusErrTx = int(nativeEndian.Uint16(be[0:2]))
		c.BusErrRx = int(nativeEndian.Uint16(be[2:4]))
	}
	if len(xstats) >= 24 { // struct can_device_stats: 6 x u32
		c.BusErrors = int(nativeEndian.Uint32(xstats[0:4]))
		c.ErrorWarning = int(nativeEndian.Uint32(xstats[4:8]))
		c.ErrorPassive = int(nativeEndian.Uint32(xstats[8:12]))
		c.BusOff = int(nativeEndian.Uint32(xstats[12:16]))
		c.ArbLost = int(nativeEndian.Uint32(xstats[16:20]))
		c.Restarts = int(nativeEndian.Uint32(xstats[20:24]))
	}
	return c
}

// parseAttrs iterates a buffer of netlink rtattr TLVs into a type→payload map.
func parseAttrs(b []byte) map[uint16][]byte {
	m := map[uint16][]byte{}
	for len(b) >= 4 {
		alen := int(nativeEndian.Uint16(b[0:2]))
		atype := nativeEndian.Uint16(b[2:4]) & nlaTypeMask
		if alen < 4 || alen > len(b) {
			break
		}
		m[atype] = b[4:alen]
		adv := (alen + 3) &^ 3 // RTA_ALIGN
		if adv >= len(b) {
			break
		}
		b = b[adv:]
	}
	return m
}

// cstr trims a NUL-terminated netlink string attribute.
func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}
