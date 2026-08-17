package engine

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The subject edge and its mapping. A
// type carrying a recordmapping records what ONE SOURCE holds; the record its
// declared edge points at is the subject those records describe, and
// recompute carries the mapped properties onto it — yielding to any manager
// row above the machine tier (primitives §6), so a hand edit — the owner's
// or a bundle's — survives a sync, legibly.

// --- subject resolution -----------------------------------------------------

// subjectTargetOf reads the LIVE record a source record points at, "" when it
// points at nothing. A tombstoned target counts as unlinked: the owner deleted
// that person, and a source record must not go on resolving to a dead id or
// refusing every later sync because of one. The ORDER BY is determinism
// insurance — the partial unique index makes a second row impossible, and if
// one ever exists again, every reader must at least agree which it is.
func (t *txn) subjectTargetOf(src eref, rel string) (eref, error) {
	var dst eref
	err := t.row(`
		SELECT e.dst_kind, e.dst FROM edges e JOIN records x ON x.kind = e.dst_kind AND x.id = e.dst
		WHERE e.src_kind = $1 AND e.src = $2 AND e.rel = $3 AND x.deleted_at IS NULL
		ORDER BY e.created_at, e.dst_kind, e.dst LIMIT 1`, src.Kind, src.ID, rel).Scan(&dst.Kind, &dst.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return eref{}, nil
	}
	return dst, err
}

// subjectOf resolves the record a source record describes, matching or
// creating the subject an unlinked record implies and linking it in line
// (proposal §6.2 — every source record has its subject from the first
// moment). It is the ONE HOP edge resolution is allowed: an edge declared
// `to: person` accepts a reference to a record whose own subject edge points
// at a person.
func (t *txn) subjectOf(src *erow, srcTy *vocabulary.Kind) (string, error) {
	m, ok := t.ds.registry().MappingFor(srcTy.Identity)
	if !ok {
		return src.ID, nil
	}
	// Two writers resolving the same unlinked record must not each mint a
	// shell: take the record's lock before looking.
	if err := t.lockRecord(src.ref()); err != nil {
		return "", err
	}
	linked, err := t.subjectTargetOf(src.ref(), m.Edge)
	if err != nil {
		return "", err
	}
	if linked.ID != "" {
		canon, err := t.canonicalOf(linked)
		if err != nil {
			return "", err
		}
		return canon.ID, nil
	}
	target, err := t.matchOrLink(src, srcTy, m)
	if err != nil {
		return "", err
	}
	// A new subject edge is a recompute trigger: the subject takes its
	// properties from the record that just resolved through it. The source
	// row is already stored here — this path only runs during somebody
	// else's write — so recompute sees it.
	if err := t.recompute(eref{Kind: m.To, ID: target}); err != nil {
		return "", err
	}
	return target, nil
}

// ensureSubject gives a source record the subject proposal §6.2 promises it,
// on its OWN write: a record whose provider carries nothing shared — a
// contact with neither email nor phone — still describes a person, and
// refusing the write would lose the record instead of the link. A record
// whose target was deleted is linked again the same way. Called after the
// write's own edges, so an explicit link wins; the caller recomputes the
// subject after the row is stored, so no recompute happens here.
func (t *txn) ensureSubject(sp *applySpec, row *erow, m *vocabulary.Mapping) error {
	if err := t.lockRecord(sp.ref()); err != nil {
		return err
	}
	linked, err := t.subjectTargetOf(sp.ref(), m.Edge)
	if err != nil || linked.ID != "" {
		return err
	}
	_, err = t.matchOrLink(row, sp.ty, m)
	return err
}

