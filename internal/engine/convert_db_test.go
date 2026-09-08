package engine_test

// A backfill and an enum remap are ordinary record writes (decision 0066),
// composed with the rename in one pass over a kind's records: a property that
// becomes required with a default beside it fills every live record lacking a
// value, and an enum value naming its previous spelling with `renamedFrom`
// rewrites every live record holding it. These hold what each step writes
// (the value, the manager row, the embed queue), what refuses one (a remap
// onto a value still admitted), that a record several steps touch appends one
// entry, and that a fresh replay reproduces the converted records.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	cvPackage = "convert.example.substrate.reamde.dev/cv"
	cvWidget  = cvPackage + "/widget"
)

// cvApply declares the widget kind with the given properties.
func cvApply(t *testing.T, ds substrate.Dataset, props map[string]any) error {
	t.Helper()
	_, err := applier(t, ds).ApplyVocabularyDocuments(context.Background(), owner, []map[string]any{
		vocabulary.PackageManifest(cvPackage, 0),
		vocabulary.KindManifest(cvPackage, map[string]any{"singular": "widget", "plural": "widgets"},
			map[string]any{"properties": props}),
	})
	return err
}

// cvEntries counts the changelog entries after head whose payload carries the
// given step key.
func cvEntries(t *testing.T, dsn string, head int64, key string) int {
	t.Helper()
	var n int
	if err := rawDB(t, dsn).QueryRow(`SELECT count(*) FROM changelog WHERE seq > $1 AND kind = $2 AND op = 'patch' AND payload->$3 IS NOT NULL`,
		head, cvWidget, key).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// cvReplays asserts a rebuild and an import into a fresh database both
// reproduce the fold byte for byte: the conversion is values in the changelog,
// never a fold-time reading of the declaration.
func cvReplays(t *testing.T, svc substrate.Service, ds substrate.Dataset) {
	t.Helper()
	ctx := context.Background()
	before := foldOf(t, ds)
	rb, ok := svc.(rebuilder)
	if !ok {
		t.Fatal("the service cannot rebuild a repository")
	}
	if _, err := rb.RebuildRepository(ctx, testdb.Username(t)); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if got := foldOf(t, ds); string(got) != string(before) {
		t.Fatalf("the rebuilt fold is not the fold\n%s", firstDifference(before, got))
	}
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	root2 := copyRepositoryDir(t, root, id)
	svc2 := mustReopen(t, testdb.NewSchema(t), root2)
	defer func() { _ = svc2.Close() }()
	ds2, err := svc2.Dataset(ctx, testdb.Username(t))
	if err != nil {
		t.Fatalf("open the imported repository: %v", err)
	}
	if got := foldOf(t, ds2); string(got) != string(before) {
		t.Fatalf("the imported fold is not the fold\n%s", firstDifference(before, got))
	}
}

func TestBackfillWritesTheDefaultOntoEveryRecordLackingAValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	base := map[string]any{
		"name": map[string]any{"type": "string"},
		"mood": map[string]any{"type": "string", "embed": true},
	}
	if err := cvApply(t, ds, base); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	// Three live widgets: one carrying a mood, one without the property, one
	// holding the empty string, which `required` refuses exactly as it refuses
	// an absent value (emptyValue), so the backfill must fill it too.
	has := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "a", "mood": "cheerful"}})
	lacks := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "b"}})
	empty := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "c", "mood": ""}})
	head := maxSeq(t, ds)

	// Without a default the narrowing still refuses with the count.
	props := map[string]any{
		"name": map[string]any{"type": "string"},
		"mood": map[string]any{"type": "string", "embed": true, "required": true},
	}
	wantNarrowingGuard(t, cvApply(t, ds, props), `property "mood" becomes required`, "2 live records", "declare a default")

	// A default `required` itself refuses is not a backfill: the declaration
	// is refused, or every backfilled record would refuse its next write.
	for _, empty := range []map[string]any{
		{"type": "string", "embed": true, "required": true, "default": ""},
		{"type": "json", "required": true, "default": []any{}},
		{"type": "json", "required": true, "default": map[string]any{}},
	} {
		props["mood"] = empty
		err := cvApply(t, ds, props)
		if err == nil || !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), "is what having none means") {
			t.Fatalf("a required property with an empty default %v must refuse at admission, got: %v", empty["default"], err)
		}
	}
	if got := mustGet(t, ds, cvWidget, lacks.ID); got.Version != lacks.Version {
		t.Fatalf("a refused empty default touched a record: %+v", got)
	}

	// With one, the apply lands and writes it.
	props["mood"] = map[string]any{"type": "string", "embed": true, "required": true, "default": "neutral"}
	if err := cvApply(t, ds, props); err != nil {
		t.Fatalf("required with a default must land: %v", err)
	}
	if got := mustGet(t, ds, cvWidget, has.ID); got.Properties["mood"] != "cheerful" || got.Version != has.Version {
		t.Fatalf("a record carrying a value was rewritten: %+v", got)
	}
	for _, r := range []*substrate.Record{lacks, empty} {
		got := mustGet(t, ds, cvWidget, r.ID)
		if got.Properties["mood"] != "neutral" {
			t.Fatalf("%s did not receive the default: %v", r.ID, got.Properties)
		}
		if got.Version == r.Version {
			t.Fatalf("%s was rewritten without moving its version", r.ID)
		}
		if want := kindVersion(t, ds, cvWidget); got.KindVersion != want {
			t.Fatalf("%s carries kindVersion %d, want the declaration's %d", r.ID, got.KindVersion, want)
		}
	}

	// The backfilled value is a write by the hand that applied the
	// declaration, at its tier, and the text is queued to embed.
	db := rawDB(t, dsn)
	var actor, tier string
	if err := db.QueryRow(`SELECT actor, tier FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = 'mood'`,
		cvWidget, lacks.ID).Scan(&actor, &tier); err != nil {
		t.Fatalf("read the manager row: %v", err)
	}
	if actor != string(owner) || tier != string(substrate.TierOwner) {
		t.Fatalf("the backfilled value's manager = %q at %q", actor, tier)
	}
	var queued int
	if err := db.QueryRow(`SELECT count(*) FROM embed_queue WHERE record_kind = $1 AND record_id IN ($2, $3) AND property = 'mood'`,
		cvWidget, lacks.ID, empty.ID).Scan(&queued); err != nil || queued != 2 {
		t.Fatalf("embed queue rows for the backfilled records = %d, %v; want 2", queued, err)
	}
	// One entry per backfilled record, a patch that says the apply filled it.
	if n := cvEntries(t, dsn, head, "backfilled"); n != 2 {
		t.Fatalf("the backfill wrote %d record entries, want 2", n)
	}

	// The declaration holds afterwards: a create without the property takes
	// the default, a patch clearing it is refused.
	fresh := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "d"}})
	if fresh.Properties["mood"] != "neutral" {
		t.Fatalf("a create did not take the default: %v", fresh.Properties)
	}
	if _, err := ds.Patch(ctx, owner, cvWidget, fresh.ID, substrate.PatchInput{Properties: map[string]any{"mood": nil}}); err == nil {
		t.Fatal("clearing a required property must refuse")
	}
	cvReplays(t, svc, ds)
}

