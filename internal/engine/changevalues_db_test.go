package engine_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Before and after values on a change row (decision 0107): asked for with
// ChangeFilter.Values, each affected record names what the entry did to each
// property, the before derived from the changelog and the after from the
// entry, both as a read of the record renders them.

const cvTask = "samples.substrate.reamde.dev/tasks/task"

// snapshots holds each record's read at each version it reached: the oracle
// every derived before and after is held to.
type snapshots map[string]map[int64]map[string]any

func (s snapshots) take(t *testing.T, ds substrate.Dataset, kind, id string) {
	t.Helper()
	rec := mustGet(t, ds, kind, id)
	if s[id] == nil {
		s[id] = map[int64]map[string]any{}
	}
	s[id][rec.Version] = rec.Properties
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

// holdToSnapshots checks every property change on a tracked record against
// the reads on either side of it, and that it names exactly the properties
// that differ between them. It returns how many changes it checked.
func holdToSnapshots(t *testing.T, changes []substrate.Change, snaps snapshots) int {
	t.Helper()
	checked := 0
	for _, c := range changes {
		for _, a := range c.Affected {
			versions, tracked := snaps[a.ID]
			if !tracked || a.Version == 0 {
				continue
			}
			after, ok := versions[a.Version]
			if !ok {
				t.Fatalf("seq %d: no read of %s at version %d", c.Seq, a.ID, a.Version)
			}
			before := versions[a.Version-1] // nil at a creation
			want := map[string]bool{}
			for _, m := range []map[string]any{before, after} {
				for name := range m {
					// The task renders its title from `name` (decision 0016):
					// the derived title is not a change of its own.
					if name == substrate.PropTitle {
						continue
					}
					if jsonOf(t, before[name]) != jsonOf(t, after[name]) {
						want[name] = true
					}
				}
			}
			got := map[string]bool{}
			for _, pc := range a.Properties {
				got[pc.Name] = true
				// A paired rename (decision 0108) reads its before under the
				// old name, and stands for the old name's clear too.
				from := pc.Name
				if pc.RenamedFrom != "" {
					from = pc.RenamedFrom
					got[from] = true
				}
				if pc.BeforeUnknown {
					t.Fatalf("seq %d %s.%s: before unknown on a history the changelog holds whole", c.Seq, a.ID, pc.Name)
				}
				if jsonOf(t, pc.Before) != jsonOf(t, before[from]) {
					t.Fatalf("seq %d %s.%s: before = %s, the read at version %d says %s", c.Seq, a.ID, pc.Name, jsonOf(t, pc.Before), a.Version-1, jsonOf(t, before[from]))
				}
				if jsonOf(t, pc.After) != jsonOf(t, after[pc.Name]) {
					t.Fatalf("seq %d %s.%s: after = %s, the read at version %d says %s", c.Seq, a.ID, pc.Name, jsonOf(t, pc.After), a.Version, jsonOf(t, after[pc.Name]))
				}
				checked++
			}
			if jsonOf(t, got) != jsonOf(t, want) {
				t.Fatalf("seq %d %s (%s): names %v, the reads differ in %v", c.Seq, a.ID, c.Op, got, want)
			}
		}
	}
	return checked
}

func TestChangeValuesCarryBeforeAndAfterAcrossPutPatchDeleteAndRestore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	snaps := snapshots{}

	task := mustPut(t, ds, owner, substrate.PutInput{Kind: cvTask, ID: "t1", Properties: map[string]any{
		"name": "Write the report", "priority": "high",
	}})
	snaps.take(t, ds, cvTask, task.ID)
	for _, props := range []map[string]any{
		{"name": "Write the quarterly report", "priority": "urgent"},
		{"description": "Numbers from finance first."},
		{"url": "https://tracker.example.com/1"},
		{"description": nil},
		{"status": "done"},
	} {
		mustPatch(t, ds, owner, cvTask, task.ID, substrate.PatchInput{Properties: props})
		snaps.take(t, ds, cvTask, task.ID)
	}
	mustPut(t, ds, owner, substrate.PutInput{Kind: cvTask, ID: task.ID, Properties: map[string]any{
		"name": "Write the report", "priority": "medium",
	}})
	snaps.take(t, ds, cvTask, task.ID)
	if _, err := ds.Delete(ctx, owner, cvTask, task.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete: %v", err)
	}
	snaps.take(t, ds, cvTask, task.ID)
	mustPut(t, ds, owner, substrate.PutInput{Kind: cvTask, ID: task.ID, Properties: map[string]any{"priority": "low"}})
	snaps.take(t, ds, cvTask, task.ID)

	scope := substrate.ChangeFilter{Kinds: []string{cvTask}, RecordID: task.ID, Values: true}
	whole, err := ds.ChangesBefore(ctx, 0, scope, 100)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	if n := holdToSnapshots(t, whole, snaps); n < 10 {
		t.Fatalf("checked %d property changes, want the whole history's", n)
	}
	forward, err := ds.Changes(ctx, 0, substrate.ChangeFilter{Values: true}, 500)
	if err != nil {
		t.Fatalf("changes: %v", err)
	}
	holdToSnapshots(t, forward, snaps)

	// A page of one row holds none of the entries its befores come from: the
	// walk reads them back, so every page says the same as the whole.
	before := int64(0)
	for range whole {
		page, err := ds.ChangesBefore(ctx, before, scope, 1)
		if err != nil {
			t.Fatalf("page under %d: %v", before, err)
		}
		if len(page) != 1 {
			t.Fatalf("page under %d = %d rows", before, len(page))
		}
		holdToSnapshots(t, page, snaps)
		before = page[0].Seq
	}

	// Spot-check the words the History page shows.
	byVersion := map[int64]substrate.AffectedRecord{}
	for _, c := range whole {
		byVersion[c.Affected[0].Version] = c.Affected[0]
	}
	find := func(v int64, name string) substrate.PropertyChange {
		t.Helper()
		for _, pc := range byVersion[v].Properties {
			if pc.Name == name {
				return pc
			}
		}
		t.Fatalf("version %d names no %s: %+v", v, name, byVersion[v].Properties)
		return substrate.PropertyChange{}
	}
	if pc := find(2, "priority"); pc.Before != "high" || pc.After != "urgent" {
		t.Fatalf("priority at v2 = %+v", pc)
	}
	if pc := find(3, "description"); pc.Before != nil || pc.After != "Numbers from finance first." {
		t.Fatalf("description added at v3 = %+v", pc)
	}
	if pc := find(5, "description"); pc.Before != "Numbers from finance first." || pc.After != nil {
		t.Fatalf("description cleared at v5 = %+v", pc)
	}
	if pc := find(6, "status"); pc.Before != "open" || pc.After != "done" {
		t.Fatalf("status at v6 = %+v", pc)
	}
	if del := byVersion[8]; len(del.Properties) != 0 || !del.Deleted {
		t.Fatalf("the delete names property changes: %+v", del)
	}
	if pc := find(9, "priority"); pc.Before != "medium" || pc.After != "low" {
		t.Fatalf("priority on the restore = %+v", pc)
	}

	// Without asking, no row carries a value.
	plain, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{Kinds: []string{cvTask}, RecordID: task.ID}, 100)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	for _, c := range plain {
		for _, a := range c.Affected {
			if a.Properties != nil {
				t.Fatalf("seq %d carries values nobody asked for: %+v", c.Seq, a.Properties)
			}
		}
	}
}

