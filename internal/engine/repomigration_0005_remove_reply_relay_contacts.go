package engine

// Repository migration 5: the contacts GitHub's relay addresses made.
//
// The Google provider's Gmail sync wrote every address in a mail header as an
// `emailaddress` hub row, and core's `googleemailaddress` mapping made a
// contact of every hub row it could not link to one. GitHub names a fresh
// `reply+<token>@reply.github.com` on every notification thread, and the
// reason for each notification as `<reason>@noreply.github.com`, so a
// repository that follows GitHub holds a contact per thread and per reason.
// Those relay addresses name no party, and the sync no longer writes a hub
// row for one (REPLY_RELAYS in the bundle's syncgmail); this removes the ones
// it wrote before, and what they made:
//
//   - each live hub row whose address is a relay goes, and every Gmail
//     message entry naming it loses its `address`, keeping the `value` and
//     `name` the header spelled, which is how the sync writes a relay now;
//   - each contact those rows ALONE made goes with them.
//
// A contact goes only when the engine wrote all of it: every source linking
// to it is a relay row this removes, nothing else points at it, every
// property is held at the machine tier, it carries no label, annotation,
// finalizer or merge trail and no reference of its own, and every state is
// still its declared initial value. An owner's edit, a hide or a promotion, a
// label, an agent's write, a merge, another provider's mirror of the same
// contact, or a record of the owner's pointing at it keeps the contact, and
// with it every relay row linking to it: the migration runs before the stored
// vocabulary loads, so it cannot recompute a contact that loses one source
// and keeps another, and a source deleted without that recompute would leave
// the contact an address and an offer no live record makes. A relay row that
// any record but a Gmail message points at (a calendar attendee, an
// address-book entry) stays too, because the sync that wrote that record
// writes the row again. So does one whose subject slot names a record other
// than a contact.
//
// Everything is an ordinary record write through the fold, as the system: a
// patch per message, a delete per row. The message patches fold under no
// declaration, so the runner asks the pass behind the open to re-derive the
// message kind's refs rows and search bands (requestUnprojected). The reads
// are set-based over the relay rows, and the messages are read and written a
// page at a time. A repository without the Google provider's hub kind reads
// one row, and one whose hub rows hold no relay reads those rows once; neither
// writes anything, and nor does a second run.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	relayHubKind     = "providers.substrate.reamde.dev/google/emailaddress"
	relayMessageKind = "providers.substrate.reamde.dev/google/gmailmessage"
	// relayHubAccount is the hub row's one declared reference. Every other
	// reference the row holds is a mapping's subject slot.
	relayHubAccount = "account"
	// relayEntryAddress is the field of a header entry naming its hub row.
	relayEntryAddress = "address"
	// relayMessagePage bounds the messages read and rewritten at a time.
	relayMessagePage = 500
)

// relayHeaders are the message properties whose entries name a hub row.
var relayHeaders = []string{"from", "to", "cc", "bcc", "replyTo"}

// replyRelays is REPLY_RELAYS in the google bundle's syncgmail
// (kinds/providers.substrate.reamde.dev/google/bundle.yaml), and THE TWO
// CHANGE TOGETHER: a relay listed there and not here keeps the contacts made
// before it was listed, and one listed here and not there is deleted now and
// written again by the next sync. Each entry is (domain, local-part prefix):
// the domain matched exactly, the prefix ignoring case, and something after
// it.
var replyRelays = []struct{ domain, prefix string }{
	{"reply.github.com", "reply+"},
	{"noreply.github.com", ""},
}

// isReplyRelay reports whether a hub row's address, in its normal form
// (domain lowercased), is a relay address: one naming a thread or a
// notification reason, never a party.
func isReplyRelay(address string) bool {
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return false
	}
	local, domain := address[:at], strings.ToLower(address[at+1:])
	for _, r := range replyRelays {
		if domain == r.domain && len(local) > len(r.prefix) && strings.HasPrefix(strings.ToLower(local), r.prefix) {
			return true
		}
	}
	return false
}

// relayNode is one relay row or one contact it links to, and the others on
// the far side of that link. keep is set once anything says it stays.
type relayNode struct {
	keep  bool
	links []string
}

