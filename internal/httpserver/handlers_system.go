package httpserver

import (
	"net/http"

	"github.com/toradex/torizon-gateway-app/internal/sysinfo"
)

// blockView adds a humanized size to a block device for the template.
type blockView struct {
	sysinfo.BlockDevice
	SizeHuman string
}

// periphData is the model for the peripherals fragment.
type periphData struct {
	USB   []sysinfo.USBDevice
	Block []blockView
	CAN   []sysinfo.CANInterface
}

// handlePeripheralsFragment renders the live USB / storage / CAN lists. The
// dashboard polls this (htmx) so attaching a USB stick shows up within seconds.
func (s *Server) handlePeripheralsFragment(w http.ResponseWriter, r *http.Request) {
	blocks := s.peripherals.Block()
	views := make([]blockView, 0, len(blocks))
	for _, b := range blocks {
		views = append(views, blockView{BlockDevice: b, SizeHuman: humanBytes(b.SizeBytes)})
	}
	renderFragment(w, "fragment_peripherals.html", "peripherals", periphData{
		USB:   s.peripherals.USB(),
		Block: views,
		CAN:   s.peripherals.CAN(),
	})
}
