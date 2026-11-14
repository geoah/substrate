package engine

// Repository migration 9: every hand that writes is an actor record, and
// everything that named a hand links one (decision 0270).
//
// Three things change for a repository written before actors were records.
//
//   - The actor DECLARATIONS go. `substrate.reamde.dev/core/actor` was a
//     declaration kind, and core declared four records of it (`api`,
//     `console`, `substratectl`, `substrate`), a package any more of its own.
//     The kind now holds the engine's actor records, so each row without a
//     `type` is tombstoned, and a package's `actors` list is cleared.
//   - A stored value that names a hand by its spelling links its actor record
//     instead: a package's `declaredBy`, a blob's `createdBy` and a call run's
//     `caller`, whose `principal` moves to `token`. The changelog conversion
//     rewrites what the history holds; this rewrites a live row the
//     conversion did not reach, and finds nothing on one it did.
//   - Every actor the repository's writes name gets its record. The history
//     names an actor by an id derived from its hand (vocabulary.ActorID), so
//     the hand behind an id is found by deriving the id of every hand the
//     repository has held: its person and the system, each package, function
//     and agent ever declared, and each app a token was approved for, read
//     from the records and from history, so a record the collector purged
//     still names its hand. A hand whose code or approval is gone is written
//     disabled. An id no hand derives is left without a record and counted
//     in the log line.
//
// Ordinary record writes, as the system. A second run finds every row in its
// new shape and every actor written.

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func migrateLinkActors(t *txn) (string, error) {
	retired, err := t.retireActorDeclarations()
	if err != nil {
		return "", err
	}
	relinked, err := t.relinkStoredActors()
	if err != nil {
		return "", err
	}
	written, unknown, err := t.writeHistoryActors()
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("retired %d actor declaration(s), linked %d stored value(s), wrote %d actor record(s), %d id(s) no hand derives",
		retired, relinked, written, unknown), nil
}

// retireActorDeclarations tombstones every live actor row without a type and
// clears every package's `actors` list.
func (t *txn) retireActorDeclarations() (int, error) {
	n := 0
	rows, err := t.liveRowsOf(kindActor)
	if err != nil {
		return 0, err
	}
	for _, r := range rows {
		if _, typed := r.Props["type"]; typed {
			continue
		}
		if err := t.migrateTombstone(r.ref()); err != nil {
			return 0, err
		}
		n++
	}
	packages, err := t.liveRowsOf(kindPackage)
	if err != nil {
		return 0, err
	}
	for _, r := range packages {
		if _, listed := r.Props["actors"]; !listed {
			continue
		}
		props := maps.Clone(r.Props)
		delete(props, "actors")
		if err := t.migrateRow(r.ref(), props, false); err != nil {
			return 0, err
		}
	}
	return n, nil
}

// relinkStoredActors rewrites each live value that still names a hand by its
// spelling into a link to the hand's actor record.
func (t *txn) relinkStoredActors() (int, error) {
	n := 0
	for kind, props := range map[string][]string{
		kindPackage:    {substrate.PackageDeclaredBy},
		kindBlob:       {blobPropCreatedBy},
		typeTriggerRun: {"caller"},
	} {
		rows, err := t.liveRowsOf(kind)
		if err != nil {
			return 0, err
		}
		for _, r := range rows {
			next, changed := relinkActorProps(r.Props, props)
			if !changed {
				continue
			}
			if err := t.migrateRow(r.ref(), next, false); err != nil {
				return 0, err
			}
			n++
		}
	}
	return n, nil
}

// relinkActorProps is props with each named property that holds a spelling
// turned into a link, and a call run's `principal` moved to `token`.
func relinkActorProps(props map[string]any, names []string) (map[string]any, bool) {
	next := maps.Clone(props)
	changed := false
	for _, name := range names {
		if spelled, ok := next[name].(string); ok && spelled != "" {
			next[name] = vocabulary.ActorRef(vocabulary.ActorID(substrate.Actor(spelled)))
			changed = true
		}
	}
	if principal, ok := next["principal"].(string); ok && slices.Contains(names, "caller") {
		if _, has := next["token"]; !has && principal != "" {
			next["token"] = principal
		}
		delete(next, "principal")
		changed = true
	}
	return next, changed
}

// writeHistoryActors writes the actor record of every hand the repository
// has held or its rows name, disabled where the hand is gone.
func (t *txn) writeHistoryActors() (written, unknown int, err error) {
	hands, err := t.repositoryHands()
	if err != nil {
		return 0, 0, err
	}
	named, err := t.namedActorIDs()
	if err != nil {
		return 0, 0, err
	}
	for _, id := range named {
		if _, ok := hands[id]; ok {
			continue
		}
		// A spelling an unconverted history still holds is its own hand.
		if !vocabulary.MintedID(id) && substrate.ActorTypeOf(substrate.Actor(id)) != "" {
			key := substrate.CanonicalActor(substrate.Actor(id))
			if _, ok := hands[vocabulary.ActorID(key)]; !ok {
				hands[vocabulary.ActorID(key)] = repositoryHand{key: key, live: key == substrate.ActorPerson || key == substrate.ActorSystem}
			}
			continue
		}
		unknown++
	}
	for _, id := range slices.Sorted(maps.Keys(hands)) {
		h := hands[id]
		row, err := t.loadRow(eref{Kind: kindActor, ID: id}, false)
		if err != nil {
			return 0, 0, err
		}
		if row != nil && row.DeletedAt == nil {
			continue
		}
		if !h.live && !slices.Contains(named, id) {
			continue
		}
		if err := t.ensureActors(h.key); err != nil {
			return 0, 0, err
		}
		if !h.live {
			if err := t.disableActor(h.key); err != nil {
				return 0, 0, err
			}
		}
		written++
	}
	return written, unknown, nil
}

