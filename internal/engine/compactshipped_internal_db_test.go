package engine

// A RECORD A PACKAGE SHIPS IS OUTSIDE COMPACTION (decision 0310): an open's
// seed writes a shipped record only where no hand but the shipping ones ever
// wrote it, and reads that from the record's history, so a compaction leaves
// the history whole and the next open finds the owner's hand where the owner
// left it. A record nobody touched still takes the newer shipped one, and an
// ordinary record beside them is compacted as before.

import (
	"cmp"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const shippedViewKind = "substrate.reamde.dev/console/collectionview"

// shippedFixture is a repository with the open's seeds on, over a copy of the
// seeded tree the test may change before it opens the repository again.
type shippedFixture struct {
	t               *testing.T
	dsn, tree, root string
	opts            []Option
	svc             substrate.Service
	ds              *dataset
}

func newShippedFixture(t *testing.T, opts ...Option) *shippedFixture {
	t.Helper()
	f := &shippedFixture{t: t, dsn: MigratedDSN(t), tree: t.TempDir(), root: t.TempDir(), opts: opts}
	if err := os.CopyFS(f.tree, SeedKinds); err != nil {
		t.Fatalf("copy the seeded tree: %v", err)
	}
	f.open()
	if _, err := f.svc.CreateRepository(context.Background(), testdb.Repository(t)); err != nil {
		t.Fatalf("create: %v", err)
	}
	f.dataset()
	t.Cleanup(func() { _ = f.svc.Close() })
	return f
}

func (f *shippedFixture) open() {
	f.t.Helper()
	svc, err := OpenForTest(f.t, context.Background(), f.dsn, append([]Option{
		WithDataRoot(f.root), WithKindsDir(f.tree),
		WithRecordSeed(true), WithUnitSeed(true), WithLLMProviderSeed(true),
	}, f.opts...)...)
	if err != nil {
		f.t.Fatalf("open: %v", err)
	}
	f.svc = svc
}

func (f *shippedFixture) dataset() {
	f.t.Helper()
	d, err := f.svc.Dataset(context.Background(), testdb.Repository(f.t))
	if err != nil {
		f.t.Fatalf("open the repository: %v", err)
	}
	f.ds = d.(*dataset)
}

// reopen is the next open of the repository, by a binary over the tree as
// it stands now.
func (f *shippedFixture) reopen() {
	f.t.Helper()
	_ = f.svc.Close()
	f.open()
	f.dataset()
}

// lines is how many lines of the history are addressed to ref.
func (f *shippedFixture) lines(ref eref) int {
	f.t.Helper()
	var n int
	if err := f.ds.db.QueryRow(`SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2`, ref.Kind, ref.ID).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// row is the record's row, nil where it has none.
func (f *shippedFixture) row(ref eref) *erow {
	f.t.Helper()
	row, err := f.ds.loadRowDB(context.Background(), ref)
	if err != nil {
		f.t.Fatal(err)
	}
	return row
}

// box is an ordinary record's kind, which no package ships.
func (f *shippedFixture) box() string {
	f.t.Helper()
	const pkg = "boxes.example.com/boxes"
	if _, err := f.ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "box"}, map[string]any{
			"properties": map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		f.t.Fatal(err)
	}
	return pkg + "/box"
}

// touched is what a person does to shipped records of each seed: one view
// with a property changed and one removed, which the engine's own hand then
// writes again (an upgrade's backfill does), and one view, one conversion
// and one provider row deleted and swept. Each leaves a line of the shipping
// hands' newest, so a state on it would take the person's line away.
type touched struct {
	edited    eref
	deleted   []eref
	untouched eref
	ordinary  eref
}

func (f *shippedFixture) touch() touched {
	f.t.Helper()
	ctx := context.Background()
	ds := f.ds
	var conversion eref
	units, err := shippedUnitRecords()
	if err != nil {
		f.t.Fatal(err)
	}
	for _, u := range units {
		if u.kind == vocabulary.KindConversion {
			conversion = u.ref()
			break
		}
	}
	tc := touched{
		edited:    eref{Kind: shippedViewKind, ID: "memory-active"},
		untouched: eref{Kind: shippedViewKind, ID: "contact-2-organizations"},
		deleted: []eref{
			{Kind: shippedViewKind, ID: "memory-retired"},
			conversion,
			{Kind: typeProvider, ID: "gemini"},
		},
	}
	for _, r := range append([]eref{tc.edited, tc.untouched}, tc.deleted...) {
		if row := f.row(r); row == nil || row.DeletedAt != nil {
			f.t.Fatalf("the seed did not write %s %s", r.Kind, r.ID)
		}
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, tc.edited.Kind, tc.edited.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "Current", "default": nil},
	}); err != nil {
		f.t.Fatalf("the person edits a view: %v", err)
	}
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		_, err := t.patch(tc.edited, substrate.PatchInput{Labels: map[string]any{"system/backfilled": true}})
		return err
	}); err != nil {
		f.t.Fatalf("the engine writes the view after the person: %v", err)
	}
	for _, r := range tc.deleted {
		if _, err := ds.Delete(ctx, substrate.ActorPerson, r.Kind, r.ID, substrate.DeleteInput{}); err != nil {
			f.t.Fatalf("the person deletes %s %s: %v", r.Kind, r.ID, err)
		}
	}
	if _, err := ds.RunGC(ctx); err != nil {
		f.t.Fatalf("sweep: %v", err)
	}
	for _, r := range tc.deleted {
		if f.row(r) != nil {
			f.t.Fatalf("the sweep left %s %s", r.Kind, r.ID)
		}
	}
	kind := f.box()
	box, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kind, Properties: map[string]any{"name": "first"}})
	if err != nil {
		f.t.Fatal(err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, kind, box.ID, substrate.PatchInput{Properties: map[string]any{"name": "second"}}); err != nil {
		f.t.Fatal(err)
	}
	tc.ordinary = eref{Kind: kind, ID: box.ID}
	return tc
}

