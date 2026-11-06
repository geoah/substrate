package api

import (
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
)

// hostServicesRoute answers which services the host pays for and the day's
// budget (decision 0230, docs/plans/host-defaults.md).
const hostServicesRoute = "/host/services"

// getHostServices serves GET /api/v1/host/services: the engine's answer, with
// the host's name from the site directory where the deployment has one. The
// route sits behind requireAuth, so a scoped app token is refused before it
// runs and a full-access one reads it as the owner does.
func (h *handler) getHostServices(w http.ResponseWriter, r *http.Request) {
	if bad := unsupportedParam(r); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad)
		return
	}
	out, err := DatasetFrom(r.Context()).HostServices(r.Context())
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	if out.Host != nil && h.site != nil {
		out.Host.Name = h.site.Info().Name
	}
	if out.Items == nil {
		out.Items = []substrate.HostService{}
	}
	writeJSON(w, http.StatusOK, out)
}
