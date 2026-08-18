package httpserver

import (
	"net/http"

	"github.com/toradex/torizon-gateway-app/internal/cloud"
	"github.com/toradex/torizon-gateway-app/internal/containers"
)

// handleCloudPage renders the Torizon Cloud page shell (which polls the fragment).
func (s *Server) handleCloudPage(w http.ResponseWriter, r *http.Request) {
	render(w, "cloud.html", s.layoutFor(w, r, "Torizon Cloud", "cloud"))
}

// cloudData is the model for the cloud fragment.
type cloudData struct {
	Info       cloud.Info
	Containers []containers.Container // for the docker-compose subsystem expansion
}

// handleCloudFragment renders the live cloud/OTA status (htmx-polled).
func (s *Server) handleCloudFragment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	data := cloudData{Info: s.cloud.Get(ctx)}
	// Containers back the docker-compose subsystem's expandable list.
	if s.containers != nil && s.containers.Available(ctx) {
		data.Containers, _ = s.containers.List(ctx)
	}
	renderFragment(w, "fragment_cloud.html", "cloud", data)
}