// shipNewer changes the untouched view in the tree, as a newer binary
// ships it.
func (f *shippedFixture) shipNewer() {
	f.t.Helper()
	path := filepath.Join(f.tree, vocabulary.PackageConsole, "views.yaml")
	raw, err := os.ReadFile(path)
	if err != nil {
		f.t.Fatal(err)
	}
	newer := strings.Replace(string(raw), "name: Organizations", "name: Companies", 1)
	if newer == string(raw) {
		f.t.Fatal("the shipped views name no Organizations view")
	}
	if err := os.WriteFile(path, []byte(newer), 0o600); err != nil {
		f.t.Fatal(err)
	}
}

// checkNextOpen holds the next open to what the person left: the edited
// view as edited, every deleted record gone, the untouched view at what the
// newer binary ships, and the ordinary record at its latest value.
func (f *shippedFixture) checkNextOpen(tc touched) {
	f.t.Helper()
	edited := f.row(tc.edited)
	if edited == nil || edited.DeletedAt != nil {
		f.t.Fatalf("the edited view is %+v after the next open", edited)
	}
	if name := edited.Props["name"]; name != "Current" {
		f.t.Errorf("the edited view is named %v after the next open, want the person's Current", name)
	}
	if def, ok := edited.Props["default"]; ok {
		f.t.Errorf("the next open put back the view's default (%v), which the person removed", def)
	}
	for _, r := range tc.deleted {
		if row := f.row(r); row != nil {
			f.t.Errorf("the next open brought back %s %s, which the person deleted", r.Kind, r.ID)
		}
	}
	if untouched := f.row(tc.untouched); untouched == nil || untouched.Props["name"] != "Companies" {
		f.t.Errorf("the view nobody touched is %+v after the next open, want the newer Companies", untouched)
	}
	if box := f.row(tc.ordinary); box == nil || box.Props["name"] != "second" {
		f.t.Errorf("the ordinary record is %+v after the next open", box)
	}
}

// `repository compact`: the shipped records keep every line, the ordinary
// one is compacted to its state, and the next open leaves each shipped
// record as the person left it.
func TestACompactionLeavesTheRecordsAPackageShipsWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newShippedFixture(t)
	tc := f.touch()
	shipped := append([]eref{tc.edited, tc.untouched}, tc.deleted...)
	before := map[eref]int{}
	for _, r := range shipped {
		before[r] = f.lines(r)
	}
	if n := f.lines(tc.ordinary); n < 2 {
		t.Fatalf("the ordinary record has %d lines before the compaction, want two", n)
	}
	if _, err := f.ds.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil {
		t.Fatalf("compact: %v", err)
	}
	for _, r := range shipped {
		if got := f.lines(r); got != before[r] {
			t.Errorf("%s %s has %d lines after the compaction, want its %d", r.Kind, r.ID, got, before[r])
		}
	}
	if n := f.lines(tc.ordinary); n != 1 {
		t.Errorf("the ordinary record has %d lines after the compaction, want its state's one", n)
	}
	f.shipNewer()
	f.reopen()
	f.checkNextOpen(tc)
}

const (
	shipperAuthority = "shipper.example.com"
	shipperPkg       = shipperAuthority + "/shipper"
	presetKind       = shipperPkg + "/preset"
)

// install takes the shipper package as its install or an upgrade does: three
// presets under the package's own hand, each with note.
func (f *shippedFixture) install(note string) {
	f.t.Helper()
	docs := []map[string]any{
		vocabulary.PackageManifest(shipperPkg, 0),
		vocabulary.BundleManifest(shipperPkg, map[string]any{"description": "the presets", "installs": []any{presetKind}}),
		vocabulary.KindManifest(shipperPkg, map[string]any{"singular": "preset"}, map[string]any{
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"note": map[string]any{"type": "string"},
			},
		}),
	}
	var data []substrate.PutInput
	for _, id := range []string{"edited", "deleted", "untouched"} {
		data = append(data, substrate.PutInput{Kind: presetKind, ID: id, Properties: map[string]any{"name": id, "note": note}})
	}
	if _, err := f.ds.InstallBundleClosure(context.Background(), substrate.BundleActor(shipperAuthority, "shipper"), docs, data, substrate.BundleInstall{}); err != nil {
		f.t.Fatalf("take the shipper package: %v", err)
	}
}

// mirror writes a preset as the package's function does, a mirror's: under
// the function's hand, twice.
func (f *shippedFixture) mirror(id string) eref {
	f.t.Helper()
	ref := eref{Kind: presetKind, ID: id}
	for _, name := range []string{"mirrored", "mirrored again"} {
		if err := f.ds.inTx(context.Background(), substrate.FunctionActor(shipperAuthority, "shipper", "mirror"), true, func(t *txn) error {
			_, err := t.put(substrate.PutInput{Kind: ref.Kind, ID: ref.ID, Properties: map[string]any{"name": name}})
			return err
		}); err != nil {
			f.t.Fatalf("the function writes %s: %v", id, err)
		}
	}
	return ref
}

