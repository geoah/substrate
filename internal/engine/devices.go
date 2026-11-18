package engine

// DEVICES (docs/plans/push.md). One install of an app that takes pushes is a
// record of `core/device`, named by the token it signed in with and deleted
// with it. What the engine holds, on every write of one:
//
//   - only a person, or an app acting for them, writes one, and only at the
//     id of the token the request carries: a device names the sign-in that
//     signs its pushes, so no write may name another's;
//   - `token` and `person` are filled in from that token and the person it
//     acts for, and fixed when the record is created;
//   - `seenAt` is stamped on every write, and a value a write sends is
//     dropped;
//   - a delete, a pin or a release is the device's own person's, through
//     any token of theirs, or an admin's.
//
// The engine deletes a device itself, as the system, in two places: the
// sweep that collects a token cascades to it (`onDelete: cascade`), and
// RunGC deletes the devices of tokens past their expiry; and the dispatcher
// asks it to delete one after the relay refused its key, only while the
// record still holds that key (DeletePushDevice).

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/geoah/substrate/internal/pushwire"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	typeDevice = vocabulary.KindDevice

	propDeviceToken    = "token"
	propDevicePerson   = "person"
	propDevicePushKey  = "pushKey"
	propDeviceNotifies = "notifies"
	propDeviceSeenAt   = "seenAt"

	// maxDeviceName bounds what a device calls itself.
	maxDeviceName = 100
)

// deviceWriter is the person a write of a device is made as: a person's own
// hand, or the person an app's token acts for. Installed code writes none.
func (t *txn) deviceWriter() (substrate.Actor, error) {
	switch substrate.ActorTypeOf(t.actor) {
	case substrate.ActorTypePerson:
		return t.ds.personActorFor(t.ctx, t.actor), nil
	case substrate.ActorTypeApp:
		if p := t.onBehalfOfKey(t.actor); substrate.ActorTypeOf(p) == substrate.ActorTypePerson {
			return p, nil
		}
	}
	return "", fmt.Errorf("%w: %w: %s may not write %s: only a person, or an app acting for them, registers their own install",
		substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(t.actor), typeDevice)
}

// prepareDevice holds a write of a device to the rules above and fills what
// the engine decides, before the managed check reads the write. It reports
// whether the write is a device's, which stamps `seenAt`.
func (t *txn) prepareDevice(sp *applySpec) (bool, error) {
	if sp.ty.Identity != typeDevice || t.internal || t.movingRecords {
		return false, nil
	}
	// A write of labels or annotations alone carries no properties, and the
	// engine's fills land in them all the same.
	if sp.props == nil {
		sp.props = map[string]any{}
	}
	delete(sp.props, propDeviceSeenAt)
	writer, err := t.deviceWriter()
	if err != nil {
		return false, err
	}
	if t.principal == "" {
		return false, fmt.Errorf("%w: %w: a %s is written with the token it names, and this write carries none",
			substrate.ErrForbidden, substrate.ErrNotPermitted, typeDevice)
	}
	if sp.id != t.principal {
		return false, fmt.Errorf("%w: %w: a %s's id is the id of the token the request carries, here %s",
			substrate.ErrForbidden, substrate.ErrNotPermitted, typeDevice, t.principal)
	}
	// The kind's `max` documents the bounds; a string's is held here, where
	// the engine stores it without reading it.
	for name, limit := range map[string]int{"name": maxDeviceName, propDevicePushKey: pushwire.MaxPushKey} {
		if v, ok := sp.props[name].(string); ok && utf8.RuneCountInString(v) > limit {
			return false, fmt.Errorf("%w: %s.%s is at most %d characters", substrate.ErrValidation, typeDevice, name, limit)
		}
	}
	want := map[string]eref{
		propDeviceToken:  {Kind: kindToken, ID: t.principal},
		propDevicePerson: {Kind: kindActor, ID: actorID(writer)},
	}
	for _, name := range []string{propDeviceToken, propDevicePerson} {
		held := referenceTargetOf(sp.existing, name)
		if held.ID != "" && held != want[name] {
			// A stored record another token or person holds is never this
			// write's: the id is the token's, so this cannot happen short of
			// a hand-made row, and it is refused rather than taken over.
			return false, fmt.Errorf("%w: %w: this %s is another sign-in's", substrate.ErrForbidden, substrate.ErrNotPermitted, typeDevice)
		}
		if named(sp.props, name) {
			if got := writtenReference(sp.props[name], want[name].Kind); got != want[name].ID {
				return false, fmt.Errorf("%w: %w: %s.%s is the request's own and fixed when the record is created: leave it out",
					substrate.ErrForbidden, substrate.ErrNotPermitted, typeDevice, name)
			}
		}
		sp.props[name] = referenceValueOf(vocabulary.RecordPath(want[name].Kind, want[name].ID))
	}
	return true, nil
}

