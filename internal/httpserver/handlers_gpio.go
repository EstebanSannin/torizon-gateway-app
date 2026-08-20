package httpserver

import (
	"net/http"
	"strconv"

	"github.com/toradex/torizon-gateway-app/internal/gpio"
)

// gpioData is the model for the GPIO page + its swappable body fragment.
type gpioData struct {
	layout
	Chips    []gpio.Chip
	Writable bool
	Notice   string
	IsError  bool
}

// handleGPIO renders the full GPIO page.
func (s *Server) handleGPIO(w http.ResponseWriter, r *http.Request) {
	render(w, "gpio.html", s.gpioData(w, r, "", false))
}

// handleGPIORead momentarily reads a free line's value, then swaps just that row.
func (s *Server) handleGPIORead(w http.ResponseWriter, r *http.Request) {
	chip, off, ok := s.gpioParams(w, r)
	if !ok {
		return
	}
	_, _ = s.gpio.ReadLine(chip, off) // value is picked up by the row render
	s.renderGPIORow(w, r, chip, off)
}

// handleGPIOSet drives a free line and holds it, then swaps just that row.
func (s *Server) handleGPIOSet(w http.ResponseWriter, r *http.Request) {
	chip, off, ok := s.gpioParams(w, r)
	if !ok {
		return
	}
	val, _ := strconv.Atoi(r.PostFormValue("value"))
	off10 := strconv.FormatUint(uint64(off), 10)
	if err := s.gpio.SetLine(chip, off, val); err != nil {
		_ = s.store.AddAudit(userFrom(r).Username, "gpio_set_failed", chip+"/"+off10+": "+err.Error(), clientIP(r))
	} else {
		_ = s.store.AddAudit(userFrom(r).Username, "gpio_set", chip+"/"+off10+"="+strconv.Itoa(val), clientIP(r))
	}
	s.renderGPIORow(w, r, chip, off)
}

// handleGPIORelease hands a held line back, then swaps just that row.
func (s *Server) handleGPIORelease(w http.ResponseWriter, r *http.Request) {
	chip, off, ok := s.gpioParams(w, r)
	if !ok {
		return
	}
	_ = s.gpio.ReleaseLine(chip, off)
	_ = s.store.AddAudit(userFrom(r).Username, "gpio_release", chip+"/"+strconv.FormatUint(uint64(off), 10), clientIP(r))
	s.renderGPIORow(w, r, chip, off)
}

// renderGPIORow renders a single line's <tr> (targeted htmx swap).
func (s *Server) renderGPIORow(w http.ResponseWriter, r *http.Request, chip string, off uint32) {
	ln, err := s.gpio.LineState(chip, off)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	renderFragment(w, "gpio.html", "gpiorow", map[string]any{
		"Line": ln, "Chip": chip, "Writable": s.gpio.Writable(), "CSRF": s.ensureCSRF(w, r),
	})
}

// gpioParams validates CSRF and parses chip + offset from the form.
func (s *Server) gpioParams(w http.ResponseWriter, r *http.Request) (string, uint32, bool) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return "", 0, false
	}
	chip := r.PostFormValue("chip")
	off, err := strconv.ParseUint(r.PostFormValue("offset"), 10, 32)
	if chip == "" || err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return "", 0, false
	}
	return chip, uint32(off), true
}

func (s *Server) gpioData(w http.ResponseWriter, r *http.Request, notice string, isErr bool) gpioData {
	d := gpioData{layout: s.layoutFor(w, r, "GPIO", "gpio"), Notice: notice, IsError: isErr}
	if s.gpio != nil {
		d.Chips = s.gpio.Chips()
		d.Writable = s.gpio.Writable()
	}
	return d
}
