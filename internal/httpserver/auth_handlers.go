package httpserver

import (
	"net/http"
	"strings"
)

// handleSetupPage renders the first-boot admin creation form (only when no
// account exists; otherwise redirects to login).
func (s *Server) handleSetupPage(w http.ResponseWriter, r *http.Request) {
	need, err := s.auth.NeedsSetup()
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !need {
		http.Redirect(w, r, "/login", http.StatusSeeOther)
		return
	}
	renderAuth(w, "setup.html", map[string]any{
		"Title": "Set up", "CSRF": s.ensureCSRF(w, r),
	})
}

// handleSetupPost creates the first administrator, then logs them in.
func (s *Server) handleSetupPost(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	confirm := r.PostFormValue("confirm")

	fail := func(msg string) {
		renderAuth(w, "setup.html", map[string]any{
			"Title": "Set up", "CSRF": s.ensureCSRF(w, r), "Error": msg, "Username": username,
		})
	}
	if password != confirm {
		fail("Passwords do not match.")
		return
	}
	if err := s.auth.CreateAdmin(username, password); err != nil {
		fail(err.Error())
		return
	}
	_ = s.store.AddAudit(username, "setup", "admin account created", clientIP(r))
	s.startSession(w, r, username, password)
}

// handleLoginPage renders the login form.
func (s *Server) handleLoginPage(w http.ResponseWriter, r *http.Request) {
	if need, _ := s.auth.NeedsSetup(); need {
		http.Redirect(w, r, "/setup", http.StatusSeeOther)
		return
	}
	renderAuth(w, "login.html", map[string]any{
		"Title": "Sign in", "CSRF": s.ensureCSRF(w, r),
	})
}

// handleLoginPost verifies credentials and starts a session.
func (s *Server) handleLoginPost(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	username := strings.TrimSpace(r.PostFormValue("username"))
	password := r.PostFormValue("password")
	s.startSession(w, r, username, password)
}

// startSession logs in and sets the session cookie, or re-renders login with an
// error. Shared by setup (auto-login) and login.
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, username, password string) {
	sid, user, err := s.auth.Login(username, password)
	if err != nil {
		_ = s.store.AddAudit(username, "login_failed", err.Error(), clientIP(r))
		renderAuth(w, "login.html", map[string]any{
			"Title": "Sign in", "CSRF": s.ensureCSRF(w, r),
			"Error": "Invalid username or password.", "Username": username,
		})
		return
	}
	setCookie(w, sessionCookie, sid, s.auth.SessionTTL())
	_ = s.store.AddAudit(user.Username, "login", "session started", clientIP(r))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// handleLogout ends the session.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	sid := cookieValue(r, sessionCookie)
	if sid != "" {
		_ = s.auth.Logout(sid)
		_ = s.store.AddAudit(userFrom(r).Username, "logout", "session ended", clientIP(r))
	}
	clearCookie(w, sessionCookie)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}