// installedTouched is what a person does to the presets the package
// shipped: one with a property changed and one removed, which the engine
// then writes again, and one deleted and swept.
type installedTouched struct {
	edited, deleted, untouched, mirrored eref
}

func (f *shippedFixture) touchInstalled() installedTouched {
	f.t.Helper()
	ctx := context.Background()
	f.install("first")
	ti := installedTouched{
		edited:    eref{Kind: presetKind, ID: "edited"},
		deleted:   eref{Kind: presetKind, ID: "deleted"},
		untouched: eref{Kind: presetKind, ID: "untouched"},
		mirrored:  f.mirror("mirrored"),
	}
	if _, err := f.ds.Patch(ctx, substrate.ActorPerson, presetKind, ti.edited.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "Mine", "note": nil},
	}); err != nil {
		f.t.Fatalf("the person edits a preset: %v", err)
	}
	if err := f.ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		_, err := t.patch(ti.edited, substrate.PatchInput{Labels: map[string]any{"system/backfilled": true}})
		return err
	}); err != nil {
		f.t.Fatalf("the engine writes the preset after the person: %v", err)
	}
	if _, err := f.ds.Delete(ctx, substrate.ActorPerson, presetKind, ti.deleted.ID, substrate.DeleteInput{}); err != nil {
		f.t.Fatalf("the person deletes a preset: %v", err)
	}
	if _, err := f.ds.RunGC(ctx); err != nil {
		f.t.Fatalf("sweep: %v", err)
	}
	return ti
}

// checkUpgrade holds the package's upgrade to what the person left: the
// edited preset as edited, the deleted one gone, the untouched one at the
// newer note, and the mirror's record as the function left it.
func (f *shippedFixture) checkUpgrade(ti installedTouched) {
	f.t.Helper()
	f.install("newer")
	if row := f.row(ti.edited); row == nil || row.Props["name"] != "Mine" {
		f.t.Errorf("the upgrade left the edited preset as %+v, want the person's Mine", row)
	} else if note, ok := row.Props["note"]; ok {
		f.t.Errorf("the upgrade put back the preset's note (%v), which the person removed", note)
	}
	if row := f.row(ti.deleted); row != nil {
		f.t.Errorf("the upgrade brought back the preset the person deleted: %+v", row)
	}
	if row := f.row(ti.untouched); row == nil || row.Props["note"] != "newer" {
		f.t.Errorf("the preset nobody touched is %+v after the upgrade, want the newer note", row)
	}
	if row := f.row(ti.mirrored); row == nil || row.Props["name"] != "mirrored again" {
		f.t.Errorf("the mirror's record is %+v after the upgrade", row)
	}
}

// `repository compact` over what an installed package ships: the presets
// keep every line and the upgrade leaves each as the person left it, while a
// record the package's function wrote, a mirror's, is compacted.
func TestACompactionLeavesWhatAnInstalledPackageShipsWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newShippedFixture(t)
	ti := f.touchInstalled()
	shipped := []eref{ti.edited, ti.deleted, ti.untouched}
	before := map[eref]int{}
	for _, r := range shipped {
		before[r] = f.lines(r)
	}
	if _, err := f.ds.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil {
		t.Fatalf("compact: %v", err)
	}
	for _, r := range shipped {
		if got := f.lines(r); got != before[r] {
			t.Errorf("preset %s has %d lines after the compaction, want its %d", r.ID, got, before[r])
		}
	}
	if n := f.lines(ti.mirrored); n != 1 {
		t.Errorf("the mirror's record has %d lines after the compaction, want its state's one", n)
	}
	f.checkUpgrade(ti)
}

// A package purged at a person's request, as the API serves it, and taken
// again writes its shipped records again (anotherHandWrote): the purge's
// lines are the engine's, whomever they acted for, and a shipped record is
// held only by a line whose actor is none of the shipping hands.
func TestAPackagePurgedForAPersonShipsItsRecordsAgain(t *testing.T) {
	t.Parallel()
	ctx := substrate.WithOnBehalfOf(context.Background(), substrate.ActorPerson)
	f := newShippedFixture(t)
	f.install("first")
	preset := eref{Kind: presetKind, ID: "untouched"}
	if err := f.ds.DisableBundle(ctx, shipperPkg); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, err := f.ds.PurgeBundle(ctx, shipperPkg); err != nil {
		t.Fatalf("purge: %v", err)
	}
	if row := f.row(preset); row == nil || row.DeletedAt == nil {
		t.Fatalf("the purge left the preset %+v, want it deleted", row)
	}
	var forPerson int
	if err := f.ds.db.QueryRowContext(context.Background(), `
		SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2 AND actor = $3 AND coalesce(on_behalf_of, '') <> ''`,
		preset.Kind, preset.ID, actorID(substrate.ActorSystem)).Scan(&forPerson); err != nil || forPerson == 0 {
		t.Fatalf("no line of the purge is the engine's for the person (%v): the test proves nothing", err)
	}
	if err := f.ds.EnableBundle(ctx, shipperPkg); err != nil {
		t.Fatalf("enable: %v", err)
	}
	f.install("again")
	if row := f.row(preset); row == nil || row.DeletedAt != nil || row.Props["note"] != "again" {
		t.Fatalf("the preset is %+v after the package is taken again, want it written again", row)
	}
}

