package engine_test

// The observed index against Postgres (decision 0160, migration 0012): the
// points every shape of observed record derives, the rollup rows the fold
// computes from them (held to internal/observed, the specification, column
// by column), the latest row of each stream, and every path that moves them:
// a rewrite, a tombstone, a restore, a purge, a correction that moves a
// point to another stream, a vocabulary change and a rebuild.

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/geoah/substrate/internal/catalog"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/observed"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/samples"
)

// obsIndex is a repository holding the observations and health samples, with
// the tamperer's seat on its database to read the index tables.
type obsIndex struct {
	observedRepo
	svc substrate.Service
	db  *sql.DB
}

func openObservedIndex(t *testing.T, opts ...engine.Option) obsIndex {
	t.Helper()
	ctx := context.Background()
	svc, dsn := newService(t, opts...)
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	c, err := catalog.Load(catalog.SampleRoot(samples.Samples()))
	if err != nil {
		t.Fatalf("load the sample catalog: %v", err)
	}
	for _, id := range []string{samples.Authority + "/observations", samples.Authority + "/health"} {
		if _, _, err := c.Import(ctx, substrate.ActorAPI, id, ds); err != nil {
			t.Fatalf("import %s: %v", id, err)
		}
	}
	home := testdb.Repository(t)
	return obsIndex{
		observedRepo: observedRepo{
			ds: ds, observation: home + "/observations/observation", trace: home + "/observations/trace",
			metric: home + "/observations/metric", source: home + "/observations/source",
			labreport: home + "/health/labreport", labresult: home + "/health/labresult",
		},
		svc: svc, db: rawDB(t, dsn),
	}
}

func (r obsIndex) mustPut(t *testing.T, kind, id string, props map[string]any) {
	t.Helper()
	if _, err := r.put(t, kind, id, props); err != nil {
		t.Fatalf("put %s %s: %v", kind, id, err)
	}
}

func (r obsIndex) sourceRef(id string) map[string]any {
	return map[string]any{"ref": r.source + "/" + id}
}

// pointRow is one row of observed_points as the test reads it.
type pointRow struct {
	ord                         int
	metric, source, about, form string
	channel, unit, code         string
	ts, tsEnd                   time.Time
	offset                      int
	delta, bounded, circular    bool
	value, base                 sql.NullFloat64
	exact, statistic, quality   sql.NullString
	resetRule, wrapAt           sql.NullString
	hold, revision              sql.NullInt64
	resetAt                     sql.NullTime
}

