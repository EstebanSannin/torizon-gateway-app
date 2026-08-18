// Package files provides a confined view of the host filesystem for the web
// file explorer. Everything is browsed relative to a root (the host filesystem,
// "/" natively or "/host" in a container). Paths shown to and received from the
// user are host-absolute (e.g. "/etc"); they are cleaned and re-rooted so a
// request can never escape the root via ".." — the foundation for the later
// write feature, which will further restrict writes to an allowlist.
package files

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Service browses a filesystem rooted at Root. Writes (when enabled) are
// confined to WriteRoots.
type Service struct {
	Root       string   // host filesystem root ("/" or "/host")
	Writable   bool     // master switch for edit/upload/delete
	WriteRoots []string // host-absolute prefixes writes are allowed under
}

// Errors surfaced to callers.
var (
	ErrWritesDisabled = errors.New("file writing is disabled")
	ErrNotWritable    = errors.New("path is outside the writable area (/etc, /var)")
	ErrRestricted     = errors.New("file is restricted")
)

// New builds a file service rooted at root. When writable, edit/upload/delete
// are permitted under /etc and /var only.
func New(root string, writable bool) *Service {
	if root == "" {
		root = "/"
	}
	return &Service{Root: root, Writable: writable, WriteRoots: []string{"/etc", "/var"}}
}

// CanWrite reports whether a path may be created/modified/deleted (writes
// enabled, inside a write root, and not a restricted secret).
func (s *Service) CanWrite(userPath string) bool {
	clean := cleanUser(userPath)
	return s.Writable && s.inWriteRoot(clean) && !isSensitive(clean)
}

func (s *Service) inWriteRoot(clean string) bool {
	for _, root := range s.WriteRoots {
		if clean == root || strings.HasPrefix(clean, root+"/") {
			return true
		}
	}
	return false
}

// Save overwrites an existing text file's content (mode/owner preserved).
func (s *Service) Save(userPath, content string) error {
	clean := cleanUser(userPath)
	if err := s.checkWrite(clean); err != nil {
		return err
	}
	real := filepath.Join(s.Root, clean)
	fi, err := os.Stat(real)
	if err != nil {
		return err
	}
	if fi.IsDir() {
		return errors.New("is a directory")
	}
	return os.WriteFile(real, []byte(content), fi.Mode().Perm())
}

