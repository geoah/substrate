package engine

// These tests run the chain through one step no release ships, after the
// steps the binary carries (testDialect): it respells the actor `api` as
// `api.converted`, on the line and in each effect that names it, which every
// repository written here holds. That is the shape of what a real step does:
// a line rewritten in place, its checksum recomputed, the fold rebuilt from
// it. A step that lands copies this test's form: a repository written at the
// dialect before, the conversion, and what the lines and the records became
// (conversion_0002_drop_recovery_wrap_test.go holds step 2's lines).

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const convertedActor = "api.converted"

// testDialect is the dialect the test step converts to: the one after the
// chain the binary carries, which every repository here is written at.
var testDialect = chainDialect(conversionSteps) + 1

func testConversion() Option {
	return func(o *options) {
		o.conversion = append(append([]conversionStep(nil), conversionSteps...),
			conversionStep{Dialect: testDialect, Name: "respell_the_api_actor", Line: respellAPIActor})
	}
}

// respellAPIActor is idempotent, as every step must be: a converted line has
// no `api` left to respell.
func respellAPIActor(e *changelogfile.Entry) error {
	if e.Actor == string(substrate.ActorAPI) {
		e.Actor = convertedActor
	}
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.UseNumber()
	var p map[string]any
	if err := dec.Decode(&p); err != nil {
		return err
	}
	effects, _ := p[foldPayloadKey].([]any)
	for _, raw := range effects {
		if eff, ok := raw.(map[string]any); ok && eff["actor"] == string(substrate.ActorAPI) {
			eff["actor"] = convertedActor
		}
	}
	out, err := json.Marshal(p)
	if err != nil {
		return err
	}
	e.Payload = out
	return nil
}

// convertSource is a repository written at dialect 1, a contact the API
// wrote, with its archive taken at that dialect. repo is its name, id its
// key, and archive the directory its archive is in.
type convertSource struct {
	dsn, root, archive, repo, id string
}

// sourceChain is the chain a source is written under: none, so it is at
// dialect 1 whatever steps the binary carries. Under the binary's own chain
// a source would already be in the dialect the test's chain converts to once
// a real step lands, and every test here would convert nothing.
func sourceChain() Option {
	return func(o *options) { o.conversion = []conversionStep{} }
}

func archiveDir(t *testing.T, dir string) archive.Store {
	t.Helper()
	d, err := archive.NewDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// writeConvertSource writes a source under binary, the options of the
// binary that writes it, and sourceChain after them.
func writeConvertSource(t *testing.T, binary ...Option) convertSource {
	t.Helper()
	ctx := context.Background()
	src := convertSource{dsn: MigratedDSN(t), root: t.TempDir(), repo: testdb.Repository(t)}
	src.archive = filepath.Join(src.root, "archive")
	opts := append([]Option{WithDataRoot(src.root), WithArchive(archiveDir(t, src.archive)), WithArchiveInterval(time.Hour)}, binary...)
	svc, err := OpenForTest(t, ctx, src.dsn, append(opts, sourceChain())...)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	info, err := svc.CreateRepository(ctx, src.repo)
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	src.id = info.ID
	ds, err := svc.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/contact", ID: fid("ada"), Properties: map[string]any{"name": "Ada"},
	}); err != nil {
		t.Fatalf("put: %v", err)
	}
	if err := ds.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatalf("archive at dialect 1: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return src
}

// openConverting opens the service over a database and an archive under the
// test's chain.
func openConverting(t *testing.T, dsn, root, archived string, opts ...Option) (substrate.Service, error) {
	t.Helper()
	return OpenForTest(t, context.Background(), dsn, append([]Option{
		WithDataRoot(root), WithArchive(archiveDir(t, archived)), WithArchiveInterval(time.Hour), testConversion(),
	}, opts...)...)
}

