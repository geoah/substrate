package engine

import (
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The effect vocabulary: a function body returns a list of effect values and
// the host applies them through the ordinary write path, transactionally with
// the cursor CAS. All seven Dataset mutations are reachable — put, patch,
// delete, link, unlink, merge, split — every one held to the manifest's
// `permissions.writes` allowlist, merge/split additionally to its
// `permissions.mutations` grant. Two contract rules live here: `ifAbsent`
// makes a put create-only (a minting function never resets state owned by
// later stages), and a put or patch addressed to a FORMER id resolves onto
// the canonical winner instead of parking (the deterministic-id trap after a
// merge). The patch effect's `offer` write-kind was REMOVED in v1 (ticket
// 002, ruling A10) — the key is refused at decode naming the removal; an
// bundle contributes by shipping its own source type + recordmapping.
// Nothing here can crash the dispatcher — every error is a delivery error
// the caller retries and parks.

// effect is one decoded effect value.
type effect struct {
	Action string
	Type   string // full identity, already checked against emit
	ID     string
	// IfAbsent makes a put create-only: any existing row — live or
	// tombstoned — is a no-op.
	IfAbsent bool
	// IfVersion, when set, is an optimistic-concurrency precondition on a put
	// or patch: the write applies only if the addressed record's stored version
	// equals it, else the whole delivery fails ErrConflict. It is the safe
	// read-then-conditional-write primitive — read a version through a host
	// read, stage the write guarded by it. A non-existent record reads as
	// version 0.
	IfVersion  *int64
	Properties map[string]any
	Edges      []substrate.EdgeInput
	// Rel/To shape link and unlink; ID is the source record.
	Rel string
	To  substrate.EdgeRef
	// Loser rides a merge (ID is the winner); MergeID rides a split.
	Loser   string
	MergeID string
}

const (
	effectPut    = "put"
	effectPatch  = "patch"
	effectDelete = "delete"
	effectLink   = "link"
	effectUnlink = "unlink"
	effectMerge  = "merge"
	effectSplit  = "split"
)

// effectKeys is the per-action closed key set. patch still RECOGNIZES
// "offer" — solely so the decode error can name its removal instead of
// reporting an anonymous unknown key.
var effectKeys = map[string]map[string]bool{
	effectPut:    {"action": true, "kind": true, "id": true, "ifAbsent": true, "ifVersion": true, "properties": true, "edges": true},
	effectPatch:  {"action": true, "kind": true, "id": true, "properties": true, "offer": true, "ifVersion": true},
	effectDelete: {"action": true, "kind": true, "id": true},
	effectLink:   {"action": true, "kind": true, "id": true, "rel": true, "to": true, "properties": true},
	effectUnlink: {"action": true, "kind": true, "id": true, "rel": true, "to": true},
	effectMerge:  {"action": true, "kind": true, "id": true, "loser": true},
	effectSplit:  {"action": true, "kind": true, "merge": true},
}

// decodeEffects decodes one run's returned values, holding every effect to
// the capability envelope.
func (ds *dataset) decodeEffects(fn *vocabulary.Function, values []any) ([]effect, error) {
	effects := make([]effect, 0, len(values))
	for i, v := range values {
		ef, err := ds.decodeEffect(fn, v)
		if err != nil {
			return nil, fmt.Errorf("effect[%d]: %w", i, err)
		}
		effects = append(effects, ef)
	}
	return effects, nil
}

// decodeEffect turns one returned value into an effect, enforcing the emit
// allowlist and the mutations grant. The id is required on every action but
// split (whose address is the merge record): functions are writers, and
// writers control the ids of what they write — that is what makes replays
// and retries idempotent by construction.
func (ds *dataset) decodeEffect(fn *vocabulary.Function, v any) (effect, error) {
	var ef effect
	m, ok := v.(map[string]any)
	if !ok {
		return ef, fmt.Errorf("an effect is a map, got %T", v)
	}
	ef.Action, _ = m["action"].(string)
	allowed, known := effectKeys[ef.Action]
	if !known {
		return ef, fmt.Errorf("action %q is not put, patch, delete, link, unlink, merge or split", ef.Action)
	}
	for k := range m {
		if !allowed[k] {
			return ef, fmt.Errorf("%s: unknown key %q", ef.Action, k)
		}
	}
	name, _ := m["kind"].(string)
	if name == "" {
		return ef, fmt.Errorf("kind is required")
	}
	ty, err := ds.resolveType(name)
	if err != nil {
		return ef, err
	}
	ef.Type = ty.Identity
	if !emitAllows(fn, ef.Type) {
		return ef, fmt.Errorf("%s is not in %s's emit allowlist", ef.Type, fn.Identity())
	}
	ef.ID, _ = m["id"].(string)
	if ef.ID == "" && ef.Action != effectSplit {
		return ef, fmt.Errorf("id is required — the function composes the ids of what it writes")
	}
	if props, ok := m["properties"]; ok {
		pm, ok := props.(map[string]any)
		if !ok {
			return ef, fmt.Errorf("properties is a map, got %T", props)
		}
		ef.Properties = pm
	}
	switch ef.Action {
	case effectPut:
		if raw, has := m["ifAbsent"]; has {
			b, isBool := raw.(bool)
			if !isBool {
				// A typo here must not silently become a destructive upsert.
				return ef, fmt.Errorf("put: ifAbsent is a boolean, got %T", raw)
			}
			ef.IfAbsent = b
		}
		if err := decodeIfVersion(m, &ef); err != nil {
			return ef, err
		}
		if ef.IfAbsent && ef.IfVersion != nil {
			// ifAbsent short-circuits ahead of the version check (an existing
			// row is a no-op regardless of ifVersion), so the two together
			// silently drop the precondition — refuse the ambiguous combination
			// rather than let ifAbsent quietly win.
			return ef, fmt.Errorf("put: ifAbsent and ifVersion cannot combine — ifAbsent makes an existing row a no-op before the version check; pick one")
		}
		if raw, ok := m["edges"]; ok {
			edges, err := decodeEffectEdges(raw)
			if err != nil {
				return ef, err
			}
			ef.Edges = edges
		}
	case effectPatch:
		if err := decodeIfVersion(m, &ef); err != nil {
			return ef, err
		}
		if _, has := m["offer"]; has {
			// Ticket 002: the offer write-kind left the v1
			// contract. Refused whatever the value — a silent drop would turn
			// an intended contribution into a pinning direct write.
			return ef, fmt.Errorf("patch: offer was removed in v1; contribute via a source type + recordmapping")
		}
	case effectLink, effectUnlink:
		ef.Rel, _ = m["rel"].(string)
		if ef.Rel == "" {
			return ef, fmt.Errorf("%s: rel is required", ef.Action)
		}
		ref, err := decodeEdgeRef(m["to"])
		if err != nil {
			return ef, fmt.Errorf("%s: to: %w", ef.Action, err)
		}
		ef.To = ref
	case effectMerge:
		if !fn.Caps.AllowsMutation(vocabulary.MutationMerge) {
			return ef, fmt.Errorf("merge needs the permissions.mutations grant %s lacks", fn.Identity())
		}
		ef.Loser, _ = m["loser"].(string)
		if ef.Loser == "" {
			return ef, fmt.Errorf("merge: loser is required (id is the winner)")
		}
	case effectSplit:
		if !fn.Caps.AllowsMutation(vocabulary.MutationSplit) {
			return ef, fmt.Errorf("split needs the permissions.mutations grant %s lacks", fn.Identity())
		}
		ef.MergeID, _ = m["merge"].(string)
		if ef.MergeID == "" {
			return ef, fmt.Errorf("split: merge is required — the merge record's id")
		}
	}
	return ef, nil
}

// decodeIfVersion reads the optional optimistic-concurrency precondition off a
// put or patch effect. It must be an integer version — a stale one fails the
// whole delivery as a conflict, so a mistyped value must never silently drop
// the precondition and let the write clobber a newer edit.
func decodeIfVersion(m map[string]any, ef *effect) error {
	raw, has := m["ifVersion"]
	if !has {
		return nil
	}
	n, ok := asInt64(raw)
	if !ok {
		return fmt.Errorf("%s: ifVersion is an integer version, got %T", ef.Action, raw)
	}
	ef.IfVersion = &n
	return nil
}

func emitAllows(fn *vocabulary.Function, ident string) bool {
	for _, t := range fn.Caps.Emit {
		if t == ident {
			return true
		}
	}
	return false
}

// decodeEffectEdges reads a put effect's edges: rel → a target or a list of
// targets, each a bare id string or a {authority, type, id} reference.
func decodeEffectEdges(raw any) ([]substrate.EdgeInput, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("edges is a map of rel → target, got %T", raw)
	}
	var out []substrate.EdgeInput
	for _, rel := range sortedKeys(m) {
		targets, ok := m[rel].([]any)
		if !ok {
			targets = []any{m[rel]}
		}
		for _, tv := range targets {
			ref, err := decodeEdgeRef(tv)
			if err != nil {
				return nil, fmt.Errorf("edges.%s: %w", rel, err)
			}
			out = append(out, substrate.EdgeInput{Rel: rel, To: ref})
		}
	}
	return out, nil
}

