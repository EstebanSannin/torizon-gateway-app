module github.com/toradex/torizon-gateway-app

go 1.23

// NOTE: The Phase-0 scaffold is intentionally stdlib-only so it builds with zero
// external modules. Planned dependencies (add when their feature lands):
//   github.com/go-chi/chi/v5              // router
//   github.com/godbus/dbus/v5            // NetworkManager over system D-Bus
//   github.com/docker/docker/client      // container management
//   modernc.org/sqlite                   // pure-Go persistence (no cgo)
//   golang.org/x/crypto/argon2           // password hashing
