package engine

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// Every path a write takes to a recordpatchpolicy, by every hand that writes
// one (refuseInstalledPolicyWrite). Installed code is refused on every path,
// whichever hand it writes through, each stamped the way production stamps
// it: a function's effect, a function it calls and an agent's function tool
// (setEffectEmit, as applyEffects does), an agent's write tool and the policy
// judge (an effect ceiling), a provider's sync (asSyncWriter). A person's own
// edit and the seed write every one, and a rebuild's replay reproduces what
// they wrote.
func TestEveryPathToAPolicyIsHeldToItsHand(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	policy := vocabulary.KindRecordPatchPolicy
	props := func(action string) map[string]any {
		return map[string]any{"selector": map[string]any{"kinds": []any{"substrate.reamde.dev/core/contact"}}, "action": action}
	}
	asOwner := func(fn func(tx *txn) error) error { return ds.inTx(ctx, substrate.ActorAPI, false, fn) }
	write := func(tx *txn, id, action string) error {
		_, err := tx.put(substrate.PutInput{Kind: policy, ID: id, Properties: props(action)})
		return err
	}
	// setup writes, as the owner, what a path needs to exist, and answers
	// what run reads: the merge a split reverses.
	setup := func(t *testing.T, fn func(tx *txn) (string, error)) string {
		t.Helper()
		var state string
		if err := asOwner(func(tx *txn) error {
			var err error
			state, err = fn(tx)
			return err
		}); err != nil {
			t.Fatal(err)
		}
		return state
	}

	// everyHand, where set, is the refusal a permitted hand meets on the
	// path too: a rule older than this one that holds every hand.
	type path struct {
		name      string
		setup     func(tx *txn, id string) (string, error)
		run       func(tx *txn, id, state string) error
		everyHand string
	}
	existing := func(tx *txn, id string) (string, error) { return "", write(tx, id, "refuse") }
	pair := func(tx *txn, id string) (string, error) {
		if err := write(tx, id+"-w", "refuse"); err != nil {
			return "", err
		}
		return "", write(tx, id+"-l", "refuse")
	}
	paths := []path{
		{"create", nil, func(tx *txn, id, _ string) error { return write(tx, id, "allow") }, ""},
		{"patch", existing, func(tx *txn, id, _ string) error {
			_, err := tx.patch(eref{Kind: policy, ID: id}, substrate.PatchInput{Properties: map[string]any{"action": "allow"}})
			return err
		}, ""},
		{"delete", existing, func(tx *txn, id, _ string) error {
			_, err := tx.softDeleteIf(eref{Kind: policy, ID: id}, nil)
			return err
		}, ""},
		// As deleteWith purges: the tombstone, then the purge, in one
		// transaction.
		{"purge", existing, func(tx *txn, id, _ string) error {
			e, err := tx.softDeleteIf(eref{Kind: policy, ID: id}, nil)
			if err != nil {
				return err
			}
			return tx.purgeNow(eref{Kind: e.Kind, ID: e.ID}, e.Finalizers)
		}, ""},
		{"merge", pair, func(tx *txn, id, _ string) error {
			_, err := tx.mergeRecord(eref{Kind: policy, ID: id + "-w"}, eref{Kind: policy, ID: id + "-l"})
			return err
		}, "not the generic merge surface"},
		// No hand merges a policy (guardMergeType, the merge row below), so
		// no merge of one exists to split; the merge is planted as a row
		// naming two policies, so the guard on the split is what answers.
		{"split", func(tx *txn, id string) (string, error) {
			if _, err := pair(tx, id); err != nil {
				return "", err
			}
			mergeID := id + "-merge"
			_, err := tx.tx.ExecContext(tx.ctx, `INSERT INTO records (kind, id, props) VALUES ($1, $2, $3)`,
				kindRecordMerge, mergeID, fmt.Sprintf(`{"winner": {"ref": "%s/%s-w"}, "loser": {"ref": "%s/%s-l"}}`, policy, id, policy, id))
			return mergeID, err
		}, func(tx *txn, _, mergeID string) error {
			_, err := tx.split(mergeID)
			return err
		}, ""},
		{"restore", func(tx *txn, id string) (string, error) {
			if err := write(tx, id, "refuse"); err != nil {
				return "", err
			}
			_, err := tx.softDeleteIf(eref{Kind: policy, ID: id}, nil)
			return "", err
		}, func(tx *txn, id, _ string) error { return write(tx, id, "allow") }, ""},
	}

	emit := []string{policy, vocabulary.KindRecordPatchRequest}
	fnActor := substrate.Actor("function:policy.test.dev:rules:open")
	asFunction := func(actor substrate.Actor) func(fn func(tx *txn) error) error {
		return func(fn func(tx *txn) error) error {
			return ds.inTx(ctx, actor, false, func(tx *txn) error { tx.setEffectEmit(emit); return fn(tx) })
		}
	}
	type hand struct {
		name    string
		allowed bool
		write   func(fn func(tx *txn) error) error
	}
	hands := []hand{
		{"a function's effect", false, asFunction(fnActor)},
		{"a function it calls", false, asFunction("function:policy.test.dev:rules:callee")},
		{"an agent's function tool", false, asFunction("function:policy.test.dev:rules:tool")},
		{"an agent's write tool", false, func(fn func(tx *txn) error) error {
			return ds.inTx(ctx, "agent:policy.test.dev:rules:scribe", false, func(tx *txn) error {
				(&effectCeiling{emit: emit}).stamp(tx)
				return fn(tx)
			})
		}},
		{"a provider's sync", false, func(fn func(tx *txn) error) error {
			return ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
				return tx.asSyncWriter("bundle:policy.test.dev:feed", func() error { return fn(tx) })
			})
		}},
		{"the policy judge", false, func(fn func(tx *txn) error) error {
			return ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
				(&effectCeiling{emit: emit, policyDecision: true}).stamp(tx)
				return fn(tx)
			})
		}},
		{"a person's own edit", true, asOwner},
		{"the seed", true, func(fn func(tx *txn) error) error { return ds.inTx(ctx, substrate.ActorSystem, true, fn) }},
	}
	for hi, h := range hands {
		for _, p := range paths {
			id := fmt.Sprintf("hand%d-%s", hi, p.name)
			var state string
			if p.setup != nil {
				state = setup(t, func(tx *txn) (string, error) { return p.setup(tx, id) })
			}
			err := h.write(func(tx *txn) error { return p.run(tx, id, state) })
			refused := err != nil && strings.Contains(err.Error(), "installed code never writes the door's own rules")
			switch {
			case !h.allowed && !refused:
				t.Errorf("%s, %s: %v, want the refusal", h.name, p.name, err)
			case h.allowed && refused:
				t.Errorf("%s, %s: %v, want the owner's hand let through", h.name, p.name, err)
			case h.allowed && p.everyHand == "" && err != nil:
				t.Errorf("%s, %s: %v, want it written", h.name, p.name, err)
			case h.allowed && p.everyHand != "" && (err == nil || !strings.Contains(err.Error(), p.everyHand)):
				t.Errorf("%s, %s: %v, want the refusal every hand meets: %s", h.name, p.name, err, p.everyHand)
			}
		}
	}

	// A rebuild's replay is the third hand that writes a policy: it
	// reproduces every row the owner and the seed left, and none the
	// refused hands tried.
	before := policyRows(t, ds)
	if len(before) == 0 {
		t.Fatal("no policy rows to replay")
	}
	if _, err := ds.svc.RebuildRepository(ctx, ds.info.ID); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if after := policyRows(t, ds); !reflect.DeepEqual(after, before) {
		t.Errorf("the rebuild's replay left the policies\n %v\nwant\n %v", after, before)
	}
}

// policyRows is every recordpatchpolicy row, live or tombstoned: its id, its
// version, whether it is deleted and its properties.
func policyRows(t *testing.T, ds *dataset) []string {
	t.Helper()
	rows, err := ds.db.QueryContext(context.Background(), `
		SELECT id, version, deleted_at IS NOT NULL, props::text FROM records WHERE kind = $1 ORDER BY id`,
		vocabulary.KindRecordPatchPolicy)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id, props string
		var version int64
		var deleted bool
		if err := rows.Scan(&id, &version, &deleted, &props); err != nil {
			t.Fatal(err)
		}
		out = append(out, fmt.Sprintf("%s v%d deleted=%v %s", id, version, deleted, props))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
