package commands

import (
	"strings"
	"testing"
)

// The URL-harvester example ships two files and a README that says to install
// them with `substratectl apply -f bundle.yaml -f triggers.yaml`. This drives the
// EXACT shipped files through the real CLI apply path — schema batch for the
// bundle closure, the resolver for each trigger — so "shipped example" means
// installable, not just green in an engine test that bypasses the resolver.
const exampleDir = "../../../kinds/web.bundles.substrate.reamde.dev"

func TestShippedURLHarvesterExampleApplies(t *testing.T) {
	h := newHarness(t)
	h.writeConfig()
	// triggers.yaml is data in core.substrate.reamde.dev; the real resolver needs the
	// `trigger` type in the registry to route it. bundle.yaml is all core
	// schema kinds and rides the batch verb without a registry lookup.
	h.fake.extraTypes = []map[string]any{
		typeRecord("trigger", "core.substrate.reamde.dev", "triggers", "builtin", nil),
	}

	out, errOut, err := h.run("apply",
		"-f", exampleDir+"/bundle.yaml",
		"-f", exampleDir+"/triggers.yaml")
	if err != nil {
		t.Fatalf("apply of the shipped example failed: %v\nstdout:\n%s\nstderr:\n%s", err, out, errOut)
	}

	// The bundle closure landed as ONE schema batch, and every schema member
	// printed an "applied" line (authority + bundle + 2 kinds + 4
	// functions + 3 agents = 11 documents).
	if got := strings.Count(out, " applied\n"); got != 11 {
		t.Fatalf("schema apply output = %d applied lines, want 11:\n%s", got, out)
	}
	batches := 0
	for _, req := range h.fake.requests {
		if req == "POST /api/v1/core.substrate.reamde.dev/vocabulary/apply" {
			batches++
		}
	}
	if batches != 1 {
		t.Fatalf("expected ONE schema batch, saw %d: %v", batches, h.fake.requests)
	}

	// The four triggers each resolved through the real `type: trigger` path and
	// were PUT into core.substrate.reamde.dev/triggers.
	for _, id := range []string{
		"web-findurls-on-message", "web-fetch-on-page",
		"web-classify-on-page", "web-rollup-weekly",
	} {
		want := "PUT " + triggerColPath + "/" + id
		var saw bool
		for _, req := range h.fake.requests {
			saw = saw || req == want
		}
		if !saw {
			t.Fatalf("trigger %s was not applied to its collection: %v", id, h.fake.requests)
		}
		if !strings.Contains(out, "core.substrate.reamde.dev/trigger/"+id+" created") {
			t.Fatalf("apply did not report %s created:\n%s", id, out)
		}
	}
}

// The shipped triggers.yaml MUST use the singular local name `type: trigger`:
// the real resolver rejects a full identity there (envelope_guard_test proves
// the rejection generically). This pins the shipped file to the accepted form
// so a regression that re-qualifies the type is caught against the real CLI.
func TestShippedTriggersUseTheSingularType(t *testing.T) {
	docs, vocabularyDocs, err := (&app{}).readDocuments([]string{exampleDir + "/triggers.yaml"})
	if err != nil {
		t.Fatalf("read triggers.yaml: %v", err)
	}
	if len(vocabularyDocs) != 0 {
		t.Fatalf("triggers.yaml carries %d schema docs, want 0 (triggers are data)", len(vocabularyDocs))
	}
	if len(docs) != 4 {
		t.Fatalf("triggers.yaml carries %d data docs, want 4", len(docs))
	}
	for _, d := range docs {
		if d.Kind != "core.substrate.reamde.dev/trigger" {
			t.Fatalf("trigger doc kind = %q, want the kind reference core.substrate.reamde.dev/trigger", d.Kind)
		}
	}
}
