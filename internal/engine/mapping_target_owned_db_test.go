package engine_test

// Record 49 at the engine: the owner of a mapping's TARGET declares it, so a
// mapping's source kind lives in a package the declaring one does not own. The
// provider here ships a mirror kind with an unpinned, optional subject slot and
// no mapping of its own; the repository's own package declares the mapping onto
// the kind it owns, and that is what mints, matches and projects.

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	// The provider: a mirror kind, a bundle document so it can be uninstalled,
	// and nothing that names a kind it does not own.
	tmPackage   = "mirror.example.com/mirror"
	tmIssueType = tmPackage + "/issue"

	// The repository's own package: the kind the mapping targets, and the
	// mapping itself.
	tmHomePackage = "ada.example.com/work"
	tmTaskType    = tmHomePackage + "/task"
	tmNoteType    = tmHomePackage + "/note"
	tmMapping     = tmHomePackage + "/issuetask"

	tmMappingKind = "substrate.reamde.dev/core/recordmapping"
)

// tmProviderDocs is the provider's closure: the mirror's subject slot is
// unpinned and NOT required, so a row lands with the slot empty until some
// package declares a mapping that fills it.
func tmProviderDocs() []map[string]any {
	kind := vocabulary.KindManifest(tmPackage,
		map[string]any{"singular": "issue", "plural": "issues"},
		map[string]any{
			"properties": map[string]any{
				"headline": map[string]any{"type": "string"},
				"task": map[string]any{
					"type": "reference", "mustExist": true, "subject": true,
				},
			},
		})
	meta, _ := kind["metadata"].(map[string]any)
	return []map[string]any{
		vocabulary.PackageManifest(tmPackage, 1),
		vocabulary.ActorManifest(tmPackage, vocabulary.PackageActor(tmPackage)),
		vocabulary.BundleManifest(tmPackage, map[string]any{
			"description": "a provider that names no user kind",
			"installs":    []any{meta["id"]},
		}),
		kind,
	}
}

// tmTaskDocs is the repository's own kind, without the mapping yet.
func tmTaskDocs() []map[string]any {
	return []map[string]any{
		vocabulary.PackageManifest(tmHomePackage, 1),
		vocabulary.KindManifest(tmHomePackage,
			map[string]any{"singular": "task", "plural": "tasks"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			}}),
	}
}

// tmMappingDoc is the declaration record 49 moved: the owner of `task`
// declaring how a mirror it does not own reaches it.
func tmMappingDoc() map[string]any {
	return vocabulary.MappingManifest(tmHomePackage, "issuetask", map[string]any{
		"from": tmIssueType, "to": tmTaskType, "property": "task",
		"match": []any{map[string]any{"from": "headline", "to": "name"}},
		"map":   map[string]any{"name": map[string]any{"path": "headline"}},
	})
}

// tmInstall installs the provider, the repository's kind and the mapping.
func tmInstall(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	docs := append(tmProviderDocs(), tmTaskDocs()...)
	docs = append(docs, tmMappingDoc())
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install the provider, the kind and the mapping: %v", err)
	}
}

// tmSubjectOf reads the task a mirror row points at, "" when the slot is empty.
func tmSubjectOf(t *testing.T, ds substrate.Dataset, id string) string {
	t.Helper()
	row := mustGet(t, ds, tmIssueType, id)
	kind, taskID, ok := vocabulary.SplitRecordPath(refPathValue(row, "task"))
	if !ok {
		return ""
	}
	if kind != tmTaskType {
		t.Fatalf("issue %s points at %s, not a task", id, kind)
	}
	return taskID
}

// A mirror row syncs against a mapping its own package never declared: the
// subject is minted, the map rule projects onto it, and a second row whose
// probe matches links to the task already there instead of minting a second.
func TestForeignMirrorMintsAndMatchesThroughTheOwnersMapping(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	tmInstall(t, ds)

	first := mustPut(t, ds, owner, substrate.PutInput{
		Kind: tmIssueType, ID: "issue-1",
		Properties: map[string]any{"headline": "Ship the thing"},
	})
	minted := tmSubjectOf(t, ds, first.ID)
	if minted == "" {
		t.Fatal("the mirror row landed with an empty subject slot")
	}
	if got := mustGet(t, ds, tmTaskType, minted).Properties["name"]; got != "Ship the thing" {
		t.Fatalf("the mapping did not project onto the minted task: name = %v", got)
	}

	// The probe decides: a second mirror row whose headline matches a live
	// task's name links to it rather than minting another.
	existing := mustPut(t, ds, owner, substrate.PutInput{
		Kind: tmTaskType, Properties: map[string]any{"name": "Write the record"},
	})
	second := mustPut(t, ds, owner, substrate.PutInput{
		Kind: tmIssueType, ID: "issue-2",
		Properties: map[string]any{"headline": "Write the record"},
	})
	if got := tmSubjectOf(t, ds, second.ID); got != existing.ID {
		t.Fatalf("the probe minted %q instead of matching %q", got, existing.ID)
	}
}