// A compaction leaves the hands that wrote a record as they were
// (recordHands): an ordinary record written by the owner, by a package's
// function and by the engine acting for the owner, deleted and written
// again, compacted to one line, reads the same hands, each with whom it
// acted for and its line's operation, before and after.
func TestACompactionLeavesTheHandsThatWroteARecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newShippedFixture(t)
	kind := f.box()
	rec, err := f.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kind, Properties: map[string]any{"name": "owner"}})
	if err != nil {
		t.Fatal(err)
	}
	ref := eref{Kind: kind, ID: rec.ID}
	for _, w := range []struct {
		actor, behalf substrate.Actor
		name          string
	}{
		{substrate.FunctionActor(shipperAuthority, "shipper", "mirror"), "", "function"},
		{substrate.ActorSystem, substrate.ActorPerson, "engine"},
		{substrate.ActorPerson, "", "owner again"},
	} {
		if err := f.ds.inTx(ctx, w.actor, true, func(t *txn) error {
			if w.behalf != "" {
				t.onBehalfOf = w.behalf
			}
			_, err := t.patch(ref, substrate.PatchInput{Properties: map[string]any{"name": w.name}})
			return err
		}); err != nil {
			t.Fatalf("the %s writes: %v", w.name, err)
		}
	}
	if _, err := f.ds.Delete(ctx, substrate.ActorPerson, kind, rec.ID, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kind, ID: rec.ID, Properties: map[string]any{"name": "written again"}}); err != nil {
		t.Fatalf("the owner writes the deleted record again: %v", err)
	}
	read := func() []recordHand {
		t.Helper()
		hands, err := recordHandsOf(ctx, f.ds.db, []eref{ref})
		if err != nil {
			t.Fatal(err)
		}
		out := hands[ref]
		slices.SortFunc(out, func(a, b recordHand) int {
			return cmp.Or(cmp.Compare(a.actor, b.actor), cmp.Compare(a.onBehalfOf, b.onBehalfOf), cmp.Compare(a.op, b.op))
		})
		return out
	}
	before := read()
	if len(before) < 4 || !slices.ContainsFunc(before, func(h recordHand) bool { return h.op == string(substrate.OpDelete) }) {
		t.Fatalf("the record was written by %v, want four hands at least, a delete among them", before)
	}
	if _, err := f.ds.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil {
		t.Fatalf("compact: %v", err)
	}
	if n := f.lines(ref); n != 1 {
		t.Fatalf("the record has %d lines after the compaction, want its state's one: the test proves nothing", n)
	}
	if after := read(); !slices.Equal(before, after) {
		t.Fatalf("the hands that wrote the record are %v after the compaction, want %v", after, before)
	}
}

// The pass: past their retention and past the deletion period, after
// `repository compact`, the shipped records keep every line and the
// deletions among them stay in history, an ordinary record is compacted and
// its old deletion forgotten, and the next open leaves each shipped record
// as the person left it.
func TestThePassLeavesTheRecordsAPackageShipsWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Now().UTC()
	batched := &batchedRefs{}
	f := newShippedFixture(t, WithCompactionInterval(-1), WithDeletionsKept(time.Hour),
		WithTestCompactionRetention(func(string) (bool, time.Duration) { return false, 0 }),
		WithTestCompactionClock(func() time.Time { return clock }),
		WithTestCompactionBatches(0, 0, batched.see))
	tc := f.touch()
	gone, err := f.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: tc.ordinary.Kind, Properties: map[string]any{"name": "gone"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.ds.Delete(ctx, substrate.ActorPerson, gone.Kind, gone.ID, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	// The head is never forgotten.
	if _, err := f.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: tc.ordinary.Kind, Properties: map[string]any{"name": "after"}}); err != nil {
		t.Fatal(err)
	}
	shipped := append([]eref{tc.edited, tc.untouched}, tc.deleted...)
	before := map[eref]int{}
	for _, r := range shipped {
		before[r] = f.lines(r)
	}
	if _, err := f.ds.svc.CompactRepository(ctx, testdb.Repository(t), 0); err != nil {
		t.Fatalf("compact: %v", err)
	}
	clock = time.Now().UTC().Add(2 * time.Hour)
	report, err := f.ds.CompactionPass(ctx)
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if report.Forgotten != 1 {
		t.Errorf("the pass forgot %d deletions, want the ordinary record's alone", report.Forgotten)
	}
	for _, r := range shipped {
		if got := f.lines(r); got != before[r] {
			t.Errorf("%s %s has %d lines after the pass, want its %d", r.Kind, r.ID, got, before[r])
		}
		if batched.has(r) {
			t.Errorf("%s %s was read by a batch of the pass, want it passed by its key", r.Kind, r.ID)
		}
	}
	if n := f.lines(tc.ordinary); n != 1 {
		t.Errorf("the ordinary record has %d lines after the pass, want its state's one", n)
	}
	if n := f.lines(eref{Kind: gone.Kind, ID: gone.ID}); n != 0 {
		t.Errorf("the ordinary record deleted long ago has %d lines after the pass, want it forgotten", n)
	}
	f.shipNewer()
	f.reopen()
	f.checkNextOpen(tc)
}

