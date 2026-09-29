package engine_test

// A put that restores a tombstone completes the conversions the tombstone
// missed (decision 0144): a conversion rewrites live records alone, so the
// restoring put rewrites the stored row into the shape the kind declares now
// before the writer's properties merge in. These hold each step on a record
// tombstoned before the apply that declared it, that the tombstone itself is
// left as it was until the restore, that the restored record's read applies
// back unchanged, that the restoring entry names each step, and that a
// rebuild and an import reproduce the restored row.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// tombstoneWidget deletes a widget, failing the test on a refusal.
func tombstoneWidget(t *testing.T, ds substrate.Dataset, id string) {
	t.Helper()
	if _, err := ds.Delete(context.Background(), owner, cvWidget, id, substrate.DeleteInput{}); err != nil {
		t.Fatalf("tombstone %s: %v", id, err)
	}
}

// storedProps reads a widget's stored properties straight off the fold, a
// tombstone included.
func storedProps(t *testing.T, dsn, id string) map[string]any {
	t.Helper()
	var raw []byte
	if err := rawDB(t, dsn).QueryRow(`SELECT props FROM records WHERE kind = $1 AND id = $2`, cvWidget, id).Scan(&raw); err != nil {
		t.Fatalf("read %s: %v", id, err)
	}
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		t.Fatal(err)
	}
	return props
}

// restorePayload reads the payload of the entry that restored a widget.
func restorePayload(t *testing.T, dsn, id string) map[string]any {
	t.Helper()
	var raw []byte
	if err := rawDB(t, dsn).QueryRow(`SELECT payload FROM changelog WHERE kind = $1 AND record_id = $2 AND op = 'put' AND payload->'restored' = 'true'::jsonb ORDER BY seq DESC LIMIT 1`,
		cvWidget, id).Scan(&raw); err != nil {
		t.Fatalf("read the restore entry of %s: %v", id, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestRestoreTakesTheSpellingARemapMovedWhileTheRecordWasATombstone(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	if err := cvApply(t, ds, map[string]any{
		"name":   map[string]any{"type": "string"},
		"status": map[string]any{"type": "enum", "values": []any{"open", "active", "closed"}},
		"moods":  map[string]any{"type": "enum", "repeated": true, "values": []any{"calm", "tense"}},
		"tier":   map[string]any{"type": "enum", "values": []any{"low", "high"}},
		"kept":   map[string]any{"type": "enum", "values": []any{"open", "active"}},
	}); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	live := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "a", "status": "active"}})
	gone := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{
		"name": "b", "status": "active", "moods": []any{"tense", "calm"}, "tier": "high",
	}})
	named := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "c", "status": "active", "kept": "active"}})
	tombstoneWidget(t, ds, gone.ID)
	tombstoneWidget(t, ds, named.ID)

	// `active` becomes `working` and `tense` becomes `anxious`; `high` is
	// removed outright, which the narrowing admits because no live record
	// holds it.
	if err := cvApply(t, ds, map[string]any{
		"name": map[string]any{"type": "string"},
		"status": map[string]any{"type": "enum", "values": []any{
			"open", map[string]any{"value": "working", "renamedFrom": "active"}, "closed",
		}},
		"moods": map[string]any{"type": "enum", "repeated": true, "values": []any{
			"calm", map[string]any{"value": "anxious", "renamedFrom": "tense"},
		}},
		"tier": map[string]any{"type": "enum", "values": []any{"low"}},
		"kept": map[string]any{"type": "enum", "values": []any{"open", "active"}},
	}); err != nil {
		t.Fatalf("the remap must land: %v", err)
	}
	if got := mustGet(t, ds, cvWidget, live.ID); got.Properties["status"] != "working" {
		t.Fatalf("the live record did not move: %v", got.Properties)
	}
	// The apply converted the live record alone: the tombstone keeps the
	// spelling it was deleted with until something restores it.
	if props := storedProps(t, dsn, gone.ID); props["status"] != "active" || props["tier"] != "high" {
		t.Fatalf("the apply rewrote a tombstone: %v", props)
	}

	restored := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, ID: gone.ID, Properties: map[string]any{"name": "b"}})
	if restored.Properties["status"] != "working" {
		t.Fatalf("the restored record holds status %v, want the remapped spelling", restored.Properties["status"])
	}
	if moods, _ := restored.Properties["moods"].([]any); len(moods) != 2 || moods[0] != "anxious" || moods[1] != "calm" {
		t.Fatalf("the restored list did not move element by element: %v", restored.Properties["moods"])
	}
	if _, still := restored.Properties["tier"]; still {
		t.Fatalf("the restored record holds a value the kind no longer admits: %v", restored.Properties)
	}
	// What the restore reads back is a record the kind admits whole.
	mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, ID: gone.ID, Properties: restored.Properties})

	payload := restorePayload(t, dsn, gone.ID)
	remapped, _ := payload["remapped"].(map[string]any)
	status, _ := remapped["status"].(map[string]any)
	moods, _ := remapped["moods"].(map[string]any)
	if status["active"] != "working" || moods["tense"] != "anxious" {
		t.Fatalf("the restore entry's remapped = %v", payload["remapped"])
	}
	if nulled, _ := payload["nulled"].([]any); len(nulled) != 1 || nulled[0] != "tier" {
		t.Fatalf("the restore entry's nulled = %v, want tier", payload["nulled"])
	}

	// A name the restoring put names is the writer's: its value stands and
	// nothing remaps it, while the property it does not name still moves.
	named2 := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, ID: named.ID, Properties: map[string]any{"status": "closed"}})
	if named2.Properties["status"] != "closed" || named2.Properties["kept"] != "active" {
		t.Fatalf("the writer's value or an admitted one moved: %v", named2.Properties)
	}
	if payload := restorePayload(t, dsn, named.ID); payload["remapped"] != nil || payload["nulled"] != nil {
		t.Fatalf("a restore that rewrote nothing names a step: %v", payload)
	}
	cvReplays(t, svc, ds)
}

