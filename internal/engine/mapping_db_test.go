package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The mapping fixtures. Source types arrive the way they arrive in
// production — installed by their connector at registration, shaped like the
// provider's payloads, with a recordmapping saying how each
// record's properties reach its person (§6.1, §7.1) — so these tests
// exercise the real path, not a hand-built registry.

const (
	googlePackage = "google.connectors.substrate.reamde.dev/google"
	slackPackage  = "slack.connectors.substrate.reamde.dev/slack"

	typeGoogleContact = googlePackage + "/contact"
	typeSlackUser     = slackPackage + "/slackuser"

	typePerson = "samples.substrate.reamde.dev/people/person"
)

// googleManifest mirrors the People API: a structured name, and
// repeated objects for what Google actually sends.
func googleManifest() enginetest.Manifest {
	return enginetest.Manifest{
		Name: "google.people", Authority: googlePackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(googlePackage, 1),
			vocabulary.ActorManifest(googlePackage, string(people)),
			vocabulary.KindManifest(googlePackage,
				map[string]any{"singular": "contact", "plural": "contacts"},
				map[string]any{
					"displayTemplate": "{name.displayName}",
					"properties": map[string]any{
						"name": map[string]any{"type": "object", "fields": map[string]any{
							"displayName": "string", "firstName": "string",
							"middleName": "string", "lastName": "string",
						}},
						"emails": map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
							"value": "email", "type": "string", "primary": "bool", "verified": "bool",
						}},
						"phones": map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
							"value": "string", "canonical": "string", "type": "string",
						}},
						"photoURL": map[string]any{"type": "url"},
						"etag":     map[string]any{"type": "string"},
						"raw":      map[string]any{"type": "json"},
						// The mapping's SUBJECT: a single, required reference
						// whose referent must exist, marked so the write path
						// can refuse a generic re-point without consulting the
						// mapping set.
						"person": map[string]any{
							"type": "reference", "kind": typePerson,
							"required": true, "mustExist": true, "subject": true,
						},
					},
				}),
		},
	}
}

func slackManifest() enginetest.Manifest {
	return enginetest.Manifest{
		Name: "slack", Authority: slackPackage,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(slackPackage, 1),
			vocabulary.ActorManifest(slackPackage, string(slack)),
			vocabulary.KindManifest(slackPackage,
				map[string]any{"singular": "slackuser", "plural": "slackusers"},
				map[string]any{
					"displayTemplate": "{displayName|realName}",
					"properties": map[string]any{
						"realName":    map[string]any{"type": "string"},
						"displayName": map[string]any{"type": "string"},
						"jobTitle":    map[string]any{"type": "string"},
						"timezone":    map[string]any{"type": "timezone"},
						"avatarURL":   map[string]any{"type": "url"},
						"email":       map[string]any{"type": "email"},
						"raw":         map[string]any{"type": "json"},
						"person": map[string]any{
							"type": "reference", "kind": typePerson,
							"required": true, "mustExist": true, "subject": true,
						},
					},
				}),
		},
	}
}

// installPeopleSources installs the google and slack source types AND the
// repository's own mappings onto person.
//
// The two halves are separate on purpose, because record 49 made them
// different authors: a provider ships mirror kinds with an empty subject slot,
// and the package that owns `person` is the only one that may say what
// projects onto it. Both mappings are therefore declared by
// samples.substrate.reamde.dev/people, under the owner's hand.
func installPeopleSources(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	for _, m := range []enginetest.Manifest{googleManifest(), slackManifest()} {
		if err := enginetest.Install(ctx, ds, substrate.ActorSystem, m); err != nil {
			t.Fatalf("register %s: %v", m.Name, err)
		}
	}
	if err := enginetest.DeclareMappings(ctx, ds, peopleMappings()...); err != nil {
		t.Fatalf("declare the person mappings: %v", err)
	}
}

// peopleMappings is what the repository declares: one mapping per provider
// mirror onto the person kind its own package owns.
func peopleMappings() []map[string]any {
	return []map[string]any{
		enginetest.PeopleMapping("contactperson", map[string]any{
			"from": typeGoogleContact, "property": "person",
			"match": []any{
				map[string]any{"from": "emails[].value", "to": "emails"},
				map[string]any{"from": "phones[].canonical", "to": "phones"},
			},
			"map": map[string]any{
				"name":   map[string]any{"path": "name.displayName"},
				"emails": map[string]any{"path": "emails[].value", "merge": "union"},
				"phones": map[string]any{"path": "phones[].canonical", "merge": "union"},
			},
		}),
		enginetest.PeopleMapping("slackuserperson", map[string]any{
			"from": typeSlackUser, "property": "person",
			"match": []any{map[string]any{"from": "email", "to": "emails"}},
			"map": map[string]any{
				"name":        map[string]any{"path": "realName"},
				"displayName": map[string]any{"path": "displayName"},
				"emails":      map[string]any{"path": "email", "merge": "union"},
			},
		}),
	}
}

// aname/gemails/gphones build the People-API-shaped object properties.
func aname(display string) map[string]any {
	return map[string]any{"displayName": display}
}

func gemails(addrs ...string) []any {
	out := make([]any, 0, len(addrs))
	for i, a := range addrs {
		out = append(out, map[string]any{"value": a, "type": "work", "primary": i == 0})
	}
	return out
}

func gphones(nums ...string) []any {
	out := make([]any, 0, len(nums))
	for _, n := range nums {
		out = append(out, map[string]any{"value": n, "canonical": n, "type": "mobile"})
	}
	return out
}

// syncSource is one connector sync of a source record: a put at the writer's
// OWN id (the encoded provider key), which is what makes a re-sync a
// primary-key upsert. `person` is left to match-or-create unless the caller
// names one.
func syncSource(t *testing.T, ds substrate.Dataset, actor substrate.Actor, typ, id string,
	props map[string]any, person ...string,
) *substrate.Record {
	t.Helper()
	in := substrate.PutInput{Kind: typ, ID: id, Properties: map[string]any{}}
	for k, v := range props {
		in.Properties[k] = v
	}
	for _, p := range person {
		// The subject is a pinned reference, so a bare id resolves.
		in.Properties["person"] = p
	}
	return mustPut(t, ds, actor, in)
}

