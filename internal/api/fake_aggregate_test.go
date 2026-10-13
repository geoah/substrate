package api

import (
	"context"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The fake's Aggregate records the query it was handed and answers a canned
// result: the route's job is the grammar and the refusals around it, and the
// reduction itself is the engine's, tested against Postgres
// (engine/aggregate_db_test.go). Two refusals the engine makes are mirrored
// here so a test can prove the route maps them to a 422.

func (d *fakeDataset) Aggregate(_ context.Context, q substrate.AggregateQuery) (*substrate.AggregateResult, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.fail("Aggregate"); err != nil {
		return nil, err
	}
	d.lastAggregate = &q
	if q.Aggregate == nil {
		return nil, fmt.Errorf("%w: aggregate: the read needs an aggregate body", substrate.ErrValidation)
	}
	selectors := 0
	for _, set := range []bool{q.Aggregate.Metric != nil, q.Aggregate.Metrics != nil, q.Aggregate.Property != nil} {
		if set {
			selectors++
		}
	}
	if selectors > 1 {
		return nil, &substrate.ValidationError{Problems: []string{"aggregate: name one of metric, metrics and property"}}
	}
	if d.aggregateResult != nil {
		out := *d.aggregateResult
		return &out, nil
	}
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.FixedZone("", 3*3600))
	value := "62"
	return &substrate.AggregateResult{
		Metric: metricRef(q.Aggregate), Unit: "/min", From: substrate.AggregateFromRaw, Exact: true,
		Bucket: q.Aggregate.Bucket,
		Rows: []substrate.AggregateRow{{
			Start: start, End: start.AddDate(0, 0, 1), Form: "gauge", From: substrate.AggregateFromRaw,
			Values: []substrate.AggregateValue{{Reducer: substrate.AggregateReducer{Name: substrate.ReducerAvg}, Value: &value, Unit: "/min"}},
			Count:  1,
		}},
		Head: substrate.AggregateHead{Changelog: int64(len(d.changes)), Generation: d.generation},
	}, nil
}

func metricRef(a *substrate.Aggregate) string {
	if a == nil || a.Metric == nil {
		return ""
	}
	return a.Metric.Ref
}
