package engine

// A ROW THAT DOES NOT VERIFY AND THE ARCHIVE (#1381). When a pass writes a
// piece again and one of its rows does not verify, it takes that line from
// the piece it replaces, where that copy is the line the table stamped, and
// goes on; the seq stays noted, and `repository verify` still names the row.
// A copy that is not the stamped line is never taken: the pass stops as it
// did. The test relies on a compaction leaving alone a record whose line
// does not verify (compact.go), so the damaged row is never one a
// compaction changed: it compacts a neighbor below the damaged row and
// leaves the damaged record's line as it was stamped. The rewrite the pass
// makes is the compaction's: no line is written between the two passes, so
// the small last piece is not written again for new history.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/archive"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func TestADamagedRowNoLongerStopsTheArchive(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		// taken says the pass takes the archive's line and ends.
		taken bool
	}{
		{"the archive holds the stamped line", true},
		{"the archive's copy is damaged too", false},
		{"the stamp alone damaged", false},
		{"a row a compaction rewrote, damaged after", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := t.TempDir()
			dir, err := archive.NewDir(store)
			if err != nil {
				t.Fatal(err)
			}
			d := openInternalDataset(t, WithArchive(dir), WithArchiveInterval(time.Hour), WithCompactionInterval(-1))
			repo := testdb.Repository(t)
			put := func(name string) string {
				t.Helper()
				rec, err := d.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: ledgerTask, Properties: map[string]any{"name": name}})
				if err != nil {
					t.Fatal(err)
				}
				return rec.ID
			}
			linesOf := func(id string) []int64 {
				t.Helper()
				rows, err := d.db.QueryContext(ctx, `SELECT seq FROM changelog WHERE record_id = $1 ORDER BY seq`, id)
				if err != nil {
					t.Fatal(err)
				}
				var out []int64
				for rows.Next() {
					var seq int64
					if err := rows.Scan(&seq); err != nil {
						t.Fatal(err)
					}
					out = append(out, seq)
				}
				if err := errors.Join(rows.Err(), rows.Close()); err != nil {
					t.Fatal(err)
				}
				return out
			}
			neighbor := put("the neighbor")
			if _, err := d.Patch(ctx, substrate.ActorPerson, ledgerTask, neighbor, substrate.PatchInput{Properties: map[string]any{"name": "the neighbor, again"}}); err != nil {
				t.Fatal(err)
			}
			damaged := put("the damaged one")
			put("the head")
			if err := d.ArchivePass(ctx); err != nil {
				t.Fatalf("the first pass: %v", err)
			}
			first, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(store, changelogfile.RepositoriesDir, d.info.ID))
			if err != nil {
				t.Fatal(err)
			}
			pieceAt := func(m changelogfile.Manifest, seq int64) string {
				t.Helper()
				for _, p := range m.Pieces {
					_, f, l, _, _ := changelogfile.ParsePieceObjectName(p.Name)
					if f <= seq && seq <= l {
						return p.Name
					}
				}
				t.Fatalf("no piece of the manifest holds seq %d", seq)
				return ""
			}
			// The neighbor's lines, the first of which its compaction
			// removes, and the damaged record's.
			nlines := linesOf(neighbor)
			bad := linesOf(damaged)[0]
			if c.name == "a row a compaction rewrote, damaged after" {
				// The row the compaction puts the neighbor's state on.
				bad = nlines[len(nlines)-1]
			}
			held := pieceAt(first, bad)
			if pieceAt(first, nlines[0]) != held {
				t.Fatal("the neighbor and the damaged row are in different pieces: the test proves nothing")
			}
			compact := func() {
				t.Helper()
				report, err := d.svc.CompactRepository(ctx, repo, linesOf(damaged)[0]-1)
				if err != nil || report.Removed == 0 {
					t.Fatalf("compact the neighbor: %+v (%v)", report, err)
				}
				var rewritten bool
				if err := d.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM archive_rewritten WHERE seq = $1)`, nlines[0]).Scan(&rewritten); err != nil || !rewritten {
					t.Fatalf("the compaction marked no rewrite of seq %d (%v): the test proves nothing", nlines[0], err)
				}
			}
			if c.name == "a row a compaction rewrote, damaged after" {
				compact()
			}
			var stamped []byte
			if err := d.db.QueryRowContext(ctx, `SELECT hash FROM changelog WHERE seq = $1`, bad).Scan(&stamped); err != nil {
				t.Fatal(err)
			}

			switch c.name {
			case "the stamp alone damaged":
				// The columns as they were, the checksum beside them not.
				other := slices.Clone(stamped)
				other[0] ^= 0xff
				if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET hash = $2 WHERE seq = $1`, bad, other); err != nil {
					t.Fatal(err)
				}
			default:
				// The row damaged, its stamp left as it was.
				if _, err := d.db.ExecContext(ctx, `UPDATE changelog SET payload = jsonb_set(payload, '{damaged}', 'true') WHERE seq = $1`, bad); err != nil {
					t.Fatal(err)
				}
			}
			if c.name == "the archive's copy is damaged too" {
				path := filepath.Join(store, filepath.FromSlash(changelogfile.ArchivePrefix(d.info.ID, first.Archive)+held))
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				edited := bytes.Replace(data, []byte("the damaged one"), []byte("the damaged two"), 1)
				if bytes.Equal(edited, data) {
					t.Fatal("the archive's piece does not hold the damaged record's line: the test proves nothing")
				}
				if err := os.WriteFile(path, edited, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if c.name != "a row a compaction rewrote, damaged after" {
				// The neighbor below the damaged row compacted, so its piece
				// is written again at the next pass; the damaged record is
				// left alone.
				compact()
			}
			if head, err := tableChangelogHead(ctx, d.db); err != nil || head != first.Head {
				t.Fatalf("the head moved from %d to %d (%v) between the passes: the small last piece would be written again for it", first.Head, head, err)
			}

			err = d.ArchivePass(ctx)
			st, serr := d.svc.CompactionStatus(ctx, repo)
			if serr != nil {
				t.Fatal(serr)
			}
			if !slices.Contains(st.Damaged, bad) {
				t.Errorf("the damaged row's seq %d is not noted: %v", bad, st.Damaged)
			}
			if !c.taken {
				if !errors.Is(err, ErrChangelogDiverged) || damagedLineOf(err) != bad {
					t.Fatalf("the pass over the damaged row: %v, want it refused at seq %d", err, bad)
				}
				return
			}
			if err != nil {
				t.Fatalf("the pass over the damaged row: %v", err)
			}
			second, _, err := changelogfile.ReadArchiveManifestDir(filepath.Join(store, changelogfile.RepositoriesDir, d.info.ID))
			if err != nil {
				t.Fatal(err)
			}
			p := pieceAt(second, bad)
			if second.Epoch <= first.Epoch || p == held {
				t.Fatalf("the pass published epoch %d with piece %s, want a later one with the piece written again", second.Epoch, p)
			}
			var line []byte
			data, err := os.ReadFile(filepath.Join(store, filepath.FromSlash(changelogfile.ArchivePrefix(d.info.ID, second.Archive)+p)))
			if err != nil {
				t.Fatal(err)
			}
			_, f, l, _, _ := changelogfile.ParsePieceObjectName(p)
			if err := changelogfile.ReadPiece(bytes.NewReader(data), f, l, func(e changelogfile.Entry, b []byte, sum [32]byte) error {
				if e.Seq == bad {
					if !bytes.Equal(sum[:], stamped) {
						return fmt.Errorf("seq %d's line in the new piece is not the stamped one", bad)
					}
					line = b
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if line == nil || bytes.Contains(line, []byte("damaged\":true")) {
				t.Fatalf("the new piece holds seq %d as %q, want the line as it was stamped", bad, line)
			}

			// verify still names the row in the table.
			verified, err := d.svc.VerifyRepositoryWith(ctx, repo, VerifyOptions{Recanonicalize: true})
			if err != nil {
				t.Fatal(err)
			}
			if verified.OK || !verifyFinding(verified, fmt.Sprintf("seq %d", bad)) {
				t.Fatalf("verify of the damaged table: %+v, want a finding naming seq %d", verified.Findings, bad)
			}

			// A restore of the new manifest verifies and holds the whole line.
			root := t.TempDir()
			if err := os.CopyFS(filepath.Join(root, "archive"), os.DirFS(store)); err != nil {
				t.Fatal(err)
			}
			restored, err := restoredFrom(t, root)
			if err != nil {
				t.Fatalf("restore: %v", err)
			}
			var hash []byte
			if err := restored.db.QueryRowContext(ctx, `SELECT hash FROM changelog WHERE seq = $1`, bad).Scan(&hash); err != nil || !bytes.Equal(hash, stamped) {
				t.Fatalf("the restored row at seq %d: %x (%v), want the stamped %x", bad, hash, err, stamped)
			}
			again, err := restored.svc.VerifyRepositoryWith(ctx, repo, VerifyOptions{Recanonicalize: true})
			if err != nil || len(again.Findings) != 0 {
				t.Fatalf("verify of the restored repository: %v (%v)", again.Findings, err)
			}
			if _, err := restored.Get(ctx, ledgerTask, damaged); err != nil {
				t.Fatalf("the damaged record in the restored repository: %v", err)
			}
		})
	}
}