// newConversation is a slack channel with the account its type requires.
func newConversation(t *testing.T, ds substrate.Dataset) *substrate.Record {
	t.Helper()
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}
	acc := mustPut(t, ds, owner, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "slack-acct-1",
		Properties: map[string]any{"provider": "slack", "label": "Work", "status": "ok"},
	})
	return mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversation", ID: "slack-C1",
		Properties: map[string]any{"category": "channel", "account": enginetest.AccountType + "/" + acc.ID},
	})
}

func personOf(t *testing.T, ds substrate.Dataset, src *substrate.Record) string {
	t.Helper()
	full := mustGet(t, ds, src.Kind, src.ID)
	kind, id, ok := vocabulary.SplitRecordPath(refPathValue(full, "person"))
	if !ok || kind != typePerson {
		t.Fatalf("%s names no person: %v", src.ID, full.Properties["person"])
	}
	return id
}

func livePersons(t *testing.T, ds substrate.Dataset) []*substrate.Record {
	t.Helper()
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"person"}}, First: 100,
	})
	if err != nil {
		t.Fatalf("list people: %v", err)
	}
	return page.Records
}

// A writer names what it owns: `metadata.id` is the upsert key, it survives a
// round trip, and a second put at the same id is the same record.
func TestWriterControlledIDRoundTrip(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	const id = "google:people-c123"
	first := syncSource(t, ds, people, typeGoogleContact, id, map[string]any{"name": aname("Alex")})
	if first.ID != id {
		t.Fatalf("id = %q, want the writer's own %q", first.ID, id)
	}
	got := mustGet(t, ds, typeGoogleContact, id)
	if got.ID != id {
		t.Fatalf("round trip = %+v", got)
	}
	if name, _ := got.Properties["name"].(map[string]any); name["displayName"] != "Alex" {
		t.Fatalf("object property round trip = %v", got.Properties["name"])
	}
	// The displayTemplate reads one level into the object property.
	if got.Properties["title"] != "Alex" {
		t.Fatalf("{name.displayName} did not derive the title: %v", got.Properties["title"])
	}
	// The same key is the same record — that IS idempotency.
	before := maxSeq(t, ds)
	again := syncSource(t, ds, people, typeGoogleContact, id, map[string]any{"name": aname("Alex")})
	if again.ID != id {
		t.Fatalf("re-sync minted %s", again.ID)
	}
	if rows := changesSince(t, ds, before); len(rows) != 0 {
		t.Fatalf("an identical re-sync wrote %d changelog rows: %+v", len(rows), rows)
	}
}

// A type some mapping points at is always server-assigned: nothing external
// names a person.
func TestSubjectTypeRejectsAClientID(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "person", ID: "person-i-named", Properties: map[string]any{"name": "Ada"},
	})
	if err == nil {
		t.Fatal("a writer must not name a person")
	}
	wantErr(t, err, substrate.ErrValidation, "client id on a subject type")

	// Server-assigned is the only way, and it works.
	p := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "Ada"}})
	if p.ID == "" || p.ID == "person-i-named" {
		t.Fatalf("person id = %q", p.ID)
	}
	// An id outside the alphabet is refused wherever it is legal to supply
	// one. A `/` is IN the alphabet (a declaration's id is a kind reference),
	// so the refusal is on a character that never travels in a path.
	if _, err := ds.Put(ctx, people, substrate.PutInput{
		Kind: typeGoogleContact, ID: "google people c1",
	}); err == nil {
		t.Fatal("an id carrying a space must be refused")
	}
}

// EXACTLY ONE candidate links (§6.2): a record arriving with an address the
// owner's person already carries attaches to that person instead of minting
// a duplicate — and fills only what nobody holds.
func TestMatchLinksSingleCandidate(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	sam := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Sam", "emails": []any{"sam@acme.com"}},
	})
	g := syncSource(t, ds, people, typeGoogleContact, "g-sam", map[string]any{
		"name":   map[string]any{"displayName": "Samuel Jones", "firstName": "Samuel", "lastName": "Jones"},
		"emails": gemails("sam@acme.com"),
		"phones": gphones("+441234567890"),
	})
	if got := personOf(t, ds, g); got != sam.ID {
		t.Fatalf("the record linked %s, want the matched person %s", got, sam.ID)
	}
	if n := len(livePersons(t, ds)); n != 1 {
		t.Fatalf("%d persons after a single-candidate match, want 1", n)
	}
	p := mustGet(t, ds, sam.Kind, sam.ID)
	// The owner's writes yield; the unheld properties fill.
	if p.Properties["name"] != "Sam" {
		t.Fatalf("recompute overwrote an owner-held name: %v", p.Properties["name"])
	}
	if got, _ := p.Properties["phones"].([]any); len(got) != 1 || got[0] != "+441234567890" {
		t.Fatalf("phones did not fill: %v", p.Properties["phones"])
	}
}

// A LATER probe decides when the earlier ones extract nothing: a contact with
// no email still matches by phone.
func TestMatchFallsThroughToTheNextProbe(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	p := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Nina", "phones": []any{"+301234567890"}},
	})
	g := syncSource(t, ds, people, typeGoogleContact, "g-nina", map[string]any{
		"name":   aname("Nina Ray"),
		"phones": gphones("+301234567890"),
	})
	if got := personOf(t, ds, g); got != p.ID {
		t.Fatalf("the phone probe should have matched %s, got %s", p.ID, got)
	}
}

// ZERO OR SEVERAL candidates mint a fresh subject (§6.2): a shared family
// address matching two people creates a third rather than guessing —
// ambiguity resolves in the console, by a human, with merge.
func TestAmbiguousMatchCreates(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	a := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alex", "emails": []any{"family@acme.com"}},
	})
	b := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alexa", "emails": []any{"family@acme.com"}},
	})
	g := syncSource(t, ds, people, typeGoogleContact, "g-fam", map[string]any{
		"name": aname("The Family"), "emails": gemails("family@acme.com"),
	})
	third := personOf(t, ds, g)
	if third == a.ID || third == b.ID {
		t.Fatalf("an ambiguous match guessed: linked %s", third)
	}
	if n := len(livePersons(t, ds)); n != 3 {
		t.Fatalf("%d persons, want 3", n)
	}
}

