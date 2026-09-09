package engine

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/geoah/substrate/internal/substrate"
)

// The `Idempotency-Key` contract (docs/api.md "Idempotency and retries",
// migration 0023): a request that names a key runs its effect once, and the
// same key with the same input answers the first attempt's stored outcome for
// idempotencyRetention. The key store is the idempotency_keys table, one row
// per (repository, operation, key), and it never enters the changelog: a
// repository restored from its directory alone forgets every key.
//
// Every entry point CONSUMES the key: it reads it off the request context and
// runs its body on a context without one (substrate.WithoutIdempotencyKey),
// so a nested write, an agent's mutate tool or a function's effects, never
// mistakes the request's key for its own.
//
// A row lives until expires_at: the retention window once settled, the lease
// while in flight. Every read holds a row to that, so a row past it is dead
// whether or not the GC sweep has reclaimed the space, and the next attempt
// takes it over. Each attempt holds its row under a random owner token, and
// every write to the row is conditional on it, so an attempt whose lease
// lapsed can neither release nor overwrite the successor that took over.
//
// Two shapes of operation write the row differently, and the difference is
// what a concurrent retry sees:
//
//   - A create, a merge and a split run in ONE transaction. The key is read
//     first without a lock; a miss goes into the transaction, which reads it
//     again as the race guard and settles the row beside the effect
//     (idempotentRecordTx). inTx holds the repository's changelog lock from
//     its start, so a concurrent retry waits for the first attempt to commit
//     and then finds the settled row.
//   - A function or agent call runs its body OUTSIDE any transaction. The key
//     is read first without a lock; a miss reserves the row (settled_at NULL)
//     and settles it in the transaction that applies the effects, or in one
//     of its own when there are none. A concurrent retry finds the
//     reservation and is refused with ErrConflict; a failed attempt releases
//     it so the retry runs again.
//
// A reservation's lease starts short (idempotencyReserveLease, enough to
// resolve and admit the callable) and is extended to the invocation's own
// deadline plus slack once that is known (extendLease), so a longer deadline
// cannot outlive its lease. One process writes a repository at a time
// (repodir.go's directory lock), so every reservation without a thread is
// dead when the repository opens and is cleared then (clearDeadReservations);
// the lease is the backstop for a process that lives on.
//
// An agent call is the exception to the release. Its tool effects commit one
// transaction at a time before the thread settles, so once a thread exists
// the effect may already have run, and a second thread under the key would
// run it again. The reservation therefore records the thread id in the
// transaction that creates the thread (attachThread) and holds the row for
// the retention window from then on: a repeat, after a failure or a dead
// process alike, is ErrConflict naming the thread, and the client reads the
// thread and runs again under a new key.
//
// The lookup runs BEFORE the callable is resolved, admitted or its input
// validated: the key is scoped by the operation and the callable's name and
// the fingerprint covers the raw input, so a stored outcome answers even
// after the callable was disabled, uninstalled or redeclared.

// idempotentOp names the operation a key binds to. A key is the client's, so
// the same string under two operations is two keys.
type idempotentOp string

const (
	idemCreate       idempotentOp = "create"
	idemFunctionCall idempotentOp = "function.call"
	idemAgentCall    idempotentOp = "agent.call"
	idemMerge        idempotentOp = "merge"
	idemSplit        idempotentOp = "split"
)

const (
	// idempotencyRetention is how long a settled key answers. It is stated
	// in docs/api.md; change both.
	idempotencyRetention = 24 * time.Hour
	// idempotencyReserveLease is a fresh reservation's life: long enough to
	// resolve and admit the callable, after which extendLease sets the real
	// deadline.
	idempotencyReserveLease = 2 * time.Minute
	// idempotencyLeaseSlack is what a reservation outlives its invocation's
	// deadline by: the effects transaction and the settle after the body's
	// clock runs out.
	idempotencyLeaseSlack = time.Minute
	// idempotencyProvisionBudget is what a function body may spend before
	// its own timeout starts: a PEP 723 body whose uv cache was evicted
	// re-resolves for up to internal/runner's uvProvisionTimeout (120s). The
	// lease covers it so a slow provision cannot hand the key to a retry.
	idempotencyProvisionBudget = 120 * time.Second
)

