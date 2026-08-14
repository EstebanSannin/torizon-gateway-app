// Package httpserver wires the HTTP(S) server: routing, template rendering,
// static asset serving, and (later) auth middleware + SSE. Phase-0 scaffold.
package httpserver

import (
	"fmt"
	"io/fs"
	"net/http"
	"time"

	"github.com/toradex/torizon-gateway-app/internal/config"
	"github.com/toradex/torizon-gateway-app/internal/containers"
	"github.com/toradex/torizon-gateway-app/internal/hal"
	"github.com/toradex/torizon-gateway-app/internal/sysinfo"
	"github.com/toradex/torizon-gateway-app/web"
)

// Server holds shared dependencies for handlers.
type Server struct {
	cfg        config.Config
	board      hal.BoardInfo
	containers *containers.Service
	mux        *http.ServeMux
}

// New builds the server and registers routes.
func New(cfg config.Config, board hal.BoardInfo, cnt *containers.Service) *Server {
	s := &Server{cfg: cfg, board: board, containers: cnt, mux: http.NewServeMux()}
	s.routes()
	return s
}

// Handler exposes the router (for the http.Server in main).
func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	// Static assets (embedded).
	staticFS, _ := fs.Sub(web.Static, "static")
	s.mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	// Health probe (for Docker/compose healthcheck).
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	// Pages.
	s.mux.HandleFunc("GET /{$}", s.handleDashboard)
	s.mux.HandleFunc("GET /network", s.placeholder("network", "Network"))
	s.mux.HandleFunc("GET /containers", s.handleContainers)
	s.mux.HandleFunc("GET /updates", s.placeholder("updates", "Updates"))

	// Live metrics stream (SSE) — stub emits a snapshot every 3s.
	s.mux.HandleFunc("GET /sse/metrics", s.handleMetricsSSE)
}

// dashData is the template model for the dashboard.
type dashData struct {
	Title, Nav     string
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
		Title: "Dashboard", Nav: "dashboard",
		Board: s.board, Metrics: m,
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
	Title, Nav string
	Available  bool
	Containers []containers.Container
	Err        string
}

func (s *Server) handleContainers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := containersData{Title: "Containers", Nav: "containers"}
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
		renderInline(w, content, map[string]any{"Title": title, "Nav": nav})
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