// A record with nothing shared — or nothing at all — still describes a
// person: a shell is minted and linked in the same transaction, with a
// server-assigned id, and takes its properties from the record it was born
// for.
func TestNoMatchCreatesShell(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	lonely := syncSource(t, ds, people, typeGoogleContact, "g-ned", map[string]any{
		"name": aname("Nameless Ned"),
	})
	shell := mustGet(t, ds, typePerson, personOf(t, ds, lonely))
	if shell.Kind != typePerson || shell.ID == lonely.ID {
		t.Fatalf("shell = %s (%s)", shell.ID, shell.Kind)
	}
	// Every shell is born demoted; states are never recomputed.
	if shell.Properties["prominence"] != "utility" {
		t.Fatalf("a shell should be born utility: %v", shell.Properties)
	}
	if shell.Properties["name"] != "Nameless Ned" {
		t.Fatalf("the shell did not recompute: %v", shell.Properties)
	}
	// The subject is one of the source record's own properties, so resolving it
	// travels in that record's own delta: the sync's put is the entry, and
	// `person` is among the properties it accepted.
	entries, err := ds.Changes(context.Background(), 0,
		substrate.ChangeFilter{Ops: []substrate.Op{substrate.OpPut}, RecordID: lonely.ID}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("the sync wrote %d entries for the source, want one: %+v", len(entries), entries)
	}
	named := false
	for _, p := range entries[0].Payload["properties"].([]any) {
		if p == "person" {
			named = true
		}
	}
	if !named {
		t.Fatalf("the entry does not name the subject it resolved: %+v", entries[0].Payload)
	}
}

// The Sam scenario, end to end (§7.1, records 51–52): a hand-written person,
// a matching Google contact that yields to the owner's holds, alternatives on
// the wire, release by null-patch, a second person for slack's sam, and a
// merge whose recompute still respects the owner — reversed exactly by split.
func TestSamScenario(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	sam := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Sam", "emails": []any{"sam@acme.com"}},
	})

	// Google matches by email, fills phones, and YIELDS on the owner's name
	// and emails.
	g := syncSource(t, ds, people, typeGoogleContact, "g-sam", map[string]any{
		"name":   map[string]any{"displayName": "Samuel Jones", "firstName": "Samuel", "lastName": "Jones"},
		"emails": gemails("sam@acme.com"),
		"phones": gphones("+441234567890"),
	})
	if got := personOf(t, ds, g); got != sam.ID {
		t.Fatalf("google's sam linked %s, want %s", got, sam.ID)
	}
	p := mustGet(t, ds, sam.Kind, sam.ID)
	if p.Properties["name"] != "Sam" {
		t.Fatalf("the owner's name did not survive the sync: %v", p.Properties["name"])
	}
	if got, _ := p.Properties["phones"].([]any); len(got) != 1 || got[0] != "+441234567890" {
		t.Fatalf("phones did not fill: %v", p.Properties["phones"])
	}
	// The yielded value is an ALTERNATIVE on the single-record read.
	meta := p.PropertyMeta
	if meta == nil {
		t.Fatalf("no propertyMeta on a managed person: %+v", p)
	}
	if meta["name"].Manager != string(owner) {
		t.Fatalf("name manager = %q, want the owner", meta["name"].Manager)
	}
	alts := meta["name"].Alternatives
	if len(alts) != 1 || alts[0].Actor != string(people) || alts[0].Value != "Samuel Jones" {
		t.Fatalf("name alternatives = %+v", alts)
	}
	// An accepted recompute records the WINNING SOURCE's actor, not system.
	if meta["phones"].Manager != string(people) {
		t.Fatalf("phones manager = %q, want %s", meta["phones"].Manager, people)
	}

	// Releasing a hand edit is a null-patch: the delete clears the value and
	// its manager, and the SAME transaction refills from live sources.
	released := mustPatch(t, ds, owner, sam.Kind, sam.ID, substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	})
	if released.Properties["name"] != "Samuel Jones" {
		t.Fatalf("release did not refill in the same transaction: %v", released.Properties["name"])
	}
	p = mustGet(t, ds, sam.Kind, sam.ID)
	if p.PropertyMeta["name"].Manager != string(people) {
		t.Fatalf("refilled manager = %q, want %s", p.PropertyMeta["name"].Manager, people)
	}
	if len(p.PropertyMeta["name"].Alternatives) != 0 {
		t.Fatalf("an offer equal to the stored value is not an alternative: %+v",
			p.PropertyMeta["name"].Alternatives)
	}

	// Slack's sam shares nothing: a second person, never a guess.
	s := syncSource(t, ds, slack, typeSlackUser, "s-sam", map[string]any{
		"realName": "Sam J", "displayName": "sam", "email": "sam@corp.example",
	})
	second := personOf(t, ds, s)
	if second == sam.ID {
		t.Fatalf("slack's sam matched across a different address")
	}

	// The owner decides they are one person. Managers migrate where the
	// winner lacks the property; recompute respects the owner's holds.
	rec, err := ds.Merge(ctx, owner, sam.Kind, sam.ID, second)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	merged := mustGet(t, ds, sam.Kind, sam.ID)
	if got, _ := merged.Properties["emails"].([]any); len(got) != 1 || got[0] != "sam@acme.com" {
		t.Fatalf("an owner-held union property must survive its own merge: %v", merged.Properties["emails"])
	}
	if merged.Properties["displayName"] != "sam" {
		t.Fatalf("the loser's displayName did not reach the winner: %v", merged.Properties["displayName"])
	}
	// Slack synced last, so within the machine tier its name wins.
	if merged.Properties["name"] != "Sam J" {
		t.Fatalf("latest source write should win the released name: %v", merged.Properties["name"])
	}
	// Slack's differing address shows as an alternative beside the hold.
	var slackOffer bool
	for _, alt := range merged.PropertyMeta["emails"].Alternatives {
		if alt.Actor == string(slack) {
			slackOffer = true
		}
	}
	if !slackOffer {
		t.Fatalf("emails alternatives = %+v", merged.PropertyMeta["emails"].Alternatives)
	}
	moved, _ := rec.Properties["moved"].(map[string]any)
	var migrated bool
	for _, m := range moved["managers"].([]any) {
		row, _ := m.(map[string]any)
		if row["property"] == "displayName" && row["actor"] == string(slack) {
			migrated = true
		}
	}
	if !migrated {
		t.Fatalf("the merge record should carry the migrated managers: %+v", moved["managers"])
	}

	// Split reverses it: the slack record and its managers go back, and both
	// sides recompute from the sources they now have.
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	if got := personOf(t, ds, s); got != second {
		t.Fatalf("split did not re-point the slack record: %s", got)
	}
	after := mustGet(t, ds, sam.Kind, sam.ID)
	if v, still := after.Properties["displayName"]; still {
		t.Fatalf("the winner kept the loser's displayName after split: %v", v)
	}
	if after.Properties["name"] != "Samuel Jones" {
		t.Fatalf("the winner should recompute from its own source: %v", after.Properties["name"])
	}
	loser := mustGet(t, ds, typePerson, second)
	if loser.Properties["displayName"] != "sam" || loser.DeletedAt != nil {
		t.Fatalf("the loser did not come back whole: %+v", loser.Properties)
	}
}

