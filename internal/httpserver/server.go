// Package httpserver wires the HTTP(S) server: routing, template rendering,
// static asset serving, and (later) auth middleware + SSE. Phase-0 scaffold.
package httpserver

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/toradex/torizon-gateway-app/internal/auth"
	"github.com/toradex/torizon-gateway-app/internal/cloud"
	"github.com/toradex/torizon-gateway-app/internal/config"
	"github.com/toradex/torizon-gateway-app/internal/containers"
	"github.com/toradex/torizon-gateway-app/internal/files"
	"github.com/toradex/torizon-gateway-app/internal/hal"
	"github.com/toradex/torizon-gateway-app/internal/logs"
	"github.com/toradex/torizon-gateway-app/internal/network"
	"github.com/toradex/torizon-gateway-app/internal/store"
	"github.com/toradex/torizon-gateway-app/internal/sysinfo"
	"github.com/toradex/torizon-gateway-app/internal/terminal"
	"github.com/toradex/torizon-gateway-app/web"
)

// Server holds shared dependencies for handlers.
type Server struct {
	cfg         config.Config
	board       hal.BoardInfo
	containers  *containers.Service
	network     *network.Service
	auth        *auth.Service
	store       *store.Store
	peripherals *sysinfo.Peripherals
	syslogs     *logs.Service
	files       *files.Service
	terminal    *terminal.Service
	cloud       *cloud.Service
	mux         *http.ServeMux

	pendMu  sync.Mutex
	pending map[string]pendingChange // token → pending network change
}

// New builds the server and registers routes.
func New(cfg config.Config, board hal.BoardInfo, cnt *containers.Service, net *network.Service, a *auth.Service, st *store.Store) *Server {
	s := &Server{
		cfg: cfg, board: board, containers: cnt, network: net, auth: a, store: st,
		peripherals: sysinfo.NewPeripherals(cfg.SysfsPath, cfg.HostRoot),
		syslogs:     logs.New(cfg.HostRoot),
		files:       files.New(cfg.HostRoot, cfg.FilesWritable),
		terminal:    terminal.New(cfg.TerminalEnabled, cfg.TerminalSSHAddr),
		cloud:       cloud.New(cfg.HostRoot, cfg.DataDir),
		mux:         http.NewServeMux(),
		pending:     make(map[string]pendingChange),
	}
	s.routes()
	return s
}

// Handler exposes the router (for the http.Server in main).
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	// Static assets (embedded).
	staticFS, _ := fs.Sub(web.Static, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Health probe (for Docker/compose healthcheck) — public.
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// Auth (public) — first-boot setup, login, logout.
	s.mux.HandleFunc("GET /setup", s.handleSetupPage)
	s.mux.HandleFunc("POST /setup", s.handleSetupPost)
	s.mux.HandleFunc("GET /login", s.handleLoginPage)
	s.mux.HandleFunc("POST /login", s.handleLoginPost)
	s.mux.HandleFunc("POST /logout", s.requireAuth(s.handleLogout))

	// Pages (protected).
	s.mux.HandleFunc("GET /{$}", s.requireAuth(s.handleDashboard))
	s.mux.HandleFunc("GET /network", s.requireAuth(s.handleNetwork))
	s.mux.HandleFunc("GET /network/{iface}/edit", s.requireAuth(s.handleNetworkEdit))
	s.mux.HandleFunc("POST /network/{iface}/apply", s.requireAuth(s.handleNetworkApply))
	s.mux.HandleFunc("POST /network/confirm", s.requireAuth(s.handleNetworkConfirm))
	s.mux.HandleFunc("POST /network/cancel", s.requireAuth(s.handleNetworkCancel))
	s.mux.HandleFunc("GET /network/wifi", s.requireAuth(s.handleWiFiFragment))
	s.mux.HandleFunc("POST /network/wifi/scan", s.requireAuth(s.handleWiFiScan))
	s.mux.HandleFunc("POST /network/wifi/connect", s.requireAuth(s.handleWiFiConnect))
	s.mux.HandleFunc("POST /network/wifi/disconnect", s.requireAuth(s.handleWiFiDisconnect))
	s.mux.HandleFunc("POST /network/wifi/forget", s.requireAuth(s.handleWiFiForget))
	s.mux.HandleFunc("GET /containers", s.requireAuth(s.handleContainers))
	s.mux.HandleFunc("GET /containers/{id}/logs", s.requireAuth(s.handleContainerLogsPage))
	s.mux.HandleFunc("POST /containers/{id}/start", s.requireAuth(s.handleContainerAction("start")))
	s.mux.HandleFunc("POST /containers/{id}/stop", s.requireAuth(s.handleContainerAction("stop")))
	s.mux.HandleFunc("POST /containers/{id}/restart", s.requireAuth(s.handleContainerAction("restart")))
	s.mux.HandleFunc("GET /updates", s.requireAuth(s.handleUpdates))
	s.mux.HandleFunc("GET /cloud", s.requireAuth(s.handleCloudPage))
	s.mux.HandleFunc("GET /logs", s.requireAuth(s.handleLogsPage))
	s.mux.HandleFunc("GET /files", s.requireAuth(s.handleFiles))
	s.mux.HandleFunc("GET /files/view", s.requireAuth(s.handleFileView))
	s.mux.HandleFunc("GET /files/edit", s.requireAuth(s.handleFileEdit))
	s.mux.HandleFunc("POST /files/save", s.requireAuth(s.handleFileSave))
	s.mux.HandleFunc("POST /files/upload", s.requireAuth(s.handleFileUpload))
	s.mux.HandleFunc("POST /files/delete", s.requireAuth(s.handleFileDelete))
	s.mux.HandleFunc("GET /terminal", s.requireAuth(s.handleTerminalPage))
	s.mux.HandleFunc("GET /ws/terminal", s.requireAuth(s.handleTerminalWS))

	// HTML fragments (htmx) — protected.
	s.mux.HandleFunc("GET /fragment/peripherals", s.requireAuth(s.handlePeripheralsFragment))
	s.mux.HandleFunc("GET /fragment/cloud", s.requireAuth(s.handleCloudFragment))
	s.mux.HandleFunc("GET /fragment/logbox", s.requireAuth(s.handleLogboxFragment))

	// Live streams (SSE) — protected.
	s.mux.HandleFunc("GET /sse/metrics", s.requireAuth(s.handleMetricsSSE))
	s.mux.HandleFunc("GET /sse/logs/{id}", s.requireAuth(s.handleContainerLogsSSE))
	s.mux.HandleFunc("GET /sse/journal", s.requireAuth(s.handleJournalSSE))
}

