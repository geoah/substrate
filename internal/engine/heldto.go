package engine

// WHOSE RULES HOLD A RUN'S WORK, STORED ON ITS OWN RECORDS (decision 0276).
//
// A run's work is held to the persons it acts for, by their roles, and to the
// apps it works for, by their tokens' grants (run.go). Work that goes on after
// the request that started it (an approved call, a background job, an
// answered ask, a resumed turn) reads who those are from the record it stands
// for, its `heldTo`, which the engine writes and nothing else may:
//
//   - a thread's at each turn's claim: the turn's person and token;
//   - a message's, an ask's and an approval's as the run writes it: the turn
//     it is written in, so a completion message carries the links of the
//     item it completes and of whoever continued it;
//   - a person or an app that continues an ask or an approval (answers,
//     approves, declines) is added to it in the same transaction, and the
//     holders before stay; the system message that reports it into the
//     thread, and starts the resume, carries the same.
//
// Work reads its item's `heldTo` and never the thread's: the thread's names
// the turn running now, which may be another person's than the one an older
// item belongs to, and nothing searches a history for a token. The one
// exception is a run a restart cut short in the middle of its turn, which is
// that turn's and reads the thread's.
//
// An item with no `heldTo` was written before this release, when nothing
// recorded it: its work is refused, the report of it carries none either and
// says so, so no resume runs from it, and the person asks again. A `heldTo`
// that is present and empty holds the work to nobody but its agent's own
// declaration: a trigger's.

import (
	"context"
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

// propHeldTo is the property on each held kind.
const propHeldTo = "heldTo"

// heldKinds are the records a run's work stands for.
var heldKinds = map[string]bool{
	typeThread: true, typeMessage: true, vocabulary.KindLLMInteraction: true, vocabulary.KindLLMApproval: true,
}

// errAskAgain refuses work whose item was written before its holders were
// recorded.
var errAskAgain = fmt.Errorf("%w: %w: this was asked before the release that records whose rules hold an agent's work: ask again", substrate.ErrForbidden, substrate.ErrNotPermitted)

// holders are the actors and the tokens one piece of work is held to: actor
// keys, and token record ids.
type holders struct {
	onBehalfOf  substrate.Actor
	continuedBy []substrate.Actor
	tokens      []string
}

// holdersOf is what holds the work ctx carries for hand: the person it acts
// for (the hand itself when a person writes in their own name), the persons
// and the tokens its records added (run.go), and the request's own token.
func holdersOf(ctx context.Context, hand substrate.Actor) holders {
	h := holders{onBehalfOf: substrate.OnBehalfOfFrom(ctx)}
	if h.onBehalfOf == "" && substrate.IsPerson(hand) {
		h.onBehalfOf = hand
	}
	for _, p := range heldPersons(ctx) {
		if p != h.onBehalfOf && !slices.Contains(h.continuedBy, p) {
			h.continuedBy = append(h.continuedBy, p)
		}
	}
	h.tokens = runTokens(ctx)
	return h
}

// withHolders is ctx bound to h: the person, the persons who continued the
// work and the tokens, as heldBy binds a stored `heldTo`.
func withHolders(ctx context.Context, h holders) context.Context {
	return withRunHolders(substrate.WithOnBehalfOf(ctx, h.onBehalfOf), h.continuedBy, h.tokens...)
}

// value renders h as the stored `heldTo`: links to the actor records and the
// token records. Empty, it is an empty object, which says nobody holds the
// work but its agent.
func (h holders) value() map[string]any {
	out := map[string]any{}
	if h.onBehalfOf != "" {
		out["onBehalfOf"] = vocabulary.ActorRef(actorID(h.onBehalfOf))
	}
	if len(h.continuedBy) > 0 {
		list := make([]any, len(h.continuedBy))
		for i, a := range h.continuedBy {
			list[i] = vocabulary.ActorRef(actorID(a))
		}
		out["continuedBy"] = list
	}
	if len(h.tokens) > 0 {
		list := make([]any, len(h.tokens))
		for i, id := range h.tokens {
			list[i] = map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindToken, id)}
		}
		out["tokens"] = list
	}
	return out
}

// storedHolders are a stored `heldTo` as record ids: false when the record
// holds none.
type storedHolders struct {
	onBehalfOf  string
	continuedBy []string
	tokens      []string
}

func storedHoldersOf(v any) (storedHolders, bool) {
	m, ok := v.(map[string]any)
	if !ok {
		return storedHolders{}, false
	}
	var out storedHolders
	_, out.onBehalfOf, _ = vocabulary.SplitRecordPath(storedReferencePath(m["onBehalfOf"]))
	for _, item := range listOf(m["continuedBy"]) {
		if _, id, ok := vocabulary.SplitRecordPath(storedReferencePath(item)); ok {
			out.continuedBy = append(out.continuedBy, id)
		}
	}
	for _, item := range listOf(m["tokens"]) {
		if _, id, ok := vocabulary.SplitRecordPath(storedReferencePath(item)); ok {
			out.tokens = append(out.tokens, id)
		}
	}
	return out, true
}

