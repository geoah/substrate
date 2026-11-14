package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The conversion's carry moves the rows outside history that bind an own kind
// or a machinery record named under the repository's name, to the address its
// converted line names (decision record 0259): a sealed value is sealed again
// under its record's `~` kind, or its `~` id, and opens there, with the
// plaintext it held; a vector's queue row takes the same address; a row of
// another authority, and a data record whose own id merely starts with the
// name, stay where they are; and a second pass changes nothing.
func TestCarryLocalResealsAndRekeysTheRowsOfOwnKinds(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	name := ds.name()
	own, other := name+"/creds/account", "other.example.com/creds/account"
	secret := vocabulary.CoreKind("secret")
	reseal := func(payload []byte, ref, fromKind, fromID, toKind, toID string) ([]byte, error) {
		plain, err := ds.openPayload(payload, sealedAAD(ref, fromKind, fromID))
		if err != nil {
			return nil, err
		}
		return ds.sealPayload(plain, sealedAAD(ref, toKind, toID))
	}
	type address struct{ kind, id string }
	rows := []struct {
		ref    string
		at     address
		want   address
		secret string
	}{
		{"secret-own", address{own, "a1"}, address{"~/creds/account", "a1"}, "own secret"},
		{"secret-other", address{other, "a1"}, address{other, "a1"}, "other secret"},
		{"secret-setting", address{secret, name + "/tasks/apiKey"}, address{secret, "~/tasks/apiKey"}, "a bundle's key"},
		{"secret-data", address{other, name + "/x"}, address{other, name + "/x"}, "a data id"},
	}
	for _, row := range rows {
		payload, err := ds.sealPayload([]byte(row.secret), sealedAAD(row.ref, row.at.kind, row.at.id))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ds.db.ExecContext(ctx, `INSERT INTO sealed (ref, record_kind, record_id, payload) VALUES ($1, $2, $3, $4)`,
			row.ref, row.at.kind, row.at.id, payload); err != nil {
			t.Fatalf("plant sealed %s: %v", row.ref, err)
		}
		if _, err := ds.db.ExecContext(ctx, `INSERT INTO embed_queue (record_kind, record_id, property) VALUES ($1, $2, 'notes')`, row.at.kind, row.at.id); err != nil {
			t.Fatalf("plant the queue row of %v: %v", row.at, err)
		}
	}

	for pass := 1; pass <= 2; pass++ {
		tx, err := ds.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := carryLocal(ctx, tx, name, referencePositions{}, actorMoves{}, reseal, false); err != nil {
			_ = tx.Rollback()
			t.Fatalf("pass %d: %v", pass, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}

		queued := map[address]bool{}
		q, err := ds.db.QueryContext(ctx, `SELECT record_kind, record_id FROM embed_queue`)
		if err != nil {
			t.Fatal(err)
		}
		for q.Next() {
			var a address
			if err := q.Scan(&a.kind, &a.id); err != nil {
				t.Fatal(err)
			}
			queued[a] = true
		}
		if err := q.Err(); err != nil {
			t.Fatal(err)
		}
		_ = q.Close()

		for _, row := range rows {
			var at address
			var payload []byte
			if err := ds.db.QueryRowContext(ctx, `SELECT record_kind, record_id, payload FROM sealed WHERE ref = $1`, row.ref).Scan(&at.kind, &at.id, &payload); err != nil {
				t.Fatal(err)
			}
			plain, err := ds.openPayload(payload, sealedAAD(row.ref, at.kind, at.id))
			if at != row.want || err != nil || string(plain) != row.secret {
				t.Errorf("pass %d: sealed %s is at %v and opens to %q (%v), want at %v, opening to %q", pass, row.ref, at, plain, err, row.want, row.secret)
			}
			if !queued[row.want] {
				t.Errorf("pass %d: no queue row at %v; the queue holds %v", pass, row.want, queued)
			}
		}
		if len(queued) != len(rows) {
			t.Errorf("pass %d: the queue holds %v, want one row per planted one", pass, queued)
		}
	}
}

// A sealed value of an own kind that does not open under the data key is
// damage, and the carry refuses it, unless a restore left the repository's
// secrets behind: then it is one of them, and it moves to its record's `~`
// binding as it is, where the open that clears it looks (lostsecrets.go).
func TestCarryLocalMovesALeftBehindSecretAsItIs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	name := ds.name()
	own := name + "/creds/account"
	lost := []byte("sealed under a key this repository no longer has")
	if _, err := ds.db.ExecContext(ctx, `INSERT INTO sealed (ref, record_kind, record_id, payload) VALUES ('secret-lost', $1, 'a1', $2)`, own, lost); err != nil {
		t.Fatal(err)
	}
	carry := func(leftBehind bool) error {
		tx, err := ds.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := carryLocal(ctx, tx, name, referencePositions{}, actorMoves{}, ds.reseal, leftBehind); err != nil {
			_ = tx.Rollback()
			return err
		}
		return tx.Commit()
	}
	if err := carry(false); !errors.Is(err, errSealedUnopened) {
		t.Fatalf("a value that does not open, nothing left behind: %v, want refused", err)
	}
	if err := carry(true); err != nil {
		t.Fatalf("a value a restore left behind: %v", err)
	}
	var kind, id string
	var payload []byte
	if err := ds.db.QueryRowContext(ctx, `SELECT record_kind, record_id, payload FROM sealed WHERE ref = 'secret-lost'`).Scan(&kind, &id, &payload); err != nil {
		t.Fatal(err)
	}
	if kind != "~/creds/account" || id != "a1" || string(payload) != string(lost) {
		t.Errorf("the left-behind value is at %s %s holding %q, want at ~/creds/account a1 as it was", kind, id, payload)
	}
}