// layout holds fields every authenticated page needs (nav highlight, current
// user, CSRF token, device identity for the topbar). Page data structs embed it.
type layout struct {
	Title, Nav string
	User       string
	CSRF       string
	Device     string // board model, shown in the topbar chip
}

// layoutFor builds the common layout fields for a protected page.
func (s *Server) layoutFor(w http.ResponseWriter, r *http.Request, title, nav string) layout {
	return layout{
		Title:  title,
		Nav:    nav,
		User:   userFrom(r).Username,
		CSRF:   s.ensureCSRF(w, r),
		Device: s.board.Model(),
	}
}

// dashData is the template model for the dashboard.
type dashData struct {
	layout
	Board          hal.BoardInfo
	Metrics        hal.Metrics
	MemUsedHuman   string
	MemTotalHuman  string
	UptimeHuman    string
	Disk           sysinfo.Disk
	DiskTotalHuman string
	DiskUsedHuman  string
	NetIface       string // primary connected interface (summary)
	NetIPv4        string
	CPU            sysinfo.CPU
	CPUMinHuman    string
	CPUMaxHuman    string
	CPUMinMHz      int
	CPUMaxMHz      int
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	m, _ := s.board.Metrics()
	// Storage of the data dir — a real host partition when the data volume is a
	// host bind mount (as it is on-device).
	disk, _ := sysinfo.DiskUsage(s.cfg.DataDir)
	data := dashData{
		layout:         s.layoutFor(w, r, "Dashboard", "dashboard"),
		Board:          s.board,
		Metrics:        m,
		MemUsedHuman:   humanBytes(m.MemUsedBytes),
		MemTotalHuman:  humanBytes(m.MemTotalBytes),
		UptimeHuman:    humanDuration(m.UptimeSeconds),
		Disk:           disk,
		DiskTotalHuman: humanBytes(disk.TotalBytes),
		DiskUsedHuman:  humanBytes(disk.UsedBytes),
	}
	data.NetIface, data.NetIPv4 = s.primaryConnection()
	data.CPU = sysinfo.CPUInfo(s.cfg.SysfsPath)
	data.CPUMinHuman = freqHuman(data.CPU.MinKHz)
	data.CPUMaxHuman = freqHuman(data.CPU.MaxKHz)
	data.CPUMinMHz = data.CPU.MinKHz / 1000
	data.CPUMaxMHz = data.CPU.MaxKHz / 1000
	render(w, "dashboard.html", data)
}

