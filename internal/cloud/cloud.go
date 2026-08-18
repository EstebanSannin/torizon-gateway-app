// Package cloud reports Torizon Cloud / OTA state by running aktualizr-info and
// parsing its output. Natively that's just "aktualizr-info"; in our container
// it runs the host binary via the host dynamic loader against the host SOTA
// storage (/host/var/sota), the same technique as the logs package.
package cloud

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Service runs aktualizr-info and inspects cloud-related processes.
type Service struct {
	base     []string // argv prefix that runs aktualizr-info
	hostRoot string   // where the host "/" is visible (for /proc scanning)
}

// New builds the service. hostRoot is "/" natively or "/host" in a container;
// cfgDir is a writable directory for the generated aktualizr config (the app's
// data dir — the distroless container has no writable /tmp).
func New(hostRoot, cfgDir string) *Service {
	if hostRoot == "" {
		hostRoot = "/"
	}
	if hostRoot == "/" {
		return &Service{base: []string{"aktualizr-info"}, hostRoot: hostRoot}
	}
	ld := firstGlob(filepath.Join(hostRoot, "lib/ld-*.so*"), filepath.Join(hostRoot, "lib64/ld-*.so*"))
	if ld == "" {
		return &Service{base: []string{"aktualizr-info"}, hostRoot: hostRoot}
	}
	if cfgDir == "" {
		cfgDir = os.TempDir()
	}
	// aktualizr-info needs to be told where the host SOTA DB lives.
	cfg := filepath.Join(cfgDir, "gw-sota.toml")
	_ = os.WriteFile(cfg, []byte(fmt.Sprintf(
		"[storage]\ntype = \"sqlite\"\npath = \"%s/var/sota\"\nsqldb_path = \"sql.db\"\n",
		strings.TrimRight(hostRoot, "/"))), 0o600)
	libPath := strings.Join(append([]string{
		filepath.Join(hostRoot, "lib"), filepath.Join(hostRoot, "lib64"),
		filepath.Join(hostRoot, "usr/lib"), filepath.Join(hostRoot, "usr/lib/systemd"),
	}, globDirs(filepath.Join(hostRoot, "usr/lib/*-linux-gnu"))...), ":")
	return &Service{
		hostRoot: hostRoot,
		base: []string{
			ld, "--library-path", libPath, filepath.Join(hostRoot, "usr/bin/aktualizr-info"), "-c", cfg,
		},
	}
}

// Subsystem is an ECU/secondary tracked by the cloud (OS, docker-compose, ...).
type Subsystem struct {
	HardwareID    string
	Kind          string // OS, docker-compose, bootloader, fuses, other
	IsPrimary     bool
	InstalledName string
	InstalledHash string
	DesiredHash   string
	UpToDate      bool
	HasTarget     bool // aktualizr reports a target for it
}

// ProcStatus reports whether a cloud-critical process is running.
type ProcStatus struct {
	Title   string
	Unit    string
	Running bool
}

// Info is the parsed cloud/OTA state.
type Info struct {
	Available       bool
	Services        []ProcStatus
	DeviceID        string
	DeviceName      string
	Provisioned     bool
	FetchedMetadata bool
	Subsystems      []Subsystem
	State           string // up-to-date, update-available, updating, unknown
	LastCorrelation string
}

// Get returns the current cloud state (process status is always populated, even
// when aktualizr-info is unavailable).
func (s *Service) Get(ctx context.Context) Info {
	services := s.processStatus()
	out, _ := s.run(ctx)
	// aktualizr-info may exit non-zero yet print valid output; trust the content.
	if !strings.Contains(out, "Device ID:") {
		return Info{Available: false, Services: services}
	}
	info := parseInfo(out)
	info.Available = true
	info.Services = services

	if name, err := s.run(ctx, "--name-only"); err == nil {
		// Only keep it as a "name" when it differs from the UUID (some setups
		// return a friendly name; here it's the UUID, which we show anyway).
		if n := strings.TrimSpace(lastLine(name)); n != "" && n != info.DeviceID {
			info.DeviceName = n
		}
	}
	// Desired hashes from the director; compare with installed to find updates.
	desired := map[string]string{}
	if dt, err := s.run(ctx, "--director-targets"); err == nil {
		desired = parseDirectorTargets(dt)
	}
	updateAvail := false
	for i := range info.Subsystems {
		ss := &info.Subsystems[i]
		ss.DesiredHash = desired[ss.HardwareID]
		if ss.DesiredHash != "" && ss.InstalledHash != "" {
			ss.UpToDate = strings.EqualFold(ss.DesiredHash, ss.InstalledHash)
			if !ss.UpToDate {
				updateAvail = true
			}
		} else {
			ss.UpToDate = true // nothing to compare
		}
	}
	switch {
	case strings.Contains(out, "Pending "):
		info.State = "updating"
	case updateAvail:
		info.State = "update-available"
	default:
		info.State = "up-to-date"
	}
	return info
}