// Declaring the mapping changes how the target kind may be written: its ids
// become server-assigned, and a chosen one is refused from that moment on.
func TestDeclaringAMappingMakesTheTargetsIDsServerAssigned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	docs := append(tmProviderDocs(), tmTaskDocs()...)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install the provider and the kind: %v", err)
	}
	// Nothing points at `task` yet, so the writer names its own id.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: tmTaskType, ID: "groceries", Properties: map[string]any{"name": "Groceries"},
	})

	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{tmMappingDoc()}); err != nil {
		t.Fatalf("declare the mapping: %v", err)
	}
	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: tmTaskType, ID: "laundry", Properties: map[string]any{"name": "Laundry"},
	})
	wantErr(t, err, substrate.ErrValidation, "a chosen id on a mapped-to kind")

	// Server-assigned still works, and addressing the record that already
	// exists is not naming it.
	mustPut(t, ds, owner, substrate.PutInput{Kind: tmTaskType, Properties: map[string]any{"name": "Laundry"}})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: tmTaskType, ID: "groceries", Properties: map[string]any{"name": "Groceries, again"},
	})
}

// One mirror kind reaches TWO of the repository's kinds through two subject
// slots: the key is (source kind, subject property), so both mappings stand and
// both subjects resolve.
func TestOneMirrorReachesTwoKindsThroughTwoSlots(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	kind := vocabulary.KindManifest(tmPackage,
		map[string]any{"singular": "issue", "plural": "issues"},
		map[string]any{
			"properties": map[string]any{
				"headline": map[string]any{"type": "string"},
				"task":     map[string]any{"type": "reference", "mustExist": true, "subject": true},
				"note":     map[string]any{"type": "reference", "mustExist": true, "subject": true},
			},
		})
	docs := []map[string]any{
		vocabulary.PackageManifest(tmPackage, 1),
		vocabulary.ActorManifest(tmPackage, vocabulary.PackageActor(tmPackage)),
		kind,
	}
	docs = append(docs, tmTaskDocs()...)
	docs = append(docs,
		vocabulary.KindManifest(tmHomePackage,
			map[string]any{"singular": "note", "plural": "notes"},
			map[string]any{"properties": map[string]any{
				"summary": map[string]any{"type": "string"},
			}}),
		tmMappingDoc(),
		vocabulary.MappingManifest(tmHomePackage, "issuenote", map[string]any{
			"from": tmIssueType, "to": tmNoteType, "property": "note",
			"map": map[string]any{"summary": map[string]any{"path": "headline"}},
		}),
	)
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install two mappings from one mirror: %v", err)
	}

	row := mustPut(t, ds, owner, substrate.PutInput{
		Kind: tmIssueType, ID: "issue-1",
		Properties: map[string]any{"headline": "Two slots"},
	})
	full := mustGet(t, ds, row.Kind, row.ID)
	taskKind, taskID, okTask := vocabulary.SplitRecordPath(refPathValue(full, "task"))
	noteKind, noteID, okNote := vocabulary.SplitRecordPath(refPathValue(full, "note"))
	if !okTask || !okNote || taskKind != tmTaskType || noteKind != tmNoteType {
		t.Fatalf("the two slots hold %v and %v", full.Properties["task"], full.Properties["note"])
	}
	if got := mustGet(t, ds, tmTaskType, taskID).Properties["name"]; got != "Two slots" {
		t.Fatalf("the task mapping projected %v", got)
	}
	if got := mustGet(t, ds, tmNoteType, noteID).Properties["summary"]; got != "Two slots" {
		t.Fatalf("the note mapping projected %v", got)
	}
}

