package providertest

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/runner"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
	"github.com/geoah/substrate/samples"
)

// The observations and health sample kinds the health adapter suites read
// back, under the shipped samples authority. Shared by every adapter suite in
// this package, so a suite's own constants never redeclare them.
const (
	obsObservation = samples.Authority + "/observations/observation"
	obsTrace       = samples.Authority + "/observations/trace"
	obsSource      = samples.Authority + "/observations/source"
	obsMetric      = samples.Authority + "/observations/metric"
	healthSleep    = samples.Authority + "/health/sleep"
	healthWorkout  = samples.Authority + "/health/workout"
	// healthMetrics prefixes a health catalogue metric's id into its record
	// path: healthMetrics + "heart_rate".
	healthMetrics = obsMetric + "/health/"
)

// The adapter stamp each shipped adapter writes on its outputs, its
// function's ADAPTER constant.
const (
	healthAppleAdapter     = "healthapple@2"
	healthWhoopAdapter     = "healthwhoop@4"
	healthGarminAdapter    = "healthgarmin@2"
	healthGoogleFitAdapter = "healthgooglefit@4"
)

// asFloat reads a number however it reached the test: a float64 from
// encoding/json, an int or int64 from Go, a json.Number from a decoder that
// keeps the digits, or digits in a string, as Fit sends its instants.
// Anything else fails the test, so an absent value never reads as zero.
func asFloat(t *testing.T, v any) float64 {
	t.Helper()
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, err := n.Float64()
		if err != nil {
			t.Fatalf("%v is not a number: %v", v, err)
		}
		return f
	case string:
		f, err := strconv.ParseFloat(n, 64)
		if err != nil {
			t.Fatalf("%q is not a number: %v", n, err)
		}
		return f
	}
	t.Fatalf("%v (%T) is not a number", v, v)
	return 0
}

// asInt is asFloat for a whole number, read without a float's rounding.
func asInt(t *testing.T, v any) int64 {
	t.Helper()
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			t.Fatalf("%v is not a whole number: %v", v, err)
		}
		return i
	case string:
		i, err := strconv.ParseInt(n, 10, 64)
		if err != nil {
			t.Fatalf("%q is not a whole number: %v", n, err)
		}
		return i
	}
	f := asFloat(t, v)
	if f != math.Trunc(f) || math.Abs(f) > 1<<53 {
		t.Fatalf("%v is not a whole number a float64 holds exactly", v)
	}
	return int64(f)
}

// asInstant reads a stored datetime, whatever spelling the engine stored it
// in, or an RFC 3339 string; anything else fails the test.
func asInstant(t *testing.T, v any) time.Time {
	t.Helper()
	at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(v))
	if err != nil {
		t.Fatalf("%v is not an instant: %v", v, err)
	}
	return at
}

// sameInstant reports whether a stored datetime is want. A value that is no
// datetime, an absent one included, is not, so the caller names the field.
func sameInstant(got any, want time.Time) bool {
	at, err := time.Parse(time.RFC3339Nano, fmt.Sprint(got))
	return err == nil && at.Equal(want)
}

// adapterOutputs is every observation, trace, sleep and workout an adapter
// wrote, by "<kind>/<id>": the kinds that carry its `adapter` stamp.
func adapterOutputs(t *testing.T, ds substrate.Dataset) map[string]*substrate.Record {
	t.Helper()
	out := map[string]*substrate.Record{}
	for _, kind := range []string{obsObservation, obsTrace, healthSleep, healthWorkout} {
		for _, row := range listLive(t, ds, kind) {
			out[kind+"/"+row.ID] = row
		}
	}
	return out
}

// adaptRow hands one adapter function one mirror row on the envelope the
// dispatcher builds for a record trigger (runner.Envelope): the row as it
// stands, or a delete with no record once it is gone. It applies what the
// function stages and answers the function's output.
func adaptRow(t *testing.T, ds substrate.Dataset, fn, kind, id string) map[string]any {
	t.Helper()
	ch := substrate.Change{Op: substrate.OpPut, Kind: kind, RecordID: id, Actor: substrate.ActorAPI}
	row, err := ds.Get(context.Background(), kind, id)
	if err != nil || row.DeletedAt != nil {
		ch.Op, row = substrate.OpDelete, nil
	}
	s := newStepper(t, ds, fn, map[string]any{})
	s.setEnvelope(runner.Envelope(ch, row, ds.Repository().Authority))
	_, out, _ := s.step(nil)
	s.apply()
	m, _ := out.(map[string]any)
	return m
}

// assertAdapterStamp holds every output to the adapter version that wrote it.
func assertAdapterStamp(t *testing.T, rows map[string]*substrate.Record, stamp string) {
	t.Helper()
	if len(rows) == 0 {
		t.Fatal("the adapter wrote nothing")
	}
	for key, row := range rows {
		if row.Properties["adapter"] != stamp {
			t.Fatalf("%s carries adapter %v, want %s", key, row.Properties["adapter"], stamp)
		}
	}
}

// upgradeAdapter lands a new version of a health adapter the way an upgrade
// does: the same closure applied again, its ADAPTER constant raised from the
// stamp it ships (`healthgarmin@2`) to the next version, and nothing upstream
// changed. It answers the new stamp.
func upgradeAdapter(t *testing.T, ds substrate.Dataset, shipped string) string {
	t.Helper()
	name, version, ok := strings.Cut(shipped, "@")
	n, err := strconv.Atoi(version)
	if !ok || err != nil {
		t.Fatalf("%q is not an adapter stamp <package>@<version>", shipped)
	}
	next := fmt.Sprintf("%s@%d", name, n+1)
	docs := loadDocs(t, samplesDir+"/"+name+"/bundle.yaml")
	rewriteSource(t, docs, `ADAPTER = "`+shipped+`"`, `ADAPTER = "`+next+`"`)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.BundleActor(samples.Authority, name), docs); err != nil {
		t.Fatalf("upgrade %s: %v", name, err)
	}
	return next
}