func TestRemapRewritesEveryRecordHoldingTheOldValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	base := map[string]any{
		"status":  map[string]any{"type": "enum", "values": []any{"open", "active", "closed"}},
		"moods":   map[string]any{"type": "enum", "repeated": true, "values": []any{"calm", "tense"}},
		"ratings": map[string]any{"type": "enum", "keyed": true, "values": []any{"good", "bad"}},
	}
	if err := cvApply(t, ds, base); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	// One record holding the old spelling in all three shapes, one holding
	// none of them, and one a connector wrote, whose manager row the remap
	// must leave as it found it.
	holds := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{
		"status": "active", "moods": []any{"calm", "tense", "calm"}, "ratings": map[string]any{"x": "bad", "y": "good"},
	}})
	clean := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{
		"status": "open", "moods": []any{"calm"}, "ratings": map[string]any{"z": "good"},
	}})
	lib := substrate.Actor("connector:library")
	theirs := mustPut(t, ds, lib, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"status": "active"}})
	db := rawDB(t, dsn)
	var libStamp time.Time
	if err := db.QueryRow(`SELECT updated_at FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = 'status'`,
		cvWidget, theirs.ID).Scan(&libStamp); err != nil {
		t.Fatalf("read the connector's manager stamp: %v", err)
	}
	head := maxSeq(t, ds)

	// Dropping the values outright still refuses with the count.
	wantNarrowingGuard(t, cvApply(t, ds, map[string]any{
		"status":  map[string]any{"type": "enum", "values": []any{"open", "closed"}},
		"moods":   base["moods"],
		"ratings": base["ratings"],
	}), `property "status" removes value(s) "active"`, "2 live records")

	// Renamed, the values move.
	if err := cvApply(t, ds, map[string]any{
		"status": map[string]any{"type": "enum", "values": []any{
			"open", map[string]any{"value": "working", "renamedFrom": "active"}, "closed",
		}},
		"moods": map[string]any{"type": "enum", "repeated": true, "values": []any{
			"calm", map[string]any{"value": "anxious", "renamedFrom": "tense"},
		}},
		"ratings": map[string]any{"type": "enum", "keyed": true, "values": []any{
			"good", map[string]any{"value": "poor", "renamedFrom": "bad"},
		}},
	}); err != nil {
		t.Fatalf("the remap must land: %v", err)
	}
	got := mustGet(t, ds, cvWidget, holds.ID)
	if got.Properties["status"] != "working" {
		t.Fatalf("the scalar did not move: %v", got.Properties)
	}
	if moods, _ := got.Properties["moods"].([]any); len(moods) != 3 || moods[0] != "calm" || moods[1] != "anxious" || moods[2] != "calm" {
		t.Fatalf("the list did not move element by element, in order: %v", got.Properties["moods"])
	}
	if ratings, _ := got.Properties["ratings"].(map[string]any); ratings["x"] != "poor" || ratings["y"] != "good" {
		t.Fatalf("the keyed map did not move value by value: %v", got.Properties["ratings"])
	}
	if got.Version == holds.Version {
		t.Fatal("a rewritten record must move its version")
	}
	if got := mustGet(t, ds, cvWidget, clean.ID); got.Version != clean.Version {
		t.Fatalf("a record holding none of the old spellings was rewritten: %+v", got)
	}
	// The spelling moved, not who wrote it: the connector's row stands, stamp
	// included.
	var actor, tier string
	var stamp time.Time
	if err := db.QueryRow(`SELECT actor, tier, updated_at FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property = 'status'`,
		cvWidget, theirs.ID).Scan(&actor, &tier, &stamp); err != nil {
		t.Fatalf("read the manager row: %v", err)
	}
	if actor != string(lib) || tier != string(substrate.TierMachine) || !stamp.Equal(libStamp) {
		t.Fatalf("the remap touched the manager row: %q at %q, stamped %s (was %s)", actor, tier, stamp, libStamp)
	}
	if mustGet(t, ds, cvWidget, theirs.ID).Properties["status"] != "working" {
		t.Fatal("the connector's record did not move")
	}
	if n := cvEntries(t, dsn, head, "remapped"); n != 2 {
		t.Fatalf("the remap wrote %d record entries, want 2 (one per record holding an old spelling)", n)
	}
	// The old spelling is undeclared afterwards.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"status": "active"}}); err == nil {
		t.Fatal("the old spelling must be refused once the remap landed")
	}
	cvReplays(t, svc, ds)
}

