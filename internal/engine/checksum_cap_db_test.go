package engine

// The changelog line cap is enforced BEFORE commit. This is an internal test
// because it drives appendChange directly: a 64 MiB payload through the fold
// would meet the tsvector limit first, and the point is the checksum step's
// own refusal, which rolls the transaction back so no row can ever sit in the
// table waiting for an append the writer will refuse.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
)

func TestSettleChecksumsRefusesALineOverTheCapBeforeCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("allocates a 64 MiB payload")
	}
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	before, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		t.Fatal(err)
	}
	fileBefore, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		t.Fatal(err)
	}

	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		return t.appendChange(substrate.ActorSystem, substrate.OpPut, "big", "geoah.example.com/p/thing",
			map[string]any{"blob": strings.Repeat("a", changelogfile.MaxLineBytes)})
	})
	if err == nil {
		t.Fatal("an entry over the line cap committed")
	}
	if !errors.Is(err, changelogfile.ErrLineTooLong) || !errors.Is(err, substrate.ErrValidation) {
		t.Fatalf("err = %v, want ErrLineTooLong as a validation error", err)
	}

	// Rolled back: neither the table nor the file moved, and the dataset is
	// not latched.
	after, err := tableChangelogHead(ctx, ds.db)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatalf("table head moved from %d to %d on a refused write", before, after)
	}
	fileAfter, err := changelogfile.OpenReadOnly(changelogfile.ChangelogDir(ds.dir))
	if err != nil {
		t.Fatal(err)
	}
	if fileAfter.Head() != fileBefore.Head() {
		t.Fatalf("file head moved from %d to %d on a refused write", fileBefore.Head(), fileAfter.Head())
	}
	if err := ds.directoryErr(); err != nil {
		t.Fatalf("the refusal latched the dataset: %v", err)
	}
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		return t.appendChange(substrate.ActorSystem, substrate.OpPut, "small", "geoah.example.com/p/thing", map[string]any{"ok": true})
	}); err != nil {
		t.Fatalf("the next write after a refused one: %v", err)
	}
}
