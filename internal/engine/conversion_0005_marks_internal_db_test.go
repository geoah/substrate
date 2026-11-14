package engine

// Conversion step 5 end to end on a repository its own database holds,
// stamped below the marks, and the fold's rule that a mark goes with its
// value (marks.go): a manager row the line holds no value under becomes no
// mark; a body its kind never declared goes at the first open, or at the
// install that makes its kind usable, and its mark with it, though the line
// that drops it says no unmark; and a mark with no value under it, which no
// line takes out, stands. This binary writes no manager row, so each test
// writes its repository here under a chain below the step and adds the rows
// a release before it wrote to the table, each line under its own checksum
// again.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	marksConvPackage = "marksconv.test.dev/memos"
	marksConvMemo    = marksConvPackage + "/memo"
)

// chainBelowMarks is the chain a source is written under: every step this
// binary carries but step 5, so the source is in dialect 4.
func chainBelowMarks() Option {
	return func(o *options) {
		o.conversion = slices.DeleteFunc(slices.Clone(conversionSteps), func(s conversionStep) bool { return s.Dialect >= 5 })
	}
}

// marksSource is a repository written under the chain the test names, with
// an archive beside its database.
type marksSource struct {
	dsn, root, archive, repo, id string
}

func openMarksSource(t *testing.T, chain ...Option) (marksSource, substrate.Service, *dataset) {
	t.Helper()
	ctx := context.Background()
	src := marksSource{dsn: MigratedDSN(t), root: t.TempDir(), repo: testdb.Repository(t)}
	src.archive = filepath.Join(src.root, "archive")
	svc, err := OpenForTest(t, ctx, src.dsn, append([]Option{
		WithDataRoot(src.root), WithArchive(archiveDir(t, src.archive)), WithArchiveInterval(time.Hour),
	}, chain...)...)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	info, err := svc.CreateRepository(ctx, src.repo)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	src.id = info.ID
	d, err := svc.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, []map[string]any{
		vocabulary.PackageManifest(marksConvPackage, 0),
		vocabulary.KindManifest(marksConvPackage, map[string]any{"singular": "memo"}, map[string]any{
			"displayTemplate": "{name}",
			"properties":      map[string]any{"name": map[string]any{"type": "string"}},
		}),
	}); err != nil {
		t.Fatalf("install: %v", err)
	}
	return src, svc, ds
}

// reopen opens the source's database and archive again under this binary's
// whole chain, so the boot converts what the source left below it.
func (src marksSource) reopen(t *testing.T) *service {
	t.Helper()
	svc, err := OpenForTest(t, context.Background(), src.dsn, WithDataRoot(src.root), WithArchive(archiveDir(t, src.archive)),
		WithArchiveInterval(time.Hour))
	if err != nil {
		t.Fatalf("reopen under the whole chain: %v", err)
	}
	return svc.(*service)
}

// lineWriting is the first line of history with a record effect on kind/id.
func lineWriting(t *testing.T, history []changelogfile.Entry, kind, id string) changelogfile.Entry {
	t.Helper()
	for _, e := range history {
		var payload struct {
			Fold []struct{ Kind, Ref, ID string } `json:"fold"`
		}
		if err := json.Unmarshal(e.Payload, &payload); err != nil {
			continue
		}
		for _, f := range payload.Fold {
			if f.Kind == "record" && f.Ref == kind && f.ID == id {
				return e
			}
		}
	}
	t.Fatalf("no line writes %s %s", kind, id)
	return changelogfile.Entry{}
}

// addManagerRows appends manager effects, as a release before the marks
// wrote them, to one line in the table, and stores the line under its own
// checksum again.
func addManagerRows(t *testing.T, src marksSource, e changelogfile.Entry, rows ...map[string]any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.UseNumber()
	var payload map[string]any
	if err := dec.Decode(&payload); err != nil {
		t.Fatal(err)
	}
	effects, _ := payload[foldPayloadKey].([]any)
	for _, r := range rows {
		effects = append(effects, r)
	}
	payload[foldPayloadKey] = effects
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if e.Payload, err = changelogfile.CanonicalJSON(raw); err != nil {
		t.Fatal(err)
	}
	storeLine(t, src, e)
}