// The write folds the repository's name only where a record names something
// (decision record 0259): a setting's and a secret's ids, which sit under a
// package's, move to `~`; the secret's value reaches the seal as the owner
// wrote it, and so does the setting's, whatever they spell.
func TestTheWriteFoldsANameAndLeavesAValue(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	name := ds.name()
	value := name + "/password-that-is-long-enough"
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kindSecret, ID: name + "/notes/apiKey", Properties: map[string]any{"value": value}}); err != nil {
		t.Fatalf("write the secret: %v", err)
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: kindSecret, ID: "~/notes/apiKey"})
	if err != nil || row == nil {
		t.Fatalf("the secret is not at ~/notes/apiKey: %v, %v", row, err)
	}
	stored, _ := row.Props["value"].(string)
	plain, err := ds.openSecretValue(ctx, stored)
	if err != nil || plain != value {
		t.Errorf("the secret opens to %q (%v), want %q as written", plain, err, value)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: kindSetting, ID: name + "/notes/feed", Properties: map[string]any{"type": "string", "value": name + "/feed"}}); err != nil {
		t.Fatalf("write the setting: %v", err)
	}
	setting, err := ds.Get(ctx, kindSetting, "~/notes/feed")
	if err != nil || setting.Properties["value"] != name+"/feed" {
		t.Errorf("the setting reads %v (%v), want its value %s as written", setting, err, name+"/feed")
	}
}

// A function names a callable in any spelling the registry accepts, and the
// call is held to, and dispatched by, the identity its grants are written
// in: the repository's full name is `~` (decision record 0259).
func TestACallableNamedByTheFullNameResolvesToTheLocalOne(t *testing.T) {
	t.Parallel()
	ds := newRaceDataset(t)
	name := ds.name()
	b := &callBackend{inv: &invocation{ds: ds}}
	for in, want := range map[string]string{
		name + "/crew/helper":      "~/crew/helper",
		"~/crew/helper":            "~/crew/helper",
		"other.example.com/crew/x": "other.example.com/crew/x",
		name + ".evil/crew/helper": name + ".evil/crew/helper",
	} {
		if got := b.ResolveCallable(in); got != want {
			t.Errorf("ResolveCallable(%q) = %q, want %q", in, got, want)
		}
	}
}