func TestChangeValuesWalkBackAcrossAMergeAndItsSplit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newVocabularyDataset(t, "people")
	const person = "samples.substrate.reamde.dev/people/person"
	snaps := snapshots{}

	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: person, ID: "grace", Properties: map[string]any{"name": "Grace B. Hopper"}})
	snaps.take(t, ds, person, winner.ID)
	loser := mustPut(t, ds, owner, substrate.PutInput{Kind: person, ID: "graceb", Properties: map[string]any{
		"name": "Grace Brewster Hopper", "emails": []any{"grace@example.com"},
	}})
	merged, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: person, Winner: winner.ID, Loser: loser.ID})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	snaps.take(t, ds, person, winner.ID)
	if _, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: merged.ID}); err != nil {
		t.Fatalf("split: %v", err)
	}
	snaps.take(t, ds, person, winner.ID)
	mustPatch(t, ds, owner, person, winner.ID, substrate.PatchInput{Properties: map[string]any{
		"name": "Grace Hopper", "emails": []any{"grace@example.com", "hopper@example.com"},
	}})
	snaps.take(t, ds, person, winner.ID)

	// The winner's history from its own scope. The split is addressed to the
	// loser: the walk still finds it through the pair it names, so the
	// winner's versions run unbroken and the patch's befores are known.
	scope := substrate.ChangeFilter{Kinds: []string{person}, RecordID: winner.ID, Values: true}
	changes, err := ds.ChangesBefore(ctx, 0, scope, 100)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	holdToSnapshots(t, changes, snaps)
	newest, err := ds.ChangesBefore(ctx, 0, scope, 1)
	if err != nil {
		t.Fatalf("newest: %v", err)
	}
	holdToSnapshots(t, newest, snaps)
	got := map[string]substrate.PropertyChange{}
	for _, pc := range newest[0].Affected[0].Properties {
		got[pc.Name] = pc
	}
	if pc := got["name"]; pc.Before != "Grace B. Hopper" || pc.After != "Grace Hopper" {
		t.Fatalf("name after the split = %+v", pc)
	}
	if pc := got["emails"]; pc.Before != nil || jsonOf(t, pc.After) != `["grace@example.com","hopper@example.com"]` {
		t.Fatalf("emails after the split = %+v", pc)
	}
}

