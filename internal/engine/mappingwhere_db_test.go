package engine_test

// A MAPPING'S WHERE (#581, record 0106). The fixture is a pull request mirror
// and a mapping that projects only the open ones onto a person: a stand-in for
// "the pull requests awaiting me" as a task, which is a projection over part
// of the source kind. A record outside the where resolves no subject, and one
// that leaves it loses its projection the way a deleted source does.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	prPackage  = "pr.connectors.substrate.reamde.dev/pr"
	typePR     = prPackage + "/pullrequest"
	typePRNote = prPackage + "/note"
	prsync     = substrate.Actor("connector:prsync")
)

func prManifest() enginetest.Manifest {
	return enginetest.Manifest{
		Name: "pr", Authority: prPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(prPackage, 1),
			vocabulary.ActorManifest(prPackage, string(prsync)),
			vocabulary.KindManifest(prPackage,
				map[string]any{"singular": "pullrequest"},
				map[string]any{
					"displayTemplate": "{heading}",
					"properties": map[string]any{
						"heading": map[string]any{"type": "string"},
						"author":  map[string]any{"type": "email"},
						"state":   map[string]any{"type": "enum", "values": []any{"open", "closed"}},
						"reviews": map[string]any{"type": "int"},
					},
				}),
			// A note ABOUT a person, which a writer may point at the pull
			// request describing them: the subject hop.
			vocabulary.KindManifest(prPackage,
				map[string]any{"singular": "note"},
				map[string]any{
					"displayTemplate": "{text}",
					"properties": map[string]any{
						"text":  map[string]any{"type": "string"},
						"about": map[string]any{"type": "reference", "kind": typePerson},
					},
				}),
		},
	}
}

// prMapping projects the pull requests the where covers; nil covers them all.
func prMapping(where map[string]any) map[string]any {
	data := map[string]any{
		"from": typePR, "property": "person",
		"match": []any{map[string]any{"from": "author", "to": "emails"}},
		"map":   map[string]any{"name": map[string]any{"path": "heading"}},
	}
	if where != nil {
		data["where"] = where
	}
	return enginetest.PeopleMapping("pullrequestperson", data)
}

var openOnly = map[string]any{"state": map[string]any{"eq": "open"}}

func syncPR(t *testing.T, ds substrate.Dataset, id, state string) *substrate.Record {
	t.Helper()
	return mustPut(t, ds, prsync, substrate.PutInput{
		Kind: typePR, ID: id,
		Properties: map[string]any{"heading": "Review " + id, "author": id + "@example.com", "state": state},
	})
}

func orphanedPersons(t *testing.T, ds substrate.Dataset) []string {
	t.Helper()
	yes := true
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{typePerson}, Orphaned: &yes}, First: 100,
	})
	if err != nil {
		t.Fatalf("list orphaned people: %v", err)
	}
	var out []string
	for _, r := range page.Records {
		out = append(out, r.ID)
	}
	return out
}

// A record outside the where resolves no subject and mints none; a record
// inside it projects exactly as it would under a mapping with no where.
func TestAMappingWhereDecidesWhichSourcesProject(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installSources(t, ds, []enginetest.Manifest{prManifest()},
		[]map[string]any{prMapping(openOnly)})

	closed := syncPR(t, ds, "pr1", "closed")
	if got := refPathValue(mustGet(t, ds, typePR, closed.ID), "person"); got != "" {
		t.Fatalf("a closed pull request resolved a subject: %s", got)
	}
	if got := livePersons(t, ds); len(got) != 0 {
		t.Fatalf("a closed pull request minted %d people", len(got))
	}

	open := syncPR(t, ds, "pr2", "open")
	p := mustGet(t, ds, typePerson, personOf(t, ds, open))
	if p.Properties["name"] != "Review pr2" {
		t.Fatalf("the open pull request did not project: %v", p.Properties)
	}

	// The closed one projects on the write that brings it inside.
	reopened := syncPR(t, ds, "pr1", "open")
	if got := mustGet(t, ds, typePerson, personOf(t, ds, reopened)); got.Properties["name"] != "Review pr1" {
		t.Fatalf("a reopened pull request did not project: %v", got.Properties)
	}
}