// Union properties recompute from live sources: a value removed at the
// provider disappears here, and what another source still asserts stays.
func TestUnionRecompute(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	g := syncSource(t, ds, people, typeGoogleContact, "g-1", map[string]any{
		"name": aname("Alex"), "emails": gemails("a@x.com", "b@x.com"),
	})
	pid := personOf(t, ds, g)
	syncSource(t, ds, slack, typeSlackUser, "s-1", map[string]any{
		"realName": "alex", "email": "c@x.com",
	}, pid)

	// Latest-first, deduped: slack synced last.
	if got, _ := mustGet(t, ds, typePerson, pid).Properties["emails"].([]any); len(got) != 3 ||
		got[0] != "c@x.com" || got[1] != "a@x.com" || got[2] != "b@x.com" {
		t.Fatalf("union = %v", got)
	}

	// Google stops asserting b@x.com: it goes; slack's stays.
	syncSource(t, ds, people, typeGoogleContact, "g-1", map[string]any{
		"name": aname("Alex"), "emails": gemails("a@x.com"),
	})
	if got, _ := mustGet(t, ds, typePerson, pid).Properties["emails"].([]any); len(got) != 2 ||
		got[0] != "a@x.com" || got[1] != "c@x.com" {
		t.Fatalf("union after removal = %v", got)
	}
}

// Within the machine tier the LATEST-UPDATED live source wins, per property —
// and an identical re-sync moves nothing, so "latest" means changed.
func TestLatestWriteWinsWithinMachineTier(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	g := syncSource(t, ds, people, typeGoogleContact, "g-1", map[string]any{"name": aname("Alexandros Papas")})
	pid := personOf(t, ds, g)
	syncSource(t, ds, slack, typeSlackUser, "s-1", map[string]any{"realName": "alex"}, pid)
	if got := mustGet(t, ds, typePerson, pid).Properties["name"]; got != "alex" {
		t.Fatalf("latest source = %v, want slack's", got)
	}
	// An identical google re-sync is a no-op: nothing moves.
	syncSource(t, ds, people, typeGoogleContact, "g-1", map[string]any{"name": aname("Alexandros Papas")})
	if got := mustGet(t, ds, typePerson, pid).Properties["name"]; got != "alex" {
		t.Fatalf("a no-op re-sync must not steal the property: %v", got)
	}
	// A CHANGED google sync is the latest write.
	syncSource(t, ds, people, typeGoogleContact, "g-1", map[string]any{"name": aname("Alexandros P.")})
	p := mustGet(t, ds, typePerson, pid)
	if p.Properties["name"] != "Alexandros P." {
		t.Fatalf("latest source = %v, want google's", p.Properties["name"])
	}
	if p.PropertyMeta["name"].Manager != string(people) {
		t.Fatalf("manager = %q, want the winning source's actor", p.PropertyMeta["name"].Manager)
	}
}

// A hand edit SURVIVES the sync now: the manager ledger is
// load-bearing, and recompute yields to anyone outside the machine tier.
func TestHandEditSurvivesTheSync(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	g := syncSource(t, ds, people, typeGoogleContact, "g-1", map[string]any{"name": aname("Alexandros Papas")})
	pid := personOf(t, ds, g)
	mustPatch(t, ds, owner, typePerson, pid, substrate.PatchInput{Properties: map[string]any{"name": "Alex"}})

	syncSource(t, ds, people, typeGoogleContact, "g-1", map[string]any{"name": aname("Alexandros P.")})
	p := mustGet(t, ds, typePerson, pid)
	if p.Properties["name"] != "Alex" {
		t.Fatalf("a fresher source overwrote the owner's hold: %v", p.Properties["name"])
	}
	alts := p.PropertyMeta["name"].Alternatives
	if len(alts) != 1 || alts[0].Value != "Alexandros P." {
		t.Fatalf("the fresher truth must be visible as an alternative: %+v", alts)
	}
}

// Re-syncing unchanged data recomputes the same values, which must write
// nothing at all: recompute flows through the ordinary no-op-suppressed write
// path, and unchanged offers stay put too.
func TestRecomputeIsSilentOnResync(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	props := map[string]any{
		"name": aname("Nina Ray"), "emails": gemails("nina@acme.com"),
	}
	syncSource(t, ds, people, typeGoogleContact, "g-c9", props)

	before := maxSeq(t, ds)
	for range 4 {
		syncSource(t, ds, people, typeGoogleContact, "g-c9", props)
	}
	if rows := changesSince(t, ds, before); len(rows) != 0 {
		t.Fatalf("4 identical re-syncs wrote %d changelog rows: %+v", len(rows), rows)
	}
}