func (s *Service) run(ctx context.Context, args ...string) (string, error) {
	argv := append(append([]string{}, s.base...), args...)
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	// aktualizr-info prints a benign "could not load 'legacy' OpenSSL provider"
	// notice that can get prepended to output; strip it so it never surfaces.
	clean := strings.ReplaceAll(string(out), "Warning: could not load 'legacy' OpenSSL provider", "")
	return clean, err
}

// parseInfo extracts device + subsystem details from aktualizr-info output.
func parseInfo(out string) Info {
	var info Info
	var cur *Subsystem
	flush := func() {
		if cur != nil {
			info.Subsystems = append(info.Subsystems, *cur)
			cur = nil
		}
	}
	for _, raw := range strings.Split(out, "\n") {
		line := strings.TrimSpace(raw)
		switch {
		case strings.HasPrefix(line, "Device ID:"):
			info.DeviceID = val(line)
		case strings.HasPrefix(line, "Provisioned on server:"):
			info.Provisioned = strings.Contains(line, "yes")
		case strings.HasPrefix(line, "Fetched metadata:"):
			info.FetchedMetadata = strings.Contains(line, "yes")
		case strings.HasPrefix(line, "Primary ECU hardware ID:"):
			flush()
			info.Subsystems = append(info.Subsystems, Subsystem{
				HardwareID: val(line), Kind: "OS", IsPrimary: true,
			})
		case strings.HasPrefix(line, "Current Primary ECU running version:"):
			// attach to the primary entry
			for i := range info.Subsystems {
				if info.Subsystems[i].IsPrimary {
					info.Subsystems[i].InstalledHash = val(line)
					info.Subsystems[i].HasTarget = true
				}
			}
		// Secondary blocks: "N) serial ID: ..."
		case strings.Contains(line, ") serial ID:"):
			flush()
			cur = &Subsystem{}
		case cur != nil && strings.HasPrefix(line, "hardware ID:"):
			cur.HardwareID = val(line)
			cur.Kind = subsystemKind(cur.HardwareID)
		case cur != nil && strings.HasPrefix(line, "installed image hash:"):
			cur.InstalledHash = val(line)
			cur.HasTarget = true
		case cur != nil && strings.HasPrefix(line, "installed image filename:"):
			cur.InstalledName = val(line)
		case cur != nil && strings.HasPrefix(line, "correlation id:"):
			info.LastCorrelation = val(line)
		}
	}
	flush()
	return info
}

// parseDirectorTargets maps hardwareId -> sha256 from director-targets JSON.
// Parsed leniently (no struct) to tolerate schema variation.
func parseDirectorTargets(j string) map[string]string {
	out := map[string]string{}
	// crude but robust: for each target object, find its hardwareIds and sha256.
	// Split on "\"hashes\"" boundaries is brittle; instead scan with a tiny state.
	dec := strings.NewReplacer("\n", "", " ", "").Replace(j)
	// find every hardwareIds":["X"] ... sha256":"Y" pair within the same target.
	for _, seg := range strings.Split(dec, "\"custom\":") {
		hw := between(seg, `"hardwareIds":["`, `"`)
		sha := between(seg, `"sha256":"`, `"`)
		if hw != "" && sha != "" {
			out[hw] = sha
		}
	}
	return out
}

// ---- small helpers ----

func val(line string) string {
	if i := strings.Index(line, ":"); i >= 0 {
		return strings.TrimSpace(line[i+1:])
	}
	return ""
}

func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	j := strings.Index(s, b)
	if j < 0 {
		return ""
	}
	return s[:j]
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

func subsystemKind(hw string) string {
	switch {
	case strings.Contains(hw, "docker-compose"):
		return "docker-compose"
	case strings.Contains(hw, "bootloader"):
		return "bootloader"
	case strings.Contains(hw, "fuses"):
		return "fuses"
	default:
		return "other"
	}
}

func firstGlob(patterns ...string) string {
	for _, p := range patterns {
		if m, _ := filepath.Glob(p); len(m) > 0 {
			return m[0]
		}
	}
	return ""
}

func globDirs(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}