// holdConverted holds a repository to a finished conversion: the stamp at
// testDialect, every line of the table converted and stamped with its own
// checksum, the fold rebuilt from the converted lines, and the archive, once
// a pass ran, holding the converted lines under testDialect and nothing
// under a dialect before it.
func holdConverted(t *testing.T, svc substrate.Service, repo string) {
	t.Helper()
	ctx := context.Background()
	d, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open the converted repository: %v", err)
	}
	ds := d.(*dataset)
	stamp, err := readChangelogDialect(ctx, ds.db)
	if err != nil || stamp != testDialect {
		t.Fatalf("stamp %d (%v), want %d", stamp, err, testDialect)
	}
	converted := 0
	for after := int64(0); ; {
		page, err := scanChecksumPage(ctx, ds.db, after, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			e := row.entry.fileEntry()
			_, sum, err := changelogfile.Encode(e)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(sum[:], row.hash) {
				t.Errorf("seq %d: the stamped checksum is not the line's", e.Seq)
			}
			if bytes.Contains(e.Payload, []byte(`"actor": "api"`)) || bytes.Contains(e.Payload, []byte(`"actor":"api"`)) || e.Actor == string(substrate.ActorAPI) {
				t.Errorf("seq %d still spells the actor api: %s", e.Seq, e.Payload)
			}
			if e.Actor == convertedActor {
				converted++
			}
			after = e.Seq
		}
	}
	if converted == 0 {
		t.Fatal("no line was converted: the test would hold nothing")
	}
	rec, err := ds.Get(ctx, "substrate.reamde.dev/core/contact", fid("ada"))
	if err != nil {
		t.Fatalf("read the contact: %v", err)
	}
	if got := rec.PropertyMeta["name"].Manager; got != convertedActor {
		t.Fatalf("the contact's name is managed by %q after the conversion, want %q: the fold was not rebuilt from the converted lines", got, convertedActor)
	}
	if err := ds.ArchivePass(ctx); err != nil {
		t.Fatalf("archive after the conversion: %v", err)
	}
	src, err := openNewestArchive(ctx, ds.svc.archive, ds.info.ID, nil)
	if err != nil || src.manifest.ChangelogDialect != testDialect {
		t.Fatalf("the archive's manifest after a pass is at dialect %d (%v), want %d", src.manifest.ChangelogDialect, err, testDialect)
	}
	prefix := changelogfile.ArchivePrefix(ds.info.ID, src.archive)
	listed, err := ds.svc.archive.List(ctx, prefix+changelogfile.ChangelogSubdir+"/")
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range listed {
		if !strings.HasPrefix(o.Key, fmt.Sprintf("%s%s/%d/", prefix, changelogfile.ChangelogSubdir, testDialect)) {
			t.Errorf("the archive still holds %s, which is not under dialect %d", o.Key, testDialect)
		}
		body, err := archive.ReadAll(ctx, ds.svc.archive, o.Key)
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(body, []byte(`"actor":"api"`)) {
			t.Errorf("the archive's %s still spells the actor api", o.Key)
		}
	}
	// The converted repository takes writes in its new dialect.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, "substrate.reamde.dev/core/contact", fid("ada"), substrate.PatchInput{
		Properties: map[string]any{"name": "Ada L."},
	}); err != nil {
		t.Fatalf("write after the conversion: %v", err)
	}
}

// A repository its own database holds is converted at the boot check, in
// the table, and folded again before it serves; the next archive pass
// writes the converted history beside the old and then drops the old.
func TestALiveRepositoryIsConvertedAtTheBootCheck(t *testing.T) {
	t.Parallel()
	src := writeConvertSource(t)
	svc, err := openConverting(t, src.dsn, src.root, src.archive)
	if err != nil {
		t.Fatalf("open under the chain: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	holdConverted(t, svc, src.repo)
}

// An archive an older release wrote, restored into an empty database, is
// converted between its rows going in and their fold.
func TestARestoredArchiveIsConvertedBeforeItFolds(t *testing.T) {
	t.Parallel()
	src := writeConvertSource(t)
	archived := filepath.Join(t.TempDir(), "archive")
	if err := copyTree(src.archive, archived); err != nil {
		t.Fatal(err)
	}
	dsn := MigratedDSN(t)
	svc, err := openConverting(t, dsn, t.TempDir(), archived)
	if err != nil {
		t.Fatalf("restore under the chain: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	holdConverted(t, svc, src.repo)
}

// crashConversion converts src under the chain and stops it at stage,
// leaving the database as a killed process would.
func crashConversion(t *testing.T, src convertSource, stage string) {
	t.Helper()
	crash := errors.New("the process died " + stage)
	_, err := openConverting(t, src.dsn, src.root, src.archive, WithTestImportFault(0, func(s string) error {
		if s == stage {
			return crash
		}
		return nil
	}))
	if !errors.Is(err, crash) {
		t.Fatalf("the boot did not stop %s: %v", stage, err)
	}
}

var conversionCrashes = []string{conversionBeforeCommit, conversionAfterCommit, importAfterFirstFold}

// The process killed at each durable step of a conversion leaves a state
// the next boot finishes into the same converted repository.
func TestAConversionCutShortIsFinishedByTheNextBoot(t *testing.T) {
	t.Parallel()
	for _, stage := range conversionCrashes {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			src := writeConvertSource(t)
			crashConversion(t, src, stage)
			svc, err := openConverting(t, src.dsn, src.root, src.archive)
			if err != nil {
				t.Fatalf("the next boot: %v", err)
			}
			t.Cleanup(func() { _ = svc.Close() })
			holdConverted(t, svc, src.repo)
		})
	}
}

// The archive a killed conversion left is still the history before it: no
// pass wrote over it, so restored into an empty database it converts on
// import into the same repository.
func TestAnArchiveTakenMidConversionRestoresWhole(t *testing.T) {
	t.Parallel()
	for _, stage := range conversionCrashes {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			src := writeConvertSource(t)
			crashConversion(t, src, stage)
			archived := filepath.Join(t.TempDir(), "archive")
			if err := copyTree(src.archive, archived); err != nil {
				t.Fatal(err)
			}
			dsn := MigratedDSN(t)
			svc, err := openConverting(t, dsn, t.TempDir(), archived)
			if err != nil {
				t.Fatalf("restore the archive: %v", err)
			}
			t.Cleanup(func() { _ = svc.Close() })
			holdConverted(t, svc, src.repo)
		})
	}
}

