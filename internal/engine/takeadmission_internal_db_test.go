package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// A take is admitted by the restore's own reading of the copy staged for it,
// run to its end and writing nothing (admitSource), before anything of the
// repository is removed. A staged history piece damaged, or a spooled
// request the manifest selects gone, between the choice's recording and the
// boot, refuses the take at the boot: the repository is whole and served,
// and the choice is still recorded.
func TestATakeWhoseStagedCopyChangedRemovesNothing(t *testing.T) {
	t.Parallel()
	for _, damage := range []string{"a history piece", "a spooled request"} {
		t.Run(damage, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			name := testdb.Repository(t)
			dsn, root := MigratedDSN(t), t.TempDir()
			svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.CreateRepository(ctx, name); err != nil {
				t.Fatal(err)
			}
			d, err := svc.Dataset(ctx, name)
			if err != nil {
				t.Fatal(err)
			}
			ds := d.(*dataset)
			id := ds.info.ID
			mustPutInternal(t, ds, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", ID: fid("ada"), Properties: map[string]any{"name": "Ada"}})
			// A request the door spooled and the drain has not recorded: the
			// stop's last pass copies it into the archive beside the history.
			if err := changelogfile.WriteSpoolFile(ds.dir, changelogfile.SpoolFileName(time.Now().UnixNano(), "fire-take"), []byte("a spooled request")); err != nil {
				t.Fatal(err)
			}
			_ = svc.Close()

			op, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
			if err != nil {
				t.Fatal(err)
			}
			if res, err := op.(*service).ResolveArchive(ctx, name, false); err != nil || res.Take == "" {
				t.Fatalf("the take = %+v, %v, want recorded", res, err)
			}
			_ = op.Close()

			var stage string
			if err := rawDBOf(t, dsn).QueryRow(`SELECT stage FROM archive_takes WHERE repository_id = $1`, id).Scan(&stage); err != nil {
				t.Fatal(err)
			}
			staged := filepath.Join(root, takeRootDir, id, stage, changelogfile.RepositoriesDir, id)
			m, _, err := changelogfile.ReadArchiveManifestDir(staged)
			if err != nil {
				t.Fatal(err)
			}
			if len(m.Pieces) == 0 || len(m.Spool) == 0 {
				t.Fatalf("the staged manifest selects %d pieces and %d spooled requests, so this test holds nothing", len(m.Pieces), len(m.Spool))
			}
			switch damage {
			case "a history piece":
				path := filepath.Join(staged, m.Archive, filepath.FromSlash(m.Pieces[len(m.Pieces)-1].Name))
				body, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, body[:len(body)/2], 0o600); err != nil {
					t.Fatal(err)
				}
			case "a spooled request":
				if err := os.Remove(filepath.Join(staged, m.Archive, filepath.FromSlash(m.Spool[0]))); err != nil {
					t.Fatal(err)
				}
			}
			before := repositoryState(t, dsn, id)

			boot, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
			if err != nil {
				t.Fatalf("the boot over a take whose staged copy changed: %v", err)
			}
			t.Cleanup(func() { _ = boot.Close() })
			if after := repositoryState(t, dsn, id); after != before {
				t.Fatalf("the refused take changed the repository: %s before, %s after", before, after)
			}
			served, err := boot.Dataset(ctx, name)
			if err != nil {
				t.Fatalf("the repository the refused take left: %v", err)
			}
			if _, err := served.Get(ctx, "substrate.reamde.dev/core/contact", fid("ada")); err != nil {
				t.Fatalf("its record: %v", err)
			}
		})
	}
}