func TestRestoreFillsTheDefaultARequiredPropertyGainedWhileTheRecordWasATombstone(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	name := map[string]any{"type": "string"}
	if err := cvApply(t, ds, map[string]any{"name": name, "mood": map[string]any{"type": "string", "embed": true}}); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	live := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "a"}})
	lib := substrate.Actor("connector:library")
	gone := mustPut(t, ds, lib, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "b"}})
	tombstoneWidget(t, ds, gone.ID)

	if err := cvApply(t, ds, map[string]any{
		"name": name,
		"mood": map[string]any{"type": "string", "embed": true, "required": true, "default": "neutral"},
	}); err != nil {
		t.Fatalf("required with a default must land: %v", err)
	}
	if got := mustGet(t, ds, cvWidget, live.ID); got.Properties["mood"] != "neutral" {
		t.Fatalf("the live record was not backfilled: %v", got.Properties)
	}
	if _, held := storedProps(t, dsn, gone.ID)["mood"]; held {
		t.Fatal("the apply backfilled a tombstone")
	}

	// Without the default the restoring put would be refused: the merged row
	// lacks the value `required` asks for.
	restored := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, ID: gone.ID, Properties: map[string]any{"name": "b"}})
	if restored.Properties["mood"] != "neutral" {
		t.Fatalf("the restored record holds mood %v, want the default", restored.Properties["mood"])
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, ID: gone.ID, Properties: restored.Properties})

	// The filled value is the restoring hand's, as a create falling back to
	// the default would record it, and its text is queued to embed.
	db := rawDB(t, dsn)
	var actor, tier string
	if err := db.QueryRow(`SELECT actor, tier FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = 'mood'`,
		cvWidget, gone.ID).Scan(&actor, &tier); err != nil {
		t.Fatalf("read the manager row: %v", err)
	}
	if actor != string(owner) || tier != string(substrate.TierOwner) {
		t.Fatalf("the filled value's manager = %q at %q", actor, tier)
	}
	var queued int
	if err := db.QueryRow(`SELECT count(*) FROM embed_queue WHERE record_kind = $1 AND record_id = $2 AND property = 'mood'`,
		cvWidget, gone.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("embed queue rows for the filled value = %d, %v; want 1", queued, err)
	}
	payload := restorePayload(t, dsn, gone.ID)
	if filled, _ := payload["backfilled"].([]any); len(filled) != 1 || filled[0] != "mood" {
		t.Fatalf("the restore entry's backfilled = %v, want mood", payload["backfilled"])
	}
	if props, _ := payload["properties"].([]any); !containsAny(props, "mood") {
		t.Fatalf("the restore entry's properties = %v, want mood among them", payload["properties"])
	}

	// A restoring put that clears the property is the writer saying there is
	// no value, which `required` refuses rather than quietly refilling.
	again := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "c"}})
	tombstoneWidget(t, ds, again.ID)
	if _, err := ds.Put(context.Background(), owner, substrate.PutInput{Kind: cvWidget, ID: again.ID, Properties: map[string]any{"mood": nil}}); err == nil {
		t.Fatal("a restore clearing a required property must refuse")
	}
	cvReplays(t, svc, ds)
}

