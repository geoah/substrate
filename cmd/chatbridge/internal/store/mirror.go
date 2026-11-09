package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
)

// Room is one portal as the local Matrix side keeps it: which network,
// chat and login it is, and the owner's own flags, which arrive through the
// owner's intent rather than the portal.
type Room struct {
	RoomID       string
	Network      string
	PortalID     string
	Receiver     string
	Pinned       *bool
	Archived     *bool
	MarkedUnread *bool
	// MutedUntil is Unix milliseconds; 0 is not muted.
	MutedUntil *int64
	Deleted    bool
}

// PutRoom records a room, keeping the flags it already has.
func (tx *Tx) PutRoom(ctx context.Context, r Room) error {
	_, err := tx.tx.ExecContext(ctx, `INSERT INTO rooms (room_id, network, portal_id, receiver) VALUES (?, ?, ?, ?)
		ON CONFLICT (room_id) DO UPDATE SET network = excluded.network, portal_id = excluded.portal_id,
			receiver = excluded.receiver, deleted = 0`,
		r.RoomID, r.Network, r.PortalID, r.Receiver)
	return err
}

// Room reads one room, nil when unknown.
func (tx *Tx) Room(ctx context.Context, roomID string) (*Room, error) {
	var r Room
	var pinned, archived, unread, muted sql.NullInt64
	var deleted int
	err := tx.tx.QueryRowContext(ctx, `SELECT room_id, network, portal_id, receiver, pinned, archived, marked_unread, muted_until, deleted
		FROM rooms WHERE room_id = ?`, roomID).
		Scan(&r.RoomID, &r.Network, &r.PortalID, &r.Receiver, &pinned, &archived, &unread, &muted, &deleted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Pinned, r.Archived, r.MarkedUnread = nullBool(pinned), nullBool(archived), nullBool(unread)
	r.MutedUntil = nullInt(muted)
	r.Deleted = deleted == 1
	return &r, nil
}

func nullBool(v sql.NullInt64) *bool {
	if !v.Valid {
		return nil
	}
	b := v.Int64 == 1
	return &b
}

// The owner's flags on a room.
const (
	FlagPinned       = "pinned"
	FlagArchived     = "archived"
	FlagMarkedUnread = "marked_unread"
	FlagMutedUntil   = "muted_until"
)

// SetRoomFlag writes one of the owner's flags and reports whether it
// changed.
func (tx *Tx) SetRoomFlag(ctx context.Context, roomID, flag string, value int64) (bool, error) {
	switch flag {
	case FlagPinned, FlagArchived, FlagMarkedUnread, FlagMutedUntil:
	default:
		return false, fmt.Errorf("no room flag %q", flag)
	}
	res, err := tx.tx.ExecContext(ctx, `UPDATE rooms SET `+flag+` = ? WHERE room_id = ? AND (`+flag+` IS NULL OR `+flag+` != ?)`,
		value, roomID, value)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SetRoomDeleted marks a room deleted, or live again.
func (tx *Tx) SetRoomDeleted(ctx context.Context, roomID string, deleted bool) error {
	_, err := tx.tx.ExecContext(ctx, `UPDATE rooms SET deleted = ? WHERE room_id = ?`, boolInt(deleted), roomID)
	return err
}

// MemberRow is one member of a room.
type MemberRow struct {
	MXID       string
	UserID     string
	Membership string
	Power      int
}

// SetMember writes a member's membership and reports whether it changed.
func (tx *Tx) SetMember(ctx context.Context, roomID, mxid, userID, membership string) (bool, error) {
	res, err := tx.tx.ExecContext(ctx, `INSERT INTO members (room_id, mxid, user_id, membership) VALUES (?, ?, ?, ?)
		ON CONFLICT (room_id, mxid) DO UPDATE SET membership = excluded.membership, user_id = excluded.user_id
		WHERE members.membership != excluded.membership`,
		roomID, mxid, userID, membership)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// SetPower writes a member's power level and reports whether it changed. A
// member not yet known is not created.
func (tx *Tx) SetPower(ctx context.Context, roomID, mxid string, power int) (bool, error) {
	res, err := tx.tx.ExecContext(ctx, `UPDATE members SET power = ? WHERE room_id = ? AND mxid = ? AND power != ?`,
		power, roomID, mxid, power)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

// Members reads a room's members.
func (tx *Tx) Members(ctx context.Context, roomID string) ([]MemberRow, error) {
	rows, err := tx.tx.QueryContext(ctx, `SELECT mxid, user_id, membership, power FROM members WHERE room_id = ? ORDER BY mxid`, roomID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemberRow
	for rows.Next() {
		var m MemberRow
		if err := rows.Scan(&m.MXID, &m.UserID, &m.Membership, &m.Power); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Part is one bridgev2 message part as the local Matrix side received it.
// Content is the part's own fold, as JSON the local Matrix side defines.
type Part struct {
	EventID   string
	RoomID    string
	MessageID string
	PartID    string
	Index     int
	Sender    string
	FromOwner bool
	// TS is Unix milliseconds.
	TS        int64
	Content   string
	EditCount int
	EditedAt  *int64
	Deleted   string
	Action    string
}

const partColumns = `event_id, room_id, message_id, part_id, idx, sender, from_owner, ts, content, edit_count, edited_at, deleted, action`

func scanPart(row interface{ Scan(...any) error }) (*Part, error) {
	var p Part
	var fromOwner int
	var editedAt sql.NullInt64
	err := row.Scan(&p.EventID, &p.RoomID, &p.MessageID, &p.PartID, &p.Index, &p.Sender, &fromOwner, &p.TS, &p.Content,
		&p.EditCount, &editedAt, &p.Deleted, &p.Action)
	if err != nil {
		return nil, err
	}
	p.FromOwner = fromOwner == 1
	p.EditedAt = nullInt(editedAt)
	return &p, nil
}

// PutPart writes a part. A part already held keeps its edits and deletion
// against an older copy, so a history page cannot undo a live edit.
func (tx *Tx) PutPart(ctx context.Context, p Part) error {
	var existing int
	err := tx.tx.QueryRowContext(ctx, `SELECT edit_count FROM parts WHERE room_id = ? AND message_id = ? AND part_id = ?`,
		p.RoomID, p.MessageID, p.PartID).Scan(&existing)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.tx.ExecContext(ctx, `INSERT INTO parts (`+partColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT (event_id) DO NOTHING`,
			p.EventID, p.RoomID, p.MessageID, p.PartID, p.Index, p.Sender, boolInt(p.FromOwner), p.TS, p.Content,
			p.EditCount, p.EditedAt, p.Deleted, p.Action)
		return err
	case err != nil:
		return err
	}
	if p.EditCount < existing {
		return nil
	}
	_, err = tx.tx.ExecContext(ctx, `UPDATE parts SET event_id = ?, content = ?, edit_count = ?, edited_at = coalesce(?, edited_at),
			action = CASE WHEN ? != '' THEN ? ELSE action END
		WHERE room_id = ? AND message_id = ? AND part_id = ?`,
		p.EventID, p.Content, p.EditCount, p.EditedAt, p.Action, p.Action, p.RoomID, p.MessageID, p.PartID)
	return err
}

// EditPart replaces a part's content with an edit and returns the new edit
// count.
func (tx *Tx) EditPart(ctx context.Context, roomID, messageID, partID, content string, editedAt int64) (int, error) {
	res, err := tx.tx.ExecContext(ctx, `UPDATE parts SET content = ?, edit_count = edit_count + 1, edited_at = ?
		WHERE room_id = ? AND message_id = ? AND part_id = ?`, content, editedAt, roomID, messageID, partID)
	if err != nil {
		return 0, err
	}
	if n, err := res.RowsAffected(); err != nil || n == 0 {
		return 0, err
	}
	var count int
	err = tx.tx.QueryRowContext(ctx, `SELECT edit_count FROM parts WHERE room_id = ? AND message_id = ? AND part_id = ?`,
		roomID, messageID, partID).Scan(&count)
	return count, err
}

// PartByEvent reads the part an event id names, nil when unknown.
func (tx *Tx) PartByEvent(ctx context.Context, eventID string) (*Part, error) {
	p, err := scanPart(tx.tx.QueryRowContext(ctx, `SELECT `+partColumns+` FROM parts WHERE event_id = ?`, eventID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// MessageParts reads every part of one message, in order.
func (tx *Tx) MessageParts(ctx context.Context, roomID, messageID string) ([]Part, error) {
	rows, err := tx.tx.QueryContext(ctx, `SELECT `+partColumns+` FROM parts WHERE room_id = ? AND message_id = ? ORDER BY idx, part_id`,
		roomID, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Part
	for rows.Next() {
		p, err := scanPart(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// MarkMessageDeleted marks every part of a message deleted and reports
// whether it was live.
func (tx *Tx) MarkMessageDeleted(ctx context.Context, roomID, messageID, scope string) (bool, error) {
	res, err := tx.tx.ExecContext(ctx, `UPDATE parts SET deleted = ? WHERE room_id = ? AND message_id = ? AND deleted != 'everyone'
		AND deleted != ?`, scope, roomID, messageID, scope)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// NewestPart is the newest live part of a room at or before ts (Unix
// milliseconds, 0 for any), nil for none.
func (tx *Tx) NewestPart(ctx context.Context, roomID string, ts int64) (*Part, error) {
	q := `SELECT ` + partColumns + ` FROM parts WHERE room_id = ? AND deleted = ''`
	args := []any{roomID}
	if ts > 0 {
		q += ` AND ts <= ?`
		args = append(args, ts)
	}
	p, err := scanPart(tx.tx.QueryRowContext(ctx, q+` ORDER BY ts DESC, idx DESC LIMIT 1`, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// OwnerSent reports whether the owner has sent anything in a room.
func (tx *Tx) OwnerSent(ctx context.Context, roomID string) (bool, error) {
	var n int
	err := tx.tx.QueryRowContext(ctx, `SELECT count(*) FROM parts WHERE room_id = ? AND from_owner = 1`, roomID).Scan(&n)
	return n > 0, err
}

// ReactionRow is one reaction a room holds.
type ReactionRow struct {
	EventID   string
	RoomID    string
	MessageID string
	Sender    string
	EmojiID   string
	Key       string
	TS        int64
}

// PutReaction writes a reaction; one sender's reaction with one emoji is
// one row, whichever event carried it.
func (tx *Tx) PutReaction(ctx context.Context, r ReactionRow) error {
	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM reactions WHERE room_id = ? AND message_id = ? AND sender = ? AND emoji_id = ? AND reaction_key = ?
		AND event_id != ?`, r.RoomID, r.MessageID, r.Sender, r.EmojiID, r.Key, r.EventID); err != nil {
		return err
	}
	_, err := tx.tx.ExecContext(ctx, `INSERT INTO reactions (event_id, room_id, message_id, sender, emoji_id, reaction_key, ts)
		VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT (event_id) DO UPDATE SET reaction_key = excluded.reaction_key, ts = excluded.ts`,
		r.EventID, r.RoomID, r.MessageID, r.Sender, r.EmojiID, r.Key, r.TS)
	return err
}

// ReactionByEvent reads the reaction an event id names, nil when unknown.
func (tx *Tx) ReactionByEvent(ctx context.Context, eventID string) (*ReactionRow, error) {
	var r ReactionRow
	err := tx.tx.QueryRowContext(ctx, `SELECT event_id, room_id, message_id, sender, emoji_id, reaction_key, ts FROM reactions WHERE event_id = ?`, eventID).
		Scan(&r.EventID, &r.RoomID, &r.MessageID, &r.Sender, &r.EmojiID, &r.Key, &r.TS)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// DeleteReaction removes a reaction.
func (tx *Tx) DeleteReaction(ctx context.Context, eventID string) error {
	_, err := tx.tx.ExecContext(ctx, `DELETE FROM reactions WHERE event_id = ?`, eventID)
	return err
}

// DeleteReactionBy removes one sender's reaction with one key.
func (tx *Tx) DeleteReactionBy(ctx context.Context, roomID, messageID, sender, key string) (bool, error) {
	res, err := tx.tx.ExecContext(ctx, `DELETE FROM reactions WHERE room_id = ? AND message_id = ? AND sender = ? AND reaction_key = ?`,
		roomID, messageID, sender, key)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// MessageReactions reads the reactions one message holds, oldest first.
func (tx *Tx) MessageReactions(ctx context.Context, roomID, messageID string) ([]ReactionRow, error) {
	rows, err := tx.tx.QueryContext(ctx, `SELECT event_id, room_id, message_id, sender, emoji_id, reaction_key, ts FROM reactions
		WHERE room_id = ? AND message_id = ? ORDER BY ts, sender, reaction_key`, roomID, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ReactionRow
	for rows.Next() {
		var r ReactionRow
		if err := rows.Scan(&r.EventID, &r.RoomID, &r.MessageID, &r.Sender, &r.EmojiID, &r.Key, &r.TS); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UserDigest reads the digest of the profile last sent for a user in one
// account, and whether one was sent at all.
func (tx *Tx) UserDigest(ctx context.Context, network, account, userID string) (string, bool, error) {
	var d string
	err := tx.tx.QueryRowContext(ctx, `SELECT digest FROM users_seen WHERE network = ? AND account = ? AND user_id = ?`,
		network, account, userID).Scan(&d)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return d, err == nil, err
}

// SetUserDigest records the profile sent for a user in one account.
func (tx *Tx) SetUserDigest(ctx context.Context, network, account, userID, digest string) error {
	_, err := tx.tx.ExecContext(ctx, `INSERT INTO users_seen (network, account, user_id, digest) VALUES (?, ?, ?, ?)
		ON CONFLICT (network, account, user_id) DO UPDATE SET digest = excluded.digest`, network, account, userID, digest)
	return err
}

// AccountsSeeing are the accounts a user was sent to.
func (tx *Tx) AccountsSeeing(ctx context.Context, network, userID string) ([]string, error) {
	rows, err := tx.tx.QueryContext(ctx, `SELECT account FROM users_seen WHERE network = ? AND user_id = ? ORDER BY account`, network, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// RoomByPortal reads the room of one portal, nil when unknown.
func (tx *Tx) RoomByPortal(ctx context.Context, network, portalID, receiver string) (*Room, error) {
	var roomID string
	err := tx.tx.QueryRowContext(ctx, `SELECT room_id FROM rooms WHERE network = ? AND portal_id = ? AND receiver = ?`,
		network, portalID, receiver).Scan(&roomID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return tx.Room(ctx, roomID)
}

// View runs fn in a transaction that only reads.
func (s *Store) View(ctx context.Context, fn func(*Tx) error) error {
	return s.Do(ctx, fn)
}

// Ghost is one network user's profile as bridgev2 pushed it.
type Ghost struct {
	Network     string
	UserID      string
	Name        string
	Avatar      string
	Identifiers []string
	IsBot       bool
}

// Ghost reads a user's profile, nil when none was pushed.
func (tx *Tx) Ghost(ctx context.Context, network, userID string) (*Ghost, error) {
	var g Ghost
	var idents string
	var isBot int
	err := tx.tx.QueryRowContext(ctx, `SELECT network, user_id, name, avatar, identifiers, is_bot FROM ghosts WHERE network = ? AND user_id = ?`,
		network, userID).Scan(&g.Network, &g.UserID, &g.Name, &g.Avatar, &idents, &isBot)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	g.IsBot = isBot == 1
	if err := json.Unmarshal([]byte(idents), &g.Identifiers); err != nil {
		return nil, err
	}
	return &g, nil
}

// PutGhost writes a user's profile.
func (tx *Tx) PutGhost(ctx context.Context, g Ghost) error {
	if g.Identifiers == nil {
		g.Identifiers = []string{}
	}
	idents, err := json.Marshal(g.Identifiers)
	if err != nil {
		return err
	}
	_, err = tx.tx.ExecContext(ctx, `INSERT INTO ghosts (network, user_id, name, avatar, identifiers, is_bot) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (network, user_id) DO UPDATE SET name = excluded.name, avatar = excluded.avatar,
			identifiers = excluded.identifiers, is_bot = excluded.is_bot`,
		g.Network, g.UserID, g.Name, g.Avatar, string(idents), boolInt(g.IsBot))
	return err
}

// DeletePart removes one part, which an edit dropped.
func (tx *Tx) DeletePart(ctx context.Context, eventID string) error {
	_, err := tx.tx.ExecContext(ctx, `DELETE FROM parts WHERE event_id = ?`, eventID)
	return err
}

// ReplaceReactions replaces the reactions one message holds.
func (tx *Tx) ReplaceReactions(ctx context.Context, roomID, messageID string, rows []ReactionRow) error {
	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM reactions WHERE room_id = ? AND message_id = ?`, roomID, messageID); err != nil {
		return err
	}
	for _, r := range rows {
		if err := tx.PutReaction(ctx, r); err != nil {
			return err
		}
	}
	return nil
}

// GetKV reads a key inside a transaction, "" when absent.
func (tx *Tx) GetKV(ctx context.Context, key string) string {
	var v string
	_ = tx.tx.QueryRowContext(ctx, `SELECT value FROM kv WHERE key = ?`, key).Scan(&v)
	return v
}

// SetKV writes a key inside a transaction.
func (tx *Tx) SetKV(ctx context.Context, key, value string) error {
	_, err := tx.tx.ExecContext(ctx, `INSERT INTO kv (key, value) VALUES (?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value`, key, value)
	return err
}

// Announced reports whether a room's chat was sent whole since its account
// was last forgotten.
func (tx *Tx) Announced(ctx context.Context, roomID string) (bool, error) {
	var a int
	err := tx.tx.QueryRowContext(ctx, `SELECT announced FROM rooms WHERE room_id = ?`, roomID).Scan(&a)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return a == 1, err
}

// SetAnnounced records that a room's chat was sent whole.
func (tx *Tx) SetAnnounced(ctx context.Context, roomID string) error {
	_, err := tx.tx.ExecContext(ctx, `UPDATE rooms SET announced = 1 WHERE room_id = ?`, roomID)
	return err
}

// ForgetAccount forgets what was sent for one account: its users and its
// chats are sent again before anything names them.
func (tx *Tx) ForgetAccount(ctx context.Context, network, account string) error {
	if _, err := tx.tx.ExecContext(ctx, `DELETE FROM users_seen WHERE network = ? AND account = ?`, network, account); err != nil {
		return err
	}
	_, err := tx.tx.ExecContext(ctx, `UPDATE rooms SET announced = 0 WHERE network = ? AND receiver = ?`, network, account)
	return err
}