// primaryConnection returns the first connected interface with an IPv4 address
// (a compact connectivity summary for the dashboard). Empty when unavailable.
func (s *Server) primaryConnection() (iface, ipv4 string) {
	if s.network == nil || !s.network.Available() {
		return "", ""
	}
	ifaces, err := s.network.Interfaces()
	if err != nil {
		return "", ""
	}
	for _, i := range ifaces {
		if i.State == "Connected" && len(i.IPv4) > 0 {
			return i.Name, i.IPv4[0]
		}
	}
	return "", ""
}

// containersData is the template model for the container list.
type containersData struct {
	layout
	Available  bool
	Containers []containers.Container
	Err        string
}

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := containersData{layout: s.layoutFor(w, r, "Containers", "containers")}
	if s.containers == nil || !s.containers.Available(ctx) {
		data.Available = false
		render(w, "containers.html", data)
		return
	}
	data.Available = true
	list, err := s.containers.List(ctx)
	if err != nil {
		data.Err = err.Error()
	}
	data.Containers = list
	render(w, "containers.html", data)
}

// placeholder renders a simple "coming soon" page for not-yet-built sections.
func (s *Server) placeholder(nav, title string) http.HandlerFunc {
	content := `{{define "content"}}
		<div class="page-header"><h1>` + title + `</h1></div>
		<div class="card"><p>This section is part of the roadmap and not yet implemented.</p>
		<p class="label">See docs/ARCHITECTURE.md for the plan.</p></div>{{end}}`
	return func(w http.ResponseWriter, r *http.Request) {
		renderInline(w, content, s.layoutFor(w, r, title, nav))
	}
}

func (s *Server) handleMetricsSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	warn, alarm, scale := sysinfo.ThermalLimits(s.cfg.SysfsPath)
	iface := sysinfo.DefaultIface()
	prevRx, prevTx := sysinfo.NetCounters(s.cfg.SysfsPath, iface)
	prevIdle, prevTotal := sysinfo.CPUSample()
	prevT := time.Now()

	emit := func() {
		m, _ := s.board.Metrics()
		now := time.Now()
		dt := now.Sub(prevT).Seconds()

		// CPU utilisation from /proc/stat deltas (true 0–100%).
		idle, total := sysinfo.CPUSample()
		var cpuPct float64
		if dTot := total - prevTotal; dTot > 0 {
			cpuPct = (1 - float64(idle-prevIdle)/float64(dTot)) * 100
		}
		prevIdle, prevTotal = idle, total

		// Network throughput (rate = delta bytes / elapsed).
		rx, tx := sysinfo.NetCounters(s.cfg.SysfsPath, iface)
		var rxRate, txRate float64
		if dt > 0 {
			rxRate = float64(rx-prevRx) / dt
			txRate = float64(tx-prevTx) / dt
		}
		prevRx, prevTx, prevT = rx, tx, now

		var memPct float64
		if m.MemTotalBytes > 0 {
			memPct = float64(m.MemUsedBytes) / float64(m.MemTotalBytes) * 100
		}

		tick := metricsTick{
			CPU: round1(cpuPct), Load: m.CPULoad1,
			Mem: round1(memPct), MemUsed: m.MemUsedBytes, MemTotal: m.MemTotalBytes,
			Temp: round1(m.SoCTempCelsius), TempWarn: warn, TempAlarm: alarm, TempScale: scale,
			FreqCur: sysinfo.CPUCurrentKHz(s.cfg.SysfsPath) / 1000,
			Uptime:  humanDuration(m.UptimeSeconds), Rx: rxRate, Tx: txRate, Iface: iface,
		}
		buf, _ := json.Marshal(tick)
		fmt.Fprintf(w, "event: tick\ndata: %s\n\n", buf)
		flusher.Flush()
	}

	emit() // paint immediately, don't wait a full tick
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			emit()
		}
	}
}

// metricsTick is the JSON payload pushed once per second to the dashboard's live
// section. Numbers only — the browser renders the gauges, bars and chart.
type metricsTick struct {
	CPU       float64 `json:"cpu"`  // utilisation %
	Load      float64 `json:"load"` // 1-min load average
	Mem       float64 `json:"mem"`  // used %
	MemUsed   uint64  `json:"memUsed"`
	MemTotal  uint64  `json:"memTotal"`
	Temp      float64 `json:"temp"`      // °C
	TempWarn  float64 `json:"tempWarn"`  // amber threshold °C
	TempAlarm float64 `json:"tempAlarm"` // red threshold °C
	TempScale float64 `json:"tempScale"` // gauge full-scale °C
	FreqCur   int     `json:"freqCur"`   // MHz
	Uptime    string  `json:"uptime"`
	Rx        float64 `json:"rx"` // bytes/s
	Tx        float64 `json:"tx"`
	Iface     string  `json:"iface"`
}

func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }
