package engine_test

// A rebuild refuses a RETIRED SPELLING. `link` and `unlink` were ops while
// edges existed (changelog dialect 1); a reference's meaning lives in the source
// record's own properties now, which no such entry carries, so there is nothing
// to translate. The refusal names the entry rather than replaying a history that
// would fold into records with no pointers on them (decision 0044).

import (
	"context"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
)

func TestRebuildRefusesARetiredLinkOp(t *testing.T) {
	t.Parallel()
	for _, op := range []string{"link", "unlink"} {
		t.Run(op, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			svc, dsn := newService(t)
			if _, err := svc.CreateRepository(ctx, "geoah", "geoah.example.com"); err != nil {
				t.Fatalf("create repository: %v", err)
			}
			ds, err := svc.Dataset(ctx, "geoah")
			if err != nil {
				t.Fatal(err)
			}
			importVocabulary(t, ds)
			kept := mustPut(t, ds, owner, substrate.PutInput{
				Kind: "samples.substrate.reamde.dev/people/person", Properties: map[string]any{"name": "Ada Lovelace"},
			})

			// One entry as the old binary spelled it, planted in the table AND
			// the segment file (the rebuild replays the file), because no
			// writer in this tree can produce the op any more, which is the
			// point.
			head := maxSeq(t, ds)
			if n := rewriteChangelogEntries(t, svc, dsn, ds, func(e *changelogfile.Entry) bool {
				if e.Seq != head {
					return false
				}
				e.Op = op
				return true
			}); n != 1 {
				t.Fatalf("planted %d entries, want 1", n)
			}

			before := foldOf(t, ds)
			_, err = svc.(rebuilder).RebuildRepository(ctx, "geoah")
			if err == nil {
				t.Fatalf("the rebuild replayed a %s entry", op)
			}
			if !strings.Contains(err.Error(), op) {
				t.Fatalf("rebuild = %v, want it to name the %s entry that stopped it", err, op)
			}
			// It refused without touching the fold: a store the rebuild cannot
			// finish must be left exactly as it was.
			if mustGet(t, ds, kept.Kind, kept.ID).DeletedAt != nil {
				t.Fatal("the refused rebuild disturbed a record")
			}
			if after := foldOf(t, ds); string(after) != string(before) {
				t.Fatal("the refused rebuild changed the fold")
			}
		})
	}
}