// repositoryHand is one hand the repository has held: its key, and whether
// its code or approval still stands.
type repositoryHand struct {
	key  substrate.Actor
	live bool
}

// repositoryHands is every hand the repository has held, by the id of its
// actor record: the person, the system, each package, function and agent
// declared (a tombstone's hand gone), and each app a token was approved for
// (gone when no live token stands for it). A record the collector purged
// still names its hand in history: its declaration's id, and the values a
// token was minted with, so those are read from the changelog too.
func (t *txn) repositoryHands() (map[string]repositoryHand, error) {
	hands := map[string]repositoryHand{}
	add := func(key substrate.Actor, live bool) {
		id := vocabulary.ActorID(key)
		if h, ok := hands[id]; ok {
			live = live || h.live
		}
		hands[id] = repositoryHand{key: substrate.CanonicalActor(key), live: live}
	}
	add(substrate.ActorPerson, true)
	add(substrate.ActorSystem, true)
	addHand := func(kind, id string, props map[string]any, live bool) {
		switch kind {
		case kindPackage:
			if authority, pkg := vocabulary.SplitPackageRef(id); authority != "" {
				add(substrate.BundleActor(authority, pkg), live)
			}
		case kindFunction, kindAgent:
			authority, pkg, name := vocabulary.SplitKindRef(id)
			if authority == "" {
				return
			}
			if kind == kindFunction {
				add(substrate.FunctionActor(authority, pkg, name), live)
			} else {
				add(substrate.AgentActor(authority, pkg, name), live)
			}
		case kindToken:
			if app := tokenApp(props); app != "" {
				add(app, live)
			}
		}
	}
	kinds := []string{kindPackage, kindFunction, kindAgent, kindToken}
	rows, err := t.query(`SELECT kind, id, props, deleted_at IS NULL FROM records WHERE kind = ANY($1::text[])`, kinds)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind, id string
		var raw []byte
		var live bool
		if err := rows.Scan(&kind, &id, &raw, &live); err != nil {
			_ = rows.Close()
			return nil, err
		}
		var props map[string]any
		if err := json.Unmarshal(raw, &props); err != nil {
			_ = rows.Close()
			return nil, err
		}
		addHand(kind, id, props, live)
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// What history says of a record the collector purged: the id of each
	// declaration, and every value a token's writes set.
	rows, err = t.query(`SELECT kind, record_id, payload::text FROM changelog WHERE kind = ANY($1::text[]) AND op IN ('put', 'patch')`, kinds)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var kind, id, raw string
		if err := rows.Scan(&kind, &id, &raw); err != nil {
			return nil, err
		}
		props := map[string]any{}
		if kind == kindToken {
			props = historicalProps([]byte(raw))
		}
		addHand(kind, id, props, false)
	}
	return hands, rows.Err()
}

// historicalProps is every property value a history line's record effects
// set, merged in order.
func historicalProps(payload []byte) map[string]any {
	var p struct {
		Fold []struct {
			Kind  string `json:"kind"`
			Delta *struct {
				Set map[string]any `json:"set"`
			} `json:"delta"`
		} `json:"fold"`
	}
	props := map[string]any{}
	if json.Unmarshal(payload, &p) != nil {
		return props
	}
	for _, effect := range p.Fold {
		if effect.Kind == "record" && effect.Delta != nil {
			maps.Copy(props, effect.Delta.Set)
		}
	}
	return props
}

// namedActorIDs is every actor the repository's history names: the actor and
// the one it acted for of every line, and the hand of every manager effect a
// line holds. Every manager row and every offer was folded or derived from
// those, so the history alone answers, and the read names no fold table a
// later binary reshapes: this migration runs again over every import.
func (t *txn) namedActorIDs() ([]string, error) {
	rows, err := t.query(`
		SELECT actor FROM changelog WHERE actor <> ''
		UNION SELECT on_behalf_of FROM changelog WHERE on_behalf_of <> ''
		UNION SELECT e ->> 'actor' FROM changelog,
			jsonb_array_elements(CASE WHEN jsonb_typeof(payload -> 'fold') = 'array' THEN payload -> 'fold' ELSE '[]'::jsonb END) e
			WHERE e ->> 'kind' = 'manager' AND coalesce(e ->> 'actor', '') <> ''`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids, rows.Err()
}

// liveRowsOf is every live row of one kind, in id order.
func (t *txn) liveRowsOf(kind string) ([]*erow, error) {
	rows, err := t.query(`SELECT `+recordCols+` FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id`, kind)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*erow
	for rows.Next() {
		r, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