// A source is at dialect 1 under a binary that carries a step: the tests
// here hold the runner, never the binary's chain, so the steps a release
// adds change none of them (finding 2 of step 3's second review).
func TestAConversionSourceIsAtDialectOneWhateverStepsTheBinaryCarries(t *testing.T) {
	t.Parallel()
	carried := func(o *options) {
		o.conversion = []conversionStep{{Dialect: 2, Name: "a_step_the_binary_carries", Line: func(*changelogfile.Entry) error { return nil }}}
	}
	src := writeConvertSource(t, carried)
	db := rawDBOf(t, src.dsn)
	var stamp int
	if err := db.QueryRow(`SELECT dialect FROM changelog_dialect WHERE repository = $1`, src.id).Scan(&stamp); err != nil || stamp != 1 {
		t.Fatalf("the source's stamp is %d (%v), want 1", stamp, err)
	}
	svc, err := openConverting(t, src.dsn, src.root, src.archive)
	if err != nil {
		t.Fatalf("open under the chain: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	holdConverted(t, svc, src.repo)
}

// thirdActor is what the second step of twoSteps respells convertedActor as.
const thirdActor = "api.three"

// twoSteps is a chain of two steps, through the dialect given, where the
// first refuses what the second writes: step 2 respells `api` as
// `api.converted` and refuses a line spelling `api.three`, and step 3
// respells `api.converted` as `api.three`. Each step alone is idempotent, so
// a copy that ever has step 2 run over a line step 3 wrote fails to open
// rather than passing for converted.
func twoSteps(through int) Option {
	steps := []conversionStep{
		{Dialect: 2, Name: "respell_the_api_actor", Line: func(e *changelogfile.Entry) error {
			if e.Actor == thirdActor || bytes.Contains(e.Payload, []byte(`"`+thirdActor+`"`)) {
				return fmt.Errorf("seq %d is in dialect 3, which step 2 does not read", e.Seq)
			}
			return respellAPIActor(e)
		}},
		{Dialect: 3, Name: "respell_the_converted_actor", Line: respellConvertedActor},
	}
	return func(o *options) { o.conversion = steps[:through-1] }
}

// respellConvertedActor respells convertedActor as thirdActor, on the line
// and in each effect that names it.
func respellConvertedActor(e *changelogfile.Entry) error {
	if e.Actor == convertedActor {
		e.Actor = thirdActor
	}
	dec := json.NewDecoder(bytes.NewReader(e.Payload))
	dec.UseNumber()
	var p map[string]any
	if err := dec.Decode(&p); err != nil {
		return err
	}
	effects, _ := p[foldPayloadKey].([]any)
	for _, raw := range effects {
		if eff, ok := raw.(map[string]any); ok && eff["actor"] == convertedActor {
			eff["actor"] = thirdActor
		}
	}
	out, err := json.Marshal(p)
	if err != nil {
		return err
	}
	e.Payload = out
	return nil
}

// A copy of the archive taken anywhere in a conversion of two steps restores
// whole under the binary that converted it, and under a binary that carries
// only the first step it restores whole or is refused, never with the first
// step run over a line the second wrote (finding 1 of step 3's second
// review). A conversion is one transaction of the table and leaves the
// archive as it was, so a copy taken while it runs, or after it stopped at
// any of its steps, is the copy from before it; only a pass changes the
// archive, and a manifest names the dialect of every piece it selects.
func TestACopyTakenAnywhereInATwoStepConversionRestoresWholeOrIsRefused(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	copies := map[string]string{}
	take := func(name, archived string) {
		dst := filepath.Join(t.TempDir(), "archive")
		if err := copyTree(archived, dst); err != nil {
			t.Fatal(err)
		}
		copies[name] = dst
	}

	src := writeConvertSource(t)
	take("before the conversion", src.archive)
	for _, stage := range conversionCrashes {
		stopped := writeConvertSource(t)
		before := filepath.Join(t.TempDir(), "archive")
		if err := copyTree(stopped.archive, before); err != nil {
			t.Fatal(err)
		}
		crash := errors.New("the process died " + stage)
		_, err := openConverting(t, stopped.dsn, stopped.root, stopped.archive, twoSteps(3), WithTestImportFault(0, func(s string) error {
			if s == stage {
				return crash
			}
			return nil
		}))
		if !errors.Is(err, crash) {
			t.Fatalf("the boot did not stop %s: %v", stage, err)
		}
		if diff := treeDiff(before, stopped.archive); diff != "" {
			t.Fatalf("the conversion stopped %s changed the archive: %s", stage, diff)
		}
	}

	store := &droppingStore{Store: archiveDir(t, src.archive)}
	svc, err := openConverting(t, src.dsn, src.root, src.archive, twoSteps(3), WithArchive(store))
	if err != nil {
		t.Fatalf("convert under the chain to 3: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	d, err := svc.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	store.drop(func(op, key string) bool {
		k, ok := changelogfile.ParseArchiveKey(key)
		return op == "put" && ok && k.Manifests
	})
	if err := ds.ArchivePass(ctx); err == nil {
		t.Fatal("the pass published with its manifest held back")
	}
	take("a pass stopped before its manifest", src.archive)
	store.drop(func(op, _ string) bool { return op == "delete" })
	_ = ds.ArchivePass(ctx)
	take("a pass published, its deletes not applied", src.archive)
	store.drop(nil)
	for range 2 {
		if err := ds.ArchivePass(ctx); err != nil {
			t.Fatalf("the pass after the conversion: %v", err)
		}
	}
	take("the passes after the conversion done", src.archive)

	for name, archived := range copies {
		newest, err := openNewestArchive(ctx, archiveDir(t, archived), src.id, nil)
		if err != nil {
			t.Fatalf("%s: read the newest manifest: %v", name, err)
		}
		dialect := newest.manifest.ChangelogDialect
		if want := 3; name != "before the conversion" && name != "a pass stopped before its manifest" && dialect != want {
			t.Fatalf("%s: the newest manifest is at dialect %d, want %d", name, dialect, want)
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, binary := range []int{3, 2} {
				// A copy of the copy each time: a restore writes an archive
				// of its own into the store it restored from.
				store := filepath.Join(t.TempDir(), "archive")
				if err := copyTree(archived, store); err != nil {
					t.Fatal(err)
				}
				svc, err := openConverting(t, MigratedDSN(t), t.TempDir(), store, twoSteps(binary))
				if dialect > binary {
					if !errors.Is(err, ErrChangelogDialectNewer) {
						t.Fatalf("a binary of dialect %d over a manifest of dialect %d: %v, want ErrChangelogDialectNewer", binary, dialect, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("restore under a binary of dialect %d: %v", binary, err)
				}
				holdRestoredAt(t, svc, src.repo, binary)
				_ = svc.Close()
			}
		})
	}
}

// holdRestoredAt holds a repository twoSteps converted to dialect: the stamp,
// every line the API wrote spelling its actor in that dialect, and the fold
// rebuilt from them.
func holdRestoredAt(t *testing.T, svc substrate.Service, repo string, dialect int) {
	t.Helper()
	ctx := context.Background()
	actor := map[int]string{2: convertedActor, 3: thirdActor}[dialect]
	d, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open the restored repository: %v", err)
	}
	ds := d.(*dataset)
	if stamp, err := readChangelogDialect(ctx, ds.db); err != nil || stamp != dialect {
		t.Fatalf("stamp %d (%v), want %d", stamp, err, dialect)
	}
	spelled := 0
	for after := int64(0); ; {
		page, err := scanChecksumPage(ctx, ds.db, after, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(page) == 0 {
			break
		}
		for _, row := range page {
			e := row.entry.fileEntry()
			switch e.Actor {
			case actor:
				spelled++
			case string(substrate.ActorAPI), convertedActor, thirdActor:
				t.Errorf("seq %d spells the actor %q at dialect %d", e.Seq, e.Actor, dialect)
			}
			after = e.Seq
		}
	}
	if spelled == 0 {
		t.Fatal("no line spells the converted actor: the test would hold nothing")
	}
	rec, err := ds.Get(ctx, "substrate.reamde.dev/core/contact", fid("ada"))
	if err != nil {
		t.Fatalf("read the contact: %v", err)
	}
	if got := rec.PropertyMeta["name"].Manager; got != actor {
		t.Fatalf("the contact's name is managed by %q, want %q", got, actor)
	}
}

// droppingStore loses the requests its filter names: each answers an error
// and never reaches the store.
type droppingStore struct {
	archive.Store
	mu     sync.Mutex
	filter func(op, key string) bool
}

func (s *droppingStore) drop(filter func(op, key string) bool) {
	s.mu.Lock()
	s.filter = filter
	s.mu.Unlock()
}

func (s *droppingStore) dropped(op, key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.filter != nil && s.filter(op, key)
}

func (s *droppingStore) Put(ctx context.Context, key string, body io.ReadSeeker, size int64) error {
	if s.dropped("put", key) {
		return errors.New("the put was lost")
	}
	return s.Store.Put(ctx, key, body, size)
}

func (s *droppingStore) Delete(ctx context.Context, key string) error {
	if s.dropped("delete", key) {
		return errors.New("the delete was lost")
	}
	return s.Store.Delete(ctx, key)
}

// treeDiff names the first file two trees do not hold alike, or "".
func treeDiff(a, b string) string {
	read := func(root string) map[string]string {
		out := map[string]string{}
		_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, path)
			data, err := os.ReadFile(path)
			out[rel] = string(data)
			return err
		})
		return out
	}
	x, y := read(a), read(b)
	for k, v := range x {
		if w, ok := y[k]; !ok || w != v {
			return k
		}
	}
	for k := range y {
		if _, ok := x[k]; !ok {
			return k
		}
	}
	return ""
}

