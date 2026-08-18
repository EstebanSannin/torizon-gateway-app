package httpserver

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"github.com/toradex/torizon-gateway-app/web"
)

// tmplFuncs are helpers available in every template.
var tmplFuncs = template.FuncMap{
	// hbytes formats a byte count regardless of the concrete integer type the
	// template passes (uint64 from metrics/disk, int64 from FileInfo.Size).
	"hbytes": func(v any) string {
		switch n := v.(type) {
		case uint64:
			return humanBytes(n)
		case int64:
			return humanBytes(uint64(max0i64(n)))
		case int:
			return humanBytes(uint64(max0i64(int64(n))))
		default:
			return ""
		}
	},
	// wifibars renders a 4-bar signal glyph filled by strength (0–100%).
	"wifibars": func(strength int) template.HTML {
		n := 1
		switch {
		case strength >= 66:
			n = 4
		case strength >= 45:
			n = 3
		case strength >= 25:
			n = 2
		}
		hs := [4]int{6, 10, 14, 18}
		var b strings.Builder
		b.WriteString(`<svg class="bars" width="22" height="20" viewBox="0 0 22 20">`)
		for i := 0; i < 4; i++ {
			cls := "off"
			if i < n {
				cls = "on"
			}
			fmt.Fprintf(&b, `<rect class="%s" x="%d" y="%d" width="4" height="%d" rx="1"/>`, cls, i*6, 20-hs[i], hs[i])
		}
		b.WriteString(`</svg>`)
		return template.HTML(b.String())
	},
}

func max0i64(n int64) int64 {
	if n < 0 {
		return 0
	}
	return n
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

func freqHuman(khz int) string {
	if khz <= 0 {
		return "—"
	}
	mhz := float64(khz) / 1000
	if mhz >= 1000 {
		return fmt.Sprintf("%.2f GHz", mhz/1000)
	}
	return fmt.Sprintf("%.0f MHz", mhz)
}