func decodeEdgeRef(v any) (substrate.EdgeRef, error) {
	switch t := v.(type) {
	case string:
		return substrate.EdgeRef{ID: t}, nil
	case map[string]any:
		ref := substrate.EdgeRef{}
		ref.Kind, _ = t["kind"].(string)
		ref.ID, _ = t["id"].(string)
		if ref.ID == "" {
			return ref, fmt.Errorf("an edge target needs an id")
		}
		return ref, nil
	default:
		return substrate.EdgeRef{}, fmt.Errorf("an edge target is an id or a {kind, id} reference, got %T", v)
	}
}

// lockEffectTargets takes the advisory locks an accumulated effect list needs
// BEFORE any effect applies, in ONE global order (the contract:
// registry-dep < subject-type < record). Two lock families live here:
//
//   - SUBJECT-TYPE locks. A put/patch/link effect on a mapping-SOURCE type
//     resolves or mints its subject under `subject|<targetKind>` (mapping.go).
//     Taken here — before any record lock, matching the ordinary source write
//     (write.go preRecordLocks) — it closes the effect-prelock ↔ mapping
//     recompute cycle: a patch to x that also puts source s
//     can no longer hold record|x while a concurrent mapping write holds
//     subject|<type> and reaches for x.
//
//   - RECORD locks, one per statically addressed record — effect ids, merge
//     losers, link/unlink far ends, put edge targets — UNIONED with each
//     address's canonical target. A former id `a`→`x` in the list would
//     otherwise lock only `a` here and discover `x` when the effect applies,
//     so two lists naming a former id and its canonical id in opposite
//     positions can still deadlock the caller-wide transaction (review-final
//     #5; Postgres aborts one, and the direct call API has no retry to absorb
//     it). Resolving the trail now — an unlocked read, re-checked under the
//     lock as each effect applies — and locking raw+canonical together in one
//     ascending pass removes that hop.
//
// Advisory xact locks stack within one transaction, so every re-lock the
// individual effects take later (lockCanonical, matchOrLink, apply's trigger
// admission) is free. Trails that MOVE after this resolve — a merge landing
// mid-delivery, a split record's endpoints — stay the documented lockCanonical
// caveat.
func (t *txn) lockEffectTargets(effects []effect) error {
	reg := t.ds.registry()
	subjects := map[string]bool{}
	ids := map[string]eref{}
	note := func(ref eref) {
		if ref.ID != "" && ref.Kind != "" {
			ids[ref.key()] = ref
		}
	}
	// refType resolves the TYPE a statically addressed edge reference names:
	// its own named type, else the declared single target of (srcType, rel).
	// An untypeable reference is skipped here — the effect's own apply
	// resolves (and refuses) it authoritatively under its locks.
	refType := func(srcType, rel string, ref substrate.EdgeRef) string {
		if named := ref.Identity(); named != "" {
			if ty, err := t.ds.resolveType(named); err == nil {
				return ty.Identity
			}
			return ""
		}
		if ty, ok := reg.ByIdentity(srcType); ok {
			if ed, ok := ty.Edge(rel); ok && ed.To != "any" {
				return ed.To
			}
		}
		return ""
	}
	for _, ef := range effects {
		note(eref{Kind: ef.Type, ID: ef.ID})
		note(eref{Kind: ef.Type, ID: ef.Loser})
		note(eref{Kind: refType(ef.Type, ef.Rel, ef.To), ID: ef.To.ID})
		for _, e := range ef.Edges {
			note(eref{Kind: refType(ef.Type, e.Rel, e.To), ID: e.To.ID})
		}
		if m, ok := reg.MappingFor(ef.Type); ok {
			subjects["subject|"+m.To] = true
		}
	}
	// Fold every raw address's canonical target into the set — resolved
	// under no lock here, re-checked under the lock when each effect applies.
	for _, key := range sortedKeys(ids) {
		canon, err := t.canonicalOf(ids[key])
		if err != nil {
			return err
		}
		note(canon)
	}
	for _, key := range sortedKeys(subjects) {
		if err := t.lockKey(key); err != nil {
			return err
		}
	}
	for _, key := range sortedKeys(ids) {
		if err := t.lockRecord(ids[key]); err != nil {
			return err
		}
	}
	return nil
}

