package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// THE FILL WRITES A GONE HAND'S RECORD DISABLED, AND NOTHING FOR AN ID NO HAND
// DERIVES. A history from before actors were records holds the line that
// declared a function the collector has since purged: a hand that is gone,
// which no write of this binary gave a record. A mark links its actor, and
// links an id no hand the repository held derives. The open's fill writes
// the function's actor record disabled, as migration 9 writes a gone hand's,
// writes nothing for the other id, and a second pass writes nothing.
func TestTheActorFillWritesAGoneHandDisabledAndNothingForAnIDNoHandDerives(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	authority := ds.Repository().Authority
	contact := mustPutInternal(t, ds, substrate.PutInput{Kind: "substrate.reamde.dev/core/contact", ID: fid("ada"), Properties: map[string]any{"name": "Ada"}})
	gone := vocabulary.ActorID(substrate.FunctionActor(authority, "gone", "fn"))
	nobody := vocabulary.DerivedMintedID("actorfill", "nobody")
	self := eref{Kind: kindRepository, ID: ds.info.ID}
	// A history from before the fill: marks linking two actors the
	// repository holds no record of, and no mark of the fill on its record.
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.appendChange(substrate.ActorSystem, substrate.OpPut, authority+"/gone/fn", kindFunction, nil); err != nil {
			return err
		}
		ref := eref{Kind: contact.Kind, ID: contact.ID}
		if err := t.setMark(ref, "name", mark{SetBy: gone, SetAt: t.now}); err != nil {
			return err
		}
		if err := t.setMark(ref, "nickname", mark{SetBy: nobody, SetAt: t.now}); err != nil {
			return err
		}
		if _, err := t.putAnnotation(self, annActorRecordsFilled, nil); err != nil {
			return err
		}
		return t.appendChange(substrate.ActorSystem, substrate.OpPatch, contact.ID, contact.Kind, nil)
	}); err != nil {
		t.Fatal(err)
	}
	held := func(id string) map[string]any {
		t.Helper()
		var raw []byte
		err := rawDB(t, ds).QueryRowContext(ctx, `SELECT props FROM records WHERE repository = $1 AND kind = $2 AND id = $3`,
			ds.info.ID, kindActor, id).Scan(&raw)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			t.Fatal(err)
		}
		var props map[string]any
		if err := json.Unmarshal(raw, &props); err != nil {
			t.Fatal(err)
		}
		return props
	}
	if held(gone) != nil || held(nobody) != nil {
		t.Fatal("the fixture's actors have records before the fill")
	}

	if err := ds.fillActorRecords(ctx); err != nil {
		t.Fatalf("the fill: %v", err)
	}
	props := held(gone)
	if props == nil || props["type"] != substrate.ActorTypeFunction || props["disabled"] != true {
		t.Fatalf("the gone function's actor record = %v, want a function's, disabled", props)
	}
	if props := held(nobody); props != nil {
		t.Fatalf("the fill wrote a record for an id no hand derives: %v", props)
	}
	head := historyHead(t, ds)
	if err := ds.fillActorRecords(ctx); err != nil {
		t.Fatalf("the second pass: %v", err)
	}
	if again := historyHead(t, ds); again != head {
		t.Fatalf("the second pass wrote %d lines, want none", again-head)
	}
}

// historyHead is the repository's last seq.
func historyHead(t *testing.T, ds *dataset) int64 {
	t.Helper()
	var head int64
	if err := rawDB(t, ds).QueryRowContext(context.Background(), `SELECT coalesce(max(seq), 0) FROM changelog WHERE repository = $1`, ds.info.ID).Scan(&head); err != nil {
		t.Fatal(err)
	}
	return head
}
