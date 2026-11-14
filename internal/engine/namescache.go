package engine

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

// namesCache holds each repository's names (decision record 0287): its name
// and the former names it still answers to. It is the ONE place in memory a
// repository's names are: a dataset holds none, and whoever needs them (the
// fold of a full-name spelling, a local package's hand, a reply that shows
// the name, a log line) asks here by the repository's id at the moment it
// needs them and keeps nothing past its request.
//
// THE RULE, the token cache's (tokencache.go): from the moment a transaction
// that changes a repository's names is about to commit until the database
// has proved that it committed or rolled back, the cache neither answers nor
// stores that repository's names, and they are read from the row in the
// reader's own statement. Three transactions change them: a rename, for the
// repository renamed; a registration or a rename that takes a lapsed former
// name, for the repository that held it; and a deletion. Each fences the id
// before its COMMIT (fence) and lifts the fence once the outcome is known
// (unfence): when the COMMIT answered, when none was sent, or, for a COMMIT
// whose answer was lost, once the names lock that transaction held is
// granted to a waiter (settleNames). Elapsed time proves nothing, and neither
// does an error while looking for the proof: a fence whose proof does not
// come stands, and costs reads. A read that began before a fence or an
// unfence is never stored, or it could put back what the fence took away.
//
// Nothing is closed, opened again or waited for because a name changed: a
// request already running on a dataset finishes on it, and its next fold asks
// the cache again. Whoever lifts a fence starts the repository's reconcile
// (reconcileNames): the row is the truth, the repository's own record follows
// it, and the next open is the backstop.
type namesCache struct {
	mu      sync.Mutex
	gen     uint64
	entries map[string]storedNames
	fences  map[string]int
}

// storedNames are a repository's name and every former name its row held at
// the read; each answer drops the ones lapsed by then.
type storedNames struct {
	name   string
	former []formerName
}

// get answers a repository's stored names, and the generation a read started
// now must carry to be stored. A fenced id answers nothing.
func (c *namesCache) get(id string) (storedNames, bool, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fences[id] > 0 {
		return storedNames{}, false, c.gen
	}
	e, ok := c.entries[id]
	return e, ok, c.gen
}

// put stores names read at generation gen, unless a fence or an unfence came
// since or the id is fenced.
func (c *namesCache) put(id string, gen uint64, e storedNames) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen || c.fences[id] > 0 {
		return
	}
	if c.entries == nil {
		c.entries = map[string]storedNames{}
	}
	c.entries[id] = e
}

// fence drops the id's names and stores none until unfence.
func (c *namesCache) fence(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fences == nil {
		c.fences = map[string]int{}
	}
	c.fences[id]++
	c.gen++
	delete(c.entries, id)
}

// unfence lifts one fence of the id once its transaction's outcome is
// proved, and drops its names again, so no read that began under the fence
// is kept.
func (c *namesCache) unfence(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fences[id]--; c.fences[id] <= 0 {
		delete(c.fences, id)
	}
	c.gen++
	delete(c.entries, id)
}

// forget drops everything the cache holds for a deleted repository.
func (c *namesCache) forget(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	delete(c.entries, id)
}

// fenced answers whether a fence of the id stands, for a test.
func (c *namesCache) fenced(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.fences[id] > 0
}

// namesOf answers repository id's names now: its name, then the former names
// still in force, newest first; none for a repository that is gone. A read of
// the row stores what it read where the rule allows, and writes nothing.
func (s *service) namesOf(ctx context.Context, id string) ([]string, error) {
	e, ok, gen := s.names.get(id)
	if !ok {
		var err error
		if e, err = s.readNames(ctx, id); err != nil {
			return nil, err
		}
		s.names.put(id, gen, e)
	}
	if e.name == "" {
		return nil, nil
	}
	out := []string{e.name}
	now := s.namesNow()
	for _, f := range e.former {
		if now.Before(f.until()) {
			out = append(out, f.name)
		}
	}
	return out, nil
}

// readNames reads a repository's name and former names from the rows, in one
// statement.
func (s *service) readNames(ctx context.Context, id string) (storedNames, error) {
	if s.maint == nil {
		return storedNames{}, fmt.Errorf("substrate/engine: read the names of %s: the service has no control plane", id)
	}
	rows, err := s.maint.QueryContext(ctx, `
		SELECT r.authority, f.name, f.retired_at
		FROM repositories r LEFT JOIN repository_former_names f ON f.repository_id = r.id
		WHERE r.id = $1
		ORDER BY f.retired_at DESC, f.name`, id)
	if err != nil {
		return storedNames{}, fmt.Errorf("substrate/engine: read the names of %s: %w", id, err)
	}
	defer func() { _ = rows.Close() }()
	var out storedNames
	for rows.Next() {
		var name sql.NullString
		var at sql.NullTime
		if err := rows.Scan(&out.name, &name, &at); err != nil {
			return storedNames{}, err
		}
		if name.Valid {
			out.former = append(out.former, formerName{name: name.String, retiredAt: at.Time.UTC()})
		}
	}
	return out, rows.Err()
}

// namesLockKeySQL is the transaction lock every transaction that changes a
// repository's names holds on its id until it ends: the proof a COMMIT whose
// answer was lost has ended is that lock granted to a waiter (settleNames).
const namesLockKeySQL = `hashtextextended('substrate.names:' || $1, 0)`

