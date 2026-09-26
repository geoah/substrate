package engine_test

// WHAT A PROBE WITH SEVERAL HITS DOES (#577). A probe that finds two people
// who share an address cannot tell them apart. The mapping's `onAmbiguous`
// says what happens next, `park` by default; and whatever it says, a probed
// value another target already holds is never written onto a target that does
// not, so no answer here makes the next probe more ambiguous than this one.

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// holdersOf lists the live persons whose `emails` carry the address.
func holdersOf(t *testing.T, ds substrate.Dataset, addr string) []string {
	t.Helper()
	var out []string
	for _, p := range livePersons(t, ds) {
		items, _ := p.Properties["emails"].([]any)
		for _, it := range items {
			if it == addr {
				out = append(out, p.ID)
				break
			}
		}
	}
	return out
}

// twoAlexes writes the two people a shared family address names.
func twoAlexes(t *testing.T, ds substrate.Dataset, addr string) (a, b *substrate.Record) {
	t.Helper()
	a = mustPut(t, ds, owner, substrate.PutInput{
		Kind: typePerson, Properties: map[string]any{"name": "Alex", "emails": []any{addr}},
	})
	b = mustPut(t, ds, owner, substrate.PutInput{
		Kind: typePerson, Properties: map[string]any{"name": "Alexa", "emails": []any{addr}},
	})
	return a, b
}

// verbatimContactMappingOn is the verbatim contact mapping with its
// `onAmbiguous` set.
func verbatimContactMappingOn(policy string) map[string]any {
	m := verbatimContactMapping()
	m["data"].(map[string]any)["onAmbiguous"] = policy
	return m
}

// THE CASCADE. A mirror whose slot is declared `required:` has to mint out of
// an ambiguous probe, and `merge: union` used to copy the shared address onto
// the shell it minted, so the next contact carrying it saw three candidates,
// minted a fourth, and poisoned the address further with every sync. The shell
// is still minted (the write cannot land otherwise), but the address stays on
// the two people who held it.
func TestAnAmbiguousMintDoesNotCopyTheSharedAddress(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)
	const addr = "family@acme.com"
	a, b := twoAlexes(t, ds, addr)

	first := syncSource(t, ds, people, typeGoogleContact, "g-fam", map[string]any{
		"name": aname("The Family"), "emails": gemails(addr, "fam@other.example.com"),
	})
	shell := personOf(t, ds, first)
	if shell == a.ID || shell == b.ID {
		t.Fatalf("an ambiguous probe under a required slot linked %s", shell)
	}
	if got := holdersOf(t, ds, addr); len(got) != 2 {
		t.Fatalf("%s is held by %v, want only the two people who held it", addr, got)
	}
	// Everything else the contact says still lands.
	p := mustGet(t, ds, typePerson, shell)
	if p.Properties["name"] != "The Family" {
		t.Fatalf("the shell did not recompute: %v", p.Properties)
	}
	if got := holdersOf(t, ds, "fam@other.example.com"); len(got) != 1 || got[0] != shell {
		t.Fatalf("the unshared address is held by %v, want the shell", got)
	}
	// The withheld address stays legible: the contact's offer is an
	// alternative beside the stored value.
	full, err := ds.Get(context.Background(), typePerson, shell)
	if err != nil {
		t.Fatal(err)
	}
	if meta, ok := full.PropertyMeta["emails"]; !ok || len(meta.Alternatives) == 0 {
		t.Fatalf("no alternative carries the withheld address: %+v", full.PropertyMeta["emails"])
	}

	// The next contact on the same address sees the same two candidates, not
	// three: the address did not spread.
	second := syncSource(t, ds, people, typeGoogleContact, "g-fam2", map[string]any{
		"name": aname("Family Account"), "emails": gemails(addr),
	})
	if personOf(t, ds, second) == shell {
		t.Fatalf("the second contact linked the first one's shell through the shared address")
	}
	if got := holdersOf(t, ds, addr); len(got) != 2 {
		t.Fatalf("%s is held by %v after a second sync, want still two", addr, got)
	}
}

// A LINKED target does not gain a probed value another target holds either.
// A contact matched to Alex by its own address, that also lists the address
// Nina holds, leaves Nina's address on Nina alone.
func TestALinkedTargetDoesNotTakeAnAddressAnotherHolds(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)
	alex := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typePerson, Properties: map[string]any{"name": "Alex"},
	})
	nina := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typePerson, Properties: map[string]any{"name": "Nina", "emails": []any{"nina@example.com"}},
	})
	// Alex's address arrives through a source first, so the machine holds it.
	syncSource(t, ds, people, typeGoogleContact, "g-alex", map[string]any{
		"name": aname("Alex"), "emails": gemails("alex@example.com"),
	}, alex.ID)
	both := syncSource(t, ds, people, typeGoogleContact, "g-alex2", map[string]any{
		"name": aname("Alex"), "emails": gemails("alex@example.com", "nina@example.com"),
	}, alex.ID)
	if got := personOf(t, ds, both); got != alex.ID {
		t.Fatalf("linked %s, want alex", got)
	}
	if got := holdersOf(t, ds, "nina@example.com"); len(got) != 1 || got[0] != nina.ID {
		t.Fatalf("nina's address is held by %v, want nina alone", got)
	}
	if got := holdersOf(t, ds, "alex@example.com"); len(got) != 1 || got[0] != alex.ID {
		t.Fatalf("alex's address is held by %v", got)
	}
}