// THE MAPPING IS THE WRITE-TIME PIN. The mirror's subject slot declares no
// kind, so without the mapping it would admit a record of any kind at all; the
// engine treats the mapping's `to` as the pin, so a note path in the task slot
// is refused and a bare id in it resolves against the task kind.
func TestTheMappingPinsAnUnpinnedSubjectSlot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	docs := append(tmProviderDocs(), tmTaskDocs()...)
	docs = append(docs,
		vocabulary.KindManifest(tmHomePackage,
			map[string]any{"singular": "note", "plural": "notes"},
			map[string]any{"properties": map[string]any{
				"summary": map[string]any{"type": "string"},
			}}),
		tmMappingDoc())
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install: %v", err)
	}
	note := mustPut(t, ds, owner, substrate.PutInput{
		Kind: tmNoteType, ID: "n1", Properties: map[string]any{"summary": "not a task"},
	})

	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: tmIssueType, ID: "issue-note",
		Properties: map[string]any{
			"headline": "wrong kind",
			"task":     vocabulary.RecordPath(tmNoteType, note.ID),
		},
	})
	wantErr(t, err, substrate.ErrValidation, "a note in the task slot")

	// The same pin completes a BARE id, which is what a slot with no
	// declaration of its own could never do.
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: tmTaskType, Properties: map[string]any{"name": "Named by hand"},
	})
	row := mustPut(t, ds, owner, substrate.PutInput{
		Kind: tmIssueType, ID: "issue-bare",
		Properties: map[string]any{"headline": "bare id", "task": task.ID},
	})
	if got := tmSubjectOf(t, ds, row.ID); got != task.ID {
		t.Fatalf("the bare id resolved to %q, want the task %q", got, task.ID)
	}
}

// TWO ADMITTED MAPPINGS ARE NOT A CHOICE. A trait-pinned reference admits both
// kinds a mirror projects onto, so the subject hop has two answers for one
// value: it refuses naming both mappings rather than taking one by load order.
func TestSubjectHopRefusesTwoAdmittedMappings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	mirror := vocabulary.KindManifest(tmPackage,
		map[string]any{"singular": "issue", "plural": "issues"},
		map[string]any{
			"properties": map[string]any{
				"headline": map[string]any{"type": "string"},
				"task":     map[string]any{"type": "reference", "mustExist": true, "subject": true},
				"note":     map[string]any{"type": "reference", "mustExist": true, "subject": true},
			},
		})
	docs := []map[string]any{
		vocabulary.PackageManifest(tmPackage, 1),
		vocabulary.ActorManifest(tmPackage, vocabulary.PackageActor(tmPackage)),
		mirror,
		vocabulary.PackageManifest(tmHomePackage, 1),
		{
			"kind":     "substrate.reamde.dev/core/trait",
			"metadata": map[string]any{"id": tmHomePackage + "/titled"},
			"data": map[string]any{
				"authority":  "ada.example.com",
				"package":    "work",
				"properties": map[string]any{"name": "string"},
			},
		},
		vocabulary.KindManifest(tmHomePackage,
			map[string]any{"singular": "task", "plural": "tasks"},
			map[string]any{
				"traits":     []any{"titled"},
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			}),
		vocabulary.KindManifest(tmHomePackage,
			map[string]any{"singular": "note", "plural": "notes"},
			map[string]any{
				"traits":     []any{"titled"},
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			}),
		// The bookmark points at "something titled", which both mapping
		// targets are.
		vocabulary.KindManifest(tmHomePackage,
			map[string]any{"singular": "bookmark", "plural": "bookmarks"},
			map[string]any{"properties": map[string]any{
				"about": map[string]any{"type": "reference", "trait": "titled", "mustExist": true},
			}}),
		vocabulary.MappingManifest(tmHomePackage, "issuetask", map[string]any{
			"from": tmIssueType, "to": tmTaskType, "property": "task",
			"map": map[string]any{"name": map[string]any{"path": "headline"}},
		}),
		vocabulary.MappingManifest(tmHomePackage, "issuenote", map[string]any{
			"from": tmIssueType, "to": tmNoteType, "property": "note",
			"map": map[string]any{"name": map[string]any{"path": "headline"}},
		}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install: %v", err)
	}
	issue := mustPut(t, ds, owner, substrate.PutInput{
		Kind: tmIssueType, ID: "issue-1", Properties: map[string]any{"headline": "Ambiguous"},
	})

	_, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: tmHomePackage + "/bookmark",
		Properties: map[string]any{
			"about": vocabulary.RecordPath(tmIssueType, issue.ID),
		},
	})
	wantErr(t, err, substrate.ErrValidation, "a hop with two admitted mappings")
	if !strings.Contains(err.Error(), tmHomePackage+"/issuetask") ||
		!strings.Contains(err.Error(), tmHomePackage+"/issuenote") {
		t.Fatalf("the refusal must name both mappings: %v", err)
	}
}

