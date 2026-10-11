package runner

// THE UNIT TABLE OVER THE HOST CALL. A function body converts, canonicalizes
// and classifies units through the engine's own table (internal/vocabulary),
// never a Python copy of it, so a body and the write path that will hold its
// effects agree on every code. The three calls read nothing from the
// repository, so they are not reads: they charge no budget and need no grant.

import (
	"encoding/json"
	"fmt"
	"math/big"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The unit host calls' names, as host.py sends them.
const (
	hostUnitsConvert   = "units.convert"
	hostUnitsCanonical = "units.canonical"
	hostUnitsMeasure   = "units.measure"
)

// serveUnits answers one unit host call; handled is false for any other call.
func serveUnits(call hostCall) (out any, handled bool, err error) {
	switch call.Host {
	case hostUnitsConvert:
		var in struct {
			Value   string `json:"value"`
			From    string `json:"from"`
			To      string `json:"to"`
			Measure string `json:"measure"`
		}
		if err := json.Unmarshal(call.Params, &in); err != nil {
			return nil, true, fmt.Errorf("%s: %w", call.Host, err)
		}
		out, err := convertUnits(in.Value, in.From, in.To, in.Measure)
		return out, true, err
	case hostUnitsCanonical:
		code, err := unitCode(call)
		if err != nil {
			return nil, true, err
		}
		canonical, err := vocabulary.CanonicalUnit(code)
		if err != nil {
			return nil, true, fmt.Errorf("%s: %w", call.Host, err)
		}
		return map[string]any{"code": canonical}, true, nil
	case hostUnitsMeasure:
		code, err := unitCode(call)
		if err != nil {
			return nil, true, err
		}
		canonical, err := vocabulary.CanonicalUnit(code)
		if err != nil {
			return nil, true, fmt.Errorf("%s: %w", call.Host, err)
		}
		m, err := vocabulary.MeasureOf(canonical)
		if err != nil {
			return nil, true, fmt.Errorf("%s: %w", call.Host, err)
		}
		return map[string]any{"measure": m.Name}, true, nil
	}
	return nil, false, nil
}

func unitCode(call hostCall) (string, error) {
	var in struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(call.Params, &in); err != nil {
		return "", fmt.Errorf("%s: %w", call.Host, err)
	}
	return in.Code, nil
}

// convertUnits converts exact digits between two units, read through the
// aliases a writer may send (`°C`, `kWh`). Without a measure, both units are
// read the way MeasureOf reads them (Cel to [degF] is an absolute
// temperature); `measure` names the one to convert in
// (`temperature-difference` for a deviation). The answer is every digit when
// the value has a finite decimal expansion, else ConvertedDigits significant
// digits with `exact: false`.
func convertUnits(value, from, to, measure string) (map[string]any, error) {
	digits, err := vocabulary.CanonicalDecimal(value)
	if err != nil {
		return nil, fmt.Errorf("%s: value: %w", hostUnitsConvert, err)
	}
	fromCode, err := vocabulary.CanonicalUnit(from)
	if err != nil {
		return nil, fmt.Errorf("%s: from: %w", hostUnitsConvert, err)
	}
	toCode, err := vocabulary.CanonicalUnit(to)
	if err != nil {
		return nil, fmt.Errorf("%s: to: %w", hostUnitsConvert, err)
	}
	v, _ := new(big.Rat).SetString(digits)
	var r *big.Rat
	if measure != "" {
		r, err = vocabulary.ConvertIn(measure, v, fromCode, toCode)
	} else {
		r, err = vocabulary.Convert(v, fromCode, toCode)
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", hostUnitsConvert, err)
	}
	out, exact := vocabulary.RatDecimal(r, vocabulary.ConvertedDigits)
	return map[string]any{"value": out, "unit": toCode, "exact": exact}, nil
}
