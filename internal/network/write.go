package network

import (
	"fmt"

	"github.com/godbus/dbus/v5"
)

// WriteProbe checks whether this process is authorized (polkit) to perform
// NetworkManager write operations. It creates a checkpoint (a no-op snapshot
// that changes nothing) and immediately destroys it. Returns nil if writes are
// allowed, or the D-Bus/polkit error otherwise.
//
// This is the decisive test for whether network *editing* can work from within
// the (unprivileged) container, or whether it requires the native deployment.
func (s *Service) WriteProbe() error {
	conn, err := s.connect()
	if err != nil {
		return err
	}
	defer conn.Close()

	var cp dbus.ObjectPath
	err = conn.Object(nmDest, nmPath).
		Call(nmIface+".CheckpointCreate", 0, []dbus.ObjectPath{}, uint32(0), uint32(0)).
		Store(&cp)
	if err != nil {
		return fmt.Errorf("checkpoint create: %w", err)
	}
	// Clean up immediately — the snapshot made no changes.
	_ = conn.Object(nmDest, nmPath).Call(nmIface+".CheckpointDestroy", 0, cp).Err
	return nil
}
