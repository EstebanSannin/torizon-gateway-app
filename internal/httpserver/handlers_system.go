package httpserver

import (
	"net/http"

	"github.com/toradex/torizon-gateway-app/internal/sysinfo"
)

// periphData is the model for the peripherals fragment.
type periphData struct {
	USB    []sysinfo.USBDevice
	Block  []sysinfo.BlockDevice
	CAN    []sysinfo.CANInterface
	Serial []sysinfo.SerialPort
	I2C    []sysinfo.Bus
	SPI    []string
	GPIO   []sysinfo.Bus
}

// handlePeripheralsFragment renders the live USB / storage / CAN / bus lists.
// The dashboard polls this (htmx) so attaching a USB stick shows up within secs.
func (s *Server) handlePeripheralsFragment(w http.ResponseWriter, r *http.Request) {
	renderFragment(w, "fragment_peripherals.html", "peripherals", periphData{
		USB:    s.peripherals.USB(),
		Block:  s.peripherals.Block(),
		CAN:    s.peripherals.CAN(),
		Serial: s.peripherals.Serial(),
		I2C:    s.peripherals.I2C(),
		SPI:    s.peripherals.SPI(),
		GPIO:   s.peripherals.GPIO(),
	})
}