// The same refusal reaches every door that would drop the source kind: an
// upgrade of the provider closure that stops shipping it, the read-only
// upgrade preview, and a delete of the kind declaration on its own.
func TestDroppingAMappedSourceKindIsRefusedAtEveryDoor(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	tmInstall(t, ds)

	// The provider's next release drops the mirror kind: everything else in
	// the closure stands, and the bundle document lists what is left.
	shrunk := []map[string]any{
		vocabulary.PackageManifest(tmPackage, 2),
		vocabulary.ActorManifest(tmPackage, vocabulary.PackageActor(tmPackage)),
		vocabulary.BundleManifest(tmPackage, map[string]any{
			"description": "a provider that dropped its mirror",
			"installs":    []any{tmPackage + "/leftover"},
		}),
		vocabulary.KindManifest(tmPackage,
			map[string]any{"singular": "leftover", "plural": "leftovers"},
			map[string]any{"properties": map[string]any{
				"name": map[string]any{"type": "string"},
			}}),
	}

	// The PREVIEW says so without writing anything.
	planner, ok := ds.(substrate.BundleUpgradePlanner)
	if !ok {
		t.Fatal("dataset does not plan bundle upgrades")
	}
	plan, err := planner.PlanBundleUpgrade(ctx, shrunk)
	if err != nil {
		t.Fatalf("plan the upgrade: %v", err)
	}
	if !tmBlocked(plan.Blockers) {
		t.Fatalf("the preview reported no mapping blocker: %+v", plan.Blockers)
	}

	// The APPLY refuses on the same line.
	_, err = applier(t, ds).ApplyVocabularyDocuments(ctx, owner, shrunk)
	wantErr(t, err, substrate.ErrGuard, "an upgrade dropping a mapped source kind")
	if !tmBlocked([]string{err.Error()}) {
		t.Fatalf("the refusal must name the mapping: %v", err)
	}
	if !hasType(t, ds, tmIssueType) {
		t.Fatal("a refused upgrade took the mirror kind with it")
	}

	// So does deleting the KIND DECLARATION on its own, the other way a kind
	// leaves a repository. It runs against a second provider that ships no
	// bundle document, because a bundle's `installs:` refuses the delete
	// first, on its own closure rule.
	const plainPackage = "plain.example.com/plain"
	const plainRowType = plainPackage + "/row"
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(plainPackage, 1),
		vocabulary.KindManifest(plainPackage,
			map[string]any{"singular": "row", "plural": "rows"},
			map[string]any{"properties": map[string]any{
				"headline": map[string]any{"type": "string"},
				"task":     map[string]any{"type": "reference", "mustExist": true, "subject": true},
			}}),
		vocabulary.MappingManifest(tmHomePackage, "rowtask", map[string]any{
			"from": plainRowType, "to": tmTaskType, "property": "task",
		}),
	}); err != nil {
		t.Fatalf("install the second provider: %v", err)
	}
	_, err = ds.Delete(ctx, owner, "substrate.reamde.dev/core/kind", plainRowType)
	wantErr(t, err, substrate.ErrGuard, "deleting a mapped source kind")
	if !strings.Contains(err.Error(), tmHomePackage+"/rowtask") ||
		!strings.Contains(err.Error(), plainRowType) {
		t.Fatalf("the refusal must name the mapping: %v", err)
	}
}

// tmBlocked reports whether a guard list names the mapping and its source.
func tmBlocked(lines []string) bool {
	for _, line := range lines {
		if strings.Contains(line, tmMapping) && strings.Contains(line, tmIssueType) {
			return true
		}
	}
	return false
}

// Uninstalling the provider is refused while a mapping in another package names
// its kind as a source, and the refusal names the mapping. Deleting the mapping
// is what unblocks it.
func TestUninstallRefusesWhileAMappingNamesTheProvidersKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	tmInstall(t, ds)
	ops := bundler(t, ds)

	// No live mirror rows, so the only thing standing in the way is the
	// mapping the repository's own package declares.
	err := ops.UninstallBundle(ctx, tmPackage)
	wantErr(t, err, substrate.ErrGuard, "uninstall while a mapping names the provider's kind")
	if !strings.Contains(err.Error(), tmMapping) || !strings.Contains(err.Error(), tmIssueType) {
		t.Fatalf("the refusal must name the mapping and the kind: %v", err)
	}
	// The refusal rolled back whole: the kind and the mapping both stand.
	if !hasType(t, ds, tmIssueType) {
		t.Fatal("a refused uninstall took the mirror kind with it")
	}
	mustGet(t, ds, tmMappingKind, tmMapping)

	if _, err := ds.Delete(ctx, owner, tmMappingKind, tmMapping); err != nil {
		t.Fatalf("delete the mapping: %v", err)
	}
	if err := ops.UninstallBundle(ctx, tmPackage); err != nil {
		t.Fatalf("uninstall after the mapping went: %v", err)
	}
	if hasType(t, ds, tmIssueType) {
		t.Fatal("the mirror kind survived the uninstall")
	}
}
