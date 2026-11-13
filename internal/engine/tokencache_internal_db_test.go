package engine

// A token is read from the table once and answered from memory after, and
// a revoke or a delete through the engine stops it at once.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/rand"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

func TestATokenIsCachedAndARevokeStopsItAtOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	mint := func(label string) (substrate.TokenInfo, string) {
		t.Helper()
		info, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: label})
		if err != nil {
			t.Fatalf("mint %s: %v", label, err)
		}
		return info, secret
	}
	authenticates := func(secret string) error {
		_, _, err := ds.svc.Authenticate(ctx, secret)
		return err
	}

	// Answered from memory: a change the process did not make goes unseen
	// until the cache empties.
	cached, secret := mint("cached")
	if err := authenticates(secret); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := ds.db.ExecContext(ctx, `UPDATE records SET deleted_at = now() WHERE kind = $1 AND id = $2`, kindToken, cached.ID); err != nil {
		t.Fatalf("tombstone the token behind the engine: %v", err)
	}
	if err := authenticates(secret); err != nil {
		t.Fatalf("a cached token read the table: %v", err)
	}
	ds.svc.tokens.empty()
	if err := authenticates(secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a token gone from the table after the cache emptied = %v, want ErrAuth", err)
	}

	// A revoke through the engine.
	_, revoked := mint("revoked")
	if err := authenticates(revoked); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if err := ds.svc.RevokeToken(ctx, revoked, ""); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := authenticates(revoked); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a revoked token = %v, want ErrAuth at once", err)
	}

	// A delete of the token record.
	deleted, secret := mint("deleted")
	if err := authenticates(secret); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, kindToken, deleted.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the token: %v", err)
	}
	if err := authenticates(secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a deleted token = %v, want ErrAuth at once", err)
	}

	// A lookup that began before the cache emptied is not kept.
	_, gen := func() (bool, uint64) { _, ok, g := ds.svc.tokens.get("stale", nowUTC()); return ok, g }()
	ds.svc.tokens.empty()
	ds.svc.tokens.put("stale", gen, tokenEntry{at: nowUTC()})
	if _, ok, _ := ds.svc.tokens.get("stale", nowUTC()); ok {
		t.Fatal("a lookup from before the cache emptied was cached")
	}
}

// heldCommits is a service whose COMMITs pass through a seam a test arms for
// one: the armed COMMIT reports reached, runs check, and is then run or not
// as the test says, and answered as it says. Postgres itself can be made to
// hold a COMMIT that ends a token: a deferred trigger on the token's row
// waits on an advisory lock the test holds, so the COMMIT lands when the
// test lets go, after the write was answered.
type heldCommits struct {
	armed   atomic.Bool
	reached chan struct{}
	check   func()
	answer  func(commit func() error) error
	// failing proves nothing while set: each attempt to prove a doubted
	// commit ended fails, and attempts counts them.
	failing  atomic.Bool
	attempts atomic.Int32
}

func newHeldCommits() *heldCommits { return &heldCommits{reached: make(chan struct{}, 1)} }

func (h *heldCommits) options() []Option {
	return []Option{
		WithTestCommitTx(func(commit func() error) error {
			if !h.armed.CompareAndSwap(true, false) {
				return commit()
			}
			h.reached <- struct{}{}
			h.check()
			return h.answer(commit)
		}),
		WithTestTokenProof(func() error {
			h.attempts.Add(1)
			if h.failing.Load() {
				return errors.New("no proof this time")
			}
			return nil
		}),
	}
}

// arm sets the next COMMIT's check and answer.
func (h *heldCommits) arm(check func(), answer func(commit func() error) error) {
	h.check, h.answer = check, answer
	h.armed.Store(true)
}

// wasReached fails the test when the armed COMMIT never reached the seam.
func (h *heldCommits) wasReached(t *testing.T) {
	t.Helper()
	select {
	case <-h.reached:
	default:
		t.Fatal("the token's deletion never reached its COMMIT's seam")
	}
}

