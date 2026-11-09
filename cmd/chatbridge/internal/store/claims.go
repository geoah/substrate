package store

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Claim is the bridge's durable promise about one action: which attempt
// carries it, at which version of the record, over which request. It is
// written before anything leaves for the network, so a restart finds it.
type Claim struct {
	Action  string
	Attempt string
	Version int64
	Digest  string
	// Claimed is false for an action that ended before a claim (expired or
	// cancelled while queued): its row only remembers the report.
	Claimed bool
	Network string
	Account string
	Kind    string
	// Request is the resolved request the attempt executes, as JSON.
	Request string
	// Txn is the transaction id the network sees.
	Txn          string
	ClaimedAt    time.Time
	DispatchedAt *time.Time
	Outcome      string
	OutcomeAt    *time.Time
	Message      string
	ErrorCode    string
	ErrorMessage string
}

// Claim writes c unless the action already has a row, and reports whether
// it did. Two workers racing for one action find out here: only one insert
// lands.
func (tx *Tx) Claim(ctx context.Context, c Claim) (bool, error) {
	res, err := tx.tx.ExecContext(ctx, `INSERT INTO claims (action, attempt, version, digest, claimed, network, account, kind, request, txn, claimed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT (action) DO NOTHING`,
		c.Action, c.Attempt, c.Version, c.Digest, boolInt(c.Claimed), c.Network, c.Account, c.Kind, c.Request, c.Txn, c.ClaimedAt.UnixMilli())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Dispatched records that the attempt is about to leave for the network. A
// claim without it never left, which a restart may rely on.
func (s *Store) Dispatched(ctx context.Context, action string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE claims SET dispatched_at = ? WHERE action = ?`, s.now().UnixMilli(), action)
	return err
}

// SetOutcome records how the attempt ended. It refuses to move an outcome
// already recorded, so a late report of the same attempt changes nothing.
func (tx *Tx) SetOutcome(ctx context.Context, action, outcome, message, code, text string) (bool, error) {
	res, err := tx.tx.ExecContext(ctx, `UPDATE claims SET outcome = ?, outcome_at = ?, result_message = ?, error_code = ?, error_message = ?
		WHERE action = ? AND outcome IS NULL`,
		outcome, tx.s.now().UnixMilli(), message, code, text, action)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

const claimColumns = `action, attempt, version, digest, claimed, network, account, kind, request, txn, claimed_at, dispatched_at,
	outcome, outcome_at, result_message, error_code, error_message`

func scanClaim(row interface{ Scan(...any) error }) (*Claim, error) {
	var c Claim
	var claimed int
	var claimedAt int64
	var dispatched, outcomeAt sql.NullInt64
	var outcome, message, code, text sql.NullString
	err := row.Scan(&c.Action, &c.Attempt, &c.Version, &c.Digest, &claimed, &c.Network, &c.Account, &c.Kind, &c.Request, &c.Txn,
		&claimedAt, &dispatched, &outcome, &outcomeAt, &message, &code, &text)
	if err != nil {
		return nil, err
	}
	c.Claimed = claimed == 1
	c.ClaimedAt = time.UnixMilli(claimedAt)
	if dispatched.Valid {
		t := time.UnixMilli(dispatched.Int64)
		c.DispatchedAt = &t
	}
	if outcomeAt.Valid {
		t := time.UnixMilli(outcomeAt.Int64)
		c.OutcomeAt = &t
	}
	c.Outcome, c.Message, c.ErrorCode, c.ErrorMessage = outcome.String, message.String, code.String, text.String
	return &c, nil
}

// GetClaim reads one action's claim, nil when it has none.
func (s *Store) GetClaim(ctx context.Context, action string) (*Claim, error) {
	c, err := scanClaim(s.db.QueryRowContext(ctx, `SELECT `+claimColumns+` FROM claims WHERE action = ?`, action))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// OpenClaims are the claims with no outcome: what a restart must settle.
func (s *Store) OpenClaims(ctx context.Context) ([]Claim, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+claimColumns+` FROM claims WHERE outcome IS NULL ORDER BY claimed_at`)
	if err != nil {
		return nil, err
	}
	var out []Claim
	for rows.Next() {
		c, err := scanClaim(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, *c)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	return out, rows.Err()
}

// RecordSend logs one send for the pace rules.
func (s *Store) RecordSend(ctx context.Context, account, chat, textDigest string, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO sends (account, chat, text_digest, at) VALUES (?, ?, ?, ?)`, account, chat, textDigest, at.UnixMilli())
	return err
}

// SendStats is what the pace rules read about past sends.
type SendStats struct {
	LastHour     int
	LastDay      int
	LastInChat   time.Time
	SameTextHour bool
}

// Sends reads the pace an account has kept, as of now.
func (s *Store) Sends(ctx context.Context, account, chat, textDigest string, now time.Time) (SendStats, error) {
	var st SendStats
	hour := now.Add(-time.Hour).UnixMilli()
	day := now.Add(-24 * time.Hour).UnixMilli()
	var last int64
	var same int
	err := s.db.QueryRowContext(ctx, `SELECT
			count(CASE WHEN at > ? THEN 1 END),
			count(*),
			coalesce(max(CASE WHEN chat = ? THEN at END), 0),
			count(CASE WHEN chat = ? AND text_digest = ? AND at > ? THEN 1 END)
		FROM sends WHERE account = ? AND at > ?`,
		hour, chat, chat, textDigest, hour, account, day).Scan(&st.LastHour, &st.LastDay, &last, &same)
	if err != nil {
		return st, err
	}
	if last > 0 {
		st.LastInChat = time.UnixMilli(last)
	}
	st.SameTextHour = same > 0
	// The log only matters for a day.
	_, err = s.db.ExecContext(ctx, `DELETE FROM sends WHERE at < ?`, day)
	return st, err
}