// matchOrLink resolves an unlinked source record to its subject: the match
// probes run in order, and the first probe whose values find candidates
// decides — exactly one links, zero or several mint a fresh subject (§6.2).
// A shared family address matching two people creates a third rather than
// guessing; a probe never merges. The caller holds the record's lock.
func (t *txn) matchOrLink(src *erow, srcTy *vocabulary.Kind, m *vocabulary.Mapping) (string, error) {
	// Concurrent resolution serializes per subject type: two syncs racing
	// the same new person must probe one after the other, so the second
	// finds the shell the first minted. Coarse, and fine at personal scale.
	if err := t.lockKey("subject|" + m.To); err != nil {
		return "", err
	}
	target, err := t.matchSubject(src, srcTy, m)
	if err != nil {
		return "", err
	}
	if target == "" {
		// The shell carries no properties, so a subject kind with a `required:`
		// property and no `default:` refuses it and the source write fails with
		// it. That is the declaration's own contract: a kind nothing can create
		// empty is not one a mapping can mint a subject of.
		shell, err := t.put(substrate.PutInput{Kind: m.To})
		if err != nil {
			return "", fmt.Errorf("substrate/engine: shell subject for %s: %w", src.ID, err)
		}
		target = shell.ID
	}
	if err := t.linkSubject(src.ref(), m.Edge, eref{Kind: m.To, ID: target}); err != nil {
		return "", err
	}
	return target, nil
}

// matchSubject runs the mapping's probes against the live target type, ""
// when nothing decides. Only an EXACTLY-ONE candidate set links.
func (t *txn) matchSubject(src *erow, srcTy *vocabulary.Kind, m *vocabulary.Mapping) (string, error) {
	to, ok := t.ds.registry().ByIdentity(m.To)
	if !ok {
		return "", nil
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
			return "", err
		}
		if len(candidates) == 0 {
			continue
		}
		// The first probe whose values find candidates decides.
		if len(candidates) == 1 {
			return candidates[0], nil
		}
		return "", nil
	}
	return "", nil
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

// linkSubject writes a source record's subject edge and records it: the link
// is a graph event the changelog must carry, even when it happens inside
// somebody else's write. Whatever the record pointed at before goes — one
// subject row per record, always.
func (t *txn) linkSubject(src eref, rel string, target eref) error {
	if _, err := t.replaceSingleEdge(rel, src, target); err != nil {
		return err
	}
	if _, err := t.putEdge(rel, src, target, nil, true); err != nil {
		return err
	}
	if err := t.bumpVersion(src); err != nil {
		return err
	}
	return t.appendChange(t.actor, substrate.OpLink, src.ID, src.Kind, map[string]any{
		"rel": rel, "dst": target.ID, "dstType": target.Kind, "subject": true,
	})
}

// refuseSubjectRel rejects the raw link/unlink verbs on a subject edge: it is
// created with its source record and moved only by merge and split, so
// re-pointing runs through the machinery that rewrites every stored edge and
// keeps the former-id trails correct.
func (t *txn) refuseSubjectRel(ty *vocabulary.Kind, rel string, op substrate.Op) error {
	m, ok := t.ds.registry().MappingFor(ty.Identity)
	if !ok || m.Edge != rel {
		return nil
	}
	return fmt.Errorf("%w: %s is %s's subject edge — %s does not move it; split and merge do",
		substrate.ErrGuard, rel, ty.Name, op)
}

