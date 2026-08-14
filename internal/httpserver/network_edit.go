package httpserver

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/toradex/torizon-gateway-app/internal/network"
)

// rollbackWindow is how long the operator has to confirm a network change
// before NetworkManager auto-reverts it (anti-lockout).
const rollbackWindow = 120 * time.Second

// pendingChange tracks an applied-but-unconfirmed network change.
type pendingChange struct {
	Checkpoint string
	Device     string
	User       string
	Deadline   time.Time
}

// findIface returns the current config for one interface name.
func (s *Server) findIface(name string) (network.Iface, bool) {
	if s.network == nil {
		return network.Iface{}, false
	}
	ifaces, err := s.network.Interfaces()
	if err != nil {
		return network.Iface{}, false
	}
	for _, i := range ifaces {
		if i.Name == name {
			return i, true
		}
	}
	return network.Iface{}, false
}

// handleNetworkEdit renders the IPv4 edit form prefilled with current values.
func (s *Server) handleNetworkEdit(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("iface")
	iface, ok := s.findIface(name)
	if !ok {
		http.Error(w, "interface not found", http.StatusNotFound)
		return
	}
	addr, prefix := splitCIDR(iface.IPv4)
	method := "auto"
	if iface.Method == "Manual" {
		method = "manual"
	}
	render(w, "network_edit.html", struct {
		layout
		Iface   network.Iface
		Method  string
		Address string
		Prefix  string
		Gateway string
		DNS     string
	}{
		layout:  s.layoutFor(w, r, "Edit "+name, "network"),
		Iface:   iface,
		Method:  method,
		Address: addr,
		Prefix:  prefix,
		Gateway: iface.Gateway,
		DNS:     strings.Join(iface.DNS, ", "),
	})
}

// handleNetworkApply validates the form, applies the change behind a checkpoint,
// and shows the confirm-or-revert page.
func (s *Server) handleNetworkApply(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	name := r.PathValue("iface")
	cfg := network.IPv4Config{
		Method:  r.PostFormValue("method"),
		Address: strings.TrimSpace(r.PostFormValue("address")),
		Gateway: strings.TrimSpace(r.PostFormValue("gateway")),
		DNS:     splitList(r.PostFormValue("dns")),
	}
	if p, err := strconv.ParseUint(strings.TrimSpace(r.PostFormValue("prefix")), 10, 32); err == nil {
		cfg.Prefix = uint32(p)
	}

	cp, err := s.network.ApplyIPv4(name, cfg, uint32(rollbackWindow.Seconds()))
	if err != nil {
		_ = s.store.AddAudit(userFrom(r).Username, "network_apply_failed", name+": "+err.Error(), clientIP(r))
		http.Error(w, "Could not apply change: "+err.Error(), http.StatusBadRequest)
		return
	}

	token := newToken()
	s.pendMu.Lock()
	s.pending[token] = pendingChange{
		Checkpoint: cp, Device: name, User: userFrom(r).Username,
		Deadline: time.Now().Add(rollbackWindow),
	}
	s.pendMu.Unlock()
	_ = s.store.AddAudit(userFrom(r).Username, "network_apply", name+" (pending confirm)", clientIP(r))

	render(w, "network_confirm.html", struct {
		layout
		Device  string
		Token   string
		Seconds int
	}{
		layout:  s.layoutFor(w, r, "Confirm change", "network"),
		Device:  name,
		Token:   token,
		Seconds: int(rollbackWindow.Seconds()),
	})
}

// handleNetworkConfirm keeps a pending change (destroys the checkpoint).
func (s *Server) handleNetworkConfirm(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	pc, ok := s.takePending(r.PostFormValue("token"))
	if !ok {
		http.Redirect(w, r, "/network", http.StatusSeeOther)
		return
	}
	if err := s.network.Confirm(pc.Checkpoint); err != nil {
		http.Error(w, "confirm failed: "+err.Error(), http.StatusInternalServerError)
		return
	}
	_ = s.store.AddAudit(pc.User, "network_confirm", pc.Device, clientIP(r))
	http.Redirect(w, r, "/network", http.StatusSeeOther)
}

// handleNetworkCancel reverts a pending change immediately.
func (s *Server) handleNetworkCancel(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	pc, ok := s.takePending(r.PostFormValue("token"))
	if ok {
		_ = s.network.Rollback(pc.Checkpoint)
		_ = s.store.AddAudit(pc.User, "network_cancel", pc.Device, clientIP(r))
	}
	http.Redirect(w, r, "/network", http.StatusSeeOther)
}

// takePending removes and returns a pending change if present and not expired.
func (s *Server) takePending(token string) (pendingChange, bool) {
	s.pendMu.Lock()
	defer s.pendMu.Unlock()
	pc, ok := s.pending[token]
	if !ok {
		return pendingChange{}, false
	}
	delete(s.pending, token)
	if time.Now().After(pc.Deadline) {
		return pendingChange{}, false // NM already rolled back
	}
	return pc, true
}

// ---- helpers ----

func newToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// splitCIDR returns the address and prefix of the first "a.b.c.d/nn" entry.
func splitCIDR(list []string) (addr, prefix string) {
	if len(list) == 0 {
		return "", "24"
	}
	a, p, ok := strings.Cut(list[0], "/")
	if !ok {
		return a, "24"
	}
	return a, p
}

func splitList(s string) []string {
	fields := strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' })
	var out []string
	for _, f := range fields {
		if f = strings.TrimSpace(f); f != "" {
			out = append(out, f)
		}
	}
	return out
}
