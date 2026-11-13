package engine

// The conversion chain carries no step yet, so these tests run it through one
// no release ships: dialect 2 respells the actor `api` as `api.converted`, on
// the line and in each effect that names it. That is the shape of what a
// later step does (the plan's steps 11, 15 and 21 rewrite history lines): a
// line rewritten in place, its checksum recomputed, the fold rebuilt from it.
// A step that lands copies this test's form: a repository written at the
// dialect before, the conversion, and what the lines and the records became.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const convertedActor = "api.converted"

func testConversion() Option {
	return func(o *options) {
		o.conversion = []conversionStep{{Dialect: 2, Name: "respell_the_api_actor", Line: respellAPIActor}}
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

// convertSource is a repository written at dialect 1: a contact the API wrote.
// repo is its name, and id the key its directory is named by.
type convertSource struct {
	dsn, root, repo, id string
}

func writeConvertSource(t *testing.T) convertSource {
	t.Helper()
	ctx := context.Background()
	src := convertSource{dsn: MigratedDSN(t), root: t.TempDir(), repo: testdb.Repository(t)}
	svc, err := OpenForTest(t, ctx, src.dsn, WithDataRoot(src.root))
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
	if err := svc.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return src
}

// openConverting opens the service over src under the test's chain.
func openConverting(t *testing.T, dsn, root string, opts ...Option) (substrate.Service, error) {
	t.Helper()
	return OpenForTest(t, context.Background(), dsn, append([]Option{WithDataRoot(root), testConversion()}, opts...)...)
}

// holdConverted holds a repository to a finished conversion: the stamp and
// the manifest at 2, no conversion directory left, every line in the files
// and the table the same and none spelling the old actor, and the fold
// rebuilt from the converted lines.
func holdConverted(t *testing.T, svc substrate.Service, dsn, root, repo string) {
	t.Helper()
	ctx := context.Background()
	d, err := svc.Dataset(ctx, repo)
	if err != nil {
		t.Fatalf("open the converted repository: %v", err)
	}
	ds := d.(*dataset)
	stamp, err := readChangelogDialect(ctx, ds.db)
	if err != nil || stamp != 2 {
		t.Fatalf("stamp %d (%v), want 2", stamp, err)
	}
	m, err := changelogfile.ReadManifest(ds.dir)
	if err != nil || m.ChangelogDialect != 2 {
		t.Fatalf("manifest dialect %d (%v), want 2", m.ChangelogDialect, err)
	}
	if got, _ := changelogfile.Converting(ds.dir); len(got) != 0 {
		t.Fatalf("conversion directories left: %v", got)
	}
	if _, err := os.Stat(filepath.Join(ds.dir, "changelog.previous")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the pre-conversion changelog is left beside the converted one: %v", err)
	}
	db := rawDB(t, ds)
	hashes := map[int64][]byte{}
	rows, err := db.QueryContext(ctx, `SELECT seq, hash, actor FROM changelog`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var seq int64
		var hash []byte
		var actor string
		if err := rows.Scan(&seq, &hash, &actor); err != nil {
			t.Fatal(err)
		}
		if actor == string(substrate.ActorAPI) {
			t.Errorf("table seq %d still names the actor api", seq)
		}
		hashes[seq] = hash
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	_ = rows.Close()
	log, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		t.Fatal(err)
	}
	if int(log.Head()) != len(hashes) {
		t.Fatalf("the file holds %d lines and the table %d", log.Head(), len(hashes))
	}
	converted := 0
	if err := log.Walk(func(e changelogfile.Entry) error {
		_, sum, err := changelogfile.Encode(e)
		if err != nil {
			return err
		}
		if !bytes.Equal(sum[:], hashes[e.Seq]) {
			return fmt.Errorf("seq %d: the file's checksum is not the table's", e.Seq)
		}
		if bytes.Contains(e.Payload, []byte(`"actor":"api"`)) || e.Actor == string(substrate.ActorAPI) {
			return fmt.Errorf("seq %d still spells the actor api: %s", e.Seq, e.Payload)
		}
		if e.Actor == convertedActor {
			converted++
		}
		return nil
	}); err != nil {
		t.Fatal(err)
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
	// The converted repository takes writes in its new dialect.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, "substrate.reamde.dev/core/contact", fid("ada"), substrate.PatchInput{
		Properties: map[string]any{"name": "Ada L."},
	}); err != nil {
		t.Fatalf("write after the conversion: %v", err)
	}
}

// A repository its own database holds is converted at the boot check, in
// place, and folded again before it serves.
func TestALiveRepositoryIsConvertedAtTheBootCheck(t *testing.T) {
	t.Parallel()
	src := writeConvertSource(t)
	svc, err := openConverting(t, src.dsn, src.root)
	if err != nil {
		t.Fatalf("open under the chain: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	holdConverted(t, svc, src.dsn, src.root, src.repo)
}

// A directory an older release wrote, restored into an empty database, is
// converted between its rows going in and their fold.
func TestAnImportedDirectoryIsConvertedBeforeItFolds(t *testing.T) {
	t.Parallel()
	src := writeConvertSource(t)
	root := t.TempDir()
	if err := copyTree(filepath.Join(src.root, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir)); err != nil {
		t.Fatal(err)
	}
	dsn := MigratedDSN(t)
	svc, err := openConverting(t, dsn, root)
	if err != nil {
		t.Fatalf("import under the chain: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	holdConverted(t, svc, dsn, root, src.repo)
}

// betweenTheRenames is the one point of a conversion no fault seam reaches:
// the swap has moved the live changelog away and not yet put the converted
// one in its place. The test stops after the commit and makes the first
// rename by hand.
const betweenTheRenames = "between the swap's two renames"

// crashConversion converts src under the chain and stops it at stage,
// leaving the directory and the database as a killed process would.
func crashConversion(t *testing.T, src convertSource, stage string) {
	t.Helper()
	at := stage
	if stage == betweenTheRenames {
		at = conversionAfterCommit
	}
	crash := errors.New("the process died " + stage)
	_, err := openConverting(t, src.dsn, src.root, WithTestImportFault(0, func(s string) error {
		if s == at {
			return crash
		}
		return nil
	}))
	if !errors.Is(err, crash) {
		t.Fatalf("the boot did not stop %s: %v", stage, err)
	}
	if stage == betweenTheRenames {
		dir := filepath.Join(src.root, changelogfile.RepositoriesDir, src.id)
		if err := os.Rename(changelogfile.ChangelogDir(dir), changelogfile.PreviousDir(dir)); err != nil {
			t.Fatal(err)
		}
	}
}

var conversionCrashes = []string{
	conversionAfterStaging, conversionAfterCommit, betweenTheRenames, conversionAfterSwap, importAfterFirstFold,
}

// The process killed at each durable step of a conversion leaves a state
// the next boot finishes into the same converted repository.
func TestAConversionCutShortIsFinishedByTheNextBoot(t *testing.T) {
	t.Parallel()
	for _, stage := range conversionCrashes {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			src := writeConvertSource(t)
			crashConversion(t, src, stage)
			svc, err := openConverting(t, src.dsn, src.root)
			if err != nil {
				t.Fatalf("the next boot: %v", err)
			}
			t.Cleanup(func() { _ = svc.Close() })
			holdConverted(t, svc, src.dsn, src.root, src.repo)
		})
	}
}

// A copy of the directory a killed conversion left, restored into an empty
// database where no table took the conversion, still holds its history and
// converts on import: whichever of the two changelogs the copy holds under
// the live name, it is one history.
func TestADirectoryCopiedMidConversionImportsWhole(t *testing.T) {
	t.Parallel()
	for _, stage := range conversionCrashes {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			src := writeConvertSource(t)
			crashConversion(t, src, stage)
			root := t.TempDir()
			if err := copyTree(filepath.Join(src.root, changelogfile.RepositoriesDir), filepath.Join(root, changelogfile.RepositoriesDir)); err != nil {
				t.Fatal(err)
			}
			dsn := MigratedDSN(t)
			svc, err := openConverting(t, dsn, root)
			if err != nil {
				t.Fatalf("import the copy: %v", err)
			}
			t.Cleanup(func() { _ = svc.Close() })
			holdConverted(t, svc, dsn, root, src.repo)
		})
	}
}

// A process that does not run the boot check (a read-only one) cannot
// convert, so it refuses to open a history below its dialect rather than
// read lines it does not speak.
func TestAReadOnlyOpenRefusesAnUnconvertedHistory(t *testing.T) {
	t.Parallel()
	src := writeConvertSource(t)
	svc, err := openConverting(t, src.dsn, src.root, WithDirectoryReadOnly())
	if err != nil {
		t.Fatalf("open read-only: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	if _, err := svc.Dataset(context.Background(), src.repo); !errors.Is(err, ErrConversionPending) {
		t.Fatalf("a read-only open of a dialect-1 history under a dialect-2 binary: %v, want ErrConversionPending", err)
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