func (r obsIndex) points(t *testing.T, kind, id string) []pointRow {
	t.Helper()
	rows, err := r.db.Query(`
		SELECT ord, metric, source, about, channel, form, ts, ts_end, utc_offset_min, unit, delta, value,
		       value_exact::text, value_base, code, statistic, bounded, quality, hold_ms, reset_rule, reset_at,
		       wrap_at::text, circular, revision
		FROM observed_points WHERE kind = $1 AND record_id = $2 ORDER BY ord`, kind, id)
	if err != nil {
		t.Fatalf("read points: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []pointRow
	for rows.Next() {
		var p pointRow
		if err := rows.Scan(&p.ord, &p.metric, &p.source, &p.about, &p.channel, &p.form, &p.ts, &p.tsEnd, &p.offset,
			&p.unit, &p.delta, &p.value, &p.exact, &p.base, &p.code, &p.statistic, &p.bounded, &p.quality, &p.hold,
			&p.resetRule, &p.resetAt, &p.wrapAt, &p.circular, &p.revision); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}

func (r obsIndex) count(t *testing.T, q string, args ...any) int {
	t.Helper()
	var n int
	if err := r.db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// Every shape of observed record derives its points: a single gauge, a coded
// gauge, a summary, an amount with its category, a counter trace, a trace
// with nulls at irregular offsets, and a bounded lab value.
func TestObservedIndexDerivesThePointsOfEveryShape(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	metric := func(id string) string { return r.metric + "/" + id }

	r.mustPut(t, r.observation, "withings/w1", map[string]any{
		"metric": r.metricRef("health/body_mass"), "source": r.sourceRef("withings-body"),
		"form": "gauge", "value": quantity("72.40", "kg"), "at": "2026-10-07T06:41:12+03:00",
		"utcOffset": 180, "revision": 3, "quality": "valid",
	})
	if got := r.points(t, r.observation, "withings/w1"); len(got) != 1 ||
		got[0].metric != metric("health/body_mass") || got[0].source != r.source+"/withings-body" ||
		got[0].form != "gauge" || !got[0].ts.Equal(mustTime(t, "2026-10-07T03:41:12Z")) || !got[0].tsEnd.Equal(got[0].ts) ||
		got[0].offset != 180 || got[0].unit != "kg" || got[0].value.Float64 != 72.4 || got[0].exact.String != "72.40" ||
		got[0].base.Float64 != 72.4 || got[0].code != "" || got[0].bounded || got[0].quality.String != "valid" ||
		got[0].revision.Int64 != 3 {
		t.Errorf("a weighing derived %+v", got)
	}

	r.mustPut(t, r.observation, "door/1", map[string]any{
		"metric": r.metricRef("air_temperature"), "about": map[string]any{"ref": r.source + "/front-door"},
		"channel": "contact", "form": "gauge", "code": "open", "hold": "PT0S", "at": "2026-10-07T17:42:05Z",
	})
	if got := r.points(t, r.observation, "door/1"); len(got) != 1 || got[0].code != "open" || got[0].value.Valid ||
		got[0].exact.Valid || got[0].about != r.source+"/front-door" || got[0].channel != "contact" ||
		got[0].hold.Int64 != 0 || !got[0].hold.Valid || got[0].offset != 0 {
		t.Errorf("a door derived %+v", got)
	}

	r.mustPut(t, r.observation, "whoop/george/1047392/recovery", map[string]any{
		"metric": r.metricRef("health/whoop_recovery"), "form": "summary", "statistic": "vendor", "period": "cycle",
		"value": quantity("67", "%"), "at": "2026-10-06T22:31:00Z", "endsAt": "2026-10-07T06:02:00Z", "revision": 1,
	})
	if got := r.points(t, r.observation, "whoop/george/1047392/recovery"); len(got) != 1 || got[0].form != "summary" ||
		got[0].statistic.String != "vendor" || !got[0].tsEnd.Equal(mustTime(t, "2026-10-07T06:02:00Z")) ||
		got[0].base.Float64 != 0.67 {
		t.Errorf("a summary derived %+v", got)
	}
	if n := r.count(t, `SELECT count(*) FROM observed_rollups WHERE form = 'summary'`); n != 0 {
		t.Errorf("a summary rolled up into %d rows", n)
	}

	r.mustPut(t, r.observation, "sleep/rem", map[string]any{
		"metric": r.metricRef("health/sleep_stage"), "form": "amount", "value": quantity("94", "min"), "code": "rem",
		"at": "2026-10-07T01:00:00Z", "endsAt": "2026-10-07T02:34:00Z",
	})
	if got := r.points(t, r.observation, "sleep/rem"); len(got) != 1 || got[0].code != "rem" || got[0].exact.String != "94" ||
		got[0].base.Float64 != 94*60 || !got[0].tsEnd.Equal(mustTime(t, "2026-10-07T02:34:00Z")) {
		t.Errorf("an amount with its category derived %+v", got)
	}

	r.mustPut(t, r.trace, "meter/2026-10-07T06", map[string]any{
		"metric": r.metricRef("energy"), "form": "counter", "unit": "kW.h", "resetRule": "monotonic",
		"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z", "every": "PT15M",
		"values": []any{"18342.655", "18342.700", "0.010", "0.050"}, "revision": 1,
	})
	meter := r.points(t, r.trace, "meter/2026-10-07T06")
	if len(meter) != 4 || meter[1].exact.String != "18342.700" || meter[2].value.Float64 != 0.01 ||
		meter[0].base.Float64 != 18342.655*3600000 || meter[0].resetRule.String != "monotonic" ||
		!meter[3].ts.Equal(mustTime(t, "2026-10-07T06:45:00Z")) || !meter[3].tsEnd.Equal(meter[3].ts) {
		t.Errorf("a counter trace derived %+v", meter)
	}

	r.mustPut(t, r.trace, "hr/2026-10-07T06", map[string]any{
		"metric": r.metricRef("health/heart_rate"), "form": "gauge", "unit": "bpm", "hold": "PT4M",
		"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z", "utcOffsetStart": 180, "utcOffsetEnd": 120,
		"offsets": []any{0, 118000, 241000, 1800000, 3599000}, "values": []any{"58", "57", nil, "112", "96"},
	})
	hr := r.points(t, r.trace, "hr/2026-10-07T06")
	if len(hr) != 5 {
		t.Fatalf("a trace with a null derived %d points", len(hr))
	}
	null := hr[2]
	if null.value.Valid || null.exact.Valid || null.code != "" || null.quality.String != "unavailable" ||
		!null.ts.Equal(mustTime(t, "2026-10-07T06:04:01Z")) {
		t.Errorf("the null point = %+v", null)
	}
	// The start offset holds before the midpoint and the end offset from it.
	for i, want := range []int{180, 180, 180, 120, 120} {
		if hr[i].offset != want || hr[i].unit != "{beats}/min" || hr[i].hold.Int64 != 240000 {
			t.Errorf("point %d: offset %d unit %s hold %v", i, hr[i].offset, hr[i].unit, hr[i].hold)
		}
	}

	r.mustPut(t, r.labresult, "medichecks/george/MC-1/tsh", map[string]any{
		"metric": r.metricRef("health/tsh"), "hold": "PT0S",
		"value": map[string]any{"value": "0.01", "unit": "m[IU]/L", "comparator": "<"},
		"at":    "2026-09-14T08:05:00+01:00", "utcOffset": 60, "status": "final", "revision": 1,
	})
	if got := r.points(t, r.labresult, "medichecks/george/MC-1/tsh"); len(got) != 1 || !got[0].bounded ||
		got[0].exact.String != "0.01" || got[0].base.Valid || got[0].offset != 60 {
		t.Errorf("a bounded lab value derived %+v", got)
	}
	// A lab result with no value and no code derives nothing.
	if _, err := r.patch(t, r.labresult, "medichecks/george/MC-1/tsh", map[string]any{
		"value": nil, "absentReason": "not-performed",
	}); err != nil {
		t.Fatal(err)
	}
	if got := r.points(t, r.labresult, "medichecks/george/MC-1/tsh"); len(got) != 0 {
		t.Errorf("an absent result derived %+v", got)
	}

	// An amount trace spans each point to the next and the last to endsAt.
	r.mustPut(t, r.trace, "steps/2026-10-07T06", map[string]any{
		"metric": r.metricRef("health/steps"), "form": "amount", "unit": "{steps}",
		"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z",
		"offsets": []any{0, 600000, 2700000}, "values": []any{"412", nil, "88"},
	})
	steps := r.points(t, r.trace, "steps/2026-10-07T06")
	if len(steps) != 3 || !steps[0].tsEnd.Equal(mustTime(t, "2026-10-07T06:10:00Z")) ||
		!steps[2].tsEnd.Equal(mustTime(t, "2026-10-07T07:00:00Z")) {
		t.Errorf("an amount trace derived %+v", steps)
	}
	checkRollupsMatchSpec(t, r.db)
}

// --- the rollups, held to the specification ---

// specPoint reads one stored point the way the specification takes it.
type specPoint struct {
	hold       [6]string // metric, kind, source, about, channel, form
	group      string
	point      observed.Point
	code, unit string
	delta      bool
	offset     int
}

func storedPoints(t *testing.T, db *sql.DB) []specPoint {
	t.Helper()
	rows, err := db.Query(`
		SELECT metric, kind, source, about, channel, form, ts, ts_end, value, value_exact::text, value_base, code,
		       unit, delta, utc_offset_min, bounded, hold_ms, reset_rule, reset_at, wrap_at::text, circular, record_id, ord
		FROM observed_points WHERE form <> 'summary'`)
	if err != nil {
		t.Fatalf("read the points: %v", err)
	}
	defer func() { _ = rows.Close() }()
	var out []specPoint
	for rows.Next() {
		var sp specPoint
		var value, base sql.NullFloat64
		var exact, resetRule, wrapAt sql.NullString
		var hold sql.NullInt64
		var resetAt sql.NullTime
		var circular bool
		p := &sp.point
		if err := rows.Scan(&sp.hold[0], &sp.hold[1], &sp.hold[2], &sp.hold[3], &sp.hold[4], &sp.hold[5],
			&p.At, &p.End, &value, &exact, &base, &sp.code, &sp.unit, &sp.delta, &sp.offset, &p.Bounded,
			&hold, &resetRule, &resetAt, &wrapAt, &circular, &p.RecordID, &p.Ord); err != nil {
			t.Fatal(err)
		}
		p.Value, p.HasValue, p.Exact, p.Code, p.Unit = value.Float64, value.Valid, exact.String, sp.code, sp.unit
		p.Unavailable = !value.Valid && sp.code == ""
		p.Reset, p.WrapAt = resetRule.String, wrapAt.String
		if hold.Valid {
			d := time.Duration(hold.Int64) * time.Millisecond
			p.Hold = &d
		}
		if resetAt.Valid {
			p.ResetAt = resetAt.Time
		}
		if circular && value.Valid {
			switch {
			case sp.unit == "rad":
				p.Circular, p.Angle = true, value.Float64
			case base.Valid:
				p.Circular, p.Angle = true, base.Float64*math.Pi/180
			}
		}
		sp.group = fmt.Sprintf("%s|%s|%v|%d", sp.code, sp.unit, sp.delta, sp.offset)
		p.Group = sp.group
		out = append(out, sp)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	// In the specification's order, not the database's: a stream's points by
	// observed.Before, whatever collation the database sorts text in.
	sort.SliceStable(out, func(i, j int) bool {
		if a, b := strings.Join(out[i].hold[:], "\x1f"), strings.Join(out[j].hold[:], "\x1f"); a != b {
			return a < b
		}
		return observed.Before(out[i].point, out[j].point)
	})
	return out
}

// rollupRow is one row of observed_rollups, its key spelled as a string.
type storedRollup struct {
	n, bounded, covered, maxGap, startGap  int64
	sum, min, max, mean, m2, twa, sin, cos sql.NullFloat64
	firstTS, lastTS, spanEnd               sql.NullTime
	firstValue, lastValue, increase        sql.NullString
	held, lastHold, resets                 sql.NullInt64
	firstRecord, lastRecord                sql.NullString
	firstOrd, lastOrd                      sql.NullInt64
	minValue, maxValue                     sql.NullString
}

func storedRollups(t *testing.T, db *sql.DB) map[string]storedRollup {
	t.Helper()
	rows, err := db.Query(`
		SELECT metric, kind, source, about, channel, form, code, unit, delta, utc_offset_min, bucket15,
		       n, bounded, sum, min, max, mean, m2, first_ts, first_value::text, last_ts, last_value::text,
		       covered_ms, held_ms, twa_sum, last_hold_ms, max_gap_ms, start_gap_ms, increase::text, resets,
		       sin_sum, cos_sum, span_end, first_record_id, first_ord, last_record_id, last_ord,
		       min_value::text, max_value::text
		FROM observed_rollups`)
	if err != nil {
		t.Fatalf("read the rollups: %v", err)
	}
	defer func() { _ = rows.Close() }()
	out := map[string]storedRollup{}
	for rows.Next() {
		var k [6]string
		var code, unit string
		var delta bool
		var offset int
		var bucket time.Time
		var s storedRollup
		if err := rows.Scan(&k[0], &k[1], &k[2], &k[3], &k[4], &k[5], &code, &unit, &delta, &offset, &bucket,
			&s.n, &s.bounded, &s.sum, &s.min, &s.max, &s.mean, &s.m2, &s.firstTS, &s.firstValue, &s.lastTS, &s.lastValue,
			&s.covered, &s.held, &s.twa, &s.lastHold, &s.maxGap, &s.startGap, &s.increase, &s.resets,
			&s.sin, &s.cos, &s.spanEnd, &s.firstRecord, &s.firstOrd, &s.lastRecord, &s.lastOrd,
			&s.minValue, &s.maxValue); err != nil {
			t.Fatal(err)
		}
		out[rollupKey(k, fmt.Sprintf("%s|%s|%v|%d", code, unit, delta, offset), bucket)] = s
	}
	return out
}

func rollupKey(hold [6]string, group string, bucket time.Time) string {
	return strings.Join(hold[:], "|") + "#" + group + "#" + bucket.UTC().Format(time.RFC3339)
}

// checkRollupsMatchSpec computes every rollup row the stored points imply
// with internal/observed and holds the table to it, column by column: the
// same rows, no more, and every value equal.
func checkRollupsMatchSpec(t *testing.T, db *sql.DB) int {
	t.Helper()
	points := storedPoints(t, db)
	want := map[string]*observed.Stats{}
	for i := 0; i < len(points); {
		j := i
		for j < len(points) && points[j].hold == points[i].hold {
			j++
		}
		stream := points[i:j]
		var prev *observed.Point
		for a := 0; a < len(stream); {
			b := a
			start := stream[a].point.At.UTC().Truncate(observed.BucketWidth)
			for b < len(stream) && stream[b].point.At.UTC().Truncate(observed.BucketWidth).Equal(start) {
				b++
			}
			list := make([]observed.Point, 0, b-a)
			for _, sp := range stream[a:b] {
				list = append(list, sp.point)
			}
			for g, s := range observed.Bucket(stream[a].hold[5], start, start.Add(observed.BucketWidth), prev, list) {
				want[rollupKey(stream[a].hold, g, start)] = s
			}
			for k := b - 1; k >= a; k-- {
				if !list[k-a].Unavailable {
					p := list[k-a]
					prev = &p
					break
				}
			}
			a = b
		}
		i = j
	}
	got := storedRollups(t, db)
	if len(got) != len(want) {
		var keys []string
		for k := range got {
			if want[k] == nil {
				keys = append(keys, "extra "+k)
			}
		}
		for k := range want {
			if _, ok := got[k]; !ok {
				keys = append(keys, "missing "+k)
			}
		}
		sort.Strings(keys)
		t.Fatalf("%d rollup rows, the points imply %d: %v", len(got), len(want), keys)
	}
	for k, w := range want {
		g, ok := got[k]
		if !ok {
			t.Errorf("no rollup row %s", k)
			continue
		}
		form := strings.Split(k, "|")[5]
		form = strings.SplitN(form, "#", 2)[0]
		compareRollup(t, k, form, g, w)
	}
	return len(want)
}

func compareRollup(t *testing.T, key, form string, g storedRollup, w *observed.Stats) {
	t.Helper()
	bad := func(col string, got, want any) {
		t.Errorf("%s: %s = %v, the specification says %v", key, col, got, want)
	}
	if g.n != w.N {
		bad("n", g.n, w.N)
	}
	if g.bounded != w.Bounded {
		bad("bounded", g.bounded, w.Bounded)
	}
	if g.covered != w.CoveredMs {
		bad("covered_ms", g.covered, w.CoveredMs)
	}
	if g.spanEnd.Valid != !w.SpanEnd.IsZero() || (g.spanEnd.Valid && !g.spanEnd.Time.Equal(w.SpanEnd)) {
		bad("span_end", g.spanEnd, w.SpanEnd)
	}
	if g.maxGap != w.MaxGapMs || g.startGap != w.StartGapMs {
		bad("max_gap_ms, start_gap_ms", [2]int64{g.maxGap, g.startGap}, [2]int64{w.MaxGapMs, w.StartGapMs})
	}
	float := func(col string, got sql.NullFloat64, want float64, present bool) {
		if got.Valid != present || (present && got.Float64 != want) {
			bad(col, got, want)
		}
	}
	values := w.Values > 0
	float("sum", g.sum, w.Sum, values)
	float("min", g.min, w.Min, values)
	float("max", g.max, w.Max, values)
	if g.minValue.Valid != values || g.maxValue.Valid != values ||
		(values && (!sameDecimal(g.minValue.String, w.MinExact) || !sameDecimal(g.maxValue.String, w.MaxExact))) {
		bad("min_value, max_value", []any{g.minValue, g.maxValue}, []any{w.MinExact, w.MaxExact})
	}
	float("mean", g.mean, w.Mean, values)
	float("m2", g.m2, w.M2, values)
	float("sin_sum", g.sin, w.SinSum, w.Angles > 0)
	float("cos_sum", g.cos, w.CosSum, w.Angles > 0)
	if form == vocabulary.FormGauge {
		float("twa_sum", g.twa, w.TWASum, values)
		if !g.held.Valid || g.held.Int64 != w.HeldMs {
			bad("held_ms", g.held, w.HeldMs)
		}
		if (w.LastHoldMs == nil) != !g.lastHold.Valid || (w.LastHoldMs != nil && *w.LastHoldMs != g.lastHold.Int64) {
			bad("last_hold_ms", g.lastHold, w.LastHoldMs)
		}
	} else if g.held.Valid || g.twa.Valid || g.lastHold.Valid {
		bad("held_ms, twa_sum, last_hold_ms", []any{g.held, g.twa, g.lastHold}, "null")
	}
	point := func(col string, ts sql.NullTime, digits, record sql.NullString, ord sql.NullInt64, p *observed.Point) {
		switch {
		case p == nil:
			if ts.Valid || digits.Valid || record.Valid || ord.Valid {
				bad(col, ts, "null")
			}
		case !ts.Valid || !ts.Time.Equal(p.At):
			bad(col+"_ts", ts, p.At)
		case p.HasValue != digits.Valid || (p.HasValue && !sameDecimal(digits.String, p.Exact)):
			bad(col+"_value", digits, p.Exact)
		case record.String != p.RecordID || ord.Int64 != p.Ord:
			bad(col+"_record_id, "+col+"_ord", []any{record, ord}, []any{p.RecordID, p.Ord})
		}
	}
	point("first", g.firstTS, g.firstValue, g.firstRecord, g.firstOrd, w.First)
	point("last", g.lastTS, g.lastValue, g.lastRecord, g.lastOrd, w.Last)
	if form == vocabulary.FormCounter && w.Increase != nil {
		if !g.increase.Valid || !sameDecimal(g.increase.String, w.Increase.RatString()) || g.resets.Int64 != w.Resets {
			bad("increase, resets", []any{g.increase, g.resets}, []any{w.Increase.RatString(), w.Resets})
		}
	} else if g.increase.Valid || g.resets.Valid {
		bad("increase, resets", []any{g.increase, g.resets}, "null")
	}
}

func sameDecimal(a, b string) bool {
	x, okA := new(big.Rat).SetString(a)
	y, okB := new(big.Rat).SetString(b)
	return okA && okB && x.Cmp(y) == 0
}

// TestObservedRollupEqualsTheRawAggregate: the rows the fold writes are the
// specification's over the stored points, for a gauge trace that straddles
// bucket boundaries, an hour after a three-hour silence, a coded gauge, a
// counter that resets twice in a bucket and an amount trace, through a
// correction, a deletion and a rebuild.
func TestObservedRollupEqualsTheRawAggregate(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	hr := func(id, at, ends string, offsets, values []any, rev int) {
		r.mustPut(t, r.trace, id, map[string]any{
			"metric": r.metricRef("health/heart_rate"), "source": r.sourceRef("garmin"), "form": "gauge",
			"unit": "/min", "at": at, "endsAt": ends, "utcOffsetStart": 180, "utcOffsetEnd": 180,
			"offsets": offsets, "values": values, "revision": rev,
		})
	}
	// 06:00 to 07:00, points across every bucket boundary of the hour and a
	// null in the middle; then 10:00 to 11:00, after three silent hours.
	hr("hr/06", "2026-10-07T06:00:00Z", "2026-10-07T07:00:00Z",
		[]any{0, 118000, 899000, 901000, 1500000, 1790000, 2700000, 3599999},
		[]any{"58", "57", "61", "63", nil, "112", "118", "96"}, 1)
	hr("hr/10", "2026-10-07T10:00:00Z", "2026-10-07T11:00:00Z",
		[]any{1000, 1800000}, []any{"60", "62"}, 1)
	// A held door, transitions in one bucket and across the next.
	for i, c := range []struct{ at, code string }{
		{"2026-10-07T08:01:00Z", "closed"},
		{"2026-10-07T08:04:00Z", "open"},
		{"2026-10-07T08:06:00Z", "closed"},
		{"2026-10-07T08:14:00Z", "open"},
		{"2026-10-07T08:31:00Z", "closed"},
	} {
		r.mustPut(t, r.observation, fmt.Sprintf("door/%d", i), map[string]any{
			"metric": r.metricRef("air_temperature"), "about": r.sourceRef("front-door"), "form": "gauge",
			"code": c.code, "hold": "PT1H", "at": c.at,
		})
	}
	// A meter that resets twice inside one bucket, read every two minutes.
	r.mustPut(t, r.trace, "meter/06", map[string]any{
		"metric": r.metricRef("energy"), "form": "counter", "unit": "kW.h", "resetRule": "monotonic",
		"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z", "every": "PT2M",
		"values": []any{"100.000", "100.250", "0.125", "0.500", "0.050", "0.300", "0.800", "1.100"},
	})
	r.mustPut(t, r.trace, "steps/06", map[string]any{
		"metric": r.metricRef("health/steps"), "form": "amount", "unit": "{steps}",
		"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z",
		"offsets": []any{0, 600000, 2700000}, "values": []any{"412", nil, "88"},
	})
	// Three heart rate buckets of the first hour (06:30 holds no point) and
	// two of the second, the door's two codes at 08:00 and one at 08:30, the
	// meter's one bucket and the steps' two.
	if n := checkRollupsMatchSpec(t, r.db); n != 11 {
		t.Fatalf("the points imply %d rollup rows, want 11", n)
	}

	// Hand-checked values of one row: the 06:00 bucket of the heart rate.
	// 58 holds to 06:01:58, 57 to 06:14:59, 61 to the end: 900000 ms held,
	// and the largest gap is 57's 781 seconds.
	var held, maxGap int64
	var twa float64
	if err := r.db.QueryRow(`SELECT held_ms, twa_sum, max_gap_ms FROM observed_rollups
		WHERE metric = $1 AND bucket15 = '2026-10-07T06:00:00Z'`, r.metric+"/health/heart_rate").Scan(&held, &twa, &maxGap); err != nil {
		t.Fatal(err)
	}
	if held != 900000 || maxGap != 781000 || twa != 58*118000+57*781000+61*1000 {
		t.Errorf("the 06:00 bucket: held %d max_gap %d twa_sum %v", held, maxGap, twa)
	}
	// The meter's 06:00 bucket: 0.25, a reset to 0.125, 0.375, a reset to
	// 0.05, then 0.25, 0.5 and 0.3: 1.85 over two resets, exact.
	var increase string
	var resets int
	if err := r.db.QueryRow(`SELECT increase::text, resets FROM observed_rollups
		WHERE metric = $1 AND bucket15 = '2026-10-07T06:00:00Z'`, r.metric+"/energy").Scan(&increase, &resets); err != nil {
		t.Fatal(err)
	}
	if !sameDecimal(increase, "1.85") || resets != 2 {
		t.Errorf("the meter's 06:00 bucket: increase %s resets %d, want 1.85 and 2", increase, resets)
	}

	// A correction, a deletion and a rebuild each leave the specification's
	// rows.
	hr("hr/06", "2026-10-07T06:00:00Z", "2026-10-07T07:00:00Z",
		[]any{0, 950000}, []any{"70", "71"}, 2)
	checkRollupsMatchSpec(t, r.db)
	if _, err := r.ds.Delete(context.Background(), substrate.ActorAPI, r.observation, "door/2", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	checkRollupsMatchSpec(t, r.db)
	before := observedTables(t, r.db)
	if _, err := r.svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := observedTables(t, r.db); after != before {
		var n []string
		rows, _ := r.db.Query(`SELECT record_id, count(*) FROM observed_points GROUP BY 1 ORDER BY 1`)
		for rows.Next() {
			var id string
			var c int
			_ = rows.Scan(&id, &c)
			n = append(n, fmt.Sprintf("%s=%d", id, c))
		}
		_ = rows.Close()
		t.Fatalf("the rebuild did not reproduce the observed tables (after: %v)\n%s", n, firstDifference([]byte(before), []byte(after)))
	}
	checkRollupsMatchSpec(t, r.db)
}

// observedTables renders the three tables whole, in key order.
func observedTables(t *testing.T, db *sql.DB) string {
	t.Helper()
	var b strings.Builder
	for _, q := range []string{
		`SELECT coalesce(jsonb_agg(to_jsonb(p) - 'repository' ORDER BY kind, record_id, ord), '[]')::text FROM observed_points p`,
		`SELECT coalesce(jsonb_agg(to_jsonb(r) - 'repository' ORDER BY metric, kind, source, about, channel, form, bucket15, code, unit, delta, utc_offset_min), '[]')::text FROM observed_rollups r`,
		`SELECT coalesce(jsonb_agg(to_jsonb(l) - 'repository' ORDER BY metric, kind, source, about, channel), '[]')::text FROM observed_latest l`,
	} {
		var doc string
		if err := db.QueryRow(q).Scan(&doc); err != nil {
			t.Fatalf("snapshot: %v", err)
		}
		b.WriteString(doc)
		b.WriteString("\n")
	}
	return b.String()
}

// latestRow is one stream's row of observed_latest.
type latestOf struct {
	ts       time.Time
	value    sql.NullString
	code     string
	recordID string
	revision sql.NullInt64
	points   int64
}

func (r obsIndex) latest(t *testing.T, metric, source string) (latestOf, bool) {
	t.Helper()
	var l latestOf
	err := r.db.QueryRow(`SELECT ts, value::text, code, record_id, revision, points FROM observed_latest
		WHERE metric = $1 AND source = $2`, r.metric+"/"+metric, source).
		Scan(&l.ts, &l.value, &l.code, &l.recordID, &l.revision, &l.points)
	if err == sql.ErrNoRows {
		return l, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return l, true
}

// A rewrite with a higher revision replaces the record's points, its
// buckets' rows and its stream's latest row in the write's transaction, and
// a stale rewrite moves none of them.
func TestObservedRewriteReplacesPointsRollupsAndLatest(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	trace := func(values []any, rev int) error {
		_, err := r.put(t, r.trace, "hr/06", map[string]any{
			"metric": r.metricRef("health/heart_rate"), "source": r.sourceRef("garmin"), "form": "gauge",
			"unit": "/min", "at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z",
			"every": "PT10M", "values": values, "revision": rev,
		})
		return err
	}
	if err := trace([]any{"58", "57", "59", "61", "60", "62"}, 1); err != nil {
		t.Fatal(err)
	}
	l, ok := r.latest(t, "health/heart_rate", r.source+"/garmin")
	if !ok || l.value.String != "62" || l.points != 6 || !l.ts.Equal(mustTime(t, "2026-10-07T06:50:00Z")) {
		t.Fatalf("latest after the write = %+v", l)
	}
	if err := trace([]any{"70", "71", "72"}, 2); err != nil {
		t.Fatal(err)
	}
	if got := r.points(t, r.trace, "hr/06"); len(got) != 3 || got[2].exact.String != "72" || got[0].revision.Int64 != 2 {
		t.Errorf("points after the rewrite = %+v", got)
	}
	l, _ = r.latest(t, "health/heart_rate", r.source+"/garmin")
	if l.value.String != "72" || l.points != 3 || !l.ts.Equal(mustTime(t, "2026-10-07T06:20:00Z")) || l.revision.Int64 != 2 {
		t.Errorf("latest after the rewrite = %+v", l)
	}
	if n := r.count(t, `SELECT count(*) FROM observed_rollups WHERE bucket15 >= '2026-10-07T06:30:00Z'`); n != 0 {
		t.Errorf("%d rollup rows survive for buckets the rewrite emptied", n)
	}
	checkRollupsMatchSpec(t, r.db)
	snapshot := observedTables(t, r.db)
	err := trace([]any{"1", "2"}, 1)
	refused(t, "a stale rewrite", err, "stale delivery")
	if got := observedTables(t, r.db); got != snapshot {
		t.Error("a refused rewrite moved the observed index")
	}
}

// A tombstone removes the record's points and recomputes its buckets and its
// stream's latest row from what remains; a put onto the tombstone restores
// them; a purge leaves what the tombstone left.
func TestObservedTombstoneRestoreAndPurge(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	ctx := context.Background()
	hour := func(id, at, ends string, values []any) map[string]any {
		return map[string]any{
			"metric": r.metricRef("health/heart_rate"), "source": r.sourceRef("garmin"), "form": "gauge",
			"unit": "/min", "at": at, "endsAt": ends, "every": "PT20M", "values": values,
		}
	}
	six := hour("hr/06", "2026-10-07T06:00:00Z", "2026-10-07T07:00:00Z", []any{"58", "57", "59"})
	seven := hour("hr/07", "2026-10-07T07:00:00Z", "2026-10-07T08:00:00Z", []any{"61", "60", "62"})
	r.mustPut(t, r.trace, "hr/06", six)
	r.mustPut(t, r.trace, "hr/07", seven)
	whole := observedTables(t, r.db)
	if l, _ := r.latest(t, "health/heart_rate", r.source+"/garmin"); l.recordID != "hr/07" || l.points != 6 {
		t.Fatalf("latest = %+v", l)
	}

	if _, err := r.ds.Delete(ctx, substrate.ActorAPI, r.trace, "hr/07", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if got := r.points(t, r.trace, "hr/07"); len(got) != 0 {
		t.Errorf("a tombstone kept %d points", len(got))
	}
	if n := r.count(t, `SELECT count(*) FROM observed_rollups WHERE bucket15 >= '2026-10-07T07:00:00Z'`); n != 0 {
		t.Errorf("%d rollup rows survive the tombstone", n)
	}
	if l, _ := r.latest(t, "health/heart_rate", r.source+"/garmin"); l.recordID != "hr/06" || l.value.String != "59" || l.points != 3 {
		t.Errorf("latest after the tombstone = %+v", l)
	}
	checkRollupsMatchSpec(t, r.db)

	r.mustPut(t, r.trace, "hr/07", seven)
	if got := observedTables(t, r.db); got != whole {
		t.Errorf("the restore did not bring the index back\n%s", firstDifference([]byte(whole), []byte(got)))
	}

	if _, err := r.ds.Delete(ctx, substrate.ActorAPI, r.trace, "hr/06", substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatal(err)
	}
	if n := r.count(t, `SELECT count(*) FROM records WHERE kind = $1 AND id = 'hr/06'`, r.trace); n != 0 {
		t.Fatal("the purge left the row")
	}
	if got := r.points(t, r.trace, "hr/06"); len(got) != 0 {
		t.Errorf("a purge kept %d points", len(got))
	}
	if l, _ := r.latest(t, "health/heart_rate", r.source+"/garmin"); l.recordID != "hr/07" || l.points != 3 {
		t.Errorf("latest after the purge = %+v", l)
	}
	checkRollupsMatchSpec(t, r.db)
	if _, err := r.ds.Delete(ctx, substrate.ActorAPI, r.trace, "hr/07", substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if _, ok := r.latest(t, "health/heart_rate", r.source+"/garmin"); ok {
		t.Error("a stream with no point kept its latest row")
	}
	if n := r.count(t, `SELECT count(*) FROM observed_rollups WHERE metric = $1`, r.metric+"/health/heart_rate"); n != 0 {
		t.Errorf("%d rollup rows survive their last point", n)
	}
}

// observed_latest breaks a tie at one instant as `last` does, by record id
// and never by revision, a counter of one record's rewrites; and a
// correction that moves the newest point to another stream recomputes both
// streams.
func TestObservedLatestTieBreakAndAMovedPoint(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	weigh := func(id, source, at, value string, rev int) {
		r.mustPut(t, r.observation, id, map[string]any{
			"metric": r.metricRef("health/body_mass"), "source": r.sourceRef(source), "form": "gauge",
			"value": quantity(value, "kg"), "at": at, "revision": rev,
		})
	}
	lastOfRow := func() string {
		t.Helper()
		var id string
		if err := r.db.QueryRow(`SELECT last_record_id FROM observed_rollups WHERE metric = $1 AND source = $2`,
			r.metric+"/health/body_mass", r.source+"/scale").Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	const at = "2026-10-07T06:00:00Z"
	weigh("a", "scale", at, "72.0", 2)
	weigh("b", "scale", at, "72.5", 1)
	l, _ := r.latest(t, "health/body_mass", r.source+"/scale")
	if l.recordID != "b" || l.points != 2 {
		t.Errorf("the latest row went to %q, want b, the greater record id, whatever the revisions: %+v", l.recordID, l)
	}
	if last := lastOfRow(); last != l.recordID {
		t.Errorf("the rollup row's last point is %q and the latest row's %q", last, l.recordID)
	}
	weigh("c", "scale", at, "73.0", 2)
	if l, _ = r.latest(t, "health/body_mass", r.source+"/scale"); l.recordID != "c" || l.points != 3 {
		t.Errorf("the tie went to %q, want the greater record id c", l.recordID)
	}

	weigh("x", "scale", "2026-10-08T06:00:00Z", "71.0", 1)
	if l, _ = r.latest(t, "health/body_mass", r.source+"/scale"); l.recordID != "x" {
		t.Fatalf("the newest point is not the latest: %+v", l)
	}
	if _, err := r.patch(t, r.observation, "x", map[string]any{"source": r.sourceRef("other-scale")}); err != nil {
		t.Fatal(err)
	}
	if l, _ = r.latest(t, "health/body_mass", r.source+"/scale"); l.recordID != "c" || l.points != 3 {
		t.Errorf("the old stream after the move = %+v", l)
	}
	if l, ok := r.latest(t, "health/body_mass", r.source+"/other-scale"); !ok || l.recordID != "x" || l.points != 1 {
		t.Errorf("the new stream after the move = %+v", l)
	}
	checkRollupsMatchSpec(t, r.db)
}

// Points at one instant are ordered by record id in byte order on every path
// (observed.Before): the rollup row's first and last point, the gauge's held
// time and the latest row, whatever collation the database sorts text in. The
// ids differ in case and in punctuation, which en_US orders the other way.
func TestObservedTiesAtOneInstantFollowByteOrder(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	ids := []string{"tie/a1", "tie/B1", "tie/b-2", "tie/b1"}
	for i, id := range ids {
		r.mustPut(t, r.observation, id, map[string]any{
			"metric": r.metricRef("air_temperature"), "source": r.sourceRef("probe"), "form": "gauge",
			"value": quantity(fmt.Sprintf("2%d", i), "Cel"), "at": "2026-10-07T06:00:00Z",
		})
	}
	byBytes := append([]string(nil), ids...)
	sort.Strings(byBytes)
	first, last := byBytes[0], byBytes[len(byBytes)-1]
	var dbFirst, dbLast string
	if err := r.db.QueryRow(`SELECT min(record_id), max(record_id) FROM observed_points WHERE record_id LIKE 'tie/%'`).
		Scan(&dbFirst, &dbLast); err != nil {
		t.Fatal(err)
	}
	t.Logf("the database orders the ids from %s to %s; byte order from %s to %s", dbFirst, dbLast, first, last)
	if l, ok := r.latest(t, "air_temperature", r.source+"/probe"); !ok || l.recordID != last {
		t.Errorf("the latest row is %q (%v), want %q, the greatest id in byte order", l.recordID, ok, last)
	}
	var rowFirst, rowLast string
	if err := r.db.QueryRow(`SELECT first_record_id, last_record_id FROM observed_rollups WHERE metric = $1`,
		r.metric+"/air_temperature").Scan(&rowFirst, &rowLast); err != nil {
		t.Fatal(err)
	}
	if rowFirst != first || rowLast != last {
		t.Errorf("the rollup row runs from %q to %q, want %q to %q in byte order", rowFirst, rowLast, first, last)
	}
	checkRollupsMatchSpec(t, r.db)
}

// A kind that starts binding `observed` derives its existing rows' points
// behind the apply that bound it, and one that stops drops them.
func TestObservedIndexFollowsTheBinding(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	ctx := context.Background()
	pkg := testdb.Repository(t) + "/readings"
	closure := func(bound bool) []map[string]any {
		traits := []any{"substrate.reamde.dev/core/temporal(range)"}
		if bound {
			traits = append(traits, "substrate.reamde.dev/core/observed(single)")
		}
		props := map[string]any{}
		for name, typ := range map[string]string{
			"channel": "string", "form": "string", "code": "string", "statistic": "string", "period": "string",
			"hold": "duration", "resetRule": "string", "resetAt": "datetime", "delta": "bool", "circular": "bool",
			"wrapAt": "decimal", "utcOffset": "int", "revision": "int", "value": "quantity",
		} {
			props[name] = map[string]any{"type": typ}
		}
		for _, name := range []string{"metric", "source", "about"} {
			props[name] = map[string]any{"type": "reference", "kind": "any"}
		}
		return []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "reading"},
				map[string]any{"traits": traits, "properties": props}),
		}
	}
	actor := substrate.BundleActor(vocabulary.SplitPackageRef(pkg))
	if _, err := r.ds.ApplyVocabularyDocuments(ctx, actor, closure(false)); err != nil {
		t.Fatalf("declare the unbound kind: %v", err)
	}
	r.mustPut(t, pkg+"/reading", "r1", map[string]any{
		"metric": r.metricRef("air_temperature"), "form": "gauge", "value": quantity("21.5", "Cel"),
		"at": "2026-10-07T06:00:00Z",
	})
	if got := r.points(t, pkg+"/reading", "r1"); len(got) != 0 {
		t.Fatalf("an unbound kind derived %d points", len(got))
	}
	if _, err := r.ds.ApplyVocabularyDocuments(ctx, actor, closure(true)); err != nil {
		t.Fatalf("bind the kind: %v", err)
	}
	engine.DrainIndexReprojection(t, r.ds)
	if got := r.points(t, pkg+"/reading", "r1"); len(got) != 1 || got[0].exact.String != "21.5" {
		t.Fatalf("binding the kind derived %+v", got)
	}
	if l, ok := r.latest(t, "air_temperature", ""); !ok || l.recordID != "r1" {
		t.Errorf("binding the kind left no latest row: %+v", l)
	}
	checkRollupsMatchSpec(t, r.db)
	if _, err := r.ds.ApplyVocabularyDocuments(ctx, actor, closure(false)); err != nil {
		t.Fatalf("unbind the kind: %v", err)
	}
	engine.DrainIndexReprojection(t, r.ds)
	if got := r.points(t, pkg+"/reading", "r1"); len(got) != 0 {
		t.Errorf("unbinding the kind kept %d points", len(got))
	}
	if _, ok := r.latest(t, "air_temperature", ""); ok {
		t.Error("unbinding the kind kept its latest row")
	}
	checkRollupsMatchSpec(t, r.db)
}

// A repository directory imported at boot folds its observed index the way
// the live writes left it.
func TestBootImportReproducesTheObservedIndex(t *testing.T) {
	t.Parallel()
	svc, ds, _ := newDatasetWithDSN(t)
	writeObservedHistory(t, ds)
	before := foldOf(t, ds)
	if !strings.Contains(string(before), `"observed_rollups": [`) || strings.Contains(string(before), `"observed_points": []`) {
		t.Fatal("the fold holds no observed rows; the import would prove nothing")
	}
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	svc2 := mustReopen(t, engine.MigratedDSN(t), copyRepositoryDir(t, root, id))
	ds2, err := svc2.Dataset(context.Background(), testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the imported repository: %v", err)
	}
	engine.DrainIndexReprojection(t, ds2)
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the imported fold is not the original\n%s", firstDifference(before, after))
	}
}

// --- the shared history ---

// The observations sample's own kinds, as importVocabulary installs them.
const (
	obsSampleObservation = "samples.substrate.reamde.dev/observations/observation"
	obsSampleTrace       = "samples.substrate.reamde.dev/observations/trace"
	obsSampleMetric      = "samples.substrate.reamde.dev/observations/metric"
)

// writeObservedHistory imports the observations sample and writes one of
// every observed shape the index derives, then moves them: a trace of a
// gauge with a null and an offset change, its rewrite at a higher revision,
// a counter trace, an amount with its category, a summary, coded gauges, a
// tombstone, a restore and a purge. The rebuild and refold tests fold it.
func writeObservedHistory(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	importVocabulary(t, ds, "observations")
	ref := func(id string) map[string]any { return map[string]any{"ref": obsSampleMetric + "/" + id} }
	put := func(kind, id string, props map[string]any) {
		t.Helper()
		mustPut(t, ds, owner, substrate.PutInput{Kind: kind, ID: id, Properties: props})
	}
	temperature := func(values []any, rev int) map[string]any {
		return map[string]any{
			"metric": ref("air_temperature"), "form": "gauge", "unit": "Cel", "hold": "PT20M",
			"at": "2026-10-25T00:00:00Z", "endsAt": "2026-10-25T01:00:00Z", "utcOffsetStart": 180, "utcOffsetEnd": 120,
			"offsets": []any{0, 118000, 899000, 1500000, 3000000}, "values": values, "revision": rev,
		}
	}
	put(obsSampleTrace, "sensor/kitchen/2026-10-25T00", temperature([]any{"21.5", "21.7", nil, "22.0", "22.4"}, 1))
	put(obsSampleTrace, "sensor/kitchen/2026-10-25T00", temperature([]any{"21.5", "21.8", "21.9", nil, "22.3"}, 2))
	put(obsSampleTrace, "meter/main/2026-10-25T00", map[string]any{
		"metric": ref("energy"), "form": "counter", "unit": "kW.h", "resetRule": "monotonic",
		"at": "2026-10-25T00:00:00Z", "endsAt": "2026-10-25T02:00:00Z", "every": "PT15M",
		"values": []any{"18342.655", "18342.700", "0.010", "0.050", "0.400", "0.900", "0.020", "0.090"},
	})
	put(obsSampleObservation, "plug/kettle/2026-10-25", map[string]any{
		"metric": ref("energy"), "form": "amount", "value": quantity("1.2", "kW.h"), "code": "kettle",
		"at": "2026-10-25T07:00:00Z", "endsAt": "2026-10-25T08:00:00Z",
	})
	put(obsSampleObservation, "plug/daily/2026-10-25", map[string]any{
		"metric": ref("power"), "form": "summary", "statistic": "avg", "period": "day",
		"value": quantity("340", "W"), "at": "2026-10-25T00:00:00Z", "endsAt": "2026-10-26T00:00:00Z",
	})
	for i, code := range []string{"closed", "open", "closed", "open"} {
		put(obsSampleObservation, fmt.Sprintf("door/front/%d", i), map[string]any{
			"metric": ref("illuminance"), "channel": "contact", "form": "gauge", "code": code,
			"at": time.Date(2026, 10, 25, 8, 3*i+1, 0, 0, time.UTC).Format(time.RFC3339),
		})
	}
	if _, err := ds.Delete(ctx, owner, obsSampleObservation, "door/front/1", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, obsSampleObservation, "door/front/2", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	put(obsSampleObservation, "door/front/2", map[string]any{
		"metric": ref("illuminance"), "channel": "contact", "form": "gauge", "code": "closed",
		"at": "2026-10-25T08:07:00Z",
	})
	if _, err := ds.Delete(ctx, owner, obsSampleObservation, "door/front/3", substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
}

// TestRebuildReproducesTheObservedTables: a rebuild clears the three tables
// and derives them again from the changelog, every column of every row, for
// a history that rewrites, deletes, restores and purges observed records.
func TestRebuildReproducesTheObservedTables(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeObservedHistory(t, ds)
	db := rawDB(t, dsn)
	before := observedTables(t, db)
	if strings.Contains(before, "\n[]\n") || strings.HasPrefix(before, "[]") {
		t.Fatalf("an observed table is empty; the rebuild would prove nothing:\n%s", before)
	}
	// Garbage in all three, so a rebuild that kept anything shows it.
	for _, table := range []string{"observed_points", "observed_rollups", "observed_latest"} {
		if _, err := db.Exec(`UPDATE ` + table + ` SET metric = metric || '/stale'`); err != nil {
			t.Fatalf("spoil %s: %v", table, err)
		}
	}
	if _, err := svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := observedTables(t, db); after != before {
		t.Fatalf("the rebuilt observed tables are not the live ones\n%s", firstDifference([]byte(before), []byte(after)))
	}
	checkRollupsMatchSpec(t, db)
}

// TestObservedRollupRowsAlwaysCarryTheirDetail: every rollup row the fold
// writes carries what a read served from it needs to answer as the raw
// points do: the record id and index of its first and last point, the
// stored digits of its smallest and largest number, an amount's span end,
// and an increase only where it holds a step (a counter's first reading has
// none before it, which is no answer, not a change of zero). The table
// refuses a row without them, so no row a read meets lacks them.
func TestObservedRollupRowsAlwaysCarryTheirDetail(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	start := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	for i, v := range []string{"10.50", "12.25"} {
		r.mustPut(t, r.observation, fmt.Sprintf("meter/%d", i), map[string]any{
			"metric": r.metricRef("energy"), "source": r.sourceRef("meter"), "form": "counter", "resetRule": "monotonic",
			"value": quantity(v, "kW.h"), "at": start.Add(time.Duration(i) * time.Hour).Format(time.RFC3339),
		})
	}
	r.mustPut(t, r.observation, "steps/1", map[string]any{
		"metric": r.metricRef("health/steps"), "source": r.sourceRef("phone"), "form": "amount",
		"value": quantity("9007199254740993", "{steps}"),
		"at":    start.Format(time.RFC3339), "endsAt": start.Add(5 * time.Minute).Format(time.RFC3339),
	})
	r.mustPut(t, r.observation, "door/1", map[string]any{
		"metric": r.metricRef("air_temperature"), "source": r.sourceRef("contact"), "form": "gauge", "code": "open",
		"at": start.Format(time.RFC3339),
	})
	r.mustPut(t, r.observation, "air/1", map[string]any{
		"metric": r.metricRef("air_temperature"), "source": r.sourceRef("thermometer"), "form": "gauge",
		"value": quantity("21.10", "Cel"), "at": start.Format(time.RFC3339),
	})
	rows := storedRollups(t, r.db)
	if len(rows) < 5 {
		t.Fatalf("%d rollup rows, want one per stream and bucket written", len(rows))
	}
	steps := 0
	for key, g := range rows {
		form := strings.SplitN(strings.Split(key, "|")[5], "#", 2)[0]
		if g.firstTS.Valid != (g.firstRecord.Valid && g.firstOrd.Valid) || g.lastTS.Valid != (g.lastRecord.Valid && g.lastOrd.Valid) {
			t.Errorf("%s: first/last %v/%v without their record id and index %v %v %v %v", key, g.firstTS.Valid, g.lastTS.Valid,
				g.firstRecord, g.firstOrd, g.lastRecord, g.lastOrd)
		}
		if g.min.Valid != g.minValue.Valid || g.max.Valid != g.maxValue.Valid {
			t.Errorf("%s: min/max %v/%v without their digits %v/%v", key, g.min, g.max, g.minValue, g.maxValue)
		}
		if g.spanEnd.Valid != (form == vocabulary.FormAmount && g.n > 0) {
			t.Errorf("%s: span_end %v on a %s row of %d points", key, g.spanEnd, form, g.n)
		}
		if form == vocabulary.FormAmount && g.minValue.String != "9007199254740993" {
			t.Errorf("%s: min_value %v, want the stored digits 9007199254740993", key, g.minValue)
		}
		if form != vocabulary.FormCounter {
			if g.increase.Valid {
				t.Errorf("%s: a %s row has an increase %v", key, form, g.increase)
			}
			continue
		}
		switch {
		case g.firstTS.Time.Equal(start) && g.increase.Valid:
			t.Errorf("%s: the counter's first reading has no reading before it, and its row has increase %v, want null", key, g.increase)
		case g.firstTS.Time.Equal(start.Add(time.Hour)) && (!g.increase.Valid || !sameDecimal(g.increase.String, "1.75")):
			t.Errorf("%s: the second reading's row has increase %v, want 1.75", key, g.increase)
		}
		if g.increase.Valid {
			steps++
		}
	}
	if steps != 1 {
		t.Errorf("%d counter rows hold a step, want the second reading's alone", steps)
	}
	// The table refuses a row missing any of it.
	repo := testdb.Repository(t)
	for _, x := range []struct{ what, columns, values string }{
		{"a number without its digits", `n, bounded, sum, min, max, covered_ms`, `1, 0, 1, 1, 1, 0`},
		{"a first and last point without their record", `n, bounded, first_ts, last_ts, covered_ms`, `1, 0, now(), now(), 0`},
		{
			"an amount without its span end", `n, bounded, first_ts, first_record_id, first_ord, last_ts, last_record_id, last_ord, covered_ms`,
			`1, 0, now(), 'x', 0, now(), 'x', 0, 0`,
		},
		{"an increase on a gauge", `n, bounded, increase, covered_ms`, `0, 0, 0, 0`},
	} {
		form := vocabulary.FormGauge
		if strings.Contains(x.what, "amount") {
			form = vocabulary.FormAmount
		}
		_, err := r.db.Exec(`INSERT INTO observed_rollups (repository, metric, kind, source, about, channel, form, code, unit, delta,
			utc_offset_min, bucket15, max_gap_ms, start_gap_ms, `+x.columns+`)
			VALUES ($1, $3, 'k', '', '', '', $2, '', '', false, 0, '2026-01-01T00:00:00Z', 0, 0, `+x.values+`)`, repo, form, x.what)
		if err == nil || !strings.Contains(err.Error(), "23514") {
			t.Errorf("a row with %s: err = %v, want a check violation (23514)", x.what, err)
		}
	}
}

// TestVerifyAndExportReadNoObservedTable: verify and the export read the
// changelog files and never the observed index, so an emptied index neither
// fails verify nor travels in the archive, and a server restoring the archive
// derives the index again.
func TestVerifyAndExportReadNoObservedTable(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	writeObservedHistory(t, ds)
	db := rawDB(t, dsn)
	want := observedTables(t, db)
	for _, table := range []string{"observed_points", "observed_rollups", "observed_latest"} {
		if _, err := db.Exec(`DELETE FROM ` + table); err != nil {
			t.Fatalf("empty %s: %v", table, err)
		}
	}
	if report := mustVerify(t, svc, testdb.Repository(t)); !report.OK {
		t.Fatalf("verify failed with the observed index emptied: %+v", report)
	}
	root, _, _ := exportToRoot(t, ds)
	dsn2 := engine.MigratedDSN(t)
	svc2 := mustReopen(t, dsn2, root)
	ds2, err := svc2.Dataset(context.Background(), testdb.Repository(t))
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	engine.DrainIndexReprojection(t, ds2)
	if got := observedTables(t, rawDB(t, dsn2)); got != want {
		t.Fatalf("the restored observed index is not the original\n%s", firstDifference([]byte(want), []byte(got)))
	}
}

// --- the review's regressions ---

// packedClosure declares one trace kind, `<pkg>/reading`, binding
// observed(packed) or binding nothing, so a test can store a row the write
// rules never saw and bind the kind over it, or take the binding away.
func packedClosure(pkg string, bound bool) []map[string]any {
	traits := []any{"substrate.reamde.dev/core/temporal(range)"}
	if bound {
		traits = append(traits, "substrate.reamde.dev/core/observed(packed)")
	}
	props := map[string]any{}
	for name, typ := range map[string]string{
		"channel": "string", "form": "string", "unit": "string", "every": "duration", "hold": "duration",
		"resetRule": "string", "resetAt": "datetime", "delta": "bool", "circular": "bool", "wrapAt": "decimal",
		"utcOffsetStart": "int", "utcOffsetEnd": "int", "revision": "int", "stats": "json", "quality": "string",
	} {
		props[name] = map[string]any{"type": typ}
	}
	for name, typ := range map[string]string{"offsets": "int", "values": "decimal", "codes": "string"} {
		props[name] = map[string]any{"type": typ, "repeated": true}
	}
	for _, name := range []string{"metric", "source", "about"} {
		props[name] = map[string]any{"type": "reference", "kind": "any"}
	}
	return []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.ActorManifest(pkg, vocabulary.PackageActor(pkg)),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "reading"},
			map[string]any{"traits": traits, "properties": props}),
	}
}

func (r obsIndex) applyPackedClosure(t *testing.T, pkg string, bound bool) {
	t.Helper()
	actor := substrate.BundleActor(vocabulary.SplitPackageRef(pkg))
	if _, err := r.ds.ApplyVocabularyDocuments(context.Background(), actor, packedClosure(pkg, bound)); err != nil {
		t.Fatalf("apply the reading kind (bound %v): %v", bound, err)
	}
}

// rebuildMatches rebuilds the repository and holds the three tables to what
// they were before it, and the rollups to the specification.
func (r obsIndex) rebuildMatches(t *testing.T) {
	t.Helper()
	before := observedTables(t, r.db)
	if _, err := r.svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := observedTables(t, r.db); after != before {
		t.Fatalf("the live observed tables are not the rebuilt ones\n%s", firstDifference([]byte(before), []byte(after)))
	}
	checkRollupsMatchSpec(t, r.db)
}

// An empty code is refused on every write, and a row stored before that rule
// reads its empty code as an unavailable point on every path: the points a
// derivation inserts, the count a deletion takes back, the rollups, the latest
// row and the rebuild. Unrelated patches leave the live tables the rebuilt
// ones; before, each re-derivation of such a row added one point to its
// stream's count.
func TestObservedEmptyCodeIsReadOnceAsUnavailable(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	door := func(codes []any) map[string]any {
		return map[string]any{
			"metric": r.metricRef("illuminance"), "source": r.sourceRef("hall"), "channel": "contact", "form": "gauge",
			"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z", "every": "PT10M", "codes": codes,
		}
	}
	_, err := r.put(t, r.trace, "door/hall", door([]any{"open", ""}))
	refused(t, "a trace with an empty code", err, "props.codes[1]: a code is not empty")
	_, err = r.put(t, r.observation, "door/hall/1", map[string]any{
		"metric": r.metricRef("illuminance"), "form": "gauge", "code": "", "at": "2026-10-07T06:00:00Z",
	})
	refused(t, "an observation with an empty code", err, "props.code: a code is not empty")

	pkg := testdb.Repository(t) + "/doors"
	r.applyPackedClosure(t, pkg, false)
	r.mustPut(t, pkg+"/reading", "d1", door([]any{"open", "", "closed"}))
	r.applyPackedClosure(t, pkg, true)
	engine.DrainIndexReprojection(t, r.ds)
	got := r.points(t, pkg+"/reading", "d1")
	if len(got) != 3 || got[1].quality.String != "unavailable" || got[0].quality.Valid {
		t.Fatalf("the stored empty code derived %+v, want its point unavailable", got)
	}
	// Re-derive the row again: unbind and bind, each a deletion and an
	// insertion of its points in one transaction.
	r.applyPackedClosure(t, pkg, false)
	r.applyPackedClosure(t, pkg, true)
	engine.DrainIndexReprojection(t, r.ds)
	if _, err := r.patch(t, pkg+"/reading", "d1", map[string]any{"quality": "valid"}); err == nil {
		t.Error("a patch kept the stored empty code")
	}

	// Unrelated patches of a trace whose null is a legitimate unavailable
	// point, each a deletion and an insertion of its points.
	r.mustPut(t, r.trace, "door/porch", map[string]any{
		"metric": r.metricRef("illuminance"), "source": r.sourceRef("porch"), "channel": "contact", "form": "gauge",
		"at": "2026-10-07T06:00:00Z", "endsAt": "2026-10-07T07:00:00Z", "every": "PT10M",
		"codes": []any{"open", nil, "closed"},
	})
	for _, q := range []string{"valid", "estimated", "valid"} {
		if _, err := r.patch(t, r.trace, "door/porch", map[string]any{"quality": q}); err != nil {
			t.Fatalf("patch quality %s: %v", q, err)
		}
	}
	for _, source := range []string{"hall", "porch"} {
		if l, ok := r.latest(t, "illuminance", r.source+"/"+source); !ok || l.points != 2 {
			t.Errorf("the %s stream counts %d points (%v), want its two available ones", source, l.points, ok)
		}
	}
	r.rebuildMatches(t)
}

// A record that leaves the fold takes its points with it whatever its kind's
// declaration now says. Removing the binding leaves old points for the
// background pass to drop row by row, and the pass is held here while one
// record is purged and another tombstoned: the purged row is gone from
// `records` before the pass could find it, so its points, its buckets' rows
// and its stream's latest row went only if the purge took them. Once the pass
// is let go and drained, the three tables are a rebuild's.
func TestObservedPurgeAfterTheBindingGoesLeavesNoPoints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var armed atomic.Bool
	release := make(chan struct{})
	held := make(chan struct{}, 1)
	var pkg string
	r := openObservedIndex(t, engine.WithTestReprojectionHook(func(ctx context.Context, kind string) error {
		if !armed.Load() || kind != pkg+"/reading" {
			return nil
		}
		select {
		case held <- struct{}{}:
		default:
		}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}))
	pkg = testdb.Repository(t) + "/meters"
	kind := pkg + "/reading"
	r.applyPackedClosure(t, pkg, true)
	engine.DrainIndexReprojection(t, r.ds)
	hour := func(at string, values []any) map[string]any {
		start := mustTime(t, at)
		return map[string]any{
			"metric": r.metricRef("air_temperature"), "source": r.sourceRef("attic"), "form": "gauge", "unit": "Cel",
			"at": at, "endsAt": start.Add(time.Hour).Format(time.RFC3339), "every": "PT20M", "values": values,
		}
	}
	r.mustPut(t, kind, "h06", hour("2026-10-07T06:00:00Z", []any{"20.5", "20.7", "20.9"}))
	r.mustPut(t, kind, "h07", hour("2026-10-07T07:00:00Z", []any{"21.5", "21.7", "21.9"}))
	r.mustPut(t, kind, "h08", hour("2026-10-07T08:00:00Z", []any{"22.5", nil, "22.9"}))
	if l, ok := r.latest(t, "air_temperature", r.source+"/attic"); !ok || l.recordID != "h08" {
		t.Fatalf("the stream's latest row is %+v (%v), want h08's", l, ok)
	}

	armed.Store(true)
	r.applyPackedClosure(t, pkg, false)
	select {
	case <-held:
	case <-time.After(time.Minute):
		t.Fatal("the pass behind the unbinding never reached the kind")
	}
	if _, err := r.ds.Delete(ctx, owner, kind, "h08", substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge h08: %v", err)
	}
	if _, err := r.ds.Delete(ctx, owner, kind, "h07", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete h07: %v", err)
	}
	for _, id := range []string{"h07", "h08"} {
		if got := r.points(t, kind, id); len(got) != 0 {
			t.Errorf("%s left the fold and kept %d points while the pass was held", id, len(got))
		}
	}
	if l, ok := r.latest(t, "air_temperature", r.source+"/attic"); !ok || l.recordID != "h06" || l.points != 3 {
		t.Errorf("with h07 and h08 gone the stream's latest row is %+v (%v), want h06's three points", l, ok)
	}

	close(release)
	engine.DrainIndexReprojection(t, r.ds)
	if n := r.count(t, `SELECT count(*) FROM observed_points WHERE kind = $1`, kind); n != 0 {
		t.Errorf("the unbound kind kept %d points after the pass", n)
	}
	if _, ok := r.latest(t, "air_temperature", r.source+"/attic"); ok {
		t.Error("the unbound kind's stream kept its latest row after the pass")
	}
	r.rebuildMatches(t)
}

// pointReads is a pgx tracer that keeps the most rows any one statement
// reading observed_points answered: what a recompute held in memory at once.
type pointReads struct {
	mu  sync.Mutex
	max int64
	sql string
}

type pointReadKey struct{}

func (p *pointReads) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	return context.WithValue(ctx, pointReadKey{}, d.SQL)
}

func (p *pointReads) TraceQueryEnd(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryEndData) {
	q, _ := ctx.Value(pointReadKey{}).(string)
	if !strings.Contains(q, "FROM observed_points") || !d.CommandTag.Select() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if n := d.CommandTag.RowsAffected(); n > p.max {
		p.max, p.sql = n, q
	}
}

func (p *pointReads) most() (int64, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.max, p.sql
}

// A rollup recompute reads a bucket's points in bounded pages, in (ts,
// record_id, ord) order with a running state, never the bucket whole. Five
// overlapping gauge traces and five overlapping counter traces put 200
// points each in one 15-minute bucket of their streams; with a page of 50,
// no read of points the engine sends answers more than 50 rows through the
// writes, a patch and a rebuild, and the rows are the specification's and
// the rebuild's. Before, a write read the whole bucket and a rebuild doubled
// its page until the bucket fit.
func TestObservedRollupRecomputeReadsBoundedPages(t *testing.T) {
	t.Parallel()
	const page = 50
	reads := &pointReads{}
	r := openObservedIndex(t, engine.WithTestObservedPageSize(page), engine.WithTestQueryTracer(reads))
	for i := range 5 {
		gauge := make([]any, 40)
		counter := make([]any, 40)
		for j := range gauge {
			gauge[j] = fmt.Sprintf("%d.%d", 20+(i*7+j*3)%9, j%10)
			counter[j] = fmt.Sprintf("%d.%03d", 1000*i+j*5, j)
		}
		gauge[17] = nil
		at := fmt.Sprintf("2026-10-07T06:00:%02dZ", i)
		r.mustPut(t, r.trace, fmt.Sprintf("attic/%d", i), map[string]any{
			"metric": r.metricRef("air_temperature"), "source": r.sourceRef("attic"), "form": "gauge", "unit": "Cel",
			"hold": "PT1M", "at": at, "endsAt": "2026-10-07T06:15:00Z", "every": "PT18S", "values": gauge,
		})
		r.mustPut(t, r.trace, fmt.Sprintf("meter/%d", i), map[string]any{
			"metric": r.metricRef("energy"), "source": r.sourceRef("meter"), "form": "counter", "unit": "kW.h",
			"resetRule": "monotonic", "at": at, "endsAt": "2026-10-07T06:15:00Z", "every": "PT18S", "values": counter,
		})
	}
	if _, err := r.patch(t, r.trace, "attic/2", map[string]any{"quality": "valid"}); err != nil {
		t.Fatalf("patch: %v", err)
	}
	if n := r.count(t, `SELECT count(*) FROM observed_points WHERE ts >= '2026-10-07T06:00:00Z' AND ts < '2026-10-07T06:15:00Z'`); n != 400 {
		t.Fatalf("the bucket holds %d points, want 400", n)
	}
	checkRollupsMatchSpec(t, r.db)
	r.rebuildMatches(t)
	if most, q := reads.most(); most > page {
		t.Fatalf("one read of observed points answered %d rows, above the page of %d:\n%s", most, page, q)
	}
}