// idempotencyOutcomeCap bounds the stored outcome. It is stated in
// docs/api.md; change both. An outcome past it is not stored: the row still
// settles, so the effect stays run-once, and the retry is told the outcome
// was not retained and, for a record, where the record is. A variable only so
// a test can lower it; nothing else writes it.
var idempotencyOutcomeCap = 1 << 20

// idempotencyKeyFrom reads the request's key off ctx, validated. Empty means
// the request carried none and the operation runs as it always did.
func idempotencyKeyFrom(ctx context.Context) (string, error) {
	key := substrate.IdempotencyKeyFrom(ctx)
	if len(key) > substrate.MaxIdempotencyKeyLength {
		return "", fmt.Errorf("%w: Idempotency-Key is %d bytes; the limit is %d",
			substrate.ErrValidation, len(key), substrate.MaxIdempotencyKeyLength)
	}
	return key, nil
}

// idempotencyFingerprint is the SHA-256 of the input's JSON. encoding/json
// writes map keys sorted, so two decodings of one request body fingerprint
// alike.
func idempotencyFingerprint(input any) (string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return "", fmt.Errorf("fingerprint the request: %w", err)
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

// idempotencyRow is one live stored key as a lookup reads it.
type idempotencyRow struct {
	fingerprint string
	settled     bool
	// outcome is nil while in flight, and nil after settlement when the
	// answer exceeded idempotencyOutcomeCap.
	outcome []byte
	// thread is the agent thread an in-flight agent call opened; empty for
	// every other row.
	thread string
	// locator names what a dropped outcome was about, so the refusal can
	// point the client at it: the record path of a created, merged or split
	// record whose outcome exceeded the cap. Empty otherwise.
	locator string
}

// replay decodes the stored outcome into out, or says why the row cannot
// answer this request. Every refusal is ErrConflict: the key exists and this
// request is not the one that may use it.
func (r *idempotencyRow) replay(key, fingerprint string, out any) error {
	if r.fingerprint != fingerprint {
		return fmt.Errorf("%w: Idempotency-Key %q was already used with a different request", substrate.ErrConflict, key)
	}
	if !r.settled {
		if r.thread != "" {
			return fmt.Errorf("%w: Idempotency-Key %q: the first request opened agent thread %s and has not settled; read the thread, and run again under a new key",
				substrate.ErrConflict, key, r.thread)
		}
		return fmt.Errorf("%w: Idempotency-Key %q: the first request is still running; retry after it answers", substrate.ErrConflict, key)
	}
	if r.outcome == nil {
		if r.locator != "" {
			return fmt.Errorf("%w: Idempotency-Key %q: the first request succeeded and wrote %s, but its outcome exceeded the %d byte retention cap and was not stored; read the record",
				substrate.ErrConflict, key, r.locator, idempotencyOutcomeCap)
		}
		return fmt.Errorf("%w: Idempotency-Key %q: the first request succeeded but its outcome exceeded the %d byte retention cap and was not stored",
			substrate.ErrConflict, key, idempotencyOutcomeCap)
	}
	if err := json.Unmarshal(r.outcome, out); err != nil {
		return fmt.Errorf("decode the stored outcome of Idempotency-Key %q: %w", key, err)
	}
	return nil
}

// idempotencySelectSQL reads the one LIVE row for a key: a row past its
// expires_at is dead and reads as absent, whether or not the sweep has
// reclaimed it yet.
const idempotencySelectSQL = `SELECT fingerprint, settled_at IS NOT NULL, outcome, coalesce(thread, ''), coalesce(locator, '')
	FROM idempotency_keys WHERE operation = $1 AND key = $2 AND expires_at > $3`

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func idempotencyRead(ctx context.Context, q rowQuerier, op idempotentOp, key string, now time.Time) (*idempotencyRow, error) {
	var r idempotencyRow
	err := q.QueryRowContext(ctx, idempotencySelectSQL, string(op), key, now).Scan(&r.fingerprint, &r.settled, &r.outcome, &r.thread, &r.locator)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read idempotency key: %w", err)
	}
	return &r, nil
}

