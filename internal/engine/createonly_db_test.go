package engine_test

// An app's approval creates the account it delivers under once (decision
// 0202): its write is a put at ifVersion 0, which creates where nothing is
// stored and refuses where anything is, a tombstone included, so an approval
// never undoes a deletion the owner made. Only a row the collection removed
// is created again.

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// accountDataset holds the mail closure's account kind alone: an
// accountconfig kind with an owner-written property, the shape an approval
// creates, without the closure's functions.
func accountDataset(t *testing.T) substrate.Dataset {
	t.Helper()
	_, ds := newCoreDataset(t)
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner,
		[]map[string]any{vocabulary.PackageManifest(mbPackage, 0), mbAccountTypeDoc()}); err != nil {
		t.Fatalf("declare the account kind: %v", err)
	}
	return ds
}

func TestAPutAtVersionZeroNeverResurrectsATombstone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := accountDataset(t)
	zero := int64(0)
	put := func(address string) (*substrate.Record, error) {
		return ds.Put(ctx, substrate.ActorConsole, substrate.PutInput{
			Kind: mbAccountType, ID: "phone-1", Properties: map[string]any{"address": address}, IfVersion: &zero,
		})
	}
	conflict := func(what string, err error, have int64) {
		t.Helper()
		wantErr(t, err, substrate.ErrConflict, what)
		var vc *substrate.VersionConflictError
		if !errors.As(err, &vc) || vc.Have != have {
			t.Fatalf("%s: %v, want a version conflict against version %d", what, err, have)
		}
	}

	created, err := put("ada@example.com")
	if err != nil {
		t.Fatalf("the create at version 0: %v", err)
	}
	if created.Version != 1 {
		t.Fatalf("the create landed at version %d", created.Version)
	}
	before := maxSeq(t, ds)
	_, err = put("eve@example.com")
	conflict("a put at version 0 over a live row", err, created.Version)
	if after := maxSeq(t, ds); after != before {
		t.Fatalf("the refused put wrote %+v", changesSince(t, ds, before))
	}

	del, err := ds.Delete(ctx, owner, mbAccountType, "phone-1", substrate.DeleteInput{})
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	before = maxSeq(t, ds)
	_, err = put("eve@example.com")
	conflict("a put at version 0 over a tombstone", err, del.Version)
	got := mustGet(t, ds, mbAccountType, "phone-1")
	if got.DeletedAt == nil || got.Version != del.Version || got.Properties["address"] != "ada@example.com" {
		t.Fatalf("the refused put touched the tombstone: deleted %v, version %d, %v", got.DeletedAt, got.Version, got.Properties)
	}
	if after := maxSeq(t, ds); after != before {
		t.Fatalf("the refused put wrote %+v", changesSince(t, ds, before))
	}

	// Collected, the id holds nothing, and the same write creates afresh.
	if _, err := ds.Delete(ctx, owner, mbAccountType, "phone-1", substrate.DeleteInput{Purge: true}); err != nil {
		t.Fatalf("purge: %v", err)
	}
	again, err := put("eve@example.com")
	if err != nil {
		t.Fatalf("the put at version 0 after the purge: %v", err)
	}
	if again.DeletedAt != nil || again.Properties["address"] != "eve@example.com" {
		t.Fatalf("after the purge the put left deleted %v, %v", again.DeletedAt, again.Properties)
	}
}

// Two approvals of one account racing each other create it once: the other
// is refused as conflict, never a second row or an overwrite.
func TestConcurrentPutsAtVersionZeroCreateOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := accountDataset(t)
	zero := int64(0)
	const n = 6
	errs := make([]error, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = ds.Put(ctx, substrate.ActorConsole, substrate.PutInput{
				Kind: mbAccountType, ID: "phone-2", IfVersion: &zero,
				Properties: map[string]any{"address": fmt.Sprintf("device%d@example.com", i)},
			})
		}()
	}
	wg.Wait()
	created := 0
	for i, err := range errs {
		switch {
		case err == nil:
			created++
		case !errors.Is(err, substrate.ErrConflict):
			t.Fatalf("put %d: %v, want created or conflict", i, err)
		}
	}
	if created != 1 {
		t.Fatalf("%d of %d racing puts at version 0 created the row, want 1", created, n)
	}
	if got := mustGet(t, ds, mbAccountType, "phone-2"); got.Version != 1 {
		t.Fatalf("the row is at version %d after one create", got.Version)
	}
}