// checkSubjectWrite enforces proposal §6.1: a subject edge is set when the
// record is created, and moved only by merge and split. Re-asserting the same
// target is what every re-sync does, so only a DIFFERENT LIVE target is
// refused — the stored dst is compared canonically, because a merge moves the
// subject out from under a connector that is still syncing the id it first
// saw, and a tombstoned target is no link at all.
func (t *txn) checkSubjectWrite(sp *applySpec, rel string, dst eref) error {
	if sp.existing == nil {
		return nil
	}
	cur, err := t.subjectTargetOf(sp.ref(), rel)
	if err != nil {
		return err
	}
	if cur.ID == "" {
		return nil
	}
	if cur, err = t.canonicalOf(cur); err != nil {
		return err
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

// evalPath evaluates a declared path against a stored row (§7.1): `a` reads a
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
// recomputed via its subject edge.
type mappedSource struct {
	row *erow
	m   *vocabulary.Mapping
	// actor is the source's own writer: the most recent property_managers
	// row on the record, falling back to the first declared actor of the
	// mapping's authority. Provenance is per property, and the changelog must
	// say a name came from Google, not from "the system".
	actor string
}

// recompute recomputes targetID's mapped properties from its live sources
// (§7.1, primitives §6). Pure function of the live records, with yield: a
// manager row above the machine tier — the owner above all, a bundle's
// pin beside it — keeps its property, and what the recompute would have
// written stays legible as the source's offer row. A record with zero live
// sources keeps only what was written to it directly.
func (t *txn) recompute(target eref) error {
	if t.recomputing {
		return nil
	}
	row, err := t.loadRow(target, false)
	if err != nil || row == nil || row.DeletedAt != nil {
		return err
	}
	reg := t.ds.registry()
	ty, err := t.ds.resolveType(row.Kind)
	if err != nil {
		return err
	}
	mappings := reg.MappingsTo(ty.Identity)
	if len(mappings) == 0 {
		return nil
	}
	srcs, err := t.subjectSourcesOf(target, mappings)
	if err != nil {
		return err
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
		return nil
	}

	// Offers first, accepted or yielded: one row per (property,
	// actor), so a held value's alternatives are visible on every read.
	if err := t.syncOffers(target, props, unionProp, srcs); err != nil {
		return err
	}

	managers, err := t.managersOf(target)
	if err != nil {
		return err
	}

	patch := map[string]any{}
	overrides := map[string]substrate.Actor{}
	for _, name := range props {
		if m, held := managers[name]; held && m.tier != substrate.TierMachine {
			continue // yield: the offer above is the whole record of it
		}
		value, actor := selectValue(unionProp[name], contributionsFor(name, srcs))
		patch[name] = value // nil deletes: release-by-omission
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
	t.actor = substrate.ActorSystem
	t.recomputing, t.recomputeManagers = true, overrides
	defer func() {
		t.actor = was
		t.recomputing, t.recomputeManagers = false, nil
	}()
	_, err = t.patch(target, substrate.PatchInput{Properties: patch})
	return err
}

// subjectSourcesOf loads the live records mapped onto a record, each joined
// through its own mapping's subject edge — an ordinary edge with the same
// name but a different declaring type is not one.
func (t *txn) subjectSourcesOf(target eref, mappings []*vocabulary.Mapping) ([]mappedSource, error) {
	byFrom := map[string]*vocabulary.Mapping{}
	for _, m := range mappings {
		byFrom[m.From] = m
	}
	rows, err := t.query(`
		SELECT e.rel, x.id, x.kind FROM edges e JOIN records x ON x.kind = e.src_kind AND x.id = e.src
		WHERE e.dst_kind = $1 AND e.dst = $2 AND e.subject AND x.deleted_at IS NULL ORDER BY x.kind, x.id`,
		target.Kind, target.ID)
	if err != nil {
		return nil, err
	}
	type candidate struct{ rel, id, typ string }
	var found []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.rel, &c.id, &c.typ); err != nil {
			_ = rows.Close()
			return nil, err
		}
		m, ok := byFrom[c.typ]
		if !ok || m.Edge != c.rel {
			continue
		}
		found = append(found, c)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	_ = rows.Close()
	var out []mappedSource
	for _, c := range found {
		r, err := t.loadRow(eref{Kind: c.typ, ID: c.id}, false)
		if err != nil {
			return nil, err
		}
		if r == nil {
			continue
		}
		m := byFrom[c.typ]
		actor, err := t.sourceActor(r.ref(), m)
		if err != nil {
			return nil, err
		}
		out = append(out, mappedSource{row: r, m: m, actor: actor})
	}
	return out, nil
}

// sourceActor is the actor a source record's contributions are attributed
// to: the most recent property_managers row of the record itself, falling
// back to the first declared actor of the mapping's authority.
func (t *txn) sourceActor(src eref, m *vocabulary.Mapping) (string, error) {
	var actor string
	err := t.row(`
		SELECT actor FROM property_managers WHERE record_kind = $1 AND record_id = $2
		ORDER BY updated_at DESC, property LIMIT 1`, src.Kind, src.ID).Scan(&actor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	if actor != "" {
		return actor, nil
	}
	if g, ok := t.ds.registry().AuthorityByName(m.Authority); ok && len(g.Actors) > 0 {
		return g.Actors[0], nil
	}
	return string(substrate.ActorSystem), nil
}

// contributionOf evaluates one source's contribution to one target property,
// nil when its mapping does not map it or the path finds nothing.
func contributionOf(s mappedSource, name string) any {
	rule, ok := s.m.Map[name]
	if !ok {
		return nil
	}
	return evalPath(s.row, rule.Path)
}

// asItems renders a contribution as union items: a list is its items, a
// scalar path contributes a singleton (§7.1).
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

// selectValue applies the §7.1 selection to one property's ordered
// candidates: atomic — the first candidate wins whole; union — the deduped
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

// property_offers holds ONE population (ticket 002, ruling A10 — the
// bundle-offer write-kind left v1): recompute's projection of what each
// live source's actor would write — rebuilt and pruned on every recompute,
// the rows behind propertyMeta's alternatives. Extensions contribute by
// shipping their own source type + recordmapping.

// syncOffers upserts one property_offers row per (property, actor) a live
// source contributes — computed with the same selection, restricted to that
// actor's sources — and deletes the rows nothing live backs any more.
// Unchanged offers write nothing.
func (t *txn) syncOffers(target eref, props []string, unionProp map[string]bool, srcs []mappedSource) error {
	current := map[offerKey]any{}
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
			if v, _ := selectValue(unionProp[name], contributionsFor(name, mine)); v != nil {
				current[offerKey{name, s.actor}] = v
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
		raw, err := jsonb(current[k])
		if err != nil {
			return err
		}
		if _, err := t.exec(`
			INSERT INTO property_offers (record_kind, record_id, property, actor, value, updated_at)
			VALUES ($1, $2, $3, $4, $5::jsonb, $6)
			ON CONFLICT (repository, record_kind, record_id, property, actor) DO UPDATE SET
				value = EXCLUDED.value, updated_at = EXCLUDED.updated_at
			WHERE property_offers.value IS DISTINCT FROM EXCLUDED.value`,
			target.Kind, target.ID, k.property, k.actor, raw, t.now); err != nil {
			return err
		}
	}
	return nil
}

// offerKey addresses one property_offers row.
type offerKey struct{ property, actor string }

func sortedOfferKeys(m map[offerKey]any) []offerKey {
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
}

// managersOf reads the target's property-manager ledger, property → manager.
// The tier column is NOT NULL since ticket 002 (destructive re-base; deploy
// wipes repositories), so the row is the whole answer.
func (t *txn) managersOf(ref eref) (map[string]managerRow, error) {
	rows, err := t.query(`SELECT property, actor, tier FROM property_managers WHERE record_kind = $1 AND record_id = $2`,
		ref.Kind, ref.ID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]managerRow{}
	for rows.Next() {
		var property, actor, tier string
		if err := rows.Scan(&property, &actor, &tier); err != nil {
			return nil, err
		}
		out[property] = managerRow{actor: actor, tier: substrate.Tier(tier)}
	}
	return out, rows.Err()
}

// recomputeSubjectOf recomputes the record a source record points at. It
// runs on every source write, changed or not, so the subject converges even
// when the write was a no-op re-sync — and because recompute itself flows
// through the ordinary write path, an unchanged recompute writes nothing.
func (t *txn) recomputeSubjectOf(src eref, m *vocabulary.Mapping) error {
	target, err := t.subjectTargetOf(src, m.Edge)
	if err != nil || target.ID == "" {
		return err
	}
	if target, err = t.canonicalOf(target); err != nil {
		return err
	}
	return t.recompute(target)
}