// idempotencyOutcome encodes an outcome for storage: nil past the cap.
func idempotencyOutcome(outcome any) ([]byte, error) {
	raw, err := json.Marshal(outcome)
	if err != nil {
		return nil, fmt.Errorf("encode the outcome: %w", err)
	}
	if len(raw) > idempotencyOutcomeCap {
		return nil, nil
	}
	return raw, nil
}

// newOwner mints an attempt's owner token.
func newOwner() (string, error) {
	return newID()
}

// idempotentRecordTx is inTx for the one-transaction operations that answer a
// record. Without a key on ctx it is inTx exactly. With one, a live stored row
// answers (or refuses) before any lock is taken; a miss goes into the
// transaction, which reads the key again as the race guard, runs fn and
// settles the row in the same transaction, so the row commits with the effect
// and never without it.
func (ds *dataset) idempotentRecordTx(ctx context.Context, actor substrate.Actor, op idempotentOp, input any, fn func(t *txn) (*substrate.Record, error)) (*substrate.Record, error) {
	key, err := idempotencyKeyFrom(ctx)
	if err != nil {
		return nil, err
	}
	if key == "" {
		var out *substrate.Record
		err := ds.inTx(ctx, actor, false, func(t *txn) error {
			e, err := fn(t)
			out = e
			return err
		})
		if err != nil {
			return nil, err
		}
		return out, nil
	}
	// The key is consumed here: whatever fn writes runs without one.
	ctx = substrate.WithoutIdempotencyKey(ctx)
	fingerprint, err := idempotencyFingerprint(input)
	if err != nil {
		return nil, err
	}
	answer := func(row *idempotencyRow) (*substrate.Record, error) {
		var replayed substrate.Record
		if err := row.replay(key, fingerprint, &replayed); err != nil {
			return nil, err
		}
		return &replayed, nil
	}
	if row, err := idempotencyRead(ctx, ds.db, op, key, nowUTC()); err != nil {
		return nil, err
	} else if row != nil {
		return answer(row)
	}
	owner, err := newOwner()
	if err != nil {
		return nil, err
	}
	var out *substrate.Record
	var stored *idempotencyRow
	err = ds.inTx(ctx, actor, false, func(t *txn) error {
		row, err := idempotencyRead(t.ctx, t.tx, op, key, t.now)
		if err != nil {
			return err
		}
		if row != nil {
			stored = row
			return nil
		}
		e, err := fn(t)
		if err != nil {
			return err
		}
		out = e
		raw, err := idempotencyOutcome(e)
		if err != nil {
			return err
		}
		// A dropped outcome still names its record, so the refusal a repeat
		// gets says where to read it.
		var locator *string
		if raw == nil {
			path := e.Kind + "/" + e.ID
			locator = &path
		}
		// The whole row in one statement: a dead row under the key (past its
		// expires_at) is taken over, a live one cannot exist past the read
		// above, and zero rows means one appeared anyway, which fails the
		// write rather than settle over it.
		res, err := t.exec(`
			INSERT INTO idempotency_keys (operation, key, fingerprint, owner, outcome, locator, settled_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (repository, operation, key) DO UPDATE
			   SET fingerprint = EXCLUDED.fingerprint, owner = EXCLUDED.owner, outcome = EXCLUDED.outcome,
			       locator = EXCLUDED.locator, thread = NULL, created_at = EXCLUDED.created_at,
			       settled_at = EXCLUDED.settled_at, expires_at = EXCLUDED.expires_at
			 WHERE idempotency_keys.expires_at <= $7`,
			string(op), key, fingerprint, owner, raw, locator, t.now, t.now.Add(idempotencyRetention))
		if err != nil {
			return fmt.Errorf("settle idempotency key: %w", err)
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return fmt.Errorf("%w: Idempotency-Key %q: another request took it while this one ran", substrate.ErrConflict, key)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if stored != nil {
		return answer(stored)
	}
	return out, nil
}

// idempotentCall is a callable's reservation: the row it holds, under its
// owner token, until the invocation settles or releases it. A nil
// *idempotentCall (no key on the request) accepts every method and does
// nothing.
type idempotentCall struct {
	ds          *dataset
	op          idempotentOp
	key         string
	fingerprint string
	owner       string
}

// beginIdempotent reserves the request's key for a callable, or answers the
// stored outcome. Three results: no key on ctx (nil, nil, nil); a stored row
// that answers with its outcome (nil, outcome, nil) or refuses (nil, nil,
// err); a reservation this invocation now owns (call, nil, nil). The read
// comes first and takes no lock: a settled key replays without touching the
// row, and only a miss (or a dead row) reaches the reserving INSERT.
func (ds *dataset) beginIdempotent(ctx context.Context, op idempotentOp, input any) (*idempotentCall, json.RawMessage, error) {
	key, err := idempotencyKeyFrom(ctx)
	if err != nil || key == "" {
		return nil, nil, err
	}
	fingerprint, err := idempotencyFingerprint(input)
	if err != nil {
		return nil, nil, err
	}
	owner, err := newOwner()
	if err != nil {
		return nil, nil, err
	}
	call := &idempotentCall{ds: ds, op: op, key: key, fingerprint: fingerprint, owner: owner}
	// Two rounds: a row that appears between the miss and the INSERT is read
	// on the second round, and a row released between the read and the
	// INSERT is taken on the second.
	for range 2 {
		row, err := idempotencyRead(ctx, ds.db, op, key, nowUTC())
		if err != nil {
			return nil, nil, err
		}
		if row != nil {
			var outcome json.RawMessage
			if err := row.replay(key, fingerprint, &outcome); err != nil {
				return nil, nil, err
			}
			return nil, outcome, nil
		}
		owned, err := call.reserve(ctx)
		if err != nil {
			return nil, nil, err
		}
		if owned {
			return call, nil, nil
		}
	}
	return nil, nil, fmt.Errorf("%w: Idempotency-Key %q: another request holds it; retry", substrate.ErrConflict, key)
}

// reserve inserts the in-flight row under this attempt's owner, or takes over
// a dead one (past its expires_at, settled or not). It runs outside every
// transaction and holds no lock afterwards, which keeps it clear of the
// global lock order (rows.go changelogLockKey).
func (c *idempotentCall) reserve(ctx context.Context) (bool, error) {
	now := nowUTC()
	var one bool
	err := c.ds.db.QueryRowContext(ctx, `
		INSERT INTO idempotency_keys (operation, key, fingerprint, owner, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (repository, operation, key) DO UPDATE
		   SET fingerprint = EXCLUDED.fingerprint, owner = EXCLUDED.owner, outcome = NULL,
		       locator = NULL, thread = NULL, settled_at = NULL, created_at = EXCLUDED.created_at,
		       expires_at = EXCLUDED.expires_at
		 WHERE idempotency_keys.expires_at <= $5
		RETURNING true`,
		string(c.op), c.key, c.fingerprint, c.owner, now, now.Add(idempotencyReserveLease)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserve idempotency key: %w", err)
	}
	return true, nil
}

// lost is the refusal every conditional write answers with when the row is
// no longer this attempt's: its lease lapsed and another attempt took over.
func (c *idempotentCall) lost() error {
	return fmt.Errorf("%w: Idempotency-Key %q: this attempt's reservation lapsed and another request took it over", substrate.ErrConflict, c.key)
}

// extendLease moves the reservation's life out to the invocation's own
// deadline plus slack, once the callable is resolved and the deadline known.
// It only ever lengthens the lease. A row this attempt no longer owns is a
// refusal, before the body runs.
func (c *idempotentCall) extendLease(ctx context.Context, deadline time.Time) error {
	if c == nil {
		return nil
	}
	res, err := c.ds.db.ExecContext(ctx, `
		UPDATE idempotency_keys SET expires_at = GREATEST(expires_at, $4)
		 WHERE operation = $1 AND key = $2 AND owner = $3 AND settled_at IS NULL AND thread IS NULL`,
		string(c.op), c.key, c.owner, deadline.Add(idempotencyLeaseSlack))
	if err != nil {
		return fmt.Errorf("extend the idempotency key's lease: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return c.lost()
	}
	return nil
}

// attachThread records the agent thread the reservation's attempt opened, in
// the transaction that creates the thread. From here the row answers for the
// full retention window whether or not the attempt settles: a repeat is
// pointed at the thread (replay), and release leaves it alone. A row this
// attempt no longer owns fails the transaction, so the thread is never
// created.
func (c *idempotentCall) attachThread(t *txn, threadID string) error {
	if c == nil {
		return nil
	}
	res, err := t.exec(`
		UPDATE idempotency_keys SET thread = $4, expires_at = $5
		 WHERE operation = $1 AND key = $2 AND owner = $3 AND settled_at IS NULL`,
		string(c.op), c.key, c.owner, threadID, t.now.Add(idempotencyRetention))
	if err != nil {
		return fmt.Errorf("attach the thread to the idempotency key: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return c.lost()
	}
	return nil
}

// idempotencySettleSQL completes this attempt's reservation and nobody
// else's: a stale attempt neither overwrites a successor's reservation nor a
// newer settled outcome.
const idempotencySettleSQL = `UPDATE idempotency_keys
	   SET outcome = $4, settled_at = $5, expires_at = $6
	 WHERE operation = $1 AND key = $2 AND owner = $3 AND settled_at IS NULL`

// settleIn completes the reservation inside the transaction that applies the
// callable's effects. A row this attempt no longer owns fails the
// transaction, so the effects roll back rather than land twice.
func (c *idempotentCall) settleIn(t *txn, outcome any) error {
	if c == nil {
		return nil
	}
	raw, err := idempotencyOutcome(outcome)
	if err != nil {
		return err
	}
	res, err := t.exec(idempotencySettleSQL, string(c.op), c.key, c.owner, raw, t.now, t.now.Add(idempotencyRetention))
	if err != nil {
		return fmt.Errorf("settle idempotency key: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return c.lost()
	}
	return nil
}

// settle completes the reservation in a transaction of its own, for an
// invocation that applied no effects. The request's cancellation is dropped:
// the body has run, and a client that gave up is the one that retries.
func (c *idempotentCall) settle(ctx context.Context, outcome any) error {
	if c == nil {
		return nil
	}
	raw, err := idempotencyOutcome(outcome)
	if err != nil {
		return err
	}
	now := nowUTC()
	res, err := c.ds.db.ExecContext(context.WithoutCancel(ctx), idempotencySettleSQL,
		string(c.op), c.key, c.owner, raw, now, now.Add(idempotencyRetention))
	if err != nil {
		return fmt.Errorf("settle idempotency key: %w", err)
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return c.lost()
	}
	return nil
}

// release drops this attempt's reservation after a failed attempt, so the
// retry runs the operation again. A reservation that opened a thread stays
// (its effects may have committed), and a row another attempt took over is
// not this one's to drop. It runs on a context that survives the request's
// cancellation, because a client that timed out is the common cause.
func (c *idempotentCall) release(ctx context.Context) {
	if c == nil {
		return
	}
	if _, err := c.ds.db.ExecContext(context.WithoutCancel(ctx), `
		DELETE FROM idempotency_keys
		 WHERE operation = $1 AND key = $2 AND owner = $3 AND settled_at IS NULL AND thread IS NULL`,
		string(c.op), c.key, c.owner); err != nil {
		c.ds.svc.log.Warn("substrate: releasing an idempotency key after a failed attempt",
			"repository", c.ds.Repository().Name, "operation", string(c.op), "error", err)
	}
}

// clearDeadReservations runs once when the repository opens. One process
// writes a repository at a time, so every reservation still in flight
// belongs to a process that is gone; a thread-bound one stays, because its
// thread's effects may have committed and it must keep pointing there.
func (ds *dataset) clearDeadReservations(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	res, err := ds.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE settled_at IS NULL AND thread IS NULL`)
	if err != nil {
		return fmt.Errorf("clear dead idempotency reservations: %w", err)
	}
	if n, _ := res.RowsAffected(); n > 0 {
		ds.svc.log.Info("substrate: cleared idempotency reservations of a previous process", "repository", ds.scope.Repository, "rows", n)
	}
	return nil
}

// sweepIdempotencyKeys reclaims the space of every row past its expires_at.
// Reads already treat such a row as absent, so the sweep changes no answer.
// It runs from RunGC.
func (ds *dataset) sweepIdempotencyKeys(ctx context.Context, now time.Time) (int64, error) {
	res, err := ds.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at <= $1`, now)
	if err != nil {
		return 0, fmt.Errorf("sweep idempotency keys: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
