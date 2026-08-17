// Package config loads runtime configuration from the environment with safe
// defaults. The host (NetworkManager/Docker) stays authoritative for device
// state; this only configures the app itself.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Config holds all app configuration. Populate with Load().
type Config struct {
	// ListenAddr is the HTTPS bind address, e.g. ":8443".
	ListenAddr string
	// DataDir holds app-owned state (SQLite DB, TLS cert). Persistent volume.
	DataDir string
	// TLSCertFile / TLSKeyFile are generated on first boot if absent.
	TLSCertFile string
	TLSKeyFile  string
	// TLSSANs are extra Subject Alternative Names (IPs/hostnames) for the
	// self-signed cert — e.g. the device's LAN IP, which a bridged container
	// cannot auto-discover. Comma-separated in GATEWAY_TLS_SANS.
	TLSSANs []string
	// Hostname advertised via mDNS, e.g. "zinnia".
	Hostname string
	// DockerSocket is the path to the Docker engine socket (container management).
	DockerSocket string
	// DBusSocket is the path to the system D-Bus socket (NetworkManager).
	DBusSocket string
	// SysfsPath is the sysfs mount to read hardware from (USB/block/CAN). "/sys"
	// natively and in a container (host-global for USB/block; CAN needs host net).
	SysfsPath string
	// SessionTTL is how long a login session stays valid.
	SessionTTL time.Duration
	// DevMode relaxes some behavior for local development (never in production).
	DevMode bool
}

// Load reads configuration from environment variables, applying defaults.
func Load() Config {
	dataDir := env("GATEWAY_DATA_DIR", "/data")
	return Config{
		ListenAddr:   env("GATEWAY_LISTEN_ADDR", ":8443"),
		DataDir:      dataDir,
		TLSCertFile:  env("GATEWAY_TLS_CERT", filepath.Join(dataDir, "tls", "cert.pem")),
		TLSKeyFile:   env("GATEWAY_TLS_KEY", filepath.Join(dataDir, "tls", "key.pem")),
		TLSSANs:      splitCSV(env("GATEWAY_TLS_SANS", "")),
		Hostname:     env("GATEWAY_HOSTNAME", "zinnia"),
		DockerSocket: env("GATEWAY_DOCKER_SOCKET", "/var/run/docker.sock"),
		DBusSocket:   env("GATEWAY_DBUS_SOCKET", "/run/dbus/system_bus_socket"),
		SysfsPath:    env("GATEWAY_SYSFS", "/sys"),
		SessionTTL:   envDuration("GATEWAY_SESSION_TTL", 24*time.Hour),
		DevMode:      env("GATEWAY_DEV_MODE", "") == "1",
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}

// splitCSV splits a comma-separated list, trimming spaces and dropping empties.
func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