// A value the target ALREADY holds stays, even when another target holds it
// too. The rule stops a duplicate from spreading; it does not strip the ones
// that exist, which the owner settles with merge.
func TestAnAddressATargetAlreadyHoldsStays(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)
	const addr = "family@acme.com"
	// Alex's address arrives through a source while nobody else holds it.
	g := syncSource(t, ds, people, typeGoogleContact, "g-a", map[string]any{
		"name": aname("Alex"), "emails": gemails(addr),
	})
	a := personOf(t, ds, g)
	// The owner then writes the same address onto somebody else by hand.
	b := mustPut(t, ds, owner, substrate.PutInput{
		Kind: typePerson, Properties: map[string]any{"name": "Alexa", "emails": []any{addr}},
	})
	// Re-syncing Alex's contact keeps the address on Alex.
	syncSource(t, ds, people, typeGoogleContact, "g-a", map[string]any{
		"name": aname("Alex A."), "emails": gemails(addr),
	})
	if got := holdersOf(t, ds, addr); len(got) != 2 {
		t.Fatalf("%s is held by %v, want both %s and %s", addr, got, a, b.ID)
	}
	if p := mustGet(t, ds, typePerson, a); p.Properties["name"] != "Alex A." {
		t.Fatalf("the re-sync did not recompute: %v", p.Properties)
	}
}

// `park`, the default and the spelled value alike: several candidates mint
// nothing, the slot stays unset, and the source carries the ambiguity mark a
// list can ask for. Settling the ambiguity and re-syncing links it and clears
// the mark.
func TestOnAmbiguousParkMarksTheSource(t *testing.T) {
	t.Parallel()
	for _, policy := range []string{"", "park"} {
		t.Run("policy="+policy, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			_, ds := newDataset(t)
			mapping := verbatimContactMapping()
			if policy != "" {
				mapping = verbatimContactMappingOn(policy)
			}
			installSources(t, ds, []enginetest.Manifest{verbatimContactManifest()}, []map[string]any{mapping})
			const addr = "family@example.com"
			a, b := twoAlexes(t, ds, addr)

			shared := mustPut(t, ds, book, substrate.PutInput{
				Kind: typeVerbatimContact, ID: "c1",
				Properties: map[string]any{"names": gnames("The Family"), "emailAddresses": gaddresses(addr)},
			})
			if got := len(livePersons(t, ds)); got != 2 {
				t.Fatalf("%d persons, want 2", got)
			}
			if v := mustGet(t, ds, shared.Kind, shared.ID).Properties["person"]; v != nil {
				t.Fatalf("a parked source linked %v", v)
			}
			if got := ambiguousIDs(t, ds, typeVerbatimContact, true); len(got) != 1 || got[0] != "c1" {
				t.Fatalf("ambiguous sources = %v, want c1", got)
			}
			if got := ambiguousIDs(t, ds, typeVerbatimContact, false); len(got) != 0 {
				t.Fatalf("unmarked sources = %v, want none", got)
			}
			// A source that offers nothing is unresolved too, but it is not
			// ambiguous, and the mark says only the second.
			mustPut(t, ds, book, substrate.PutInput{
				Kind: typeVerbatimContact, ID: "c2", Properties: map[string]any{"resourceName": "people/c2"},
			})
			if got := ambiguousIDs(t, ds, typeVerbatimContact, true); len(got) != 1 {
				t.Fatalf("ambiguous sources = %v, want c1 alone", got)
			}

			if _, err := ds.Delete(ctx, owner, typePerson, b.ID, substrate.DeleteInput{}); err != nil {
				t.Fatalf("delete: %v", err)
			}
			resynced := mustPut(t, ds, book, substrate.PutInput{
				Kind: typeVerbatimContact, ID: "c1",
				Properties: map[string]any{
					"names": gnames("The Family"), "emailAddresses": gaddresses(addr), "nicknames": []any{"Fam"},
				},
			})
			if got := personOf(t, ds, resynced); got != a.ID {
				t.Fatalf("the resync linked %s, want %s", got, a.ID)
			}
			if got := ambiguousIDs(t, ds, typeVerbatimContact, true); len(got) != 0 {
				t.Fatalf("ambiguous sources = %v after the link, want none", got)
			}
		})
	}
}