func storeLine(t *testing.T, src marksSource, e changelogfile.Entry) {
	t.Helper()
	_, sum, err := changelogfile.Encode(e)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rawDBOf(t, src.dsn).ExecContext(context.Background(),
		`UPDATE changelog SET payload = $3::jsonb, hash = $4 WHERE repository = $1 AND seq = $2`,
		src.id, e.Seq, []byte(e.Payload), sum[:]); err != nil {
		t.Fatal(err)
	}
}

// storedMark reads one property's mark from its table: whether there is one,
// and whether it pins.
func storedMark(t *testing.T, src marksSource, kind, id, property string) (held, pinned bool) {
	t.Helper()
	err := rawDBOf(t, src.dsn).QueryRowContext(context.Background(),
		`SELECT pinned FROM property_marks WHERE repository = $1 AND record_kind = $2 AND record_id = $3 AND property = $4`,
		src.id, kind, id, property).Scan(&pinned)
	if errors.Is(err, sql.ErrNoRows) {
		return false, false
	}
	if err != nil {
		t.Fatal(err)
	}
	return true, pinned
}

// A DATABASE ALREADY PAST THE ACTOR LINKS converts the same way. Step 3 took
// a package's `actors` out of each line and left the line's manager row on
// it, which every database that ran a binary at dialect 3 or 4 holds with no
// value under it: step 5 drops the row with the value, and makes a mark of
// every other.
func TestAManagerRowStep3LeftWithNoValueBecomesNoMark(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src, svc, ds := openMarksSource(t, chainBelowMarks())
	if stamp, err := readChangelogDialect(ctx, ds.db); err != nil || stamp != 4 {
		t.Fatalf("the source is stamped %d (%v), want 4", stamp, err)
	}
	line := lineWriting(t, walkedHistory(ctx, t, ds.db), kindPackage, marksConvPackage)
	person := vocabulary.ActorID(substrate.ActorPerson)
	addManagerRows(t, src, line,
		map[string]any{
			"kind": "manager", "ref": kindPackage, "id": marksConvPackage, "property": "actors",
			"actor": vocabulary.ActorID(substrate.ActorSystem), "tier": "machine",
		},
		map[string]any{
			"kind": "manager", "ref": kindPackage, "id": marksConvPackage, "property": "authority",
			"actor": person, "tier": "owner",
		})
	if err := ds.ArchivePass(ctx); err != nil {
		t.Fatalf("archive at dialect 4: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}

	svc2 := src.reopen(t)
	t.Cleanup(func() { _ = svc2.Close() })
	d2, err := svc2.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatalf("open the converted repository: %v", err)
	}
	ds2 := d2.(*dataset)
	if stamp, err := readChangelogDialect(ctx, ds2.db); err != nil || stamp != maxChangelogDialect {
		t.Fatalf("stamp %d (%v), want %d", stamp, err, maxChangelogDialect)
	}
	converted := walkedHistory(ctx, t, ds2.db)[line.Seq-1]
	if bytes.Contains(converted.Payload, []byte(`"manager"`)) || bytes.Contains(converted.Payload, []byte(`"actors"`)) {
		t.Fatalf("the line still names the manager row on actors: %s", converted.Payload)
	}
	if held, _ := storedMark(t, src, kindPackage, marksConvPackage, "actors"); held {
		t.Fatal("the package holds a mark on actors, which no line gives a value")
	}
	if held, pinned := storedMark(t, src, kindPackage, marksConvPackage, "authority"); !held || !pinned {
		t.Fatalf("the manager row on authority became mark %t, pinned %t: the step did not run", held, pinned)
	}
}

// A BODY ITS KIND NEVER DECLARED GOES WITH ITS MARK. A line from before
// dialect 4 carries a body on a kind that declares none, and a release's
// manager row on it: the conversion makes the row a pinned mark, and the
// first open's repository migration 11 drops the body on a line of the
// system's that says no unmark. The fold takes the mark with the value, live
// and in a rebuild, and a second run finds nothing to do.
func TestABodyItsKindDoesNotDeclareGoesWithItsMarkAtTheFirstOpen(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src, svc, ds := openMarksSource(t, chainBelowBody())
	orphan := fid("orphan")
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: marksConvMemo, ID: orphan, Properties: map[string]any{"name": "Left over"},
	}); err != nil {
		t.Fatal(err)
	}
	line := lineWriting(t, walkedHistory(ctx, t, ds.db), marksConvMemo, orphan)
	if !spellBodyAsDialect3(t, &line, "a word written before 0041") {
		t.Fatal("the memo's line took no body")
	}
	addManagerRows(t, src, line, map[string]any{
		"kind": "manager", "ref": marksConvMemo, "id": orphan,
		"property": "body", "actor": vocabulary.ActorID(substrate.ActorPerson), "tier": "owner",
	})
	if err := ds.ArchivePass(ctx); err != nil {
		t.Fatalf("archive at dialect 3: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	// The source ran migration 11 when it was created; the history it is
	// handed now is one the migration has not met.
	MakeMigrationPending(t, src.dsn, src.repo, 11)

	svc2 := src.reopen(t)
	// The boot converted and folded the history: the body is the memo's,
	// and the manager row a pinned mark on it.
	if held, pinned := storedMark(t, src, marksConvMemo, orphan, "body"); !held || !pinned {
		t.Fatalf("after the conversion the body's mark is held %t, pinned %t, want a pin", held, pinned)
	}
	d2, err := svc2.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatalf("open the converted repository: %v", err)
	}
	ds2 := d2.(*dataset)
	memo, err := ds2.Get(ctx, marksConvMemo, orphan)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := memo.Properties["body"]; has {
		t.Fatalf("the memo still holds a body its kind does not declare: %v", memo.Properties["body"])
	}
	if held, _ := storedMark(t, src, marksConvMemo, orphan, "body"); held {
		t.Fatal("the body's mark outlived the body")
	}
	history := walkedHistory(ctx, t, ds2.db)
	var drops []changelogfile.Entry
	for _, e := range history {
		if e.Seq > line.Seq && e.RecordID == orphan && e.Actor == vocabulary.ActorID(substrate.ActorSystem) {
			drops = append(drops, e)
		}
	}
	if len(drops) != 1 || bytes.Contains(compactJSON(t, drops[0].Payload), []byte(`"kind":"unmark"`)) {
		t.Fatalf("the system wrote %d lines on the memo, want the one that drops its body and says no unmark: %v", len(drops), drops)
	}
	if _, err := svc2.RebuildRepository(ctx, src.repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if held, _ := storedMark(t, src, marksConvMemo, orphan, "body"); held {
		t.Fatal("a rebuild gives the body's mark back")
	}

	// A second run finds nothing to do.
	if err := svc2.Close(); err != nil {
		t.Fatal(err)
	}
	MakeMigrationPending(t, src.dsn, src.repo, 11)
	svc3 := src.reopen(t)
	t.Cleanup(func() { _ = svc3.Close() })
	d3, err := svc3.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	if again := walkedHistory(ctx, t, d3.(*dataset).db); len(again) != len(history) {
		t.Fatalf("a second run wrote %d lines, want none", len(again)-len(history))
	}
}

func compactJSON(t *testing.T, raw []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := json.Compact(&b, raw); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// A BODY KEPT WHILE ITS KIND WAS UNUSABLE GOES WITH ITS MARK AT THE INSTALL.
// Its package is refused at the first open, so migration 11 keeps the body
// and the pin the conversion made of its manager row; the install that makes
// the kind usable again drops the body (dropHiddenBodies), and the fold
// takes the pin with it, live and in a rebuild.
func TestABodyKeptForAnUnusableKindGoesWithItsMarkAtTheInstall(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src, svc, ds := openMarksSource(t, chainBelowBody())
	if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, bodyArchiveDocs()); err != nil {
		t.Fatalf("install the archive: %v", err)
	}
	kept := fid("kept")
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: bodyNote, ID: kept, Properties: map[string]any{"name": "Kept while quarantined"},
	}); err != nil {
		t.Fatal(err)
	}
	line := lineWriting(t, walkedHistory(ctx, t, ds.db), bodyNote, kept)
	if !spellBodyAsDialect3(t, &line, "kept through the quarantine") {
		t.Fatal("the note's line took no body")
	}
	addManagerRows(t, src, line, map[string]any{
		"kind": "manager", "ref": bodyNote, "id": kept,
		"property": "body", "actor": vocabulary.ActorID(substrate.ActorPerson), "tier": "owner",
	})
	if err := ds.ArchivePass(ctx); err != nil {
		t.Fatalf("archive at dialect 3: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	MakeMigrationPending(t, src.dsn, src.repo, 11)

	svc2 := src.reopen(t)
	t.Cleanup(func() { _ = svc2.Close() })
	// The note's stored declaration stops loading under this binary, so the
	// first open refuses its package and migration 11 leaves its body.
	if _, err := rawDBOf(t, src.dsn).ExecContext(ctx, `UPDATE records SET props = jsonb_set(props, '{properties,name,type}', '"no-such-type"')
		WHERE repository = $1 AND kind = $2 AND id = $3`, src.id, kindKind, bodyNote); err != nil {
		t.Fatal(err)
	}
	d2, err := svc2.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatalf("open the converted repository: %v", err)
	}
	ds2 := d2.(*dataset)
	if held, pinned := storedMark(t, src, bodyNote, kept, "body"); !held || !pinned {
		t.Fatalf("the kept body's mark is held %t, pinned %t, want the pin it had", held, pinned)
	}
	head := len(walkedHistory(ctx, t, ds2.db))
	if _, err := ds2.ApplyVocabularyDocuments(ctx, substrate.ActorPerson, bodyArchiveDocs()); err != nil {
		t.Fatalf("re-install the archive: %v", err)
	}
	note, err := ds2.Get(ctx, bodyNote, kept)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := note.Properties["body"]; has {
		t.Fatalf("the install left a body its usable kind does not declare: %v", note.Properties["body"])
	}
	if held, _ := storedMark(t, src, bodyNote, kept, "body"); held {
		t.Fatal("the body's mark outlived the body the install dropped")
	}
	var drops int
	for _, e := range walkedHistory(ctx, t, ds2.db)[head:] {
		if e.RecordID == kept && e.Actor == vocabulary.ActorID(substrate.ActorSystem) {
			drops++
			if bytes.Contains(compactJSON(t, e.Payload), []byte(`"kind":"unmark"`)) {
				t.Fatalf("the drop says unmark, so it holds nothing of the fold's rule: %s", e.Payload)
			}
		}
	}
	if drops != 1 {
		t.Fatalf("the install wrote %d lines on the note, want the one that drops its body", drops)
	}
	if _, err := svc2.RebuildRepository(ctx, src.repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if held, _ := storedMark(t, src, bodyNote, kept, "body"); held {
		t.Fatal("a rebuild gives the dropped body's mark back")
	}
}

// A MARK NO LINE TAKES OUT STANDS. A mark on a property with no value under
// it (a mapping's slot that has none yet) is not the fold's to take: a line
// that takes another property out of the row takes that one's mark alone,
// and a rebuild leaves both as the live write did.
func TestAMarkOnAPropertyWithNoValueIsLeftByALineThatTakesAnotherOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src, svc, ds := openMarksSource(t)
	t.Cleanup(func() { _ = svc.Close() })
	memo := fid("memo")
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{
		Kind: marksConvMemo, ID: memo, Properties: map[string]any{"name": "Has a name"},
	}); err != nil {
		t.Fatal(err)
	}
	ref := eref{Kind: marksConvMemo, ID: memo}
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.setMark(ref, "slot", mark{SetBy: vocabulary.ActorID(substrate.ActorSystem), SetAt: t.now}); err != nil {
			return err
		}
		return t.appendChange(substrate.ActorSystem, substrate.OpPatch, memo, marksConvMemo, nil)
	}); err != nil {
		t.Fatal(err)
	}
	if held, _ := storedMark(t, src, marksConvMemo, memo, "name"); !held {
		t.Fatal("the person's write left no mark on name")
	}
	// The name goes on a line of the person's that says it; the slot is in
	// no line.
	if _, err := ds.Patch(ctx, substrate.ActorPerson, marksConvMemo, memo, substrate.PatchInput{
		Properties: map[string]any{"name": nil},
	}); err != nil {
		t.Fatal(err)
	}
	check := func(when string) {
		t.Helper()
		if held, _ := storedMark(t, src, marksConvMemo, memo, "name"); held {
			t.Fatalf("%s: the name's mark outlived the name", when)
		}
		if held, _ := storedMark(t, src, marksConvMemo, memo, "slot"); !held {
			t.Fatalf("%s: the slot's mark went, though no line took a value of it out", when)
		}
	}
	check("live")
	if _, err := svc.(*service).RebuildRepository(ctx, src.repo); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	check("after a rebuild")
}
