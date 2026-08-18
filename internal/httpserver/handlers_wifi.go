package httpserver

import (
	"net/http"
	"strings"
	"time"

	"github.com/toradex/torizon-gateway-app/internal/network"
)

// wifiData is the model for the Wi-Fi section (full fragment) and its network
// list sub-fragment.
type wifiData struct {
	CSRF      string
	Stations  []string // Wi-Fi station interfaces (for the selector)
	Iface     string   // selected interface
	Connected *network.WiFiConnection
	Nets      []network.WiFiAP
	Saved     []string
	Scanned   bool
	Notice    string
	IsError   bool
}

// wifiIface resolves the selected Wi-Fi interface from the request, falling back
// to the first station. Returns the choice and the full station list.
func (s *Server) wifiIface(r *http.Request) (string, []string) {
	var stations []string
	if s.network != nil && s.network.Available() {
		stations, _ = s.network.WiFiStations()
	}
	sel := r.FormValue("iface")
	for _, st := range stations {
		if st == sel {
			return sel, stations
		}
	}
	if len(stations) > 0 {
		return stations[0], stations
	}
	return "", stations
}

// renderWiFi renders the whole Wi-Fi section (selector, connected panel, saved,
// and the network list area). scanned controls whether the list is populated.
func (s *Server) renderWiFi(w http.ResponseWriter, r *http.Request, iface string, stations []string, scanned bool, notice string, isErr bool) {
	data := wifiData{
		CSRF: s.ensureCSRF(w, r), Stations: stations, Iface: iface,
		Scanned: scanned, Notice: notice, IsError: isErr,
	}
	if iface != "" {
		data.Connected = s.network.WiFiActive(iface)
		data.Saved, _ = s.network.WiFiSaved()
		if scanned {
			data.Nets, _, _ = s.network.WiFiNetworks(iface)
		}
	}
	renderFragment(w, "fragment_wifi.html", "wifi", data)
}

// handleWiFiFragment renders the section on load (no scan yet).
func (s *Server) handleWiFiFragment(w http.ResponseWriter, r *http.Request) {
	iface, stations := s.wifiIface(r)
	s.renderWiFi(w, r, iface, stations, false, "", false)
}

// handleWiFiScan triggers a rescan and renders just the results list into the
// #wifi-networks sub-area (leaving the connected panel/selector untouched).
func (s *Server) handleWiFiScan(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	iface, _ := s.wifiIface(r)
	data := wifiData{CSRF: s.ensureCSRF(w, r), Iface: iface, Scanned: true}
	if iface == "" {
		data.Notice, data.IsError = "No Wi-Fi interface available.", true
		renderFragment(w, "fragment_wifi.html", "wifinets", data)
		return
	}
	if err := s.network.WiFiScan(iface); err != nil {
		data.Notice, data.IsError = "Scan failed: "+err.Error(), true
		renderFragment(w, "fragment_wifi.html", "wifinets", data)
		return
	}
	time.Sleep(2 * time.Second) // let NetworkManager collect results
	data.Nets, _, _ = s.network.WiFiNetworks(iface)
	renderFragment(w, "fragment_wifi.html", "wifinets", data)
}

// handleWiFiConnect joins a network then re-renders the whole section so the
// connected panel updates and the modal closes.
func (s *Server) handleWiFiConnect(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	iface, stations := s.wifiIface(r)
	ssid := strings.TrimSpace(r.PostFormValue("ssid"))
	password := r.PostFormValue("password")
	if iface == "" || ssid == "" {
		s.renderWiFi(w, r, iface, stations, true, "Missing network.", true)
		return
	}
	if err := s.network.WiFiConnect(iface, ssid, password); err != nil {
		_ = s.store.AddAudit(userFrom(r).Username, "wifi_connect_failed", ssid+": "+err.Error(), clientIP(r))
		s.renderWiFi(w, r, iface, stations, true, "Could not connect to "+ssid+": "+err.Error(), true)
		return
	}
	_ = s.store.AddAudit(userFrom(r).Username, "wifi_connect", ssid, clientIP(r))
	time.Sleep(3 * time.Second) // allow association + DHCP before reading state
	s.renderWiFi(w, r, iface, stations, true, "", false)
}

// handleWiFiDisconnect drops the active connection and resets to a clean state.
func (s *Server) handleWiFiDisconnect(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	iface, stations := s.wifiIface(r)
	if iface != "" {
		if err := s.network.WiFiDisconnect(iface); err != nil {
			s.renderWiFi(w, r, iface, stations, false, "Disconnect failed: "+err.Error(), true)
			return
		}
		_ = s.store.AddAudit(userFrom(r).Username, "wifi_disconnect", iface, clientIP(r))
	}
	s.renderWiFi(w, r, iface, stations, false, "", false)
}

// handleWiFiForget deletes a saved profile and re-renders the section.
func (s *Server) handleWiFiForget(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	iface, stations := s.wifiIface(r)
	ssid := strings.TrimSpace(r.PostFormValue("ssid"))
	if ssid != "" {
		if err := s.network.WiFiForget(ssid); err != nil {
			s.renderWiFi(w, r, iface, stations, false, "Could not forget "+ssid+": "+err.Error(), true)
			return
		}
		_ = s.store.AddAudit(userFrom(r).Username, "wifi_forget", ssid, clientIP(r))
	}
	s.renderWiFi(w, r, iface, stations, false, "Forgot "+ssid+".", false)
}
