package providertest

import (
	"context"
	"fmt"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/samples"
)

// The observations and health sample kinds the health adapter suites read
// back, under the shipped samples authority. Shared by every adapter suite in
// this package, so a suite's own constants never redeclare them.
const (
	obsObservation = samples.Authority + "/observations/observation"
	obsTrace       = samples.Authority + "/observations/trace"
	obsSource      = samples.Authority + "/observations/source"
	healthSleep    = samples.Authority + "/health/sleep"
	healthWorkout  = samples.Authority + "/health/workout"
)

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
// does: the same closure applied again, its ADAPTER constant raised from @1
// to @2, and nothing upstream changed.
func upgradeAdapter(t *testing.T, ds substrate.Dataset, name string) {
	t.Helper()
	docs := loadDocs(t, samplesDir+"/"+name+"/bundle.yaml")
	rewriteSource(t, docs, `ADAPTER = "`+name+`@1"`, `ADAPTER = "`+name+`@2"`)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.BundleActor(samples.Authority, name), docs); err != nil {
		t.Fatalf("upgrade %s: %v", name, err)
	}
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
// every output `<name>@1` wrote is rewritten by `<name>@2`, at a higher
// version and at the revision it already held, and no output appears or
// goes.
func assertAdapterRewrite(t *testing.T, ds substrate.Dataset, name string, before map[string]*substrate.Record) {
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
		case now.Properties["adapter"] != name+"@2" || now.Version <= was.Version:
			t.Fatalf("%s: adapter %v at version %d (was %d), want %s@2 at a higher version",
				key, now.Properties["adapter"], now.Version, was.Version, name)
		case fmt.Sprint(now.Properties["revision"]) != fmt.Sprint(was.Properties["revision"]):
			t.Fatalf("%s: revision %v, was %v; an adapter-only rewrite keeps the upstream's",
				key, now.Properties["revision"], was.Properties["revision"])
		}
	}
}
