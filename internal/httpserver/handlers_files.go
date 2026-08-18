package httpserver

import (
	"net/http"
	"path"

	"github.com/toradex/torizon-gateway-app/internal/files"
)

// crumb is a breadcrumb segment.
type crumb struct {
	Name string
	Path string
}

// breadcrumbs builds clickable path segments for a host-absolute path.
func breadcrumbs(p string) []crumb {
	out := []crumb{{Name: "/", Path: "/"}}
	acc := ""
	for _, seg := range splitNonEmpty(p, '/') {
		acc += "/" + seg
		out = append(out, crumb{Name: seg, Path: acc})
	}
	return out
}

func splitNonEmpty(s string, sep byte) []string {
	var out []string
	cur := ""
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			if cur != "" {
				out = append(out, cur)
			}
			cur = ""
			continue
		}
		cur += string(s[i])
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

// handleFiles renders a directory listing (read-only file explorer).
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if p == "" {
		p = "/"
	}
	cleaned, entries, err := s.files.List(p)
	data := struct {
		layout
		Path    string
		Parent  string
		Crumbs  []crumb
		Entries []files.Entry
		Err     string
	}{
		layout:  s.layoutFor(w, r, "Files", "files"),
		Path:    cleaned,
		Parent:  path.Dir(cleaned),
		Crumbs:  breadcrumbs(cleaned),
		Entries: entries,
	}
	if err != nil {
		data.Err = err.Error()
	}
	render(w, "files.html", data)
}

// handleFileView renders a single file's content (read-only, capped).
func (s *Server) handleFileView(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	fv, err := s.files.Read(p, 1<<20) // 1 MiB cap
	data := struct {
		layout
		View   files.FileView
		Parent string
		Crumbs []crumb
		Err    string
	}{
		layout: s.layoutFor(w, r, "Files", "files"),
		View:   fv,
		Parent: path.Dir(fv.Path),
		Crumbs: breadcrumbs(fv.Path),
	}
	if err != nil {
		data.Err = err.Error()
		data.Parent = "/"
	}
	render(w, "fileview.html", data)
}
