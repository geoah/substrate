package engine

// The Google contact-id migration (codex sdk review #5; fleet review Claude
// P0-2, Codex H2/H6): the deployed bundle composed contact ids as
// account+"-"+resourceName — non-injective (account `a` + resource
// `people/b-people-c` collides with account `a-people-b` + resource
// `people/c`) and unbounded against the 128-char id cap. The sync now
// composes host.ids.external("google-contacts", account, resourceName), and
// the bundle ships contactsidmigration — a BOUNDED callable that re-keys the
// rows a pre-SDK-id install holds, one {limit, cursor} batch per call, the
// way the operator drives it: loop until nextCursor is empty.
//
// Three tests against the shipped closure:
//
//  1. TestGoogleContactsIDMigration — the batched end-to-end: contacts seeded
//     under the OLD ids exactly as the deployed sync wrote them (no person
//     edge — the mapping matched or minted), MORE contacts than the batch
//     limit, the loop driven until nextCursor comes back empty. Covers the
//     three person-continuity shapes (email-matched, EMAIL-LESS — the shell
//     hazard — and cross-account shared-email), then: the same logical
//     contacts under the NEW ids, the SAME persons (none re-minted, none
//     orphaned), old ids tombstoned, an idempotent re-run, and a
//     post-migration sync upsert landing on the migrated row.
//
//  2. TestGoogleContactsIDMigrationAbsorbedMerge — the absorbed-orphan fix
//     (Codex H2): a sync raced the migration and already wrote the contact
//     under its new id. Email-less case: the raced row re-minted a fresh
//     person shell; the migration must fold that shell into the ORIGINAL
//     person by merge (the original may carry owner edits, so it wins) — one
//     person survives, the shell resolves onto it. Email-matched case: the
//     raced row resolved the SAME person; plain absorb, no merge.
//
//  3. TestGoogleContactsIDMigrationSkipPaths — the refuse-to-clobber paths:
//     a target tombstoned under the batch (the contact was deleted upstream
//     after the upgrade) is neither resurrected nor swept — counted skipped,
//     nothing written, stable across re-runs; likewise a row with no
//     provider key to derive an id from.

import (
	"context"
	"os/exec"
	"testing"

	"github.com/geoah/substrate/internal/runner/substratefn"
	"github.com/geoah/substrate/internal/substrate"
)

// oldGoogleContactID is the DEPLOYED composition this migration retires:
// account + "-" + resourceName with '/' folded to '-'.
func oldGoogleContactID(account, resourceName string) string {
	return account + "-" + "people-" + resourceName[len("people/"):]
}

// newGoogleContactID mirrors the sync body's host.ids.external through the Go
// SDK's pure twin — the id-vector tests hold the two runtimes byte-identical,
// so this is exactly the id the python migration and the re-keyed sync mint.
func newGoogleContactID(account, resourceName string) string {
	return substratefn.ExternalID("google-contacts", account, resourceName)
}

// migrationHarness installs the google closure into a fresh repository and hands
// back the seed/read/call helpers the three migration tests share.
type migrationHarness struct {
	t   *testing.T
	ctx context.Context
	ds  *dataset
}

func newMigrationHarness(t *testing.T) *migrationHarness {
	t.Helper()
	if testing.Short() {
		t.Skip("db test")
	}
	if _, err := exec.LookPath("uv"); err != nil {
		t.Skip("uv not on PATH — the closure's sync body warms through uv at install")
	}
	ctx := context.Background()
	ds := openInternalDataset(t)
	vocabularyDocs := loadYAMLDocs(t, googleExampleDir+"/bundle.yaml")
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, vocabularyDocs); err != nil {
		if isUVProvisionError(err) {
			t.Skipf("bundle install could not warm the PEP 723 body (uv offline?): %v", err)
		}
		t.Fatalf("install the google bundle: %v", err)
	}
	return &migrationHarness{t: t, ctx: ctx, ds: ds}
}

// seed writes one contact row the way the DEPLOYED sync wrote it: an explicit
// id, no person edge (the mapping matches or mints).
func (h *migrationHarness) seed(id, account, resourceName, display string, emails []string) string {
	h.t.Helper()
	props := map[string]any{
		"account": account,
		"name":    map[string]any{"displayName": display},
	}
	if resourceName != "" {
		props["resourceName"] = resourceName
	}
	if len(emails) > 0 {
		items := make([]any, 0, len(emails))
		for _, v := range emails {
			items = append(items, map[string]any{"value": v})
		}
		props["emails"] = items
	}
	if _, err := h.ds.Put(h.ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: googleContactType, ID: id, Properties: props,
	}); err != nil {
		h.t.Fatalf("seed %s: %v", id, err)
	}
	return id
}

