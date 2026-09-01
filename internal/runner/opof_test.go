package runner

// OpOf is the one place a changelog op becomes the change class a trigger's
// `source.record.ops` declares, and docs/changelog.md#change-verbs prints the
// mapping as a table anybody writing a trigger reads. Nothing exercised it, so
// the table and the code could disagree with every test still green.

import (
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestOpOfFoldsTheChangelogOntoThreeChangeClasses(t *testing.T) {
	t.Parallel()
	created := map[string]any{"created": true}
	for _, c := range []struct {
		name    string
		change  substrate.Change
		want    string
		because string
	}{
		{
			"a put that created the row",
			substrate.Change{Op: substrate.OpPut, Payload: created},
			vocabulary.FunctionOpCreate, "the payload says the record came into existence",
		},
		{
			"a put over a live row",
			substrate.Change{Op: substrate.OpPut},
			vocabulary.FunctionOpUpdate, "no `created` marker, so the record was already there",
		},
		{
			"a put that restored a tombstone",
			substrate.Change{Op: substrate.OpPut, Payload: map[string]any{"restored": true}},
			vocabulary.FunctionOpUpdate, "a restore is not a create: the id existed before",
		},
		{"a patch", substrate.Change{Op: substrate.OpPatch}, vocabulary.FunctionOpUpdate, ""},
		{
			"a merge",
			substrate.Change{Op: substrate.OpMerge},
			vocabulary.FunctionOpUpdate,
			"the loser's tombstone rides the winner's entry, so no delete class is delivered for it",
		},
		{
			"a split",
			substrate.Change{Op: substrate.OpSplit},
			vocabulary.FunctionOpUpdate,
			"the loser comes back under one entry, so no create class is delivered for it",
		},
		{"a delete", substrate.Change{Op: substrate.OpDelete}, vocabulary.FunctionOpDelete, ""},
		{
			"a gc",
			substrate.Change{Op: substrate.OpGC},
			vocabulary.FunctionOpDelete,
			"the collector's pass reaches a trigger as the delete it is",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := OpOf(c.change); got != c.want {
				t.Fatalf("OpOf(%s) = %q, want %q %s", c.change.Op, got, c.want, c.because)
			}
		})
	}
}
