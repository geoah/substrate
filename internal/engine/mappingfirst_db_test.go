package engine_test

// THE HEAD OF A REPEATED SOURCE, in both places it is asked for: a display
// template's `{names[].displayName}` and a map rule's `merge: first`. The
// fixture is a mirror kind that keeps its provider's arrays VERBATIM — a
// Google contact's `names[]`, `emailAddresses[]` — which is the shape that had
// no spelling at all before: a person has one name, and a repeated source
// could probe one and never set one.
//
// It declares NO subject slot — the mapping owns it (record 96) — so it is
// also the fixture for what an unresolved source does: a record that offers
// nothing mints nothing, and an ambiguous probe parks instead of minting a
// duplicate (record 0087).

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	bookPackage         = "book.connectors.substrate.reamde.dev/book"
	typeVerbatimContact = bookPackage + "/verbatimcontact"
	book                = substrate.Actor("connector:book")
)

// verbatimContactManifest is the mirror as a provider that does not normalize
// ships it: every People array kept as it arrived, and no subject slot.
func verbatimContactManifest() enginetest.Manifest {
	return enginetest.Manifest{
		Name: "book", Authority: bookPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(bookPackage, 1),
			vocabulary.ActorManifest(bookPackage, string(book)),
			vocabulary.KindManifest(bookPackage,
				map[string]any{"singular": "verbatimcontact"},
				map[string]any{
					// The name is the first entry's; a contact with no names at
					// all falls through to its nickname and then to the
					// provider's own key, which is the only thing every row has.
					"displayTemplate": "{names[].displayName|nicknames[]|resourceName}",
					"properties": map[string]any{
						"resourceName": map[string]any{"type": "string"},
						"names": map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
							"displayName": "string", "givenName": "string",
						}},
						"emailAddresses": map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
							"value": "email", "primary": "bool",
						}},
						"nicknames": map[string]any{"type": "string", "repeated": true},
					},
				}),
		},
	}
}

func verbatimContactMapping() map[string]any {
	return enginetest.PeopleMapping("verbatimcontactperson", map[string]any{
		"from": typeVerbatimContact, "property": "person",
		"match": []any{map[string]any{"from": "emailAddresses[].value", "to": "emails"}},
		"map": map[string]any{
			// `names[].displayName` is repeated and `person.name` is one
			// string: merge: first is the only rule that admits the pair.
			"name":   map[string]any{"path": "names[].displayName", "merge": "first"},
			"emails": map[string]any{"path": "emailAddresses[].value", "merge": "union"},
		},
	})
}

func installVerbatimContacts(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	installSources(t, ds,
		[]enginetest.Manifest{verbatimContactManifest()},
		[]map[string]any{verbatimContactMapping()})
}

func gnames(displays ...string) []any {
	out := make([]any, 0, len(displays))
	for _, d := range displays {
		out = append(out, map[string]any{"displayName": d})
	}
	return out
}

func gaddresses(addrs ...string) []any {
	out := make([]any, 0, len(addrs))
	for i, a := range addrs {
		out = append(out, map[string]any{"value": a, "primary": i == 0})
	}
	return out
}

// A display template renders the FIRST entry of a list, and an empty list
// hands the token's next alternative its turn — which is what makes a record
// whose provider sends arrays title itself as anything but its id.
func TestFirstOfAListTitlesTheRecord(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installVerbatimContacts(t, ds)

	named := mustPut(t, ds, book, substrate.PutInput{
		Kind: typeVerbatimContact, ID: "c1",
		Properties: map[string]any{
			"resourceName":   "people/c1",
			"names":          gnames("Alex Example", "A. Example"),
			"emailAddresses": gaddresses("alex@example.com"),
		},
	})
	if named.Title != "Alex Example" {
		t.Fatalf("title = %q, want the first name", named.Title)
	}
	// An entry that carries no displayName is not the answer: the first entry
	// that RENDERS something is.
	sparse := mustPut(t, ds, book, substrate.PutInput{
		Kind: typeVerbatimContact, ID: "c2",
		Properties: map[string]any{
			"resourceName":   "people/c2",
			"names":          []any{map[string]any{"givenName": "Nina"}, map[string]any{"displayName": "Nina Example"}},
			"emailAddresses": gaddresses("nina@example.com"),
		},
	})
	if sparse.Title != "Nina Example" {
		t.Fatalf("title = %q, want the first entry that renders", sparse.Title)
	}
	// An empty list falls through to the next alternative — the repeated
	// SCALAR form, and then the bare property.
	nick := mustPut(t, ds, book, substrate.PutInput{
		Kind: typeVerbatimContact, ID: "c3",
		Properties: map[string]any{
			"resourceName":   "people/c3",
			"nicknames":      []any{"Sam"},
			"emailAddresses": gaddresses("sam@example.com"),
		},
	})
	if nick.Title != "Sam" {
		t.Fatalf("title = %q, want the first nickname", nick.Title)
	}
	bare := mustPut(t, ds, book, substrate.PutInput{
		Kind: typeVerbatimContact, ID: "c4",
		Properties: map[string]any{"resourceName": "people/c4", "emailAddresses": gaddresses("x@example.com")},
	})
	if bare.Title != "people/c4" {
		t.Fatalf("title = %q, want the last alternative", bare.Title)
	}
}

