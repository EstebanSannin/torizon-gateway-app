package hal

import "os"

// When the app runs inside a container, some host files are shadowed by the
// container's own (notably /etc/os-release) and must be read from the host
// filesystem mounted at /host/* by docker-compose. These resolvers prefer the
// host-mounted copy and fall back to the native path when running directly on
// the host. Keeping this in one place avoids scattering /host knowledge around.

// firstExisting returns the first path that exists, or the last candidate as a
// best-effort default (so callers still get a sensible path to attempt).
func firstExisting(paths ...string) string {
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if len(paths) > 0 {
		return paths[len(paths)-1]
	}
	return ""
}

// osReleasePath resolves the host's os-release (Torizon OS), preferring the
// container mount.
func osReleasePath() string {
	return firstExisting("/host/os-release", "/etc/os-release")
}

// deviceTreeModelPath resolves the device-tree "model" node across the paths it
// can appear at, in and out of a container.
func deviceTreeModelPath() string {
	return firstExisting(
		"/proc/device-tree/model",             // native (procfs is present in containers too)
		"/sys/firmware/devicetree/base/model", // canonical sysfs location
		"/host/device-tree/model",             // explicit compose mount
	)
}
