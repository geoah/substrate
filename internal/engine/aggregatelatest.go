package engine

// The latest read (aggregate.go): each group's newest point, straight from
// observed_latest, which takes no rollup.

import (
	"context"
	"database/sql"
	"sort"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// aggregateLatest answers one row per group of the groupBy stream keys: the
// newest point of the group's streams (the latest instant, then the greater
// record id), with the streams' point count.
func (ds *dataset) aggregateLatest(ctx context.Context, tx *sql.Tx, p *aggPlan) (*substrate.AggregateResult, error) {
	streams, err := ds.selectStreams(ctx, tx, p)
	if err != nil {
		return nil, err
	}
	if links := ds.newSourceLinks(p); links != nil {
		if _, err := links.expand(ctx, tx, streams, 0, p.maxSamples); err != nil {
			return nil, err
		}
		streams, _ = links.keep(streams, streams, nil)
	}
	res := &substrate.AggregateResult{Metric: p.metric, From: substrate.AggregateFromRaw, Exact: true}
	var units *aggUnits
	if p.unit != "" {
		if units, err = ds.aggregateUnits(ctx, tx, p, streams); err != nil {
			return nil, err
		}
		res.Unit = units.value
	}
	type group struct {
		key    substrate.AggregateGroup
		sort   string
		newest *aggStream
		points int64
	}
	byKey := map[string]*group{}
	var list []*group
	for _, s := range streams {
		key, k := p.groupKey(s, false)
		g := byKey[k]
		if g == nil {
			g = &group{key: key, sort: k}
			byKey[k] = g
			list = append(list, g)
		}
		g.points += s.points
		if g.newest == nil || newer(s, g.newest) {
			g.newest = s
		}
	}
	if len(list) > p.maxGroups {
		return nil, refuse("aggregate.budget.maxGroups", "the latest read makes %d groups, over %d; narrow the filter or raise maxGroups (the cap is %d)",
			len(list), p.maxGroups, substrate.AggregateMaxGroupsCap)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].sort < list[j].sort })
	sources := map[string]bool{}
	for _, g := range list {
		s := g.newest
		zone := time.FixedZone("", int(s.offset)*60)
		row := substrate.AggregateRow{
			Start: s.ts.In(zone), End: s.tsEnd.In(zone), Form: s.form, From: substrate.AggregateFromRaw,
			Count: g.points,
		}
		if s.form == vocabulary.FormSummary {
			row.From = substrate.AggregateFromSummaries
		}
		if s.comparator != "" {
			row.Bounded = 1
		}
		if len(p.groupBy) > 0 {
			key := g.key
			row.Group = &key
		}
		if s.source != "" {
			row.Sources = []string{s.source}
			sources[s.source] = true
		}
		v := substrate.AggregateValue{Reducer: substrate.AggregateReducer{Name: substrate.ReducerLast}, Code: s.code}
		if s.value.Valid {
			digits, unit := s.value.String, s.unit
			if units != nil && s.unit != "" {
				c, err := units.factor(s.unit, s.delta)
				if err != nil {
					return nil, refuse("aggregate.unit", "the stream from %s stores %s, which does not convert to %s: %v", orOwner(s.source), s.unit, units.value, err)
				}
				var exact bool
				if digits, exact = c.digits(digits); !exact {
					res.Exact = false
				}
				unit = units.value
			}
			v.Value, v.Unit, v.Comparator = &digits, unit, s.comparator
		}
		row.Values = []substrate.AggregateValue{v}
		res.Rows = append(res.Rows, row)
	}
	for s := range sources {
		res.Sources = append(res.Sources, s)
	}
	sort.Strings(res.Sources)
	return res, nil
}

// newer orders two streams' newest points as observed_latest orders points
// (observedsettle.go latestOrder): a record is in one stream, so the record
// id decides a tie.
func newer(a, b *aggStream) bool {
	if !a.ts.Equal(b.ts) {
		return a.ts.After(b.ts)
	}
	return a.recordID > b.recordID
}
