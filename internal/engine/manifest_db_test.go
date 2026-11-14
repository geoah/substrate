package engine_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// reopenWith is reopen with extra options, for a seam.
func reopenWith(t *testing.T, dsn, root string, opts ...engine.Option) substrate.Service {
	t.Helper()
	all := []engine.Option{
		engine.WithKindsFS(engine.SeedKinds),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(engine.TestCredentialKey),
	}
	svc, err := engine.OpenForTest(t, context.Background(), dsn, append(all, opts...)...)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	return svc
}

// An archive whose manifest names a dialect above this binary's is refused
// at its restore before any row exists: the manifest is the one thing a
// restore checks before it writes, and a binary that cannot read the history
// must not begin to import it.
func TestARestoreRefusesAManifestAboveTheBinaryBeforeAnyRow(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		edit func(*changelogfile.Manifest)
		want error
	}{
		{"changelog dialect", func(m *changelogfile.Manifest) { m.ChangelogDialect = engine.MaxChangelogDialect() + 1 }, engine.ErrChangelogDialectNewer},
		{"vocabulary dialect", func(m *changelogfile.Manifest) { m.VocabularyDialect = engine.MaxSchemaDialect() + 1 }, engine.ErrVocabularyDialectNewer},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			svc, ds, _ := newDatasetWithDSN(t)
			mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "written by a newer binary"}})
			id := repositoryIDOf(t, ds)
			root := engine.DataRootOf(svc)
			_ = svc.Close()

			root2 := copyRepositoryDir(t, root, id)
			path := archivedManifestFile(t, filepath.Join(root2, "archive", changelogfile.RepositoriesDir, id))
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			m, err := changelogfile.DecodeArchiveManifest(raw, id)
			if err != nil {
				t.Fatal(err)
			}
			// The next epoch, naming the dialect and what a newer binary
			// wrote: no piece, which the refusal must never come to read.
			c.edit(&m)
			m.Epoch, m.Head, m.HeadSum, m.Pieces = m.Epoch+1, 0, "", nil
			repoDir := filepath.Join(root2, "archive", changelogfile.RepositoriesDir, id)
			name, err := changelogfile.WriteArchiveManifestDir(repoDir, m)
			if err != nil {
				t.Fatal(err)
			}
			path = filepath.Join(repoDir, changelogfile.ManifestsSubdir, m.Archive, name)
			edited, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			dsn2 := engine.MigratedDSN(t)
			_, err = reopen(t, dsn2, root2)
			if !errors.Is(err, c.want) {
				t.Fatalf("boot over the archive: err = %v, want %v", err, c.want)
			}
			db := rawDB(t, dsn2)
			for _, table := range []string{"repositories", "changelog_dialect", "vocabulary_dialect", "changelog", "import_progress", "records"} {
				if n := countRows(t, db, table); n != 0 {
					t.Fatalf("the refused restore left %d %s row(s)", n, table)
				}
			}
			if after, err := os.ReadFile(path); err != nil || !bytes.Equal(after, edited) {
				t.Fatalf("the refused restore touched the archive (%v)", err)
			}
		})
	}
}
