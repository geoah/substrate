package engine

import (
	"fmt"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The effect vocabulary: a function body returns a list of effect values and
// the host applies them through the ordinary write path, transactionally with
// the cursor CAS. All seven Dataset mutations are reachable — put, patch,
// delete, merge, split — every one held to the manifest's
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
	// Loser rides a merge (ID is the winner); MergeID rides a split.
	Loser   string
	MergeID string
}

const (
	effectPut    = "put"
	effectPatch  = "patch"
	effectDelete = "delete"
	effectMerge  = "merge"
	effectSplit  = "split"
)

// effectKeys is the per-action closed key set. patch still RECOGNIZES
// "offer" — solely so the decode error can name its removal instead of
// reporting an anonymous unknown key.
var effectKeys = map[string]map[string]bool{
	effectPut:    {"action": true, "kind": true, "id": true, "ifAbsent": true, "ifVersion": true, "properties": true},
	effectPatch:  {"action": true, "kind": true, "id": true, "properties": true, "offer": true, "ifVersion": true},
	effectDelete: {"action": true, "kind": true, "id": true},
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
		return ef, fmt.Errorf("action %q is not put, patch, delete, merge or split", ef.Action)
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

// lockEffectTargets takes the advisory locks an accumulated effect list needs
// BEFORE any effect applies, in ONE global order (the contract:
// registry-dep < subject-type < record). Two lock families live here:
//
//   - SUBJECT-TYPE locks. A put or patch effect on a mapping-SOURCE type
//     resolves or mints its subject under `subject|<targetKind>` (mapping.go).
//     Taken here — before any record lock, matching the ordinary source write
//     (write.go preRecordLocks) — it closes the effect-prelock ↔ mapping
//     recompute cycle: a patch to x that also puts source s
//     can no longer hold record|x while a concurrent mapping write holds
//     subject|<type> and reaches for x.
//
//   - RECORD locks, one per statically addressed record — effect ids, merge
//     losers, and every record a written reference names — UNIONED with each
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
	for _, ef := range effects {
		note(eref{Kind: ef.Type, ID: ef.ID})
		note(eref{Kind: ef.Type, ID: ef.Loser})
		// Every record the effect's own reference values name. The value is
		// AUTHORED here (coercion runs at the write), so only a value the
		// declaration's pin can complete is typeable; the rest are skipped, and
		// the effect's own apply resolves and refuses them under its locks.
		for _, ref := range effectReferenceTargets(reg, ef) {
			note(ref)
			// A reference naming a mapping SOURCE record takes the subject hop
			// when it applies (references.go subjectHop), and that hop resolves
			// the source's subject under subject|<to>. Planning the key here,
			// ahead of every record lock, is what keeps one order: without it
			// this list could hold a record lock and then reach for a subject
			// lock a concurrent source write already holds while reaching for
			// that same record.
			for _, m := range reg.MappingsFrom(ref.Kind) {
				subjects["subject|"+m.To] = true
			}
		}
		for _, m := range reg.MappingsFrom(ef.Type) {
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
			Kind: ef.Type, ID: ref.ID, Properties: ef.Properties, IfVersion: ef.IfVersion,
		})
		return err
	case effectPatch:
		// t.patch locks and canonicalizes former ids itself.
		_, err := t.patch(eref{Kind: ef.Type, ID: ef.ID}, substrate.PatchInput{Properties: ef.Properties, IfVersion: ef.IfVersion})
		return err
	case effectDelete:
		_, err := t.softDelete(eref{Kind: ef.Type, ID: ef.ID})
		return err
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
		// The effect's `type` names the merged records' type; verify it against
		// the merge record's own `winner` reference before splitting.
		rec, err := t.loadRow(eref{Kind: kindRecordMerge, ID: ef.MergeID}, false)
		if err != nil {
			return err
		}
		winner := referenceTargetOf(rec, "winner")
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

// effectReferenceTargets lists the records an effect's own property values
// name, so the pre-lock pass can take their record locks in the one global
// order. It reads the AUTHORED value against the declaration: a full path
// names its kind, a bare id needs the declaration's `kind:` pin to complete it,
// and anything else is left to the effect's own apply, which resolves and
// refuses it under the locks it holds.
func effectReferenceTargets(reg *vocabulary.Registry, ef effect) []eref {
	ty, ok := reg.ByIdentity(ef.Type)
	if !ok || len(ef.Properties) == 0 {
		return nil
	}
	var out []eref
	for _, name := range sortedKeys(ef.Properties) {
		p, declared := ty.Prop(name)
		if !declared || p.Datatype != vocabulary.DatatypeReference {
			continue
		}
		values, isList := ef.Properties[name].([]any)
		if !isList {
			values = []any{ef.Properties[name]}
		}
		for _, v := range values {
			path := referencePathOf(v)
			if path == "" {
				continue
			}
			if kind, id, isPath := vocabulary.SplitRecordPath(path); isPath {
				out = append(out, eref{Kind: kind, ID: id})
				continue
			}
			if p.To != "" && p.To != vocabulary.ToAny {
				out = append(out, eref{Kind: p.To, ID: path})
			}
		}
	}
	return out
}
