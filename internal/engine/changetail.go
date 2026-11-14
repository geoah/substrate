package engine

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sync"

	"github.com/geoah/substrate/internal/substrate"
)

// changeTail is the repository's one reader of the changelog's tail, in
// memory, feeding every stream that follows it (substrate.WithTailRead). A
// commit signals the highest seq it wrote (broadcaster.signal), every stream
// wakes and asks for the rows after its cursor, and the first to ask reads the
// new rows once while the others wait for that read and are answered from
// memory: a hundred streams cost one read per change.
//
// The window holds every row a read of the changelog would answer, delivery
// entries hidden, with seq in (from, upTo], in seq order and with no gap.
// Writers serialize on the changelog lock from their start and signal after
// their commit, so when a seq is signaled every seq below it is committed,
// and a read bounded by the signaled seq is complete. A cursor below from is
// read from the table as before, so a stream resumed from far back skips
// nothing either.
//
// A commit whose answer was lost signals nothing, though it may have landed,
// so it resets the window (reset) and wakes the streams (broadcaster.nudge):
// the read each makes opens the window again at the table's head, and a
// stream below that reads the table, where the write is.
// The answer may be lost before Postgres finished the commit, so a window
// reopened after a reset reads the head only once it holds the changelog
// lock, which the doubted transaction holds until it ends either way.
type changeTail struct {
	// read admits one reader of the table at a time; the others wait for it
	// and find the window current.
	read sync.Mutex

	mu    sync.Mutex
	ready bool
	from  int64
	upTo  int64
	rows  []substrate.Change
	// gen moves with every reset, so a read that began before one leaves
	// the window as the reset left it.
	gen uint64
	// fenced is set by a reset until the window reopens under the
	// changelog lock.
	fenced bool
}

// reset empties the window after a commit that may have landed unsignaled.
func (tail *changeTail) reset() {
	tail.mu.Lock()
	defer tail.mu.Unlock()
	tail.ready, tail.from, tail.upTo, tail.rows = false, 0, 0, nil
	tail.fenced = true
	tail.gen++
}

// tailWindow bounds the rows the window keeps. A stream more than this many
// rows behind reads the table, as every stream did before.
const tailWindow = 1024

// tailBatch bounds one read of the table into the window.
const tailBatch = 500

// tailChanges answers a stream's Changes from the window, reading the table
// once for every stream when the window is behind the last signaled seq. ok
// is false where the window cannot answer: a filter it does not evaluate (a
// search, the values), or a cursor below the window.
func (ds *dataset) tailChanges(ctx context.Context, after int64, f substrate.ChangeFilter, limit int) (out []substrate.Change, ok bool, err error) {
	if f.Q != "" || f.Values {
		return nil, false, nil
	}
	match, err := ds.changeMatcher(f)
	if err != nil {
		return nil, true, err
	}
	if err := ds.catchUpTail(ctx); err != nil {
		return nil, true, err
	}
	if limit <= 0 {
		limit = 100
	}
	tail := &ds.tail
	tail.mu.Lock()
	defer tail.mu.Unlock()
	if !tail.ready || after < tail.from {
		return nil, false, nil
	}
	i, _ := slices.BinarySearchFunc(tail.rows, after+1, func(c substrate.Change, seq int64) int {
		return compareSeq(c.Seq, seq)
	})
	for ; i < len(tail.rows) && len(out) < limit; i++ {
		if match(tail.rows[i]) {
			out = append(out, tail.rows[i])
		}
	}
	return out, true, nil
}