// `oldest` links the candidate created first, and the address it shares
// stays where it was.
func TestOnAmbiguousOldestLinksTheFirstCreated(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installSources(t, ds, []enginetest.Manifest{verbatimContactManifest()},
		[]map[string]any{verbatimContactMappingOn("oldest")})
	const addr = "family@example.com"
	a, b := twoAlexes(t, ds, addr)

	for _, id := range []string{"c1", "c2"} {
		src := mustPut(t, ds, book, substrate.PutInput{
			Kind: typeVerbatimContact, ID: id,
			Properties: map[string]any{"names": gnames("The Family"), "emailAddresses": gaddresses(addr)},
		})
		if got := personOf(t, ds, src); got != a.ID {
			t.Fatalf("%s linked %s, want the older %s (not %s)", id, got, a.ID, b.ID)
		}
	}
	if got := len(livePersons(t, ds)); got != 2 {
		t.Fatalf("%d persons, want 2", got)
	}
	if got := ambiguousIDs(t, ds, typeVerbatimContact, true); len(got) != 0 {
		t.Fatalf("a linked source is marked ambiguous: %v", got)
	}
}

// `mint` mints a shell, as the engine did before #577, but the shell does not
// take the shared address: the next probe on it still sees two candidates.
func TestOnAmbiguousMintMintsWithoutTheSharedAddress(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installSources(t, ds, []enginetest.Manifest{verbatimContactManifest()},
		[]map[string]any{verbatimContactMappingOn("mint")})
	const addr = "family@example.com"
	a, b := twoAlexes(t, ds, addr)

	var shells []string
	for _, id := range []string{"c1", "c2"} {
		src := mustPut(t, ds, book, substrate.PutInput{
			Kind: typeVerbatimContact, ID: id,
			Properties: map[string]any{"names": gnames("The Family"), "emailAddresses": gaddresses(addr)},
		})
		pid := personOf(t, ds, src)
		if pid == a.ID || pid == b.ID {
			t.Fatalf("%s linked an existing person under mint", id)
		}
		shells = append(shells, pid)
	}
	if shells[0] == shells[1] {
		t.Fatalf("the second contact linked the first one's shell")
	}
	if got := holdersOf(t, ds, addr); len(got) != 2 {
		t.Fatalf("%s is held by %v, want only the two people who held it", addr, got)
	}
	if got := ambiguousIDs(t, ds, typeVerbatimContact, true); len(got) != 0 {
		t.Fatalf("a linked source is marked ambiguous: %v", got)
	}
}

// ambiguousIDs lists the records of a kind the ambiguity filter answers.
func ambiguousIDs(t *testing.T, ds substrate.Dataset, kind string, mark bool) []string {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{kind}, Ambiguous: &mark}, First: 100,
	})
	if err != nil {
		t.Fatalf("list ambiguous %s: %v", kind, err)
	}
	var out []string
	for _, r := range page.Records {
		out = append(out, r.ID)
	}
	return out
}

// THE MARK IS DERIVED. A rebuild replays a fold that carries no marks and
// reads them again from the records; a mapping change reads them again under
// the candidate, so a source kind whose mapping goes loses every mark.
func TestTheAmbiguityMarkIsDerivedAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	installVerbatimContacts(t, ds)
	const addr = "family@example.com"
	oldest, _ := twoAlexes(t, ds, addr)
	mustPut(t, ds, book, substrate.PutInput{
		Kind: typeVerbatimContact, ID: "c1",
		Properties: map[string]any{"names": gnames("The Family"), "emailAddresses": gaddresses(addr)},
	})
	if got := ambiguousIDs(t, ds, typeVerbatimContact, true); len(got) != 1 {
		t.Fatalf("ambiguous sources = %v, want c1", got)
	}

	rb, ok := svc.(rebuilder)
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	if _, err := rb.RebuildRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := ambiguousIDs(t, ds, typeVerbatimContact, true); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("ambiguous sources after a rebuild = %v, want c1", got)
	}

	// A mapping change that still parks c1 re-reads the mark for every
	// source of the kind, and the apply's backfill (decision record 0106)
	// probes c1 again and leaves it unlinked.
	edited := verbatimContactMappingOn("park")
	edited["data"].(map[string]any)["description"] = "Contacts project onto people."
	if err := enginetest.DeclareMappings(ctx, ds, edited); err != nil {
		t.Fatalf("declare the edited mapping: %v", err)
	}
	if got := ambiguousIDs(t, ds, typeVerbatimContact, true); len(got) != 1 || got[0] != "c1" {
		t.Fatalf("ambiguous sources after a mapping edit = %v, want c1 still", got)
	}

	// Switching the policy is an apply of the mapping, and that apply links
	// the unlinked sources under the new policy (decision record 0106): c1
	// takes the oldest Alex and its mark clears in the same transaction.
	if err := enginetest.DeclareMappings(ctx, ds, verbatimContactMappingOn("oldest")); err != nil {
		t.Fatalf("declare oldest: %v", err)
	}
	if got := personOf(t, ds, mustGet(t, ds, typeVerbatimContact, "c1")); got != oldest.ID {
		t.Fatalf("c1 names person %q, want the oldest Alex %q", got, oldest.ID)
	}
	if got := ambiguousIDs(t, ds, typeVerbatimContact, true); len(got) != 0 {
		t.Fatalf("ambiguous sources after the linking apply = %v, want none", got)
	}
}
