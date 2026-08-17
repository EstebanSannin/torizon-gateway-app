// Package httpserver wires the HTTP(S) server: routing, template rendering,
// static asset serving, and (later) auth middleware + SSE. Phase-0 scaffold.
package httpserver

import (
	"fmt"
	"io/fs"
	"net/http"
	"sync"
	"time"

	"github.com/toradex/torizon-gateway-app/internal/auth"
	"github.com/toradex/torizon-gateway-app/internal/config"
	"github.com/toradex/torizon-gateway-app/internal/containers"
	"github.com/toradex/torizon-gateway-app/internal/hal"
	"github.com/toradex/torizon-gateway-app/internal/network"
	"github.com/toradex/torizon-gateway-app/internal/store"
	"github.com/toradex/torizon-gateway-app/internal/sysinfo"
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
	mux         *http.ServeMux

	pendMu  sync.Mutex
	pending map[string]pendingChange // token → pending network change
}

// New builds the server and registers routes.
func New(cfg config.Config, board hal.BoardInfo, cnt *containers.Service, net *network.Service, a *auth.Service, st *store.Store) *Server {
	s := &Server{
		cfg: cfg, board: board, containers: cnt, network: net, auth: a, store: st,
		peripherals: sysinfo.NewPeripherals(cfg.SysfsPath, cfg.HostRoot),
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
	s.mux.HandleFunc("GET /containers", s.requireAuth(s.handleContainers))
	s.mux.HandleFunc("GET /containers/{id}/logs", s.requireAuth(s.handleContainerLogsPage))
	s.mux.HandleFunc("POST /containers/{id}/start", s.requireAuth(s.handleContainerAction("start")))
	s.mux.HandleFunc("POST /containers/{id}/stop", s.requireAuth(s.handleContainerAction("stop")))
	s.mux.HandleFunc("POST /containers/{id}/restart", s.requireAuth(s.handleContainerAction("restart")))
	s.mux.HandleFunc("GET /updates", s.requireAuth(s.handleUpdates))

	// HTML fragments (htmx polling) — protected.
	s.mux.HandleFunc("GET /fragment/peripherals", s.requireAuth(s.handlePeripheralsFragment))

	// Live streams (SSE) — protected.
	s.mux.HandleFunc("GET /sse/metrics", s.requireAuth(s.handleMetricsSSE))
	s.mux.HandleFunc("GET /sse/logs/{id}", s.requireAuth(s.handleContainerLogsSSE))
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
	UptimeHuman    string
	Disk           sysinfo.Disk
	DiskTotalHuman string
	DiskUsedHuman  string
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
		UptimeHuman:    humanDuration(m.UptimeSeconds),
		Disk:           disk,
		DiskTotalHuman: humanBytes(disk.TotalBytes),
		DiskUsedHuman:  humanBytes(disk.UsedBytes),
	}
	render(w, "dashboard.html", data)
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

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			m, _ := s.board.Metrics()
			// htmx SSE: one named event per swap target.
			fmt.Fprintf(w, "event: cpu\ndata: %.2f\n\n", m.CPULoad1)
			fmt.Fprintf(w, "event: mem\ndata: %s\n\n", humanBytes(m.MemUsedBytes))
			fmt.Fprintf(w, "event: temp\ndata: %s\n\n", tempStr(m.SoCTempCelsius))
			fmt.Fprintf(w, "event: uptime\ndata: %s\n\n", humanDuration(m.UptimeSeconds))
			flusher.Flush()
		}
	}
}