// writtenReference is the id a written reference names, "" when it names
// none or a record of another kind.
func writtenReference(v any, kind string) string {
	if refs := referenceTargets(v); len(refs) == 1 && refs[0].Kind == kind {
		return refs[0].ID
	}
	return writtenReferenceID(v, kind)
}

// guardDeviceOwner holds what a hand does to a stored device outside a put
// or a patch (its delete, a pin or a release) to the device's own person,
// through any token of theirs, or to a person who holds `admin`.
func (t *txn) guardDeviceOwner(kind string, row *erow, what string) error {
	if kind != typeDevice || t.internal {
		return nil
	}
	writer, err := t.deviceWriter()
	if err != nil {
		return err
	}
	if referenceTargetOf(row, propDevicePerson).ID == actorID(writer) {
		return nil
	}
	roles, err := t.rolesOf(t.ds.personActorFor(t.ctx, writer))
	if err != nil {
		return err
	}
	if roles.Admin() {
		return nil
	}
	return fmt.Errorf("%w: %w: %s may not %s this %s: it is another person's",
		substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(t.actor), what, typeDevice)
}

// deviceSweepBatch bounds the devices one sweep reads.
const deviceSweepBatch = 500

// sweepExpiredDevices deletes, as the system, every live device whose token
// is past its expiry, gone or deleted: the collection of a deleted token
// cascades to its device, and nothing else would reach one whose token
// expired. It reports how many it deleted.
func (ds *dataset) sweepExpiredDevices(ctx context.Context, now time.Time) (int, error) {
	return ds.sweepDevices(ctx, now, deviceSweepBatch)
}

// sweepDevices is one sweep of at most batch devices.
func (ds *dataset) sweepDevices(ctx context.Context, now time.Time, batch int) (int, error) {
	if ds.svc.readOnly {
		return 0, nil
	}
	if _, err := ds.resolveType(typeDevice); err != nil {
		// A repository whose upgrade has not declared the kind holds none.
		return 0, nil
	}
	// The predicate runs before the LIMIT, so a sweep past the first batch
	// is never needed to reach a device whose token went: each batch holds
	// only devices to delete, and the next sweep takes the rest. An
	// `expiresAt` the engine did not write is left for the Go check below
	// rather than failing the cast.
	rows, err := ds.db.QueryContext(ctx, `
		SELECT d.id, tk.id IS NULL OR tk.deleted_at IS NOT NULL, COALESCE(tk.props->>'expiresAt', '')
		FROM records d
		LEFT JOIN records tk ON tk.kind = $2 AND tk.id = d.id
		WHERE d.kind = $1 AND d.deleted_at IS NULL
		  AND (tk.id IS NULL OR tk.deleted_at IS NOT NULL
		       OR (tk.props->>'expiresAt' ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$'
		           AND (tk.props->>'expiresAt')::timestamptz <= $4))
		ORDER BY d.id LIMIT $3`, typeDevice, kindToken, batch, now)
	if err != nil {
		return 0, err
	}
	var gone []string
	for rows.Next() {
		var id, expires string
		var missing bool
		if err := rows.Scan(&id, &missing, &expires); err != nil {
			_ = rows.Close()
			return 0, err
		}
		if exp := parseStamp(sql.NullString{String: expires, Valid: expires != ""}); missing || (exp != nil && !now.Before(*exp)) {
			gone = append(gone, id)
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	deleted := 0
	for _, id := range gone {
		err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
			row, err := t.loadRow(eref{Kind: typeDevice, ID: id}, true)
			if err != nil || row == nil || row.DeletedAt != nil {
				return err
			}
			_, err = t.softDelete(eref{Kind: typeDevice, ID: id})
			return err
		})
		if err != nil {
			return deleted, fmt.Errorf("delete the device of expired token %s: %w", id, err)
		}
		deleted++
	}
	return deleted, nil
}