func TestChangeValuesBoundAManyTimesMergedRecordsPairHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	// A budget of sixteen: the pair history below is twenty rows, which an
	// unbounded pair read spends whole before any walk reads a row.
	_, ds := newCoreDataset(t, engine.WithValuesBudget(16))
	importVocabulary(t, ds, "people")
	const person = "samples.substrate.reamde.dev/people/person"
	snaps := snapshots{}

	ada := mustPut(t, ds, owner, substrate.PutInput{Kind: person, ID: "ada", Properties: map[string]any{"name": "Ada"}})
	snaps.take(t, ds, person, ada.ID)
	winner := mustPut(t, ds, owner, substrate.PutInput{Kind: person, ID: "grace", Properties: map[string]any{"name": "Grace B. Hopper"}})
	snaps.take(t, ds, person, winner.ID)
	mustPut(t, ds, owner, substrate.PutInput{Kind: person, ID: "graceb", Properties: map[string]any{"name": "Grace Brewster Hopper"}})
	for range 10 {
		merged, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: person, Winner: winner.ID, Loser: "graceb"})
		if err != nil {
			t.Fatalf("merge: %v", err)
		}
		snaps.take(t, ds, person, winner.ID)
		if _, err := ds.Split(ctx, owner, substrate.SplitInput{Merge: merged.ID}); err != nil {
			t.Fatalf("split: %v", err)
		}
		snaps.take(t, ds, person, winner.ID)
	}
	for _, name := range []string{"Grace M. Hopper", "Grace Hopper"} {
		mustPatch(t, ds, owner, person, winner.ID, substrate.PatchInput{Properties: map[string]any{"name": name}})
		snaps.take(t, ds, person, winner.ID)
	}
	mustPatch(t, ds, owner, person, ada.ID, substrate.PatchInput{Properties: map[string]any{"name": "Ada Lovelace"}})
	snaps.take(t, ds, person, ada.ID)

	// The newest two rows: the pair read stops at its share, so the walks
	// still have the rest to read: Grace's previous name one entry back, and
	// Ada's at her creation, which no pair names.
	newest, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{Kinds: []string{person}, Values: true}, 2)
	if err != nil {
		t.Fatalf("newest: %v", err)
	}
	if n := holdToSnapshots(t, newest, snaps); n != 2 {
		t.Fatalf("checked %d property changes, want the two names", n)
	}

	// The whole history under the same budget: what the unread pairs leave
	// unknown reads unknown, and nothing reads stale.
	whole, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{Kinds: []string{person}, RecordID: winner.ID, Values: true}, 100)
	if err != nil {
		t.Fatalf("whole: %v", err)
	}
	unknown := 0
	for _, c := range whole {
		for _, a := range c.Affected {
			versions, tracked := snaps[a.ID]
			if !tracked || a.Version == 0 {
				continue
			}
			for _, pc := range a.Properties {
				if pc.BeforeUnknown {
					unknown++
					continue
				}
				if want := versions[a.Version-1][pc.Name]; jsonOf(t, pc.Before) != jsonOf(t, want) {
					t.Fatalf("seq %d %s.%s: before = %s, the read at version %d says %s", c.Seq, a.ID, pc.Name, jsonOf(t, pc.Before), a.Version-1, jsonOf(t, want))
				}
			}
		}
	}
	t.Logf("%d befores read unknown", unknown)
}

