package engine

import (
	"bytes"
	"context"
	"crypto/subtle"
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

// applySpec is the normalised shape both put and patch reduce to.
type applySpec struct {
	ty       *vocabulary.Kind
	id       string
	existing *erow
	op       substrate.Op

	title *string
	body  *string

	props       map[string]any
	labels      map[string]any
	annotations map[string]any

	at     *time.Time
	endsAt *time.Time
	dueAt  *time.Time
	// clearHot names the hot properties this write set to null: a null
	// deletes, exactly as it does on every other property.
	clearHot map[string]bool

	// states are the state PROPERTIES this write names (MODEL §11.4): they
	// arrive in the properties map and are split out here, because storage
	// keeps them in their own column.
	states map[string]string
	edges  []substrate.EdgeInput

	addFinalizers    []string
	removeFinalizers []string

	resurrect bool
}

// ref is the write's full (type, id) storage identity.
func (sp *applySpec) ref() eref { return eref{Kind: sp.ty.Identity, ID: sp.id} }

// propTarget and propTargetVersion carry an edit's CAS contract: the version
// of the target the stored diff was computed against (§7).
const (
	propTarget        = "target"
	propTargetVersion = "targetVersion"
)

// diffConflict marks an onEnter apply that lost — a stale applyDiff CAS, a
// refused applyMerge — so the transition fails, the request stays proposed,
// and the caller records the conflict.
type diffConflict struct {
	action string // onEnterApplyDiff or onEnterApplyMerge
	edit   eref
	err    error
}

func (e *diffConflict) Error() string { return fmt.Sprintf("%s on %s: %v", e.action, e.edit.ID, e.err) }
func (e *diffConflict) Unwrap() error { return e.err }

func (ds *dataset) Put(ctx context.Context, actor substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	// Schema is records: a put of a schema kind is a batch of one, through
	// the loader as admission (schemawrite.go).
	if ty, err := ds.resolveType(in.Kind); err == nil {
		if _, isVocabulary := vocabularyRecordKinds[ty.Identity]; isVocabulary {
			return ds.putSchemaRecord(ctx, actor, ty, in)
		}
	}
	return ds.putWith(ctx, actor, in, false)
}

func (ds *dataset) putInternal(ctx context.Context, actor substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	return ds.putWith(ctx, actor, in, true)
}

func (ds *dataset) putWith(ctx context.Context, actor substrate.Actor, in substrate.PutInput, internal bool) (*substrate.Record, error) {
	var out *substrate.Record
	err := ds.inTx(ctx, actor, internal, func(t *txn) error {
		e, err := t.put(in)
		out = e
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (t *txn) put(in substrate.PutInput) (*substrate.Record, error) {
	sp, err := t.putSpec(in)
	if err != nil {
		return nil, err
	}
	return t.apply(sp)
}

func (t *txn) putSpec(in substrate.PutInput) (*applySpec, error) {
	ty, err := t.ds.resolveType(in.Kind)
	if err != nil {
		return nil, err
	}
	if err := t.guardSystemKind(ty, substrate.OpPut); err != nil {
		return nil, err
	}
	// Global lock order: the registry-dep/subject locks this type needs come
	// before its own record lock.
	if err := t.preRecordLocks(ty); err != nil {
		return nil, err
	}
	authored, hot, states, err := splitProps(ty, in.Properties)
	if err != nil {
		return nil, err
	}
	props, err := coerceProps(ty, authored)
	if err != nil {
		return nil, err
	}
	labels, err := coerceLabels(t.actor, in.Labels)
	if err != nil {
		return nil, err
	}

	// A supplied id is the writer's OWN key, not an address to resolve: a
	// former id (of this type — trails are per-type) is refused outright
	// (conflict), so nothing here canonicalizes. Reads, patches, links and
	// deletes still follow the trail.
	id := in.ID
	if id != "" {
		if err := t.checkID(ty.Identity, id); err != nil {
			return nil, err
		}
	} else if id, err = newID(); err != nil {
		return nil, err
	}

	existing, err := t.loadRow(eref{Kind: ty.Identity, ID: id}, true)
	if err != nil {
		return nil, err
	}
	if in.ID != "" && existing == nil {
		if err := t.checkCreateID(ty); err != nil {
			return nil, err
		}
	}
	if err := checkCAS(existing, in.IfVersion); err != nil {
		return nil, err
	}

	return &applySpec{
		ty: ty, id: id, existing: existing, op: substrate.OpPut,
		title: hot.title, body: hot.body,
		props: props, labels: labels, annotations: in.Annotations,
		at: hot.at, endsAt: hot.endsAt, dueAt: hot.dueAt,
		clearHot: hot.clear,
		states:   states, edges: in.Edges,
		// A put onto a TOMBSTONE restores that record: same id, same row,
		// undeleted. It is not id reuse, so the canonical-id contract is
		// untouched — and deletion stays explicit, because only `delete`
		// tombstones.
		resurrect: existing != nil && existing.DeletedAt != nil,
	}, nil
}

// checkID polices the id itself, whatever the write does with it: one
// URL path segment, and never somebody's former id WITHIN THIS TYPE —
// addressing a write at a merged-away id would silently write the winner of
// a merge the writer never made. Another type holding the
// same id is no collision: identity is the (type, id) pair.
func (t *txn) checkID(typ, id string) error {
	if !vocabulary.ValidID(id) {
		return fmt.Errorf("%w: %q is not a record id (URL-path-safe, at most %d characters)",
			substrate.ErrValidation, id, vocabulary.MaxIDLen)
	}
	// The advisory lock precedes the former-id read: a concurrent merge of
	// this id either committed already (the trail below sees it) or queues
	// behind this writer — the check can no longer pass on a pre-merge
	// snapshot and then land the write on a tombstoned loser.
	if err := t.lockRecord(eref{Kind: typ, ID: id}); err != nil {
		return err
	}
	former, err := t.formerTarget(typ, id)
	if err != nil {
		return err
	}
	if former != "" {
		return fmt.Errorf("%w: %s is a former id of %s; ids are never reused", substrate.ErrConflict, id, former)
	}
	return nil
}

// checkCreateID enforces proposal §6's naming rule, which is about WHO NAMES a
// new record: a type some mapping points at is always server-assigned,
// because nothing external names a subject. Addressing a record that already
// exists is not naming it, so `PUT …/{plural}/{id}` — the console's Save, and
// `substrate apply` — works on every type.
func (t *txn) checkCreateID(ty *vocabulary.Kind) error {
	if len(t.ds.registry().MappingsTo(ty.Identity)) == 0 {
		return nil
	}
	return fmt.Errorf("%w: %s ids are server-assigned — a mapping points at it, and nothing external names a subject",
		substrate.ErrValidation, ty.Name)
}

// hotProps are the properties that occupy their own storage column: everything
// authored is a property, and these keep a column underneath.
// A nil pointer means the write did not mention the property; `clear` names
// the ones it set to null, which DELETES them exactly as a null does on any
// other property.
type hotProps struct {
	title  *string
	body   *string
	at     *time.Time
	endsAt *time.Time
	dueAt  *time.Time
	clear  map[string]bool
}

func (h *hotProps) clearing(name string) {
	if h.clear == nil {
		h.clear = map[string]bool{}
	}
	h.clear[name] = true
}

// mentions reports whether the write named any hot property at all.
func (h *hotProps) mentions() bool {
	return h.title != nil || h.body != nil || h.at != nil || h.endsAt != nil ||
		h.dueAt != nil || len(h.clear) > 0
}

// splitProps takes one authored properties map apart into the three places
// storage keeps them: ordinary properties, the hot columns, and the states
// column. They travel together on the wire — one map, one namespace — and
// part company here.
func splitProps(ty *vocabulary.Kind, in map[string]any) (map[string]any, hotProps, map[string]string, error) {
	var hot hotProps
	if len(in) == 0 {
		return nil, hot, nil, nil
	}
	var props map[string]any
	var states map[string]string
	for _, name := range sortedKeys(in) {
		v := in[name]
		switch {
		case name == substrate.PropTitle, name == substrate.PropBody:
			if v == nil {
				// Null clears it; the column is NOT NULL, so empty IS cleared.
				hot.clearing(name)
				continue
			}
			s, ok := v.(string)
			if !ok {
				return nil, hot, nil, fmt.Errorf("%w: properties.%s: expected a string", substrate.ErrValidation, name)
			}
			if name == substrate.PropTitle {
				hot.title = &s
			} else {
				hot.body = &s
			}
			continue
		case isHotTime(name) && ty.UsesHot(name):
			if v == nil {
				hot.clearing(name)
				continue
			}
			ts, err := hotTime(name, v)
			if err != nil {
				return nil, hot, nil, err
			}
			switch name {
			case substrate.PropAt:
				hot.at = ts
			case substrate.PropEndsAt:
				hot.endsAt = ts
			case substrate.PropDueAt:
				hot.dueAt = ts
			}
			continue
		case isHotTime(name):
			if _, declared := ty.Prop(name); !declared {
				return nil, hot, nil, fmt.Errorf("%w: %s declares no %s (bind a temporal trait)",
					substrate.ErrValidation, ty.Name, name)
			}
		}
		if _, isState := ty.StateProp(name); !isState {
			if props == nil {
				props = make(map[string]any, len(in))
			}
			props[name] = v
			continue
		}
		s, ok := v.(string)
		if !ok || s == "" {
			return nil, hot, nil, fmt.Errorf("%w: properties.%s is a state property: expected one of its states, got %v",
				substrate.ErrValidation, name, v)
		}
		if states == nil {
			states = map[string]string{}
		}
		states[name] = s
	}
	return props, hot, states, nil
}

func isHotTime(name string) bool {
	return name == substrate.PropAt || name == substrate.PropEndsAt || name == substrate.PropDueAt
}

func hotTime(name string, v any) (*time.Time, error) {
	s, err := asString(v)
	if err != nil {
		return nil, fmt.Errorf("%w: properties.%s: expected an RFC 3339 instant", substrate.ErrValidation, name)
	}
	ts, err := parseTime(s)
	if err != nil {
		return nil, fmt.Errorf("%w: properties.%s: %w", substrate.ErrValidation, name, err)
	}
	// Postgres keeps microseconds: truncate so a re-sync of the same instant
	// compares equal instead of drifting every poll.
	ts = ts.UTC().Truncate(time.Microsecond)
	return &ts, nil
}

func (ds *dataset) Patch(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error) {
	// A patch addressed at a schema record routes through admission: the
	// merged declaration must still close (schemawrite.go). The addressed
	// type says which rows are schema rows; no peek by bare id exists.
	if ty, err := ds.resolveType(typ); err == nil {
		if _, isVocabulary := vocabularyRecordKinds[ty.Identity]; isVocabulary {
			existing, err := ds.Get(ctx, ty.Identity, id)
			if err != nil {
				return nil, err
			}
			return ds.patchSchemaRecord(ctx, actor, existing, in)
		}
	}
	return ds.patchWith(ctx, actor, typ, id, in, false)
}

func (ds *dataset) patchInternal(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error) {
	return ds.patchWith(ctx, actor, typ, id, in, true)
}

func (ds *dataset) patchWith(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.PatchInput, internal bool) (*substrate.Record, error) {
	var out *substrate.Record
	err := ds.inTx(ctx, actor, internal, func(t *txn) error {
		ty, err := t.ds.resolveType(typ)
		if err != nil {
			return err
		}
		e, err := t.patch(eref{Kind: ty.Identity, ID: id}, in)
		out = e
		return err
	})
	var dc *diffConflict
	if errors.As(err, &dc) {
		// The transition rolled back with the transaction; the conflict
		// annotation is the record the owner sees.
		note := map[string]any{
			"reason": dc.err.Error(), "at": nowUTC().Format(time.RFC3339Nano),
		}
		if aerr := ds.inTx(ctx, actor, true, func(t *txn) error {
			row, err := t.loadRow(dc.edit, true)
			if err != nil || row == nil {
				return err
			}
			if _, err := t.putAnnotation(dc.edit, annApplyConflict, note); err != nil {
				return err
			}
			return t.appendChange(actor, substrate.OpPatch, dc.edit.ID, row.Kind,
				map[string]any{"conflict": note})
		}); aerr != nil {
			return nil, aerr
		}
		return nil, fmt.Errorf("%w: %w", substrate.ErrConflict, dc.err)
	}
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (t *txn) patch(ref eref, in substrate.PatchInput) (*substrate.Record, error) {
	ty, err := t.ds.resolveType(ref.Kind)
	if err != nil {
		return nil, err
	}
	ref.Kind = ty.Identity
	// Global lock order: take the registry-dep/subject locks this addressed
	// record's type needs BEFORE its own record lock (the contract "lock
	// ordering"). A trigger patch can then never hold record|id while a
	// connector registration holds the registry-dep lock exclusive (#7), and a
	// source patch orders its subject lock ahead of the record locks recompute
	// needs (#6).
	if !t.recomputing {
		if err := t.preRecordLocks(ty); err != nil {
			return nil, err
		}
	}
	// Lock, then resolve: the addressing must not race a merge (§6.3).
	ref, err = t.lockCanonical(ref)
	if err != nil {
		return nil, err
	}
	id := ref.ID
	existing, err := t.loadRow(ref, true)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, fmt.Errorf("%w: record %s", substrate.ErrNotFound, id)
	}
	authored, hot, states, err := splitProps(ty, in.Properties)
	if err != nil {
		return nil, err
	}
	if err := t.guardSystemKind(ty, substrate.OpPatch); err != nil {
		switch {
		// A repository's lifecycle machine is the one system transition the
		// generic surface may drive. (The connector record's `options`
		// exception went with the mirror: ACCOUNTS.md A1 moved option
		// answers onto the per-account `connectoraccount` record — an
		// ordinary type — so the connector record is fully the substrate's
		// again.)
		case ty.Identity == kindRepository && len(states) > 0 && isTransitionOnly(in, authored, hot):
		default:
			return nil, err
		}
	}
	// Deciding a change request is optimistic on the REQUEST's own version: a
	// reviewer accepts/rejects the envelope it read, so a concurrent change to
	// the request cannot slip under the decision. The requirement holds for
	// the human/owner decision path; an bundle actor's atomic effect
	// carries no read-then-decide window and is bounded instead by the emit
	// ceiling at acceptance.
	if ty.Identity == vocabulary.KindRecordPatchRequest {
		if _, deciding := states[propDecision]; deciding && in.IfVersion == nil &&
			t.tier != substrate.TierBundle {
			return nil, fmt.Errorf("%w: deciding a change request must carry ifVersion — the request version you reviewed, so a concurrent change cannot slip under your decision",
				substrate.ErrConflict)
		}
	}
	if err := checkCAS(existing, in.IfVersion); err != nil {
		return nil, err
	}
	props, err := coerceProps(ty, authored)
	if err != nil {
		return nil, err
	}
	labels, err := coerceLabels(t.actor, in.Labels)
	if err != nil {
		return nil, err
	}
	return t.apply(&applySpec{
		ty: ty, id: id, existing: existing, op: substrate.OpPatch,
		title: hot.title, body: hot.body,
		props: props, labels: labels, annotations: in.Annotations,
		at: hot.at, endsAt: hot.endsAt, dueAt: hot.dueAt,
		clearHot:      hot.clear,
		states:        states,
		addFinalizers: in.AddFinalizers, removeFinalizers: in.RemoveFinalizers,
	})
}

// isTransitionOnly reports whether a patch carries nothing but state
// properties — the shape the repository lifecycle exception admits.
func isTransitionOnly(in substrate.PatchInput, rest map[string]any, hot hotProps) bool {
	return !hot.mentions() && len(rest) == 0 && len(in.Labels) == 0 &&
		len(in.Annotations) == 0 &&
		len(in.AddFinalizers) == 0 && len(in.RemoveFinalizers) == 0
}

// apply is the single write path: machines, metadata, edges, then one
// no-op-suppressed row write and at most one changelog row.
//
// NOTHING RANKS WRITERS: any actor's direct write overwrites
// anything, and the property-manager ledger records who last had a change
// accepted. The one reader that defers to it is mapping RECOMPUTE (record
// 51), which yields to any manager outside the machine tier — that is
// recompute restraining itself, not this path guarding anyone.
func (t *txn) apply(sp *applySpec) (*substrate.Record, error) {
	create := sp.existing == nil
	row := sp.existing
	// What the record was, before this write mutates the loaded row in place.
	// The changelog entry carries the DELTA between this and what the write produces
	// (fold.go), values and all, which is what makes the changelog replayable.
	before := sp.existing.clone()

	// The reviewed envelope is IMMUTABLE once a change request exists:
	// op/targetKind/targetId/diff and the target edge are the values a
	// reviewer read, so a later write must not swap them under an undecided
	// request and turn a harmless patch into an arbitrary delete.
	if sp.ty.Identity == vocabulary.KindRecordPatchRequest && !create {
		if err := guardImmutableEnvelope(sp); err != nil {
			return nil, err
		}
	}
	if create {
		row = &erow{
			ID: sp.id, Kind: sp.ty.Identity, States: map[string]string{},
			Props: map[string]any{}, Labels: map[string]any{},
			CreatedAt: t.now, UpdatedAt: t.now,
		}
	}
	payload := map[string]any{}
	var accepted []string
	// deleted names the accepted properties this write removed: a delete
	// clears the manager row rather than claiming it, and on a
	// mapped target it is the release trigger recompute refills from.
	deleted := map[string]bool{}
	// srcMapping is non-nil when this type is a source record (§6.1): its
	// subject edge is guarded, ensured, and recomputed through.
	srcMapping, _ := t.ds.registry().MappingFor(sp.ty.Identity)

	// A blob-ref must name a known blob: the shape passed
	// coercion, the existence gate is here inside the transaction.
	if err := t.validateBlobRefs(sp.ty, sp.props); err != nil {
		return nil, err
	}
	// A reference must name a known TYPE: coercion checked the
	// shape, this resolves the referent type, refuses a `to:` mismatch, and
	// rewrites the value canonical — before the row's property map is built,
	// so the trigger-callable and every other stored reference lands as
	// {authority, type, id}. The referent RECORD need not exist: it is a pointer.
	if err := t.validateReferences(sp.ty, sp.props); err != nil {
		return nil, err
	}
	// A `blob` manifest write must hold the byte-store invariants:
	// id == digest, and `stored` only once the bytes exist.
	if err := t.guardBlobWrite(sp); err != nil {
		return nil, err
	}

	take := func(name string, cur any, had bool, next any) bool {
		if next == nil {
			return had
		}
		if had && jsonEqual(cur, next) {
			return false
		}
		accepted = append(accepted, name)
		return true
	}

	// State properties: creations are born in the declared initial state;
	// transitions belong to patch.
	var pendingDiff, pendingMerge bool
	if create {
		for _, name := range sortedKeys(sp.ty.Machines) {
			m := sp.ty.Machines[name]
			initial := m.Initial
			if want, ok := sp.states[name]; ok {
				if !m.HasState(want) {
					return nil, fmt.Errorf("%w: %q is not a state of %s", substrate.ErrValidation, want, name)
				}
				// A creating write may name any declared state.
				initial = want
			}
			row.States[name] = initial
		}
	} else {
		for _, name := range sortedKeys(sp.states) {
			target := sp.states[name]
			m := sp.ty.Machines[name]
			cur := row.States[name]
			// A patch naming the CURRENT state is a no-op, never an illegal
			// transition: level-triggered function effects re-assert states on
			// every delivery (githubtasks patches `done` for a closed issue it
			// cannot read), and loop containment rests on the re-assertion
			// writing no changelog row.
			if cur == target {
				continue
			}
			if sp.op == substrate.OpPut {
				return nil, fmt.Errorf("%w: put may not move %s from %q to %q — patch does transitions",
					substrate.ErrGuard, name, cur, target)
			}
			if !m.HasState(target) {
				return nil, fmt.Errorf("%w: %q is not a state of %s", substrate.ErrValidation, target, name)
			}
			tr := m.Transition(cur, target)
			if tr == nil {
				return nil, fmt.Errorf("%w: %s has no transition %q → %q", substrate.ErrGuard, name, cur, target)
			}
			row.States[name] = target
			for _, stamp := range sortedKeys(tr.Stamps) {
				row.Props[stamp] = t.now.Format(time.RFC3339Nano)
				accepted = append(accepted, stamp)
			}
			switch tr.OnEnter {
			case onEnterApplyDiff:
				pendingDiff = true
			case onEnterApplyMerge:
				pendingMerge = true
			}
		}
	}

	// An accepted edit is CAS'd against the version its diff was computed
	// against, and the target is an EDGE (MODEL §11.5): the version it had
	// when this write pointed at it is read below, once the edges are
	// resolved. What it pointed at BEFORE is read here, because the edge
	// write is about to overwrite it.
	var prevTarget eref
	if appliesDiff(sp.ty) && !create {
		var err error
		if prevTarget, err = t.edgeTargetOf(sp.ref(), propTarget); err != nil {
			return nil, err
		}
	}

	// Properties.
	for _, name := range sortedKeys(sp.props) {
		next := sp.props[name]
		cur, had := row.Props[name]
		if next == nil {
			if !had {
				continue
			}
			delete(row.Props, name)
			accepted = append(accepted, name)
			deleted[name] = true
			continue
		}
		if take(name, cur, had, next) {
			row.Props[name] = next
		}
	}

	// Title and body are properties with their own column; a displayTemplate
	// overrides the writer's title entirely. The column is NOT NULL, so a
	// null clears to empty.
	if sp.ty.Template == nil {
		switch {
		case sp.clearHot[substrate.PropTitle]:
			if take(substrate.PropTitle, row.Title, true, "") {
				row.Title = ""
				deleted[substrate.PropTitle] = true
			}
		case sp.title != nil:
			if take(substrate.PropTitle, row.Title, true, *sp.title) {
				row.Title = *sp.title
			}
		}
	}
	switch {
	case sp.clearHot[substrate.PropBody]:
		if take(substrate.PropBody, row.Body, true, "") {
			row.Body = ""
			deleted[substrate.PropBody] = true
		}
	case sp.body != nil:
		if take(substrate.PropBody, row.Body, true, *sp.body) {
			row.Body = *sp.body
		}
	}

	// Trait-backed hot columns.
	for _, hc := range []struct {
		name string
		in   *time.Time
		dst  **time.Time
	}{
		{substrate.PropAt, sp.at, &row.At},
		{substrate.PropEndsAt, sp.endsAt, &row.EndsAt},
		{substrate.PropDueAt, sp.dueAt, &row.DueAt},
	} {
		if sp.clearHot[hc.name] {
			if *hc.dst != nil {
				*hc.dst = nil
				accepted = append(accepted, hc.name)
				deleted[hc.name] = true
			}
			continue
		}
		if hc.in == nil {
			continue
		}
		v := *hc.in
		var cur any
		if *hc.dst != nil {
			cur = (*hc.dst).UTC().Format(time.RFC3339Nano)
		}
		if take(hc.name, cur, *hc.dst != nil, v.Format(time.RFC3339Nano)) {
			*hc.dst = &v
		}
	}

	// Labels.
	for _, k := range sortedKeys(sp.labels) {
		v := sp.labels[k]
		if v == nil {
			delete(row.Labels, k)
			continue
		}
		row.Labels[k] = v
	}

	// Finalizers.
	if len(sp.addFinalizers) > 0 || len(sp.removeFinalizers) > 0 {
		row.Finalizers = mergeFinalizers(row.Finalizers, sp.addFinalizers, sp.removeFinalizers)
	}

	subChanged := false

	// Edges.
	var newTarget eref
	for _, e := range sp.edges {
		ed, ok := sp.ty.Edge(e.Rel)
		if !ok {
			return nil, fmt.Errorf("%w: %s declares no edge %q", substrate.ErrValidation, sp.ty.Name, e.Rel)
		}
		dst, err := t.resolveEdgeRef(ed, e.To)
		if err != nil {
			return nil, err
		}
		// The reviewed target of a change request is immutable after propose:
		// a re-sync of the SAME target is a harmless no-op, but a swap to
		// another (version-matching) record would smuggle a different
		// operation past the reviewer's decision.
		if sp.ty.Identity == vocabulary.KindRecordPatchRequest && !create && e.Rel == propTarget && dst != prevTarget {
			return nil, fmt.Errorf("%w: the target edge is immutable on a change request — the reviewed target is fixed at propose time",
				substrate.ErrForbidden)
		}
		isSubject := srcMapping != nil && e.Rel == srcMapping.Edge
		if isSubject {
			if err := t.checkSubjectWrite(sp, e.Rel, dst); err != nil {
				return nil, err
			}
		}
		if !ed.Many {
			cleared, err := t.replaceSingleEdge(e.Rel, sp.ref(), dst)
			if err != nil {
				return nil, err
			}
			subChanged = subChanged || cleared
		}
		ok2, err := t.putEdge(e.Rel, sp.ref(), dst, e.Properties, isSubject)
		if err != nil {
			return nil, err
		}
		subChanged = subChanged || ok2
		if e.Rel == propTarget {
			newTarget = dst
		}
	}

	// The edit's CAS anchor: re-asserting the same target must not rebase the
	// diff onto whatever the target has become since, so only a NEW target
	// re-anchors it.
	if newTarget.ID != "" && newTarget != prevTarget {
		if err := t.stampTargetVersion(sp, row, newTarget, &accepted); err != nil {
			return nil, err
		}
	}

	// A source record is never left unlinked: whatever the
	// write carried, it ends pointing at a subject — the one it named, one a
	// match probe found, or a shell born here.
	if srcMapping != nil {
		if err := t.ensureSubject(sp, row, srcMapping); err != nil {
			return nil, err
		}
	}

	// Required edges are asserted at birth: a record is created with them or
	// not at all. A later patch that does not touch edges is unaffected.
	if create {
		if err := t.checkRequiredEdges(sp); err != nil {
			return nil, err
		}
	}

	// Annotations.
	for _, k := range sortedKeys(sp.annotations) {
		if err := metaKeyAllowed(t.actor, k); err != nil {
			return nil, err
		}
		ok, err := t.putAnnotation(sp.ref(), k, sp.annotations[k])
		if err != nil {
			return nil, err
		}
		subChanged = subChanged || ok
	}

	// Trigger rows are admitted at write time: the merged result must parse,
	// its guard must compile and its callable must resolve — a trigger the
	// dispatcher cannot run never lands (engine/triggers.go). Admission holds
	// the SHARED registry-dependency lock through commit and re-verifies the
	// callable's record row under it, so a bundle upgrade's dropped-reference
	// query (exclusive side, schemawrite.go) can never race this trigger into
	// existence across its breakage check. Internal
	// writes skip both — a connector registration writes triggers inside the
	// schema batch that already holds the exclusive side.
	if sp.ty.Identity == typeTrigger {
		if !t.internal {
			if err := t.lockKeyShared(registryDepKey(t.ds)); err != nil {
				return nil, err
			}
		}
		if err := t.ds.validateTriggerRow(t.ds.registry(), sp.id, row.Props, !t.internal); err != nil {
			return nil, err
		}
		if !t.internal {
			tr, err := parseTrigger(sp.id, row.Props)
			if err != nil {
				return nil, fmt.Errorf("%w: trigger: %w", substrate.ErrValidation, err)
			}
			if err := t.checkTriggerCallableRow(tr); err != nil {
				return nil, err
			}
		}
	}

	// Bundle-owned types carry the lifecycle rules (engine/bundles.go):
	// read-only when uninstalled, config/accounts frozen when disabled, and
	// the configType's one-live-record singleton on create.
	if err := t.checkBundleWrite(sp.ty, sp.id, create || sp.resurrect); err != nil {
		return nil, err
	}
	// External create of a bundle's config/account record is owner-only: the
	// facility and connector never stand one up.
	if err := t.checkBundleOwnerGate(sp.ty, create || sp.resurrect); err != nil {
		return nil, err
	}
	// Per-property ownership, enforced after the merged row is known: a
	// `writer:`-restricted property (tokenRef/tokenStatus/grantedScopes →
	// oauth; syncToken/lastSyncedAt/syncStatus → connector; email/toggles →
	// owner) accepts a change only from its role's actor.
	if err := t.checkPropertyOwnership(sp.ty, accepted); err != nil {
		return nil, err
	}

	title, err := t.deriveTitle(sp.ty, row)
	if err != nil {
		return nil, err
	}
	row.Title = title

	// Secret-typed property values move into the sealed store here, at the
	// storage boundary: the JSONB that lands in the row, and the changelog
	// delta derived from it, carry an opaque ref and never the material.
	// Comparison and projection above ran on plaintext; substitution here
	// keeps both unchanged.
	accepted, err = t.storeSecretProps(sp.ty, sp.ref(), before, row, accepted)
	if err != nil {
		return nil, err
	}

	// The record lands through the fold, as a delta carrying its values: the
	// same effect the changelog entry below will hold, applied by the same function a
	// rebuild replays it with (fold.go). `force` is the edge/annotation change
	// that must move the row even though no column of it did; `restored` lifts
	// the tombstone the put landed on.
	res, err := t.foldRow(before, row, subChanged, sp.resurrect)
	if err != nil {
		return nil, err
	}
	changed, created := res.changed, res.created
	if res.row != nil {
		row = res.row
	}

	// A write that changed nothing writes no changelog row: re-syncing
	// identical data must stay silent.
	if changed {
		// The manager ledger, per accepted property: a delete clears the row
		// (release — record 51), a recompute credits the WINNING SOURCE's
		// actor at the MACHINE tier (attribution never pins — the machine's
		// own rows stay the machine's to overwrite), and every direct write
		// claims its own actor at the write context's tier (ticket 002:
		// actor data, never name grammar).
		managers := map[string]any{}
		for _, name := range accepted {
			switch {
			case deleted[name]:
				if err := t.deleteManager(sp.ref(), name); err != nil {
					return nil, err
				}
			case t.recomputing:
				actor, ok := t.recomputeManagers[name]
				if !ok {
					actor = t.actor
				}
				if err := t.setManager(sp.ref(), name, actor, substrate.TierMachine); err != nil {
					return nil, err
				}
				managers[name] = string(actor)
			default:
				if err := t.setManager(sp.ref(), name, t.actor, t.tier); err != nil {
					return nil, err
				}
			}
			if p, ok := sp.ty.Prop(name); ok && p.Embed {
				if err := t.enqueueEmbed(sp.ref(), name); err != nil {
					return nil, err
				}
			}
		}
		if created {
			payload["created"] = true
		}
		if sp.resurrect {
			payload["restored"] = true
		}
		if len(accepted) > 0 {
			payload["properties"] = accepted
		}
		if len(managers) > 0 {
			payload["managers"] = managers
		}
		if len(sp.states) > 0 {
			payload["states"] = row.States
		}
		if err := t.appendChange(t.actor, sp.op, sp.id, sp.ty.Identity, payload); err != nil {
			return nil, err
		}
		// A created (or restored) trigger's bookkeeping initializes in ITS
		// OWN transaction, cursor at the creation's seq: the trigger reacts
		// to what happens next, and a write between creation and the first
		// dispatch is never skipped.
		if sp.ty.Identity == typeTrigger && (created || sp.resurrect) {
			if err := t.initTriggerBookkeeping(sp.id, row.Props); err != nil {
				return nil, err
			}
		}
	}

	// A source record's write is recompute's trigger: the record it points
	// at recomputes from the whole set. It runs on every such
	// write, changed or not, so the subject converges even when this write
	// was a no-op re-sync — and because recompute itself flows through this
	// path, an unchanged recompute writes nothing.
	if srcMapping != nil && !t.recomputing {
		if err := t.recomputeSubjectOf(sp.ref(), srcMapping); err != nil {
			return nil, err
		}
	}

	// A property DELETED on a mapped target is a release: the
	// same transaction recomputes from live sources, so the property refills
	// on the spot — no unpin verb, no flag lifecycle. The row is reloaded so
	// the caller sees what the recompute refilled.
	if changed && !t.recomputing && len(deleted) > 0 &&
		len(t.ds.registry().MappingsTo(sp.ty.Identity)) > 0 {
		if err := t.recompute(sp.ref()); err != nil {
			return nil, err
		}
		fresh, err := t.loadRow(sp.ref(), false)
		if err != nil {
			return nil, err
		}
		if fresh != nil {
			row = fresh
		}
	}

	if pendingDiff && sp.op == substrate.OpPatch {
		if err := t.applyEditDiff(row); err != nil {
			return nil, err
		}
	}
	if pendingMerge && sp.op == substrate.OpPatch {
		if err := t.applyMergeRequest(row); err != nil {
			return nil, err
		}
	}
	return t.record(row, sp.ty)
}

// The two declared transition effects (schema/load.go onEnterActions).
const (
	onEnterApplyDiff  = "applyDiff"
	onEnterApplyMerge = "applyMerge"
)

// appliesDiff reports whether a type's machines can apply a stored diff to
// another record — the types whose writes must record a target version.
func appliesDiff(ty *vocabulary.Kind) bool {
	for _, m := range ty.Machines {
		for _, tr := range m.Transitions {
			if tr.OnEnter == onEnterApplyDiff {
				return true
			}
		}
	}
	return false
}

// stampTargetVersion records the target's current version on the write that
// points an edit at it, unless the writer asserted one itself. The target
// itself is an edge now (MODEL §11.5); only the version it was read at is a
// property.
func (t *txn) stampTargetVersion(sp *applySpec, row *erow, target eref, accepted *[]string) error {
	if !appliesDiff(sp.ty) {
		return nil
	}
	if _, ok := sp.props[propTargetVersion]; ok {
		return nil
	}
	tgt, err := t.loadRow(target, false)
	if err != nil {
		return err
	}
	if tgt == nil {
		return fmt.Errorf("%w: edit target %s", substrate.ErrNotFound, target.ID)
	}
	row.Props[propTargetVersion] = tgt.Version
	*accepted = append(*accepted, propTargetVersion)
	return nil
}

// checkRequiredEdges asserts every declared required edge is present on a
// freshly created record.
func (t *txn) checkRequiredEdges(sp *applySpec) error {
	var required []string
	for _, name := range sp.ty.EdgeOrder {
		if sp.ty.Edges[name].Required {
			required = append(required, name)
		}
	}
	if len(required) == 0 {
		return nil
	}
	edges, err := t.edgesOf(sp.ref())
	if err != nil {
		return err
	}
	var missing []string
	for _, name := range required {
		if len(edges[name]) == 0 {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("%w: %s requires edge %s", substrate.ErrValidation,
			sp.ty.Name, strings.Join(missing, ", "))
	}
	return nil
}

// checkBundleOwnerGate refuses an EXTERNAL create/resurrect of a bundle's
// config or account record by a non-owner actor. Internal
// writes (facility teardown, purge, mirrors) bypass; patches are governed by
// per-property ownership below, not this coarse gate.
func (t *txn) checkBundleOwnerGate(ty *vocabulary.Kind, creating bool) error {
	if t.internal || !creating || !isBundleOwnerGated(ty) {
		return nil
	}
	if t.tier != substrate.TierOwner {
		return fmt.Errorf("%w: only the owner may create a %s — connections are owner-managed",
			substrate.ErrForbidden, ty.Name)
	}
	return nil
}

// isBundleOwnerGated reports whether a type is a bundle's config or account
// record — the records whose external create/delete is owner-only.
func isBundleOwnerGated(ty *vocabulary.Kind) bool {
	return ty.Implements(vocabulary.TraitBundleConfigCore) || ty.Implements(vocabulary.TraitAccountConfigCore)
}

// checkPropertyOwnership holds every CHANGED property with a `writer:`
// restriction to its role's actor. Enforced uniformly —
// including internal writes — so the OAuth facility, the connector function
// and the owner each write only their own hands; the console blacklist is a
// convenience, not the boundary.
func (t *txn) checkPropertyOwnership(ty *vocabulary.Kind, accepted []string) error {
	for _, name := range accepted {
		p, ok := ty.Prop(name)
		if !ok || p.Writer == "" {
			continue
		}
		if !t.actorMayWriteProp(p.Writer) {
			return fmt.Errorf("%w: %s.%s is written by the %q role, not %s",
				substrate.ErrForbidden, ty.Identity, name, p.Writer, t.actor)
		}
	}
	return nil
}

// actorMayWriteProp reports whether this transaction's actor fills a
// property-writer role: oauth is the facility's own actor, connector is an
// bundle-tier write context (installed code — the tier is write-context
// data, ticket 002), owner is an owner-tier one.
func (t *txn) actorMayWriteProp(writer string) bool {
	switch writer {
	case vocabulary.WriterOAuth:
		return t.actor == actorOAuth
	case vocabulary.WriterConnector:
		return t.tier == substrate.TierBundle
	case vocabulary.WriterOwner:
		return t.tier == substrate.TierOwner
	}
	return true
}

// storeSecretProps moves every ACCEPTED secret-typed property value into the
// sealed store at the storage boundary, leaving the ref in its place, so the
// records fold and the changelog delta both carry an opaque address and
// never the material. Authorship and server-side state decide what each
// value is, never the value's own bytes:
//
//   - a property this write did not accept carries its stored ref through
//     the merge untouched (legacy plaintext is the reseal migration's to
//     move, not an unrelated patch's);
//   - an accepted value naming an existing sealed row OF THIS RECORD is a
//     carried ref: the auth and OAuth machinery insert their material first
//     and put the ref through here second, in one transaction;
//   - an accepted plaintext equal to the current material keeps the current
//     ref AND leaves the accepted list, so a re-pasted secret neither mints
//     a delta nor steals the property's manager attribution;
//   - any other accepted value stores under a fresh ref, and the ref it
//     replaces is DELETED: rotation erases material rather than retiring it
//     into an immutable log. An accepted deletion erases the same way.
//
// Returns the accepted list with the no-op names pruned.
func (t *txn) storeSecretProps(ty *vocabulary.Kind, owner eref, before, row *erow, accepted []string) ([]string, error) {
	hasSecret := false
	for _, name := range ty.PropOrder {
		if ty.Props[name].Secret() {
			hasSecret = true
			break
		}
	}
	if !hasSecret {
		return accepted, nil
	}
	authored := make(map[string]bool, len(accepted))
	for _, name := range accepted {
		authored[name] = true
	}
	beforeRef := func(name string) string {
		if before == nil {
			return ""
		}
		if old, ok := before.Props[name].(string); ok && strings.HasPrefix(old, secretRefPrefix) {
			return old
		}
		return ""
	}
	drop := map[string]bool{}
	for _, name := range ty.PropOrder {
		if !ty.Props[name].Secret() || !authored[name] {
			continue
		}
		s, ok := row.Props[name].(string)
		if !ok || s == "" {
			// An accepted deletion (or clear) with a stored ref behind it
			// erases the material now.
			if old := beforeRef(name); old != "" {
				if _, err := t.exec(`DELETE FROM sealed WHERE ref = $1`, old); err != nil {
					return nil, err
				}
			}
			continue
		}
		carried, err := t.sealedRefOf(s, owner)
		if err != nil {
			return nil, err
		}
		if carried {
			continue
		}
		old := beforeRef(name)
		if old != "" {
			// openSealedRef locks the row FOR UPDATE, serializing the compare
			// with a concurrent rotation's delete.
			cur, err := t.openSealedRef(old)
			switch {
			case err == nil:
				if subtle.ConstantTimeCompare(cur, []byte(s)) == 1 {
					row.Props[name] = old
					drop[name] = true
					continue
				}
			case errors.Is(err, sql.ErrNoRows):
				// The row is gone (a concurrent rotation or teardown won):
				// nothing to compare against and nothing to erase.
				old = ""
			default:
				// A row that exists but does not open means the credential
				// key is wrong: rotating THROUGH that state would erase
				// material the corrected key could still read, and mask the
				// misconfiguration. Fail the write instead.
				return nil, fmt.Errorf("substrate/engine: open stored secret %s.%s: %w", ty.Identity, name, err)
			}
		}
		ref, err := t.storeSecretValue(owner, s)
		if err != nil {
			return nil, err
		}
		row.Props[name] = ref
		if old != "" {
			if _, err := t.exec(`DELETE FROM sealed WHERE ref = $1`, old); err != nil {
				return nil, err
			}
		}
	}
	if len(drop) == 0 {
		return accepted, nil
	}
	pruned := make([]string, 0, len(accepted))
	for _, name := range accepted {
		if !drop[name] {
			pruned = append(pruned, name)
		}
	}
	return pruned, nil
}

// asInt64 reads an integer stored in jsonb. A json.Number (the runner decodes
// effect numbers with UseNumber so a concurrency token keeps full 64-bit
// fidelity in transit) converts through Int64 directly — never through
// float64, which would round a version past 2^53 to a neighboring integer and
// accept it as that different precondition. Everything else falls back through
// the float path (a value that round-tripped through float64 already).
func asInt64(v any) (int64, bool) {
	if n, ok := v.(json.Number); ok {
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return i, true
	}
	f, err := asFloat(v)
	if err != nil || f != float64(int64(f)) {
		return 0, false
	}
	return int64(f), true
}

// The request ops recordpatchrequest carries: a change request is
// a patch by default (back-compat — an existing request stores no `op`), and
// may instead CREATE or DELETE its target on accept. All three flow through
// the same reviewed transition; nothing here writes without an accept.
const (
	opPatch  = "patch"
	opCreate = "create"
	opDelete = "delete"
)

// propDecision is the change-request state property whose transition to
// accepted/rejected is the reviewed decision (§7).
const propDecision = "decision"

// guardImmutableEnvelope refuses a write that would alter the reviewed
// envelope of an already-existing change request: the operation
// verb, its target fields and the diff are frozen at propose time. `decision`
// (the state) and `rationale` stay mutable — deciding is the whole point, and a
// note is harmless. The stored value may be re-asserted identically (an
// idempotent re-put), but never changed. The target EDGE is guarded in the
// edge loop, where the write's resolved target can be compared to the current
// one (a re-sync of the same target is fine; a swap is not).
func guardImmutableEnvelope(sp *applySpec) error {
	for _, name := range []string{"op", "targetKind", "targetId", "diff"} {
		next, named := sp.props[name]
		if !named {
			continue
		}
		if !jsonEqual(sp.existing.Props[name], next) {
			return fmt.Errorf("%w: %s is immutable on a proposed change request — the reviewed envelope is fixed at propose time",
				substrate.ErrForbidden, name)
		}
	}
	return nil
}

// propertyWritable reports whether a name may appear in a proposed diff for
// ty: a declared property (state properties included — a transition is a
// legitimate patch), the reserved title/body columns, or a temporal column the
// type binds. The identity keys `type`/`id` are never writable. A SECRET-typed
// property is writable in principle, but a proposal never carries it (a raw
// secret must not sit in the request's non-secret json diff) — normalizeDiff
// rejects it separately.
func propertyWritable(ty *vocabulary.Kind, name string) bool {
	switch name {
	case substrate.PropTitle, substrate.PropBody:
		return true
	case substrate.PropAt, substrate.PropEndsAt, substrate.PropDueAt:
		return ty.UsesHot(name)
	}
	_, ok := ty.Prop(name)
	return ok
}

// sensitiveProp reports whether name is a sensitive property of ty — the ones
// a proposed diff must never carry.
func sensitiveProp(ty *vocabulary.Kind, name string) bool {
	p, ok := ty.Prop(name)
	return ok && p.Sensitive()
}

// normalizeDiff validates and normalises a proposed change against the target
// type's schema and returns the stored WRAPPER form `{"properties":{…}}` (plus
// any labels/annotations, and edges when allowEdges) — the shape the accept
// transition decodes. Two input shapes are accepted, consistently:
//
//   - the WRAPPER form — an object carrying a `properties` key (optionally
//     `labels`/`annotations`, and `edges` when allowEdges); any OTHER top-level
//     key is rejected as unknown; and
//   - the BARE form — a plain property map (a real model's `{saved:true}`),
//     coerced into `{"properties":{…}}`.
//
// A top-level `properties` key selects the wrapper form; a bare map with a
// literal `properties` property is not expressible (documented ambiguity, and
// no shipped type declares such a property). Every property named must be
// WRITABLE on ty; the immutable identity keys `type`/`id` and any undeclared
// property are rejected; and a diff that would change nothing is rejected — so
// a malformed proposal never reaches the inbox.
func normalizeDiff(ty *vocabulary.Kind, diff map[string]any, op string) (map[string]any, error) {
	allowEdges := op == opCreate
	if len(diff) == 0 {
		return nil, fmt.Errorf("%w: the diff is empty — name at least one property to change", substrate.ErrValidation)
	}
	out := map[string]any{}
	var propsRaw map[string]any
	if pv, wrapped := diff["properties"]; wrapped {
		pm, ok := pv.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: diff.properties must be an object", substrate.ErrValidation)
		}
		propsRaw = pm
		allowedTop := map[string]bool{"properties": true, "labels": true, "annotations": true}
		if allowEdges {
			allowedTop["edges"] = true
		}
		for _, k := range sortedKeys(diff) {
			if !allowedTop[k] {
				return nil, fmt.Errorf("%w: diff has an unknown top-level key %q — a change wraps its properties under \"properties\"",
					substrate.ErrValidation, k)
			}
			if k != "properties" {
				out[k] = diff[k]
			}
		}
	} else {
		propsRaw = diff
	}
	var problems []string
	for _, name := range sortedKeys(propsRaw) {
		switch name {
		case "type", "id", "metadata":
			problems = append(problems, fmt.Sprintf("%q is immutable — a change cannot alter a record's kind or identity", name))
			continue
		}
		// Declaration is checked BEFORE the null-cleanup exception (review-p0
		// #5): a proposal against an UNDECLARED property is a model-visible
		// error even when it is `{bogus: null}`, so a malformed name never
		// reaches the inbox to become an accept-time no-op or a legacy delete.
		if !propertyWritable(ty, name) {
			problems = append(problems, fmt.Sprintf("%q is not a property of %s", name, ty.Identity))
			continue
		}
		// A raw sensitive value must never sit in the request's json diff:
		// redaction guards the target, but the pending request would expose
		// the value under diff.properties to every reader.
		if sensitiveProp(ty, name) {
			problems = append(problems, fmt.Sprintf("%q is sensitive: a change request must not carry a raw value for it", name))
		}
	}
	if len(problems) > 0 {
		return nil, fmt.Errorf("%w: %s", substrate.ErrValidation, strings.Join(problems, "; "))
	}
	if len(propsRaw) == 0 {
		return nil, fmt.Errorf("%w: the diff names no property to change", substrate.ErrValidation)
	}
	out["properties"] = propsRaw
	// Full value/edge validation against the schema, in a NON-writing pass:
	// strictly decode the operation-specific shape, then run the ordinary
	// property coercion and edge declaration/shape checks, so a wrong-typed
	// value, an undeclared nested field or an undeclared create edge is
	// refused at PROPOSE time instead of only at accept.
	if err := validateDiffShape(ty, out, op); err != nil {
		return nil, err
	}
	return out, nil
}

// validateDiffShape strictly decodes a normalised diff into its
// operation-specific input shape and validates every value and edge WITHOUT
// writing anything.
func validateDiffShape(ty *vocabulary.Kind, norm map[string]any, op string) error {
	raw, err := json.Marshal(norm)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if op == opCreate {
		var in substrate.PutInput
		if err := dec.Decode(&in); err != nil {
			return fmt.Errorf("%w: the create shape is invalid (%w)", substrate.ErrValidation, err)
		}
		return validateCreateShape(ty, in)
	}
	var in substrate.PatchInput
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("%w: the patch shape is invalid (%w)", substrate.ErrValidation, err)
	}
	return validatePatchShape(ty, in)
}

// validatePatchShape coerces a proposed patch's properties and labels through
// the target schema without writing.
func validatePatchShape(ty *vocabulary.Kind, in substrate.PatchInput) error {
	authored, _, _, err := splitProps(ty, in.Properties)
	if err != nil {
		return err
	}
	if _, err := coerceProps(ty, authored); err != nil {
		return err
	}
	if _, err := coerceLabels(substrate.ActorAPI, in.Labels); err != nil {
		return err
	}
	return nil
}

// validateCreateShape coerces a proposed create's properties and labels and
// checks every named edge is declared and well-shaped, without writing. Target
// EXISTENCE is an accept-time concern (the record a create edge points at may
// be minted alongside it), so only declaration and reference shape are checked
// here.
func validateCreateShape(ty *vocabulary.Kind, in substrate.PutInput) error {
	authored, _, _, err := splitProps(ty, in.Properties)
	if err != nil {
		return err
	}
	if _, err := coerceProps(ty, authored); err != nil {
		return err
	}
	if _, err := coerceLabels(substrate.ActorAPI, in.Labels); err != nil {
		return err
	}
	for _, e := range in.Edges {
		ed, ok := ty.Edge(e.Rel)
		if !ok {
			return fmt.Errorf("%w: %s declares no edge %q", substrate.ErrValidation, ty.Name, e.Rel)
		}
		if e.To.ID == "" {
			return fmt.Errorf("%w: edge %q target needs an id", substrate.ErrValidation, e.Rel)
		}
		if e.To.Identity() == "" && ed.To == "any" {
			return fmt.Errorf("%w: edge %q is polymorphic — it needs the full {authority, type, id} reference",
				substrate.ErrValidation, e.Rel)
		}
	}
	return nil
}

// setEffectEmit marks this transaction as EXTENSION DISPATCH: it records the
// effective emit ceiling of the bundle actor whose effects it is about to
// apply, and stamps the bundle tier onto the write context
// explicitly — the dispatcher KNOWS it is running installed
// code, so the tier is stated here, never re-derived from the actor. Called
// at every effect-application site — trigger delivery, schedule/webhook
// fire, host Call, and the agent function-tool loop — so that accepting a
// change request inside those effects can authorize the transitive write
// against the same ceiling.
func (t *txn) setEffectEmit(emit []string) {
	t.effEmit = emit
	t.effEmitSet = true
	t.tier = substrate.TierBundle
}

// authorizeRequestOp bounds an bundle actor's ACCEPT to its effective emit
// set. Owner (and machine) acceptance stays unbounded — a human
// decision is the whole point of the reviewed write. But when a FUNCTION or
// AGENT actor drives the accepted transition, materializing the target would
// otherwise let a capability that can only emit the REQUEST type write an
// arbitrary record: a confused deputy. The type checked is the type that would
// be written — `targetKind` for a create, the target record's type for a patch
// or delete. An bundle actor with no ceiling carried (a generic-API write
// under a function/agent actor) is refused outright: fail closed.
func (t *txn) authorizeRequestOp(op, targetKind string) error {
	if t.tier != substrate.TierBundle {
		return nil
	}
	if !t.effEmitSet {
		return fmt.Errorf("%w: %s may not accept a change request — no effective emit ceiling was carried into acceptance",
			substrate.ErrForbidden, t.actor)
	}
	for _, e := range t.effEmit {
		if e == targetKind {
			return nil
		}
	}
	return fmt.Errorf("%w: %s may not materialize a %s through an accepted %s request — %s is not in its effective emit allowlist",
		substrate.ErrForbidden, t.actor, targetKind, op, targetKind)
}

// isRequestRefusal reports whether an error from materializing an accepted
// request is a DETERMINISTIC refusal — a validation, conflict, guard, authz or
// missing-target failure that re-running would reproduce. Those are wrapped as
// a diffConflict so the still-proposed request carries a conflict annotation
// the owner can read; a non-deterministic error (a real db
// fault) propagates untouched and fails the transaction outright.
func isRequestRefusal(err error) bool {
	return errors.Is(err, substrate.ErrValidation) || errors.Is(err, substrate.ErrConflict) ||
		errors.Is(err, substrate.ErrGuard) || errors.Is(err, substrate.ErrNotFound) ||
		errors.Is(err, substrate.ErrForbidden)
}

// applyEditDiff materializes an accepted change request in the transition's
// transaction (§7). It branches on the request's `op`: a patch applies the
// stored diff to an existing target, a create mints the named record
// create-if-absent, a delete tombstones it — every one idempotent on replay
// and every failure a visible transition failure (a rolled-back diffConflict),
// never a silent green accept. Every deterministic refusal from the op branch
// is centralized into a diffConflict here, so a missing target,
// a vanished edge target, an authz refusal and a divergent create all annotate
// the request instead of failing the transaction with a bare error.
func (t *txn) applyEditDiff(edit *erow) error {
	err := t.materializeAccepted(edit)
	if err == nil {
		return nil
	}
	var dc *diffConflict
	if errors.As(err, &dc) {
		return err
	}
	if isRequestRefusal(err) {
		return &diffConflict{action: onEnterApplyDiff, edit: edit.ref(), err: err}
	}
	return err
}

// materializeAccepted routes an accepted request to its op handler. The
// handlers return PLAIN sentinel errors; applyEditDiff wraps the deterministic
// ones as a diffConflict.
func (t *txn) materializeAccepted(edit *erow) error {
	op, _ := edit.Props["op"].(string)
	switch op {
	case "", opPatch:
		return t.applyPatchRequest(edit)
	case opCreate:
		return t.applyCreateRequest(edit)
	case opDelete:
		return t.applyDeleteRequest(edit)
	default:
		return fmt.Errorf("%w: unknown request op %q — one of create, patch, delete", substrate.ErrValidation, op)
	}
}

// diffEmpty reports whether a decoded patch diff would change nothing at all —
// no property, label, annotation or finalizer named. An accepted request whose
// diff is empty must FAIL the transition, never stamp decidedAt and apply
// nothing.
func diffEmpty(in substrate.PatchInput) bool {
	return len(in.Properties) == 0 && len(in.Labels) == 0 && len(in.Annotations) == 0 &&
		len(in.AddFinalizers) == 0 && len(in.RemoveFinalizers) == 0
}

// applyPatchRequest applies an accepted patch request's stored diff to its
// target, CAS'd against the version the request recorded. The target is read
// from the edge that names it (MODEL §11.5). The decode is STRICT
// (DisallowUnknownFields) and the diff is checked for emptiness and for
// applying no change: a wrapper-less `{saved:true}` fails to decode, an empty
// `{properties:{}}` fails the emptiness check, and a diff the target already
// satisfies fails the no-op check — none of them a silent success.
func (t *txn) applyPatchRequest(edit *erow) error {
	target, err := t.edgeTargetOf(edit.ref(), propTarget)
	if err != nil {
		return err
	}
	if target.ID == "" {
		return fmt.Errorf("%w: patch request %s has no target", substrate.ErrValidation, edit.ID)
	}
	// Resolve the target under the request lock to bound the accept (review-p0
	// #1: the transitive patch is authorized against the accepting actor's
	// effective emit set) and to refuse a target that vanished after propose.
	canon, err := t.lockCanonical(target)
	if err != nil {
		return err
	}
	row, err := t.loadRow(canon, true)
	if err != nil {
		return err
	}
	if row == nil || row.DeletedAt != nil {
		return fmt.Errorf("%w: patch target %s", substrate.ErrNotFound, target.ID)
	}
	if err := t.authorizeRequestOp(opPatch, row.Kind); err != nil {
		return err
	}
	in, err := decodeDiff(edit)
	if err != nil {
		return err
	}
	if diffEmpty(in) {
		return fmt.Errorf(
			"%w: the diff changes nothing — a proposal must name at least one property to change", substrate.ErrValidation)
	}
	// The diff was computed against a version of the target; anything newer
	// means an owner write would be clobbered (§7).
	if in.IfVersion == nil {
		v, ok := asInt64(edit.Props[propTargetVersion])
		if !ok {
			return fmt.Errorf(
				"%w: edit records no %s to check %s against",
				substrate.ErrConflict, propTargetVersion, target.ID)
		}
		in.IfVersion = &v
	}
	ent, err := t.patch(canon, in)
	if err != nil {
		return err
	}
	// No-op suppression means the target's version does not move when the diff
	// applies nothing (the stored value already matched). A green accept that
	// changed nothing is exactly the silent no-op of issue 004: fail it.
	if ent != nil && in.IfVersion != nil && ent.Version == *in.IfVersion {
		return fmt.Errorf(
			"%w: the diff applied no change — the target already matches the proposed values", substrate.ErrValidation)
	}
	return nil
}

// decodeDiff decodes a patch request's stored diff into a PatchInput with
// unknown fields REFUSED: a real model's wrapper-less `{saved:true}` names
// `saved` at the top level, which is not a PatchInput field, so it fails here
// instead of decoding into an empty patch that applies nothing.
func decodeDiff(edit *erow) (substrate.PatchInput, error) {
	var in substrate.PatchInput
	raw, err := json.Marshal(edit.Props["diff"])
	if err != nil {
		return in, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, fmt.Errorf(
			"%w: the diff is not shaped as a patch (%w) — wrap the changes under \"properties\"", substrate.ErrValidation, err)
	}
	return in, nil
}

// applyCreateRequest mints the record a create request names, through the
// ordinary write path under the accepting actor. It is
// create-if-absent + idempotent on replay: the per-record advisory lock makes
// the existence check atomic (the effect ifAbsent pattern, effects.go), so a
// re-materialized request or a second request naming the same id is a no-op,
// never a reset of state a later stage owns.
func (t *txn) applyCreateRequest(edit *erow) error {
	typeIdent, _ := edit.Props["targetKind"].(string)
	targetID, _ := edit.Props["targetId"].(string)
	if typeIdent == "" || targetID == "" {
		return fmt.Errorf("%w: create request names no targetKind/targetId", substrate.ErrValidation)
	}
	ty, err := t.ds.resolveType(typeIdent)
	if err != nil {
		return err
	}
	if err := t.authorizeRequestOp(opCreate, ty.Identity); err != nil {
		return err
	}
	in, err := decodeCreate(edit)
	if err != nil {
		return err
	}
	in.Kind = ty.Identity
	canon, err := t.lockCanonical(eref{Kind: ty.Identity, ID: targetID})
	if err != nil {
		return err
	}
	id := canon.ID
	// Under the advisory lock the check-then-put is atomic. A create against an
	// id that is already taken is idempotent — a verified no-op — ONLY when the
	// live existing record is the very thing the request would mint: same type,
	// and every proposed property and edge already matches. Any
	// other collision — a divergent shape or a tombstone — is a diffConflict,
	// never a green accept that materialized nothing.
	row, err := t.loadRow(canon, true)
	if err != nil {
		return err
	}
	if row != nil {
		match, err := t.existingSatisfiesCreate(row, ty, in)
		if err != nil {
			return err
		}
		if match {
			return nil
		}
		return fmt.Errorf("%w: create request for %s collides with an existing %s that does not match the proposal",
			substrate.ErrConflict, id, row.Kind)
	}
	in.ID = id
	if _, err := t.put(in); err != nil {
		return err
	}
	return nil
}

// existingSatisfiesCreate reports whether a live record already IS what a
// create request would mint: the proposed type, and every proposed property,
// hot column, state and edge already present with the proposed value. A
// tombstoned or wrong-type row never matches — a create neither resurrects nor
// overwrites. The proposal is coerced the way the write path would coerce it,
// so the comparison is against normalised values.
func (t *txn) existingSatisfiesCreate(row *erow, ty *vocabulary.Kind, in substrate.PutInput) (bool, error) {
	if row.DeletedAt != nil || row.Kind != ty.Identity {
		return false, nil
	}
	authored, hot, states, err := splitProps(ty, in.Properties)
	if err != nil {
		return false, err
	}
	props, err := coerceProps(ty, authored)
	if err != nil {
		return false, err
	}
	for _, name := range sortedKeys(props) {
		if !jsonEqual(row.Props[name], props[name]) {
			return false, nil
		}
	}
	if hot.title != nil && row.Title != *hot.title {
		return false, nil
	}
	if hot.body != nil && row.Body != *hot.body {
		return false, nil
	}
	for _, hc := range []struct {
		in  *time.Time
		cur *time.Time
	}{{hot.at, row.At}, {hot.endsAt, row.EndsAt}, {hot.dueAt, row.DueAt}} {
		if hc.in == nil {
			continue
		}
		if hc.cur == nil || !hc.in.Equal(*hc.cur) {
			return false, nil
		}
	}
	for _, name := range sortedKeys(states) {
		if row.States[name] != states[name] {
			return false, nil
		}
	}
	if len(in.Edges) > 0 {
		edges, err := t.edgesOf(row.ref())
		if err != nil {
			return false, err
		}
		for _, e := range in.Edges {
			ed, ok := ty.Edge(e.Rel)
			if !ok {
				return false, nil
			}
			dst, err := t.resolveEdgeRef(ed, e.To)
			if err != nil {
				return false, err
			}
			present := false
			for _, have := range edges[e.Rel] {
				if have == dst {
					present = true
					break
				}
			}
			if !present {
				return false, nil
			}
		}
	}
	return true, nil
}

// decodeCreate decodes a create request's stored diff into a PutInput —
// `{properties:{…}, edges:[…]}` — with unknown fields refused, the same
// strictness the patch path uses.
func decodeCreate(edit *erow) (substrate.PutInput, error) {
	var in substrate.PutInput
	raw, err := json.Marshal(edit.Props["diff"])
	if err != nil {
		return in, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return in, fmt.Errorf(
			"%w: the create shape is undecodable (%w) — wrap properties under \"properties\" and edges under \"edges\"", substrate.ErrValidation, err)
	}
	return in, nil
}

// applyDeleteRequest tombstones a delete request's target through the ordinary
// soft-delete path, idempotent on replay: an already-gone target
// is a verified no-op.
func (t *txn) applyDeleteRequest(edit *erow) error {
	target, err := t.edgeTargetOf(edit.ref(), propTarget)
	if err != nil {
		return err
	}
	if target.ID == "" {
		return fmt.Errorf("%w: delete request %s has no target", substrate.ErrValidation, edit.ID)
	}
	id, err := t.lockCanonical(target)
	if err != nil {
		return err
	}
	row, err := t.loadRow(id, true)
	if err != nil {
		return err
	}
	if row == nil || row.DeletedAt != nil {
		// Already gone: a verified no-op on replay. The delete is idempotent,
		// so no authz gate on a target that no longer exists to tombstone.
		return nil
	}
	// A delete tombstones the target record; the accept is bounded by the
	// accepting bundle actor's emit for THAT record's type.
	if err := t.authorizeRequestOp(opDelete, row.Kind); err != nil {
		return err
	}
	if _, err := t.softDelete(id); err != nil {
		return err
	}
	return nil
}

// applyMergeRequest performs an accepted merge request: winner and loser come
// from the request's edges and Merge's OWN guards re-run here, in the
// transition's transaction. A refused merge — already merged away (the loser
// edge then points at the winner and the merge is self-into-self), deleted,
// type mismatch — fails the transition whole; the caller rolls back and
// annotates the request, exactly the applyDiff conflict pattern.
func (t *txn) applyMergeRequest(req *erow) error {
	winner, err := t.edgeTargetOf(req.ref(), "winner")
	if err != nil {
		return err
	}
	loser, err := t.edgeTargetOf(req.ref(), "loser")
	if err != nil {
		return err
	}
	if winner.ID == "" || loser.ID == "" {
		return fmt.Errorf("%w: merge request %s names no winner/loser", substrate.ErrValidation, req.ID)
	}
	if _, err := t.mergeRecord(winner, loser); err != nil {
		if errors.Is(err, substrate.ErrConflict) || errors.Is(err, substrate.ErrValidation) ||
			errors.Is(err, substrate.ErrNotFound) || errors.Is(err, substrate.ErrForbidden) {
			return &diffConflict{action: onEnterApplyMerge, edit: req.ref(), err: err}
		}
		return err
	}
	return nil
}

func (ds *dataset) Delete(ctx context.Context, actor substrate.Actor, typ, id string) (*substrate.Record, error) {
	ty, err := ds.resolveType(typ)
	if err != nil {
		return nil, err
	}
	// A schema record's delete removes the declaration through admission: the
	// closure must hold without it, and a type with live instances refuses
	// (schemawrite.go).
	if _, isVocabulary := vocabularyRecordKinds[ty.Identity]; isVocabulary {
		existing, err := ds.Get(ctx, ty.Identity, id)
		if err != nil {
			return nil, err
		}
		return ds.deleteVocabularyRecord(ctx, actor, existing)
	}
	return ds.deleteWith(ctx, actor, eref{Kind: ty.Identity, ID: id}, false)
}

func (ds *dataset) deleteWith(ctx context.Context, actor substrate.Actor, ref eref, internal bool) (*substrate.Record, error) {
	var out *substrate.Record
	err := ds.inTx(ctx, actor, internal, func(t *txn) error {
		e, err := t.softDelete(ref)
		out = e
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// softDelete tombstones one record inside the caller's transaction — the
// delete path proper, shared by the Delete mutation and a function's delete
// effect.
func (t *txn) softDelete(ref eref) (*substrate.Record, error) {
	// A former id denotes its canonical record everywhere (§6.3) —
	// including here, or a delete through a merged-away id 404s while
	// every read and patch of the same id resolves. Lock, then resolve: the
	// addressing must not race a merge.
	ref, err := t.lockCanonical(ref)
	if err != nil {
		return nil, err
	}
	id := ref.ID
	row, err := t.loadRow(ref, true)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, fmt.Errorf("%w: record %s", substrate.ErrNotFound, id)
	}
	ty, err := t.ds.resolveType(row.Kind)
	if err != nil {
		return nil, err
	}
	// Revoking a token is a delete; the projection prune deletes internally.
	if !t.internal && systemKinds[ty.Identity] && ty.Identity != kindToken {
		return nil, fmt.Errorf("%w: %s records are managed by the substrate", substrate.ErrForbidden, ty.Identity)
	}
	// Bundle lifecycle holds deletes too: read-only when uninstalled, frozen
	// config/accounts when disabled (engine/bundles.go); purge is internal.
	if err := t.checkBundleDelete(ty); err != nil {
		return nil, err
	}
	// External delete of a bundle's config/account record is owner-only: the
	// facility's finalizer teardown and purge run internally and bypass; a
	// non-owner API actor may not tear down a connection.
	if !t.internal && isBundleOwnerGated(ty) && t.tier != substrate.TierOwner {
		return nil, fmt.Errorf("%w: only the owner may delete a %s — connections are owner-managed",
			substrate.ErrForbidden, ty.Name)
	}
	if row.DeletedAt == nil {
		if _, err := t.tombstone(ref, ""); err != nil {
			return nil, err
		}
		row.Version++
		row.DeletedAt = &t.now
		row.UpdatedAt = t.now
		if err := t.appendChange(t.actor, substrate.OpDelete, id, row.Kind, map[string]any{
			"finalizers": row.Finalizers,
		}); err != nil {
			return nil, err
		}
		// A tombstoned trigger takes its bookkeeping with it: cursor, parked
		// failures and fire state. A restore re-initializes at its own moment.
		if row.Kind == typeTrigger {
			if err := t.dropTriggerBookkeeping(id); err != nil {
				return nil, err
			}
		}
		// Deleting a source record changes the set its subject recomputes
		// from — and delete does not run through apply, so the trigger has
		// to be here, or the subject keeps values no source carries.
		if m, ok := t.ds.registry().MappingFor(ty.Identity); ok {
			if err := t.recomputeSubjectOf(ref, m); err != nil {
				return nil, err
			}
		}
	}
	return t.record(row, ty)
}

func (ds *dataset) Link(ctx context.Context, actor substrate.Actor, srcType, src, rel string, to substrate.EdgeRef, props map[string]any) error {
	return ds.inTx(ctx, actor, false, func(t *txn) error {
		ty, err := t.ds.resolveType(srcType)
		if err != nil {
			return err
		}
		return t.link(rel, eref{Kind: ty.Identity, ID: src}, to, props)
	})
}

// link writes one edge inside the caller's transaction — the Link mutation's
// body, shared with a function's link effect. Both ends canonicalize: an
// edge written at a former id belongs to the record that id now denotes,
// whichever end carried it. Both ends lock BEFORE resolving (in ascending
// (type, id) order, merge's own order), so the addressing cannot race a
// merge. The target reference is `{authority, type, id}`; bare `{id}` is legal
// only where the declaration pins a single target type.
func (t *txn) link(rel string, src eref, to substrate.EdgeRef, props map[string]any) error {
	if to.ID == "" {
		return fmt.Errorf("%w: edge target needs an id", substrate.ErrValidation)
	}
	ty, err := t.ds.resolveType(src.Kind)
	if err != nil {
		return err
	}
	if err := t.guardSystemKind(ty, substrate.OpLink); err != nil {
		return err
	}
	ed, ok := ty.Edge(rel)
	if !ok {
		return fmt.Errorf("%w: %s declares no edge %q", substrate.ErrValidation, ty.Name, rel)
	}
	// The reviewed target of a change request is immutable after propose: a
	// generic Link must not restamp the single `target` edge under an
	// undecided request and slip a version-matching substitute past the
	// accept's CAS.
	if ty.Identity == vocabulary.KindRecordPatchRequest && rel == propTarget {
		return fmt.Errorf("%w: the target edge is immutable on a change request — the reviewed target is fixed at propose time",
			substrate.ErrForbidden)
	}
	if err := t.refuseSubjectRel(ty, rel, substrate.OpLink); err != nil {
		return err
	}
	dstType, err := t.edgeTargetType(ed, to)
	if err != nil {
		return err
	}
	src, dst, err := t.lockCanonicalPair(src, eref{Kind: dstType, ID: to.ID})
	if err != nil {
		return err
	}
	srcRow, err := t.loadRow(src, true)
	if err != nil {
		return err
	}
	if srcRow == nil {
		return fmt.Errorf("%w: record %s", substrate.ErrNotFound, src.ID)
	}
	dstRow, err := t.loadRow(dst, false)
	if err != nil {
		return err
	}
	if dstRow == nil {
		return fmt.Errorf("%w: record %s", substrate.ErrNotFound, dst.ID)
	}
	changed := false
	if !ed.Many {
		cleared, err := t.replaceSingleEdge(rel, src, dst)
		if err != nil {
			return err
		}
		changed = cleared
	}
	// The subject rel is refused above, so a raw link never writes a
	// subject row.
	ok2, err := t.putEdge(rel, src, dst, props, false)
	if err != nil {
		return err
	}
	if !changed && !ok2 {
		return nil
	}
	if err := t.bumpVersion(src); err != nil {
		return err
	}
	return t.appendChange(t.actor, substrate.OpLink, src.ID, srcRow.Kind, map[string]any{
		"rel": rel, "dst": dst.ID, "dstType": dst.Kind,
	})
}

func (ds *dataset) Unlink(ctx context.Context, actor substrate.Actor, srcType, src, rel string, to substrate.EdgeRef) error {
	return ds.inTx(ctx, actor, false, func(t *txn) error {
		ty, err := t.ds.resolveType(srcType)
		if err != nil {
			return err
		}
		return t.unlink(rel, eref{Kind: ty.Identity, ID: src}, to)
	})
}

// unlink removes one edge inside the caller's transaction — the Unlink
// mutation's body, shared with a function's unlink effect. Both ends lock
// before resolving, exactly as link does.
func (t *txn) unlink(rel string, src eref, to substrate.EdgeRef) error {
	if to.ID == "" {
		return fmt.Errorf("%w: edge target needs an id", substrate.ErrValidation)
	}
	ty, err := t.ds.resolveType(src.Kind)
	if err != nil {
		return err
	}
	if err := t.guardSystemKind(ty, substrate.OpUnlink); err != nil {
		return err
	}
	if ty.Identity == vocabulary.KindRecordPatchRequest && rel == propTarget {
		return fmt.Errorf("%w: the target edge is immutable on a change request — the reviewed target is fixed at propose time",
			substrate.ErrForbidden)
	}
	dstType := ""
	if ed, ok := ty.Edge(rel); ok {
		if err := t.refuseSubjectRel(ty, rel, substrate.OpUnlink); err != nil {
			return err
		}
		dstType, err = t.edgeTargetType(ed, to)
		if err != nil {
			return err
		}
	} else {
		if named := to.Identity(); named != "" {
			want, err := t.ds.resolveType(named)
			if err != nil {
				return err
			}
			dstType = want.Identity
		} else {
			return fmt.Errorf("%w: %s declares no edge %q — the target needs the full {authority, type, id} reference",
				substrate.ErrValidation, ty.Name, rel)
		}
	}
	src, dst, err := t.lockCanonicalPair(src, eref{Kind: dstType, ID: to.ID})
	if err != nil {
		return err
	}
	srcRow, err := t.loadRow(src, true)
	if err != nil {
		return err
	}
	if srcRow == nil {
		return fmt.Errorf("%w: record %s", substrate.ErrNotFound, src.ID)
	}
	removed, err := t.deleteEdge(rel, src, dst)
	if err != nil || !removed {
		return err
	}
	if err := t.bumpVersion(src); err != nil {
		return err
	}
	return t.appendChange(t.actor, substrate.OpUnlink, src.ID, srcRow.Kind, map[string]any{
		"rel": rel, "dst": dst.ID, "dstType": dst.Kind,
	})
}

// edgeTargetType resolves the TYPE an edge reference names: the reference's
// own {authority, type} when it carries one (checked against the declaration),
// else the declaration's single target type. A bare {id} on a `to: any` edge
// is refused — the full form is required there.
func (t *txn) edgeTargetType(ed *vocabulary.Edge, ref substrate.EdgeRef) (string, error) {
	named := ref.Identity()
	if named != "" {
		want, err := t.ds.resolveType(named)
		if err != nil {
			return "", err
		}
		if ed.To != "any" && want.Identity != ed.To {
			// The one-hop resolution (resolveEdgeRef) may still land this on
			// the declared type; here the named type is simply returned and the
			// caller decides.
			return want.Identity, nil
		}
		return want.Identity, nil
	}
	if ed.To == "any" {
		return "", fmt.Errorf("%w: a polymorphic edge needs the full {authority, type, id} reference",
			substrate.ErrValidation)
	}
	return ed.To, nil
}

// resolveEdgeRef turns a write's edge target into a full (type, id) identity.
// A reference is `{authority, type, id}`; a bare `{id}` is shorthand on a
// single-target edge, where the declaration supplies the type — resolution
// scopes to that type — and `to: any` requires the full form. ONE HOP is
// allowed: where the edge declares a type T and the reference names a record
// whose own subject edge points at a T, the stored edge lands on that T.
func (t *txn) resolveEdgeRef(ed *vocabulary.Edge, ref substrate.EdgeRef) (eref, error) {
	if ref.ID == "" {
		return eref{}, fmt.Errorf("%w: edge target needs an id", substrate.ErrValidation)
	}
	named, err := t.edgeTargetType(ed, ref)
	if err != nil {
		return eref{}, err
	}
	// Lock, then resolve: an edge target addressed by a former id must not
	// race the merge that made it one.
	target, err := t.lockCanonical(eref{Kind: named, ID: ref.ID})
	if err != nil {
		return eref{}, err
	}
	row, err := t.loadRow(target, false)
	if err != nil {
		return eref{}, err
	}
	if row == nil {
		return eref{}, fmt.Errorf("%w: edge target %s %s", substrate.ErrNotFound, named, ref.ID)
	}
	if ed.To == "any" || row.Kind == ed.To {
		return target, nil
	}
	// One hop: the reference may name a source record for the declared type.
	ty, err := t.ds.resolveType(row.Kind)
	if err != nil {
		return eref{}, err
	}
	if m, ok := t.ds.registry().MappingFor(ty.Identity); ok && m.To == ed.To {
		id, err := t.subjectOf(row, ty)
		if err != nil {
			return eref{}, err
		}
		return eref{Kind: m.To, ID: id}, nil
	}
	return eref{}, fmt.Errorf("%w: edge points at %s, not %s", substrate.ErrValidation, ed.To, row.Kind)
}

// --- shared helpers ---

func (t *txn) guardSystemKind(ty *vocabulary.Kind, op substrate.Op) error {
	if t.internal || !systemKinds[ty.Identity] {
		return nil
	}
	return fmt.Errorf("%w: %s records are managed by the substrate, not the generic %s surface",
		substrate.ErrForbidden, ty.Identity, op)
}

// preRecordLocks takes the locks the global order (the contract:
// registry-dep < subject-type < record) places BEFORE a write's own record
// lock:
//
//   - the SHARED registry-dependency lock for a trigger write, so an owner
//     trigger write can never hold the trigger's record lock while a connector
//     registration holds the dep lock EXCLUSIVE and reaches for that same
//     record through its default-trigger installer;
//   - the subject-type lock for a mapping-SOURCE write, matching the effect
//     lock plan so a source write and an effect list never
//     order subject|<type> and the source's record lock in opposite ways.
//
// Both are reentrant, so the per-site re-acquisitions — apply's trigger
// admission (validateTriggerRow under the same shared lock), matchOrLink's
// subject serialize — are free. A recompute's own internal writes skip it:
// they run inside a write that already ordered these locks, and recompute
// never writes a trigger or first-resolves a subject.
func (t *txn) preRecordLocks(ty *vocabulary.Kind) error {
	if t.recomputing {
		return nil
	}
	if ty.Identity == typeTrigger && !t.internal {
		if err := t.lockKeyShared(registryDepKey(t.ds)); err != nil {
			return err
		}
	}
	if m, ok := t.ds.registry().MappingFor(ty.Identity); ok {
		if err := t.lockKey("subject|" + m.To); err != nil {
			return err
		}
	}
	return nil
}

func checkCAS(existing *erow, ifVersion *int64) error {
	if ifVersion == nil {
		return nil
	}
	var have int64
	if existing != nil {
		have = existing.Version
	}
	if have != *ifVersion {
		return fmt.Errorf("%w: ifVersion %d, stored %d", substrate.ErrConflict, *ifVersion, have)
	}
	return nil
}

func mergeFinalizers(cur, add, remove []string) []string {
	set := map[string]bool{}
	for _, f := range cur {
		set[f] = true
	}
	for _, f := range add {
		set[f] = true
	}
	for _, f := range remove {
		delete(set, f)
	}
	out := make([]string, 0, len(set))
	for f := range set {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}

func jsonEqual(a, b any) bool {
	ra, err1 := json.Marshal(a)
	rb, err2 := json.Marshal(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return bytes.Equal(ra, rb)
}