// A backfill can never write a reference nothing validated: a reference
// property declares no `default` at all (referencePropKeys), so the loader
// refuses the pair before any row is read, and the record is untouched. The
// backfill still puts every value through the write path's reference
// validation (backfillValues), so reserving the key would not open a hole.
func TestBackfillCannotDeclareAReferenceDefault(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	const holder = cvPackage + "/holder"
	name := map[string]any{"type": "string"}
	if err := cvApply(t, ds, map[string]any{"name": name}); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	orphan := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"name": "a"}})

	err := cvApply(t, ds, map[string]any{"name": name, "holder": map[string]any{
		"type": "reference", "kind": holder, "required": true, "mustExist": true, "default": holder + "/nobody",
	}})
	if !errors.Is(err, substrate.ErrValidation) || !strings.Contains(err.Error(), `unknown key "default"`) {
		t.Fatalf("a reference with a default must refuse at the loader, got: %v", err)
	}
	if got := mustGet(t, ds, cvWidget, orphan.ID); got.Version != orphan.Version || got.Properties["holder"] != nil {
		t.Fatalf("a refused declaration touched the record: %+v", got)
	}
}

// cvMappedClosure is the widget beside a source kind describing it: the source
// carries a `status` value set and a subject reference, and the mapping
// projects its status onto the widget's. values is the status set both kinds
// declare.
func cvMappedClosure(values []any) []map[string]any {
	const source = cvPackage + "/widgetsource"
	return []map[string]any{
		vocabulary.PackageManifest(cvPackage, 0),
		vocabulary.KindManifest(cvPackage, map[string]any{"singular": "widget", "plural": "widgets"},
			map[string]any{"properties": map[string]any{
				"status": map[string]any{"type": "enum", "values": values},
			}}),
		vocabulary.KindManifest(cvPackage, map[string]any{"singular": "widgetsource", "plural": "widgetsources"},
			map[string]any{"properties": map[string]any{
				"status": map[string]any{"type": "enum", "values": values},
				"widget": map[string]any{
					"type": "reference", "kind": cvWidget,
					"required": true, "mustExist": true, "subject": true,
				},
			}}),
		vocabulary.MappingManifest(cvPackage, "widgetsourcewidget", map[string]any{
			"from": source, "to": cvWidget, "property": "widget",
			"map": map[string]any{"status": map[string]any{"path": "status"}},
		}),
	}
}