func TestChangeValuesRedactASecretLikeARead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, db := newSealingDataset(t)
	sgPutProvider(t, ds, sgPlainKey)
	firstRef := storedAPIKeyRef(t, db)
	mustPatch(t, ds, owner, sgProviderKind, "prov", substrate.PatchInput{Properties: map[string]any{
		"apiKey": "sk-second-67890", "label": "renamed",
	}})

	changes, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{Kinds: []string{sgProviderKind}, RecordID: "prov", Values: true}, 10)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	raw := jsonOf(t, changes)
	for _, leak := range []string{sgPlainKey, "sk-second-67890", firstRef, storedAPIKeyRef(t, db)} {
		if strings.Contains(raw, leak) {
			t.Fatalf("a change row spells %q: %s", leak, raw)
		}
	}
	var sawKey, sawLabel bool
	for _, c := range changes {
		if c.Op != substrate.OpPatch {
			continue
		}
		for _, pc := range c.Affected[0].Properties {
			switch pc.Name {
			case "apiKey":
				sawKey = pc.Before == "<redacted>" && pc.After == "<redacted>"
			case "label":
				sawLabel = pc.Before == "prov" && pc.After == "renamed"
			}
		}
	}
	if !sawKey || !sawLabel {
		t.Fatalf("patch values: key redacted both sides = %v, label plain = %v: %s", sawKey, sawLabel, raw)
	}
}

// A secret's history outlives its declaration: renamed, dropped or retyped,
// the value an earlier entry sealed is still no reader's to see.
func TestChangeValuesKeepAFormerSecretRedacted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, db := newSealingDataset(t)
	const (
		pkg         = "redact.example.substrate.reamde.dev/rd"
		vault       = pkg + "/vault"
		fingerprint = "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08"
	)
	declare := func(props map[string]any) {
		t.Helper()
		if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "vault"}, map[string]any{"properties": props}),
		}); err != nil {
			t.Fatalf("declare the vault: %v", err)
		}
	}
	declare(map[string]any{
		"label":       map[string]any{"type": "string"},
		"token":       map[string]any{"type": "secret"},
		"fingerprint": map[string]any{"type": "digest"},
		"pin":         map[string]any{"type": "secret"},
	})
	mustPut(t, ds, owner, substrate.PutInput{Kind: vault, ID: "v1", Properties: map[string]any{
		"label": "first", "token": "s3cret-one", "fingerprint": fingerprint, "pin": "pin-0042",
	}})
	mustPatch(t, ds, owner, vault, "v1", substrate.PatchInput{Properties: map[string]any{"token": "s3cret-two"}})
	// Cleared, so no live record holds the shape the next declaration drops
	// and retypes.
	mustPatch(t, ds, owner, vault, "v1", substrate.PatchInput{Properties: map[string]any{"fingerprint": nil, "pin": nil}})
	// token renamed (still a secret), fingerprint dropped, pin retyped to a
	// plain string.
	declare(map[string]any{
		"label":  map[string]any{"type": "string"},
		"apiKey": map[string]any{"type": "secret", "renamedFrom": "token"},
		"pin":    map[string]any{"type": "string"},
	})
	mustPatch(t, ds, owner, vault, "v1", substrate.PatchInput{Properties: map[string]any{"label": "second", "pin": "now-plain"}})

	var sealed []string
	rows, err := db.Query(`SELECT payload::text FROM changelog WHERE kind = $1 AND record_id = 'v1'`, vault)
	if err != nil {
		t.Fatalf("read the raw changelog: %v", err)
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			t.Fatal(err)
		}
		sealed = append(sealed, p)
	}
	_ = rows.Close()
	if !strings.Contains(strings.Join(sealed, "\n"), `"secret:`) {
		t.Fatalf("the changelog holds no sealed ref to guard: %v", sealed)
	}

	changes, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{Kinds: []string{vault}, RecordID: "v1", Values: true}, 100)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	raw := jsonOf(t, changes)
	for _, leak := range []string{`secret:`, fingerprint, "s3cret-one", "s3cret-two", "pin-0042"} {
		if strings.Contains(raw, leak) {
			t.Fatalf("a change row spells %q: %s", leak, raw)
		}
	}
	var sawRename, sawPlain bool
	for _, c := range changes {
		for _, pc := range c.Affected[0].Properties {
			switch {
			case pc.Name == "apiKey" && pc.RenamedFrom == "token" && pc.After == engine.Redacted && pc.Before == engine.Redacted:
				sawRename = true
			case pc.Name == "pin" && pc.After == "now-plain":
				sawPlain = pc.Before == nil && !pc.BeforeUnknown
			}
		}
	}
	if !sawRename || !sawPlain {
		t.Fatalf("rename row redacted on both sides = %v, the retyped value plain = %v: %s", sawRename, sawPlain, raw)
	}
}