// errAnswerLost is a COMMIT's answer lost on its way back.
var errAnswerLost = errors.New("the connection broke before the COMMIT answered")

// commitHold makes Postgres hold every COMMIT that ends a token of this
// repository while the test holds an advisory lock: a deferred trigger on
// the token's row waits on it, and fails the COMMIT once let go when told
// to roll back. The wait outlasts a cancel, as a backend whose client went
// away goes on with its COMMIT: the write's own context ends once the write
// has answered, and the COMMIT lands after.
type commitHold struct {
	db  *sql.DB
	key int32
}

func newCommitHold(t *testing.T, ds *dataset) *commitHold {
	t.Helper()
	c := &commitHold{db: rawDB(t, ds), key: rand.Int31n(1<<30) + 1}
	for _, stmt := range []string{
		`CREATE TABLE test_token_commit (rollback boolean NOT NULL)`,
		`INSERT INTO test_token_commit VALUES (false)`,
		fmt.Sprintf(`CREATE FUNCTION test_hold_token_commit() RETURNS trigger LANGUAGE plpgsql SECURITY DEFINER AS $$
			BEGIN
				LOOP
					BEGIN
						PERFORM pg_advisory_xact_lock(%d);
						EXIT;
					EXCEPTION WHEN query_canceled THEN
						NULL;
					END;
				END LOOP;
				IF (SELECT rollback FROM test_token_commit) THEN
					RAISE EXCEPTION 'the held COMMIT rolls back';
				END IF;
				RETURN NULL;
			END $$`, c.key),
		`CREATE CONSTRAINT TRIGGER test_hold_token_commit AFTER UPDATE ON records
			DEFERRABLE INITIALLY DEFERRED FOR EACH ROW
			WHEN (NEW.kind = 'substrate.reamde.dev/core/token' AND NEW.deleted_at IS NOT NULL)
			EXECUTE FUNCTION test_hold_token_commit()`,
	} {
		if _, err := c.db.Exec(stmt); err != nil {
			t.Fatalf("hold the token's COMMIT: %v", err)
		}
	}
	return c
}

// hold takes the lock, and release lets the held COMMIT go: committing, or
// failing when rollback is set.
func (c *commitHold) hold(t *testing.T) (release func(rollback bool)) {
	t.Helper()
	ctx := context.Background()
	conn, err := c.db.Conn(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_lock($1)`, c.key); err != nil {
		t.Fatal(err)
	}
	return func(rollback bool) {
		if _, err := c.db.ExecContext(ctx, `UPDATE test_token_commit SET rollback = $1`, rollback); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.ExecContext(ctx, `SELECT pg_advisory_unlock($1)`, c.key); err != nil {
			t.Fatal(err)
		}
		_ = conn.Close()
	}
}

// blocked answers whether a COMMIT waits on the lock.
func (c *commitHold) blocked(t *testing.T) func() bool {
	return func() bool {
		var n int
		if err := c.db.QueryRow(`SELECT count(*) FROM pg_locks
			WHERE locktype = 'advisory' AND NOT granted AND classid = 0 AND objid = $1`, c.key).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n > 0
	}
}

// proofWaitsOnTheLock waits until the database shows a proof's request for a
// repository's changelog lock waiting, read through the owner's connection,
// which sees every session's.
func proofWaitsOnTheLock(t *testing.T, ds *dataset) {
	t.Helper()
	owner := rawDB(t, ds)
	eventually(t, "the proof waits on the changelog lock", func() bool {
		var n int
		if err := owner.QueryRow(`SELECT count(*) FROM pg_stat_activity
			WHERE datname = current_database() AND wait_event_type = 'Lock' AND wait_event = 'advisory'
			  AND query LIKE '%pg_advisory_xact_lock(hashtext(current_schema()%'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n > 0
	})
}