// DeletePushDevice deletes a device as the system, in one transaction that
// reads it first and deletes it only while it still holds pushKey: an
// install that registered again while a send was in flight keeps its new
// registration.
func (ds *dataset) DeletePushDevice(ctx context.Context, id, pushKey string) (bool, error) {
	deleted := false
	err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		deleted = false
		ref := eref{Kind: typeDevice, ID: id}
		if err := t.lockRecord(ref); err != nil {
			return err
		}
		row, err := t.loadRow(ref, true)
		if err != nil || row == nil || row.DeletedAt != nil {
			return err
		}
		if held, _ := row.Props[propDevicePushKey].(string); held != pushKey {
			return nil
		}
		if _, err := t.softDelete(ref); err != nil {
			return err
		}
		deleted = true
		return nil
	})
	return deleted, err
}

// PushDevices are the live devices of the person whose actor record is
// person, each with its token, whose token is live and not expired.
func (ds *dataset) PushDevices(ctx context.Context, person string) ([]substrate.PushDevice, error) {
	if person == "" {
		return nil, nil
	}
	if _, err := ds.resolveType(typeDevice); err != nil {
		return nil, nil
	}
	// A person taken out of the repository, or disabled, has no devices to
	// push to, whatever records they left.
	var member bool
	if err := ds.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM records WHERE kind = $1 AND id = $2
		AND deleted_at IS NULL AND COALESCE(props->>'disabled', '') <> 'true')`, kindActor, person).Scan(&member); err != nil || !member {
		return nil, err
	}
	b := &builder{}
	q := `SELECT d.id, d.props FROM records d WHERE d.kind = ` + b.arg(typeDevice) + ` AND d.deleted_at IS NULL AND ` +
		referencePathSQL("d.props", propDevicePerson) + ` = ` + b.arg(vocabulary.RecordPath(kindActor, person)) + ` ORDER BY d.id`
	rows, err := ds.db.QueryContext(ctx, q, b.args...)
	if err != nil {
		return nil, err
	}
	type found struct {
		id    string
		props map[string]any
	}
	var devices []found
	for rows.Next() {
		var f found
		var raw []byte
		if err := rows.Scan(&f.id, &raw); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if err := json.Unmarshal(raw, &f.props); err != nil {
			_ = rows.Close()
			return nil, err
		}
		devices = append(devices, f)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil || len(devices) == 0 {
		return nil, err
	}
	tokens, err := ds.Tokens(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]substrate.TokenInfo, len(tokens))
	for _, tok := range tokens {
		byID[tok.ID] = tok
	}
	now := nowUTC()
	var out []substrate.PushDevice
	for _, d := range devices {
		tok, live := byID[d.id]
		if !live || tok.Retired || (tok.ExpiresAt != nil && !now.Before(*tok.ExpiresAt)) {
			continue
		}
		key, _ := d.props[propDevicePushKey].(string)
		if key == "" {
			continue
		}
		var notifies []string
		if list, ok := d.props[propDeviceNotifies].([]any); ok {
			for _, v := range list {
				if s, ok := v.(string); ok {
					notifies = append(notifies, s)
				}
			}
		}
		out = append(out, substrate.PushDevice{ID: d.id, Person: person, PushKey: key, Notifies: notifies, Token: tok})
	}
	return out, nil
}
