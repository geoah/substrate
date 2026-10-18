package runner

// THE UNIT TABLE OVER THE HOST CALL. A function body converts, canonicalizes
// and classifies units through the engine's own table (internal/vocabulary),
// never a Python copy of it, so a body and the write path that will hold its
// effects agree on every code. The three calls read nothing from the
// repository, so they are not reads: they charge no budget and need no grant.
// The host-call loop serves them synchronously, so each is bounded instead: a
// value of at most vocabulary.MaxDecimalDigits digits and a code of at most
// the grammar's components, and none once the invocation's context is done.

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The unit host calls' names, as host.py sends them.
const (
	hostUnitsConvert   = "units.convert"
	hostUnitsCanonical = "units.canonical"
	hostUnitsMeasure   = "units.measure"
)

// serveUnits answers one unit host call; handled is false for any other call.
func serveUnits(ctx context.Context, call hostCall) (out any, handled bool, err error) {
	switch call.Host {
	case hostUnitsConvert, hostUnitsCanonical, hostUnitsMeasure:
		if err := ctx.Err(); err != nil {
			return nil, true, fmt.Errorf("%s: %w", call.Host, err)
		}
	}
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
// aliases a writer may send (`°C`, `kWh`), as vocabulary.ConvertDigits does:
// every digit when the value has a finite decimal expansion, else
// ConvertedDigits significant digits with `exact: false`. The value holds at
// most vocabulary.MaxDecimalDigits digits, refused by name before any
// arithmetic.
func convertUnits(value, from, to, measure string) (map[string]any, error) {
	c, err := vocabulary.ConvertDigits(value, from, to, measure)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", hostUnitsConvert, err)
	}
	return map[string]any{"value": c.Value, "unit": c.Unit, "exact": c.Exact}, nil
}
