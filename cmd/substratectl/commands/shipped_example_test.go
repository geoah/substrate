package commands

import (
	"strings"
	"testing"
)

// The reading-list example ships four files and a README that says to install
// them with one `substratectl apply`. This drives the EXACT shipped files
// through the real CLI apply path: one vocabulary batch for the closure, and
// the resolver for each data record. "Shipped example" has to mean
// installable, not just green in an engine test that bypasses the resolver.
const exampleDir = "../../../samples/readinglist"

func TestShippedReadingListExampleApplies(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	// triggers.yaml is data in substrate.reamde.dev/core; the real resolver needs the
	// `trigger` type in the registry to route it. bundle.yaml is all core
	// schema kinds and rides the batch verb without a registry lookup.
	h.fake.extraTypes = []map[string]any{
		typeRecord("trigger", "substrate.reamde.dev/core", "builtin", nil),
		typeRecord("setting", "substrate.reamde.dev/core", "builtin", nil),
		// The closure's own kind, as the registry would hold it once the
		// batch above landed: digest.yaml is a record OF the thing this very
		// apply declared.
		typeRecord("digest", "samples.substrate.reamde.dev/readinglist", "installed", nil),
	}

	out, errOut, err := h.run("apply",
		"-f", exampleDir+"/bundle.yaml",
		"-f", exampleDir+"/settings.yaml",
		"-f", exampleDir+"/digest.yaml",
		"-f", exampleDir+"/triggers.yaml")
	if err != nil {
		t.Fatalf("apply of the shipped example failed: %v\nstdout:\n%s\nstderr:\n%s", err, out, errOut)
	}

	// The bundle closure landed as ONE schema batch, and every schema member
	// printed an "applied" line (package + bundle + 2 kinds + 4 functions +
	// 3 agents = 11 documents).
	if got := strings.Count(out, " applied\n"); got != 11 {
		t.Fatalf("schema apply output = %d applied lines, want 11:\n%s", got, out)
	}
	batches := 0
	for _, req := range h.fake.requests {
		if req == "POST /api/v1/vocabulary/apply" {
			batches++
		}
	}
	if batches != 1 {
		t.Fatalf("expected ONE schema batch, saw %d: %v", batches, h.fake.requests)
	}

	// The four triggers each resolved through the real `type: trigger` path and
	// were PUT into substrate.reamde.dev/core/trigger.
	for _, id := range []string{
		"readinglist-findurls-on-message", "readinglist-fetch-on-page",
		"readinglist-classify-on-page", "readinglist-rollup-weekly",
	} {
		want := "PUT " + triggerColPath + "/" + id
		var saw bool
		for _, req := range h.fake.requests {
			saw = saw || req == want
		}
		if !saw {
			t.Fatalf("trigger %s was not applied to its collection: %v", id, h.fake.requests)
		}
		if !strings.Contains(out, "substrate.reamde.dev/core/trigger/"+id+" created") {
			t.Fatalf("apply did not report %s created:\n%s", id, out)
		}
	}
}
