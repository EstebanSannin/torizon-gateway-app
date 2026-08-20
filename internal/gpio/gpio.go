// Package gpio reads and drives GPIO lines through the kernel GPIO character
// device (/dev/gpiochipN) via the v1 uAPI ioctls — pure stdlib, no cgo, no
// libgpiod. Reading line info never requests a line (safe); reading a value
// momentarily requests a free line as input; setting a value requests a free
// line as output and HOLDS it (the gateway becomes the line's consumer) until
// released, so the driven level persists. Writes are gated by the caller.
package gpio

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// v1 uAPI ioctl request numbers (computed from the kernel _IOx macros).
const (
	getChipInfo   = 0x8044B401 // GPIO_GET_CHIPINFO_IOCTL
	getLineInfo   = 0xC048B402 // GPIO_GET_LINEINFO_IOCTL
	getLineHandle = 0xC16CB403 // GPIO_GET_LINEHANDLE_IOCTL
	getLineValues = 0xC040B408 // GPIOHANDLE_GET_LINE_VALUES_IOCTL
	setLineValues = 0xC040B409 // GPIOHANDLE_SET_LINE_VALUES_IOCTL

	flagUsed      = 1 << 0 // GPIOLINE_FLAG_KERNEL
	flagIsOut     = 1 << 1
	flagActiveLow = 1 << 2

	reqInput  = 1 << 0 // GPIOHANDLE_REQUEST_INPUT
	reqOutput = 1 << 1 // GPIOHANDLE_REQUEST_OUTPUT

	label = "torizon-gateway" // our consumer label for held lines
)

// ourLabel identifies lines this process holds. Kept short (uAPI cap 31 chars).
var chipRe = regexp.MustCompile(`^gpiochip[0-9]{1,3}$`)

type chipInfo struct {
	name  [32]byte
	label [32]byte
	lines uint32
}
type lineInfo struct {
	offset   uint32
	flags    uint32
	name     [32]byte
	consumer [32]byte
}
type handleRequest struct {
	lineOffsets   [64]uint32
	flags         uint32
	defaultValues [64]uint8
	consumerLabel [32]byte
	lines         uint32
	fd            int32
}
type handleData struct {
	values [64]uint8
}

// Line is a single GPIO line's state.
type Line struct {
	Offset    uint32
	Name      string
	Consumer  string
	Output    bool
	ActiveLow bool
	Used      bool // requested by a driver or another process
	HeldByUs  bool // held (driven) by this gateway
	Value     int  // 0/1 if known (held or recently read), else -1
}

// Chip is a GPIO controller and its lines.
type Chip struct {
	Dev      string // "gpiochip1"
	Label    string // "30210000.gpio"
	NumLines uint32
	UsedN    int
	Lines    []Line
}

type held struct {
	fd    int
	value int
}

// Service reads/drives GPIO. hostRoot locates /dev in the container.
type Service struct {
	hostRoot string
	writable bool
	mu       sync.Mutex
	held     map[string]*held // key "gpiochipN:offset"
	lastRead map[string]int   // ephemeral input reads
}

func New(hostRoot string, writable bool) *Service {
	if hostRoot == "" {
		hostRoot = "/"
	}
	return &Service{hostRoot: hostRoot, writable: writable, held: map[string]*held{}, lastRead: map[string]int{}}
}

func (s *Service) Writable() bool { return s.writable }

func (s *Service) devPath(chip string) string {
	return filepath.Join(s.hostRoot, "dev", chip)
}

func key(chip string, off uint32) string { return fmt.Sprintf("%s:%d", chip, off) }

// Chips enumerates gpiochip0..N and every line (read-only — never requests a
// line, so it can't disturb the system).
func (s *Service) Chips() []Chip {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Chip
	for i := 0; ; i++ {
		dev := fmt.Sprintf("gpiochip%d", i)
		f, err := os.Open(s.devPath(dev))
		if err != nil {
			break // no more chips
		}
		var ci chipInfo
		if ioctl(f.Fd(), getChipInfo, unsafe.Pointer(&ci)) != nil {
			f.Close()
			break
		}
		chip := Chip{Dev: dev, Label: cstr(ci.label[:]), NumLines: ci.lines}
		for off := uint32(0); off < ci.lines; off++ {
			li := lineInfo{offset: off}
			if ioctl(f.Fd(), getLineInfo, unsafe.Pointer(&li)) != nil {
				continue
			}
			cons := cstr(li.consumer[:])
			ln := Line{
				Offset: off, Name: cstr(li.name[:]), Consumer: cons,
				Output: li.flags&flagIsOut != 0, ActiveLow: li.flags&flagActiveLow != 0,
				Used: li.flags&flagUsed != 0, Value: -1,
			}
			k := key(dev, off)
			if h, ok := s.held[k]; ok {
				ln.HeldByUs, ln.Value, ln.Output = true, h.value, true
			} else if v, ok := s.lastRead[k]; ok {
				ln.Value = v
			}
			if ln.Used {
				chip.UsedN++
			}
			chip.Lines = append(chip.Lines, ln)
		}
		f.Close()
		out = append(out, chip)
	}
	return out
}

