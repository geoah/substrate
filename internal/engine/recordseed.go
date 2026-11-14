package engine

// THE RECORD SEED (decision record 0229): the data records a seeded package
// ships beside its declarations (vocabulary.LoadShipped), such as the
// console package's collection views. The units have their own seed
// (unitseed.go), whose graph checks no other record needs.
//
// It writes at registration, in the creation transaction after the
// declarations, and again at every open after the shipped vocabulary upgrade,
// which is what declares a newer binary's kinds in an older repository. A
// shipped record the repository lacks is written, and one whose stored
// properties differ from it is refreshed with apply semantics, unless any
// hand but the seed's and the engine's ever wrote or deleted it
// (anotherHandWrote, decision record 0191): the owner's edit and deletion
// stand. A record whose kind the repository does not declare is skipped, and
// so is one whose read or write fails at an open, which is logged: the open
// never fails for a shipped record.
//
// The comparison reads the stored properties as JSON, so a shipped record
// spells a reference as the engine stores it, `{ref: …}`, or every open would
// find it different and write it again.

import (
	"context"
	"fmt"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func shippedRecordRef(r vocabulary.ShippedRecord) eref { return eref{Kind: r.Kind, ID: r.ID} }

// shippedRecordRefs is every record an open's seeds would consider writing,
// read from the lists they write from and where each seed runs: the records
// the seeded packages ship (here), the units and conversions (unitseed.go),
// the vendor provider rows (seedproviders.go) and the roles (roles.go). It
// is one half of what keeps its deletion through the forgetting
// (shippedAmong), so it is the seeds' own enumeration and no second one: a
// record this binary no longer ships is not in it.
func (s *service) shippedRecordRefs() (map[eref]bool, error) {
	out := map[eref]bool{}
	if s.seedRecords {
		for _, r := range s.shippedRecords {
			out[shippedRecordRef(r)] = true
		}
	}
	if s.seedUnits {
		units, err := shippedUnitRecords()
		if err != nil {
			return nil, err
		}
		for _, u := range units {
			out[u.ref()] = true
		}
	}
	if s.seedLLMProviders {
		for _, p := range defaultLLMProviders {
			out[eref{Kind: typeProvider, ID: p.id}] = true
		}
	}
	for _, r := range scope.ShippedRoles {
		out[eref{Kind: kindRole, ID: r.ID}] = true
	}
	return out, nil
}

// shippedAmong is which of refs a package ships, and the hand that ships
// each: the records whose deletion the pass keeps rather than forgets
// (decision 0319), because a seed or an install writes a shipped record
// again unless the hands that wrote it hold one other than the shipping ones
// (anotherHandWrote), and a forgotten deletion would leave no hand. A
// record ships
// where an open's seeds write it (shippedRecordRefs), under the seed's hand,
// or where a package's own hand (bundle:<authority>:<package>, catalog
// install) is among the hands that ever wrote it (recordHands), at its
// install or an upgrade. A declaration and a trigger are neither: the
// version decides an upgrade of a declaration, an install writes its
// triggers again whatever their history says, and a trigger's delivery
// history is most of what compaction takes.
func (ds *dataset) shippedAmong(ctx context.Context, q dbx, refs []eref) (map[eref]substrate.Actor, error) {
	listed, err := ds.svc.shippedRecordRefs()
	if err != nil {
		return nil, err
	}
	out := map[eref]substrate.Actor{}
	var asked []eref
	for _, r := range refs {
		switch {
		case listed[r]:
			out[r] = substrate.ActorSeed
		case r.Kind != typeTrigger && !vocabulary.IsDeclarationRef(r.Kind):
			asked = append(asked, r)
		}
	}
	if len(asked) == 0 {
		return out, nil
	}
	hands, err := recordHandsOf(ctx, q, asked)
	if err != nil {
		return nil, err
	}
	keys := map[string]substrate.Actor{}
	for r, hs := range hands {
		for _, h := range hs {
			key, ok := keys[h.actor]
			if !ok {
				key = actorKeyIn(ctx, q, h.actor)
				keys[h.actor] = key
			}
			if substrate.IsBundleActor(key) && key != substrate.ActorSeed {
				out[r] = key
				break
			}
		}
	}
	return out, nil
}

// seedShippedRecords writes every shipped record the transaction's
// declarations name that is missing or stale and no other hand holds. It is
// the creation transaction's step, so a failed write fails the creation: a
// shipped record the seeded vocabulary refuses is a defect of the tree.
func (t *txn) seedShippedRecords() error {
	if !t.ds.svc.seedRecords {
		return nil
	}
	for _, rec := range t.ds.svc.shippedRecords {
		if err := t.seedShippedRecord(rec); err != nil {
			return err
		}
	}
	return nil
}

// seedShippedRecord writes one shipped record where the seed may, deciding
// under the record's lock.
func (t *txn) seedShippedRecord(rec vocabulary.ShippedRecord) error {
	if _, err := t.resolveType(rec.Kind); err != nil {
		return nil
	}
	ref := shippedRecordRef(rec)
	row, err := t.loadRow(ref, true)
	if err != nil {
		return err
	}
	if shippedRowCurrent(row, rec) {
		return nil
	}
	held, err := t.anotherHandWrote(ref, substrate.ActorSeed)
	if err != nil || held {
		return err
	}
	if rec.Kind == typeModel {
		// The owner's own record for this row and model stands in for the
		// shipped one, which the write would refuse (admitModelRow).
		if other, err := modelPairHolder(t.ctx, t.tx, rec.ID, rec.Properties); err != nil || other != "" {
			return err
		}
	}
	if _, err := t.put(substrate.PutInput{Kind: rec.Kind, ID: rec.ID, Properties: rec.Properties}); err != nil {
		return fmt.Errorf("substrate/engine: seed %s %s: %w", rec.Kind, rec.ID, err)
	}
	return nil
}

// ensureShippedRecords is the open's catch-up. Each record is read outside
// any transaction first, so an open with nothing to write takes no changelog
// lock, and each one that needs a write takes its own transaction, so one
// refused record never holds back the others.
func (ds *dataset) ensureShippedRecords(ctx context.Context) error {
	if ds.svc.readOnly || !ds.svc.seedRecords {
		return nil
	}
	reg := ds.registry()
	for _, rec := range ds.svc.shippedRecords {
		if _, ok := reg.ByIdentity(rec.Kind); !ok {
			continue
		}
		write, err := ds.shippedRecordOwed(ctx, rec)
		if err == nil && !write {
			continue
		}
		if err == nil {
			err = ds.inTx(ctx, substrate.ActorSeed, true, func(t *txn) error {
				return t.seedShippedRecord(rec)
			})
		}
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			ds.svc.log.Warn("substrate: a shipped record was not seeded; the repository opens without it",
				"repository", ds.scope.Repository, "kind", rec.Kind, "id", rec.ID, "error", err)
			continue
		}
		ds.svc.log.Info("substrate: seeded a shipped record", "repository", ds.scope.Repository, "kind", rec.Kind, "id", rec.ID)
	}
	return nil
}

// shippedRecordOwed reports whether the seed would write rec, as
// seedShippedRecord decides it again under the lock: the stored row is
// absent, deleted or stale, only the shipping hands ever wrote it, and, for
// a shipped model, no other record holds its row and model.
func (ds *dataset) shippedRecordOwed(ctx context.Context, rec vocabulary.ShippedRecord) (bool, error) {
	row, err := ds.loadRowDB(ctx, shippedRecordRef(rec))
	if err != nil || shippedRowCurrent(row, rec) {
		return false, err
	}
	held, err := anotherHandIn(ctx, ds.db, shippedRecordRef(rec), substrate.ActorSeed)
	if err != nil || held {
		return false, err
	}
	if rec.Kind == typeModel {
		other, err := modelPairHolder(ctx, ds.db, rec.ID, rec.Properties)
		return other == "" && err == nil, err
	}
	return true, nil
}

// shippedRowCurrent reports whether a stored row already holds what rec
// ships: live, with every shipped property at its shipped value.
func shippedRowCurrent(row *erow, rec vocabulary.ShippedRecord) bool {
	return row != nil && row.DeletedAt == nil && coversProps(row.Props, rec.Properties)
}
