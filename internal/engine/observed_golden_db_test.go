package engine_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

var updateObservedGolden = flag.Bool("update-observed-golden", false,
	"rewrite testdata/observed_golden.json from what this binary derives")

// observedGolden is where the golden corpus's derived rows are kept.
const observedGolden = "testdata/observed_golden.json"

// The observed index derives what testdata/observed_golden.json holds from
// a fixed corpus: every form, single and packed, offsets known, unknown and
// changing (a clock change, a +05:30 trip), codes, two units of one measure,
// nulls, a censored lab value, counters that reset and wrap, an angle, a
// temperature difference, holds, and a metric that names its own tiers. The
// file holds the segments, the points they derive, every tier's rollup rows
// and the latest rows. A change to what any of those derive changes what a
// live repository holds against what a rebuild derives, so the change must
// bump observedIndexVersion, which re-derives every repository at its next
// open; this test is what says so.
func TestObservedDerivationMatchesTheGolden(t *testing.T) {
	t.Parallel()
	r := openObservedIndex(t)
	writeGoldenCorpus(t, r)
	got := goldenDump(t, r)
	if *updateObservedGolden {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(observedGolden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(observedGolden)
	if err != nil {
		t.Fatalf("read %s: %v (generate it with -update-observed-golden)", observedGolden, err)
	}
	if got != string(want) {
		t.Fatalf("the observed index derives other rows from the golden corpus than %s holds. "+
			"A change to deriveObserved, the rollup builders, observed.Compose, the tiers, the fingerprint or the latest "+
			"row changes what a live repository holds against what a rebuild derives: bump observedIndexVersion in "+
			"internal/engine/observedindex.go so every repository re-derives at its next open, then regenerate the file with\n"+
			"  go test ./internal/engine -run '^TestObservedDerivationMatchesTheGolden$' -update-observed-golden\n"+
			"first difference, golden then derived:\n%s", observedGolden, firstDifference(want, []byte(got)))
	}
}

// writeGoldenCorpus writes the golden corpus, every record at a fixed id.
func writeGoldenCorpus(t *testing.T, r obsIndex) {
	t.Helper()
	ctx := context.Background()
	berlinSummer, berlinWinter := 120, 60
	trace := func(id string, props map[string]any) {
		t.Helper()
		r.mustPut(t, r.trace, id, props)
	}
	obs := func(id string, props map[string]any) {
		t.Helper()
		r.mustPut(t, r.observation, id, props)
	}
	r.mustPut(t, r.metric, "golden/wind", map[string]any{
		"name": "Wind direction", "measures": "angle", "unit": "deg", "form": "gauge",
	})
	r.mustPut(t, r.metric, "golden/fine", map[string]any{
		"name": "Fine power", "measures": "power", "unit": "W", "form": "gauge", "rollups": []any{"5m", "1h"},
	})
	// A gauge trace each hour across the clock change of 2026-10-25, a null
	// in the middle of one, the second hour's hold shorter than its gaps.
	for i, c := range []struct {
		at     string
		offset int
		values []any
	}{
		{"2026-10-24T22:00:00Z", berlinSummer, []any{"21.5", "21.7", nil, "22.0", "22.4", "22.1"}},
		{"2026-10-24T23:00:00Z", berlinSummer, []any{"21.0", "20.9", "20.8", "20.6", "20.7", "20.5"}},
		{"2026-10-25T00:00:00Z", berlinSummer, []any{"20.4", "20.3", "20.1", "20.0", "19.9", "19.8"}},
		{"2026-10-25T01:00:00Z", berlinWinter, []any{"19.7", "19.6", "19.4", "19.5", "19.3", "19.2"}},
		{"2026-10-25T02:00:00Z", berlinWinter, []any{"19.1", "19.0", "18.9", "18.8", "18.7", "18.6"}},
	} {
		start, _ := time.Parse(time.RFC3339, c.at)
		props := map[string]any{
			"metric": r.metricRef("air_temperature"), "source": r.sourceRef("attic"), "form": "gauge", "unit": "Cel",
			"at": c.at, "endsAt": start.Add(time.Hour).Format(time.RFC3339), "every": "PT10M", "values": c.values,
			"utcOffset": c.offset,
		}
		if i == 1 {
			props["hold"] = "PT4M"
		}
		trace(fmt.Sprintf("attic/%d", i), props)
	}
	// A gauge written without an offset, a temperature difference, two
	// units of one measure, and an angle.
	trace("cellar/0", map[string]any{
		"metric": r.metricRef("air_temperature"), "source": r.sourceRef("cellar"), "form": "gauge", "unit": "Cel",
		"at": "2026-10-25T03:00:00Z", "endsAt": "2026-10-25T04:00:00Z", "offsets": []any{0, 1200000, 3000000},
		"values": []any{"12.0", "12.5", "12.25"},
	})
	obs("delta/0", map[string]any{
		"metric": r.metricRef("air_temperature"), "source": r.sourceRef("delta"), "form": "gauge", "delta": true,
		"value": quantity("1.5", "K"), "at": "2026-10-25T03:10:00Z", "utcOffset": berlinWinter,
	})
	obs("weight/kg", map[string]any{
		"metric": r.metricRef("health/body_mass"), "source": r.sourceRef("scale"), "form": "gauge", "hold": "PT0S",
		"value": quantity("72.40", "kg"), "at": "2026-10-25T06:41:12Z", "utcOffset": berlinWinter,
	})
	obs("weight/lb", map[string]any{
		"metric": r.metricRef("health/body_mass"), "source": r.sourceRef("scale"), "form": "gauge", "hold": "PT0S",
		"value": quantity("159.5", "[lb_av]"), "at": "2026-10-26T06:40:00Z", "utcOffset": berlinWinter,
	})
	trace("wind/0", map[string]any{
		"metric": r.metricRef("golden/wind"), "source": r.sourceRef("vane"), "form": "gauge", "unit": "deg", "circular": true,
		"at": "2026-10-25T03:00:00Z", "endsAt": "2026-10-25T03:30:00Z", "every": "PT5M",
		"values": []any{"350", "10", "355", "5", "0", "345"}, "utcOffset": berlinWinter,
	})
	// A coded gauge, single and packed.
	for i, code := range []string{"closed", "open", "closed"} {
		obs(fmt.Sprintf("door/%d", i), map[string]any{
			"metric": r.metricRef("illuminance"), "source": r.sourceRef("hall"), "channel": "contact", "form": "gauge",
			"code": code, "at": time.Date(2026, 10, 25, 8, 7*i+1, 0, 0, time.UTC).Format(time.RFC3339),
		})
	}
	trace("stages/0", map[string]any{
		"metric": r.metricRef("health/sleep_stage"), "source": r.sourceRef("ring"), "form": "gauge",
		"at": "2026-10-24T23:00:00Z", "endsAt": "2026-10-25T01:00:00Z", "offsets": []any{0, 1800000, 3600000, 5400000},
		"codes": []any{"light", "deep", "rem", "awake"}, "utcOffset": berlinSummer,
	})
	// Amounts: a trace on a +05:30 trip with a null, and a coded single.
	trace("steps/0", map[string]any{
		"metric": r.metricRef("health/steps"), "source": r.sourceRef("phone"), "form": "amount", "unit": "{steps}",
		"at": "2026-10-25T04:30:00Z", "endsAt": "2026-10-25T06:30:00Z", "offsets": []any{0, 600000, 2700000, 5400000},
		"values": []any{"412", nil, "88", "1005"}, "utcOffset": 330,
	})
	obs("sleep/rem", map[string]any{
		"metric": r.metricRef("health/sleep_stage"), "source": r.sourceRef("watch"), "form": "amount",
		"value": quantity("94", "min"), "code": "rem", "at": "2026-10-25T01:00:00Z", "endsAt": "2026-10-25T02:34:00Z",
		"utcOffset": berlinWinter,
	})
	// Counters: one that resets, one that wraps.
	trace("meter/0", map[string]any{
		"metric": r.metricRef("energy"), "source": r.sourceRef("meter"), "form": "counter", "unit": "kW.h", "resetRule": "monotonic",
		"at": "2026-10-25T05:00:00Z", "endsAt": "2026-10-25T07:00:00Z", "every": "PT15M", "utcOffset": berlinWinter,
		"values": []any{"18342.655", "18342.700", "0.010", "0.050", "0.400", "0.900", "0.020", "0.090"},
	})
	trace("odometer/0", map[string]any{
		"metric": r.metricRef("energy"), "source": r.sourceRef("odometer"), "form": "counter", "unit": "kW.h",
		"resetRule": "wrap", "wrapAt": "1000", "at": "2026-10-25T05:00:00Z", "endsAt": "2026-10-25T06:00:00Z",
		"every": "PT20M", "values": []any{"990", "998", "4"}, "utcOffset": berlinWinter,
	})
	// A summary, a censored lab value and a metric with its own tiers.
	obs("night/hrv", map[string]any{
		"metric": r.metricRef("health/whoop_recovery"), "source": r.sourceRef("strap"), "form": "summary", "statistic": "vendor",
		"period": "cycle", "value": quantity("67", "%"), "at": "2026-10-24T22:31:00Z", "endsAt": "2026-10-25T06:02:00Z",
	})
	r.mustPut(t, r.labresult, "lab/tsh", map[string]any{
		"metric": r.metricRef("health/tsh"), "hold": "PT0S",
		"value": map[string]any{"value": "0.01", "unit": "m[IU]/L", "comparator": "<"},
		"at":    "2026-10-25T08:05:00Z", "utcOffset": berlinWinter, "status": "final",
	})
	trace("fine/0", map[string]any{
		"metric": r.metricRef("golden/fine"), "source": r.sourceRef("plug"), "form": "gauge", "unit": "W",
		"at": "2026-10-25T09:00:00Z", "endsAt": "2026-10-25T09:20:00Z", "every": "PT2M", "utcOffset": berlinWinter,
		"values": []any{"40", "42", "39", "120", "118", "41", "40", "40", "39", "38"},
	})
	if _, err := r.ds.Delete(ctx, owner, r.observation, "door/1", substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
}

// goldenDump renders the corpus's segments, points, rollup rows and latest
// rows as one document, the test repository's authority spelled HOME.
func goldenDump(t *testing.T, r obsIndex) string {
	t.Helper()
	observedTestViews(t, r.db)
	var b strings.Builder
	// One row a line, so a change reads as the rows it moved.
	section := func(name, q string) {
		rows, err := r.db.Query(q)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		defer func() { _ = rows.Close() }()
		fmt.Fprintf(&b, "%q: [\n", name)
		first := true
		for rows.Next() {
			var row string
			if err := rows.Scan(&row); err != nil {
				t.Fatal(err)
			}
			if !first {
				b.WriteString(",\n")
			}
			first = false
			b.WriteString(row)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		b.WriteString("\n],\n")
	}
	b.WriteString("{\n")
	section("segments", `SELECT (to_jsonb(g) - 'repository' - 'stream'
			|| jsonb_build_object('key', jsonb_build_array(s.metric, s.kind, s.source, s.about, s.channel, s.statistic)))::text
		FROM observed_segments g JOIN observed_streams s ON s.id = g.stream ORDER BY g.kind, g.record_id`)
	section("rollups", `SELECT (to_jsonb(r) - 'repository' - 'stream')::text FROM test_rollup_tiers r
		ORDER BY metric, kind, source, about, channel, statistic, form, tier, bucket, code, unit, delta, utc_offset_min, offset_known`)
	section("latest", `SELECT (to_jsonb(l) - 'repository' - 'stream')::text FROM test_latest l
		ORDER BY metric, kind, source, about, channel, statistic`)
	b.WriteString("\"points\": [\n")
	for i, p := range derivedPoints(t, r.ds) {
		line, err := json.Marshal(p)
		if err != nil {
			t.Fatal(err)
		}
		if i > 0 {
			b.WriteString(",\n")
		}
		b.Write(line)
	}
	b.WriteString("\n]\n}\n")
	return strings.ReplaceAll(b.String(), testdb.Repository(t), "HOME")
}
