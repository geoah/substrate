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
// Two shapes of operation write the row differently, and the difference is
// what a concurrent retry sees:
//
//   - A create, a merge and a split run in ONE transaction, so the row is
//     looked up and written inside that transaction (idempotentRecordTx).
//     inTx holds the repository's changelog lock from its start, so a
//     concurrent retry waits for the first attempt to commit and then finds
//     the settled row.
//   - A function or agent call runs its body OUTSIDE any transaction, so the
//     row is reserved first (settled_at NULL) and settled in the transaction
//     that applies the effects, or in a transaction of its own when there are
//     none. A concurrent retry finds the reservation and is refused with
//     ErrConflict; a failed attempt releases it so the retry runs again.
//
// The reservation carries a lease (idempotencyLease): a process that dies
// between reserving and settling leaves the row in flight, and the next
// attempt after the lease takes it over instead of waiting for the sweep.

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
	// idempotencyLease bounds an in-flight reservation. It must outlast the
	// longest callable: an agent's deadline caps at
	// vocabulary.MaxAgentDeadlineSec (600s), and a function body's runner
	// timeout is shorter.
	idempotencyLease = 15 * time.Minute
	// idempotencyOutcomeCap bounds the stored outcome. It is stated in
	// docs/api.md; change both. An outcome past it is not stored: the row
	// still settles, so the effect stays run-once, and the retry is told the
	// outcome was not retained.
	idempotencyOutcomeCap = 1 << 20
)

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

// idempotencyRow is one stored key as a lookup reads it.
type idempotencyRow struct {
	fingerprint string
	settled     bool
	// outcome is nil while in flight, and nil after settlement when the
	// answer exceeded idempotencyOutcomeCap.
	outcome []byte
}

// replay decodes the stored outcome into out, or says why the row cannot
// answer this request. Every refusal is ErrConflict: the key exists and this
// request is not the one that may use it.
func (r *idempotencyRow) replay(key, fingerprint string, out any) error {
	if r.fingerprint != fingerprint {
		return fmt.Errorf("%w: Idempotency-Key %q was already used with a different request", substrate.ErrConflict, key)
	}
	if !r.settled {
		return fmt.Errorf("%w: Idempotency-Key %q: the first request is still running; retry after it answers", substrate.ErrConflict, key)
	}
	if r.outcome == nil {
		return fmt.Errorf("%w: Idempotency-Key %q: the first request succeeded but its outcome exceeded the %d byte retention cap and was not stored",
			substrate.ErrConflict, key, idempotencyOutcomeCap)
	}
	if err := json.Unmarshal(r.outcome, out); err != nil {
		return fmt.Errorf("decode the stored outcome of Idempotency-Key %q: %w", key, err)
	}
	return nil
}

const idempotencySelectSQL = `SELECT fingerprint, settled_at IS NOT NULL, outcome
	FROM idempotency_keys WHERE operation = $1 AND key = $2`

// idempotencyUpsertSQL settles a key: it lands the settled row for a
// one-transaction operation and completes a callable's reservation alike.
const idempotencyUpsertSQL = `INSERT INTO idempotency_keys (operation, key, fingerprint, outcome, settled_at, expires_at)
	VALUES ($1, $2, $3, $4, $5, $6)
	ON CONFLICT (repository, operation, key) DO UPDATE
	   SET fingerprint = EXCLUDED.fingerprint, outcome = EXCLUDED.outcome,
	       settled_at = EXCLUDED.settled_at, expires_at = EXCLUDED.expires_at`