// A source that leaves the where stops contributing, and its subject orphans
// the way a deleted source's does. The pointer stands, so coming back inside
// projects onto the same subject instead of minting a second one.
func TestASourceLeavingTheWhereLosesItsProjection(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installSources(t, ds, []enginetest.Manifest{prManifest()},
		[]map[string]any{prMapping(openOnly)})

	open := syncPR(t, ds, "pr1", "open")
	person := personOf(t, ds, open)

	syncPR(t, ds, "pr1", "closed")
	p := mustGet(t, ds, typePerson, person)
	if _, has := p.Properties["name"]; has {
		t.Fatalf("a closed pull request still projects its name: %v", p.Properties)
	}
	if got := orphanedPersons(t, ds); len(got) != 1 || got[0] != person {
		t.Fatalf("orphaned = %v, want the person the closed pull request described", got)
	}

	back := syncPR(t, ds, "pr1", "open")
	if got := personOf(t, ds, back); got != person {
		t.Fatalf("a reopened pull request resolved %s, want the same person %s", got, person)
	}
	if got := mustGet(t, ds, typePerson, person); got.Properties["name"] != "Review pr1" {
		t.Fatalf("the reopened pull request did not project again: %v", got.Properties)
	}
	if got := orphanedPersons(t, ds); len(got) != 0 {
		t.Fatalf("orphaned = %v after the source came back", got)
	}
	if got := livePersons(t, ds); len(got) != 1 {
		t.Fatalf("%d people, want one", len(got))
	}
}

// Narrowing a live mapping takes effect in the apply: every target whose
// sources the new where leaves out is recomputed without them.
func TestNarrowingAMappingWhereRecomputesItsTargets(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installSources(t, ds, []enginetest.Manifest{prManifest()},
		[]map[string]any{prMapping(nil)})
	closed := syncPR(t, ds, "pr1", "closed")
	person := personOf(t, ds, closed)
	if got := mustGet(t, ds, typePerson, person); got.Properties["name"] != "Review pr1" {
		t.Fatalf("no where: the closed pull request did not project: %v", got.Properties)
	}

	if err := enginetest.DeclareMappings(context.Background(), ds, prMapping(openOnly)); err != nil {
		t.Fatalf("narrow the mapping: %v", err)
	}
	if got := mustGet(t, ds, typePerson, person); got.Properties["name"] != nil {
		t.Fatalf("the narrowed mapping still projects the closed pull request: %v", got.Properties)
	}
}

// The subject hop never mints for a record the mapping does not cover.
func TestTheSubjectHopRefusesAnUncoveredSource(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installSources(t, ds, []enginetest.Manifest{prManifest()},
		[]map[string]any{prMapping(openOnly)})
	closed := syncPR(t, ds, "pr1", "closed")
	// The same hop through a covered one resolves.
	open := syncPR(t, ds, "pr2", "open")
	note := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typePRNote, Properties: map[string]any{
			"text": "about the author", "about": vocabulary.RecordPath(typePR, open.ID),
		},
	})
	if got := refPathValue(mustGet(t, ds, typePRNote, note.ID), "about"); got != vocabulary.RecordPath(typePerson, personOf(t, ds, open)) {
		t.Fatalf("the hop through an open pull request stored %q", got)
	}
	_, err := ds.Put(context.Background(), owner, substrate.PutInput{
		Kind: typePRNote, Properties: map[string]any{
			"text": "about the author", "about": vocabulary.RecordPath(typePR, closed.ID),
		},
	})
	if err == nil || !strings.Contains(err.Error(), "does not cover") {
		t.Fatalf("a hop through a closed pull request = %v, want a refusal naming the where", err)
	}
}

// A where the filter grammar refuses fails the apply that declares it, not
// the first write of its source kind.
func TestAMappingWhereIsCheckedAtApply(t *testing.T) {
	t.Parallel()
	for name, where := range map[string]map[string]any{
		"match on a number":    {"reviews": map[string]any{"match": "two"}},
		"a number that is not": {"reviews": map[string]any{"gt": "many"}},
		"an undeclared name":   {"status": map[string]any{"eq": "open"}},
		"a bare value":         {"state": "open"},
		"an unknown operator":  {"state": map[string]any{"is": "open"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, ds := newDataset(t)
			if err := enginetest.Install(context.Background(), ds, substrate.ActorSystem, prManifest()); err != nil {
				t.Fatal(err)
			}
			err := enginetest.DeclareMappings(context.Background(), ds, prMapping(where))
			if err == nil {
				t.Fatalf("where %v was admitted", where)
			}
			if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "where") {
				t.Fatalf("where %v: %v, want a validation error naming data.where", where, err)
			}
		})
	}
}