// applyEffect routes one effect through the ordinary write path inside the
// dispatcher's transaction: no-op suppression, the manager ledger,
// transitions (a patch naming a state value) and validation all apply
// untouched. The target's type is verified — a lying effect rolls the whole
// delivery back.
func (t *txn) applyEffect(ef effect) error {
	switch ef.Action {
	case effectPut:
		// A put addressed to a former id resolves onto the canonical winner:
		// a function's deterministic ids must survive a merge — parking every
		// later delivery on "ids are never reused" is the trap this closes.
		// The per-record advisory lock is taken BEFORE the trail resolves and
		// the resolution re-checked under it (lockCanonical), so a concurrent
		// merge can never slip between resolve and apply and get its loser
		// resurrected by this write.
		ref, err := t.lockCanonical(eref{Kind: ef.Type, ID: ef.ID})
		if err != nil {
			return err
		}
		if ef.IfAbsent {
			// Under the advisory lock this check-then-act is atomic: two
			// concurrent minters of one absent id serialize here, and the
			// second sees the first's row.
			row, err := t.loadRow(ref, false)
			if err != nil {
				return err
			}
			if row != nil {
				// Create-only: the record exists (live or tombstoned), so the
				// mint is a no-op — re-mention and replay never reset state
				// owned by later stages.
				return nil
			}
		}
		_, err = t.put(substrate.PutInput{
			Kind: ef.Type, ID: ref.ID, Properties: ef.Properties, Edges: ef.Edges,
			IfVersion: ef.IfVersion,
		})
		return err
	case effectPatch:
		// t.patch locks and canonicalizes former ids itself.
		_, err := t.patch(eref{Kind: ef.Type, ID: ef.ID}, substrate.PatchInput{Properties: ef.Properties, IfVersion: ef.IfVersion})
		return err
	case effectDelete:
		_, err := t.softDelete(eref{Kind: ef.Type, ID: ef.ID})
		return err
	case effectLink, effectUnlink:
		// Lock, then resolve — t.link/t.unlink re-lock the pair, and the
		// advisory locks are reentrant within the transaction.
		src, err := t.lockCanonical(eref{Kind: ef.Type, ID: ef.ID})
		if err != nil {
			return err
		}
		srcRow, err := t.loadRow(src, true)
		if err != nil {
			return err
		}
		if srcRow == nil {
			return fmt.Errorf("%w: record %s", substrate.ErrNotFound, ef.ID)
		}
		if ef.Action == effectLink {
			return t.link(ef.Rel, src, ef.To, ef.Properties)
		}
		return t.unlink(ef.Rel, src, ef.To)
	case effectMerge:
		winner := eref{Kind: ef.Type, ID: ef.ID}
		row, err := t.loadRow(winner, false)
		if err != nil {
			return err
		}
		if row == nil {
			return fmt.Errorf("%w: merge winner %s", substrate.ErrNotFound, ef.ID)
		}
		_, err = t.mergeRecord(winner, eref{Kind: ef.Type, ID: ef.Loser})
		return err
	case effectSplit:
		// The effect's `type` names the merged records' type; verify it
		// against the record's winner edge before splitting.
		winner, err := t.edgeTargetOf(eref{Kind: kindRecordMerge, ID: ef.MergeID}, "winner")
		if err != nil {
			return err
		}
		if winner.ID != "" && winner.Kind != ef.Type {
			return fmt.Errorf("%w: %s merged a %s, not the %s the effect names",
				substrate.ErrValidation, ef.MergeID, winner.Kind, ef.Type)
		}
		_, err = t.split(ef.MergeID)
		return err
	default:
		return fmt.Errorf("%w: effect action %q", substrate.ErrValidation, ef.Action)
	}
}
