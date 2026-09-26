package engine

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The subject reference and its mapping. A
// type carrying a recordmapping records what ONE SOURCE holds; the record its
// declared subject reference points at is the subject those records describe,
// and recompute carries the mapped properties onto it — yielding to any manager
// row above the machine tier, so a hand edit (the owner's or a bundle's)
// survives a sync, legibly.

// --- subject resolution -----------------------------------------------------

// subjectTargetOf reads the LIVE record a source record's subject reference
// names, "" when it names nothing. It reads the refs index rather than the
// property, so the stored destination is one statement away.
//
// THE STORED ID IS RESOLVED, NOT TRUSTED. A merge repoints nothing: the value
// keeps naming the loser and resolution runs on read (decision 0044), so
// liveness is asked of the CANONICAL record the stored id now denotes. Asking
// the literal id would call a merged-away subject unpointed and let the next
// sync mint a duplicate.
//
// A tombstoned canonical target counts as unpointed: the owner deleted that
// person, and a source record must not go on resolving to a dead id or refusing
// every later sync because of one. The returned eref is the canonical one; the
// stored value stays as written.
func (t *txn) subjectTargetOf(src eref, property string) (eref, error) {
	var stored eref
	err := t.row(`
		SELECT r.dst_kind, r.dst FROM refs r
		WHERE r.src_kind = $1 AND r.src = $2 AND r.property = $3 AND r.path = ''
		ORDER BY r.ord LIMIT 1`, src.Kind, src.ID, property).Scan(&stored.Kind, &stored.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return eref{}, nil
	}
	if err != nil {
		return eref{}, err
	}
	return t.liveCanonical(stored)
}

// storedSubjectOf is the subject hop through a TOMBSTONED source: the
// canonical record its subject slot stores, whether that record is live or a
// tombstone itself. A tombstone with no subject stored has nothing to resolve
// to, and the reference is refused naming it.
func (t *txn) storedSubjectOf(src eref, srcTy *vocabulary.Kind, m *vocabulary.Mapping) (eref, error) {
	var stored eref
	err := t.row(`
		SELECT r.dst_kind, r.dst FROM refs r
		WHERE r.src_kind = $1 AND r.src = $2 AND r.property = $3 AND r.path = ''
		ORDER BY r.ord LIMIT 1`, src.Kind, src.ID, m.Property).Scan(&stored.Kind, &stored.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return eref{}, fmt.Errorf("reference names %s, which is deleted and describes no %s",
			vocabulary.RecordPath(srcTy.Identity, src.ID), m.To)
	}
	if err != nil {
		return eref{}, err
	}
	return t.canonicalOf(stored)
}

// liveCanonical resolves a stored destination through the former-id trail and
// reports the canonical record when it is live, the zero eref when it is a
// tombstone or absent. A plain tombstone with no trail behind it resolves to
// itself and is therefore not live, which is what makes a deleted target
// re-resolvable.
func (t *txn) liveCanonical(stored eref) (eref, error) {
	canon, err := t.canonicalOf(stored)
	if err != nil {
		return eref{}, err
	}
	row, err := t.loadRow(canon, false)
	if err != nil || row == nil || row.DeletedAt != nil {
		return eref{}, err
	}
	return canon, nil
}

// subjectOf resolves the record a source record describes THROUGH ONE MAPPING,
// matching or minting the subject an unpointed record implies and storing the
// pointer in line (every source record has its subject from the first
// moment). The caller chooses the mapping, because a source kind may
// carry one per subject property (record 49).
//
// It runs OUT OF BAND — inside somebody else's write, when a reference names
// this record and the declaration pins the subject's kind (references.go
// subjectHop) — so the source row is already stored and the pointer is written
// through the ordinary path, as the engine's own hand.
func (t *txn) subjectOf(src *erow, srcTy *vocabulary.Kind, m *vocabulary.Mapping) (string, error) {
	// Two writers resolving the same unpointed record must not each mint a
	// shell: take the record's lock before looking.
	if err := t.lockRecord(src.ref()); err != nil {
		return "", err
	}
	linked, err := t.subjectTargetOf(src.ref(), m.Property)
	if err != nil {
		return "", err
	}
	if linked.ID != "" {
		// Already canonical: subjectTargetOf resolves the stored id.
		return linked.ID, nil
	}
	// THE HOP DEMANDS A SUBJECT. Somebody's write names this mirror in a slot
	// pinned at the subject kind, so there has to be a record to point at:
	// this is the one caller that mints whatever the source carries, and the
	// one that still mints out of an ambiguous probe.
	target, _, err := t.matchOrMint(src, srcTy, m, true)
	if err != nil {
		return "", err
	}
	if err := t.writeSubject(src.ref(), m.Property, eref{Kind: m.To, ID: target}); err != nil {
		return "", err
	}
	// A new subject pointer is a recompute trigger: the subject takes its
	// properties from the record that just resolved through it.
	if err := t.recompute(eref{Kind: m.To, ID: target}); err != nil {
		return "", err
	}
	return target, nil
}

// writeSubject stores a source record's subject pointer through the ordinary
// write path, as the engine's own hand: the subject is one of the record's own
// properties, so the change travels in that record's delta and a rebuild
// replays it. Internal, because put and patch refuse to move a subject
// (write.go) and this IS the move that creates one.
func (t *txn) writeSubject(src eref, property string, target eref) error {
	was := t.internal
	t.internal = true
	defer func() { t.internal = was }()
	_, err := t.patch(src, substrate.PatchInput{Properties: map[string]any{
		property: vocabulary.RecordPath(target.Kind, target.ID),
	}})
	return err
}