// seedOld seeds a contact under the OLD id composition.
func (h *migrationHarness) seedOld(account, resourceName, display string, emails []string) string {
	h.t.Helper()
	return h.seed(oldGoogleContactID(account, resourceName), account, resourceName, display, emails)
}

// personOf returns the single person the contact's subject edge names.
func (h *migrationHarness) personOf(id string) string {
	h.t.Helper()
	e, err := h.ds.Get(h.ctx, googleContactType, id)
	if err != nil {
		h.t.Fatalf("get %s: %v", id, err)
	}
	targets := e.Edges["person"]
	if len(targets) != 1 {
		h.t.Fatalf("%s carries %d person edges, want 1", id, len(targets))
	}
	return targets[0].ID
}

// migrationResult is one call's decoded output.
type migrationResult struct {
	migrated, absorbed, skipped, applied int
	nextCursor                           string
}

// call invokes the migration once with the given input.
func (h *migrationHarness) call(input map[string]any) migrationResult {
	h.t.Helper()
	out, applied, err := h.ds.CallFunction(h.ctx, googleMigrationFn, input)
	if err != nil {
		h.t.Fatalf("call %s with %v: %v", googleMigrationFn, input, err)
	}
	m, _ := out.(map[string]any)
	num := func(key string) int {
		f, ok := m[key].(float64)
		if !ok {
			h.t.Fatalf("output %q missing or not a number: %#v", key, out)
		}
		return int(f)
	}
	cursor, ok := m["nextCursor"].(string)
	if !ok {
		h.t.Fatalf("output nextCursor missing or not a string: %#v", out)
	}
	return migrationResult{
		migrated: num("migrated"), absorbed: num("absorbed"),
		skipped: num("skipped"), applied: applied, nextCursor: cursor,
	}
}

// drain drives the operator loop: call with `limit`, feed nextCursor back
// until it comes back empty, and return the summed counts.
func (h *migrationHarness) drain(limit int) migrationResult {
	h.t.Helper()
	var total migrationResult
	input := map[string]any{"limit": limit}
	for calls := 0; ; calls++ {
		if calls > 16 {
			h.t.Fatalf("migration loop did not drain after %d calls", calls)
		}
		res := h.call(input)
		total.migrated += res.migrated
		total.absorbed += res.absorbed
		total.skipped += res.skipped
		total.applied += res.applied
		if res.nextCursor == "" {
			return total
		}
		input = map[string]any{"limit": limit, "cursor": res.nextCursor}
	}
}