func listOf(v any) []any {
	list, _ := v.([]any)
	return list
}

// stampHeldTo writes the engine's `heldTo` into a held kind's row, after the
// write's own checks: a hand never names it (its `writer:` is the system),
// and the value is the work's, read from the transaction's context. It
// answers the properties the delta carries.
//
//   - A record the run creates, and a thread a turn claims (claimThread,
//     never a hand's write of its lease), take the holders of the work
//     writing them.
//   - An ask or an approval a person or an app continues keeps its holders
//     and adds the continuer and the continuing request's token; one written
//     before this release has none, and is left without, so the work it
//     would resume is refused (errAskAgain).
func (t *txn) stampHeldTo(sp *applySpec, row, before *erow, create bool, accepted []string) []string {
	kind := sp.ty.Identity
	if !heldKinds[kind] || sp.ty.Props[propHeldTo] == nil {
		return accepted
	}
	claim := kind == typeThread && t.claiming
	switch {
	case create && kind == typeMessage && t.report != nil:
		// A report row carries the holders of the item it reports, which its
		// writer passed (putThreadSystemRowID), never the writer's own; none
		// is an item asked before this release, whose report carries none.
		if t.report.held == nil {
			return accepted
		}
		row.Props[propHeldTo] = maps.Clone(t.report.held)
	case create || claim:
		h := holdersOf(t.ctx, t.actor)
		// A person's key names the first person; the request's identity says
		// which person of the repository it is (people.go).
		if substrate.IsPerson(h.onBehalfOf) {
			h.onBehalfOf = t.ds.personActorFor(t.ctx, h.onBehalfOf)
		}
		row.Props[propHeldTo] = h.value()
	case kind == vocabulary.KindLLMInteraction || kind == vocabulary.KindLLMApproval:
		switch substrate.ActorTypeOf(t.actor) {
		case substrate.ActorTypePerson, substrate.ActorTypeApp:
		default:
			return accepted
		}
		held, ok := before.Props[propHeldTo].(map[string]any)
		if !ok {
			return accepted
		}
		row.Props[propHeldTo] = t.continuedBy(held)
	default:
		return accepted
	}
	if !slices.Contains(accepted, propHeldTo) {
		accepted = append(accepted, propHeldTo)
	}
	return accepted
}

// continuedBy is held with the hand of this transaction added as one who
// continued the work, a person or an app, and the request's token: the
// holders before stay. Any other hand continues nothing.
func (t *txn) continuedBy(held map[string]any) map[string]any {
	next := maps.Clone(held)
	switch substrate.ActorTypeOf(t.actor) {
	case substrate.ActorTypePerson, substrate.ActorTypeApp:
	default:
		return next
	}
	ids, _ := storedHoldersOf(held)
	continuer := actorID(t.actor)
	if substrate.IsPerson(t.actor) {
		continuer = actorID(t.ds.personActorFor(t.ctx, t.actor))
	}
	if continuer != ids.onBehalfOf && !slices.Contains(ids.continuedBy, continuer) {
		next["continuedBy"] = append(slices.Clone(listOf(next["continuedBy"])), vocabulary.ActorRef(continuer))
	}
	if tok := substrate.PrincipalFrom(t.ctx); tok != "" && !slices.Contains(ids.tokens, tok) {
		next["tokens"] = append(slices.Clone(listOf(next["tokens"])),
			map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindToken, tok)})
	}
	return next
}

// reportHolders are the holders a report row is written with (txn.report).
type reportHolders struct{ held map[string]any }

// heldOf is a stored `heldTo`, nil where the record holds none.
func heldOf(v any) map[string]any {
	m, _ := v.(map[string]any)
	return m
}

// itemHolders are the holders the report of the resolved item rec of ty
// carries: a run's record's own `heldTo`; for a change request, which is no
// run's record, whom the line that created it names, and the hand deciding
// it as one who continued it. Nil where the item has none: it was asked
// before this release.
func (t *txn) itemHolders(ty *vocabulary.Kind, rec *erow) (map[string]any, error) {
	switch {
	case heldKinds[ty.Identity]:
		return heldOf(rec.Props[propHeldTo]), nil
	case ty.Identity == vocabulary.KindRecordPatchRequest:
		return t.requestHolders(rec)
	}
	return nil, nil
}

// requestHolders are a change request's holders: the person and the token
// the line that created it names, and the hand deciding it now. A request
// whose creating line is gone has none.
func (t *txn) requestHolders(req *erow) (map[string]any, error) {
	if err := t.ds.svc.ruleFault("holders"); err != nil {
		return nil, fmt.Errorf("read whom change request %s acts for: %w", req.ID, err)
	}
	var actor, behalf, token string
	err := t.row(`SELECT actor, coalesce(on_behalf_of, ''), coalesce(principal, '') FROM changelog
		WHERE kind = $1 AND record_id = $2 ORDER BY seq LIMIT 1`, vocabulary.KindRecordPatchRequest, req.ID).Scan(&actor, &behalf, &token)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read whom change request %s acts for: %w", req.ID, err)
	}
	if behalf == "" {
		key, err := t.ds.actorKeyOfID(t.ctx, actor)
		if err != nil {
			return nil, err
		}
		if substrate.ActorTypeOf(key) == substrate.ActorTypePerson {
			behalf = actor
		}
	}
	held := map[string]any{}
	if behalf != "" {
		held["onBehalfOf"] = vocabulary.ActorRef(behalf)
	}
	if token != "" {
		held["tokens"] = []any{map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindToken, token)}}
	}
	return t.continuedBy(held), nil
}

