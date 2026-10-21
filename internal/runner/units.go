package runner

// THE UNIT TABLE OVER THE HOST CALL. A function body converts, canonicalizes
// and classifies units through the engine's own code, never a Python copy of
// it, so a body and the write path that will hold its effects agree on every
// unit. Before the unit graph switch (decision 0186) the calls read the
// engine's table and nothing from the repository, so they are not reads:
// they charge no budget and need no grant. Under the switch they read the
// repository's unit graph (Backend.Units), loaded at the first unit call and
// kept for the invocation, and each charges one call against the read budget,
// with no grant, since units are no kind a body is granted. The host-call
// loop serves them synchronously, so each is bounded: a value of at most
// vocabulary.MaxDecimalDigits digits, and none once the invocation's context
// is done.

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
	hostUnitsConverts  = "units.converts"
)

func isUnitCall(host string) bool {
	switch host {
	case hostUnitsConvert, hostUnitsCanonical, hostUnitsMeasure, hostUnitsConverts:
		return true
	}
	return false
}

// unitGraph is the repository's graph for the invocation, loaded at the first
// unit call; nil where the backend answers none, which is the engine's table.
func (s *readState) unitGraph(ctx context.Context) (*vocabulary.UnitGraph, error) {
	if s.unitsLoaded {
		return s.units, nil
	}
	g, err := s.backend.Units(ctx)
	if err != nil {
		return nil, err
	}
	s.units, s.unitsLoaded = g, true
	return g, nil
}

// serveUnitCall answers one unit host call; handled is false for any other
// call.
func (s *readState) serveUnitCall(ctx context.Context, call hostCall) (out any, handled bool, err error) {
	if !isUnitCall(call.Host) {
		return nil, false, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, true, fmt.Errorf("%s: %w", call.Host, err)
	}
	g, err := s.unitGraph(ctx)
	if err != nil {
		return nil, true, fmt.Errorf("%s: %w", call.Host, err)
	}
	if g == nil {
		return serveUnits(ctx, call)
	}
	if err := s.chargeCall(); err != nil {
		return nil, true, s.trip(err)
	}
	out, err = serveGraphUnits(ctx, g, call)
	return out, true, err
}

// serveGraphUnits answers a unit host call through the repository's graph.
func serveGraphUnits(ctx context.Context, g *vocabulary.UnitGraph, call hostCall) (any, error) {
	switch call.Host {
	case hostUnitsConvert:
		var in struct {
			Value   string `json:"value"`
			From    string `json:"from"`
			To      string `json:"to"`
			Measure string `json:"measure"`
			Delta   bool   `json:"delta"`
		}
		if err := json.Unmarshal(call.Params, &in); err != nil {
			return nil, fmt.Errorf("%s: %w", call.Host, err)
		}
		if in.Measure != "" {
			return nil, fmt.Errorf("%s: measure is the engine's table's, and this repository converts through its unit graph: pass delta=True for a difference", call.Host)
		}
		c, err := g.Convert(ctx, in.Value, in.From, in.To, in.Delta)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", call.Host, err)
		}
		return map[string]any{"value": c.Value, "unit": c.Unit, "exact": c.Exact}, nil
	case hostUnitsCanonical:
		code, err := unitCode(call)
		if err != nil {
			return nil, err
		}
		id, ok := g.Resolve(code)
		if !ok {
			return nil, fmt.Errorf("%s: %q names no unit of this repository", call.Host, code)
		}
		return map[string]any{"code": id}, nil
	case hostUnitsConverts:
		a, b, err := unitPair(call)
		if err != nil {
			return nil, err
		}
		return map[string]any{"converts": graphConverts(g, a, b)}, nil
	case hostUnitsMeasure:
		return nil, fmt.Errorf("%s: a measure is the engine's table's, and this repository converts through its unit graph: ask units.converts(a, b)", call.Host)
	}
	return nil, fmt.Errorf("%s: not a unit call", call.Host)
}

// graphConverts reports whether two spellings name units a linear conversion
// joins: exactly, both ways, as a filter or an aggregate converts.
func graphConverts(g *vocabulary.UnitGraph, a, b string) bool {
	ia, ok := g.Resolve(a)
	if !ok {
		return false
	}
	ib, ok := g.Resolve(b)
	if !ok {
		return false
	}
	_, _, err := g.Linear(ia, ib, false)
	return err == nil
}

func unitPair(call hostCall) (string, string, error) {
	var in struct {
		A string `json:"a"`
		B string `json:"b"`
	}
	if err := json.Unmarshal(call.Params, &in); err != nil {
		return "", "", fmt.Errorf("%s: %w", call.Host, err)
	}
	return in.A, in.B, nil
}

// serveUnits answers one unit host call through the engine's table.
func serveUnits(ctx context.Context, call hostCall) (out any, handled bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, true, fmt.Errorf("%s: %w", call.Host, err)
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
	case hostUnitsConverts:
		a, b, err := unitPair(call)
		if err != nil {
			return nil, true, err
		}
		return map[string]any{"converts": tableConverts(a, b)}, true, nil
	}
	return nil, false, nil
}

// tableConverts reports whether the engine's table reads two spellings in
// one measure.
func tableConverts(a, b string) bool {
	ca, err := vocabulary.CanonicalUnit(a)
	if err != nil {
		return false
	}
	cb, err := vocabulary.CanonicalUnit(b)
	if err != nil {
		return false
	}
	ma, err := vocabulary.MeasureOf(ca)
	if err != nil {
		return false
	}
	_, err = vocabulary.ResolveMeasure(cb, ma.Name)
	return err == nil
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
