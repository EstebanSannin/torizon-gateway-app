package httpserver

import (
	"net/http"
	"strings"
	"time"

	"github.com/toradex/torizon-gateway-app/internal/network"
)

// wifiData is the model for the Wi-Fi fragment (network page section).
type wifiData struct {
	CSRF    string
	Station string // Wi-Fi interface name; "" when the device has none
	Active  string // SSID currently connected, if any
	Nets    []network.WiFiAP
	Saved   []string
	Notice  string // status or error to surface after an action
	IsError bool
}

// wifiStation returns the first Wi-Fi station interface, or "" if none.
func (s *Server) wifiStation() string {
	if s.network == nil || !s.network.Available() {
		return ""
	}
	stations, err := s.network.WiFiStations()
	if err != nil || len(stations) == 0 {
		return ""
	}
	return stations[0]
}

// renderWiFi builds and renders the Wi-Fi fragment for the current station.
func (s *Server) renderWiFi(w http.ResponseWriter, r *http.Request, notice string, isErr bool) {
	data := wifiData{
		CSRF:    s.ensureCSRF(w, r),
		Station: s.wifiStation(),
		Notice:  notice,
		IsError: isErr,
	}
	if data.Station != "" {
		data.Nets, data.Active, _ = s.network.WiFiNetworks(data.Station)
		data.Saved, _ = s.network.WiFiSaved()
	}
	renderFragment(w, "fragment_wifi.html", "wifi", data)
}

// handleWiFiFragment renders the Wi-Fi networks section (htmx load target).
func (s *Server) handleWiFiFragment(w http.ResponseWriter, r *http.Request) {
	s.renderWiFi(w, r, "", false)
}

// handleWiFiScan triggers a rescan, gives it a moment, then re-renders the list.
func (s *Server) handleWiFiScan(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	station := s.wifiStation()
	if station == "" {
		s.renderWiFi(w, r, "No Wi-Fi interface available.", true)
		return
	}
	if err := s.network.WiFiScan(station); err != nil {
		s.renderWiFi(w, r, "Scan failed: "+err.Error(), true)
		return
	}
	time.Sleep(2 * time.Second) // let NetworkManager collect results
	s.renderWiFi(w, r, "", false)
}

// handleWiFiConnect joins a network (creating/activating a profile).
func (s *Server) handleWiFiConnect(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	station := s.wifiStation()
	ssid := strings.TrimSpace(r.PostFormValue("ssid"))
	password := r.PostFormValue("password")
	if station == "" || ssid == "" {
		s.renderWiFi(w, r, "Missing network.", true)
		return
	}
	if err := s.network.WiFiConnect(station, ssid, password); err != nil {
		_ = s.store.AddAudit(userFrom(r).Username, "wifi_connect_failed", ssid+": "+err.Error(), clientIP(r))
		s.renderWiFi(w, r, "Could not connect to "+ssid+": "+err.Error(), true)
		return
	}
	_ = s.store.AddAudit(userFrom(r).Username, "wifi_connect", ssid, clientIP(r))
	time.Sleep(3 * time.Second) // allow association + DHCP before showing state
	s.renderWiFi(w, r, "Connecting to "+ssid+"…", false)
}

// handleWiFiDisconnect drops the active Wi-Fi connection.
func (s *Server) handleWiFiDisconnect(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	station := s.wifiStation()
	if station == "" {
		s.renderWiFi(w, r, "No Wi-Fi interface available.", true)
		return
	}
	if err := s.network.WiFiDisconnect(station); err != nil {
		s.renderWiFi(w, r, "Disconnect failed: "+err.Error(), true)
		return
	}
	_ = s.store.AddAudit(userFrom(r).Username, "wifi_disconnect", station, clientIP(r))
	s.renderWiFi(w, r, "", false)
}

// handleWiFiForget deletes a saved Wi-Fi profile.
func (s *Server) handleWiFiForget(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	ssid := strings.TrimSpace(r.PostFormValue("ssid"))
	if ssid == "" {
		s.renderWiFi(w, r, "", false)
		return
	}
	if err := s.network.WiFiForget(ssid); err != nil {
		s.renderWiFi(w, r, "Could not forget "+ssid+": "+err.Error(), true)
		return
	}
	_ = s.store.AddAudit(userFrom(r).Username, "wifi_forget", ssid, clientIP(r))
	s.renderWiFi(w, r, "Forgot "+ssid+".", false)
}