// ensureSubject gives a source record its subject on its OWN write: a record
// whose provider carries nothing shared (a contact with neither email nor
// phone) still describes a person, and refusing the write would lose the
// record instead of the link. A record whose target was
// deleted is pointed again the same way. A record that offers NOTHING AT ALL,
// and one whose probe found several candidates under `onAmbiguous: park`,
// leaves the slot unset and is resolved again on its next write (matchOrMint,
// records 0087 and 0103).
//
// It writes the value INTO THE ROW the caller is about to fold, and never
// through a nested write: the subject is one of the source record's own
// properties now, so it travels in that record's delta like every other value.
// The write's own value wins — a caller that named a subject reaches here with
// the property already set — and the caller recomputes the subject after the
// row is stored, so no recompute happens here. It reports whether it set the
// property, so the caller can credit the write in the manager ledger, and
// whether the source PARKED on an ambiguous probe, so the caller can mark it
// once the row is stored (ambiguous.go).
func (t *txn) ensureSubject(sp *applySpec, row *erow, m *vocabulary.Mapping) (set, parked bool, err error) {
	if err := t.lockRecord(sp.ref()); err != nil {
		return false, false, err
	}
	if path := referencePathOf(row.Props[m.Property]); path != "" {
		// A value naming a live record stands, and the id it names is RESOLVED
		// first: a merge repoints nothing, so a subject that was merged away
		// still spells the loser and asking the literal id for liveness would
		// call the record unpointed and mint a duplicate over the merge. The
		// stored value is left exactly as written; resolution stays read-side.
		//
		// One naming a tombstone with no trail behind it (or nothing) is
		// re-resolved, exactly as an unset property is.
		if kind, id, ok := vocabulary.SplitRecordPath(path); ok {
			live, err := t.liveCanonical(eref{Kind: kind, ID: id})
			if err != nil {
				return false, false, err
			}
			if live.ID != "" {
				return false, false, nil
			}
		}
	}
	// A REQUIRED slot has to be filled or the record does not land at all, so
	// a kind that declares its own subject reference `required:` (every bundle
	// written before record 96 does) keeps the old unconditional mint. The
	// slot a mapping synthesizes is not required, and there the write may
	// leave it unset: a source that offers nothing mints nothing, and an
	// ambiguous probe does what the mapping's onAmbiguous says, parking by
	// default (records 0087 and 0103).
	slot, declared := sp.ty.Prop(m.Property)
	target, parked, err := t.matchOrMint(row, sp.ty, m, declared && slot.Required)
	if err != nil || target == "" {
		return false, parked, err
	}
	// THE STORED SHAPE, not the bare path. This runs AFTER coercion (write.go
	// ensureSubject), so nothing downstream normalizes what it writes: a bare
	// string here would be the one value in the store that the one-shape rule
	// does not hold for (decision 0044), readable only because every reader
	// still tolerates the old spelling.
	row.Props[m.Property] = referenceValueOf(vocabulary.RecordPath(m.To, target))
	return true, false, nil
}

// matchOrMint resolves an unpointed source record to its subject: the match
// probes run in order, and the first probe whose values find candidates
// decides: exactly one is taken. It returns the subject's id and writes
// nothing onto the source: the caller stores the pointer. The caller holds the
// record's lock.
//
// Nothing matched is where mustMint rules. A caller that DEMANDS a subject —
// the subject hop, or a source kind whose slot is declared `required:` — mints
// a shell, which is the hub's growth and how a person who exists nowhere else
// comes to exist. Every other caller gets "" and leaves the slot unset:
//
//   - SEVERAL candidates is not no candidates (#577). Minting a third person
//     out of two who share an address, and then unioning that address onto the
//     shell, made every later probe on it ambiguous too — convergence that
//     degraded as more sources synced. What happens instead is the mapping's
//     `onAmbiguous` (record 0103): `park`, the default, leaves the slot unset
//     for the next write after the owner merges the two; `oldest` links the
//     candidate created first; `mint` mints a shell whatever the caller
//     demands. A demanding caller under `park` still mints, because it cannot
//     wait. None of the three puts the shared value on a new target: that is
//     recompute's rule (withheldElsewhere), not this function's.
//   - A source with NOTHING TO OFFER mints nothing — the empty shells #578
//     counts, cut off at their source: 614 Slack users with no profile at all
//     each minted an empty person. A record that carries no probe value and no
//     mapped value says nothing about any subject, and a shell born from it is
//     an empty row nothing can ever match.
//
// Both are recoverable on the next write of the source, because an unset slot
// is resolved again exactly as an absent one is.
func (t *txn) matchOrMint(src *erow, srcTy *vocabulary.Kind, m *vocabulary.Mapping, mustMint bool) (target string, parked bool, err error) {
	// Concurrent resolution serializes per subject type: two syncs racing
	// the same new person must probe one after the other, so the second
	// finds the shell the first minted. Coarse, and fine at personal scale.
	if err := t.lockKey("subject|" + m.To); err != nil {
		return "", false, err
	}
	target, candidates, err := t.matchSubject(src, srcTy, m)
	if err != nil || target != "" {
		return target, false, err
	}
	ambiguous := len(candidates) > 1
	switch {
	case ambiguous && m.OnAmbiguous == vocabulary.OnAmbiguousOldest:
		target, err := t.oldestOf(m.To, candidates)
		return target, false, err
	case ambiguous && m.OnAmbiguous == vocabulary.OnAmbiguousMint:
		// Mints below, whatever the caller demands.
	case !mustMint && (ambiguous || !sourceOffers(src, srcTy, m)):
		return "", ambiguous, nil
	}
	// The shell carries no properties, so a subject kind with a `required:`
	// property and no `default:` refuses it and the source write fails with
	// it. That is the declaration's own contract: a kind nothing can create
	// empty is not one a mapping can mint a subject of.
	shell, err := t.put(substrate.PutInput{Kind: m.To})
	if err != nil {
		return "", false, fmt.Errorf("substrate/engine: shell subject for %s: %w", src.ID, err)
	}
	return shell.ID, false, nil
}