// STATES ARE NEVER RECOMPUTED: the loader refuses a mapping that targets one,
// at registration, so no amount of syncing can promote a person.
func TestStatesAreNeverRecomputed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	const pkg = "promoter.connectors.substrate.reamde.dev/promoter"
	m := enginetest.Manifest{
		Name: "promoter", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.ActorManifest(pkg, "connector:promoter"),
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "promoterrow", "plural": "promoterrows"},
				map[string]any{
					"properties": map[string]any{
						"name":       map[string]any{"type": "string"},
						"prominence": map[string]any{"type": "string"},
						"person": map[string]any{
							"type": "reference", "kind": typePerson,
							"required": true, "mustExist": true, "subject": true,
						},
					},
				}),
		},
	}
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, m); err != nil {
		t.Fatalf("register the source kind: %v", err)
	}
	// The mapping is the people package's to declare (record 49), and this one
	// tries to write a state.
	promote := enginetest.PeopleMapping("promoterrowperson", map[string]any{
		"from": pkg + "/promoterrow", "property": "person",
		"map": map[string]any{
			"name":       map[string]any{"path": "name"},
			"prominence": map[string]any{"path": "prominence"},
		},
	})
	err := enginetest.DeclareMappings(ctx, ds, promote)
	if err == nil {
		t.Fatal("a mapping targeting a state must not register")
	}
	wantErr(t, err, substrate.ErrValidation, "state as a map target")

	// Without that rule the mapping installs, and a sync leaves the state
	// exactly where the declaration puts it.
	data, _ := promote["data"].(map[string]any)
	mp, _ := data["map"].(map[string]any)
	delete(mp, "prominence")
	if err := enginetest.DeclareMappings(ctx, ds, promote); err != nil {
		t.Fatalf("declare the mapping: %v", err)
	}
	row := mustPut(t, ds, substrate.Actor("connector:promoter"), substrate.PutInput{
		Kind: pkg + "/promoterrow", ID: "p1",
		Properties: map[string]any{"name": "Ada", "prominence": "known"},
	})
	person := mustGet(t, ds, typePerson, personOf(t, ds, row))
	if person.Properties["prominence"] != "utility" {
		t.Fatalf("a sync promoted a person: %v", person.Properties["prominence"])
	}
	if person.Properties["name"] != "Ada" {
		t.Fatalf("the ordinary property should still land: %v", person.Properties)
	}
}

// The subject is set when the record is created and moved only by merge
// and split.
func TestSubjectEdgeIsCreateTimeOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	g := syncSource(t, ds, people, typeGoogleContact, "g-c1", map[string]any{"name": aname("Alex")})
	pid := personOf(t, ds, g)
	other := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "Someone"}})

	// Re-asserting the same target is what every re-sync does.
	syncSource(t, ds, people, typeGoogleContact, "g-c1", map[string]any{"name": aname("Alex")}, pid)

	// Re-pointing it is not a write. The refusal is the same whichever generic
	// verb asks: put and patch both reach apply's subject guard.
	if _, err := ds.Put(ctx, people, substrate.PutInput{
		Kind: typeGoogleContact, ID: "g-c1",
		Properties: map[string]any{"person": other.ID},
	}); err == nil {
		t.Fatal("a put should not re-point a subject")
	} else {
		wantErr(t, err, substrate.ErrGuard, "split + merge")
	}
	if _, err := ds.Patch(ctx, people, typeGoogleContact, "g-c1", substrate.PatchInput{
		Properties: map[string]any{"person": other.ID},
	}); err == nil {
		t.Fatal("a patch should not re-point a subject")
	} else {
		wantErr(t, err, substrate.ErrGuard, "split + merge")
	}
	if got := personOf(t, ds, g); got != pid {
		t.Fatalf("the subject moved after all: %s", got)
	}
}

// ONE HOP (§6.2): a reference pinned at `person` accepts the slackuser the
// connector actually has, and the stored value lands on the person.
func TestOneHopResolution(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	s := syncSource(t, ds, slack, typeSlackUser, "s-U1", map[string]any{"realName": "alex"})
	pid := personOf(t, ds, s)
	conv := newConversation(t, ds)
	msg := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversationmessage", ID: "s-msg-1",
		Properties: map[string]any{
			"text": "hi", "at": "2026-08-03T10:00:00Z", "conversation": conv.ID,
			"author": vocabulary.RecordPath(slackPackage+"/slackuser", s.ID),
		},
	})
	author := refPathValue(mustGet(t, ds, msg.Kind, msg.ID), "author")
	if author != vocabulary.RecordPath(typePerson, pid) {
		t.Fatalf("author should resolve to the person, got %q", author)
	}
}

// A source record left unlinked — which only a bad migration can produce,
// since the loader makes the edge required — gets its subject the moment
// anything resolves through it.
func TestUnlinkedSourceGetsAShell(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, raw, _ := newDatasetWithDB(t)
	installPeopleSources(t, ds)

	g := syncSource(t, ds, people, typeGoogleContact, "g-c1", map[string]any{"name": aname("Alex")})
	pid := personOf(t, ds, g)

	// Strip the pointer behind the engine's back, as a bad migration would: the
	// property AND its projection in the refs index.
	if _, err := raw.ExecContext(ctx, `UPDATE records SET props = props - 'person' WHERE id = $1`, g.ID); err != nil {
		t.Fatalf("strip the subject: %v", err)
	}
	if _, err := raw.ExecContext(ctx, `DELETE FROM refs WHERE src = $1 AND property = 'person'`, g.ID); err != nil {
		t.Fatalf("strip the subject from the index: %v", err)
	}
	if _, err := ds.Delete(ctx, owner, typePerson, pid); err != nil {
		t.Fatalf("delete the orphaned person: %v", err)
	}

	// Resolving through the record links a new shell in line.
	msg := mustPut(t, ds, slack, substrate.PutInput{
		Kind: "conversationmessage", ID: "s-msg-1",
		Properties: map[string]any{
			"text": "hi", "at": "2026-08-03T10:00:00Z",
			"conversation": newConversation(t, ds).ID,
			"author":       vocabulary.RecordPath(googlePackage+"/contact", g.ID),
		},
	})
	authorKind, authorID, ok := vocabulary.SplitRecordPath(
		refPathValue(mustGet(t, ds, msg.Kind, msg.ID), "author"))
	if !ok {
		t.Fatalf("author did not resolve at all")
	}
	shell := mustGet(t, ds, authorKind, authorID)
	if shell.Kind != typePerson || shell.ID == pid {
		t.Fatalf("expected a fresh shell person, got %s (%s)", shell.ID, shell.Kind)
	}
	if got := personOf(t, ds, g); got != shell.ID {
		t.Fatalf("the record was not linked to its shell: %s", got)
	}
	if shell.Properties["prominence"] != "utility" {
		t.Fatalf("a shell should be born utility: %v", shell.Properties)
	}
	if shell.Properties["name"] != "Alex" {
		t.Fatalf("the shell did not recompute: %v", shell.Properties)
	}
}