func TestGoogleContactsIDMigration(t *testing.T) {
	t.Parallel()
	h := newMigrationHarness(t)
	ctx, ds := h.ctx, h.ds

	const (
		acctA = "workacct"
		acctB = "homeacct"
	)
	oldMatched := h.seedOld(acctA, "people/c1", "Ada Lovelace", []string{"ada@example.com"})
	oldShell := h.seedOld(acctA, "people/c2", "No Email Contact", nil)
	oldShared := h.seedOld(acctB, "people/c1", "Ada (home book)", []string{"ada@example.com"})

	pMatched := h.personOf(oldMatched)
	pShell := h.personOf(oldShell)
	pShared := h.personOf(oldShared)
	if pShared != pMatched {
		t.Fatalf("the shared-email contact minted its own person (%s vs %s) — the match probe should have linked it", pShared, pMatched)
	}
	if pShell == pMatched {
		t.Fatalf("the email-less contact linked to the matched person — it should have minted a shell")
	}
	if got := countLiveOf(t, ds, googlePersonType); got != 2 {
		t.Fatalf("live persons before migration = %d, want 2", got)
	}
	adaPerson, err := ds.Get(ctx, googlePersonType, pMatched)
	if err != nil {
		t.Fatalf("get person %s: %v", pMatched, err)
	}
	wantDisplay := adaPerson.Properties["displayName"]
	if wantDisplay == nil {
		t.Fatalf("the mapping carried no displayName onto person %s", pMatched)
	}

	// --- the migration, driven as the operator drives it: 3 contacts, batch
	// limit 2, so ONE call cannot finish — the first call must hand back a
	// non-empty nextCursor, and the loop drains the rest (Claude P0-2 /
	// Codex H6: the batch is what keeps a book's effects out of one frame).
	first := h.call(map[string]any{"limit": 2})
	if first.migrated != 2 || first.absorbed != 0 || first.skipped != 0 {
		t.Fatalf("first batch = %+v, want migrated=2 absorbed=0 skipped=0", first)
	}
	if first.applied != 4 {
		t.Fatalf("first batch applied %d effects, want 4 (2 re-puts + 2 tombstones)", first.applied)
	}
	if first.nextCursor == "" {
		t.Fatalf("first batch of a 3-contact book at limit 2 returned an empty nextCursor — the loop would stop early")
	}
	rest := migrationResult{}
	input := map[string]any{"limit": 2, "cursor": first.nextCursor}
	for calls := 0; ; calls++ {
		if calls > 8 {
			t.Fatalf("migration loop did not drain after %d calls", calls)
		}
		res := h.call(input)
		rest.migrated += res.migrated
		rest.absorbed += res.absorbed
		rest.skipped += res.skipped
		rest.applied += res.applied
		if res.nextCursor == "" {
			break
		}
		input = map[string]any{"limit": 2, "cursor": res.nextCursor}
	}
	if got := first.migrated + rest.migrated; got != 3 {
		t.Fatalf("loop migrated = %d, want 3", got)
	}
	if got := first.absorbed + rest.absorbed + first.skipped + rest.skipped; got != 0 {
		t.Fatalf("loop absorbed+skipped = %d, want 0", got)
	}
	if got := first.applied + rest.applied; got != 6 {
		t.Fatalf("loop applied %d effects, want 6 (3 re-puts + 3 tombstones)", got)
	}

	// --- the same logical contacts under the NEW ids, persons intact.
	for _, c := range []struct {
		account, resource, oldID, person string
	}{
		{acctA, "people/c1", oldMatched, pMatched},
		{acctA, "people/c2", oldShell, pShell},
		{acctB, "people/c1", oldShared, pShared},
	} {
		newID := newGoogleContactID(c.account, c.resource)
		e, err := ds.Get(ctx, googleContactType, newID)
		if err != nil {
			t.Fatalf("migrated row %s (%s %s): %v", newID, c.account, c.resource, err)
		}
		if e.DeletedAt != nil || e.Kind != googleContactType {
			t.Fatalf("migrated row %s: deleted=%v type=%s", newID, e.DeletedAt, e.Kind)
		}
		if got := e.Properties["resourceName"]; got != c.resource {
			t.Fatalf("migrated row %s resourceName = %v, want %s", newID, got, c.resource)
		}
		if got := e.Properties["account"]; got != c.account {
			t.Fatalf("migrated row %s account = %v, want %s", newID, got, c.account)
		}
		// Person continuity: the SAME person record, carried explicitly —
		// never a re-minted shell (the email-less contact would have minted
		// one if the migration had left the link to the match probe).
		if got := h.personOf(newID); got != c.person {
			t.Fatalf("migrated row %s points at person %s, want the original %s", newID, got, c.person)
		}
		// The old id is tombstoned — gone from every live read, not purged.
		old, err := ds.Get(ctx, googleContactType, c.oldID)
		if err != nil {
			t.Fatalf("old row %s after migration: %v", c.oldID, err)
		}
		if old.DeletedAt == nil {
			t.Fatalf("old row %s is still live after migration", c.oldID)
		}
	}
	if got := countLiveOf(t, ds, googleContactType); got != 3 {
		t.Fatalf("live contacts after migration = %d, want 3 (no duplicates)", got)
	}
	if got := countLiveOf(t, ds, googlePersonType); got != 2 {
		t.Fatalf("live persons after migration = %d, want 2 (none re-minted, none orphaned)", got)
	}
	adaPerson, err = ds.Get(ctx, googlePersonType, pMatched)
	if err != nil {
		t.Fatalf("get person %s after migration: %v", pMatched, err)
	}
	if got := adaPerson.Properties["displayName"]; got != wantDisplay {
		t.Fatalf("person %s displayName = %v after migration, want %v", pMatched, got, wantDisplay)
	}

	// --- idempotent: a re-run finds nothing on the old scheme, drains in a
	// bounded loop, and writes nothing.
	rerun := h.drain(200)
	if rerun.migrated != 0 || rerun.absorbed != 0 || rerun.skipped != 0 || rerun.applied != 0 {
		t.Fatalf("re-run = %+v, want all zero", rerun)
	}

	// --- a fresh sync upserts cleanly under the new scheme: the id the sync
	// now composes IS the migrated id (asserted above via substratefn.ExternalID),
	// so its next re-put of the same provider record lands on the migrated
	// row — no duplicate, same person.
	newMatched := newGoogleContactID(acctA, "people/c1")
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: googleContactType, ID: newMatched, Properties: map[string]any{
			"account":      acctA,
			"resourceName": "people/c1",
			"name":         map[string]any{"displayName": "Ada Lovelace, Countess"},
			"emails":       []any{map[string]any{"value": "ada@example.com"}},
		},
	}); err != nil {
		t.Fatalf("post-migration upsert under %s: %v", newMatched, err)
	}
	if got := countLiveOf(t, ds, googleContactType); got != 3 {
		t.Fatalf("live contacts after a re-sync upsert = %d, want 3", got)
	}
	if got := h.personOf(newMatched); got != pMatched {
		t.Fatalf("re-sync upsert re-pointed %s at %s, want %s", newMatched, got, pMatched)
	}

	// The new ids stay inside the writer-supplied cap by construction.
	if l := len(newMatched); l > 128 {
		t.Fatalf("new id %q is %d chars — over the 128 cap", newMatched, l)
	}
}