// A vocabulary apply's rename is one change under the new name, paired to
// the old one, never a removal and an addition (decision 0108).
func TestChangeValuesPairARenameAsOneMove(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := rnApply(t, ds, rnClosure(rnBaseProps(), nil, "size")); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	g := mustPut(t, ds, owner, substrate.PutInput{Kind: rnGizmo, Properties: map[string]any{
		"size": "big", "token": "s3cret", "note": "kept",
	}})
	mustPatch(t, ds, owner, rnGizmo, g.ID, substrate.PatchInput{Properties: map[string]any{"size": "bigger"}})
	if err := rnApply(t, ds, rnClosure(rnRenamedProps(), nil, "dimensions")); err != nil {
		t.Fatalf("the rename must land: %v", err)
	}
	mustPatch(t, ds, owner, rnGizmo, g.ID, substrate.PatchInput{Properties: map[string]any{"dimensions": "huge"}})

	changes, err := ds.ChangesBefore(ctx, 0, substrate.ChangeFilter{Kinds: []string{rnGizmo}, RecordID: g.ID, Values: true}, 10)
	if err != nil {
		t.Fatalf("changes before: %v", err)
	}
	if len(changes) != 4 {
		t.Fatalf("got %d changes, want put, patch, rename and patch", len(changes))
	}
	props := func(c substrate.Change) map[string]substrate.PropertyChange {
		out := map[string]substrate.PropertyChange{}
		for _, a := range c.Affected {
			if a.ID != g.ID {
				continue
			}
			for _, pc := range a.Properties {
				out[pc.Name] = pc
			}
		}
		return out
	}
	// The rename: each moved value is one change under its new name, the
	// before read under the old one, and the old names carry nothing.
	rename := props(changes[1])
	want := map[string]substrate.PropertyChange{
		"dimensions": {Name: "dimensions", RenamedFrom: "size", Before: "bigger", After: "bigger"},
		"apiKey":     {Name: "apiKey", RenamedFrom: "token", Before: engine.Redacted, After: engine.Redacted},
	}
	if len(rename) != len(want) {
		t.Fatalf("rename changes = %+v, want only %v", rename, want)
	}
	for name, w := range want {
		if got := rename[name]; jsonOf(t, got) != jsonOf(t, w) {
			t.Fatalf("rename change %s = %s, want %s", name, jsonOf(t, got), jsonOf(t, w))
		}
	}
	// A write after the rename finds its before in the rename's entry.
	if got := props(changes[0])["dimensions"]; got.RenamedFrom != "" || got.Before != "bigger" || got.After != "huge" || got.BeforeUnknown {
		t.Fatalf("patch after the rename = %s, want bigger to huge", jsonOf(t, got))
	}
}
