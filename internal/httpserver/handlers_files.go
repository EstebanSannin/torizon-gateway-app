package httpserver

import (
	"net/http"
	"net/url"
	"path"
	"strings"

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
		Path        string
		Parent      string
		Crumbs      []crumb
		Entries     []files.Entry
		CanWriteDir bool
		Err         string
	}{
		layout:      s.layoutFor(w, r, "Files", "files"),
		Path:        cleaned,
		Parent:      path.Dir(cleaned),
		Crumbs:      breadcrumbs(cleaned),
		Entries:     entries,
		CanWriteDir: s.files.CanWrite(cleaned),
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
		View     files.FileView
		Parent   string
		Crumbs   []crumb
		CanWrite bool
		Err      string
	}{
		layout:   s.layoutFor(w, r, "Files", "files"),
		View:     fv,
		Parent:   path.Dir(fv.Path),
		Crumbs:   breadcrumbs(fv.Path),
		CanWrite: s.files.CanWrite(p),
	}
	if err != nil {
		data.Err = err.Error()
		data.Parent = "/"
	}
	render(w, "fileview.html", data)
}

// handleFileEdit renders the in-browser text editor for a writable file.
func (s *Server) handleFileEdit(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query().Get("path")
	if !s.files.CanWrite(p) {
		http.Error(w, "not editable", http.StatusForbidden)
		return
	}
	fv, err := s.files.Read(p, 1<<20)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if fv.Binary {
		http.Error(w, "cannot edit a binary file", http.StatusBadRequest)
		return
	}
	render(w, "fileedit.html", struct {
		layout
		View      files.FileView
		Parent    string
		Crumbs    []crumb
		Truncated bool
	}{
		layout:    s.layoutFor(w, r, "Edit", "files"),
		View:      fv,
		Parent:    path.Dir(fv.Path),
		Crumbs:    breadcrumbs(fv.Path),
		Truncated: fv.Truncated,
	})
}

// handleFileSave writes edited content back to a file.
func (s *Server) handleFileSave(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	p := r.PostFormValue("path")
	// Normalize CRLF the browser may send to LF.
	content := strings.ReplaceAll(r.PostFormValue("content"), "\r\n", "\n")
	if err := s.files.Save(p, content); err != nil {
		_ = s.store.AddAudit(userFrom(r).Username, "file_save_failed", p+": "+err.Error(), clientIP(r))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = s.store.AddAudit(userFrom(r).Username, "file_save", p, clientIP(r))
	http.Redirect(w, r, "/files/view?path="+url.QueryEscape(p), http.StatusSeeOther)
}

// handleFileUpload saves an uploaded file into a writable directory.
func (s *Server) handleFileUpload(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(16 << 20); err != nil {
		http.Error(w, "invalid upload", http.StatusBadRequest)
		return
	}
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	dir := r.PostFormValue("path")
	file, hdr, err := r.FormFile("file")
	if err != nil {
		http.Error(w, "no file", http.StatusBadRequest)
		return
	}
	defer file.Close()
	dest, err := s.files.Upload(dir, hdr.Filename, file, 16<<20) // 16 MiB cap
	if err != nil {
		_ = s.store.AddAudit(userFrom(r).Username, "file_upload_failed", dir+"/"+hdr.Filename+": "+err.Error(), clientIP(r))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = s.store.AddAudit(userFrom(r).Username, "file_upload", dest, clientIP(r))
	http.Redirect(w, r, "/files?path="+url.QueryEscape(dir), http.StatusSeeOther)
}

// handleFileDelete removes a file (or empty dir) within the writable area.
func (s *Server) handleFileDelete(w http.ResponseWriter, r *http.Request) {
	if !checkCSRF(r) {
		http.Error(w, "invalid CSRF token", http.StatusForbidden)
		return
	}
	p := r.PostFormValue("path")
	if err := s.files.Delete(p); err != nil {
		_ = s.store.AddAudit(userFrom(r).Username, "file_delete_failed", p+": "+err.Error(), clientIP(r))
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	_ = s.store.AddAudit(userFrom(r).Username, "file_delete", p, clientIP(r))
	http.Redirect(w, r, "/files?path="+url.QueryEscape(path.Dir(p)), http.StatusSeeOther)
}