// A 0.x directory's import stopped right after its conversion committed
// finds the directory already set aside (rule 6): the table took every line
// in the directory's dialect, the directory was set aside, and only then was
// the table converted. The next boot finishes the removal without reading
// the directory, so no line is ever held to the directory across a
// conversion.
func TestADirectoryImportCutShortAfterItsConversionIsFinishedByTheNextBoot(t *testing.T) {
	t.Parallel()
	src := writeConvertSource(t)
	root := t.TempDir()
	dir := legacyDirFromArchive(t, src.archive, root, src.id, src.id)
	dsn := MigratedDSN(t)
	archived := filepath.Join(t.TempDir(), "archive")
	crash := errors.New("the process died after the conversion committed")
	_, err := openConverting(t, dsn, root, archived, WithTestImportFault(0, func(s string) error {
		if s == conversionAfterCommit {
			return crash
		}
		return nil
	}))
	if !errors.Is(err, crash) {
		t.Fatalf("the boot did not stop after the conversion committed: %v", err)
	}
	if changelogfile.HasHistory(dir) || !changelogfile.HistoryAdopted(dir) {
		t.Fatal("the directory was not set aside before the conversion")
	}
	svc, err := openConverting(t, dsn, root, archived)
	if err != nil {
		t.Fatalf("the next boot: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if changelogfile.HasHistory(dir) || changelogfile.HistoryAdopted(dir) {
		t.Fatal("the finished import left the directory's history beside the table")
	}
	holdConverted(t, svc, src.repo)
}

// An archive this database published in a dialect two steps before the
// table's, converted in one boot, is written again whole in the table's
// dialect by the next pass, and the pieces of the dialect before are
// deleted: the record says the archive is this database's, so no line of it
// is compared, and a conversion of several steps needs no checksum of any
// dialect between (finding 6 of the archive's fifth review).
func TestAnArchiveSeveralDialectsBehindIsWrittenAgainWhole(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeConvertSource(t)
	svc, err := OpenForTest(t, ctx, src.dsn, WithDataRoot(src.root), WithArchive(archiveDir(t, src.archive)),
		WithArchiveInterval(time.Hour), testConversionTo3())
	if err != nil {
		t.Fatalf("open under a chain to 3: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	d, err := svc.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	if err := ds.ArchivePass(ctx); err != nil {
		t.Fatalf("the pass after a conversion of two steps: %v", err)
	}
	report, err := svc.(*service).VerifyRepository(ctx, src.repo)
	if err != nil || !report.OK || report.ArchiveUnverified != "" {
		t.Fatalf("verify after the pass: %+v (%v)", report, err)
	}
	newest, err := openNewestArchive(ctx, ds.svc.archive, src.id, nil)
	if err != nil || newest.manifest.ChangelogDialect != 3 {
		t.Fatalf("the archive's newest manifest: %v, want dialect 3", err)
	}
	listed, err := ds.svc.archive.List(ctx, changelogfile.ArchivePrefix(src.id, newest.archive)+changelogfile.ChangelogSubdir+"/1/")
	if err != nil || len(listed) != 0 {
		t.Fatalf("the pieces of dialect 1 outlived the publication that dropped them: %d (%v)", len(listed), err)
	}
}

// testConversionTo3 is testConversion with a third dialect after it, a step
// that rewrites nothing: a chain longer than one step.
func testConversionTo3() Option {
	return func(o *options) {
		o.conversion = []conversionStep{
			{Dialect: 2, Name: "respell_the_api_actor", Line: respellAPIActor},
			{Dialect: 3, Name: "a_step_that_rewrites_nothing", Line: func(*changelogfile.Entry) error { return nil }},
		}
	}
}

// A database behind an archive another database went on writing, in a
// later dialect, is not taken over by it (rule 4): the boot converts the
// database's own history and takes nothing from the archive, and the pass
// stops at the newer archive and writes nothing. The operator's choice to
// take the archive replaces the repository whole at the next boot, the
// archive's lines and all.
func TestADatabaseBehindItsArchiveIsKeptUntilTheOperatorTakesTheArchive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeConvertSource(t)

	// The archive goes on without that database: restored elsewhere by a
	// binary of dialect 2, it takes two more contacts in dialect 2.
	ahead := filepath.Join(t.TempDir(), "archive")
	if err := copyTree(src.archive, ahead); err != nil {
		t.Fatal(err)
	}
	other, err := openConverting(t, MigratedDSN(t), t.TempDir(), ahead)
	if err != nil {
		t.Fatalf("restore the archive elsewhere: %v", err)
	}
	d, err := other.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"bea", "cyd"} {
		if _, err := d.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: "substrate.reamde.dev/core/contact", ID: fid(name), Properties: map[string]any{"name": name},
		}); err != nil {
			t.Fatalf("put %s: %v", name, err)
		}
	}
	if err := d.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatalf("archive in dialect 2: %v", err)
	}
	aheadHead, err := tableChangelogHead(ctx, d.(*dataset).db)
	if err != nil {
		t.Fatal(err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}

	open := func() (substrate.Service, *dataset) {
		svc, err := OpenForTest(t, ctx, src.dsn, WithDataRoot(src.root), WithArchive(archiveDir(t, ahead)),
			WithArchiveInterval(time.Hour), testConversionTo3())
		if err != nil {
			t.Fatalf("boot the database behind its archive under a chain to 3: %v", err)
		}
		d, err := svc.Dataset(ctx, src.repo)
		if err != nil {
			t.Fatal(err)
		}
		return svc, d.(*dataset)
	}
	svc, ds := open()
	if stamp, err := readChangelogDialect(ctx, ds.db); err != nil || stamp != 3 {
		t.Fatalf("stamp %d (%v), want 3", stamp, err)
	}
	if _, err := ds.Get(ctx, "substrate.reamde.dev/core/contact", fid("bea")); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the boot took the archive's contact over the database's history: %v", err)
	}
	if err := ds.ArchivePass(ctx); !errors.Is(err, ErrArchiveDiverged) {
		t.Fatalf("a pass beside the newer archive: %v, want ErrArchiveDiverged", err)
	}
	if _, err := svc.(*service).ResolveArchive(ctx, src.repo, false); err != nil {
		t.Fatalf("take the archive: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	svc, ds = open()
	t.Cleanup(func() { _ = svc.Close() })
	if head, err := tableChangelogHead(ctx, ds.db); err != nil || head < aheadHead {
		t.Fatalf("the table's head after the take is %d (%v), want the archive's %d", head, err, aheadHead)
	}
	for _, name := range []string{"bea", "cyd"} {
		if _, err := ds.Get(ctx, "substrate.reamde.dev/core/contact", fid(name)); err != nil {
			t.Fatalf("the contact %s the archive held: %v", name, err)
		}
	}
	if err := ds.ArchivePass(ctx); err != nil {
		t.Fatalf("the first pass after the take: %v", err)
	}
}

// A history that holds no line takes the dialect its first lines are in,
// down as well as up, where the stamp only ever moves up: the lines of an
// older archive restored over an empty table that a newer one had claimed
// would otherwise be claimed as converted. A history with lines keeps its
// claim.
func TestAnEmptyHistoryTakesTheDialectOfItsFirstLines(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	src := writeConvertSource(t)
	svc, err := OpenForTest(t, ctx, src.dsn, WithDataRoot(src.root), WithArchive(archiveDir(t, src.archive)), WithArchiveInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	s := svc.(*service)
	id, err := vocabulary.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.scopedDB(Repository{ID: id}.scope())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = empty.Close() })
	if _, err := empty.ExecContext(ctx, changelogDialectStamp, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := empty.ExecContext(ctx, changelogDialectClaimEmpty, 1); err != nil {
		t.Fatal(err)
	}
	if got, err := readChangelogDialect(ctx, empty); err != nil || got != 1 {
		t.Fatalf("an empty history claimed down to 1 reads %d (%v)", got, err)
	}

	d, err := svc.Dataset(ctx, src.repo)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	before, err := readChangelogDialect(ctx, ds.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.db.ExecContext(ctx, changelogDialectClaimEmpty, before+1); err != nil {
		t.Fatal(err)
	}
	if got, err := readChangelogDialect(ctx, ds.db); err != nil || got != before {
		t.Fatalf("a history with lines moved its claim from %d to %d (%v)", before, got, err)
	}
}

// rebindConversion is a chain whose one step moves the llm/provider record
// `a` to `a2`: its lines, and, in its Carry, its sealed value, sealed again
// bound to the new record. It is the shape of a step that changes what a
// sealed value is bound to, the change an archive's sealed objects must not
// take before its manifest does.
func rebindConversion() Option {
	return func(o *options) {
		o.conversion = []conversionStep{{Dialect: 2, Name: "move_provider_a_to_a2", ForRepository: func(r conversionRepository) (conversionPass, error) {
			return conversionPass{
				Line: func(e *changelogfile.Entry) error {
					if e.Kind == bindingProviderKind && e.RecordID == "a" {
						e.RecordID = "a2"
					}
					dec := json.NewDecoder(bytes.NewReader(e.Payload))
					dec.UseNumber()
					var p map[string]any
					if err := dec.Decode(&p); err != nil {
						return err
					}
					effects, _ := p[foldPayloadKey].([]any)
					for _, raw := range effects {
						if eff, ok := raw.(map[string]any); ok && eff["ref"] == bindingProviderKind && eff["id"] == "a" {
							eff["id"] = "a2"
						}
					}
					out, err := json.Marshal(p)
					if err != nil {
						return err
					}
					e.Payload = out
					return nil
				},
				Carry: func(ctx context.Context, tx *sql.Tx) error {
					rows, err := tx.QueryContext(ctx, `SELECT ref, payload FROM sealed WHERE record_kind = $1 AND record_id = 'a'`, bindingProviderKind)
					if err != nil {
						return err
					}
					moved := map[string][]byte{}
					for rows.Next() {
						var ref string
						var payload []byte
						if err := rows.Scan(&ref, &payload); err != nil {
							_ = rows.Close()
							return err
						}
						moved[ref] = payload
					}
					if err := rows.Err(); err != nil {
						_ = rows.Close()
						return err
					}
					if err := rows.Close(); err != nil {
						return err
					}
					for ref, payload := range moved {
						again, err := r.Reseal(payload, ref, bindingProviderKind, "a", bindingProviderKind, "a2")
						if err != nil {
							return err
						}
						if _, err := tx.ExecContext(ctx, `UPDATE sealed SET payload = $2, record_id = 'a2' WHERE ref = $1`, ref, again); err != nil {
							return err
						}
					}
					return nil
				},
			}, nil
		}}}
	}
}

// A pass stopped after it wrote the sealed values of a conversion that moved
// one to another record leaves the archive at its manifest before, and a
// restore of it opens that manifest's sealed value as its records at that
// head hold it: bound to the record they name, opening under that binding.
// The next pass moves the archive whole.
func TestAPassStoppedAfterTheSealedValuesOfARebindingConversionLeavesTheArchiveOpening(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dsn, root := MigratedDSN(t), t.TempDir()
	archived := filepath.Join(root, "archive")
	store := archiveDir(t, archived)
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithArchive(store), WithArchiveInterval(time.Hour), sourceChain())
	if err != nil {
		t.Fatal(err)
	}
	repo := testdb.Repository(t)
	info, err := svc.CreateRepository(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	d, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	ref := putProviderSecret(t, ds, "a", "secret-a")
	if err := ds.ArchivePass(ctx); err != nil {
		t.Fatalf("archive at dialect 1: %v", err)
	}
	var before []byte
	if err := ds.db.QueryRowContext(ctx, `SELECT payload FROM sealed WHERE ref = $1`, ref).Scan(&before); err != nil {
		t.Fatal(err)
	}
	keys, err := svc.(*service).repoKeysOn(ctx, svc.(*service).maint, info.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	opens := func(when, id string, want []byte) {
		t.Helper()
		src, err := openNewestArchive(ctx, store, info.ID, nil)
		if err != nil {
			t.Fatalf("%s: open the archive: %v", when, err)
		}
		sealed, err := src.Sealed(ctx)
		if err != nil {
			t.Fatalf("%s: the archive's sealed values: %v", when, err)
		}
		for _, rec := range sealed {
			if rec.Ref != ref {
				continue
			}
			if rec.RecordID != id || (want != nil && !bytes.Equal(rec.Payload, want)) {
				t.Fatalf("%s: the archive at head %d holds %s bound to %s, want the version bound to %s", when, src.Head(), ref, rec.RecordID, id)
			}
			if _, err := openRepoPayload(rec.Payload, keys.dek, sealedAAD(ref, bindingProviderKind, id)); err != nil {
				t.Fatalf("%s: the archive's %s does not open bound to %s: %v", when, ref, id, err)
			}
			return
		}
		t.Fatalf("%s: the archive holds no sealed value %s", when, ref)
	}

	stop := errors.New("the pass stopped after the sealed values")
	stopped, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithArchive(store), WithArchiveInterval(time.Hour), rebindConversion(),
		WithTestArchiveFault(func(stage string) error {
			if stage == archiveAfterSealed {
				return stop
			}
			return nil
		}))
	if err != nil {
		t.Fatalf("boot under the rebinding chain: %v", err)
	}
	d, err = stopped.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.(*dataset).ArchivePass(ctx); !errors.Is(err, stop) {
		t.Fatalf("the pass did not stop after the sealed values: %v", err)
	}
	var rebound string
	if err := d.(*dataset).db.QueryRowContext(ctx, `SELECT record_id FROM sealed WHERE ref = $1`, ref).Scan(&rebound); err != nil || rebound != "a2" {
		t.Fatalf("the conversion did not move the sealed value: %q (%v)", rebound, err)
	}
	if err := stopped.Close(); err != nil {
		t.Fatal(err)
	}
	opens("stopped after the sealed values", "a", before)

	next, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithArchive(store), WithArchiveInterval(time.Hour), rebindConversion())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = next.Close() })
	d, err = next.Dataset(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatalf("the next pass: %v", err)
	}
	opens("the next pass", "a2", nil)
}

