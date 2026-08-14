package httpserver

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/toradex/torizon-gateway-app/internal/containers"
	"github.com/toradex/torizon-gateway-app/internal/hal"
	"github.com/toradex/torizon-gateway-app/internal/network"
)

// handleContainerAction performs start/stop/restart on a container, guarding
// against the gateway stopping itself. CSRF-protected and audited.
func (s *Server) handleContainerAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !checkCSRF(r) {
			http.Error(w, "invalid CSRF token", http.StatusForbidden)
			return
		}
		if s.containers == nil {
			http.Error(w, "docker unavailable", http.StatusServiceUnavailable)
			return
		}
		id := r.PathValue("id")
		// Guardrail: never stop/restart the gateway app itself.
		if (action == "stop" || action == "restart") && s.containers.IsSelf(id) {
			http.Error(w, "refusing to "+action+" the gateway app itself", http.StatusBadRequest)
			return
		}
		ctx := r.Context()
		var err error
		switch action {
		case "start":
			err = s.containers.Start(ctx, id)
		case "stop":
			err = s.containers.Stop(ctx, id)
		case "restart":
			err = s.containers.Restart(ctx, id)
		}
		user := userFrom(r).Username
		if err != nil {
			_ = s.store.AddAudit(user, "container_"+action+"_failed", id+": "+err.Error(), clientIP(r))
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		_ = s.store.AddAudit(user, "container_"+action, id, clientIP(r))
		http.Redirect(w, r, "/containers", http.StatusSeeOther)
	}
}

// networkData is the template model for the read-only Network view.
type networkData struct {
	layout
	Available bool
	Ifaces    []network.Iface
	Err       string
}

// handleNetwork lists host network interfaces from NetworkManager (read-only).
func (s *Server) handleNetwork(w http.ResponseWriter, r *http.Request) {
	data := networkData{layout: s.layoutFor(w, r, "Network", "network")}
	if s.network == nil || !s.network.Available() {
		render(w, "network.html", data)
		return
	}
	data.Available = true
	ifaces, err := s.network.Interfaces()
	if err != nil {
		data.Err = err.Error()
	}
	data.Ifaces = ifaces
	render(w, "network.html", data)
}

// handleContainerLogsPage renders the live-log viewer for one container.
func (s *Server) handleContainerLogsPage(w http.ResponseWriter, r *http.Request) {
	render(w, "logs.html", struct {
		layout
		ID string
	}{
		layout: s.layoutFor(w, r, "Logs", "containers"),
		ID:     r.PathValue("id"),
	})
}

// handleContainerLogsSSE streams a container's stdout/stderr as SSE "line"
// events. The client (htmx) appends each line to the log view.
func (s *Server) handleContainerLogsSSE(w http.ResponseWriter, r *http.Request) {
	if s.containers == nil {
		http.Error(w, "docker unavailable", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ctx := r.Context()
	stream, tty, err := s.containers.Logs(ctx, r.PathValue("id"), 200)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	// Close the upstream stream when the client disconnects, unblocking the read.
	go func() {
		<-ctx.Done()
		stream.Close()
	}()

	_ = containers.StreamLines(stream, tty, func(line string) {
		// One <div> per line; escape to prevent HTML injection from log content.
		safe := html.EscapeString(strings.TrimRight(line, "\r"))
		fmt.Fprintf(w, "event: line\ndata: <div>%s</div>\n\n", safe)
		flusher.Flush()
	})
}

// updatesData is the template model for the read-only Updates view.
type updatesData struct {
	layout
	PrettyName string
	VersionID  string
	Variant    string
	Codename   string
}

// handleUpdates shows the currently installed OS version. Applying offline
// (Lockbox) updates is Phase 3 — see docs/ARCHITECTURE.md §8.4.
func (s *Server) handleUpdates(w http.ResponseWriter, r *http.Request) {
	osr := hal.OSRelease()
	render(w, "updates.html", updatesData{
		layout:     s.layoutFor(w, r, "Updates", "updates"),
		PrettyName: osr["PRETTY_NAME"],
		VersionID:  osr["VERSION_ID"],
		Variant:    osr["VARIANT"],
		Codename:   osr["VERSION_CODENAME"],
	})
}