// Upload writes an uploaded file into a directory (basename only; capped size).
func (s *Service) Upload(dirPath, filename string, src io.Reader, maxBytes int64) (string, error) {
	cleanDir := cleanUser(dirPath)
	name := filepath.Base(filepath.Clean("/" + filename))
	if name == "." || name == "/" || name == "" {
		return "", errors.New("invalid filename")
	}
	dest := cleanUser(cleanDir + "/" + name)
	if err := s.checkWrite(dest); err != nil {
		return "", err
	}
	if di, err := os.Stat(filepath.Join(s.Root, cleanDir)); err != nil || !di.IsDir() {
		return "", errors.New("target is not a directory")
	}
	real := filepath.Join(s.Root, dest)
	f, err := os.OpenFile(real, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	n, err := io.Copy(f, io.LimitReader(src, maxBytes+1))
	if err != nil {
		return "", err
	}
	if n > maxBytes {
		os.Remove(real)
		return "", errors.New("file exceeds the size limit")
	}
	return dest, nil
}

// Delete removes a file or empty directory within the writable area.
func (s *Service) Delete(userPath string) error {
	clean := cleanUser(userPath)
	if err := s.checkWrite(clean); err != nil {
		return err
	}
	return os.Remove(filepath.Join(s.Root, clean))
}

// checkWrite enforces the write policy for a cleaned path.
func (s *Service) checkWrite(clean string) error {
	if !s.Writable {
		return ErrWritesDisabled
	}
	if !s.inWriteRoot(clean) {
		return ErrNotWritable
	}
	if isSensitive(clean) {
		return ErrRestricted
	}
	return nil
}

// isSensitive matches files we refuse to read or write even as root (secrets).
func isSensitive(clean string) bool {
	base := filepath.Base(clean)
	switch base {
	case "shadow", "gshadow", "shadow-", "gshadow-", "id_rsa", "id_dsa", "id_ecdsa", "id_ed25519":
		return true
	}
	if strings.HasSuffix(base, ".key") {
		return true
	}
	if strings.HasPrefix(base, "ssh_host_") && strings.HasSuffix(base, "_key") {
		return true
	}
	return false
}

// Entry is a directory entry.
type Entry struct {
	Name    string
	IsDir   bool
	IsLink  bool
	Size    int64
	Mode    string
	ModTime time.Time
}

// Resolve maps a host-absolute user path to the real filesystem path under Root,
// guaranteeing the result stays within Root (defense against "..").
func (s *Service) Resolve(userPath string) string {
	clean := filepath.Clean("/" + strings.TrimPrefix(userPath, "/")) // absolute, no ".."
	return filepath.Join(s.Root, clean)
}

// clean returns the normalized host-absolute path for display.
func cleanUser(userPath string) string {
	return filepath.Clean("/" + strings.TrimPrefix(userPath, "/"))
}

// List returns the entries of a directory (host-absolute path). Returns the
// cleaned path so callers can render an accurate breadcrumb.
func (s *Service) List(userPath string) (path string, entries []Entry, err error) {
	path = cleanUser(userPath)
	real := filepath.Join(s.Root, path)

	fi, err := os.Stat(real)
	if err != nil {
		return path, nil, err
	}
	if !fi.IsDir() {
		return path, nil, errors.New("not a directory")
	}

	dirents, err := os.ReadDir(real)
	if err != nil {
		return path, nil, err
	}
	const maxEntries = 3000
	for i, de := range dirents {
		if i >= maxEntries {
			break
		}
		info, ierr := de.Info()
		e := Entry{Name: de.Name(), IsDir: de.IsDir(), IsLink: de.Type()&fs.ModeSymlink != 0}
		if ierr == nil {
			e.Size = info.Size()
			e.Mode = info.Mode().String()
			e.ModTime = info.ModTime()
			// For a symlink, report whether the target is a directory (nicer nav).
			if e.IsLink {
				if ti, terr := os.Stat(real + "/" + de.Name()); terr == nil {
					e.IsDir = ti.IsDir()
				}
			}
		}
		entries = append(entries, e)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir // directories first
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return path, entries, nil
}

// FileView is the content of a file for display.
type FileView struct {
	Path      string
	Size      int64
	Mode      string
	ModTime   time.Time
	Content   string
	Truncated bool
	Binary    bool
}

// Read returns a file's content for viewing, capped at maxBytes. Binary files
// are reported (Binary=true) without content.
func (s *Service) Read(userPath string, maxBytes int64) (FileView, error) {
	path := cleanUser(userPath)
	if isSensitive(path) {
		return FileView{}, ErrRestricted
	}
	real := filepath.Join(s.Root, path)

	fi, err := os.Stat(real)
	if err != nil {
		return FileView{}, err
	}
	if fi.IsDir() {
		return FileView{}, errors.New("is a directory")
	}
	fv := FileView{Path: path, Size: fi.Size(), Mode: fi.Mode().String(), ModTime: fi.ModTime()}

	f, err := os.Open(real)
	if err != nil {
		return FileView{}, err
	}
	defer f.Close()

	buf := make([]byte, maxBytes+1)
	n, _ := f.Read(buf)
	data := buf[:max0(n)]
	if int64(n) > maxBytes {
		data = data[:maxBytes]
		fv.Truncated = true
	}
	if isBinary(data) {
		fv.Binary = true
		return fv, nil
	}
	fv.Content = string(data)
	return fv, nil
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}

// isBinary reports whether data looks non-textual (NUL byte or invalid UTF-8).
func isBinary(data []byte) bool {
	if len(data) == 0 {
		return false
	}
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return !utf8.Valid(data)
}