// oldestOf picks the candidate created first, the id breaking a tie. Ids are
// random, so creation is the one order among candidates that means something:
// a shell an earlier ambiguity minted is always younger than the people it
// could not tell apart.
func (t *txn) oldestOf(kind string, candidates []string) (string, error) {
	var id string
	err := t.row(`
		SELECT id FROM records
		WHERE kind = $1 AND id = ANY($2::text[]) AND deleted_at IS NULL
		ORDER BY created_at, id LIMIT 1`, kind, candidates).Scan(&id)
	return id, err
}

// matchSubject runs the mapping's probes against the target kind the
// transaction's declarations hold, "" when nothing decides. Only an
// EXACTLY-ONE candidate set links; `candidates` is the other way of deciding
// nothing (the deciding probe's SEVERAL hits, in id order), which the caller
// must not confuse with a probe that found none (nil).
func (t *txn) matchSubject(src *erow, srcTy *vocabulary.Kind, m *vocabulary.Mapping) (string, []string, error) {
	to, ok := t.declarations().ByIdentity(m.To)
	if !ok {
		return "", nil, nil
	}
	for _, probe := range m.Match {
		values := probeValues(srcTy, src, probe)
		if len(values) == 0 {
			continue
		}
		tp, ok := to.Props[probe.To]
		if !ok {
			continue
		}
		candidates, err := t.probeCandidates(m.To, tp, values)
		if err != nil {
			return "", nil, err
		}
		if len(candidates) == 0 {
			continue
		}
		// The first probe whose values find candidates decides.
		if len(candidates) == 1 {
			return candidates[0], nil, nil
		}
		sort.Strings(candidates)
		return "", candidates, nil
	}
	return "", nil, nil
}

// sourceOffers reports whether a source record carries anything its mapping
// can use: one probe value, or one mapped path with a value in it. An empty
// list and an empty string are nothing, the same as an absent property.
//
// A mapping with neither probes nor map rules is link-only: it carries
// structure and copies nothing, so every record of its source kind describes a
// subject by being one, and this answers yes for all of them.
func sourceOffers(src *erow, srcTy *vocabulary.Kind, m *vocabulary.Mapping) bool {
	if len(m.Match) == 0 && len(m.Map) == 0 {
		return true
	}
	for _, probe := range m.Match {
		if len(probeValues(srcTy, src, probe)) > 0 {
			return true
		}
	}
	for _, name := range m.MapOrder {
		if carriesValue(contributionOf(mappedSource{row: src, m: m}, name)) {
			return true
		}
	}
	return false
}

// carriesValue reports whether an evaluated path found anything to write. An
// empty list is what a repeated property with no entries evaluates to, and an
// empty string what a provider writes for a field its payload left blank;
// neither says anything about a subject.
func carriesValue(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case []any:
		return len(x) > 0
	case string:
		return strings.TrimSpace(x) != ""
	case map[string]any:
		return len(x) > 0
	}
	return true
}

