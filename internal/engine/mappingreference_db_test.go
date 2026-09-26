package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The relation fixture (#580): a tracker whose ticket mirror points at Slack
// user mirrors, and the tracker's own card kind pinned at the person those
// users describe. The mapping copies the mirror references as they are, and
// the card's pin is what turns each one into its person.
const (
	pmPackage  = "pm.test.dev/pm"
	typeTicket = pmPackage + "/ticket"
	typeCard   = pmPackage + "/card"
	pmsync     = substrate.Actor("connector:pmsync")
)

func pmManifest() enginetest.Manifest {
	return enginetest.Manifest{
		Name: "pm", Authority: pmPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pmPackage, 1),
			vocabulary.ActorManifest(pmPackage, string(pmsync)),
			vocabulary.KindManifest(pmPackage,
				map[string]any{"singular": "card"},
				map[string]any{
					"displayTemplate": "{name}",
					"properties": map[string]any{
						"name":     map[string]any{"type": "string"},
						"owner":    map[string]any{"type": "reference", "kind": typePerson, "mustExist": true},
						"watchers": map[string]any{"type": "reference", "kind": typePerson, "repeated": true},
					},
				}),
			vocabulary.KindManifest(pmPackage,
				map[string]any{"singular": "ticket"},
				map[string]any{
					"displayTemplate": "{heading}",
					"properties": map[string]any{
						"heading":   map[string]any{"type": "string"},
						"assignee":  map[string]any{"type": "reference", "kind": typeSlackUser},
						"assignees": map[string]any{"type": "reference", "kind": typeSlackUser, "repeated": true},
					},
				}),
			vocabulary.MappingManifest(pmPackage, "ticketcard", map[string]any{
				"from": typeTicket, "to": typeCard, "property": "card",
				"map": map[string]any{
					"name":     map[string]any{"path": "heading"},
					"owner":    map[string]any{"path": "assignee"},
					"watchers": map[string]any{"path": "assignees"},
				},
			}),
		},
	}
}

// relationFixture installs the tracker beside the people sources, with two
// Slack users of one person (ada) and a third of another (grace). sync writes
// ticket T1 naming all three and returns the card it maps onto.
type relationFixture struct {
	svc        substrate.Service
	ds         substrate.Dataset
	ada, grace string
	sync       func(heading string) *substrate.Record
}

func newRelationFixture(t *testing.T) relationFixture {
	t.Helper()
	ctx := context.Background()
	svc, ds := newDataset(t)
	installPeopleSources(t, ds)
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, pmManifest()); err != nil {
		t.Fatalf("install the tracker: %v", err)
	}

	// Two Slack users on one address converge on one person.
	u1 := syncSource(t, ds, slack, typeSlackUser, "U1", map[string]any{"realName": "Ada", "email": "ada@example.com"})
	u2 := syncSource(t, ds, slack, typeSlackUser, "U2", map[string]any{"realName": "Ada L", "email": "ada@example.com"})
	ada := personOf(t, ds, u1)
	if personOf(t, ds, u2) != ada {
		t.Fatal("the fixture needs both users on one person")
	}
	u3 := syncSource(t, ds, slack, typeSlackUser, "U3", map[string]any{"realName": "Grace", "email": "grace@example.com"})
	grace := personOf(t, ds, u3)

	sync := func(heading string) *substrate.Record {
		t.Helper()
		ticket := mustPut(t, ds, pmsync, substrate.PutInput{Kind: typeTicket, ID: "T1", Properties: map[string]any{
			"heading":   heading,
			"assignee":  typeSlackUser + "/" + u1.ID,
			"assignees": []any{typeSlackUser + "/" + u1.ID, typeSlackUser + "/" + u3.ID, typeSlackUser + "/" + u2.ID},
		}})
		kind, id, ok := vocabulary.SplitRecordPath(refPathValue(mustGet(t, ds, ticket.Kind, ticket.ID), "card"))
		if !ok || kind != typeCard {
			t.Fatalf("the ticket names no card: %v", ticket.Properties["card"])
		}
		return mustGet(t, ds, kind, id)
	}
	return relationFixture{svc: svc, ds: ds, ada: ada, grace: grace, sync: sync}
}

