// Package updates reads the Torizon update client (aktualizr-torizon)
// configuration and drives it over the system D-Bus (org.uptane.Aktualizr).
//
// The daemon exposes CheckForUpdates / Consent / OfflineUpdate / Cancel methods
// and the ConsentRequired / InstallUpdatesAutomatically properties, so a "check
// now" is a clean D-Bus call — no service restart. When InstallUpdatesAutomatically
// is 0 the daemon downloads but waits for Consent before installing.
package updates

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/godbus/dbus/v5"
)

const (
	dbusDest  = "org.uptane.Aktualizr"
	dbusPath  = "/org/uptane/aktualizr"
	dbusIface = "org.uptane.Aktualizr"

	systemdDest   = "org.freedesktop.systemd1"
	systemdPath   = "/org/freedesktop/systemd1"
	systemdMgr    = "org.freedesktop.systemd1.Manager"
	aktualizrUnit = "aktualizr-torizon.service"

	pollingFragment = "etc/sota/conf.d/60-polling-interval.toml"
)

// Config is the effective aktualizr configuration relevant to updates, parsed
// from the merged conf.d TOML fragments (defaults then /etc overrides).
type Config struct {
	Mode         string // "Online" (server provisioned) or "Offline"
	ServerHost   string // OTA server host from the gateway URL file, or ""
	PollingSec   int
	RollbackMode string
	Secondaries  bool
	OSTreeHash   string // active OSTree deployment (from /proc/cmdline)
}

// Status is the live update-client state read over D-Bus.
type Status struct {
	Available       bool   // the D-Bus API answered
	AutoInstall     bool   // InstallUpdatesAutomatically != 0
	ConsentRequired string // non-empty when an update awaits consent
}

// Service reads the update config and talks to aktualizr over the system D-Bus.
type Service struct {
	busAddr  string
	hostRoot string
}

func New(dbusSocket, hostRoot string) *Service {
	if hostRoot == "" {
		hostRoot = "/"
	}
	return &Service{busAddr: "unix:path=" + dbusSocket, hostRoot: hostRoot}
}

// ReadConfig parses the merged aktualizr conf.d fragments plus the resolved
// server URL and active OSTree deployment.
func (s *Service) ReadConfig() Config {
	kv := map[string]string{}
	for _, dir := range []string{"usr/lib/sota/conf.d", "etc/sota/conf.d"} {
		files, _ := filepath.Glob(filepath.Join(s.hostRoot, dir, "*.toml"))
		sort.Strings(files)
		for _, f := range files {
			mergeToml(f, kv)
		}
	}
	cfg := Config{
		PollingSec:   atoi(kv["polling_sec"]),
		RollbackMode: kv["rollback_mode"],
		OSTreeHash:   s.ostreeHash(),
	}
	if scf := kv["secondary_config_file"]; scf != "" {
		if b, err := os.ReadFile(s.hostPath(scf)); err == nil && len(strings.TrimSpace(string(b))) > 2 {
			cfg.Secondaries = true
		}
	}
	if sup := kv["server_url_path"]; sup != "" {
		if b, err := os.ReadFile(s.hostPath(sup)); err == nil {
			if u, err := url.Parse(strings.TrimSpace(string(b))); err == nil {
				cfg.ServerHost = u.Host
			}
		}
	}
	if cfg.ServerHost != "" {
		cfg.Mode = "Online"
	} else {
		cfg.Mode = "Offline"
	}
	return cfg
}

// Status reads the live D-Bus state (best-effort; Available=false if unreachable).
func (s *Service) Status() Status {
	conn, err := s.connect()
	if err != nil {
		return Status{}
	}
	defer conn.Close()
	obj := conn.Object(dbusDest, dbusPath)
	var st Status
	if v, err := obj.GetProperty(dbusIface + ".InstallUpdatesAutomatically"); err == nil {
		st.Available = true
		// Per the aktualizr D-Bus spec: 0 (default) = install automatically,
		// 1 = require consent. Only enforced when a client actively manages it.
		if i, ok := v.Value().(int32); ok {
			st.AutoInstall = i == 0
		}
	}
	if v, err := obj.GetProperty(dbusIface + ".ConsentRequired"); err == nil {
		st.Available = true
		if s, ok := v.Value().(string); ok {
			st.ConsentRequired = s
		}
	}
	return st
}

// CheckForUpdates triggers an immediate update check via D-Bus.
func (s *Service) CheckForUpdates() error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Object(dbusDest, dbusPath).Call(dbusIface+".CheckForUpdates", 0).Err
}

// ConfigWritable reports whether the polling config fragment can be written
// (the host /etc is mounted read-write).
func (s *Service) ConfigWritable() bool {
	tmp := filepath.Join(s.hostRoot, "etc/sota/conf.d", ".gw-write-test")
	if err := os.WriteFile(tmp, nil, 0o644); err != nil {
		return false
	}
	os.Remove(tmp)
	return true
}