// A step that asks for the repository's declarations is handed every
// version its history holds, read from the table being converted, in the
// dialect the step sees its lines in: a step below it that rewrote a
// declaration line has rewritten what it is handed.
func TestAStepIsHandedTheRepositorysDeclarationsInItsDialect(t *testing.T) {
	t.Parallel()
	src := writeConvertSource(t)
	var seen []declarationVersion
	// Each step's Line, the one step 3's Declarations were built after
	// included, is called once for every line, in seq order (finding 3 of
	// the archive's sixth review): calls holds every seq each step saw.
	calls := map[int][]int64{}
	counting := func(step int, line func(*changelogfile.Entry) error) func(*changelogfile.Entry) error {
		return func(e *changelogfile.Entry) error {
			calls[step] = append(calls[step], e.Seq)
			return line(e)
		}
	}
	mark := func(e *changelogfile.Entry) error {
		dec := json.NewDecoder(bytes.NewReader(e.Payload))
		dec.UseNumber()
		var p map[string]any
		if err := dec.Decode(&p); err != nil {
			return err
		}
		effects, _ := p[foldPayloadKey].([]any)
		for _, raw := range effects {
			eff, ok := raw.(map[string]any)
			if !ok || eff["ref"] != kindKind {
				continue
			}
			if delta, ok := eff["delta"].(map[string]any); ok {
				if set, ok := delta["set"].(map[string]any); ok {
					set["convertedBy"] = "step 2"
				}
			}
		}
		out, err := json.Marshal(p)
		if err != nil {
			return err
		}
		e.Payload = out
		return nil
	}
	chain := func(o *options) {
		o.conversion = []conversionStep{
			{Dialect: 2, Name: "mark_the_kind_declarations", ForRepository: func(conversionRepository) (conversionPass, error) {
				return conversionPass{Line: counting(2, mark)}, nil
			}},
			{Dialect: 3, Name: "read_the_declarations", ForRepository: func(r conversionRepository) (conversionPass, error) {
				var err error
				seen, err = r.Declarations()
				return conversionPass{Line: counting(3, func(*changelogfile.Entry) error { return nil })}, err
			}},
		}
	}
	svc, err := OpenForTest(t, context.Background(), src.dsn, WithDataRoot(src.root), WithArchive(archiveDir(t, src.archive)), WithArchiveInterval(time.Hour), chain)
	if err != nil {
		t.Fatalf("convert under the chain: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	kinds, contact := 0, false
	for i, v := range seen {
		if i > 0 && v.Seq < seen[i-1].Seq {
			t.Fatalf("the declarations are not in seq order: %d after %d", v.Seq, seen[i-1].Seq)
		}
		if v.Ref != kindKind || v.Deleted {
			continue
		}
		kinds++
		if v.Properties["convertedBy"] != "step 2" {
			t.Fatalf("the kind %s at seq %d is not as step 2 left it: %v", v.ID, v.Seq, v.Properties["convertedBy"])
		}
		if v.ID == "substrate.reamde.dev/core/contact" {
			contact = true
		}
	}
	if kinds == 0 || !contact {
		t.Fatalf("handed %d declaration versions, %d of kinds, the contact kind among them = %v", len(seen), kinds, contact)
	}
	d, err := svc.Dataset(context.Background(), src.repo)
	if err != nil {
		t.Fatal(err)
	}
	head, err := tableChangelogHead(context.Background(), d.(*dataset).db)
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []int{2, 3} {
		got := calls[step]
		if int64(len(got)) != head {
			t.Fatalf("step %d's Line was called %d times over a history of %d lines", step, len(got), head)
		}
		for i, seq := range got {
			if seq != int64(i)+1 {
				t.Fatalf("step %d's Line saw seq %d as its call %d: every line once, in seq order", step, seq, i+1)
			}
		}
	}
}

// A process that does not run the boot check (a read-only one) cannot
// convert, so it refuses to open a history below its dialect rather than
// read lines it does not speak.
func TestAReadOnlyOpenRefusesAnUnconvertedHistory(t *testing.T) {
	t.Parallel()
	src := writeConvertSource(t)
	svc, err := openConverting(t, src.dsn, src.root, src.archive, WithDirectoryReadOnly())
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.Dataset(context.Background(), src.repo); !errors.Is(err, ErrConversionPending) {
		t.Fatalf("a read-only open of a history below the binary's dialect: %v, want ErrConversionPending", err)
	}
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
}
