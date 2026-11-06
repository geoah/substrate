package api

import (
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
)

// operatorDefaultsRoute answers which slots the operator fills for this
// repository and today's allowance (decision 0230, docs/plans/host-defaults.md).
const operatorDefaultsRoute = "/operator/defaults"

// getOperatorDefaults serves GET /api/v1/operator/defaults: the engine's
// answer as it stands, the site's name included, because the engine words its
// refusals with that name too. The route sits behind requireAuth, so a scoped
// app token is refused before it runs and a full-access one reads it as the
// owner does.
func (h *handler) getOperatorDefaults(w http.ResponseWriter, r *http.Request) {
	if bad := unsupportedParam(r); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad)
		return
	}
	out, err := DatasetFrom(r.Context()).OperatorDefaults(r.Context())
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	if out.Items == nil {
		out.Items = []substrate.OperatorDefault{}
	}
	writeJSON(w, http.StatusOK, out)
}
