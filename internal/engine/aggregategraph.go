package engine

// THE AGGREGATE READ'S UNITS THROUGH THE REPOSITORY'S UNIT GRAPH (decision
// 0186). A read answers in the
// request's `unit`, else the metric's, else the first stream's, and every
// stored value and rollup row converts into it at read through the exact
// affine map between their units (aggUnits.factor), so a corrected
// conversion corrects every answer with no re-derivation. A unit outside the
// answer unit's linear component is refused, naming the units in it; a level
// refuses the reducers its scale does not take, by the graph's `scale`.

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// aggregateUnits settles the read's units: the request's unit, else the
// metric's, else the first stream's, and refuses a request unit outside that
// unit's linear component, naming the units in it. A stored unit that does
// not convert is refused where the read takes a value in it
// (aggRun.conversion), not here: a stream's unit is its newest point's, and a
// point outside the range refuses nothing.
func (ds *dataset) aggregateUnits(ctx context.Context, tx *sql.Tx, p *aggPlan, streams []*aggStream) (*aggUnits, error) {
	at, err := ds.unitGraph(ctx, tx)
	if err != nil {
		return nil, err
	}
	g := at.graph
	u := &aggUnits{duration: "s", graph: g}
	anchor := ""
	if p.metric != "" {
		c, _, err := metricContract(ctx, tx, ds.registry(), p.metric)
		if err != nil {
			return nil, err
		}
		if id, held, err := vocabulary.MetricUnitOf(g, c.UnitValue); err == nil && held {
			anchor = id
		}
	}
	var first *aggStream
	for _, s := range streams {
		if s.unit == "" {
			continue
		}
		if first == nil || (first.form == vocabulary.FormSummary && s.form != vocabulary.FormSummary) {
			first = s
		}
	}
	if anchor == "" && first != nil {
		if id, ok := g.Resolve(first.unit); ok {
			anchor = id
		}
	}
	durations := false
	for _, r := range p.reducers {
		if r.Name == substrate.ReducerDurationIn || r.Name == substrate.ReducerTimeInState {
			durations = true
		}
	}
	if p.durationUnit != "" {
		if !durations && p.kind != aggLatest && !p.isEntry() {
			return nil, refuse("aggregate.durationUnit", "durationUnit is the unit durationIn and timeInState answer their times in, and this read asks neither")
		}
		u.duration = p.durationUnit
	}
	if p.unit != "" {
		id, ok := g.Resolve(p.unit)
		apart := false
		if ok && anchor != "" {
			_, _, err := g.Linear(id, anchor, false)
			apart = err != nil
		}
		switch {
		case (!ok || apart) && isTimeUnit(p.unit) && durations:
			return nil, refuse(p.path("unit"), "%s is a unit of duration, and unit converts the values of %s; name it in durationUnit, the unit durationIn and timeInState answer their times in",
				p.unit, aggMetricName(p))
		case !ok:
			return nil, refuse(p.path("unit"), "%q names no unit of this repository", p.unit)
		case anchor == "" && first == nil:
			return nil, refuse(p.path("unit"), "%s converts nothing here: the selected streams hold codes; a timeInState answers its times in durationUnit", p.unit)
		case anchor != "":
			if _, _, err := g.Linear(id, anchor, false); err != nil {
				return nil, refuse(p.path("unit"), "%s does not convert to %s, the unit of %s: %v; name one of %s",
					id, anchor, aggMetricName(p), err, componentList(g, anchor))
			}
		}
		u.value = id
	}
	if u.value == "" {
		u.value = anchor
	}
	if u.value == "" && first != nil {
		u.value = first.unit
	}
	return u, nil
}

// componentList names the units of a unit's linear component, at most a
// dozen of them.
func componentList(g *vocabulary.UnitGraph, id string) string {
	members := g.Component(id)
	if len(members) > 12 {
		return strings.Join(members[:12], ", ") + fmt.Sprintf(" and %d more", len(members)-12)
	}
	return strings.Join(members, ", ")
}

// orCodes is a unit for a refusal, or what a read holds without one.
func orCodes(unit string) string {
	if unit == "" {
		return "codes"
	}
	return unit
}

// levelRefusal says why a reducer does not apply to values in a unit, by the
// graph's scale of the unit, or "" where it does: a level's values do not
// add, and a power level's do not average either.
func levelRefusal(scale vocabulary.UnitScale, unit, reducer string) string {
	switch {
	case scale == "":
		return ""
	case slices.Contains(levelAdds, reducer):
		return fmt.Sprintf("%s is logarithmic, and its values do not add (61 and 62 %s are not 123 %s)", unit, unit, unit)
	case scale == vocabulary.UnitScalePowerLevel && slices.Contains(levelMeans, reducer):
		return fmt.Sprintf("%s is the level of a power, whose mean is not the mean of its decibels (60 and 80 %s an hour each hold 77.0, not 70)", unit, unit)
	}
	return ""
}

// checkLevels refuses, on every form and every read, a reducer or a rolling
// reducer that adds or averages what a level's values do not (levelRefusal),
// what naming the metric or the property read. count, min, max, first, last,
// percentile and durationIn read a level as it is. A nil graph holds no
// level.
func (p *aggPlan) checkLevels(g *vocabulary.UnitGraph, unit, what string) error {
	if g == nil {
		return nil
	}
	scale := g.Scale(unit)
	for i, r := range p.reducers {
		if why := levelRefusal(scale, unit, r.Name); why != "" {
			return p.refuse(p.reducerPath(i), "%s does not apply to %s: %s; read count, min, max, first, last or percentile",
				r.String(), what, why)
		}
	}
	if p.rolling == nil {
		return nil
	}
	for i, r := range p.rolling.Reducers {
		if why := levelRefusal(scale, unit, r.Name); why != "" {
			return p.refuse(fmt.Sprintf("aggregate.rolling.reducers[%d]", i), "a rolling %s does not apply to %s: %s; roll min or max",
				r.String(), what, why)
		}
	}
	return nil
}