func migrateRemoveReplyRelayContacts(t *txn) (string, error) {
	var declared bool
	if err := t.row(`SELECT EXISTS (SELECT 1 FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL)`,
		kindKind, relayHubKind).Scan(&declared); err != nil {
		return "", err
	}
	if !declared {
		return "the Google provider is not installed", nil
	}
	hubs, err := t.relayHubs()
	if err != nil {
		return "", err
	}
	if len(hubs) == 0 {
		return "no hub row holds a relay address", nil
	}
	contacts, err := t.relayContacts(hubs)
	if err != nil {
		return "", err
	}
	// A kept contact keeps every relay row linking to it, and a kept relay
	// row keeps the contact it links to, until nothing moves.
	for changed := true; changed; {
		changed = false
		for _, side := range [2][2]map[string]*relayNode{{hubs, contacts}, {contacts, hubs}} {
			for _, n := range side[0] {
				if n.keep {
					continue
				}
				for _, id := range n.links {
					if side[1][id].keep {
						n.keep, changed = true, true
						break
					}
				}
			}
		}
	}
	gone := func(nodes map[string]*relayNode) []string {
		var ids []string
		for id, n := range nodes {
			if !n.keep {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		return ids
	}
	goneHubs, goneContacts := gone(hubs), gone(contacts)
	messages, err := t.detachRelayEntries(goneHubs)
	if err != nil {
		return "", err
	}
	for _, id := range goneHubs {
		if err := t.migrateTombstone(eref{Kind: relayHubKind, ID: id}); err != nil {
			return "", err
		}
	}
	for _, id := range goneContacts {
		if err := t.migrateTombstone(eref{Kind: vocabulary.KindContact, ID: id}); err != nil {
			return "", err
		}
	}
	kept := len(hubs) - len(goneHubs)
	if len(goneHubs) == 0 {
		return fmt.Sprintf("kept all %d hub rows holding a relay address: each one's contact, or another record, holds more than the relay made", kept), nil
	}
	return fmt.Sprintf("deleted %d hub rows holding a relay address and %d contacts they alone made, and took the address off %d messages; kept %d hub rows whose contact, or another record, holds more than the relay made",
		len(goneHubs), len(goneContacts), messages, kept), nil
}

// relayHubs reads every live hub row holding a relay address, each
// marked kept when it carries anything of its own or something other than a
// Gmail message's header points at it, and linked to the contacts its
// subject slots name.
func (t *txn) relayHubs() (map[string]*relayNode, error) {
	patterns := make([]string, 0, len(replyRelays))
	for _, r := range replyRelays {
		patterns = append(patterns, "%@"+r.domain)
	}
	rows, err := t.query(`
		SELECT id, props->>'address', labels = '{}'::jsonb AND cardinality(finalizers) = 0 FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND lower(props->>'address') LIKE ANY($2::text[])`,
		relayHubKind, patterns)
	if err != nil {
		return nil, err
	}
	hubs := map[string]*relayNode{}
	for rows.Next() {
		var id, address string
		var bare bool
		if err := rows.Scan(&id, &address, &bare); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if isReplyRelay(address) {
			hubs[id] = &relayNode{keep: !bare}
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(hubs) == 0 {
		return hubs, nil
	}
	ids := slices.Sorted(maps.Keys(hubs))
	// What an owner leaves on a row: an annotation, a property held at their
	// tier (the sync's own hold at the bundle tier, the mapping's slot at the
	// machine one), a merge. And a referrer other than a message header: the
	// calendar, contacts and drive syncs write the row again for theirs.
	keep, err := t.idSet(`
		SELECT record_id FROM annotations WHERE record_kind = $1 AND record_id = ANY($2::text[])
		UNION SELECT record_id FROM property_managers WHERE record_kind = $1 AND record_id = ANY($2::text[]) AND tier = $3
		UNION SELECT record_id FROM former_ids WHERE record_kind = $1 AND record_id = ANY($2::text[])
		UNION SELECT r.dst FROM refs r JOIN records x ON x.kind = r.src_kind AND x.id = r.src
		  WHERE r.dst_kind = $1 AND r.dst = ANY($2::text[]) AND x.deleted_at IS NULL
		    AND NOT (r.src_kind = $4 AND r.property = ANY($5::text[]))`,
		relayHubKind, ids, string(substrate.TierOwner), relayMessageKind, relayHeaders)
	if err != nil {
		return nil, err
	}
	for id := range keep {
		hubs[id].keep = true
	}
	slots, err := t.query(`
		SELECT r.src, r.path, r.dst_kind, r.dst, x.id IS NOT NULL AND x.deleted_at IS NULL, f.record_id IS NOT NULL
		FROM refs r
		LEFT JOIN records x ON x.kind = r.dst_kind AND x.id = r.dst
		LEFT JOIN former_ids f ON f.record_kind = r.dst_kind AND f.former_id = r.dst
		WHERE r.src_kind = $1 AND r.src = ANY($2::text[]) AND r.property <> $3`,
		relayHubKind, ids, relayHubAccount)
	if err != nil {
		return nil, err
	}
	defer func() { _ = slots.Close() }()
	for slots.Next() {
		var src, path, dstKind, dst string
		var live, merged bool
		if err := slots.Scan(&src, &path, &dstKind, &dst, &live, &merged); err != nil {
			return nil, err
		}
		h := hubs[src]
		switch {
		case path != "" || dstKind != vocabulary.KindContact || merged:
			// Another mapping's subject, or a contact merged into another:
			// a target this cannot judge or a merge the owner made.
			h.keep = true
		case live:
			h.links = append(h.links, dst)
		}
		// A slot naming a contact that is deleted, with no trail, links
		// to nothing.
	}
	return hubs, slots.Err()
}

// relayContacts reads the live contacts the relay rows link to, each marked
// kept unless the engine wrote all of it and only relay rows point at it.
func (t *txn) relayContacts(hubs map[string]*relayNode) (map[string]*relayNode, error) {
	contacts := map[string]*relayNode{}
	for _, h := range hubs {
		for _, id := range h.links {
			contacts[id] = &relayNode{}
		}
	}
	if len(contacts) == 0 {
		return contacts, nil
	}
	ids := slices.Sorted(maps.Keys(contacts))
	initial, err := t.initialStates(vocabulary.KindContact)
	if err != nil {
		return nil, err
	}
	rows, err := t.query(`
		SELECT id, states, labels = '{}'::jsonb AND cardinality(finalizers) = 0 FROM records
		WHERE kind = $1 AND id = ANY($2::text[])`,
		vocabulary.KindContact, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		var raw []byte
		var bare bool
		if err := rows.Scan(&id, &raw, &bare); err != nil {
			_ = rows.Close()
			return nil, err
		}
		var states map[string]string
		if err := json.Unmarshal(raw, &states); err != nil {
			_ = rows.Close()
			return nil, fmt.Errorf("the states of contact %s: %w", id, err)
		}
		c := contacts[id]
		c.keep = !bare
		for name, value := range states {
			if initial[name] != value {
				c.keep = true
			}
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	// A hand's property, an annotation, a merge it absorbed, a reference of
	// its own.
	keep, err := t.idSet(`
		SELECT record_id FROM property_managers WHERE record_kind = $1 AND record_id = ANY($2::text[]) AND tier <> $3
		UNION SELECT record_id FROM annotations WHERE record_kind = $1 AND record_id = ANY($2::text[])
		UNION SELECT record_id FROM former_ids WHERE record_kind = $1 AND record_id = ANY($2::text[])
		UNION SELECT src FROM refs WHERE src_kind = $1 AND src = ANY($2::text[])`,
		vocabulary.KindContact, ids, string(substrate.TierMachine))
	if err != nil {
		return nil, err
	}
	for id := range keep {
		contacts[id].keep = true
	}
	refs, err := t.query(`
		SELECT r.dst, r.src_kind, r.src FROM refs r JOIN records x ON x.kind = r.src_kind AND x.id = r.src
		WHERE r.dst_kind = $1 AND r.dst = ANY($2::text[]) AND x.deleted_at IS NULL`,
		vocabulary.KindContact, ids)
	if err != nil {
		return nil, err
	}
	defer func() { _ = refs.Close() }()
	for refs.Next() {
		var dst, srcKind, src string
		if err := refs.Scan(&dst, &srcKind, &src); err != nil {
			return nil, err
		}
		c := contacts[dst]
		if _, relay := hubs[src]; srcKind == relayHubKind && relay {
			c.links = append(c.links, src)
			continue
		}
		c.keep = true
	}
	return contacts, refs.Err()
}

// initialStates reads the initial value of every state a stored kind
// declares, from its declaration row: the vocabulary has not loaded yet.
func (t *txn) initialStates(kind string) (map[string]string, error) {
	var raw []byte
	err := t.row(`SELECT props->'properties' FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`,
		kindKind, kind).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil || raw == nil {
		return nil, err
	}
	var props map[string]struct {
		Type    string `json:"type"`
		Initial string `json:"initial"`
	}
	if err := json.Unmarshal(raw, &props); err != nil {
		return nil, fmt.Errorf("the stored declaration of %s: %w", kind, err)
	}
	out := map[string]string{}
	for name, p := range props {
		if p.Type == "state" && p.Initial != "" {
			out[name] = p.Initial
		}
	}
	return out, nil
}

// idSet runs a query of one id column and collects what it answers.
func (t *txn) idSet(q string, args ...any) (map[string]bool, error) {
	rows, err := t.query(q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bool{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// detachRelayEntries takes `address` off every live message's header entry
// naming one of the hub rows, a page of messages at a time, and reports how
// many messages it rewrote.
func (t *txn) detachRelayEntries(hubs []string) (int, error) {
	if len(hubs) == 0 {
		return 0, nil
	}
	gone := map[string]bool{}
	for _, id := range hubs {
		gone[vocabulary.RecordPath(relayHubKind, id)] = true
	}
	// Collected before any write, so no cursor is open on the transaction's
	// connection while it writes.
	ids, err := t.idSet(`
		SELECT r.src FROM refs r JOIN records x ON x.kind = r.src_kind AND x.id = r.src
		WHERE r.dst_kind = $1 AND r.dst = ANY($2::text[]) AND r.src_kind = $3 AND x.deleted_at IS NULL`,
		relayHubKind, hubs, relayMessageKind)
	if err != nil {
		return 0, err
	}
	sorted := slices.Sorted(maps.Keys(ids))
	rewrote := 0
	for page := range slices.Chunk(sorted, relayMessagePage) {
		rows, err := t.ds.scanRows(t.ctx, t.tx,
			`SELECT `+recordCols+` FROM records WHERE kind = $1 AND id = ANY($2::text[]) AND deleted_at IS NULL ORDER BY id FOR UPDATE`,
			[]any{relayMessageKind, page})
		if err != nil {
			return rewrote, err
		}
		for _, row := range rows {
			before := row.clone()
			// A copy, so the row loaded before the write still reads as it
			// was and the fold sees the difference.
			row.Props = maps.Clone(row.Props)
			var changed []string
			for _, name := range relayHeaders {
				if entries, ok := withoutRelayAddresses(row.Props[name], gone); ok {
					row.Props[name] = entries
					changed = append(changed, name)
				}
			}
			if len(changed) == 0 {
				continue
			}
			res, err := t.foldRow(before, row, false, false)
			if err != nil {
				return rewrote, err
			}
			if !res.changed {
				continue
			}
			if err := t.appendChange(substrate.ActorSystem, substrate.OpPatch, row.ID, row.Kind,
				map[string]any{"properties": changed}); err != nil {
				return rewrote, err
			}
			rewrote++
		}
	}
	return rewrote, nil
}

// withoutRelayAddresses is a header's entries with `address` dropped from each
// one naming a hub row in gone, and whether any was.
func withoutRelayAddresses(v any, gone map[string]bool) ([]any, bool) {
	entries, ok := v.([]any)
	if !ok {
		return nil, false
	}
	out := make([]any, len(entries))
	changed := false
	for i, e := range entries {
		out[i] = e
		entry, ok := e.(map[string]any)
		if !ok || !gone[storedReferencePath(entry[relayEntryAddress])] {
			continue
		}
		entry = maps.Clone(entry)
		delete(entry, relayEntryAddress)
		out[i] = entry
		changed = true
	}
	return out, changed
}

// migrateTombstone deletes one record as the system, as the delete verb does.
// The registry is empty while a migration runs, so afterTombstone clears
// neither mark; the row's are cleared here, because a tombstone carries none
// and a rebuild derives none for it.
func (t *txn) migrateTombstone(ref eref) error {
	changed, err := t.tombstone(ref, "")
	if err != nil || !changed {
		return err
	}
	if err := t.appendChange(substrate.ActorSystem, substrate.OpDelete, ref.ID, ref.Kind,
		map[string]any{"finalizers": []string{}}); err != nil {
		return err
	}
	if err := t.afterTombstone(ref); err != nil {
		return err
	}
	_, err = t.exec(`UPDATE records SET orphaned_at = NULL, ambiguous_at = NULL
		WHERE kind = $1 AND id = $2 AND (orphaned_at IS NOT NULL OR ambiguous_at IS NOT NULL)`, ref.Kind, ref.ID)
	return err
}