// eventually waits for cond, failing the test after five seconds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("never: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// The rule (tokencache.go): from the moment a transaction that ends a token
// is about to commit until the database proves it committed or rolled back,
// the cache neither answers from nor stores a token of that repository. Each
// case drives the engine's own deletion of a token through its COMMIT.
func TestATokenCommitFencesTheCacheUntilItsOutcomeIsProved(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHeldCommits()
	ds := newRaceDataset(t, h.options()...)
	repo := ds.scope.Repository
	held := newCommitHold(t, ds)
	blocked := held.blocked(t)
	release := held.hold(t)
	mint := func(label string) (substrate.TokenInfo, string) {
		t.Helper()
		info, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: label})
		if err != nil {
			t.Fatalf("mint %s: %v", label, err)
		}
		return info, secret
	}
	authenticates := func(secret string) error {
		_, _, err := ds.svc.Authenticate(ctx, secret)
		return err
	}
	cached := func(secret string) bool { return ds.svc.tokens.cached(hashToken(secret)) }
	// standing holds a lookup of a live token read from the table and
	// nothing kept: what the fence is for.
	standing := func(when, secret string) {
		t.Helper()
		if err := authenticates(secret); err != nil {
			t.Fatalf("%s: a live token = %v, want it read from the table", when, err)
		}
		if !ds.svc.tokens.fenced(repo) || cached(secret) {
			t.Fatalf("%s: fenced %v, cached %v; want the repository fenced and nothing kept", when, ds.svc.tokens.fenced(repo), cached(secret))
		}
	}
	other, otherSecret := mint("another of the repository")
	_ = other

	// A COMMIT that lands late: Postgres holds it, its write is answered in
	// doubt, and it lands after.
	late, lateSecret := mint("revoked late")
	if err := authenticates(lateSecret); err != nil || !cached(lateSecret) {
		t.Fatalf("a token before its revoke: %v, cached %v", err, cached(lateSecret))
	}
	landed := make(chan error, 1)
	h.arm(func() { standing("before the COMMIT", lateSecret) }, func(commit func() error) error {
		go func() { landed <- commit() }()
		eventually(t, "the COMMIT waits in Postgres", blocked)
		return errAnswerLost
	})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, kindToken, late.ID, substrate.DeleteInput{}); !errors.Is(err, substrate.ErrWriteInDoubt) {
		t.Fatalf("a revoke whose answer was lost = %v, want ErrWriteInDoubt", err)
	}
	h.wasReached(t)
	standing("in doubt, the COMMIT still waiting", lateSecret)
	standing("in doubt, another token of the repository", otherSecret)
	// The waiter has asked the database for the lock, and waits on it,
	// before the COMMIT is let go: a proof that did not wait would lift the
	// fence here.
	proofWaitsOnTheLock(t, ds)
	standing("in doubt, the proof waiting on the lock", lateSecret)
	release(false)
	<-landed
	eventually(t, "the fence lifts once the late COMMIT is proved", func() bool { return !ds.svc.tokens.fenced(repo) })
	if err := authenticates(lateSecret); !errors.Is(err, substrate.ErrAuth) || cached(lateSecret) {
		t.Fatalf("after the late COMMIT: %v, cached %v; want ErrAuth and nothing kept", err, cached(lateSecret))
	}

	// A COMMIT answered in doubt that rolls back: once proved, the token
	// stands and is cached again.
	release = held.hold(t)
	kept, keptSecret := mint("revoked, rolled back")
	h.arm(func() {}, func(commit func() error) error {
		go func() { landed <- commit() }()
		eventually(t, "the COMMIT waits in Postgres", blocked)
		return errAnswerLost
	})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, kindToken, kept.ID, substrate.DeleteInput{}); !errors.Is(err, substrate.ErrWriteInDoubt) {
		t.Fatalf("a revoke whose answer was lost = %v, want ErrWriteInDoubt", err)
	}
	h.wasReached(t)
	standing("in doubt, before the rollback", keptSecret)
	release(true)
	<-landed
	eventually(t, "the fence lifts once the rollback is proved", func() bool { return !ds.svc.tokens.fenced(repo) })
	// Only now has the held COMMIT read the flag: its client's answer came
	// back long before.
	if _, err := held.db.Exec(`UPDATE test_token_commit SET rollback = false`); err != nil {
		t.Fatal(err)
	}
	if err := authenticates(keptSecret); err != nil || !cached(keptSecret) {
		t.Fatalf("after the rollback: %v, cached %v; want the token standing and cached", err, cached(keptSecret))
	}

	// The waiter that cannot get its proof keeps the fence, however long.
	h.failing.Store(true)
	failingFrom := h.attempts.Load()
	lost, _ := mint("revoked, proof failing")
	h.arm(func() {}, func(commit func() error) error {
		if err := commit(); err != nil {
			return err
		}
		return errAnswerLost
	})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, kindToken, lost.ID, substrate.DeleteInput{}); !errors.Is(err, substrate.ErrWriteInDoubt) {
		t.Fatalf("a revoke whose answer was lost = %v, want ErrWriteInDoubt", err)
	}
	h.wasReached(t)
	// Two more in doubt while the proof fails: one waiter carries the three.
	for _, label := range []string{"second in doubt", "third in doubt"} {
		more, _ := mint(label)
		h.arm(func() {}, func(commit func() error) error {
			if err := commit(); err != nil {
				return err
			}
			return errAnswerLost
		})
		if _, err := ds.Delete(ctx, substrate.ActorAPI, kindToken, more.ID, substrate.DeleteInput{}); !errors.Is(err, substrate.ErrWriteInDoubt) {
			t.Fatalf("a revoke whose answer was lost = %v, want ErrWriteInDoubt", err)
		}
		h.wasReached(t)
	}
	eventually(t, "three attempts at the proof fail", func() bool { return h.attempts.Load() >= failingFrom+3 })
	p := &ds.svc.proofs
	p.mu.Lock()
	pending, waiters := p.pending[repo], len(p.running)
	p.mu.Unlock()
	if pending != 3 || waiters != 1 {
		t.Fatalf("three commits in doubt hold %d fences under %d waiters, want 3 under 1", pending, waiters)
	}
	standing("after the proof failed", otherSecret)
	h.failing.Store(false)
	eventually(t, "the fence lifts once the proof comes", func() bool { return !ds.svc.tokens.fenced(repo) })
	if err := authenticates(otherSecret); err != nil || !cached(otherSecret) {
		t.Fatalf("after the proof: %v, cached %v; want the token standing and cached", err, cached(otherSecret))
	}

	// A COMMIT answered: no doubt, and the fence lifts as it answers.
	answered, answeredSecret := mint("revoked")
	h.arm(func() { standing("before an answered COMMIT", answeredSecret) }, func(commit func() error) error { return commit() })
	if err := ds.svc.RevokeToken(ctx, answeredSecret, ""); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	h.wasReached(t)
	_ = answered
	if ds.svc.tokens.fenced(repo) {
		t.Fatal("the fence stands after an answered COMMIT")
	}
	if err := authenticates(answeredSecret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("an answered revoke = %v, want ErrAuth", err)
	}
}

