package engine

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The records' priority policy drops a record overlapping a kept record of a
// higher rank with the same match values, and nothing else: a dropped
// record drops nothing, two records of one rank never drop each other, a
// record of another match is kept, and a record with no `at` overlaps
// nothing.
func TestKeepBySourceDropsOnlyUnderAKeptHigherRank(t *testing.T) {
	t0 := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	span := func(from, to, rank int, match string) policyRecord {
		return policyRecord{start: t0.Add(time.Duration(from) * time.Minute), end: t0.Add(time.Duration(to) * time.Minute), placed: true, rank: rank, match: match}
	}
	recs := []policyRecord{
		span(0, 10, 0, ""),  // a: kept
		span(5, 15, 1, ""),  // b: under a, dropped
		span(12, 20, 2, ""), // c: under b alone, which was dropped: kept
		span(30, 40, 1, ""), // d: same rank as e, kept
		span(35, 45, 1, ""), // e: kept
		span(50, 60, 0, "x"),
		span(55, 65, 1, "y"), // another match: kept
		span(56, 58, 1, "x"), // the same match: dropped
		{rank: 1, match: ""}, // no at: kept
		span(9, 10, 3, ""),   // under a: dropped
		span(10, 11, 1, ""),  // starts where a ends, inside c's rank-2 span: kept
	}
	keep, err := keepBySource(context.Background(), recs)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for i, k := range keep {
		got = append(got, fmt.Sprintf("%d:%v", i, k))
	}
	want := "0:true 1:false 2:true 3:true 4:true 5:true 6:true 7:false 8:true 9:false 10:true"
	if strings.Join(got, " ") != want {
		t.Errorf("kept %s, want %s", strings.Join(got, " "), want)
	}
}

// An order ranks at most aggRecordOrderCap sources: the policy weighs the
// records once per rank.
func TestRecordSourcesBoundTheirOrder(t *testing.T) {
	order := make([]string, aggRecordOrderCap+1)
	for i := range order {
		order[i] = fmt.Sprintf("s%d", i)
	}
	_, err := checkRecordSources("aggregate.bucket.records.sources", &substrate.AggregateRecordSources{Policy: "priority", Property: "source", Order: order})
	if err == nil || !strings.Contains(err.Error(), "at most 100") {
		t.Errorf("err = %v, want a refusal naming the cap", err)
	}
	if _, err := checkRecordSources("p", &substrate.AggregateRecordSources{Policy: "priority", Property: "source", Order: order[:aggRecordOrderCap]}); err != nil {
		t.Errorf("an order at the cap: %v", err)
	}
}