// replayAdapter reprocesses every unchanged mirror row through the adapter's
// record trigger, from the start of the changelog: what an adapter's README
// tells an owner to do after an upgrade.
func replayAdapter(t *testing.T, ds substrate.Dataset, trigger string) {
	t.Helper()
	if err := ds.ReplayTrigger(context.Background(), trigger, 0); err != nil {
		t.Fatalf("replay %s: %v", trigger, err)
	}
	drainTriggers(t, ds)
	if n := parkedFailures(t, ds); n != 0 {
		t.Fatalf("%d deliveries parked replaying %s", n, trigger)
	}
}

// assertAdapterRewrite holds an adapter-only rewrite to decision record 0162:
// every output the shipped adapter wrote is rewritten under `stamp`, at a
// higher version and at the revision it already held, and no output appears
// or goes.
func assertAdapterRewrite(t *testing.T, ds substrate.Dataset, stamp string, before map[string]*substrate.Record) {
	t.Helper()
	after := adapterOutputs(t, ds)
	if len(after) != len(before) {
		t.Fatalf("%d outputs after the upgrade, %d before", len(after), len(before))
	}
	for key, was := range before {
		now := after[key]
		switch {
		case now == nil:
			t.Fatalf("%s is gone after the upgrade", key)
		case now.Properties["adapter"] != stamp || now.Version <= was.Version:
			t.Fatalf("%s: adapter %v at version %d (was %d), want %s at a higher version",
				key, now.Properties["adapter"], now.Version, was.Version, stamp)
		case fmt.Sprint(now.Properties["revision"]) != fmt.Sprint(was.Properties["revision"]):
			t.Fatalf("%s: revision %v, was %v; an adapter-only rewrite keeps the upstream's",
				key, now.Properties["revision"], was.Properties["revision"])
		}
	}
}

// catalogueHolds is the hold every health metric declares in the catalogue
// (samples/health/metrics.yaml), "" where it declares none, by its id
// (`health/heart_rate`).
func catalogueHolds(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, doc := range loadDocs(t, samplesDir+"/health/metrics.yaml") {
		meta, _ := doc["metadata"].(map[string]any)
		data, _ := doc["data"].(map[string]any)
		props, _ := data["properties"].(map[string]any)
		id, _ := meta["id"].(string)
		if id == "" || props == nil {
			continue
		}
		hold, _ := props["hold"].(string)
		out[id] = hold
	}
	if len(out) == 0 {
		t.Fatal("the health catalogue declares no metric")
	}
	return out
}

// assertCatalogueHolds holds every gauge an adapter wrote, trace or single
// observation, to the hold its metric declares in the catalogue. A gauge
// written without one holds until its stream's next point, so a night's
// last reading would cover the whole next day.
func assertCatalogueHolds(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	holds := catalogueHolds(t)
	wrong := map[string]string{}
	seen := 0
	for _, kind := range []string{obsTrace, obsObservation} {
		for _, row := range listLive(t, ds, kind) {
			if row.Properties["form"] != "gauge" {
				continue
			}
			seen++
			metric := strings.TrimPrefix(storedRefPath(row.Properties["metric"]), samples.Authority+"/observations/metric/")
			want, known := holds[metric]
			got, _ := row.Properties["hold"].(string)
			if !known || got != want {
				wrong[metric] = fmt.Sprintf("%s holds %q, the catalogue %q", row.ID, got, want)
			}
		}
	}
	if seen == 0 {
		t.Error("the adapter wrote no gauge")
	}
	for _, metric := range slices.Sorted(maps.Keys(wrong)) {
		t.Errorf("%s: %s", metric, wrong[metric])
	}
}

// adapterConstant is the ADAPTER line at the top of an adapter's function.
var adapterConstant = regexp.MustCompile(`(?m)^ADAPTER = "([^"]+)"$`)

// TestHealthAdaptersStampTheirPackageVersion: each adapter's ADAPTER stamp is
// `<package>@<package version>`, and the shared constants the suites assert
// are those stamps. A stamp raised without the package's version is code no
// repository that imported the package is offered, and a package version
// raised without the stamp rewrites nothing on a replay.
func TestHealthAdaptersStampTheirPackageVersion(t *testing.T) {
	t.Parallel()
	for name, want := range map[string]string{
		"healthapple": healthAppleAdapter, "healthgarmin": healthGarminAdapter,
		"healthgooglefit": healthGoogleFitAdapter, "healthwhoop": healthWhoopAdapter,
	} {
		var version any
		var stamps []string
		for _, d := range loadDocs(t, samplesDir+"/"+name+"/bundle.yaml") {
			data, _ := d["data"].(map[string]any)
			switch d["kind"] {
			case vocabulary.CoreKind(vocabulary.DocPackage):
				version = data["version"]
			case typeFunction:
				src, _ := data["source"].(string)
				for _, m := range adapterConstant.FindAllStringSubmatch(src, -1) {
					stamps = append(stamps, m[1])
				}
			}
		}
		if len(stamps) != 1 || stamps[0] != fmt.Sprintf("%s@%v", name, version) || stamps[0] != want {
			t.Errorf("%s at package version %v stamps %v, want %s@%v and the suites' %s", name, version, stamps, name, version, want)
		}
	}
}