// assertTicketBacksCard: the card's owner and watchers name the ticket as
// their source and list no alternative. An offer compared as the mirror it
// spells would read as an alternative the ticket never offered.
func assertTicketBacksCard(t *testing.T, card *substrate.Record) {
	t.Helper()
	for _, name := range []string{"owner", "watchers"} {
		meta := card.PropertyMeta[name]
		if meta.Source != typeTicket+"/T1" || len(meta.Alternatives) != 0 {
			t.Fatalf("%s meta = %+v, want the ticket as the source and no alternative", name, meta)
		}
	}
}

// A map rule carries a relation whose other end is itself mirrored (#580,
// record 0106): the ticket's `assignee` at a Slack user lands on the card as
// the user's PERSON, the read backs it with the ticket's offer, and two users
// of one person are one watcher.
func TestMappedReferenceLandsOnTheMirrorsSubject(t *testing.T) {
	t.Parallel()
	f := newRelationFixture(t)

	card := f.sync("ship it")
	if got, want := refPathValue(card, "owner"), typePerson+"/"+f.ada; got != want {
		t.Fatalf("owner = %q, want the user's person %q", got, want)
	}
	var watchers []string
	for _, w := range asList(card.Properties["watchers"]) {
		watchers = append(watchers, referencePath(w))
	}
	if want := []string{typePerson + "/" + f.ada, typePerson + "/" + f.grace}; !equalStrings(watchers, want) {
		t.Fatalf("watchers = %v, want %v: two users of one person are one watcher", watchers, want)
	}
	assertTicketBacksCard(t, card)

	// A re-sync of the same relation writes nothing onto the card.
	if again := f.sync("ship it"); again.Version != card.Version {
		t.Fatalf("a re-sync moved the card from v%d to v%d", card.Version, again.Version)
	}
}

// The offers behind a mapped mirror reference stay a function of the source
// rows (record 0106): a merge or a delete of the mirror's person writes
// neither the ticket nor the card, so a rebuild derives the same offers and
// the same fold, and the read still backs the card with the ticket.
func TestMappedReferenceRebuildsAfterItsPersonMoves(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		move func(t *testing.T, f relationFixture)
	}{
		{"merge", func(t *testing.T, f relationFixture) {
			if _, err := f.ds.Merge(context.Background(), owner, substrate.MergeInput{Kind: typePerson, Winner: f.grace, Loser: f.ada}); err != nil {
				t.Fatalf("merge ada into grace: %v", err)
			}
		}},
		{"delete", func(t *testing.T, f relationFixture) {
			if _, err := f.ds.Delete(context.Background(), owner, typePerson, f.ada, substrate.DeleteInput{}); err != nil {
				t.Fatalf("delete ada: %v", err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			f := newRelationFixture(t)
			card := f.sync("ship it")
			tc.move(t, f)
			assertTicketBacksCard(t, mustGet(t, f.ds, card.Kind, card.ID))

			before := foldOf(t, f.ds)
			if _, err := f.svc.(rebuilder).RebuildRepository(context.Background(), testdb.Repository(t)); err != nil {
				t.Fatalf("rebuild: %v", err)
			}
			if after := foldOf(t, f.ds); string(before) != string(after) {
				t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, after))
			}
			assertTicketBacksCard(t, mustGet(t, f.ds, card.Kind, card.ID))
		})
	}
}

func asList(v any) []any {
	items, _ := v.([]any)
	return items
}

func referencePath(v any) string {
	if m, ok := v.(map[string]any); ok {
		s, _ := m[vocabulary.ReferenceValueKey].(string)
		return s
	}
	s, _ := v.(string)
	return s
}