func TestRestoreRemovesADroppedPropertyAndMovesARenamedOne(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	name := map[string]any{"type": "string"}
	if err := cvApply(t, ds, map[string]any{
		"name":   name,
		"mood":   map[string]any{"type": "string", "embed": true},
		"token":  map[string]any{"type": "secret"},
		"size":   map[string]any{"type": "string"},
		"weight": map[string]any{"type": "string"},
	}); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	lib := substrate.Actor("connector:library")
	gone := mustPut(t, ds, lib, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{
		"name": "a", "mood": "cheerful", "token": "shh", "size": "large", "weight": "heavy",
	}})
	db := rawDB(t, dsn)
	ref, _ := storedProps(t, dsn, gone.ID)["token"].(string)
	if ref == "" {
		t.Fatal("the secret was not sealed")
	}
	sealedRows := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM sealed WHERE ref = $1`, ref).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	tombstoneWidget(t, ds, gone.ID)

	// `mood` and `token` are dropped, `size` is renamed `dimensions`, and
	// `weight` is renamed `mass` and retyped, a value the tombstone's string
	// cannot become. No live record holds any of them, so the plan has no
	// step and needs no confirmation.
	if err := cvApply(t, ds, map[string]any{
		"name":       name,
		"dimensions": map[string]any{"type": "string", "renamedFrom": "size"},
		"mass":       map[string]any{"type": "int", "renamedFrom": "weight"},
	}); err != nil {
		t.Fatalf("the drop and the rename must land: %v", err)
	}
	if props := storedProps(t, dsn, gone.ID); props["mood"] != "cheerful" || props["size"] != "large" || sealedRows() != 1 {
		t.Fatalf("the apply rewrote a tombstone: %v", props)
	}

	restored := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, ID: gone.ID, Properties: map[string]any{"name": "a"}})
	for _, dropped := range []string{"mood", "token", "size", "weight", "mass"} {
		if _, still := restored.Properties[dropped]; still {
			t.Fatalf("the restored record holds %s: %v", dropped, restored.Properties)
		}
	}
	if restored.Properties["dimensions"] != "large" {
		t.Fatalf("the restored record holds dimensions %v, want the value it held as size", restored.Properties["dimensions"])
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, ID: gone.ID, Properties: restored.Properties})

	// The dropped values leave with their manager rows, queue rows and sealed
	// material; the renamed value keeps the manager its writer had.
	var managers, queued int
	if err := db.QueryRow(`SELECT count(*) FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property IN ('mood', 'token', 'size', 'weight', 'mass')`,
		cvWidget, gone.ID).Scan(&managers); err != nil || managers != 0 {
		t.Fatalf("manager rows left behind = %d, %v", managers, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM embed_queue WHERE record_kind = $1 AND record_id = $2 AND property = 'mood'`,
		cvWidget, gone.ID).Scan(&queued); err != nil || queued != 0 {
		t.Fatalf("embed queue rows left behind = %d, %v", queued, err)
	}
	if sealedRows() != 0 {
		t.Fatal("the sealed material outlived the property")
	}
	var actor string
	if err := db.QueryRow(`SELECT actor FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = 'dimensions'`,
		cvWidget, gone.ID).Scan(&actor); err != nil || actor != string(lib) {
		t.Fatalf("the renamed value's manager = %q, %v; want %q", actor, err, lib)
	}

	payload := restorePayload(t, dsn, gone.ID)
	// The retyped rename moved nothing: what left the record is `weight`.
	nulled, _ := payload["nulled"].([]any)
	if len(nulled) != 3 || nulled[0] != "mood" || nulled[1] != "token" || nulled[2] != "weight" {
		t.Fatalf("the restore entry's nulled = %v, want mood, token and weight", payload["nulled"])
	}
	if renamed, _ := payload["renamed"].(map[string]any); len(renamed) != 1 || renamed["size"] != "dimensions" {
		t.Fatalf("the restore entry's renamed = %v, want size to dimensions", payload["renamed"])
	}
	// The values read pairs the rename as one move and shows the drops
	// leaving, off the restore's own entry.
	changes, err := ds.ChangesBefore(context.Background(), 0, substrate.ChangeFilter{Kinds: []string{cvWidget}, RecordID: gone.ID, Values: true}, 10)
	if err != nil {
		t.Fatalf("the restored record's changes: %v", err)
	}
	var movedSize, leftMood bool
	for _, c := range changes {
		if c.Payload["restored"] != true {
			continue
		}
		for _, pc := range c.Affected[0].Properties {
			movedSize = movedSize || (pc.Name == "dimensions" && pc.RenamedFrom == "size" && pc.Before == "large" && pc.After == "large")
			leftMood = leftMood || (pc.Name == "mood" && pc.Before != nil && pc.After == nil)
		}
	}
	if !movedSize || !leftMood {
		t.Fatalf("the restore's change row does not read the rename and the drop: %s", jsonOf(t, changes))
	}
	cvReplays(t, svc, ds)
}