// A repository's deletion is under the rule too: its tokens' repository is
// fenced from before the erase's COMMIT until its outcome is proved.
func TestARepositoryErasedInDoubtFencesItsTokens(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHeldCommits()
	ds := newRaceDataset(t, h.options()...)
	repo := ds.scope.Repository
	_, secret := func() (substrate.TokenInfo, string) {
		info, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "of an erased repository"})
		if err != nil {
			t.Fatal(err)
		}
		return info, secret
	}()
	hash := hashToken(secret)
	if _, _, err := ds.svc.cachedTokenByHash(ctx, hash); err != nil || !ds.svc.tokens.cached(hash) {
		t.Fatalf("the token before the erase: %v, cached %v", err, ds.svc.tokens.cached(hash))
	}
	h.failing.Store(true)
	h.arm(func() {
		if _, _, err := ds.svc.cachedTokenByHash(ctx, hash); err != nil {
			t.Errorf("before the erase's COMMIT: %v, want the token read from the table", err)
		}
		if !ds.svc.tokens.fenced(repo) || ds.svc.tokens.cached(hash) {
			t.Errorf("before the erase's COMMIT: fenced %v, cached %v", ds.svc.tokens.fenced(repo), ds.svc.tokens.cached(hash))
		}
	}, func(commit func() error) error {
		if err := commit(); err != nil {
			return err
		}
		return errAnswerLost
	})
	if err := ds.svc.eraseRepository(ctx, repo); err == nil {
		t.Fatal("an erase whose answer was lost reported success")
	}
	h.wasReached(t)
	eventually(t, "an attempt at the proof fails", func() bool { return h.attempts.Load() >= 1 })
	if !ds.svc.tokens.fenced(repo) {
		t.Fatal("the fence lifted without its proof")
	}
	h.failing.Store(false)
	eventually(t, "the fence lifts once the proof comes", func() bool { return !ds.svc.tokens.fenced(repo) })
	if _, _, err := ds.svc.cachedTokenByHash(ctx, hash); !errors.Is(err, substrate.ErrAuth) || ds.svc.tokens.cached(hash) {
		t.Fatalf("after the erase: %v, cached %v; want ErrAuth and nothing kept", err, ds.svc.tokens.cached(hash))
	}
}