// probeValues extracts one probe's identifier values from a source record,
// normalized: strings trimmed, email values lowercased. The loader keeps
// probes in the short-string family, so everything here is a string.
func probeValues(srcTy *vocabulary.Kind, src *erow, probe vocabulary.MatchRule) []string {
	sp, _, err := vocabulary.PathProperty(srcTy, probe.From)
	if err != nil {
		return nil
	}
	raw := evalPath(src, probe.From)
	items, ok := raw.([]any)
	if !ok {
		items = []any{raw}
	}
	seen := map[string]bool{}
	var out []string
	for _, item := range items {
		s, _ := item.(string)
		s = strings.TrimSpace(s)
		if sp.Datatype == vocabulary.DatatypeEmail {
			s = strings.ToLower(s)
		}
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// probeCandidates lists the distinct live records of the target type whose
// probe property carries any of the values (repeated: containment; scalar:
// equality).
func (t *txn) probeCandidates(toIdentity string, tp *vocabulary.Property, values []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, v := range values {
		var rows *sql.Rows
		var err error
		if tp.Repeated {
			needle, merr := json.Marshal([]string{v})
			if merr != nil {
				return nil, merr
			}
			rows, err = t.query(`
				SELECT id FROM records
				WHERE kind = $1 AND deleted_at IS NULL AND props->$2 @> $3::jsonb
				ORDER BY id`, toIdentity, tp.Name, needle)
		} else {
			rows, err = t.query(`
				SELECT id FROM records
				WHERE kind = $1 AND deleted_at IS NULL AND props->>$2 = $3
				ORDER BY id`, toIdentity, tp.Name, v)
		}
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
	}
	return out, nil
}

// checkSubjectWrite refuses a change of the subject reference: it is set when
// the record is created, and moved only by merge and split. Re-asserting the same
// target is what every re-sync does, so only a DIFFERENT LIVE target is refused.
// BOTH SIDES are resolved through the former-id trail before they are compared,
// because a merge moves the subject out from under a connector that is still
// syncing the id it first saw: the stored pointer and the incoming one may spell
// one record two ways. A tombstoned target is no pointer at all.
func (t *txn) checkSubjectWrite(sp *applySpec, property string, dst eref) error {
	if sp.existing == nil {
		return nil
	}
	cur, err := t.subjectTargetOf(sp.ref(), property)
	if err != nil {
		return err
	}
	if cur.ID == "" {
		return nil
	}
	if dst.ID != "" {
		if dst, err = t.canonicalOf(dst); err != nil {
			return err
		}
	}
	if cur == dst {
		return nil
	}
	return fmt.Errorf("%w: %s's subject is %s; re-pointing is split + merge, not a write",
		substrate.ErrGuard, sp.id, cur.ID)
}

// --- path evaluation ---------------------------------------------------------

// hotValue reads a column-backed property off a stored row in the form the
// write path takes it back: RFC 3339 for the instants, the string itself for
// title and body. nil means the row carries none.
func hotValue(row *erow, name string) any {
	switch name {
	case substrate.PropTitle:
		if row.Title == "" {
			return nil
		}
		return row.Title
	case substrate.PropBody:
		if row.Body == "" {
			return nil
		}
		return row.Body
	}
	var ts *time.Time
	switch name {
	case substrate.PropAt:
		ts = row.At
	case substrate.PropEndsAt:
		ts = row.EndsAt
	case substrate.PropDueAt:
		ts = row.DueAt
	}
	if ts == nil {
		return nil
	}
	return ts.UTC().Format(time.RFC3339Nano)
}

// isHotProp reports whether name is one of the column-backed properties.
func isHotProp(name string) bool {
	return name == substrate.PropTitle || name == substrate.PropBody || isHotTime(name)
}

// evalPath evaluates a declared path against a stored row: `a` reads a
// property (column-backed included), `a.b` walks into an object property,
// `a[].b` extracts one field across a repeated one — nil when absent, and the
// `[]` form yields a list.
func evalPath(row *erow, p vocabulary.Path) any {
	if p.Field == "" {
		if isHotProp(p.Prop) {
			return hotValue(row, p.Prop)
		}
		return row.Props[p.Prop]
	}
	v := row.Props[p.Prop]
	if p.OverList {
		items, _ := v.([]any)
		var out []any
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if fv, has := m[p.Field]; has && fv != nil {
				out = append(out, fv)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m[p.Field]
}

// --- recompute ---------------------------------------------------------------

// mappedSource is one live source record joined to the target being
// recomputed, joined through its own mapping's subject reference.
type mappedSource struct {
	row *erow
	m   *vocabulary.Mapping
	// actor is the source's own writer: the most recent property_managers
	// row on the record, falling back to the first declared actor of the
	// mapping's authority. Provenance is per property, and the changelog must
	// say a name came from Google, not from "the system".
	actor string
}

// changedMappingTargets lists the target kinds whose mapping set differs
// between two registries: a mapping added, removed or redefined, in identity
// order.
func changedMappingTargets(old, cand *vocabulary.Registry) []string {
	targets := map[string]bool{}
	for _, m := range old.Mappings() {
		targets[m.To] = true
	}
	for _, m := range cand.Mappings() {
		targets[m.To] = true
	}
	var out []string
	for _, k := range sortedKeys(targets) {
		if !reflect.DeepEqual(old.MappingsTo(k), cand.MappingsTo(k)) {
			out = append(out, k)
		}
	}
	return out
}

// mappedInputs is what a target's offers and its accepted values are both
// computed from: its live sources, latest write first, and the union of the
// properties its mappings map.
type mappedInputs struct {
	row       *erow
	ty        *vocabulary.Kind
	srcs      []mappedSource
	props     []string
	unionProp map[string]bool
	// probed is the target properties some mapping's probe matches on, the
	// ones withheldElsewhere guards.
	probed map[string]*vocabulary.Property
}

// mappedInputsOf loads a target's mapped inputs, nil when there is nothing to
// compute: the target is not live, nothing maps onto its kind, or every
// mapping onto it is link-only.
func (t *txn) mappedInputsOf(target eref) (*mappedInputs, error) {
	row, err := t.loadRow(target, false)
	if err != nil || row == nil || row.DeletedAt != nil {
		return nil, err
	}
	reg := t.declarations()
	ty, err := t.resolveType(row.Kind)
	if err != nil {
		return nil, err
	}
	mappings := reg.MappingsTo(ty.Identity)
	if len(mappings) == 0 {
		return nil, nil
	}
	srcs, err := t.subjectSourcesOf(target, mappings)
	if err != nil {
		return nil, err
	}
	// Latest write wins, deterministically: sources written in one
	// transaction share updated_at, so equal instants order by type then id
	// — a tie-break, not a timestamp.
	sort.SliceStable(srcs, func(i, j int) bool {
		a, b := srcs[i], srcs[j]
		if !a.row.UpdatedAt.Equal(b.row.UpdatedAt) {
			return a.row.UpdatedAt.After(b.row.UpdatedAt)
		}
		if a.row.Kind != b.row.Kind {
			return a.row.Kind < b.row.Kind
		}
		return a.row.ID < b.row.ID
	})

	// The mapped property set is the union across mappings; a property is
	// union-merged the moment any rule says so.
	propSet := map[string]bool{}
	unionProp := map[string]bool{}
	for _, m := range mappings {
		for _, name := range m.MapOrder {
			propSet[name] = true
			if m.Map[name].Merge == vocabulary.MergeUnion {
				unionProp[name] = true
			}
		}
	}
	props := sortedKeys(propSet)
	if len(props) == 0 {
		// A link-only mapping carries structure and copies nothing.
		return nil, nil
	}
	probed := map[string]*vocabulary.Property{}
	for _, m := range mappings {
		for _, probe := range m.Match {
			if tp, ok := ty.Props[probe.To]; ok {
				probed[probe.To] = tp
			}
		}
	}
	return &mappedInputs{row: row, ty: ty, srcs: srcs, props: props, unionProp: unionProp, probed: probed}, nil
}

// syncOffersOf is the offers half of recompute alone: the target's
// property_offers rows from its live sources, and nothing else. It never
// reaches t.patch, so no accepted value moves and nothing appends, which is
// what lets a rebuild and an import derive the table again
// (rebuild.go rederiveOffers).
func (t *txn) syncOffersOf(target eref) error {
	in, err := t.mappedInputsOf(target)
	if err != nil || in == nil {
		return err
	}
	return t.syncOffers(target, in.props, in.unionProp, in.srcs)
}

// recompute recomputes targetID's mapped properties from its live sources.
// Pure function of the live records, with yield: a manager row above the
// machine tier (the owner above all, a bundle's pin beside it) keeps its
// property, and what the recompute would have written stays legible as the
// source's offer row. A record with zero live
// sources keeps only what was written to it directly.
func (t *txn) recompute(target eref) error {
	if t.recomputing {
		return nil
	}
	if err := t.recomputeValues(target); err != nil {
		return err
	}
	// LAST, and outside the value half: the mark is read off what the
	// recompute leaves behind — no live source, and nothing above the machine
	// tier holding a property (orphans.go). It runs even where there was
	// nothing to recompute, because a link-only mapping's target orphans the
	// same way a copying one's does.
	return t.syncOrphaned(target)
}

// recomputeValues is recompute's value half: the offers, and the mapped
// properties the machine tier still holds.
func (t *txn) recomputeValues(target eref) error {
	in, err := t.mappedInputsOf(target)
	if err != nil || in == nil {
		return err
	}

	// Offers first, accepted or yielded: one row per (property,
	// actor), so a held value's alternatives are visible on every read.
	if err := t.syncOffers(target, in.props, in.unionProp, in.srcs); err != nil {
		return err
	}

	managers, err := t.managersOf(target)
	if err != nil {
		return err
	}

	patch := map[string]any{}
	overrides := map[string]substrate.Actor{}
	for _, name := range in.props {
		if m, held := managers[name]; held && m.tier != substrate.TierMachine {
			continue // yield: the offer above is the whole record of it
		}
		cands := contributionsFor(name, in.srcs)
		if tp := in.probed[name]; tp != nil {
			if cands, err = t.withheldElsewhere(in.row, tp, cands); err != nil {
				return err
			}
		}
		value, actor := selectValue(in.unionProp[name], cands)
		// nil deletes: release-by-omission. Not on a required property, which
		// the write path refuses to empty (checkRequiredProps): the last value
		// stands, and its property_managers row goes on crediting the actor
		// whose source has gone, until something writes it. Otherwise the
		// delete or sweep that removed the property's last source would fail
		// on the refusal, the sweep on every pass.
		if value == nil {
			if p, ok := in.ty.Props[name]; ok && p.Required {
				continue
			}
		}
		patch[name] = value
		if value != nil {
			overrides[name] = substrate.Actor(actor)
		}
	}
	if len(patch) == 0 {
		return nil
	}

	// Recompute writes as `system`, in the trigger's transaction, through
	// the ordinary write path — no-op suppression holds, so re-syncing
	// identical data writes nothing — and never triggers recompute.
	was := t.actor
	t.recomputeInitiator = was
	t.actor = substrate.ActorSystem
	t.recomputing, t.recomputeManagers = true, overrides
	defer func() {
		t.actor = was
		t.recomputing, t.recomputeManagers = false, nil
		t.recomputeInitiator = ""
	}()
	_, err = t.patch(target, substrate.PatchInput{Properties: patch})
	return err
}

// subjectSourcesOf loads the live records mapped onto a record, each joined
// through its own mapping's declared subject PROPERTY, and credits each to
// the actor its contributions are attributed to.
//
// The sites are subjectSourceSites'; this is the loading half, kept apart
// because asking WHETHER a target still has a live source (orphans.go
// isOrphan) must not pay for the rows and the actor lookups.
func (t *txn) subjectSourcesOf(target eref, mappings []*vocabulary.Mapping) ([]mappedSource, error) {
	found, err := t.subjectSourceSites(target, mappings)
	if err != nil {
		return nil, err
	}
	bySlot := map[sourceSlot]*vocabulary.Mapping{}
	for _, m := range mappings {
		bySlot[sourceSlot{m.From, m.Property}] = m
	}
	var out []mappedSource
	for _, c := range found {
		r, err := t.loadRow(eref{Kind: c.typ, ID: c.id}, false)
		if err != nil {
			return nil, err
		}
		if r == nil {
			continue
		}
		m := bySlot[sourceSlot{c.typ, c.rel}]
		actor, err := t.sourceActor(r.ref(), m)
		if err != nil {
			return nil, err
		}
		out = append(out, mappedSource{row: r, m: m, actor: actor})
	}
	return out, nil
}

// sourceSlot addresses one mapping's entry point: the source KIND and the
// subject property on it. One source kind may reach one target through two
// subject properties (record 49), so the property is half the key.
type sourceSlot struct{ kind, property string }

// sourceSite is one live source record's link at one slot.
type sourceSite struct{ rel, id, typ string }

// subjectSourceSites lists the live records whose mapping-owned subject slot
// names a record — an ordinary reference with the same name on a different
// declaring kind is not one.
//
// It matches every id the target has ever had, not only the canonical one: a
// merge does not repoint reference values (they resolve forward through the
// former-id trail on read), so a source synced before its subject won a merge
// still names the loser id, and recomputing from the canonical id alone would
// drop that source's contributions on the floor — or, in the orphan mark's
// reading, would call a described record undescribed.
func (t *txn) subjectSourceSites(target eref, mappings []*vocabulary.Mapping) ([]sourceSite, error) {
	// Keyed by the pair, because one source kind may reach one target through
	// two subject properties (record 49) and keying by kind alone would drop
	// the second mapping's sources.
	bySlot := map[sourceSlot]*vocabulary.Mapping{}
	for _, m := range mappings {
		bySlot[sourceSlot{m.From, m.Property}] = m
	}
	ids, err := t.idsOf(target)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return nil, err
	}
	rows, err := t.query(`
		SELECT r.property, x.id, x.kind FROM refs r JOIN records x ON x.kind = r.src_kind AND x.id = r.src
		WHERE r.dst_kind = $1 AND r.dst IN (SELECT jsonb_array_elements_text($2::jsonb))
		  AND r.path = '' AND x.deleted_at IS NULL ORDER BY x.kind, x.id`,
		target.Kind, raw)
	if err != nil {
		return nil, err
	}
	var found []sourceSite
	// Deduped per (kind, id, PROPERTY): one row reaching two targets through
	// two subject slots is a source of both, and a key without the property
	// would drop the second slot's contributions. The refs index holds one row
	// per site, so the same slot cannot repeat here.
	seen := map[sourceSlot]bool{}
	for rows.Next() {
		var c sourceSite
		if err := rows.Scan(&c.rel, &c.id, &c.typ); err != nil {
			_ = rows.Close()
			return nil, err
		}
		site := sourceSlot{c.typ + "/" + c.id, c.rel}
		if _, ok := bySlot[sourceSlot{c.typ, c.rel}]; !ok || seen[site] {
			continue
		}
		seen[site] = true
		found = append(found, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	return found, nil
}

// sourceActor is the actor a source record's contributions are attributed
// to: the most recent property_managers row of the record itself, falling back
// to the first declared actor of the package that owns the SOURCE KIND.
//
// The source's package, not the mapping's: since record 49 the mapping is the
// TARGET owner's declaration, so crediting its package would say a synced name
// came from the repository's own vocabulary rather than from Google.
//
// THE SUBJECT SLOT IS NOT A CONTRIBUTION and is excluded by name. Its manager
// is the mapping itself (record 96), written by the engine as the link
// resolves, and it is written LAST — so without this the most recent row on
// every freshly synced source record would be the engine's own bookkeeping,
// and every value that record offers would be attributed to the mapping
// instead of to the connector that fetched it.
func (t *txn) sourceActor(src eref, m *vocabulary.Mapping) (string, error) {
	var actor string
	err := t.row(`
		SELECT actor FROM property_managers WHERE record_kind = $1 AND record_id = $2 AND property <> $3
		ORDER BY updated_at DESC, property LIMIT 1`, src.Kind, src.ID, m.Property).Scan(&actor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if actor != "" {
		return actor, nil
	}
	if g, ok := t.declarations().PackageByName(vocabulary.KindPackage(m.From)); ok && len(g.Actors) > 0 {
		return g.Actors[0], nil
	}
	return string(substrate.ActorSystem), nil
}

// contributionOf evaluates one source's contribution to one target property,
// nil when its mapping does not map it or the path finds nothing.
//
// `merge: first` is applied HERE, per rule and not per property, because it is
// a statement about the SOURCE's repetition and not about how the target
// combines what its sources offer: the head is what this source contributes,
// and the selection across sources is the ordinary latest-write-wins.
func contributionOf(s mappedSource, name string) any {
	rule, ok := s.m.Map[name]
	if !ok {
		return nil
	}
	v := evalPath(s.row, rule.Path)
	if rule.Merge == vocabulary.MergeFirst {
		return firstItem(v)
	}
	return v
}

// withheldElsewhere drops from each contribution the PROBED values another
// live target already holds and this one does not (#577, record 0103). A
// probe on a value two targets hold is ambiguous, so writing a value onto a
// second target is what turns one source's link into every later source's
// ambiguity: the shell an ambiguous probe minted used to take the shared
// address through `merge: union`, and the next probe on it saw three
// candidates. The rule is per value and reads the target's stored value, so a
// duplicate that already exists stays where it is (the owner settles it with
// merge) and only a new one is refused.
//
// A withheld value is not lost: the source still carries it, its offer row
// still does, and a read shows it as an alternative beside the stored value,
// which is where the owner adopts it by writing it. A contribution left with
// nothing contributes nothing, exactly as an empty source does.
//
// Values compare the way a probe compares them (probeKey), because the
// question is what the next probe would find.
func (t *txn) withheldElsewhere(target *erow, tp *vocabulary.Property, cands []contribution) ([]contribution, error) {
	held := map[string]bool{}
	for _, item := range asItems(target.Props[tp.Name]) {
		if key, ok := probeKey(tp, item); ok {
			held[key] = true
		}
	}
	withheld := map[string]bool{}
	decided := map[string]bool{}
	out := make([]contribution, 0, len(cands))
	for _, c := range cands {
		items, isList := c.value.([]any)
		if !isList {
			items = []any{c.value}
		}
		if len(items) == 0 {
			out = append(out, c)
			continue
		}
		kept := make([]any, 0, len(items))
		for _, item := range items {
			key, ok := probeKey(tp, item)
			if !ok || held[key] {
				kept = append(kept, item)
				continue
			}
			if !decided[key] {
				holders, err := t.probeCandidates(target.Kind, tp, []string{key})
				if err != nil {
					return nil, err
				}
				for _, h := range holders {
					if h != target.ID {
						withheld[key] = true
						break
					}
				}
				decided[key] = true
			}
			if !withheld[key] {
				kept = append(kept, item)
			}
		}
		switch {
		case len(kept) == 0:
			continue
		case isList:
			c.value = kept
		default:
			c.value = kept[0]
		}
		out = append(out, c)
	}
	return out, nil
}

// probeKey is one value as a probe looks it up: a trimmed string, lowercased
// for an email property, and not a probe value at all when it is empty or not
// a string.
func probeKey(tp *vocabulary.Property, v any) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if tp.Datatype == vocabulary.DatatypeEmail {
		s = strings.ToLower(s)
	}
	return s, s != ""
}

// firstItem is the head of a repeated contribution: a list's first non-null
// item, a scalar itself, and nil for a list with nothing in it — an EMPTY
// source contributes nothing rather than an empty value, so a contact whose
// `names[]` is empty leaves the person's name to whatever else offers one
// instead of clearing it.
func firstItem(v any) any {
	items, ok := v.([]any)
	if !ok {
		return v
	}
	for _, item := range items {
		if item != nil {
			return item
		}
	}
	return nil
}

// asItems renders a contribution as union items: a list is its items, a
// scalar path contributes a singleton.
func asItems(v any) []any {
	if v == nil {
		return nil
	}
	if items, ok := v.([]any); ok {
		return items
	}
	return []any{v}
}

// contribution is one candidate value for one target property: a live
// source's mapped path.
type contribution struct {
	updatedAt time.Time
	// a, b are the deterministic tie-break keys: the source's type and id.
	a, b  string
	actor string
	value any
}

// contributionsFor collects one property's candidates across the live
// sources, ordered latest-updated first — equal instants order by the keys,
// so the order is a tie-break, not a timestamp.
func contributionsFor(name string, srcs []mappedSource) []contribution {
	var out []contribution
	for _, s := range srcs {
		if v := contributionOf(s, name); v != nil {
			out = append(out, contribution{
				updatedAt: s.row.UpdatedAt, a: s.row.Kind, b: s.row.ID, actor: s.actor, value: v,
			})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		x, y := out[i], out[j]
		if !x.updatedAt.Equal(y.updatedAt) {
			return x.updatedAt.After(y.updatedAt)
		}
		if x.a != y.a {
			return x.a < y.a
		}
		return x.b < y.b
	})
	return out
}

// selectValue applies the selection to one property's ordered candidates:
// atomic takes the first candidate whole, union takes the deduped
// concatenation of every candidate's items, attributed to the first
// contributing one. nil, "" when nothing live carries the property.
func selectValue(union bool, cands []contribution) (any, string) {
	if !union {
		if len(cands) == 0 {
			return nil, ""
		}
		return cands[0].value, cands[0].actor
	}
	var items []any
	actor := ""
	for _, c := range cands {
		contributed := asItems(c.value)
		if len(contributed) == 0 {
			continue
		}
		if actor == "" {
			actor = c.actor
		}
		for _, item := range contributed {
			dup := false
			for _, have := range items {
				if jsonEqual(have, item) {
					dup = true
					break
				}
			}
			if !dup {
				items = append(items, item)
			}
		}
	}
	if len(items) == 0 {
		return nil, ""
	}
	return items, actor
}

// property_offers holds ONE population, because there is no bundle-offer
// write-kind: recompute's projection of what each live source's actor would
// write, rebuilt and pruned on every recompute, the rows behind
// propertyMeta's alternatives. A bundle contributes by shipping its own
// source type + recordmapping.

// syncOffers upserts one property_offers row per (property, actor) a live
// source contributes — computed with the same selection, restricted to that
// actor's sources — and deletes the rows nothing live backs any more.
// Unchanged offers write nothing.
//
// A row's updated_at is the updated_at of the latest source record carrying
// the property for that actor, never the transaction's clock, and its source
// is that record's path: both are a function of the live records exactly as
// the value is, so a rebuild, which derives the table again (rebuild.go
// rederiveOffers), reproduces them.
func (t *txn) syncOffers(target eref, props []string, unionProp map[string]bool, srcs []mappedSource) error {
	current := map[offerKey]offer{}
	for _, name := range props {
		actors := map[string]bool{}
		for _, s := range srcs {
			if actors[s.actor] {
				continue
			}
			var mine []mappedSource
			for _, x := range srcs {
				if x.actor == s.actor {
					mine = append(mine, x)
				}
			}
			cands := contributionsFor(name, mine)
			if v, _ := selectValue(unionProp[name], cands); v != nil {
				// cands[0] is the latest source CARRYING the path. For a union
				// property it may carry an empty list and contribute no item,
				// so the stamp is not always a contributing source's; it is
				// the same on the live path and the rebuild, which is what
				// the stamp has to be.
				current[offerKey{name, s.actor}] = offer{
					value: v, at: cands[0].updatedAt,
					source: vocabulary.RecordPath(cands[0].a, cands[0].b),
				}
			}
			actors[s.actor] = true
		}
	}
	rows, err := t.query(`SELECT property, actor FROM property_offers WHERE record_kind = $1 AND record_id = $2`,
		target.Kind, target.ID)
	if err != nil {
		return err
	}
	var stale []offerKey
	for rows.Next() {
		var k offerKey
		if err := rows.Scan(&k.property, &k.actor); err != nil {
			_ = rows.Close()
			return err
		}
		if _, live := current[k]; !live {
			stale = append(stale, k)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, k := range stale {
		if _, err := t.exec(`
			DELETE FROM property_offers
			WHERE record_kind = $1 AND record_id = $2 AND property = $3 AND actor = $4`,
			target.Kind, target.ID, k.property, k.actor); err != nil {
			return err
		}
	}
	for _, k := range sortedOfferKeys(current) {
		o := current[k]
		raw, err := jsonb(o.value)
		if err != nil {
			return err
		}
		if _, err := t.exec(`
			INSERT INTO property_offers (record_kind, record_id, property, actor, value, updated_at, source)
			VALUES ($1, $2, $3, $4, $5::jsonb, $6, $7)
			ON CONFLICT (repository, record_kind, record_id, property, actor) DO UPDATE SET
				value = EXCLUDED.value, updated_at = EXCLUDED.updated_at, source = EXCLUDED.source
			WHERE property_offers.value IS DISTINCT FROM EXCLUDED.value
			   OR property_offers.updated_at IS DISTINCT FROM EXCLUDED.updated_at
			   OR property_offers.source IS DISTINCT FROM EXCLUDED.source`,
			target.Kind, target.ID, k.property, k.actor, raw, o.at, o.source); err != nil {
			return err
		}
	}
	return nil
}

// offerKey addresses one property_offers row.
type offerKey struct{ property, actor string }

// offer is one row's derived content: the value, its source's stamp, and the
// source itself — the record path of the live source the value and the stamp
// are read from (cands[0]: the latest source carrying the path), which is what
// lets a read say WHICH mirror an alternative came from rather than only which
// actor wrote it, when eight mirrors share one actor (record 0094).
type offer struct {
	value  any
	at     time.Time
	source string
}

func sortedOfferKeys(m map[offerKey]offer) []offerKey {
	out := make([]offerKey, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].property != out[j].property {
			return out[i].property < out[j].property
		}
		return out[i].actor < out[j].actor
	})
	return out
}

// managerRow is one property's manager as recompute reads it: the actor for
// attribution, the stored tier for yield.
type managerRow struct {
	actor string
	tier  substrate.Tier
	// principal is the token id the write stood behind, empty where none did.
	// A kind move carries it with the manager (move.go), because who wrote a
	// value is the whole row and not two thirds of it.
	principal string
}

// managersOf reads the target's property-manager ledger, property → manager.
// The tier column is NOT NULL, so the row is the whole answer.
func (t *txn) managersOf(ref eref) (map[string]managerRow, error) {
	rows, err := t.query(
		`SELECT property, actor, tier, coalesce(principal, '') FROM property_managers WHERE record_kind = $1 AND record_id = $2`,
		ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]managerRow{}
	for rows.Next() {
		var property, actor, tier, principal string
		if err := rows.Scan(&property, &actor, &tier, &principal); err != nil {
			return nil, err
		}
		out[property] = managerRow{actor: actor, tier: substrate.Tier(tier), principal: principal}
	}
	return out, rows.Err()
}

// recomputeSubjectOf recomputes the record a source record points at. It
// runs on every source write, changed or not, so the subject converges even
// when the write was a no-op re-sync — and because recompute itself flows
// through the ordinary write path, an unchanged recompute writes nothing.
func (t *txn) recomputeSubjectOf(src eref, m *vocabulary.Mapping) error {
	target, err := t.subjectTargetOf(src, m.Property)
	if err != nil || target.ID == "" {
		return err
	}
	return t.recompute(target)
}

// afterTombstone is what every non-fold tombstone owes the mapping graph once
// the entry reporting it is appended: softDelete, the sweep's cascade and a
// merge's loser all call it. The record's own offer rows go, because a
// tombstone is not a live target and a rebuild derives offers for live
// records alone; and its subjects recompute, because as a source it has left
// the live set. It runs after the reporting entry rather than inside the
// tombstone so that the tombstone effect rides that entry and the
// recompute's own patch rides its own.
func (t *txn) afterTombstone(ref eref) error {
	if _, err := t.exec(`DELETE FROM property_offers WHERE record_kind = $1 AND record_id = $2`,
		ref.Kind, ref.ID); err != nil {
		return err
	}
	// And its orphan mark, for the same reason: a tombstone is not a live
	// target, and the mark is a reading of the live set (orphans.go). A put
	// that brings the record back re-derives it on that write.
	if len(t.declarations().MappingsTo(ref.Kind)) > 0 {
		if _, err := t.exec(`UPDATE records SET orphaned_at = NULL WHERE kind = $1 AND id = $2 AND orphaned_at IS NOT NULL`,
			ref.Kind, ref.ID); err != nil {
			return err
		}
	}
	// And its ambiguity mark, as a source (ambiguous.go).
	if len(t.declarations().MappingsFrom(ref.Kind)) > 0 {
		if err := t.markAmbiguous(ref, false); err != nil {
			return err
		}
	}
	return t.recomputeSubjectsOf(ref)
}

// releaseMachineManaged releases the named properties on a record where the
// machine tier manages them: what recompute wrote for mappings a vocabulary
// apply removed (recomputeMappingTargets). Each is nulled through the same
// recomputing patch recompute uses, so the value and its manager row go
// together; a required property keeps its value, as recompute leaves it. A
// property held above the machine tier, or one the machine wrote for no
// mapping, is not named and is not touched.
func (t *txn) releaseMachineManaged(target eref, props []string) error {
	if len(props) == 0 {
		return nil
	}
	row, err := t.loadRow(target, false)
	if err != nil || row == nil || row.DeletedAt != nil {
		return err
	}
	ty, err := t.resolveType(row.Kind)
	if err != nil {
		return err
	}
	managers, err := t.managersOf(target)
	if err != nil {
		return err
	}
	patch := map[string]any{}
	for _, name := range props {
		if m, held := managers[name]; !held || m.tier != substrate.TierMachine {
			continue
		}
		if p, ok := ty.Props[name]; ok && p.Required {
			continue
		}
		patch[name] = nil
	}
	if len(patch) == 0 {
		return nil
	}
	was := t.actor
	t.recomputeInitiator = was
	t.actor = substrate.ActorSystem
	t.recomputing, t.recomputeManagers = true, nil
	defer func() {
		t.actor = was
		t.recomputing, t.recomputeManagers = false, nil
		t.recomputeInitiator = ""
	}()
	_, err = t.patch(target, substrate.PatchInput{Properties: patch})
	return err
}

// recomputeSubjectsOf recomputes every subject a source record points at, one
// per mapping its kind carries (record 49). A kind the registry does not hold
// carries no mapping, so a record of a parked package recomputes nothing.
func (t *txn) recomputeSubjectsOf(src eref) error {
	reg := t.declarations()
	ty, ok := reg.ByIdentity(src.Kind)
	if !ok {
		return nil
	}
	for _, m := range reg.MappingsFrom(ty.Identity) {
		if err := t.recomputeSubjectOf(src, m); err != nil {
			return err
		}
	}
	return nil
}
