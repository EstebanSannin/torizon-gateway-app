// Package sysinfo provides OS-level (not board-specific) read-only system
// facts that aren't part of the hardware identity in package hal — e.g. disk
// usage. Network interfaces will join here in Phase 2 via NetworkManager (a
// bridged container can't see the host's interfaces directly).
package sysinfo

import "syscall"

// Disk describes filesystem usage for a path.
type Disk struct {
	Path       string
	TotalBytes uint64
	FreeBytes  uint64
	UsedBytes  uint64
	UsedPct    float64
}

// DiskUsage returns usage for the filesystem backing path. When the app runs in
// a container, pass a path that is bind-mounted from the host (e.g. the data
// dir) so the numbers reflect a real host partition rather than the container
// overlay.
func DiskUsage(path string) (Disk, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return Disk{Path: path}, err
	}
	bs := uint64(st.Bsize)
	total := st.Blocks * bs
	free := st.Bavail * bs // available to unprivileged users
	used := total - free
	var pct float64
	if total > 0 {
		pct = float64(used) / float64(total) * 100
	}
	return Disk{
		Path:       path,
		TotalBytes: total,
		FreeBytes:  free,
		UsedBytes:  used,
		UsedPct:    pct,
	}, nil
}
