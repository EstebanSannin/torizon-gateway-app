package cloud

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// processStatus reports whether the two cloud-critical daemons are running:
// aktualizr (OTA client) and the Torizon Remote Access Client (rac). It scans
// the host /proc (available via the host mount) for their process names — no
// systemd/D-Bus dependency, and it directly answers "is the process alive".
func (s *Service) processStatus() []ProcStatus {
	comms := scanComms(filepath.Join(s.hostRoot, "proc"))
	hasPrefix := func(p string) bool {
		for c := range comms {
			if strings.HasPrefix(c, p) {
				return true
			}
		}
		return false
	}
	return []ProcStatus{
		{Title: "Aktualizr (OTA client)", Unit: "aktualizr-torizon.service", Running: hasPrefix("aktualizr")},
		{Title: "Remote access", Unit: "remote-access.service", Running: comms["rac"]},
	}
}

// scanComms returns the set of process command names (/proc/<pid>/comm).
func scanComms(procDir string) map[string]bool {
	out := map[string]bool{}
	entries, err := os.ReadDir(procDir)
	if err != nil {
		return out
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue // not a PID dir
		}
		b, err := os.ReadFile(filepath.Join(procDir, e.Name(), "comm"))
		if err != nil {
			continue
		}
		out[strings.TrimSpace(string(b))] = true
	}
	return out
}
