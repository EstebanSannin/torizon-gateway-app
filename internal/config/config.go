// Package config loads runtime configuration from the environment with safe
// defaults. The host (NetworkManager/Docker) stays authoritative for device
// state; this only configures the app itself.
package config

import (
	"os"
	"path/filepath"
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
	// Hostname advertised via mDNS, e.g. "zinnia".
	Hostname string
	// DockerSocket is the path to the Docker engine socket (container management).
	DockerSocket string
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
		Hostname:     env("GATEWAY_HOSTNAME", "zinnia"),
		DockerSocket: env("GATEWAY_DOCKER_SOCKET", "/var/run/docker.sock"),
		DevMode:      env("GATEWAY_DEV_MODE", "") == "1",
	}
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return def
}
