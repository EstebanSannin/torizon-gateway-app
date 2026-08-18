package httpserver

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"

	"github.com/toradex/torizon-gateway-app/web"
)

// tmplFuncs are helpers available in every template.
var tmplFuncs = template.FuncMap{
	"hbytes": humanBytes,
	"urlq":   url.QueryEscape,
}

// render parses base.html + the given page file (both from the embedded FS) and
// executes the "base" template. Templates are re-parsed per request for now;
// switch to a parse-once cache before GA.
func render(w http.ResponseWriter, pageFile string, data any) {
	tmpl, err := template.New("t").Funcs(tmplFuncs).ParseFS(web.Templates, "templates/base.html", "templates/"+pageFile)
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

// renderInline parses base.html plus an inline `{{define "content"}}...{{end}}`
// string. Used for simple placeholder pages.
func renderInline(w http.ResponseWriter, contentTmpl string, data any) {
	tmpl, err := template.New("t").Funcs(tmplFuncs).ParseFS(web.Templates, "templates/base.html")
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if _, err := tmpl.Parse(contentTmpl); err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, "base", data); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

// renderFragment renders a single named template (an HTML fragment, no base
// layout) — used by htmx-polled endpoints.
func renderFragment(w http.ResponseWriter, file, define string, data any) {
	tmpl, err := template.New("t").Funcs(tmplFuncs).ParseFS(web.Templates, "templates/"+file)
	if err != nil {
		http.Error(w, "template error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if err := tmpl.ExecuteTemplate(w, define, data); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
	}
}

// ---- humanize helpers ----

func humanBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

func humanDuration(sec int64) string {
	d := sec / 86400
	h := (sec % 86400) / 3600
	m := (sec % 3600) / 60
	switch {
	case d > 0:
		return fmt.Sprintf("%dd %dh", d, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

func tempStr(c float64) string {
	if c <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f°C", c)
}