// LineState reads the current state of a single line (for a targeted row update
// after an action, so the rest of the page isn't disturbed).
func (s *Service) LineState(chip string, off uint32) (Line, error) {
	if !chipRe.MatchString(chip) {
		return Line{}, errors.New("invalid chip")
	}
	f, err := os.Open(s.devPath(chip))
	if err != nil {
		return Line{}, err
	}
	defer f.Close()
	li := lineInfo{offset: off}
	if err := ioctl(f.Fd(), getLineInfo, unsafe.Pointer(&li)); err != nil {
		return Line{}, err
	}
	ln := Line{
		Offset: off, Name: cstr(li.name[:]), Consumer: cstr(li.consumer[:]),
		Output: li.flags&flagIsOut != 0, ActiveLow: li.flags&flagActiveLow != 0,
		Used: li.flags&flagUsed != 0, Value: -1,
	}
	s.mu.Lock()
	k := key(chip, off)
	if h, ok := s.held[k]; ok {
		ln.HeldByUs, ln.Value, ln.Output = true, h.value, true
	} else if v, ok := s.lastRead[k]; ok {
		ln.Value = v
	}
	s.mu.Unlock()
	return ln, nil
}

// ReadLine momentarily requests a free line as input and reads its value.
func (s *Service) ReadLine(chip string, off uint32) (int, error) {
	if !chipRe.MatchString(chip) {
		return 0, errors.New("invalid chip")
	}
	f, err := os.OpenFile(s.devPath(chip), os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	req := handleRequest{flags: reqInput, lines: 1}
	req.lineOffsets[0] = off
	copy(req.consumerLabel[:], label)
	if err := ioctl(f.Fd(), getLineHandle, unsafe.Pointer(&req)); err != nil {
		return 0, fmt.Errorf("request line: %w", err)
	}
	lf := os.NewFile(uintptr(req.fd), "gpio-line")
	defer lf.Close()
	var data handleData
	if err := ioctl(lf.Fd(), getLineValues, unsafe.Pointer(&data)); err != nil {
		return 0, err
	}
	v := int(data.values[0])
	s.mu.Lock()
	s.lastRead[key(chip, off)] = v
	s.mu.Unlock()
	return v, nil
}

// SetLine drives a free line to value and holds it (or updates an already-held
// line). Gated by the writable flag.
func (s *Service) SetLine(chip string, off uint32, value int) error {
	if !s.writable {
		return errors.New("GPIO writes are disabled")
	}
	if !chipRe.MatchString(chip) {
		return errors.New("invalid chip")
	}
	if value != 0 {
		value = 1
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(chip, off)
	if h, ok := s.held[k]; ok { // already ours — just change the value
		data := handleData{}
		data.values[0] = uint8(value)
		if err := ioctl(uintptr(h.fd), setLineValues, unsafe.Pointer(&data)); err != nil {
			return err
		}
		h.value = value
		return nil
	}
	f, err := os.OpenFile(s.devPath(chip), os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	req := handleRequest{flags: reqOutput, lines: 1}
	req.lineOffsets[0] = off
	req.defaultValues[0] = uint8(value)
	copy(req.consumerLabel[:], label)
	if err := ioctl(f.Fd(), getLineHandle, unsafe.Pointer(&req)); err != nil {
		return fmt.Errorf("request line: %w", err)
	}
	s.held[k] = &held{fd: int(req.fd), value: value}
	delete(s.lastRead, k)
	return nil
}

// ReleaseLine hands a held line back (it reverts to its default state).
func (s *Service) ReleaseLine(chip string, off uint32) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(chip, off)
	h, ok := s.held[k]
	if !ok {
		return nil
	}
	syscall.Close(h.fd)
	delete(s.held, k)
	return nil
}

func ioctl(fd, req uintptr, arg unsafe.Pointer) error {
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg)); e != 0 {
		return e
	}
	return nil
}

func cstr(b []byte) string {
	if i := strings.IndexByte(string(b), 0); i >= 0 {
		return string(b[:i])
	}
	return string(b)
}