// A converted record is a source write like any other: its subjects recompute
// in the same transaction, so a mapped target and its offer rows read the new
// spelling, a rebuild (which derives the offers from the sources) agrees with
// the live fold, and a later source write is not refused for projecting a
// value the declaration dropped.
func TestRemapRecomputesTheSubjectsOfAMappedSource(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	const source = cvPackage + "/widgetsource"
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, cvMappedClosure([]any{"open", "active"})); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	lib := substrate.Actor("connector:library")
	src := mustPut(t, ds, lib, substrate.PutInput{Kind: source, ID: "src:1", Properties: map[string]any{"status": "active"}})
	targetKind, targetID, _ := vocabulary.SplitRecordPath(refPathValue(mustGet(t, ds, source, src.ID), "widget"))
	if targetKind != cvWidget || mustGet(t, ds, cvWidget, targetID).Properties["status"] != "active" {
		t.Fatalf("the mapping did not project the source's status onto a widget")
	}
	db := rawDB(t, dsn)
	offerValue := func() string {
		t.Helper()
		var raw string
		if err := db.QueryRow(`SELECT value::text FROM property_offers WHERE record_kind = $1 AND record_id = $2 AND property = 'status'`,
			cvWidget, targetID).Scan(&raw); err != nil {
			t.Fatalf("read the offer row: %v", err)
		}
		return raw
	}
	if got := offerValue(); got != `"active"` {
		t.Fatalf("offer before the remap = %s", got)
	}

	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, cvMappedClosure([]any{
		"open", map[string]any{"value": "working", "renamedFrom": "active"},
	})); err != nil {
		t.Fatalf("the remap must land: %v", err)
	}
	if got := mustGet(t, ds, source, src.ID); got.Properties["status"] != "working" {
		t.Fatalf("the source did not move: %v", got.Properties)
	}
	if got := mustGet(t, ds, cvWidget, targetID); got.Properties["status"] != "working" {
		t.Fatalf("the mapped target did not follow its source: %v", got.Properties)
	}
	if got := offerValue(); got != `"working"` {
		t.Fatalf("offer after the remap = %s, want the new spelling", got)
	}
	// The source keeps working under the new declaration, and its target
	// follows as before.
	mustPut(t, ds, lib, substrate.PutInput{Kind: source, ID: "src:1", Properties: map[string]any{"status": "open"}})
	if got := mustGet(t, ds, cvWidget, targetID); got.Properties["status"] != "open" {
		t.Fatalf("a later source write did not project: %v", got.Properties)
	}
	cvReplays(t, svc, ds)
}

// A remap onto a value the stored declaration still admits would make the
// records holding either spelling one set. Nothing here discards a stored
// distinction, so it refuses by declaration, under the named error, and the
// same intent onto a new value lands.
func TestRemapOntoARetainedValueIsRefusedAsLossy(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := cvApply(t, ds, map[string]any{
		"status": map[string]any{"type": "enum", "values": []any{"open", "active"}},
	}); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	held := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"status": "active"}})

	err := cvApply(t, ds, map[string]any{
		"status": map[string]any{"type": "enum", "values": []any{
			map[string]any{"value": "open", "renamedFrom": "active"},
		}},
	})
	if err == nil {
		t.Fatal("a remap onto a retained value must refuse")
	}
	if !errors.Is(err, substrate.ErrGuard) || !errors.Is(err, substrate.ErrLossyConversion) {
		t.Fatalf("the refusal must be a guard AND the named lossy error, got: %v", err)
	}
	if !strings.Contains(err.Error(), `property "status" renames value "active" onto "open", which the stored declaration still admits`) {
		t.Fatalf("the refusal must name the collapse, got: %v", err)
	}
	if got := mustGet(t, ds, cvWidget, held.ID); got.Properties["status"] != "active" || got.Version != held.Version {
		t.Fatalf("a refused remap touched the record: %+v", got)
	}

	if err := cvApply(t, ds, map[string]any{
		"status": map[string]any{"type": "enum", "values": []any{
			"open", map[string]any{"value": "working", "renamedFrom": "active"},
		}},
	}); err != nil {
		t.Fatalf("the same rename onto a new value must land: %v", err)
	}
	if got := mustGet(t, ds, cvWidget, held.ID); got.Properties["status"] != "working" {
		t.Fatalf("the value did not move: %v", got.Properties)
	}
}