// SetPolling writes the polling interval fragment and restarts aktualizr so it
// takes effect (aktualizr reads its config only at startup).
func (s *Service) SetPolling(seconds int) error {
	if seconds < 5 || seconds > 86400 {
		return errors.New("polling interval must be between 5 and 86400 seconds")
	}
	path := filepath.Join(s.hostRoot, pollingFragment)
	body := fmt.Sprintf("[uptane]\npolling_sec = %d\n", seconds)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return s.restartAktualizr()
}

// OfflineSource is a candidate Secure Offline Update (Lockbox) found on the
// device — a directory whose root contains a "metadata" folder.
type OfflineSource struct {
	Path      string // host path (what OfflineUpdate receives)
	HasImages bool   // an "images" directory is also present
}

// OfflineSources scans mounted removable media for Lockbox directories. Best
// effort: a USB plugged in after the container started may not be visible, in
// which case the operator can enter the path directly.
func (s *Service) OfflineSources() []OfflineSource {
	var out []OfflineSource
	seen := map[string]bool{}
	add := func(dir string) {
		// Dedupe by resolved path — /media is a symlink to /var/rootdirs/media,
		// so the same Lockbox is reachable under multiple scan roots.
		real := dir
		if r, err := filepath.EvalSymlinks(dir); err == nil {
			real = r
		}
		if seen[real] {
			return
		}
		if md, err := os.Stat(filepath.Join(dir, "metadata")); err != nil || !md.IsDir() {
			return
		}
		seen[real] = true
		_, imgErr := os.Stat(filepath.Join(dir, "images"))
		out = append(out, OfflineSource{Path: s.unhost(dir), HasImages: imgErr == nil})
	}
	for _, root := range []string{"media", "run/media", "var/rootdirs/media"} {
		base := filepath.Join(s.hostRoot, root)
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			d1 := filepath.Join(base, e.Name())
			add(d1) // the mount root
			if sub, err := os.ReadDir(d1); err == nil {
				for _, se := range sub { // one level down (mount/<subdir>)
					if se.IsDir() {
						add(filepath.Join(d1, se.Name()))
					}
				}
			}
		}
	}
	return out
}

// ValidateLockbox checks that a host path looks like a Lockbox (has metadata/).
func (s *Service) ValidateLockbox(hostPath string) (OfflineSource, error) {
	if hostPath == "" {
		return OfflineSource{}, errors.New("no path given")
	}
	dir := filepath.Join(s.hostRoot, strings.TrimPrefix(hostPath, "/"))
	if md, err := os.Stat(filepath.Join(dir, "metadata")); err != nil || !md.IsDir() {
		return OfflineSource{}, errors.New("no metadata/ directory found at " + hostPath)
	}
	_, imgErr := os.Stat(filepath.Join(dir, "images"))
	return OfflineSource{Path: hostPath, HasImages: imgErr == nil}, nil
}

// OfflineUpdate asks aktualizr to validate and install the Lockbox at the given
// host path (org.uptane.Aktualizr.OfflineUpdate). The device verifies the
// signatures against its own root of trust, installs, and reboots.
func (s *Service) OfflineUpdate(hostPath string) error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	return conn.Object(dbusDest, dbusPath).Call(dbusIface+".OfflineUpdate", 0, hostPath).Err
}

// unhost strips the container's host-root prefix, yielding the path as aktualizr
// (running on the host) sees it.
func (s *Service) unhost(dir string) string {
	if s.hostRoot == "" || s.hostRoot == "/" {
		return dir
	}
	p := strings.TrimPrefix(dir, s.hostRoot)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// restartAktualizr restarts the update client via the systemd D-Bus manager.
func (s *Service) restartAktualizr() error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()
	var job dbus.ObjectPath
	return conn.Object(systemdDest, systemdPath).
		Call(systemdMgr+".RestartUnit", 0, aktualizrUnit, "replace").Store(&job)
}

// ---- helpers ----

func (s *Service) connect() (*dbus.Conn, error) {
	conn, err := dbus.Dial(s.busAddr)
	if err != nil {
		return nil, err
	}
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return nil, err
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func (s *Service) hostPath(p string) string {
	return filepath.Join(s.hostRoot, strings.TrimPrefix(p, "/"))
}

// ostreeHash extracts the active deployment hash from the kernel command line
// ("ostree=/ostree/boot.1/torizon/<hash>/0").
func (s *Service) ostreeHash() string {
	b, err := os.ReadFile(s.hostPath("proc/cmdline"))
	if err != nil {
		return ""
	}
	for _, tok := range strings.Fields(string(b)) {
		if v, ok := strings.CutPrefix(tok, "ostree="); ok {
			parts := strings.Split(strings.Trim(v, "/"), "/")
			for i, p := range parts {
				if p == "torizon" && i+1 < len(parts) {
					return parts[i+1]
				}
			}
		}
	}
	return ""
}

func mergeToml(path string, kv map[string]string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "[") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		kv[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"`)
	}
}

func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
