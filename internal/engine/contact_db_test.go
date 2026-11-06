package engine_test

import (
	"context"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// ownerContactSetting names the owner's own contact (decision record 0231).
const ownerContactSetting = vocabulary.PackageCore + "/ownerContact"

// Contacts are the owner's data inside core, so two contacts of one person
// merge (record 0231), and the setting naming the owner's contact keeps
// reaching it when it is the loser: a read by a former id returns the winner.
// Core's other kinds still refuse the generic surface.
func TestMergeTakesContacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newCoreDataset(t)
	mine := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       vocabulary.KindContact,
		Properties: map[string]any{"name": "Ada", "contactType": "person"},
	})
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: vocabulary.PackageCore + "/setting", ID: ownerContactSetting,
		Properties: map[string]any{"value": mine.ID},
	})
	google := mustPut(t, ds, owner, substrate.PutInput{
		Kind:       vocabulary.KindContact,
		Properties: map[string]any{"name": "Ada Lovelace", "emails": []any{"ada@example.com"}},
	})
	if _, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: vocabulary.KindContact, Winner: google.ID, Loser: mine.ID}); err != nil {
		t.Fatalf("merge the owner's contact into their address-book contact: %v", err)
	}
	pointer := mustGet(t, ds, vocabulary.PackageCore+"/setting", ownerContactSetting)
	id, _ := pointer.Properties["value"].(string)
	if got := mustGet(t, ds, vocabulary.KindContact, id); got.CanonicalID != google.ID {
		t.Fatalf("the owner's contact resolves to %q after the merge, want the winner %s", got.CanonicalID, google.ID)
	}

	prefs := vocabulary.PackageCore + "/consolepreference"
	mustPut(t, ds, owner, substrate.PutInput{Kind: prefs, ID: "a", Properties: map[string]any{}})
	mustPut(t, ds, owner, substrate.PutInput{Kind: prefs, ID: "b", Properties: map[string]any{}})
	_, err := ds.Merge(ctx, owner, substrate.MergeInput{Kind: prefs, Winner: "a", Loser: "b"})
	wantErr(t, err, substrate.ErrForbidden, "merging two records of a seeded core kind that is not the owner's data")
}
