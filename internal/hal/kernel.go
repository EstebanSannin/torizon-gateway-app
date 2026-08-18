package hal

import (
	"os"
	"runtime"
	"strings"
	"time"
)

// KernelInfo is parsed from /proc/version — the kernel release plus build
// metadata (toolchain, linker, build flavour and date). Raw is the verbatim
// banner and the fallback when a non-standard kernel doesn't parse.
type KernelInfo struct {
	Release   string // 6.6.142-7.7.0
	Arch      string // aarch64
	Compiler  string // GCC 13.4.0
	Triple    string // aarch64-tdx-linux
	Linker    string // GNU ld 2.42.0
	Build     string // #1-Torizon
	SMP       bool
	Preempt   string // PREEMPT / PREEMPT_RT / PREEMPT_DYNAMIC / ""
	BuildDate string // 19 Jun 2026 · 08:16 UTC
	Builder   string // oe-user@oe-host
	Raw       string
}

// Kernel reads and parses /proc/version. The kernel is shared with the host, so
// the container's /proc/version already reports the host kernel.
func Kernel() KernelInfo {
	b, _ := os.ReadFile("/proc/version")
	raw := strings.TrimSpace(string(b))
	k := KernelInfo{Raw: raw, Arch: runtime.GOARCH}
	if raw == "" {
		return k
	}
	s := strings.TrimPrefix(raw, "Linux version ")
	k.Release, s, _ = strings.Cut(s, " ")

	k.Builder, s = firstParen(s) // (oe-user@oe-host)
	var comp string
	comp, s = firstParen(s) // (compiler … , linker …) — may nest parens
	k.parseCompiler(comp)
	k.parseTail(strings.TrimSpace(s)) // #1-Torizon SMP PREEMPT <date>
	return k
}

func (k *KernelInfo) parseCompiler(comp string) {
	cc, ld, _ := strings.Cut(comp, ", ")
	// compiler: "aarch64-tdx-linux-gcc (GCC) 13.4.0"
	if i := strings.Index(cc, " ("); i > 0 {
		prefix := cc[:i] // aarch64-tdx-linux-gcc
		k.Triple = strings.TrimSuffix(strings.TrimSuffix(prefix, "-gcc"), "-g++")
		if d := strings.IndexByte(prefix, '-'); d > 0 {
			k.Arch = prefix[:d] // aarch64
		}
		name, ver := firstParen(cc[i:]) // "GCC", " 13.4.0"
		k.Compiler = strings.TrimSpace(name + " " + strings.TrimSpace(ver))
	} else if cc != "" {
		k.Compiler = cc
	}
	// linker: "GNU ld (GNU Binutils) 2.42.0.20240723"
	if ld != "" {
		if i := strings.Index(ld, " ("); i > 0 {
			_, ver := firstParen(ld[i:])
			k.Linker = strings.TrimSpace(ld[:i] + " " + shortVer(strings.TrimSpace(ver)))
		} else {
			k.Linker = ld
		}
	}
}

func (k *KernelInfo) parseTail(t string) {
	fields := strings.Fields(t)
	weekday := map[string]bool{"Mon": true, "Tue": true, "Wed": true, "Thu": true, "Fri": true, "Sat": true, "Sun": true}
	for i, f := range fields {
		switch {
		case i == 0 && strings.HasPrefix(f, "#"):
			k.Build = f
		case f == "SMP":
			k.SMP = true
		case strings.HasPrefix(f, "PREEMPT"):
			k.Preempt = f
		case weekday[f]:
			k.BuildDate = formatKernelDate(strings.Join(fields[i:], " "))
			return
		}
	}
}

// firstParen returns the contents of the first balanced (…) group and the
// remainder after it (handling nested parentheses).
func firstParen(s string) (inner, rest string) {
	i := strings.IndexByte(s, '(')
	if i < 0 {
		return "", s
	}
	depth := 0
	for j := i; j < len(s); j++ {
		switch s[j] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return s[i+1 : j], s[j+1:]
			}
		}
	}
	return s[i+1:], ""
}

// shortVer trims a dotted version to its first three components
// ("2.42.0.20240723" → "2.42.0").
func shortVer(v string) string {
	parts := strings.Split(v, ".")
	if len(parts) > 3 {
		return strings.Join(parts[:3], ".")
	}
	return v
}

func formatKernelDate(s string) string {
	s = strings.Join(strings.Fields(s), " ") // collapse padding spaces
	if t, err := time.Parse("Mon Jan 2 15:04:05 MST 2006", s); err == nil {
		return t.Format("02 Jan 2006 · 15:04 MST")
	}
	return s
}
