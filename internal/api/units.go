package api

import (
	"fmt"
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The unit table's two routes. GET /units answers the repository's unit graph
// (decision 0186) and, beside it, the engine's own table (internal/vocabulary),
// which the write path still holds a quantity to; the conversion route
// converts through that table and reads nothing from the repository.
const (
	unitsRoute        = "/units"
	unitsConvertRoute = "/units/convert"
)

// convertParams are the conversion's query parameters: the value as exact
// digits, the two unit spellings, and the measure to convert in where the
// units' own reading is not meant.
var convertParams = []string{"value", "from", "to", "measure", "delta"}

// getUnits answers the repository's units, conversions and graph revision,
// the shipped records its seed skipped, and the engine's table: every
// measure, its listed codes with their print symbols and exact factors to the
// base, and the input aliases.
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
	out.Measures, out.Aliases = []substrate.UnitMeasure{}, vocabulary.UnitAliases()
	for _, m := range vocabulary.Measures() {
		um := substrate.UnitMeasure{
			Name: m.Name, Base: m.Base, Scale: string(m.Scale), Display: m.Display,
			Dimension: m.Dimension(), Note: m.Note, Units: make([]substrate.UnitCode, 0, len(m.Units)),
		}
		for _, code := range m.Units {
			uc := substrate.UnitCode{Code: code, Print: vocabulary.UnitSymbol(code)}
			if f, o, err := vocabulary.ToBase(m.Name, code); err == nil {
				uc.Factor = f.RatString()
				if o.Sign() != 0 {
					uc.Offset = o.RatString()
				}
			}
			um.Units = append(um.Units, uc)
		}
		out.Measures = append(out.Measures, um)
	}
	writeJSON(w, http.StatusOK, out)
}

// getUnitsConvert converts one value exactly (vocabulary.ConvertDigits): a
// missing parameter is a bad_request, and a value, a unit or a pair of units
// the table refuses is a validation error carrying the table's reason.
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
	// Under the unit graph switch the repository's graph converts, a
	// difference marked by delta; `measure` is the table's spelling of it.
	if h.unitGraph {
		if q.Get("measure") != "" {
			writeError(w, http.StatusBadRequest, codeBadRequest, "measure is the engine's table's: this deployment converts through the repository's unit graph, so pass delta=true for a difference")
			return
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
		return
	}
	if q.Get("delta") != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, "delta converts through the repository's unit graph, which this deployment does not read yet; name measure=temperature-difference")
		return
	}
	c, err := vocabulary.ConvertDigits(q.Get("value"), q.Get("from"), q.Get("to"), q.Get("measure"))
	if err != nil {
		writeSubstrateError(w, r, fmt.Errorf("%w: %w", substrate.ErrValidation, err))
		return
	}
	writeJSON(w, http.StatusOK, substrate.UnitConversion{Value: c.Value, Unit: c.Unit, Measure: c.Measure, Exact: c.Exact})
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
