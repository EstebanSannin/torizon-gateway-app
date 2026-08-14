package httpserver

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/toradex/torizon-gateway-app/internal/containers"
	"github.com/toradex/torizon-gateway-app/internal/hal"
)

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
