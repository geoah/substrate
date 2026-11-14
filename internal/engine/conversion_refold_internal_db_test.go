package engine

// A conversion rebuilds the fold only where a step changed a line of the
// repository, or where a step says the fold must be rebuilt (Refolds), as
// decision 0309 has it: a raise of the dialect that changes no line raises
// the stamp and leaves every fold row as it stood, while a marker an import
// set before stays set.

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// chainPlus is this binary's chain and one more step.
func chainPlus(step conversionStep) Option {
	return func(o *options) {
		step.Dialect = chainDialect(conversionSteps) + 1
		o.conversion = append(slices.Clone(conversionSteps), step)
	}
}

func rewritesNothing(*changelogfile.Entry) error { return nil }

// renamesAda rewrites the contact's name in every line that writes it, once.
func renamesAda(e *changelogfile.Entry) error {
	e.Payload = bytes.ReplaceAll(e.Payload, []byte(`"Ada"`), []byte(`"Ada L."`))
	return nil
}

// refoldWorld is a repository this binary wrote, at its dialect, with a
// contact, and its database and data root.
type refoldWorld struct {
	dsn, root, repo, id string
}

func newRefoldWorld(t *testing.T) refoldWorld {
	t.Helper()
	ctx := context.Background()
	w := refoldWorld{dsn: MigratedDSN(t), root: t.TempDir(), repo: testdb.Repository(t)}
	svc, err := OpenForTest(t, ctx, w.dsn, WithDataRoot(w.root), WithArchive(archiveDir(t, filepath.Join(w.root, "archive"))), WithArchiveInterval(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	info, err := svc.CreateRepository(ctx, w.repo)
	if err != nil {
		t.Fatal(err)
	}
	w.id = info.ID
	d, err := svc.Dataset(ctx, w.repo)
	if err != nil {
		t.Fatal(err)
	}
	mustPutInternal(t, d.(*dataset), substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", ID: fid("ada"), Properties: map[string]any{"name": "Ada"}})
	if err := d.(*dataset).ArchivePass(ctx); err != nil {
		t.Fatal(err)
	}
	if err := svc.Close(); err != nil {
		t.Fatal(err)
	}
	return w
}

func (w refoldWorld) open(t *testing.T, opts ...Option) (*service, error) {
	t.Helper()
	svc, err := OpenForTest(t, context.Background(), w.dsn, append([]Option{
		WithDataRoot(w.root), WithArchive(archiveDir(t, filepath.Join(w.root, "archive"))), WithArchiveInterval(time.Hour),
	}, opts...)...)
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc.(*service), nil
}

// foldRows is where every fold row of the repository physically stands and
// which transaction wrote it: a replay rewrites each one, so a fold left
// standing reads the same.
func (w refoldWorld) foldRows(t *testing.T) string {
	t.Helper()
	var out string
	if err := rawDBOf(t, w.dsn).QueryRow(`
		SELECT coalesce(string_agg(ctid::text || '@' || xmin::text, ',' ORDER BY kind, id), '') FROM records WHERE repository = $1`,
		w.id).Scan(&out); err != nil {
		t.Fatal(err)
	}
	var marks string
	if err := rawDBOf(t, w.dsn).QueryRow(`
		SELECT coalesce(string_agg(ctid::text || '@' || xmin::text, ',' ORDER BY record_kind, record_id, property), '') FROM property_marks WHERE repository = $1`,
		w.id).Scan(&marks); err != nil {
		t.Fatal(err)
	}
	return out + "|" + marks
}

func (w refoldWorld) state(t *testing.T) (dialect int, marked bool) {
	t.Helper()
	db := rawDBOf(t, w.dsn)
	if err := db.QueryRow(`SELECT dialect FROM changelog_dialect WHERE repository = $1`, w.id).Scan(&dialect); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM import_progress WHERE repository = $1)`, w.id).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	return dialect, marked
}

func (w refoldWorld) contactName(t *testing.T, s *service) any {
	t.Helper()
	d, err := s.Dataset(context.Background(), w.repo)
	if err != nil {
		t.Fatalf("open the repository: %v", err)
	}
	rec, err := d.Get(context.Background(), "substrate.reamde.dev/core/contact", fid("ada"))
	if err != nil {
		t.Fatalf("read the contact: %v", err)
	}
	return rec.Properties["name"]
}

func TestARaiseThatChangesNoLineLeavesTheFoldStanding(t *testing.T) {
	t.Parallel()
	w := newRefoldWorld(t)
	before := w.foldRows(t)
	s, err := w.open(t, chainPlus(conversionStep{Name: "rewrites_nothing", Line: rewritesNothing}))
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	if dialect, marked := w.state(t); dialect != chainDialect(conversionSteps)+1 || marked {
		t.Fatalf("after the raise the stamp is %d and the marker set %t, want %d and none", dialect, marked, chainDialect(conversionSteps)+1)
	}
	if after := w.foldRows(t); after != before {
		t.Fatal("a raise that changed no line rewrote the fold")
	}
	if name := w.contactName(t, s); name != "Ada" {
		t.Fatalf("the contact reads %v", name)
	}
}

func TestAStepThatRefoldsRebuildsTheFoldThoughNoLineChanged(t *testing.T) {
	t.Parallel()
	w := newRefoldWorld(t)
	before := w.foldRows(t)
	s, err := w.open(t, chainPlus(conversionStep{Name: "refolds", Line: rewritesNothing, Refolds: true}))
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	if _, marked := w.state(t); marked {
		t.Fatal("the marker outlived the rebuild")
	}
	if after := w.foldRows(t); after == before {
		t.Fatal("a step that refolds left the fold standing")
	}
	if name := w.contactName(t, s); name != "Ada" {
		t.Fatalf("the contact reads %v", name)
	}
}

func TestAStepThatChangesALineRebuildsTheFold(t *testing.T) {
	t.Parallel()
	w := newRefoldWorld(t)
	s, err := w.open(t, chainPlus(conversionStep{Name: "renames_ada", Line: renamesAda}))
	if err != nil {
		t.Fatalf("the start: %v", err)
	}
	if _, marked := w.state(t); marked {
		t.Fatal("the marker outlived the rebuild")
	}
	if name := w.contactName(t, s); name != "Ada L." {
		t.Fatalf("the contact reads %v after its line changed, want the fold rebuilt from it", name)
	}
}

// A start cut short after the conversion committed a raise that changed no
// line leaves nothing to finish: the stamp is raised, no marker is set, and
// the next start opens the repository on the fold as it stood.
func TestAStartCutShortAfterARaiseThatChangedNoLineLeavesNothingToFinish(t *testing.T) {
	t.Parallel()
	w := newRefoldWorld(t)
	before := w.foldRows(t)
	chain := chainPlus(conversionStep{Name: "rewrites_nothing", Line: rewritesNothing})
	crash := errors.New("the process died after the commit")
	if _, err := w.open(t, chain, WithTestImportFault(0, func(stage string) error {
		if stage == conversionAfterCommit {
			return crash
		}
		return nil
	})); !errors.Is(err, crash) {
		t.Fatalf("the start did not die after the conversion's commit: %v", err)
	}
	if dialect, marked := w.state(t); dialect != chainDialect(conversionSteps)+1 || marked {
		t.Fatalf("the cut start left stamp %d and marker %t, want the raise and no marker", dialect, marked)
	}
	s, err := w.open(t, chain)
	if err != nil {
		t.Fatalf("the next start: %v", err)
	}
	if after := w.foldRows(t); after != before {
		t.Fatal("the next start rebuilt a fold nothing asked it to")
	}
	if name := w.contactName(t, s); name != "Ada" {
		t.Fatalf("the contact reads %v", name)
	}
}

// A restore sets the marker before its conversion, and a conversion that
// changes no line leaves it set: the restore's fold is rebuilt as it always
// was, and the repository serves.
func TestARestoreThroughARaiseThatChangesNoLineStillFolds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	w := newRefoldWorld(t)
	dsn := MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(t.TempDir()), WithArchive(archiveDir(t, filepath.Join(w.root, "archive"))),
		WithArchiveInterval(time.Hour), chainPlus(conversionStep{Name: "rewrites_nothing", Line: rewritesNothing}))
	if err != nil {
		t.Fatalf("the restore: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	restored := refoldWorld{dsn: dsn, repo: w.repo, id: w.id}
	if dialect, marked := restored.state(t); dialect != chainDialect(conversionSteps)+1 || marked {
		t.Fatalf("after the restore the stamp is %d and the marker set %t, want the raise and the marker cleared by the fold", dialect, marked)
	}
	if name := restored.contactName(t, svc.(*service)); name != "Ada" {
		t.Fatalf("the restored contact reads %v", name)
	}
}