// The carry moves a stored Idempotency-Key answer to the record as it reads
// after the conversion: a record outcome's kind, the reference its kind's
// declaration puts in it and nothing else of its properties, the hand a mark
// links, an answer about an actor record by its hand (its id and its name),
// and the locator of an outcome too large to store, an actor's included. The key keeps its fingerprint and its
// settlement, so a repeat is still answered and never runs again; a
// callable's own output stays as it was stored; a second pass changes
// nothing.
func TestCarryLocalMovesTheStoredIdempotencyAnswers(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	name := ds.name()
	refs := referencePositionsOf([]declarationVersion{{
		Seq: 1, Ref: kindKind, ID: name + "/tasks/task",
		Properties: map[string]any{"properties": map[string]any{
			"name":  map[string]any{"type": "string"},
			"about": map[string]any{"type": "reference", "kind": name + "/tasks/task"},
		}},
	}, {
		Seq: 1, Ref: kindKind, ID: kindActor,
		Properties: map[string]any{"properties": map[string]any{
			"function": map[string]any{"type": "reference", "kind": kindFunction},
		}},
	}}, name)
	stamp := name + "/lab/stamp"
	moves := actorMovesOf(name, []declarationVersion{{
		Seq: 2, Ref: kindFunction, ID: stamp, Properties: map[string]any{"authority": name, "package": "lab"},
	}}, nil)
	oldHand := vocabulary.ActorID(substrate.FunctionActor(name, "lab", "stamp"))
	newHand := vocabulary.ActorID(substrate.FunctionActor(vocabulary.Local, "lab", "stamp"))
	settled := time.Now().UTC().Truncate(time.Second)
	plant := func(op, key string, outcome []byte, locator string) {
		t.Helper()
		var at *string
		if locator != "" {
			at = &locator
		}
		if _, err := ds.db.ExecContext(ctx, `INSERT INTO idempotency_keys (operation, key, fingerprint, owner, outcome, locator, settled_at, expires_at)
			VALUES ($1, $2, $3, 'first', $4, $5, $6, $7)`, op, key, "fp-"+key, outcome, at, settled, settled.Add(time.Hour)); err != nil {
			t.Fatalf("plant %s: %v", key, err)
		}
	}
	text := name + "/tasks/task is how I wrote it"
	// Every record link the answer holds names the moved hand's actor record:
	// a mark's setter, its source, an alternative's source and a mapped
	// source.
	oldActor := `{"ref":"` + kindActor + `/` + oldHand + `"}`
	put := `{"id":"t1","kind":"` + name + `/tasks/task","properties":{"name":"` + text + `","about":{"ref":"` + name + `/tasks/task/t2"}},"labels":{},"version":3,"createdAt":"2026-11-01T00:00:00Z","updatedAt":"2026-11-01T00:00:00Z","created":true,` +
		`"marks":{"name":{"pinned":true,"setBy":` + oldActor + `,"derivedFrom":[` + oldActor + `],"setAt":"2026-11-01T00:00:00Z",` +
		`"alternatives":[{"derivedFrom":` + oldActor + `,"value":"other","updatedAt":"2026-11-01T00:00:00Z"}]}},` +
		`"linkedFrom":[{"ref":"` + kindActor + `/` + oldHand + `","kind":"` + kindActor + `","property":"about","mapping":"` + name + `/lab/mirror"}]}`
	actor := `{"id":"` + oldHand + `","kind":"` + kindActor + `","properties":{"type":"function","name":"` + stamp + `","function":{"ref":"` + kindFunction + `/` + stamp + `"}},"labels":{},"version":1,"createdAt":"2026-11-01T00:00:00Z","updatedAt":"2026-11-01T00:00:00Z"}`
	called := `{"output":{"kind":"` + name + `/tasks/task"},"effects":1}`
	plant(string(idemPut), "k-put", []byte(put), "")
	plant(string(idemPatch), "k-large", nil, name+"/tasks/task/t9")
	plant(string(idemFunctionCall), "k-call", []byte(called), "")
	plant(string(idemPatch), "k-actor", []byte(actor), "")
	plant(string(idemPatch), "k-actor-large", nil, kindActor+"/"+oldHand)

	var after string
	for pass := 1; pass <= 2; pass++ {
		tx, err := ds.db.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := carryLocal(ctx, tx, name, refs, moves, ds.reseal, false); err != nil {
			_ = tx.Rollback()
			t.Fatalf("pass %d: %v", pass, err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		row, err := idempotencyRead(ctx, ds.db, idemPut, "k-put", time.Now())
		if err != nil || row == nil {
			t.Fatalf("pass %d: read the put's key: %v, %v", pass, row, err)
		}
		var replayed keyedRecord
		if err := row.replay("k-put", "fp-k-put", &replayed); err != nil {
			t.Fatalf("pass %d: a repeat of the put is not answered: %v", pass, err)
		}
		about, _ := replayed.Properties["about"].(map[string]any)
		if replayed.Kind != "~/tasks/task" || about["ref"] != "~/tasks/task/t2" || replayed.Properties["name"] != text ||
			!replayed.Created || replayed.Version != 3 {
			t.Errorf("pass %d: the put answers %+v", pass, replayed)
		}
		newActor := kindActor + "/" + newHand
		if mark := replayed.Marks["name"]; mark.SetBy == nil || mark.SetBy.Ref != newActor {
			t.Errorf("pass %d: the put's mark links %+v, want the hand under ~", pass, mark.SetBy)
		}
		if from := replayed.Marks["name"].DerivedFrom; len(from) != 1 || from[0].Ref != newActor {
			t.Errorf("pass %d: the put's mark is derived from %+v, want the hand under ~", pass, from)
		}
		if alts := replayed.Marks["name"].Alternatives; len(alts) != 1 || alts[0].DerivedFrom == nil || alts[0].DerivedFrom.Ref != newActor {
			t.Errorf("pass %d: the put's alternative is derived from %+v, want the hand under ~", pass, alts)
		}
		if links := replayed.LinkedFrom; len(links) != 1 || links[0].Ref != newActor || links[0].Mapping != "~/lab/mirror" {
			t.Errorf("pass %d: the put is linked from %+v, want the hand under ~ by the mapping under ~", pass, links)
		}
		if pass == 2 && string(row.outcome) != after {
			t.Errorf("a second pass moved the put's outcome:\n  %s\n  %s", after, row.outcome)
		}
		after = string(row.outcome)

		large, err := idempotencyRead(ctx, ds.db, idemPatch, "k-large", time.Now())
		if err != nil || large == nil || large.locator != "~/tasks/task/t9" {
			t.Errorf("pass %d: the large outcome's locator: %+v, %v", pass, large, err)
		}
		hand, err := idempotencyRead(ctx, ds.db, idemPatch, "k-actor", time.Now())
		if err != nil || hand == nil {
			t.Fatalf("pass %d: read the actor's key: %v, %v", pass, hand, err)
		}
		var held keyedRecord
		if err := hand.replay("k-actor", "fp-k-actor", &held); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
		fn, _ := held.Properties["function"].(map[string]any)
		if held.ID != newHand || held.Properties["name"] != "~/lab/stamp" || fn["ref"] != kindFunction+"/~/lab/stamp" {
			t.Errorf("pass %d: the actor's answer is %+v, want the hand under ~", pass, held)
		}
		large2, err := idempotencyRead(ctx, ds.db, idemPatch, "k-actor-large", time.Now())
		if err != nil || large2 == nil || large2.locator != kindActor+"/"+newHand {
			t.Errorf("pass %d: the actor's large outcome's locator: %+v, %v", pass, large2, err)
		}
		call, err := idempotencyRead(ctx, ds.db, idemFunctionCall, "k-call", time.Now())
		if err != nil || call == nil || string(call.outcome) != called {
			t.Errorf("pass %d: the call's own output moved: %+v, %v", pass, call, err)
		}
	}
	var fingerprints, unsettled int
	if err := ds.db.QueryRowContext(ctx, `SELECT count(*) FILTER (WHERE fingerprint = 'fp-' || key), count(*) FILTER (WHERE settled_at <> $1)
		FROM idempotency_keys`, settled).Scan(&fingerprints, &unsettled); err != nil {
		t.Fatal(err)
	}
	if fingerprints != 5 || unsettled != 0 {
		t.Errorf("%d of 5 keys keep their fingerprint, %d moved their settlement", fingerprints, unsettled)
	}
}
