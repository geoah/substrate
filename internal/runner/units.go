package runner

// THE UNIT GRAPH OVER THE HOST CALL. A function body converts, resolves and
// compares units through the engine's own code, never a Python copy of it, so
// a body and the write path that will hold its effects agree on every unit.
// The calls read the repository's unit graph (decision 0186, Backend.Units),
// loaded at the first unit call and kept for the invocation, and each charges
// one call against the read budget, with no grant, since units are no kind a
// body is granted. The host-call
// loop serves them synchronously, so each is bounded: a value of at most
// vocabulary.MaxDecimalDigits digits, and none once the invocation's context
// is done.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/geoah/substrate/internal/vocabulary"
)

// The unit host calls' names, as host.py sends them.
const (
	hostUnitsConvert   = "units.convert"
	hostUnitsCanonical = "units.canonical"
	hostUnitsConverts  = "units.converts"
)

func isUnitCall(host string) bool {
	switch host {
	case hostUnitsConvert, hostUnitsCanonical, hostUnitsConverts:
		return true
	}
	return false
}

// unitGraph is the repository's graph for the invocation, loaded at the first
// unit call.
func (s *readState) unitGraph(ctx context.Context) (*vocabulary.UnitGraph, error) {
	if s.unitsLoaded {
		return s.units, nil
	}
	g, err := s.backend.Units(ctx)
	if err != nil {
		return nil, err
	}
	if g == nil {
		return nil, errors.New("the backend answers no unit graph")
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
		// measure is the retired spelling of delta. Read as absent it would
		// convert a difference with the offset, so it is refused by name, and
		// any other key a body could not have meant is refused with it.
		var in struct {
			Value   string          `json:"value"`
			From    string          `json:"from"`
			To      string          `json:"to"`
			Delta   bool            `json:"delta"`
			Measure json.RawMessage `json:"measure"`
		}
		dec := json.NewDecoder(bytes.NewReader(call.Params))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&in); err != nil {
			return nil, fmt.Errorf("%s: %w", call.Host, err)
		}
		if in.Measure != nil {
			return nil, fmt.Errorf("%s: measure is retired: pass delta=True to convert a difference, and nothing otherwise", call.Host)
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

func unitCode(call hostCall) (string, error) {
	var in struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(call.Params, &in); err != nil {
		return "", fmt.Errorf("%s: %w", call.Host, err)
	}
	return in.Code, nil
}
