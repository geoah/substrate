package engine

import (
	"context"
	"database/sql"
	"errors"
)

// Id machinery: the former-id trail every read and write resolves through, and
// the per-record advisory lock writes serialize on. Identity is the (type, id)
// pair: trails resolve within one type, and lock keys carry the type beside
// the id.

// canonicalOf follows the former-id trail to the record an id now denotes
// within its type. Merge flattens the trail (moving the loser's own trail
// onto the winner), so this is a single hop in practice; the loop is a cycle
// guard, not a chain walk.
func canonicalOf(id string, lookup func(string) (string, error)) (string, error) {
	seen := map[string]bool{id: true}
	cur := id
	for range 8 {
		next, err := lookup(cur)
		if err != nil {
			return "", err
		}
		if next == "" || seen[next] {
			return cur, nil
		}
		seen[next] = true
		cur = next
	}
	return cur, nil
}

func (t *txn) canonicalOf(ref eref) (eref, error) {
	id, err := canonicalOf(ref.ID, func(cur string) (string, error) {
		return t.formerTarget(ref.Kind, cur)
	})
	if err != nil {
		return eref{}, err
	}
	return eref{Kind: ref.Kind, ID: id}, nil
}

func (ds *dataset) canonicalOf(ctx context.Context, x dbx, ref eref) (eref, error) {
	id, err := canonicalOf(ref.ID, func(cur string) (string, error) {
		var next string
		err := x.QueryRowContext(ctx,
			`SELECT record_id FROM former_ids WHERE record_kind = $1 AND former_id = $2`,
			ref.Kind, cur).Scan(&next)
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return next, err
	})
	if err != nil {
		return eref{}, err
	}
	return eref{Kind: ref.Kind, ID: id}, nil
}

// lockRecord takes the write lock for one record — keyed by the FULL (type,
// id) identity, so two types holding the same id never contend. Callers take
// them in ascending (type, id) order.
func (t *txn) lockRecord(ref eref) error {
	return t.lockKey("record|" + ref.key())
}

// lockCanonical is the merge barrier every ADDRESSED write goes through: it
// takes the per-record advisory lock on the addressed (type, id) BEFORE
// reading the former-id trail, then re-resolves and re-locks hop by hop until
// the resolution is stable under a held lock. Merge takes the same locks
// before it tombstones a loser, so resolve-then-wait can no longer straddle a
// merge commit: either the merge committed first (the trail is visible under
// the lock) or it queues behind this writer — a write addressed at a loser
// can never see it live, wait out the merge on the row lock, and resurrect
// it. Crossing chains can deadlock in theory; Postgres detects that, aborts
// one side, and the delivery's ordinary retry absorbs it.
func (t *txn) lockCanonical(ref eref) (eref, error) {
	cur := ref
	for range 8 {
		if err := t.lockRecord(cur); err != nil {
			return eref{}, err
		}
		next, err := t.canonicalOf(cur)
		if err != nil {
			return eref{}, err
		}
		if next == cur {
			return cur, nil
		}
		cur = next
	}
	return cur, nil
}

// lockCanonicalPair locks two addressed records in ascending (type, id)
// order — the same deterministic order merge takes — then resolves each under
// the locks.
func (t *txn) lockCanonicalPair(a, b eref) (eref, eref, error) {
	first, second := a, b
	if second.less(first) {
		first, second = second, first
	}
	if err := t.lockRecord(first); err != nil {
		return eref{}, eref{}, err
	}
	if err := t.lockRecord(second); err != nil {
		return eref{}, eref{}, err
	}
	ca, err := t.lockCanonical(a)
	if err != nil {
		return eref{}, eref{}, err
	}
	cb, err := t.lockCanonical(b)
	if err != nil {
		return eref{}, eref{}, err
	}
	return ca, cb, nil
}

// lockKey takes a transaction-scoped advisory lock on an arbitrary key,
// PER REPOSITORY. Advisory locks are cluster-wide — they know nothing about
// schemas, tables or row level security — so the repository has to be in the
// key or every write on the box serializes on one lock, which is exactly what
// v0 did. The scope composes it (Scope.lockKey), so no caller can forget.
func (t *txn) lockKey(key string) error {
	_, err := t.exec(`SELECT pg_advisory_xact_lock(hashtext($1)::bigint)`, t.ds.scope.lockKey(key))
	return err
}

// lockKeyShared takes the SHARED side of a transaction-scoped advisory lock:
// any number of shared holders coexist, an exclusive lockKey on the same key
// waits them out (and blocks new ones). Per repository, like lockKey.
func (t *txn) lockKeyShared(key string) error {
	_, err := t.exec(`SELECT pg_advisory_xact_lock_shared(hashtext($1)::bigint)`, t.ds.scope.lockKey(key))
	return err
}

// lockRegistryDepShared takes the SHARED registry-dependency lock (bundles.go
// registryDepKey) once per transaction and remembers it. Every data write takes
// it before it resolves its kind, and holds it to commit, so the declaration the
// write validates against is the declaration its refs rows project against.
//
// FIRST IN THE ORDER. The lock order this tree holds is registry-dep <
// subject-type < record, so this is taken before any of the write's own locks;
// taking it at the write's entry rather than after the kind is resolved is what
// makes "the declaration cannot move under this write" true of the resolution
// too.
//
// A vocabulary apply holds the EXCLUSIVE side of the same key and writes rows
// inside its own transaction (the projection, a bundle's data documents), which
// reach this. Re-requesting a lock the same transaction already holds always
// succeeds in Postgres, so no self-deadlock is possible.
func (t *txn) lockRegistryDepShared() error {
	if t.heldRegistryDep {
		return nil
	}
	if err := t.lockKeyShared(registryDepKey(t.ds)); err != nil {
		return err
	}
	t.heldRegistryDep = true
	return nil
}