type rowQuerier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func idempotencyRead(ctx context.Context, q rowQuerier, op idempotentOp, key string) (*idempotencyRow, error) {
	var r idempotencyRow
	err := q.QueryRowContext(ctx, idempotencySelectSQL, string(op), key).Scan(&r.fingerprint, &r.settled, &r.outcome)
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

// idempotencySettle writes the settled row inside this transaction: it
// commits with the effect or rolls back with it.
func (t *txn) idempotencySettle(op idempotentOp, key, fingerprint string, outcome any) error {
	raw, err := idempotencyOutcome(outcome)
	if err != nil {
		return err
	}
	_, err = t.exec(idempotencyUpsertSQL, string(op), key, fingerprint, raw, t.now, t.now.Add(idempotencyRetention))
	if err != nil {
		return fmt.Errorf("settle idempotency key: %w", err)
	}
	return nil
}

// idempotentRecordTx is inTx for the one-transaction operations that answer a
// record. Without a key on ctx it is inTx exactly. With one, the transaction
// first looks the key up: a stored row answers (or refuses) without running
// fn, and a fresh key runs fn and settles the row in the same transaction.
func (ds *dataset) idempotentRecordTx(ctx context.Context, actor substrate.Actor, op idempotentOp, input any, fn func(t *txn) (*substrate.Record, error)) (*substrate.Record, error) {
	key, err := idempotencyKeyFrom(ctx)
	if err != nil {
		return nil, err
	}
	var fingerprint string
	if key != "" {
		if fingerprint, err = idempotencyFingerprint(input); err != nil {
			return nil, err
		}
	}
	var out *substrate.Record
	var stored *idempotencyRow
	err = ds.inTx(ctx, actor, false, func(t *txn) error {
		if key != "" {
			row, err := idempotencyRead(t.ctx, t.tx, op, key)
			if err != nil {
				return err
			}
			if row != nil {
				stored = row
				return nil
			}
		}
		e, err := fn(t)
		if err != nil {
			return err
		}
		out = e
		if key == "" {
			return nil
		}
		return t.idempotencySettle(op, key, fingerprint, e)
	})
	if err != nil {
		return nil, err
	}
	if stored != nil {
		var replayed substrate.Record
		if err := stored.replay(key, fingerprint, &replayed); err != nil {
			return nil, err
		}
		return &replayed, nil
	}
	return out, nil
}

// idempotentCall is a callable's reservation: the key it holds until the
// invocation settles or releases it. A nil *idempotentCall (no key on the
// request) accepts every method and does nothing.
type idempotentCall struct {
	ds          *dataset
	op          idempotentOp
	key         string
	fingerprint string
}

// beginIdempotent reserves the request's key for a callable, or answers the
// stored outcome. Three results: no key on ctx (nil, nil, nil); a stored row
// that answers with its outcome (nil, outcome, nil) or refuses (nil, nil,
// err); a reservation this invocation now owns (call, nil, nil).
func (ds *dataset) beginIdempotent(ctx context.Context, op idempotentOp, input any) (*idempotentCall, json.RawMessage, error) {
	key, err := idempotencyKeyFrom(ctx)
	if err != nil || key == "" {
		return nil, nil, err
	}
	fingerprint, err := idempotencyFingerprint(input)
	if err != nil {
		return nil, nil, err
	}
	call := &idempotentCall{ds: ds, op: op, key: key, fingerprint: fingerprint}
	// Two rounds: a reservation released between the refused insert and the
	// read is a row the second round takes.
	for range 2 {
		owned, err := call.reserve(ctx)
		if err != nil {
			return nil, nil, err
		}
		if owned {
			return call, nil, nil
		}
		row, err := idempotencyRead(ctx, ds.db, op, key)
		if err != nil {
			return nil, nil, err
		}
		if row == nil {
			continue
		}
		var outcome json.RawMessage
		if err := row.replay(key, fingerprint, &outcome); err != nil {
			return nil, nil, err
		}
		return nil, outcome, nil
	}
	return nil, nil, fmt.Errorf("%w: Idempotency-Key %q: another request holds it; retry", substrate.ErrConflict, key)
}

// reserve inserts the in-flight row, or takes over one whose lease lapsed.
// It runs outside every transaction and holds no lock afterwards, which keeps
// it clear of the global lock order (rows.go changelogLockKey).
func (c *idempotentCall) reserve(ctx context.Context) (bool, error) {
	now := nowUTC()
	var one bool
	err := c.ds.db.QueryRowContext(ctx, `
		INSERT INTO idempotency_keys (operation, key, fingerprint, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (repository, operation, key) DO UPDATE
		   SET fingerprint = EXCLUDED.fingerprint, created_at = EXCLUDED.created_at,
		       expires_at = EXCLUDED.expires_at
		 WHERE idempotency_keys.settled_at IS NULL AND idempotency_keys.expires_at < $4
		RETURNING true`,
		string(c.op), c.key, c.fingerprint, now, now.Add(idempotencyLease)).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserve idempotency key: %w", err)
	}
	return true, nil
}

// settleIn completes the reservation inside the transaction that applies the
// callable's effects.
func (c *idempotentCall) settleIn(t *txn, outcome any) error {
	if c == nil {
		return nil
	}
	return t.idempotencySettle(c.op, c.key, c.fingerprint, outcome)
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
	_, err = c.ds.db.ExecContext(context.WithoutCancel(ctx), idempotencyUpsertSQL,
		string(c.op), c.key, c.fingerprint, raw, now, now.Add(idempotencyRetention))
	if err != nil {
		return fmt.Errorf("settle idempotency key: %w", err)
	}
	return nil
}

// release drops the reservation after a failed attempt, so the retry runs
// the operation again. It runs on a context that survives the request's
// cancellation, because a client that timed out is the common cause.
func (c *idempotentCall) release(ctx context.Context) {
	if c == nil {
		return
	}
	if _, err := c.ds.db.ExecContext(context.WithoutCancel(ctx), `
		DELETE FROM idempotency_keys
		 WHERE operation = $1 AND key = $2 AND settled_at IS NULL`, string(c.op), c.key); err != nil {
		c.ds.svc.log.Warn("substrate: releasing an idempotency key after a failed attempt",
			"repository", c.ds.Repository().Name, "operation", string(c.op), "error", err)
	}
}

// sweepIdempotencyKeys deletes every key past its retention window, and every
// reservation past its lease. It runs from RunGC.
func (ds *dataset) sweepIdempotencyKeys(ctx context.Context, now time.Time) (int64, error) {
	res, err := ds.db.ExecContext(ctx, `DELETE FROM idempotency_keys WHERE expires_at < $1`, now)
	if err != nil {
		return 0, fmt.Errorf("sweep idempotency keys: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