// lockNames takes repository id's names lock in tx, for the rest of tx.
func lockNames(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(`+namesLockKeySQL+`)`, id)
	return err
}

// commitFencingNames commits a control-plane transaction that changes the
// names of ids, each locked in it (lockNames), under the names cache's rule:
// fenced before the COMMIT; when the COMMIT answers, unfenced and reconciled;
// when its answer is lost, left to settleNames, which does both once the
// outcome is proved. A deletion (gone) forgets the id instead: there is
// nothing left to reconcile.
func (s *service) commitFencingNames(tx *sql.Tx, gone bool, ids ...string) error {
	for _, id := range ids {
		s.names.fence(id)
	}
	commit := tx.Commit
	if s.testNamesCommit != nil {
		commit = func() error { return s.testNamesCommit(tx) }
	}
	err := commit()
	for _, id := range ids {
		if err != nil {
			s.settleNames(id, gone)
			continue
		}
		s.liftNames(id, gone)
	}
	return err
}

// commitErasing is a deletion's COMMIT under both caches' rules: the
// repository's tokens (commitFencingTokens) and its names are fenced around
// the one COMMIT, and each is lifted, or left to its own proof, by the
// outcome.
func (s *service) commitErasing(tx *sql.Tx, id string) error {
	s.names.fence(id)
	err := s.commitFencingTokens(tx, id)
	if err != nil {
		s.settleNames(id, true)
	} else {
		s.liftNames(id, true)
	}
	return err
}

// liftNames lifts one fence of id whose transaction's outcome is proved, and
// starts what follows a name that may have changed: a deleted repository is
// forgotten, any other reconciled.
func (s *service) liftNames(id string, gone bool) {
	s.names.unfence(id)
	if gone {
		s.names.forget(id)
		return
	}
	s.reconcileNames(id)
}

// namesProofRetryMax caps the pause between two failed attempts at a proof.
const namesProofRetryMax = 5 * time.Second

// namesProofs is the one waiter per repository that proves a lost COMMIT
// answer's transaction ended: pending counts the fences it owes, by whether
// they were a deletion's, and running says whether the waiter runs.
type namesProofs struct {
	mu      sync.Mutex
	pending map[string]map[bool]int
	running map[string]bool
}

// settleNames lifts the fence a COMMIT whose answer was lost put up on id,
// once the database proves that transaction ended: it held id's names lock
// until it ended, so the lock granted to the repository's one waiter is that
// proof. The waiter runs in the background and is waited for by nobody; a
// failed attempt proves nothing and the next follows, and a server shutting
// down leaves the fence standing.
func (s *service) settleNames(id string, gone bool) {
	p := &s.namesProofs
	p.mu.Lock()
	if p.pending == nil {
		p.pending, p.running = map[string]map[bool]int{}, map[string]bool{}
	}
	if p.pending[id] == nil {
		p.pending[id] = map[bool]int{}
	}
	p.pending[id][gone]++
	if p.running[id] {
		p.mu.Unlock()
		return
	}
	p.running[id] = true
	p.mu.Unlock()
	s.spawn("prove a names commit ended", id, func(ctx context.Context) { s.proveNames(ctx, id) })
}

// proveNames is a repository's one waiter. An attempt proves the fences
// handed to it before it began: each doubted transaction sent its COMMIT, the
// names lock held, before its fence was handed here, so the lock asked for
// after that is granted after the transaction ended.
func (s *service) proveNames(ctx context.Context, id string) {
	p := &s.namesProofs
	pause := 50 * time.Millisecond
	for {
		p.mu.Lock()
		owed := p.pending[id]
		if len(owed) == 0 {
			delete(p.pending, id)
			delete(p.running, id)
			p.mu.Unlock()
			return
		}
		proving := map[bool]int{}
		for gone, n := range owed {
			proving[gone] = n
		}
		p.mu.Unlock()
		if err := s.awaitNamesLock(ctx, id); err == nil {
			p.mu.Lock()
			for gone, n := range proving {
				if p.pending[id][gone] -= n; p.pending[id][gone] <= 0 {
					delete(p.pending[id], gone)
				}
			}
			p.mu.Unlock()
			for gone, n := range proving {
				for range n {
					s.liftNames(id, gone)
				}
			}
			pause = 50 * time.Millisecond
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(pause):
		}
		pause = min(2*pause, namesProofRetryMax)
	}
}

// awaitNamesLock waits for id's names lock and lets it go at once.
func (s *service) awaitNamesLock(ctx context.Context, id string) error {
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockNames(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

// reconcileNames brings the repository's own record (`name`, `authority`) to
// the name its row has, in the background, waited for by nobody: whoever
// lifts a fence starts it. The row is the truth and the record follows it; a
// reconcile that fails, or a process that dies first, leaves it to the next
// open, which reconciles as it always has. A repository with no dataset open
// here has nothing to bring: its next open does it.
func (s *service) reconcileNames(id string) {
	s.mu.Lock()
	ds := s.datasets[id]
	s.mu.Unlock()
	if ds == nil {
		return
	}
	s.spawn("reconcile a renamed repository", id, func(ctx context.Context) {
		if err := ds.reconcileRepositoryName(ctx); err != nil {
			s.log.Warn("substrate: could not bring the repository's record to its name", "repository", id, "error", err)
		}
	})
}