// TestGoogleContactsIDMigrationAbsorbedMerge proves the absorbed-orphan fix
// (Codex H2): a sync beat the migration to a contact's NEW id. The email-less
// case re-minted a fresh person shell for the raced row; the migration must
// fold that shell into the ORIGINAL person — the original may carry owner
// edits, so it wins — leaving ONE person, the shell resolving onto it. The
// email-matched case resolved the SAME person and absorbs without a merge.
func TestGoogleContactsIDMigrationAbsorbedMerge(t *testing.T) {
	t.Parallel()
	h := newMigrationHarness(t)
	ctx, ds := h.ctx, h.ds
	const acct = "workacct"

	// The email-less hazard: the old row minted P1...
	oldShell := h.seedOld(acct, "people/c7", "No Email Contact", nil)
	p1 := h.personOf(oldShell)
	// ...which the owner then edited — the edit must survive the fold.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, googlePersonType, p1, substrate.PatchInput{
		Properties: map[string]any{"displayName": "Someone I Actually Know"},
	}); err != nil {
		t.Fatalf("owner-edit person %s: %v", p1, err)
	}
	// The raced sync: the same contact lands under its NEW id with no email
	// to match on, so the mapping mints a SECOND person shell P2 — exactly
	// the orphan the old absorbed path shipped.
	newShell := h.seed(newGoogleContactID(acct, "people/c7"), acct, "people/c7", "No Email Contact", nil)
	p2 := h.personOf(newShell)
	if p2 == p1 {
		t.Fatalf("the raced new-id row linked to the original person — the test needs the re-minted shell")
	}

	// The email-matched control: both rows resolve the SAME person, so the
	// absorb must NOT merge anything.
	oldMatched := h.seedOld(acct, "people/c8", "Bob", []string{"bob@example.com"})
	p3 := h.personOf(oldMatched)
	newMatched := h.seed(newGoogleContactID(acct, "people/c8"), acct, "people/c8", "Bob", []string{"bob@example.com"})
	if got := h.personOf(newMatched); got != p3 {
		t.Fatalf("the raced email-matched row minted a person (%s vs %s) — the probe should have linked it", got, p3)
	}
	if got := countLiveOf(t, ds, googlePersonType); got != 3 {
		t.Fatalf("live persons before migration = %d, want 3 (P1, the raced shell, P3)", got)
	}

	res := h.drain(200)
	if res.migrated != 0 || res.absorbed != 2 || res.skipped != 0 {
		t.Fatalf("drain = %+v, want migrated=0 absorbed=2 skipped=0", res)
	}
	if res.applied != 3 {
		t.Fatalf("drain applied %d effects, want 3 (1 person merge + 2 tombstones)", res.applied)
	}

	// ONE person survives the email-less pair: P1, the original, alive and
	// carrying the owner edit; the raced shell folded into it.
	winner, err := ds.Get(ctx, googlePersonType, p1)
	if err != nil {
		t.Fatalf("get person %s: %v", p1, err)
	}
	if winner.DeletedAt != nil {
		t.Fatalf("the ORIGINAL person %s was tombstoned — the merge picked the wrong winner", p1)
	}
	if got := winner.Properties["displayName"]; got != "Someone I Actually Know" {
		t.Fatalf("winner displayName = %v — the owner edit did not survive the fold", got)
	}
	folded, err := ds.Get(ctx, googlePersonType, p2)
	if err != nil {
		t.Fatalf("get the folded shell %s: %v", p2, err)
	}
	if folded.ID != p1 {
		t.Fatalf("the shell %s resolves to %s, want the original person %s", p2, folded.ID, p1)
	}
	// The surviving new-id row hangs off the ORIGINAL person.
	if got := h.personOf(newShell); got != p1 {
		t.Fatalf("absorbed row %s points at %s, want the original person %s", newShell, got, p1)
	}
	if got := countLiveOf(t, ds, googlePersonType); got != 2 {
		t.Fatalf("live persons after migration = %d, want 2 (P1 + P3; the shell folded)", got)
	}

	// The email-matched pair absorbed without touching P3.
	if got := h.personOf(newMatched); got != p3 {
		t.Fatalf("absorbed row %s points at %s, want %s", newMatched, got, p3)
	}
	// Both old rows tombstoned; the two new rows are the live book.
	for _, id := range []string{oldShell, oldMatched} {
		e, err := ds.Get(ctx, googleContactType, id)
		if err != nil {
			t.Fatalf("old row %s: %v", id, err)
		}
		if e.DeletedAt == nil {
			t.Fatalf("old row %s is still live after the absorb", id)
		}
	}
	if got := countLiveOf(t, ds, googleContactType); got != 2 {
		t.Fatalf("live contacts after migration = %d, want 2", got)
	}

	// Idempotent: nothing left to do, and nothing re-merges.
	rerun := h.drain(200)
	if rerun.migrated != 0 || rerun.absorbed != 0 || rerun.skipped != 0 || rerun.applied != 0 {
		t.Fatalf("re-run = %+v, want all zero", rerun)
	}
}