// `merge: first` writes the head of a repeated source onto a single-valued
// target, and an EMPTY source writes nothing rather than clearing it: the
// second contact, which carries the same address and no name at all, converges
// onto the same person and leaves the name where it was.
func TestMergeFirstSetsASingleValuedTarget(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installVerbatimContacts(t, ds)

	full := mustPut(t, ds, book, substrate.PutInput{
		Kind: typeVerbatimContact, ID: "c1",
		Properties: map[string]any{
			"names":          gnames("Alex Example", "A. Example"),
			"emailAddresses": gaddresses("alex@example.com", "alex@work.example.com"),
		},
	})
	pid := personOf(t, ds, full)
	person := mustGet(t, ds, typePerson, pid)
	if person.Properties["name"] != "Alex Example" {
		t.Fatalf("person.name = %v, want the first of names[]", person.Properties["name"])
	}
	if got := len(person.Properties["emails"].([]any)); got != 2 {
		t.Fatalf("person.emails = %v, want the union of both addresses", person.Properties["emails"])
	}

	nameless := mustPut(t, ds, book, substrate.PutInput{
		Kind: typeVerbatimContact, ID: "c2",
		Properties: map[string]any{
			"names":          []any{},
			"emailAddresses": gaddresses("alex@example.com"),
		},
	})
	if got := personOf(t, ds, nameless); got != pid {
		t.Fatalf("the second contact linked %s, want the person the address names", got)
	}
	person = mustGet(t, ds, typePerson, pid)
	if person.Properties["name"] != "Alex Example" {
		t.Fatalf("an empty source cleared the name: %v", person.Properties["name"])
	}
}

// A MINT THAT WOULD WRITE NOTHING IS NOT MADE. A source record carrying no
// probe value and no mapped value says nothing about any subject, and the
// shell it used to mint was an empty row nothing could ever match — 614 of
// them on one seeded repository. The slot stays unset and the record's next
// write resolves it, so nothing is lost by waiting.
func TestASourceWithNothingToOfferMintsNoPerson(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installVerbatimContacts(t, ds)
	before := len(livePersons(t, ds))

	empty := mustPut(t, ds, book, substrate.PutInput{
		Kind: typeVerbatimContact, ID: "c1",
		Properties: map[string]any{"resourceName": "people/c1"},
	})
	if got := len(livePersons(t, ds)); got != before {
		t.Fatalf("%d persons, want the %d there were: an empty source minted one", got, before)
	}
	stored := mustGet(t, ds, empty.Kind, empty.ID)
	if v, ok := stored.Properties["person"]; ok && v != nil {
		t.Fatalf("the subject slot was filled with %v", v)
	}
	// The record itself landed: the write is not refused, it is unresolved.
	if stored.Title != "people/c1" {
		t.Fatalf("title = %q", stored.Title)
	}

	// A name is something to say about a person, so the next write mints.
	named := mustPatch(t, ds, book, empty.Kind, empty.ID, substrate.PatchInput{
		Properties: map[string]any{"names": gnames("Ned Example")},
	})
	pid := personOf(t, ds, named)
	if got := len(livePersons(t, ds)); got != before+1 {
		t.Fatalf("%d persons, want one more", got)
	}
	if person := mustGet(t, ds, typePerson, pid); person.Properties["name"] != "Ned Example" {
		t.Fatalf("the shell did not recompute: %v", person.Properties)
	}
}

// SEVERAL CANDIDATES IS NOT NO CANDIDATES (#577). A probe that cannot tell two
// people apart used to mint a third and union the probed address onto it, so
// every later probe on that address saw three candidates and minted a fourth —
// convergence that degraded as more sources synced. The source parks unlinked
// instead, and resolves the moment the ambiguity is gone.
func TestAnAmbiguousProbeParksInsteadOfMinting(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installVerbatimContacts(t, ds)

	a := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alex", "emails": []any{"family@example.com"}},
	})
	b := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alexa", "emails": []any{"family@example.com"}},
	})
	shared := mustPut(t, ds, book, substrate.PutInput{
		Kind: typeVerbatimContact, ID: "c1",
		Properties: map[string]any{
			"names":          gnames("The Family"),
			"emailAddresses": gaddresses("family@example.com"),
		},
	})
	if got := len(livePersons(t, ds)); got != 2 {
		t.Fatalf("%d persons, want the 2 the owner wrote: the ambiguity minted a third", got)
	}
	stored := mustGet(t, ds, shared.Kind, shared.ID)
	if v, ok := stored.Properties["person"]; ok && v != nil {
		t.Fatalf("an ambiguous probe linked %v", v)
	}
	// And the address did not land on anything new, which is what poisoned the
	// next probe.
	for _, p := range livePersons(t, ds) {
		if p.ID != a.ID && p.ID != b.ID {
			t.Fatalf("person %s appeared", p.ID)
		}
	}

	// The owner settles it — here by deleting one — and the next sync of the
	// same record links to what is left.
	if _, err := ds.Delete(ctx, owner, typePerson, b.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	resynced := mustPut(t, ds, book, substrate.PutInput{
		Kind: typeVerbatimContact, ID: "c1",
		Properties: map[string]any{
			"names":          gnames("The Family"),
			"emailAddresses": gaddresses("family@example.com"),
			"nicknames":      []any{"Fam"},
		},
	})
	if got := personOf(t, ds, resynced); got != a.ID {
		t.Fatalf("the resync linked %s, want the one person left", got)
	}
}
