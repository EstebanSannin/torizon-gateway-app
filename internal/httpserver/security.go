package httpserver

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"html/template"
	"net/http"
	"time"

	"github.com/toradex/torizon-gateway-app/internal/store"
	"github.com/toradex/torizon-gateway-app/web"
)

const (
	sessionCookie = "gw_session"
	csrfCookie    = "gw_csrf"
	csrfField     = "csrf_token"
)

type ctxKey int

const userCtxKey ctxKey = 0

// userFrom returns the authenticated user stored by requireAuth (zero value if
// none — protected handlers always have one).
func userFrom(r *http.Request) store.User {
	u, _ := r.Context().Value(userCtxKey).(store.User)
	return u
}

// requireAuth gates a handler: forces first-boot setup, else requires a valid
// session, else redirects to login.
func (s *Server) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if need, err := s.auth.NeedsSetup(); err == nil && need {
			http.Redirect(w, r, "/setup", http.StatusSeeOther)
			return
		}
		user, ok := s.auth.Validate(cookieValue(r, sessionCookie))
		if !ok {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		ctx := context.WithValue(r.Context(), userCtxKey, user)
		next(w, r.WithContext(ctx))
	}
}

// ---- cookies ----

func cookieValue(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

func setCookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   true,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Now().Add(ttl),
		MaxAge:   int(ttl.Seconds()),
	})
}

func clearCookie(w http.ResponseWriter, name string) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: "", Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// ---- CSRF (double-submit cookie) ----

// ensureCSRF returns the request's CSRF token, minting and setting one if
// absent. Rendered into forms and compared against the cookie on POST.
func (s *Server) ensureCSRF(w http.ResponseWriter, r *http.Request) string {
	if tok := cookieValue(r, csrfCookie); tok != "" {
		return tok
	}
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	tok := hex.EncodeToString(b)
	setCookie(w, csrfCookie, tok, 12*time.Hour)
	return tok
}

// checkCSRF validates the posted token against the cookie (constant time).
func checkCSRF(r *http.Request) bool {
	cookie := cookieValue(r, csrfCookie)
	form := r.PostFormValue(csrfField)
	if cookie == "" || form == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(cookie), []byte(form)) == 1
}

// ---- auth-layout rendering (no sidebar) ----

func renderAuth(w http.ResponseWriter, page string, data any) {
	tmpl, err := template.ParseFS(web.Templates, "templates/auth_base.html", "templates/"+page)
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "authbase", data); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

// clientIP is a best-effort remote address for audit logging.
func clientIP(r *http.Request) string {
	return r.RemoteAddr
}
