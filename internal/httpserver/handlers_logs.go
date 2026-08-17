package httpserver

import (
	"fmt"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/toradex/torizon-gateway-app/internal/logs"
)

// validUnit guards the journalctl -u argument (defense in depth; it's passed as
// an argv value, never through a shell). No leading dash, no shell/space chars.
var validUnit = regexp.MustCompile(`^[A-Za-z0-9@:._\\-]+$`)

// handleLogsPage renders the system-log viewer (journal + kernel).
func (s *Server) handleLogsPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	render(w, "journal.html", struct {
		layout
		Available bool
		Units     []string
	}{
		layout:    s.layoutFor(w, r, "System logs", "logs"),
		Available: s.syslogs != nil && s.syslogs.Available(ctx),
		Units:     s.syslogs.Units(ctx),
	})
}

// handleLogboxFragment renders the streaming log box with an SSE URL carrying
// the current filters (htmx swaps this in whenever a filter changes).
func (s *Server) handleLogboxFragment(w http.ResponseWriter, r *http.Request) {
	q := url.Values{}
	if u := r.URL.Query().Get("unit"); u != "" && validUnit.MatchString(u) {
		q.Set("unit", u)
	}
	if r.URL.Query().Get("kernel") == "1" {
		q.Set("kernel", "1")
	}
	if t := r.URL.Query().Get("tail"); t != "" {
		q.Set("tail", t)
	}
	renderFragment(w, "fragment_logbox.html", "logbox", struct{ Query string }{Query: q.Encode()})
}

// handleJournalSSE streams journalctl -f output as SSE "line" events.
func (s *Server) handleJournalSSE(w http.ResponseWriter, r *http.Request) {
	if s.syslogs == nil {
		http.Error(w, "logs unavailable", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	opts := logs.Options{
		Kernel: r.URL.Query().Get("kernel") == "1",
		Tail:   atoiDefault(r.URL.Query().Get("tail"), 200),
	}
	if u := r.URL.Query().Get("unit"); u != "" && validUnit.MatchString(u) {
		opts.Unit = u
	}

	ctx := r.Context()
	stream, err := s.syslogs.Stream(ctx, opts)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer stream.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	go func() { <-ctx.Done(); stream.Close() }()

	logs.ScanLines(stream, func(line string) {
		fmt.Fprintf(w, "event: line\ndata: <div>%s</div>\n\n", html.EscapeString(strings.TrimRight(line, "\r")))
		flusher.Flush()
	})
}

func atoiDefault(s string, def int) int {
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}
