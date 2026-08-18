package httpserver

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
)

// wsUpgrader upgrades the terminal WebSocket. The default CheckOrigin rejects
// cross-origin requests (Origin must match Host), which — together with the
// auth cookie — protects the endpoint from cross-site use.
var wsUpgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}

// termAuth is the first WebSocket message from the client.
type termAuth struct {
	User     string `json:"user"`
	Password string `json:"password"`
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

// termCtrl is a control message (e.g. resize) sent as a text frame.
type termCtrl struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

// handleTerminalPage renders the terminal UI (or a disabled notice).
func (s *Server) handleTerminalPage(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil || !s.terminal.Enabled() {
		content := `{{define "content"}}<div class="card"><p style="margin:0">The web terminal is disabled.</p>
			<p class="muted" style="margin-bottom:0">Enable it with <code>GATEWAY_TERMINAL_ENABLED=1</code>.</p></div>{{end}}`
		renderInline(w, content, s.layoutFor(w, r, "Terminal", "terminal"))
		return
	}
	render(w, "terminal.html", s.layoutFor(w, r, "Terminal", "terminal"))
}

// handleTerminalWS bridges a browser terminal (xterm.js) to an SSH shell.
// Protocol: first text frame = JSON auth; then binary frames = keystrokes,
// text frames = control (resize). Server → client: binary = shell output.
func (s *Server) handleTerminalWS(w http.ResponseWriter, r *http.Request) {
	if s.terminal == nil || !s.terminal.Enabled() {
		http.Error(w, "terminal disabled", http.StatusForbidden)
		return
	}
	webUser := userFrom(r).Username

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	_, msg, err := conn.ReadMessage()
	if err != nil {
		return
	}
	var a termAuth
	if err := json.Unmarshal(msg, &a); err != nil {
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n*** bad auth frame ***\r\n"))
		return
	}

	sess, err := s.terminal.Dial(a.User, a.Password, a.Cols, a.Rows)
	if err != nil {
		_ = s.store.AddAudit(webUser, "terminal_auth_failed", a.User+"@"+s.terminal.SSHAddr()+": "+err.Error(), clientIP(r))
		conn.WriteMessage(websocket.TextMessage, []byte("\r\n*** SSH connection failed: "+err.Error()+" ***\r\n"))
		return
	}
	defer sess.Close()
	target := a.User + "@" + s.terminal.SSHAddr()
	_ = s.store.AddAudit(webUser, "terminal_open", target, clientIP(r))
	defer func() { _ = s.store.AddAudit(webUser, "terminal_close", target, clientIP(r)) }()

	// shell output → browser (binary frames)
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := sess.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				conn.Close()
				return
			}
		}
	}()

	// browser → shell (input) + control frames (resize)
	for {
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		if mt == websocket.TextMessage {
			var c termCtrl
			if json.Unmarshal(data, &c) == nil && c.Type == "resize" {
				sess.Resize(c.Cols, c.Rows)
			}
			continue
		}
		if _, err := sess.Write(data); err != nil {
			return
		}
	}
}