func compareSeq(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// catchUpTail brings the window up to the last seq a commit signaled.
func (ds *dataset) catchUpTail(ctx context.Context) error {
	tail := &ds.tail
	target := ds.watch.lastSignaled()
	tail.mu.Lock()
	current := tail.ready && tail.upTo >= target
	tail.mu.Unlock()
	if current {
		return nil
	}
	tail.read.Lock()
	defer tail.read.Unlock()
	tail.mu.Lock()
	ready, upTo, gen, fenced := tail.ready, tail.upTo, tail.gen, tail.fenced
	tail.mu.Unlock()
	if !ready {
		// The window opens at the head: a stream resumed below it reads the
		// table until it reaches the window.
		head, err := ds.tailHead(ctx, fenced)
		if err != nil {
			return fmt.Errorf("substrate/engine: open the change tail: %w", err)
		}
		tail.mu.Lock()
		if tail.gen == gen {
			tail.ready, tail.from, tail.upTo, tail.fenced = true, head, head, false
		}
		tail.mu.Unlock()
		return nil
	}
	for upTo < target {
		b := &builder{}
		b.add(`seq > ` + b.arg(upTo))
		b.add(`seq <= ` + b.arg(target))
		hideDeliveryEntries(b)
		ds.noteChangeRead()
		rows, err := ds.queryChanges(ctx, b, `seq`, tailBatch, false)
		if err != nil {
			return err
		}
		next := target
		if len(rows) == tailBatch {
			next = rows[len(rows)-1].Seq
		}
		tail.mu.Lock()
		if tail.gen != gen {
			// A reset landed during the read: the window opens again at
			// the next one.
			tail.mu.Unlock()
			return nil
		}
		tail.rows = append(tail.rows, rows...)
		tail.upTo = next
		if drop := len(tail.rows) - tailWindow; drop > 0 {
			tail.from = tail.rows[drop-1].Seq
			tail.rows = slices.Clone(tail.rows[drop:])
		}
		tail.mu.Unlock()
		upTo = next
	}
	return nil
}

// tailHead reads the changelog's head for the window to open at. fenced, the
// read waits for the changelog lock first: no writer holds it after that,
// the doubted one included, and the read that follows sees every commit that
// ended before.
func (ds *dataset) tailHead(ctx context.Context, fenced bool) (int64, error) {
	var head int64
	if !fenced {
		err := ds.db.QueryRowContext(ctx, `SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&head)
		if h := ds.svc.testTailHeadHook; h != nil {
			h()
		}
		return head, err
	}
	// READ COMMITTED: the head is read on a snapshot taken after the lock
	// was granted, not when the transaction began waiting for it.
	tx, err := ds.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(`+advisoryKeySQL+`)`, ds.scope.lockKey(changelogLockKey)); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&head); err != nil {
		return 0, err
	}
	if h := ds.svc.testTailHeadHook; h != nil {
		h()
	}
	return head, tx.Commit()
}

// changeMatcher is buildChangeFilter in Go, for the arms the window
// evaluates: the kinds resolved through the same registry, the ops and the
// actors, each included or excluded, and the record scope with the merge and
// split pair it follows.
func (ds *dataset) changeMatcher(f substrate.ChangeFilter) (func(substrate.Change) bool, error) {
	kinds, err := ds.resolveChangeKinds(f.Kinds)
	if err != nil {
		return nil, err
	}
	exclude, err := ds.resolveChangeKinds(f.ExcludeKinds)
	if err != nil {
		return nil, err
	}
	return func(c substrate.Change) bool {
		if len(f.Kinds) > 0 && !slices.Contains(kinds, c.Kind) {
			return false
		}
		if slices.Contains(exclude, c.Kind) {
			return false
		}
		if len(f.Ops) > 0 && !slices.Contains(f.Ops, c.Op) {
			return false
		}
		if slices.Contains(f.ExcludeOps, c.Op) {
			return false
		}
		if len(f.Actors) > 0 && !slices.Contains(f.Actors, c.Actor) {
			return false
		}
		if slices.Contains(f.ExcludeActors, c.Actor) {
			return false
		}
		if f.RecordID != "" && c.RecordID != f.RecordID {
			if c.Op != substrate.OpMerge && c.Op != substrate.OpSplit {
				return false
			}
			winner, _ := c.Payload[payloadWinner].(string)
			loser, _ := c.Payload[payloadLoser].(string)
			if winner != f.RecordID && loser != f.RecordID {
				return false
			}
		}
		return true
	}, nil
}

// noteChangeRead reports one read of the changelog's rows a stream caused,
// to the test seam that counts them.
func (ds *dataset) noteChangeRead() {
	if h := ds.svc.testChangeReadHook; h != nil {
		h(ds.scope.Repository)
	}
}