// The pass over what an installed package ships: past every retention and
// the deletion period, the presets keep every line and the deletion among
// them stays in history, so the upgrade leaves each as the person left it;
// a record the package's function wrote, a mirror's, is compacted, and one
// it wrote that the person deleted long ago is forgotten.
func TestThePassLeavesWhatAnInstalledPackageShipsWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Now().UTC()
	batched := &batchedRefs{}
	f := newShippedFixture(t, WithCompactionInterval(-1), WithDeletionsKept(time.Hour),
		WithTestCompactionRetention(func(string) (bool, time.Duration) { return false, 0 }),
		WithTestCompactionClock(func() time.Time { return clock }),
		WithTestCompactionBatches(0, 0, batched.see))
	ti := f.touchInstalled()
	gone := f.mirror("gone")
	if _, err := f.ds.Delete(ctx, substrate.ActorPerson, gone.Kind, gone.ID, substrate.DeleteInput{}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.ds.RunGC(ctx); err != nil {
		t.Fatal(err)
	}
	// The head is never forgotten.
	f.mirror("after")
	shipped := []eref{ti.edited, ti.deleted, ti.untouched}
	before := map[eref]int{}
	for _, r := range shipped {
		before[r] = f.lines(r)
	}
	clock = time.Now().UTC().Add(2 * time.Hour)
	report, err := f.ds.CompactionPass(ctx)
	if err != nil {
		t.Fatalf("pass: %v", err)
	}
	if report.Forgotten != 1 {
		t.Errorf("the pass forgot %d deletions, want the mirror's record's alone", report.Forgotten)
	}
	for _, r := range shipped {
		if got := f.lines(r); got != before[r] {
			t.Errorf("preset %s has %d lines after the pass, want its %d", r.ID, got, before[r])
		}
		if batched.has(r) {
			t.Errorf("preset %s was read by a batch of the pass, want it passed by its key", r.ID)
		}
	}
	if n := f.lines(ti.mirrored); n != 1 {
		t.Errorf("the mirror's record has %d lines after the pass, want its state's one", n)
	}
	if n := f.lines(gone); n != 0 {
		t.Errorf("the mirror's record deleted long ago has %d lines after the pass, want it forgotten", n)
	}
	f.checkUpgrade(ti)
}

// batchedRefs is every record a pass's batches compacted
// (WithTestCompactionBatches).
type batchedRefs struct {
	mu   sync.Mutex
	refs map[eref]bool
}

func (b *batchedRefs) see(kind string, ids []string, _ int64, _ int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.refs == nil {
		b.refs = map[eref]bool{}
	}
	for _, id := range ids {
		b.refs[eref{Kind: kind, ID: id}] = true
	}
}

func (b *batchedRefs) has(r eref) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.refs[r]
}

