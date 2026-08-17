package httpserver

import (
	"net/http"

	"github.com/toradex/torizon-gateway-app/internal/sysinfo"
)

// periphData is the model for the peripherals fragment.
type periphData struct {
	USB   []sysinfo.USBDevice
	Block []sysinfo.BlockDevice
	CAN   []sysinfo.CANInterface
}

// handlePeripheralsFragment renders the live USB / storage / CAN lists. The
// dashboard polls this (htmx) so attaching a USB stick shows up within seconds.
func (s *Server) handlePeripheralsFragment(w http.ResponseWriter, r *http.Request) {
	renderFragment(w, "fragment_peripherals.html", "peripherals", periphData{
		USB:   s.peripherals.USB(),
		Block: s.peripherals.Block(),
		CAN:   s.peripherals.CAN(),
	})
}
