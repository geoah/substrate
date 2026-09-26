package engine_test

// A PROBE THAT FOLDS CASE (#586, record 0107). A probe compares exactly by
// default, so `Ada Example` and `ada Example` are two people. `fold: case` on
// a probe lowercases both ends, the source value and the target's stored one,
// so a spelling that differs only in case links the person already there.

import (
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
)

// slackNameMapping probes a slack user's real name onto person.name, folded
// or not.
func slackNameMapping(fold bool) map[string]any {
	probe := map[string]any{"from": "realName", "to": "name"}
	if fold {
		probe["fold"] = "case"
	}
	return enginetest.PeopleMapping("slackuserperson", map[string]any{
		"from": typeSlackUser, "property": "person",
		"match": []any{probe},
		"map": map[string]any{
			"name":        map[string]any{"path": "realName"},
			"displayName": map[string]any{"path": "displayName"},
		},
	})
}

// foldedContactMapping probes a contact's addresses onto person.emails with
// `fold: case`.
func foldedContactMapping() map[string]any {
	return enginetest.PeopleMapping("contactperson", map[string]any{
		"from": typeGoogleContact, "property": "person",
		"match": []any{map[string]any{"from": "emails[].value", "to": "emails", "fold": "case"}},
		"map": map[string]any{
			"name":   map[string]any{"path": "name.displayName"},
			"emails": map[string]any{"path": "emails[].value", "merge": "union"},
		},
	})
}

func TestAFoldedProbeMatchesAScalarInAnyCase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		fold bool
		same bool
	}{
		// The default stays exact: a casing difference is two people.
		{"exact", false, false},
		{"folded", true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, ds := newDataset(t)
			installSources(t, ds, []enginetest.Manifest{slackManifest()},
				[]map[string]any{slackNameMapping(tc.fold)})
			ada := mustPut(t, ds, owner, substrate.PutInput{
				Kind: typePerson, Properties: map[string]any{"name": "Ada Example"},
			})
			u := syncSource(t, ds, slack, typeSlackUser, "U-ADA", map[string]any{
				"realName": "  ada example ",
			})
			got := personOf(t, ds, u)
			if (got == ada.ID) != tc.same {
				t.Fatalf("linked %s (ada is %s), want same=%v", got, ada.ID, tc.same)
			}
			if n := len(livePersons(t, ds)); tc.same && n != 1 {
				t.Fatalf("%d people, want 1", n)
			}
		})
	}
}

// A repeated target is folded item by item: the person holds the address the
// way a human typed it, and the contact's lowercase spelling finds it.
func TestAFoldedProbeMatchesARepeatedItemInAnyCase(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installSources(t, ds, []enginetest.Manifest{googleManifest()},
		[]map[string]any{foldedContactMapping()})
	ada := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typePerson, Properties: map[string]any{"name": "Ada", "emails": []any{"Ada@Example.com"}},
	})
	c := syncSource(t, ds, people, typeGoogleContact, "g-ada", map[string]any{
		"name": aname("Ada"), "emails": gemails("ada@example.com"),
	})
	if got := personOf(t, ds, c); got != ada.ID {
		t.Fatalf("linked %s, want ada %s", got, ada.ID)
	}
	if n := len(livePersons(t, ds)); n != 1 {
		t.Fatalf("%d people, want 1", n)
	}
}

// A probed value never spreads (record 0103), and under a fold another
// target's value in a different casing is the same value: the next folded
// probe would find both.
func TestAFoldedProbedValueDoesNotSpreadInAnotherCase(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installSources(t, ds, []enginetest.Manifest{googleManifest()},
		[]map[string]any{foldedContactMapping()})
	alex := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typePerson, Properties: map[string]any{"name": "Alex"},
	})
	nina := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typePerson, Properties: map[string]any{"name": "Nina", "emails": []any{"Nina@Example.com"}},
	})
	syncSource(t, ds, people, typeGoogleContact, "g-alex", map[string]any{
		"name": aname("Alex"), "emails": gemails("alex@example.com", "nina@example.com"),
	}, alex.ID)
	p := mustGet(t, ds, typePerson, alex.ID)
	items, _ := p.Properties["emails"].([]any)
	for _, it := range items {
		if it == "nina@example.com" {
			t.Fatalf("alex took the address nina holds as %s: %v", nina.ID, items)
		}
	}
	if len(items) != 1 || items[0] != "alex@example.com" {
		t.Fatalf("alex's emails = %v, want [alex@example.com]", items)
	}
}