// An attempt whose wait runs out while the doubted COMMIT is still held
// proves nothing: the fence stands through several, and lifts after the
// COMMIT lands and an attempt is granted the lock.
func TestATokenProofThatTimesOutKeepsTheFence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := newHeldCommits()
	ds := newRaceDataset(t, append(h.options(), WithTestTokenProofAttempt(150*time.Millisecond))...)
	repo := ds.scope.Repository
	held := newCommitHold(t, ds)
	release := held.hold(t)
	info, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "revoked late"})
	if err != nil {
		t.Fatal(err)
	}
	other, otherSecret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "another"})
	if err != nil {
		t.Fatal(err)
	}
	_ = other
	landed := make(chan error, 1)
	h.arm(func() {}, func(commit func() error) error {
		go func() { landed <- commit() }()
		eventually(t, "the COMMIT waits in Postgres", held.blocked(t))
		return errAnswerLost
	})
	if _, err := ds.Delete(ctx, substrate.ActorAPI, kindToken, info.ID, substrate.DeleteInput{}); !errors.Is(err, substrate.ErrWriteInDoubt) {
		t.Fatalf("a revoke whose answer was lost = %v, want ErrWriteInDoubt", err)
	}
	h.wasReached(t)
	start := h.attempts.Load()
	eventually(t, "three attempts run out while the COMMIT is held", func() bool { return h.attempts.Load() >= start+3 })
	if !ds.svc.tokens.fenced(repo) {
		t.Fatal("the fence lifted on attempts that ran out")
	}
	if _, _, err := ds.svc.Authenticate(ctx, otherSecret); err != nil || ds.svc.tokens.cached(hashToken(otherSecret)) {
		t.Fatalf("a live token while the proof runs out: %v, cached %v; want it read and not kept", err, ds.svc.tokens.cached(hashToken(otherSecret)))
	}
	release(false)
	<-landed
	eventually(t, "the fence lifts once an attempt is granted the lock", func() bool { return !ds.svc.tokens.fenced(repo) })
	if _, _, err := ds.svc.Authenticate(ctx, secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("the late revoke = %v, want ErrAuth", err)
	}
}
