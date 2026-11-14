package engine

// A TAKE'S CONVERSION IS TRIED BEFORE THE REPOSITORY GOES (#1368). The
// admission runs the conversion chain over every line of the copy it
// stages, keeping nothing, so a line a step refuses refuses the take while
// the repository is whole and served, when the take is asked for and at the
// boot that would carry out one recorded. The repository here was converted
// at the boot check, and its archive is still the one written at dialect 1:
// the chain the take runs is the binary's, through the local namespace
// (which reads the repository's declarations first), and a test step that
// refuses one line of it.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

// refusedLine is what the refusing step refuses a line for holding.
const refusedLine = "a shape no step foresaw"

// refusingConversion is the binary's chain and a step after it that
// refuses a line holding refusedLine while refusing is set.
func refusingConversion(refusing *atomic.Bool) Option {
	return func(o *options) {
		o.conversion = append(append([]conversionStep(nil), conversionSteps...),
			conversionStep{Dialect: testDialect, Name: "refuse_a_shape", Line: func(e *changelogfile.Entry) error {
				if refusing.Load() && bytes.Contains(e.Payload, []byte(refusedLine)) {
					return errors.New("this step does not read the line")
				}
				return nil
			}})
	}
}

func TestATakeWhoseConversionRefusesALineIsRefusedWhileTheRepositoryIsWhole(t *testing.T) {
	t.Parallel()
	for _, bad := range []bool{true, false} {
		name := "a line the conversion refuses"
		if !bad {
			name = "no line the conversion refuses"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			src := convertSource{dsn: MigratedDSN(t), root: t.TempDir(), repo: testdb.Repository(t)}
			src.archive = filepath.Join(src.root, "archive")
			svc, err := OpenForTest(t, ctx, src.dsn, WithDataRoot(src.root), WithArchive(archiveDir(t, src.archive)), WithArchiveInterval(time.Hour), sourceChain())
			if err != nil {
				t.Fatal(err)
			}
			info, err := svc.CreateRepository(ctx, src.repo)
			if err != nil {
				t.Fatal(err)
			}
			src.id = info.ID
			d, err := svc.Dataset(ctx, src.repo)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{
				Kind: "substrate.reamde.dev/core/contact", ID: fid("ada"), Properties: map[string]any{"name": "Ada"},
			}); err != nil {
				t.Fatal(err)
			}
			if bad {
				if _, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{
					Kind: "substrate.reamde.dev/core/contact", ID: fid("odd"), Properties: map[string]any{"name": refusedLine},
				}); err != nil {
					t.Fatal(err)
				}
			}
			if err := d.(*dataset).ArchivePass(ctx); err != nil {
				t.Fatalf("archive at dialect 1: %v", err)
			}
			if err := svc.Close(); err != nil {
				t.Fatal(err)
			}
			// The archive as dialect 1 left it: the server that converts the
			// table publishes its own at its close, and drops this one.
			m, name, err := changelogfile.ReadArchiveManifestDir(filepath.Join(src.archive, changelogfile.RepositoriesDir, src.id))
			if err != nil {
				t.Fatal(err)
			}
			saved := t.TempDir()
			if err := os.CopyFS(saved, os.DirFS(src.archive)); err != nil {
				t.Fatal(err)
			}
			var bad1 int64
			db := rawDBOf(t, src.dsn)
			if bad {
				if err := db.QueryRow(`SELECT seq FROM changelog WHERE repository = $1 AND record_id = $2`, src.id, fid("odd")).Scan(&bad1); err != nil {
					t.Fatal(err)
				}
			}
			records := func() int {
				t.Helper()
				var n int
				if err := db.QueryRow(`SELECT count(*) FROM records WHERE repository = $1`, src.id).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			takes := func() int {
				t.Helper()
				var n int
				if err := db.QueryRow(`SELECT count(*) FROM archive_takes WHERE repository_id = $1`, src.id).Scan(&n); err != nil {
					t.Fatal(err)
				}
				return n
			}
			served := func(t *testing.T, svc substrate.Service) {
				t.Helper()
				d, err := svc.Dataset(ctx, src.repo)
				if err != nil {
					t.Fatalf("the repository is not served: %v", err)
				}
				if _, err := d.Get(ctx, "substrate.reamde.dev/core/contact", fid("ada")); err != nil {
					t.Fatalf("the repository's contact: %v", err)
				}
			}

			// The boot check converts the live table; the step refuses
			// nothing yet, and the archive stays at dialect 1.
			var refusing atomic.Bool
			op, err := openConverting(t, src.dsn, src.root, src.archive, refusingConversion(&refusing))
			if err != nil {
				t.Fatalf("the boot that converts the table: %v", err)
			}
			served(t, op)
			before := records()
			refusing.Store(true)
			_, err = op.(*service).ResolveArchive(ctx, src.repo, false)
			if !bad {
				if err != nil {
					t.Fatalf("take the archive: %v", err)
				}
				_ = op.Close()
				after, err := openConverting(t, src.dsn, src.root, src.archive, refusingConversion(&refusing))
				if err != nil {
					t.Fatalf("the boot that carries out the take: %v", err)
				}
				t.Cleanup(func() { _ = after.Close() })
				if n := takes(); n != 0 {
					t.Fatalf("the take is still recorded (%d) after the boot", n)
				}
				served(t, after)
				return
			}
			want := fmt.Sprintf("conversion step %d (refuse_a_shape) at seq %d", testDialect, bad1)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("a take of an archive whose conversion refuses a line: %v, want it refused naming %q", err, want)
			}
			if n := takes(); n != 0 {
				t.Fatalf("the refused take recorded %d choice(s)", n)
			}
			if entries, err := os.ReadDir(filepath.Join(src.root, "archive-take", src.id)); err == nil && len(entries) != 0 {
				t.Fatalf("the refused take left %d staged copies", len(entries))
			}
			if after := records(); after != before {
				t.Fatalf("the repository holds %d records, it held %d", after, before)
			}
			served(t, op)
			_ = op.Close()

			// Recorded by a binary whose chain took the line, the archive at
			// dialect 1 put back beside what the close published: the boot
			// admits it again, and leaves the repository whole and served
			// and its choice recorded.
			if err := filepath.WalkDir(saved, func(path string, e os.DirEntry, err error) error {
				if err != nil || e.IsDir() {
					return err
				}
				rel, err := filepath.Rel(saved, path)
				if err != nil {
					return err
				}
				dst := filepath.Join(src.archive, rel)
				if _, err := os.Stat(dst); err == nil {
					return nil
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
					return err
				}
				return os.WriteFile(dst, data, 0o644)
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := db.Exec(`INSERT INTO archive_takes (repository_id, stage, archive, manifest) VALUES ($1, $2, $3, $4)`,
				src.id, "a18e7b6f2c41d9a003b2c7e91", m.Archive, name); err != nil {
				t.Fatal(err)
			}
			again, err := openConverting(t, src.dsn, src.root, src.archive, refusingConversion(&refusing))
			if err != nil {
				t.Fatalf("the boot beside a take whose conversion refuses a line: %v", err)
			}
			t.Cleanup(func() { _ = again.Close() })
			if after := records(); after != before {
				t.Fatalf("the repository holds %d records after the boot, it held %d", after, before)
			}
			if n := takes(); n != 1 {
				t.Fatalf("the take: %d recorded, want it kept", n)
			}
			served(t, again)
		})
	}
}
