package api

import (
	"fmt"
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
)

// The units' two routes (decision 0186): GET /units answers the repository's
// unit graph, and GET /units/convert converts one value through it.
const (
	unitsRoute        = "/units"
	unitsConvertRoute = "/units/convert"
)

// convertParams are the conversion's query parameters: the value as exact
// digits, the two unit spellings, and delta for a difference.
var convertParams = []string{"value", "from", "to", "delta"}

// getUnits answers the repository's units, conversions and graph revision,
// and the shipped records its seed skipped.
func (h *handler) getUnits(w http.ResponseWriter, r *http.Request) {
	if bad := unsupportedParam(r); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad)
		return
	}
	out, err := DatasetFrom(r.Context()).Units(r.Context())
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// getUnitsConvert converts one value through the repository's graph: a
// missing parameter is a bad_request, and a value, a unit or a pair of units
// the graph refuses is a validation error carrying the reason.
func (h *handler) getUnitsConvert(w http.ResponseWriter, r *http.Request) {
	if bad := unsupportedParam(r, convertParams...); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad)
		return
	}
	q := r.URL.Query()
	for _, name := range []string{"value", "from", "to"} {
		if q.Get(name) == "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, fmt.Sprintf("%s is required: the conversion reads value, from and to", name))
			return
		}
	}
	delta, err := boolParam(q.Get("delta"))
	if err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, "delta: "+err.Error())
		return
	}
	c, err := DatasetFrom(r.Context()).ConvertUnit(r.Context(), substrate.UnitConvert{
		Value: q.Get("value"), From: q.Get("from"), To: q.Get("to"), Delta: delta,
	})
	if err != nil {
		writeSubstrateError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// boolParam reads a query flag: absent or false is false, true is true.
func boolParam(v string) (bool, error) {
	switch v {
	case "", "false":
		return false, nil
	case "true":
		return true, nil
	}
	return false, fmt.Errorf("%q is true or false", v)
}