// A kept value is held to what a write of it would store, not only to the
// declaration's shape: a string retyped to a secret is material nobody sealed,
// a secret retyped to a string is a sealed ref read as text, a reference the
// pin no longer admits fails the registry gate a write runs, and a string
// retyped to a datetime is stored normalized, so the restored read applies
// back without moving the record.
func TestRestoreHoldsAKeptValueToWhatAWriteStores(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	const gadget = cvPackage + "/gadget"
	apply := func(props map[string]any) error {
		docs := append(cvDocs(props), vocabulary.KindManifest(cvPackage, map[string]any{"singular": "gadget"},
			map[string]any{"properties": map[string]any{"name": map[string]any{"type": "string"}}}))
		_, err := ds.ApplyVocabularyDocuments(ctx, owner, docs)
		return err
	}
	name := map[string]any{"type": "string"}
	if err := apply(map[string]any{
		"name":  name,
		"pin":   map[string]any{"type": "string"},
		"token": map[string]any{"type": "secret"},
		"when":  map[string]any{"type": "string"},
		"link":  map[string]any{"type": "reference", "kind": "any"},
	}); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	target := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "target"}})
	gone := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{
		"name": "g", "pin": "1234", "token": "shh", "when": "2026-01-01T02:00:00+02:00",
		"link": cvWidget + "/" + target.ID,
	}})
	db := rawDB(t, dsn)
	ref, _ := storedProps(t, dsn, gone.ID)["token"].(string)
	var sealed int
	if err := db.QueryRow(`SELECT count(*) FROM sealed WHERE ref = $1`, ref).Scan(&sealed); err != nil || sealed != 1 {
		t.Fatalf("the secret was not sealed: %d, %v", sealed, err)
	}
	tombstoneWidget(t, ds, gone.ID)

	// Four retypes no live record holds a value for, so none is counted.
	if err := apply(map[string]any{
		"name":  name,
		"pin":   map[string]any{"type": "secret"},
		"token": map[string]any{"type": "string"},
		"when":  map[string]any{"type": "datetime"},
		"link":  map[string]any{"type": "reference", "kind": gadget},
	}); err != nil {
		t.Fatalf("the retypes must land: %v", err)
	}

	restored := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, ID: gone.ID, Properties: map[string]any{"name": "g"}})
	for _, refused := range []string{"pin", "token", "link"} {
		if _, still := restored.Properties[refused]; still {
			t.Fatalf("the restored record holds %s: %v", refused, restored.Properties)
		}
	}
	if restored.Properties["when"] != "2026-01-01T00:00:00Z" {
		t.Fatalf("the restored record holds when %v, want the value a datetime write stores", restored.Properties["when"])
	}
	if props := storedProps(t, dsn, gone.ID); props["pin"] != nil {
		t.Fatalf("the unsealed material stayed in the row: %v", props)
	}
	if err := db.QueryRow(`SELECT count(*) FROM sealed WHERE ref = $1`, ref).Scan(&sealed); err != nil || sealed != 0 {
		t.Fatalf("the sealed material outlived its secret: %d, %v", sealed, err)
	}
	// The read applies back as the no-op it should be.
	again := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, ID: gone.ID, Properties: restored.Properties})
	if again.Version != restored.Version {
		t.Fatalf("applying the restored read back moved the record from version %d to %d: %v",
			restored.Version, again.Version, again.Properties)
	}

	payload := restorePayload(t, dsn, gone.ID)
	nulled, _ := payload["nulled"].([]any)
	if len(nulled) != 3 || nulled[0] != "link" || nulled[1] != "pin" || nulled[2] != "token" {
		t.Fatalf("the restore entry's nulled = %v, want link, pin and token", payload["nulled"])
	}
	if props, _ := payload["properties"].([]any); !containsAny(props, "when") {
		t.Fatalf("the restore entry's properties = %v, want when among them", payload["properties"])
	}
	cvReplays(t, svc, ds)
}

// containsAny reports whether a decoded JSON list holds the string.
func containsAny(list []any, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