// The owner's erasure of a shipped record, the one door that still takes a
// shipped record's history (decision 0310), and what the seeds' and the
// install's rule reads afterwards. A permanent delete leaves the record's
// purge line alone, under the hand that deleted it, with no value and, for
// what a package's hand created, that hand: the pass never forgets that
// deletion, and neither the next open nor the package's upgrade writes the
// record again. A forget stands where the record's newest line is the
// owner's, and is refused where it is a shipping hand's: the engine's after
// the owner, or the package's or the seed's alone.
func TestTheOwnersErasureOfARecordAPackageShipsStands(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	clock := time.Now().UTC()
	f := newShippedFixture(t, WithCompactionInterval(-1), WithDeletionsKept(time.Hour),
		WithTestCompactionClock(func() time.Time { return clock }))
	f.install("first")
	ds := f.ds
	view := func(id string) eref { return eref{Kind: shippedViewKind, ID: id} }
	preset := func(id string) eref { return eref{Kind: presetKind, ID: id} }
	units, err := shippedUnitRecords()
	if err != nil {
		t.Fatal(err)
	}
	var conversion eref
	for _, u := range units {
		if u.kind == vocabulary.KindConversion {
			conversion = u.ref()
			break
		}
	}

	purged := []eref{view("memory-retired"), conversion, {Kind: typeProvider, ID: "gemini"}, preset("deleted")}
	for _, r := range purged {
		if _, err := ds.Delete(ctx, substrate.ActorPerson, r.Kind, r.ID, substrate.DeleteInput{Purge: true}); err != nil {
			t.Fatalf("the permanent delete of %s %s: %v", r.Kind, r.ID, err)
		}
	}
	shippers := []string{actorID(substrate.ActorSeed), actorID(substrate.ActorSystem), actorID(substrate.BundleActor(shipperAuthority, "shipper"))}
	purgeLine := func(r eref) {
		t.Helper()
		rows, err := ds.db.QueryContext(ctx, `SELECT op, actor, payload::text FROM changelog WHERE kind = $1 AND record_id = $2`, r.Kind, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		var ops []string
		for rows.Next() {
			var op, actor, payload string
			if err := rows.Scan(&op, &actor, &payload); err != nil {
				t.Fatal(err)
			}
			ops = append(ops, op)
			if op != string(substrate.OpGC) || slices.Contains(shippers, actor) {
				t.Errorf("%s %s keeps a %s line by %s, want its purge alone, by the hand that deleted it", r.Kind, r.ID, op, actor)
			}
			for _, value := range []string{"Retired", "generativelanguage", `"first"`} {
				if strings.Contains(payload, value) {
					t.Errorf("%s %s's purge line keeps the value %s: %s", r.Kind, r.ID, value, payload)
				}
			}
		}
		if err := errors.Join(rows.Err(), rows.Close()); err != nil {
			t.Fatal(err)
		}
		if len(ops) != 1 {
			t.Errorf("%s %s keeps %v, want its purge line alone", r.Kind, r.ID, ops)
		}
	}
	for _, r := range purged {
		purgeLine(r)
	}

	// Forgotten: the owner's line is the newest.
	for r, change := range map[eref]map[string]any{
		view("memory-active"): {"name": "Mine"},
		preset("edited"):      {"name": "Mine", "note": "mine"},
	} {
		if _, err := ds.Patch(ctx, substrate.ActorPerson, r.Kind, r.ID, substrate.PatchInput{Properties: change}); err != nil {
			t.Fatal(err)
		}
		if _, err := ds.Forget(ctx, substrate.ActorPerson, r.Kind, r.ID, substrate.ForgetInput{}); err != nil {
			t.Fatalf("forget %s: %v", r.ID, err)
		}
		var kept int
		if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2 AND (payload::text LIKE '%"Active"%' OR payload::text LIKE '%"first"%')`, r.Kind, r.ID).Scan(&kept); err != nil {
			t.Fatal(err)
		}
		if kept != 0 {
			t.Errorf("%s keeps %d lines with its earlier value after its forget", r.ID, kept)
		}
	}

	// Refused: the engine wrote after the owner, and nobody but the shipping
	// hands wrote.
	under := view("contact-1-people")
	if _, err := ds.Patch(ctx, substrate.ActorPerson, under.Kind, under.ID, substrate.PatchInput{Properties: map[string]any{"name": "Folks"}}); err != nil {
		t.Fatal(err)
	}
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		_, err := t.patch(under, substrate.PatchInput{Labels: map[string]any{"system/backfilled": true}})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	for _, r := range []eref{under, view("contact-2-organizations"), preset("untouched")} {
		lines := f.lines(r)
		if _, err := ds.Forget(ctx, substrate.ActorPerson, r.Kind, r.ID, substrate.ForgetInput{}); !errors.Is(err, substrate.ErrConflict) {
			t.Errorf("the forget of %s, whose newest line is a shipping hand's, answered %v, want refused", r.ID, err)
		}
		if n := f.lines(r); n != lines {
			t.Errorf("the refused forget of %s left %d lines of %d", r.ID, n, lines)
		}
	}

	// The pass, past the deletion period, forgets none of the deletions.
	f.mirror("after")
	clock = time.Now().UTC().Add(2 * time.Hour)
	if report, err := ds.CompactionPass(ctx); err != nil || report.Forgotten != 0 {
		t.Fatalf("the pass forgot %d deletions (%v), want none of a shipped record", report.Forgotten, err)
	}
	for _, r := range purged {
		purgeLine(r)
	}

	f.shipNewer()
	f.reopen()
	f.install("newer")
	for _, r := range purged {
		if row := f.row(r); row != nil {
			t.Errorf("%s %s is back after the next open and the upgrade, which the owner permanently deleted", r.Kind, r.ID)
		}
	}
	for r, want := range map[eref]string{
		view("memory-active"): "Mine", preset("edited"): "Mine", under: "Folks",
		view("contact-2-organizations"): "Companies", preset("untouched"): "untouched",
	} {
		if row := f.row(r); row == nil || row.DeletedAt != nil || row.Props["name"] != want {
			t.Errorf("%s is %+v after the next open and the upgrade, want it named %s", r.ID, row, want)
		}
	}
	if row := f.row(preset("untouched")); row == nil || row.Props["note"] != "newer" {
		t.Errorf("the preset nobody touched is %+v after the upgrade, want the newer note", row)
	}
}

// A preset the package installed, merged into the owner's record that is
// then permanently deleted, goes with it, under the engine's hand acting for
// the owner, and stays gone: the next open and the package's upgrade read
// that the owner removed it, and neither writes it again.
func TestAShippedRecordMergedIntoOneErasedStaysGone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newShippedFixture(t)
	f.install("first")
	mine, err := f.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: presetKind, Properties: map[string]any{"name": "mine"}})
	if err != nil {
		t.Fatal(err)
	}
	merged := eref{Kind: presetKind, ID: "untouched"}
	if _, err := f.ds.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: presetKind, Winner: mine.ID, Loser: merged.ID}); err != nil {
		t.Fatalf("merge the installed preset into the owner's: %v", err)
	}
	deleted, err := f.ds.Delete(ctx, substrate.ActorPerson, presetKind, mine.ID, substrate.DeleteInput{Purge: true})
	if err != nil {
		t.Fatalf("the permanent delete: %v", err)
	}
	if e := deleted.Erasure; e == nil || e.Pending || !slices.Contains(e.Purged, vocabulary.RecordPath(merged.Kind, merged.ID)) {
		t.Fatalf("the delete's erasure is %+v, want it done and the merged preset purged with it", e)
	}
	f.reopen()
	f.install("newer")
	if row := f.row(merged); row != nil {
		t.Fatalf("the installed preset merged away and permanently deleted is back after the next open and the upgrade: %+v", row)
	}
}

const (
	feedKind    = shipperPkg + "/feed"
	feedMapping = shipperPkg + "/feedpreset"
)

// installFed takes the shipper package as an upgrade does, with a feed
// whose mapping copies its note into the preset it names by its name. A
// kind a mapping points at refuses a chosen id at a create (write.go
// checkCreateID), so the presets are the ones an earlier version shipped
// (install), which an upgrade updates.
func (f *shippedFixture) installFed(note string) {
	f.t.Helper()
	docs := []map[string]any{
		vocabulary.PackageManifest(shipperPkg, 0),
		vocabulary.BundleManifest(shipperPkg, map[string]any{"description": "the presets", "installs": []any{presetKind, feedKind, feedMapping}}),
		vocabulary.KindManifest(shipperPkg, map[string]any{"singular": "preset"}, map[string]any{
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"note": map[string]any{"type": "string"},
			},
		}),
		vocabulary.KindManifest(shipperPkg, map[string]any{"singular": "feed"}, map[string]any{
			"properties": map[string]any{
				"name": map[string]any{"type": "string"},
				"note": map[string]any{"type": "string"},
				"preset": map[string]any{
					"type": "reference", "kind": presetKind,
					"required": true, "mustExist": true, "subject": true,
				},
			},
		}),
		vocabulary.MappingManifest(shipperPkg, "feedpreset", map[string]any{
			"from": feedKind, "to": presetKind, "property": "preset",
			"match": []any{map[string]any{"from": "name", "to": "name"}},
			"map":   map[string]any{"note": map[string]any{"path": "note"}},
		}),
	}
	var data []substrate.PutInput
	for _, id := range []string{"edited", "deleted", "untouched"} {
		data = append(data, substrate.PutInput{Kind: presetKind, ID: id, Properties: map[string]any{"name": id, "note": note}})
	}
	if _, err := f.ds.InstallBundleClosure(context.Background(), substrate.BundleActor(shipperAuthority, "shipper"), docs, data, substrate.BundleInstall{}); err != nil {
		f.t.Fatalf("take the shipper package: %v", err)
	}
}

// A forget and a permanent delete of a mapping's source erase the copy it
// made in a preset the package installed, which the owner renamed and the
// engine wrote after: the rename stands at the next open and the upgrade.
// The package's later version declares the mapping onto its own presets.
// With no erasure the rename stands as well, which is what the erasures
// are held to.
func TestErasingAMappingsSourceKeepsTheOwnersEditOfAShippedTarget(t *testing.T) {
	t.Parallel()
	const (
		none   = "no erasure"
		forget = "a forget of the source"
		purge  = "a permanent delete of the source"
	)
	for _, name := range []string{none, forget, purge} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			f := newShippedFixture(t)
			f.install("first")
			f.installFed("first")
			target := eref{Kind: presetKind, ID: "edited"}
			if _, err := f.ds.Patch(ctx, substrate.ActorPerson, target.Kind, target.ID, substrate.PatchInput{Properties: map[string]any{"name": "Mine"}}); err != nil {
				t.Fatalf("the owner renames the preset: %v", err)
			}
			feed, err := f.ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: feedKind, Properties: map[string]any{"name": "Mine", "note": "fed"}})
			if err != nil {
				t.Fatalf("put the feed: %v", err)
			}
			if row := f.row(target); row == nil || row.Props["note"] != "fed" {
				t.Fatalf("the mapping did not copy the feed's note into the preset (%+v): the test proves nothing", row)
			}
			if err := f.ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
				_, err := t.patch(target, substrate.PatchInput{Labels: map[string]any{"system/backfilled": true}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			switch name {
			case purge:
				if _, err := f.ds.Delete(ctx, substrate.ActorPerson, feed.Kind, feed.ID, substrate.DeleteInput{Purge: true}); err != nil {
					t.Fatalf("the permanent delete of the feed: %v", err)
				}
			case forget:
				if _, err := f.ds.Forget(ctx, substrate.ActorPerson, feed.Kind, feed.ID, substrate.ForgetInput{}); err != nil {
					t.Fatalf("the forget of the feed: %v", err)
				}
			}
			var copies int
			if err := f.ds.db.QueryRowContext(ctx, `SELECT count(*) FROM compaction_erasures WHERE record_kind = $1 AND record_id = $2`, target.Kind, target.ID).Scan(&copies); err != nil || copies != 0 {
				t.Fatalf("the preset's erasure waits (%d, %v), want it done", copies, err)
			}
			f.reopen()
			f.installFed("newer")
			if row := f.row(target); row == nil || row.DeletedAt != nil || row.Props["name"] != "Mine" {
				t.Fatalf("the preset the owner renamed is %+v after the next open and the upgrade, want it named Mine", row)
			}
		})
	}
}

// Decision 0311's answer is the same after a compaction of the purge. The
// engine deletes and purges a preset the package installed, acting for the
// owner, and an erasure compacts what is left of it: one line, the purge's,
// carrying a state whose writers keep each replaced line's hand with its
// operation (a compaction never replaces a purge that is not a record's
// last effect, so the purge's line stays while the history does). The
// upgrade still reads the owner's deletion and does not write the preset.
func TestTheEnginesPurgeForAHandSurvivesACompaction(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newShippedFixture(t)
	f.install("first")
	ref := eref{Kind: presetKind, ID: "untouched"}
	if err := f.ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		t.onBehalfOf = substrate.ActorPerson
		row, err := t.softDelete(ref)
		if err != nil {
			return err
		}
		return t.purgeNow(ref, row.Finalizers)
	}); err != nil {
		t.Fatalf("the engine purges the preset for the owner: %v", err)
	}
	head, err := f.ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.ds.withCompactionTx(ctx, true, func(t *txn) error {
		t.erasing = true
		defer func() { t.erasing = false }()
		report, err := t.forgetRecords(head.Seq, []eref{ref})
		if err == nil && report.unindexed {
			err = errors.New("the history's index is not whole")
		}
		return err
	}); err != nil {
		t.Fatalf("compact the preset: %v", err)
	}
	var lines int
	var writers string
	if err := f.ds.db.QueryRowContext(ctx, `
		SELECT count(*), coalesce(string_agg(w::text, ' '), '') FROM changelog c
		LEFT JOIN LATERAL jsonb_array_elements(c.payload -> 'fold') e ON e ->> 'kind' = 'state'
		LEFT JOIN LATERAL jsonb_array_elements(coalesce(e #> '{state,writers}', '[]'::jsonb)) w ON true
		WHERE c.kind = $1 AND c.record_id = $2`, ref.Kind, ref.ID).Scan(&lines, &writers); err != nil {
		t.Fatal(err)
	}
	for _, op := range []substrate.Op{substrate.OpPut, substrate.OpDelete, substrate.OpGC} {
		if !strings.Contains(writers, `"op": "`+string(op)+`"`) {
			t.Errorf("the state's writers are %s, want the %s line's hand with its operation", writers, op)
		}
	}
	hands, err := recordHandsOf(ctx, f.ds.db, []eref{ref})
	if err != nil {
		t.Fatal(err)
	}
	purge := recordHand{actorID(substrate.ActorSystem), actorID(substrate.ActorPerson), string(substrate.OpGC)}
	if !slices.Contains(hands[ref], purge) {
		t.Fatalf("the preset's hands after the compaction are %+v, want the engine's purge for the owner among them", hands[ref])
	}
	f.reopen()
	f.install("newer")
	if row := f.row(ref); row != nil {
		t.Fatalf("the preset the engine purged for the owner is back after the compaction and the upgrade: %+v", row)
	}
}

// Decision 0311 for what an erased record owned: a package ships a shelf
// and an item the shelf owns, the owner permanently deletes the shelf, and
// the delete's cascade takes the item under the engine's hand acting for
// the owner. Neither the next open nor the upgrade writes either again.
func TestAShippedRecordTheErasedRecordOwnedStaysGone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	f := newShippedFixture(t)
	const pkg = shipperAuthority + "/shelves"
	shelf, item := eref{Kind: pkg + "/shelf", ID: "s1"}, eref{Kind: pkg + "/item", ID: "i1"}
	install := func(note string) {
		t.Helper()
		docs := []map[string]any{
			vocabulary.PackageManifest(pkg, 0),
			vocabulary.BundleManifest(pkg, map[string]any{"description": "the shelves", "installs": []any{shelf.Kind, item.Kind}}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "shelf"}, map[string]any{
				"properties": map[string]any{"name": map[string]any{"type": "string"}},
			}),
			vocabulary.KindManifest(pkg, map[string]any{"singular": "item"}, map[string]any{
				"properties": map[string]any{
					"name":  map[string]any{"type": "string"},
					"shelf": map[string]any{"type": "reference", "kind": shelf.Kind, "onDelete": "cascade"},
				},
			}),
		}
		data := []substrate.PutInput{
			{Kind: shelf.Kind, ID: shelf.ID, Properties: map[string]any{"name": note}},
			{Kind: item.Kind, ID: item.ID, Properties: map[string]any{"name": note, "shelf": map[string]any{"ref": vocabulary.RecordPath(shelf.Kind, shelf.ID)}}},
		}
		if _, err := f.ds.InstallBundleClosure(ctx, substrate.BundleActor(shipperAuthority, "shelves"), docs, data, substrate.BundleInstall{}); err != nil {
			t.Fatalf("take the shelves package: %v", err)
		}
	}
	install("first")
	if _, err := f.ds.Delete(substrate.WithOnBehalfOf(ctx, substrate.ActorPerson), substrate.ActorPerson, shelf.Kind, shelf.ID, substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("the permanent delete of the shelf: %v", err)
	}
	var cascaded int
	if err := f.ds.db.QueryRowContext(ctx, `SELECT count(*) FROM changelog WHERE kind = $1 AND record_id = $2 AND op = $3 AND actor = $4 AND on_behalf_of = $5`,
		item.Kind, item.ID, string(substrate.OpGC), actorID(substrate.ActorSystem), actorID(substrate.ActorPerson)).Scan(&cascaded); err != nil || cascaded != 1 {
		t.Fatalf("the cascade wrote %d of the engine's purges of the item for the owner (%v), want one", cascaded, err)
	}
	f.reopen()
	install("newer")
	for _, r := range []eref{shelf, item} {
		if row := f.row(r); row != nil && row.DeletedAt == nil {
			t.Errorf("%s is back after the next open and the upgrade: %+v", r.ID, row)
		}
	}
}