// The owner deleting a person must not maim the records that pointed at it:
// they keep their own ids, the next sync succeeds, and it links a fresh
// subject through the ordinary match-or-create path.
func TestDeletedTargetDoesNotStripItsSources(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	src := syncSource(t, ds, people, typeGoogleContact, "g-c1", map[string]any{"name": aname("Alex")})
	gone := personOf(t, ds, src)
	if _, err := ds.Delete(ctx, owner, typePerson, gone); err != nil {
		t.Fatalf("delete: %v", err)
	}

	again := syncSource(t, ds, people, typeGoogleContact, "g-c1", map[string]any{"name": aname("Alex")})
	if again.ID != src.ID {
		t.Fatalf("the record was re-minted as %s", again.ID)
	}
	fresh := personOf(t, ds, again)
	if fresh == gone {
		t.Fatalf("still pointing at the deleted person %s", gone)
	}
	if got := mustGet(t, ds, typePerson, fresh); got.DeletedAt != nil || got.Properties["name"] != "Alex" {
		t.Fatalf("re-linked person = %+v", got)
	}
}

// Deleting a source record recomputes its subject — release-by-omission all
// the way down: with ZERO live sources the person keeps only what was
// written to it directly.
func TestDeletingASourceRecomputes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	g := syncSource(t, ds, people, typeGoogleContact, "g-c1", map[string]any{
		"name": aname("Alex"), "phones": gphones("+441234567890"),
	})
	pid := personOf(t, ds, g)
	syncSource(t, ds, slack, typeSlackUser, "s-U1", map[string]any{"realName": "alex"}, pid)
	mustPatch(t, ds, owner, typePerson, pid, substrate.PatchInput{Properties: map[string]any{"displayName": "Al"}})

	if _, err := ds.Delete(ctx, owner, g.Kind, g.ID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	person := mustGet(t, ds, typePerson, pid)
	if v, still := person.Properties["phones"]; still {
		t.Fatalf("phones outlived the record that carried them: %v", v)
	}
	if person.Properties["name"] != "alex" {
		t.Fatalf("the surviving record should own the name: %v", person.Properties)
	}

	// The last source goes too: the machine-held properties go with it, and
	// the owner's own write stays.
	if _, err := ds.Delete(ctx, owner, typeSlackUser, mustGet(t, ds, typeSlackUser, "s-U1").ID); err != nil {
		t.Fatalf("delete source: %v", err)
	}
	person = mustGet(t, ds, typePerson, pid)
	if v, still := person.Properties["name"]; still {
		t.Fatalf("a sourceless person keeps only direct writes, got name=%v", v)
	}
	if person.Properties["displayName"] != "Al" {
		t.Fatalf("the owner's own write must survive: %v", person.Properties)
	}
	if person.DeletedAt != nil {
		t.Fatal("deleting a source must not delete what it described")
	}
}

// A source record restored from a tombstone recomputes again, onto the SAME
// record it always described.
func TestResurrectedSourceRecomputes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	src := syncSource(t, ds, people, typeGoogleContact, "g-c1", map[string]any{"name": aname("Alex")})
	pid := personOf(t, ds, src)

	// The contact goes to Trash: its contributions go with it.
	if _, err := ds.Delete(ctx, people, src.Kind, src.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := mustGet(t, ds, typePerson, pid); got.DeletedAt != nil || got.Properties["name"] != nil {
		t.Fatalf("after the source's delete: %+v", got.Properties)
	}

	// …and comes back. The re-sync restores the record, the person keeps its
	// id, and recompute resumes.
	back := syncSource(t, ds, people, typeGoogleContact, "g-c1", map[string]any{"name": aname("Alexandros")})
	if back.ID != src.ID || back.DeletedAt != nil {
		t.Fatalf("restore = %s deletedAt=%v", back.ID, back.DeletedAt)
	}
	if got := personOf(t, ds, back); got != pid {
		t.Fatalf("the restored record points at %s, want the person it always had (%s)", got, pid)
	}
	if got := mustGet(t, ds, typePerson, pid).Properties["name"]; got != "Alexandros" {
		t.Fatalf("recompute did not resume: %v", got)
	}
}

// A nested merge followed by an out-of-order split must never leave a source
// record naming two subjects.
func TestNestedMergeSplitKeepsOneSubject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	src := syncSource(t, ds, slack, typeSlackUser, "s-U1", map[string]any{"realName": "Ada"})
	a := personOf(t, ds, src)
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "B"}})
	c := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "C"}})

	m1, err := ds.Merge(ctx, owner, b.Kind, b.ID, a)
	if err != nil {
		t.Fatalf("merge a into b: %v", err)
	}
	// NOTHING REPOINTS: the source still names the person it was resolved to,
	// and that id resolves forward through the former-id trail on read.
	if got := personOf(t, ds, src); got != a {
		t.Fatalf("the merge rewrote the source's subject: %s", got)
	}
	if got := mustGet(t, ds, typePerson, a).CanonicalID; got != b.ID {
		t.Fatalf("the source's subject resolves to %q, want the winner %s", got, b.ID)
	}
	m2, err := ds.Merge(ctx, owner, c.Kind, c.ID, b.ID)
	if err != nil {
		t.Fatalf("merge b into c: %v", err)
	}
	if got := personOf(t, ds, src); got != a {
		t.Fatalf("the nested merge rewrote the source's subject: %s", got)
	}

	// Split the OUTER merge first — the order nothing forbids.
	if _, err := ds.Split(ctx, owner, m1.ID); err != nil {
		t.Fatalf("split the first merge: %v", err)
	}
	// personOf fails outright on a second value.
	first := personOf(t, ds, src)
	if _, err := ds.Split(ctx, owner, m2.ID); err != nil {
		t.Fatalf("split the nested merge: %v", err)
	}
	second := personOf(t, ds, src)
	t.Logf("targets after out-of-order splits: %s then %s", first, second)
	// And a re-sync still works, whichever person it ended on: one value, no
	// guard.
	syncSource(t, ds, slack, typeSlackUser, "s-U1", map[string]any{"realName": "Ada"})
}