// heldBy is ctx bound to whose rules hold the work the record kind/id stands
// for, read from its `heldTo`: the person it acts for, the persons who
// continued it and the tokens. A record with none was written before this
// release, and its work is refused; a lookup that fails is an error, so no
// work goes on for want of it.
func (ds *dataset) heldBy(ctx context.Context, kind, id string) (context.Context, error) {
	if err := ds.svc.ruleFault("holders"); err != nil {
		return ctx, fmt.Errorf("read whose rules hold %s %s: %w", kind, id, err)
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: kind, ID: id})
	if err != nil {
		return ctx, fmt.Errorf("read whose rules hold %s %s: %w", kind, id, err)
	}
	if row == nil {
		return ctx, fmt.Errorf("%w: %s %s is gone", substrate.ErrNotFound, kind, id)
	}
	held, ok := storedHoldersOf(row.Props[propHeldTo])
	if !ok {
		return ctx, errAskAgain
	}
	behalf, err := ds.actorKeyOfID(ctx, held.onBehalfOf)
	if err != nil {
		return ctx, err
	}
	var persons []substrate.Actor
	for _, id := range held.continuedBy {
		key, err := ds.actorKeyOfID(ctx, id)
		if err != nil {
			return ctx, err
		}
		persons = append(persons, key)
	}
	return withRunHolders(substrate.WithOnBehalfOf(ctx, behalf), persons, held.tokens...), nil
}

// actorKeyOfID is the hand an actor record id stands for, read from the
// record where the id does not say it; "" for "". Unlike actorKeyIn, a read
// that fails is an error.
func (ds *dataset) actorKeyOfID(ctx context.Context, id string) (substrate.Actor, error) {
	if id == "" {
		return "", nil
	}
	if err := ds.svc.ruleFault("holders"); err != nil {
		return "", fmt.Errorf("read the actor %s the work is held to: %w", id, err)
	}
	if key, ok := vocabulary.ActorKeyOf(id); ok {
		return key, nil
	}
	if id == actorID(substrate.ActorPerson) {
		return substrate.ActorPerson, nil
	}
	var raw []byte
	err := ds.db.QueryRowContext(ctx, `SELECT props FROM records WHERE kind = $1 AND id = $2`, kindActor, id).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return substrate.ActorByID(id), nil
	}
	if err != nil {
		return "", fmt.Errorf("read the actor %s the work is held to: %w", id, err)
	}
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		return "", fmt.Errorf("read the actor %s the work is held to: %w", id, err)
	}
	if key, ok := vocabulary.ActorKeyFrom(props); ok {
		return key, nil
	}
	return substrate.ActorByID(id), nil
}

// settlingHeld is heldBy for a settlement after a stop: the engine's own
// bookkeeping of work a stop left behind, which an open must finish. Work
// from before this release, or whose record is gone, is settled for nobody,
// and its report carries the item's holders, none (putThreadSystemRowID);
// a person the history took out is settled for nobody too (settling).
func (ds *dataset) settlingHeld(ctx context.Context, kind, id string) (context.Context, error) {
	held, err := ds.heldBy(ctx, kind, id)
	if errors.Is(err, errAskAgain) || errors.Is(err, substrate.ErrNotFound) {
		return substrate.WithOnBehalfOf(ctx, ""), nil
	}
	return ds.settling(held, err)
}

// capturedBy is what a stored snapshot of reads (the pinned block) was
// captured under, as one string: whose work it was, and answer, what the
// reads' own scope answered for each pinned kind (agentpinned.go
// pinnedAnswer). It reads nothing: the answer is the reads' own, so there is
// no second account of what a holder may read.
func (ds *dataset) capturedBy(ctx context.Context, hand substrate.Actor, answer string) string {
	h := holdersOf(ctx, hand)
	if substrate.IsPerson(h.onBehalfOf) {
		h.onBehalfOf = ds.personActorFor(ctx, h.onBehalfOf)
	}
	return captureKey(h, answer)
}

// captureKey is capturedBy's string: the holders' ids, sorted where order
// says nothing, and the scope's answer.
func captureKey(h holders, answer string) string {
	cont := make([]string, len(h.continuedBy))
	for i, a := range h.continuedBy {
		cont[i] = actorID(a)
	}
	slices.Sort(cont)
	toks := slices.Clone(h.tokens)
	slices.Sort(toks)
	return actorID(h.onBehalfOf) + "\x00" + strings.Join(cont, ",") + "\x00" + strings.Join(toks, ",") + "\x00" + answer
}