// The three steps compose on one kind in one apply: renames first, then
// backfills, then remaps, and a record any of them touches is rewritten once,
// so a rename that also becomes required with a default fills the records the
// old name was missing on, and an enum renamed with a value renamed inside it
// lands both in one entry.
func TestRenameBackfillAndRemapComposeIntoOneEntryPerRecord(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	if err := cvApply(t, ds, map[string]any{
		"size":  map[string]any{"type": "string"},
		"level": map[string]any{"type": "enum", "values": []any{"low", "high"}},
	}); err != nil {
		t.Fatalf("install the package: %v", err)
	}
	both := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"size": "big", "level": "high"}})
	unsized := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"level": "low"}})
	plain := mustPut(t, ds, owner, substrate.PutInput{Kind: cvWidget, Properties: map[string]any{"size": "small", "level": "low"}})
	head := maxSeq(t, ds)

	if err := cvApply(t, ds, map[string]any{
		"dimensions": map[string]any{"type": "string", "required": true, "default": "unsized", "renamedFrom": "size"},
		"grade": map[string]any{"type": "enum", "renamedFrom": "level", "values": []any{
			"low", map[string]any{"value": "top", "renamedFrom": "high"},
		}},
	}); err != nil {
		t.Fatalf("the composed conversion must land: %v", err)
	}
	want := map[string]map[string]any{
		both.ID:    {"dimensions": "big", "grade": "top"},
		unsized.ID: {"dimensions": "unsized", "grade": "low"},
		plain.ID:   {"dimensions": "small", "grade": "low"},
	}
	for id, props := range want {
		got := mustGet(t, ds, cvWidget, id)
		for k, v := range props {
			if got.Properties[k] != v {
				t.Fatalf("%s.%s = %v, want %v (%v)", id, k, got.Properties[k], v, got.Properties)
			}
		}
		for _, old := range []string{"size", "level"} {
			if _, still := got.Properties[old]; still {
				t.Fatalf("%s still carries %q: %v", id, old, got.Properties)
			}
		}
	}
	// One entry per record, whatever the number of steps that touched it.
	var entries int
	if err := rawDB(t, dsn).QueryRow(`SELECT count(*) FROM changelog WHERE seq > $1 AND kind = $2 AND op = 'patch'`,
		head, cvWidget).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 3 {
		t.Fatalf("the conversion wrote %d record entries, want 3 (one per record)", entries)
	}
	// Each entry says which steps moved it: the unsized record was renamed
	// (level) and backfilled (dimensions); the record holding both was renamed
	// twice and remapped once.
	stepsOf := func(id string) (renamed, backfilled, remapped bool) {
		t.Helper()
		err := rawDB(t, dsn).QueryRow(`SELECT payload ? 'renamed', payload ? 'backfilled', payload ? 'remapped' FROM changelog WHERE seq > $1 AND kind = $2 AND record_id = $3`,
			head, cvWidget, id).Scan(&renamed, &backfilled, &remapped)
		if err != nil {
			t.Fatalf("read the entry of %s: %v", id, err)
		}
		return renamed, backfilled, remapped
	}
	if r, b, m := stepsOf(unsized.ID); !r || !b || m {
		t.Fatalf("the unsized record's entry says renamed=%v backfilled=%v remapped=%v", r, b, m)
	}
	if r, b, m := stepsOf(both.ID); !r || b || !m {
		t.Fatalf("the full record's entry says renamed=%v backfilled=%v remapped=%v", r, b, m)
	}
	if r, b, m := stepsOf(plain.ID); !r || b || m {
		t.Fatalf("the plain record's entry says renamed=%v backfilled=%v remapped=%v", r, b, m)
	}
	cvReplays(t, svc, ds)
}