// Concurrent resolution of the same unlinked record mints one shell, not one
// per writer: the record's lock serializes them and the partial unique index
// is what makes it a guarantee.
func TestConcurrentShellBirthMintsOneShell(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, raw, _ := newDatasetWithDB(t)
	installPeopleSources(t, ds)

	src := syncSource(t, ds, people, typeGoogleContact, "g-c1", map[string]any{"name": aname("Alex")})
	// Strip the subject behind the engine's back, as a bad migration would: the
	// property AND its projection in the refs index.
	if _, err := raw.ExecContext(ctx, `UPDATE records SET props = props - 'person' WHERE id = $1`, src.ID); err != nil {
		t.Fatalf("strip the subject: %v", err)
	}
	if _, err := raw.ExecContext(ctx, `DELETE FROM refs WHERE src = $1 AND property = 'person'`, src.ID); err != nil {
		t.Fatalf("strip the subject from the index: %v", err)
	}
	conv := newConversation(t, ds)

	errs := make(chan error, 2)
	for i := range 2 {
		go func() {
			_, err := ds.Put(ctx, slack, substrate.PutInput{
				Kind: "conversationmessage", ID: "s-msg-" + string(rune('a'+i)),
				Properties: map[string]any{
					"text": "hi", "conversation": conv.ID,
					"author": vocabulary.RecordPath(googlePackage+"/contact", src.ID),
				},
			})
			errs <- err
		}()
	}
	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent resolution: %v", err)
		}
	}
	var rows int
	if err := raw.QueryRowContext(ctx,
		`SELECT count(*) FROM refs WHERE src = $1 AND property = 'person'`, src.ID).Scan(&rows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if rows != 1 {
		t.Fatalf("%d subject rows after concurrent shell birth", rows)
	}
}

// Object properties validate recursively on write: undeclared
// fields are rejected, a null field is dropped from the stored object, and
// each field coerces with the scalar rules.
func TestObjectPropertyValidation(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	// An undeclared field is a validation error, named.
	if _, err := ds.Put(ctx, people, substrate.PutInput{
		Kind: typeGoogleContact, ID: "g-bad-1",
		Properties: map[string]any{"name": map[string]any{"display": "Alex"}},
	}); err == nil {
		t.Fatal("an undeclared field must be rejected")
	} else {
		wantErr(t, err, substrate.ErrValidation, "undeclared object field")
	}
	// A field coerces by its declared kind — inside a repeated object too.
	if _, err := ds.Put(ctx, people, substrate.PutInput{
		Kind: typeGoogleContact, ID: "g-bad-2",
		Properties: map[string]any{"emails": []any{map[string]any{"value": "not-an-email"}}},
	}); err == nil {
		t.Fatal("a bad field value must be rejected")
	} else {
		wantErr(t, err, substrate.ErrValidation, "bad field value")
	}
	// A repeated object is a LIST of objects.
	if _, err := ds.Put(ctx, people, substrate.PutInput{
		Kind: typeGoogleContact, ID: "g-bad-3",
		Properties: map[string]any{"emails": map[string]any{"value": "a@x.com"}},
	}); err == nil {
		t.Fatal("a repeated object takes a list")
	}
	// A field explicitly null is dropped from the stored object.
	g := mustPut(t, ds, people, substrate.PutInput{
		Kind: typeGoogleContact, ID: "g-ok",
		Properties: map[string]any{
			"name": map[string]any{"displayName": "Alex", "firstName": nil},
		},
	})
	name, _ := mustGet(t, ds, g.Kind, g.ID).Properties["name"].(map[string]any)
	if name["displayName"] != "Alex" {
		t.Fatalf("stored object = %v", name)
	}
	if _, has := name["firstName"]; has {
		t.Fatalf("a null field must be dropped, got %v", name)
	}

	// Fields nest to vocabulary.MaxFieldDepth — a kind's own property is level 1
	// — and one level past it is a LOAD error, because the narrowing guards walk
	// exactly that many jsonb notches.
	const pkg = "nested.connectors.substrate.reamde.dev/nested"
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "nested", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.ActorManifest(pkg, "connector:nested"),
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "row", "plural": "rows"},
				map[string]any{"properties": map[string]any{
					"outer": map[string]any{"type": "object", "fields": map[string]any{
						"inner": map[string]any{"type": "object", "fields": map[string]any{
							"leaf": "string",
						}},
					}},
				}}),
		},
	}); err != nil {
		t.Fatalf("an object nested inside an object must register: %v", err)
	}
	const deep = "toodeep.connectors.substrate.reamde.dev/toodeep"
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "toodeep", Authority: deep,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(deep, 1),
			vocabulary.ActorManifest(deep, "connector:toodeep"),
			vocabulary.KindManifest(deep,
				map[string]any{"singular": "row", "plural": "rows"},
				map[string]any{"properties": map[string]any{
					"l1": map[string]any{"type": "object", "fields": map[string]any{
						"l2": map[string]any{"type": "object", "fields": map[string]any{
							"l3": map[string]any{"type": "object", "fields": map[string]any{
								"l4": map[string]any{"type": "object", "fields": map[string]any{
									"l5": "string",
								}},
							}},
						}},
					}},
				}}),
		},
	}); err == nil {
		t.Fatal("a level-5 field must not register")
	} else {
		wantErr(t, err, substrate.ErrValidation, "fields nest")
	}
}

