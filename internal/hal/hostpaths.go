package hal

import (
	"os"
	"path/filepath"
)

// When the app runs inside a container, some host files are shadowed by the
// container's own (notably /etc/os-release). The host filesystem is made
// visible at a root — "/" natively, or a mount like "/host" in a container
// (GATEWAY_HOST_ROOT). These resolvers try the host-root copy first and fall
// back to native/legacy paths. Keeping /host knowledge in one place avoids
// scattering it around.

var hostRoot = "/"

// SetHostRoot configures where the host filesystem "/" is visible. Call once at
// startup (from main) before Detect.
func SetHostRoot(root string) {
	if root != "" {
		hostRoot = root
	}
}

func hostPath(p string) string { return filepath.Join(hostRoot, p) }

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

// osReleasePath resolves the host's os-release (Torizon OS).
func osReleasePath() string {
	return firstExisting(
		hostPath("etc/os-release"), // host filesystem root (container mount or native)
		"/host/os-release",         // legacy explicit compose mount
		"/etc/os-release",          // native fallback
	)
}

// deviceTreeModelPath resolves the device-tree "model" node across the paths it
// can appear at, in and out of a container.
func deviceTreeModelPath() string {
	return firstExisting(
		hostPath("sys/firmware/devicetree/base/model"),
		hostPath("proc/device-tree/model"),
		"/proc/device-tree/model",
		"/sys/firmware/devicetree/base/model",
		"/host/device-tree/model", // legacy explicit compose mount
	)
}

// deviceTreeSerialPath resolves the device-tree "serial-number" node (Toradex
// modules populate this with the module serial).
func deviceTreeSerialPath() string {
	return firstExisting(
		hostPath("sys/firmware/devicetree/base/serial-number"),
		hostPath("proc/device-tree/serial-number"),
		"/proc/device-tree/serial-number",
		"/sys/firmware/devicetree/base/serial-number",
		"/host/device-tree/serial-number",
	)
}
