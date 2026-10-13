package api

import (
	"fmt"
	"net/http"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The unit table's two routes. Both answer from the engine's own table
// (internal/vocabulary), the one the write path holds a quantity to, and read
// nothing from the repository: a client converting what a list read returns
// gets the server's exact answer instead of copying factors from the docs.
const (
	unitsRoute        = "/units"
	unitsConvertRoute = "/units/convert"
)

// convertParams are the conversion's query parameters: the value as exact
// digits, the two unit spellings, and the measure to convert in where the
// units' own reading is not meant.
var convertParams = []string{"value", "from", "to", "measure"}

// getUnits answers the unit table: every measure, its listed codes with their
// print symbols and exact factors to the base, and the input aliases.
func (h *handler) getUnits(w http.ResponseWriter, r *http.Request) {
	if bad := unsupportedParam(r); bad != "" {
		writeError(w, http.StatusBadRequest, codeBadRequest, bad)
		return
	}
	out := substrate.UnitTable{Measures: []substrate.UnitMeasure{}, Aliases: vocabulary.UnitAliases()}
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
	c, err := vocabulary.ConvertDigits(q.Get("value"), q.Get("from"), q.Get("to"), q.Get("measure"))
	if err != nil {
		writeSubstrateError(w, r, fmt.Errorf("%w: %w", substrate.ErrValidation, err))
		return
	}
	writeJSON(w, http.StatusOK, substrate.UnitConversion{Value: c.Value, Unit: c.Unit, Measure: c.Measure, Exact: c.Exact})
}