// `title` is a legal map target through its storage column, and `body` is one
// through the declared, column-backed property a kind carries (#68), so a
// source's text reaches the work it describes.
func TestHotMapTargets(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	const pkg = "library.connectors.substrate.reamde.dev/library"
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, enginetest.Manifest{
		Name: "library", Authority: pkg,
		Manifests: []map[string]any{
			vocabulary.PackageManifest(pkg, 1),
			vocabulary.ActorManifest(pkg, "connector:library"),
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "work", "plural": "works"},
				map[string]any{"properties": map[string]any{
					"subtitle": map[string]any{"type": "string"},
					"body":     map[string]any{"type": "text"},
				}}),
			vocabulary.KindManifest(pkg,
				map[string]any{"singular": "libraryrow", "plural": "libraryrows"},
				map[string]any{
					"traits": []any{"temporal(point)"},
					"properties": map[string]any{
						"subtitle": map[string]any{"type": "string"},
						"body":     map[string]any{"type": "text"},
						"work": map[string]any{
							"type": "reference", "kind": pkg + "/work",
							"required": true, "mustExist": true, "subject": true,
						},
					},
				}),
			vocabulary.MappingManifest(pkg, "libraryrowwork", map[string]any{
				"from": pkg + "/libraryrow", "to": pkg + "/work", "property": "work",
				"map": map[string]any{
					"title":    map[string]any{"path": "title"},
					"body":     map[string]any{"path": "body"},
					"subtitle": map[string]any{"path": "subtitle"},
				},
			}),
		},
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	lib := substrate.Actor("connector:library")
	row := mustPut(t, ds, lib, substrate.PutInput{
		Kind: pkg + "/libraryrow", ID: "lib:1",
		Properties: map[string]any{
			"title": "Piranesi", "body": "a house of statues",
			"at": "2020-09-15T00:00:00Z", "subtitle": "a novel",
		},
	})
	full := mustGet(t, ds, row.Kind, row.ID)
	workKind, workID, _ := vocabulary.SplitRecordPath(refPathValue(full, "work"))
	work := mustGet(t, ds, workKind, workID)
	if work.Properties["title"] != "Piranesi" {
		t.Fatalf("title did not land: %v", work.Properties["title"])
	}
	if work.Properties["body"] != "a house of statues" {
		t.Fatalf("body did not land: %v", work.Properties["body"])
	}
	if work.Properties["subtitle"] != "a novel" {
		t.Fatalf("the declared property did not land: %v", work.Properties)
	}
	// Clearing the source's title clears the target's too.
	mustPatch(t, ds, lib, row.Kind, row.ID, substrate.PatchInput{Properties: map[string]any{"title": nil}})
	if v, still := mustGet(t, ds, work.Kind, work.ID).Properties["title"]; still {
		t.Fatalf("the title outlived its only source: %v", v)
	}
}

// Re-registering a connector whose manifest CHANGED validates it: the authority is
// already loaded, so nothing swaps in until the next repository-open — which is
// exactly why a payload that would not load then must not be stored now.
func TestReRegistrationValidatesTheChangedManifest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installPeopleSources(t, ds)

	// `name.displayName` maps onto person.name, a string: an int source
	// field fails the path type-check.
	broken := googleManifest()
	data, _ := broken.Manifests[2]["data"].(map[string]any)
	props, _ := data["properties"].(map[string]any)
	name, _ := props["name"].(map[string]any)
	fields, _ := name["fields"].(map[string]any)
	fields["displayName"] = "int"
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, broken); err == nil {
		t.Fatal("a manifest that cannot load must not register")
	} else {
		wantErr(t, err, substrate.ErrValidation, "changed manifest")
	}

	// A reference pinned at a mapped source type is refused the same way:
	// resolution stays one hop deep.
	alsoBroken := googleManifest()
	data, _ = alsoBroken.Manifests[2]["data"].(map[string]any)
	alsoProps, _ := data["properties"].(map[string]any)
	alsoProps["friend"] = map[string]any{"type": "reference", "kind": "contact"}
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, alsoBroken); err == nil {
		t.Fatal("a reference at a mapped source type must not register")
	}

	// And the good one still re-registers.
	if err := enginetest.Install(ctx, ds, substrate.ActorSystem, googleManifest()); err != nil {
		t.Fatalf("re-registering the unchanged manifest: %v", err)
	}
}

// Prominence demotes in search and nowhere else: a `utility` person ranks
// below every `known` match, however well it scores.
func TestSearchDemotesUtilityPersons(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	utility := mustPut(t, ds, people, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Zorionak Zorionak Zorionak"},
	})
	known := mustPut(t, ds, people, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Zorionak"},
	})
	if s := mustGet(t, ds, utility.Kind, utility.ID).Properties["prominence"]; s != "utility" {
		t.Fatalf("born %q, want utility", s)
	}
	mustPatch(t, ds, people, known.Kind, known.ID, substrate.PatchInput{
		Properties: map[string]any{"prominence": "known"},
	})

	hits, err := ds.Search(ctx, substrate.SearchInput{Q: "Zorionak", Mode: substrate.SearchLexical})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if len(hits) != 2 {
		t.Fatalf("expected both people, got %d", len(hits))
	}
	if hits[0].Record.ID != known.ID {
		t.Fatalf("a utility person outranked a known one: %+v", hits[0].Record.Properties)
	}
	// A type with no prominence machine is never demoted by it.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "organization", Properties: map[string]any{"name": "Zorionak Ltd"},
	})
	hits, err = ds.Search(ctx, substrate.SearchInput{
		Q: "Zorionak", Mode: substrate.SearchLexical, Kinds: []string{"organization", "person"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) < 3 || hits[len(hits)-1].Record.ID != utility.ID {
		t.Fatalf("the utility person should sit last: %+v", ids(recordsOf(hits)))
	}
}

func recordsOf(hits []substrate.Hit) []*substrate.Record {
	out := make([]*substrate.Record, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.Record)
	}
	return out
}

// Deletion needs no declaration: a null addresses any
// stored property — a schema that stops declaring one must still be able to
// remove its values — while an undeclared VALUE stays refused.
func TestUndeclaredNullDeletes(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	ctx := context.Background()
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "task", Properties: map[string]any{"name": "t"},
	})
	if _, err := ds.Patch(ctx, owner, task.Kind, task.ID, substrate.PatchInput{
		Properties: map[string]any{"neverDeclared": "x"},
	}); err == nil {
		t.Fatal("an undeclared value must be refused")
	}
	// A null for a property the row never carried is a no-op, not an error.
	e, err := ds.Patch(ctx, owner, task.Kind, task.ID, substrate.PatchInput{
		Properties: map[string]any{"neverDeclared": nil},
	})
	if err != nil {
		t.Fatalf("undeclared null: %v", err)
	}
	if _, has := e.Properties["neverDeclared"]; has {
		t.Fatalf("no-op null materialized a property: %v", e.Properties)
	}
}
