// Package logs reads the systemd journal (which also captures the kernel ring
// buffer) via journalctl. Natively that's just "journalctl". Inside our
// distroless container journalctl isn't present, so we run the HOST journalctl
// through the host's dynamic loader against the host filesystem mounted at the
// host root — derived automatically, with a GATEWAY_JOURNALCTL override.
//
// This container path is a dev convenience; the production (native) deployment
// simply calls journalctl directly. Reading the journal needs the process to be
// in the systemd-journal group (see deploy/docker-compose.yml group_add).
package logs

import (
	"bufio"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Service builds and runs journalctl invocations.
type Service struct {
	base []string // argv prefix that runs journalctl (+ -D dir in a container)
}

// New builds a Service. hostRoot is "/" natively or e.g. "/host" in a container.
func New(hostRoot string) *Service {
	return &Service{base: journalctlBase(hostRoot)}
}

// Options selects what to stream.
type Options struct {
	Unit   string // systemd unit filter (validated by the caller against Units)
	Kernel bool   // kernel messages only (-k)
	Tail   int    // initial lines before following
}

// Available reports whether journalctl can be run and read.
func (s *Service) Available(ctx context.Context) bool {
	argv := append(append([]string{}, s.base...), "-n", "1", "--no-pager")
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Run() == nil
}

// Units returns the distinct systemd units present in the journal (for the
// filter dropdown), sorted.
func (s *Service) Units(ctx context.Context) []string {
	argv := append(append([]string{}, s.base...), "-F", "_SYSTEMD_UNIT", "--no-pager")
	out, err := exec.CommandContext(ctx, argv[0], argv[1:]...).Output()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var units []string
	for _, l := range strings.Split(string(out), "\n") {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		units = append(units, l)
	}
	sort.Strings(units)
	return units
}

// Stream starts `journalctl -f` with the given filters and returns its output.
// Closing the reader (or cancelling ctx) stops journalctl.
func (s *Service) Stream(ctx context.Context, opts Options) (io.ReadCloser, error) {
	tail := opts.Tail
	if tail <= 0 || tail > 1000 {
		tail = 200
	}
	argv := append([]string{}, s.base...)
	argv = append(argv, "-o", "short-iso", "--no-pager", "-n", strconv.Itoa(tail), "-f")
	if opts.Kernel {
		argv = append(argv, "-k")
	}
	if opts.Unit != "" {
		argv = append(argv, "-u", opts.Unit)
	}
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), "SYSTEMD_COLORS=0")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = cmd.Stdout // surface journalctl errors in the stream
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return &procReader{cmd: cmd, out: stdout}, nil
}

// ScanLines invokes emit for each complete line until the reader ends.
func ScanLines(r io.Reader, emit func(string)) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		emit(sc.Text())
	}
}

// procReader ties a stream to its journalctl process; Close stops it.
type procReader struct {
	cmd *exec.Cmd
	out io.ReadCloser
}

func (p *procReader) Read(b []byte) (int, error) { return p.out.Read(b) }
func (p *procReader) Close() error {
	p.out.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	return p.cmd.Wait()
}

// journalctlBase derives the argv prefix that runs journalctl.
func journalctlBase(hostRoot string) []string {
	if override := os.Getenv("GATEWAY_JOURNALCTL"); override != "" {
		return strings.Fields(override)
	}
	if hostRoot == "" || hostRoot == "/" {
		return []string{"journalctl"} // native
	}
	// Container: run the host journalctl via the host dynamic loader.
	ld := firstGlob(
		filepath.Join(hostRoot, "lib/ld-*.so*"),
		filepath.Join(hostRoot, "lib64/ld-*.so*"),
		filepath.Join(hostRoot, "usr/lib/ld-*.so*"),
	)
	libPath := strings.Join(append([]string{
		filepath.Join(hostRoot, "lib"),
		filepath.Join(hostRoot, "lib64"),
		filepath.Join(hostRoot, "usr/lib"),
		filepath.Join(hostRoot, "usr/lib/systemd"),
	}, globDirs(filepath.Join(hostRoot, "usr/lib/*-linux-gnu"))...), ":")
	journalDir := firstExistingDir(
		filepath.Join(hostRoot, "var/log/journal"),
		filepath.Join(hostRoot, "run/log/journal"),
	)
	if ld == "" || journalDir == "" {
		return []string{"journalctl"} // best effort; likely fails, surfaced as unavailable
	}
	return []string{ld, "--library-path", libPath, filepath.Join(hostRoot, "usr/bin/journalctl"), "-D", journalDir}
}

func firstGlob(patterns ...string) string {
	for _, p := range patterns {
		if m, _ := filepath.Glob(p); len(m) > 0 {
			return m[0]
		}
	}
	return ""
}

func globDirs(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

func firstExistingDir(paths ...string) string {
	for _, p := range paths {
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			return p
		}
	}
	return ""
}