// TestGoogleContactsIDMigrationSkipPaths proves the refuse-to-clobber paths:
// a target that CHANGED under the migration to a tombstone (the contact was
// deleted upstream after the id-scheme upgrade — the delete addressed the new
// id while the row still lived at the old one) is neither resurrected by the
// re-put nor swept from under the operator; it is counted, logged and left
// alone, stable across re-runs. Same for a row with no provider key.
func TestGoogleContactsIDMigrationSkipPaths(t *testing.T) {
	t.Parallel()
	h := newMigrationHarness(t)
	ctx, ds := h.ctx, h.ds
	const acct = "workacct"

	// The upstream-deleted contact: old row live, new id TOMBSTONED.
	oldRow := h.seedOld(acct, "people/c5", "Eve", []string{"eve@example.com"})
	newID := newGoogleContactID(acct, "people/c5")
	h.seed(newID, acct, "people/c5", "Eve", []string{"eve@example.com"})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, googleContactType, newID); err != nil {
		t.Fatalf("tombstone the new id %s: %v", newID, err)
	}
	// The unkeyable row: no resourceName to derive an id from.
	broken := h.seed("brokenrow", acct, "", "Key-less Row", nil)

	for pass := 1; pass <= 2; pass++ {
		res := h.drain(200)
		if res.migrated != 0 || res.absorbed != 0 || res.skipped != 2 {
			t.Fatalf("pass %d = %+v, want migrated=0 absorbed=0 skipped=2 (re-reported every pass)", pass, res)
		}
		if res.applied != 0 {
			t.Fatalf("pass %d applied %d effects — the skip paths write NOTHING", pass, res.applied)
		}
	}

	// The old row is untouched — never deleted from under the operator.
	old, err := ds.Get(ctx, googleContactType, oldRow)
	if err != nil {
		t.Fatalf("old row %s: %v", oldRow, err)
	}
	if old.DeletedAt != nil {
		t.Fatalf("old row %s was deleted — a skipped row must be left alone", oldRow)
	}
	// The tombstone is untouched — never restored (the resurrection hole).
	stone, err := ds.Get(ctx, googleContactType, newID)
	if err != nil {
		t.Fatalf("tombstoned target %s: %v", newID, err)
	}
	if stone.DeletedAt == nil {
		t.Fatalf("the tombstoned target %s came back to life — the re-put resurrected an upstream delete", newID)
	}
	// The unkeyable row is untouched too.
	if e, err := ds.Get(ctx, googleContactType, broken); err != nil || e.DeletedAt != nil {
		t.Fatalf("unkeyable row %s: err=%v deleted=%v", broken, err, e.DeletedAt)
	}
}
