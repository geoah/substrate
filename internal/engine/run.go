package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/geoah/substrate/internal/reach"
	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// WHAT HOLDS A RUN (decision 0276). Whose rules hold the reads and writes of
// a run is read from the run's own records, here, each time it reads or
// writes; no door decides it once. Every read and every write of the run's
// code passes all of:
//
//   - its code's own declaration (handWrites; a read's allowlist);
//   - the persons the work acts for, by the roles they hold now: the one
//     the request driving the turn names, or for work read back from its
//     record, the `heldTo` of that record (heldto.go heldBy), and every
//     person who continued it;
//   - the apps it works for, by their token's grant now: the token of the
//     request driving the turn, or the tokens the record's `heldTo` names,
//     a continuer's among them. An app's token revoked or deleted refuses
//     the rest of the work it holds.
//
// A later turn, started by another hand, is held by that hand. Work a turn
// leaves behind (an approved call, a background job, a resume) is held by
// its own record, never by the thread's latest turn. A hand that continues a run it did not start (an
// answer, an approval, a decision, a chat message) is admitted only where
// it could have started that run itself (mayContinue).
//
// The one thing a run writes outside the persons and the apps is the record
// of the run itself, under one of the exemptions below. Beside them, the
// engine's own hand (txn.internal) writes its bookkeeping past the check:
// every place that takes it, and every place that names an exemption, is
// listed in oneCheckSkips (onecheckskips_test.go), which fails on any other.

// exemptOp names one operation of a run that is the record of the run, not
// work it does for anyone: what it writes is held to the hand that writes
// it alone. These are every exemption there is.
type exemptOp string

const (
	// exemptConversation is the loop's own write of a run's conversation:
	// its thread, the messages it posts (the user's turn among them), the
	// approvals it lands, the jobs it records and the resolution an answer
	// writes into it (agentloop.go, agentjobs.go, approvals.go,
	// agentdecision.go).
	exemptConversation exemptOp = "the run's own conversation"
	// exemptAsk is the ask tool's interaction (interactions.go): the question
	// itself, which a person answers.
	exemptAsk exemptOp = "the ask"
	// exemptRunRecord is a trigger's run record (functions.go putSystemRun):
	// what a delivery did, which the engine keeps whoever the delivery
	// acted for.
	exemptRunRecord exemptOp = "a trigger run's own record"
)

// exemptKinds are the kinds each exemption writes: an exempt transaction
// that writes any other kind is held as any write is.
var exemptKinds = map[exemptOp][]string{
	exemptConversation: {typeThread, typeMessage, vocabulary.KindLLMApproval},
	exemptAsk:          {vocabulary.KindLLMInteraction},
	exemptRunRecord:    {typeTriggerRun},
}

// exempts reports whether this transaction is an exemption writing a kind
// that exemption writes.
func (t *txn) exempts(kind string) bool {
	return t.exempt != "" && slices.Contains(exemptKinds[t.exempt], kind)
}

// runRecordTx runs fn as the record of a run, under op.
func (ds *dataset) runRecordTx(ctx context.Context, actor substrate.Actor, op exemptOp, fn func(*txn) error) error {
	return ds.inTx(ctx, actor, false, func(t *txn) error {
		t.exempt = op
		return fn(t)
	})
}

// runHolders are the persons and the tokens a piece of work carries from its
// records, beyond the request it runs in.
type runHolders struct {
	persons []substrate.Actor
	tokens  []string
}

type runHoldersKey struct{}

// withRunHolders adds persons and tokens to what holds the work in ctx.
func withRunHolders(ctx context.Context, persons []substrate.Actor, tokens ...string) context.Context {
	h, _ := ctx.Value(runHoldersKey{}).(runHolders)
	next := runHolders{persons: slices.Clone(h.persons), tokens: slices.Clone(h.tokens)}
	for _, p := range persons {
		if substrate.ActorTypeOf(p) == substrate.ActorTypePerson && !slices.Contains(next.persons, p) {
			next.persons = append(next.persons, p)
		}
	}
	for _, tok := range tokens {
		if tok != "" && !slices.Contains(next.tokens, tok) {
			next.tokens = append(next.tokens, tok)
		}
	}
	return context.WithValue(ctx, runHoldersKey{}, next)
}

// heldPersons are the persons whose roles hold the work in ctx beside the
// one it acts for.
func heldPersons(ctx context.Context) []substrate.Actor {
	h, _ := ctx.Value(runHoldersKey{}).(runHolders)
	return h.persons
}

// runTokens are the tokens whose grants hold the work in ctx: the request's
// own, which is its turn's at the turn's start, and every one the work's
// records added.
func runTokens(ctx context.Context) []string {
	h, _ := ctx.Value(runHoldersKey{}).(runHolders)
	out := slices.Clone(h.tokens)
	if p := substrate.PrincipalFrom(ctx); p != "" && !slices.Contains(out, p) {
		out = append(out, p)
	}
	return out
}

// appGrants are the grants the tokens hold the work to, each read now. An
// app's token (one naming a client or carrying scopes) caps the work at its
// grant; one with full access, or an assistant's, caps nothing beyond the
// code; a person's sign-in caps nothing, the person being held by their
// roles. An app's token revoked, or a token gone, refuses the work, and a
// read that fails is an error.
func (ds *dataset) appGrants(ctx context.Context, q dbx, tokens []string) ([]scope.Grant, error) {
	if err := ds.svc.ruleFault("grant"); err != nil && len(tokens) > 0 {
		return nil, fmt.Errorf("read the grants that hold the work: %w", err)
	}
	var out []scope.Grant
	for _, id := range tokens {
		var raw []byte
		var deleted bool
		err := q.QueryRowContext(ctx, `SELECT props, deleted_at IS NOT NULL FROM records WHERE kind = $1 AND id = $2`,
			kindToken, id).Scan(&raw, &deleted)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("%w: %w: the token this work runs for is gone", substrate.ErrForbidden, substrate.ErrNotPermitted)
		}
		if err != nil {
			return nil, fmt.Errorf("read the token this work runs for: %w", err)
		}
		var props struct {
			Scopes   []string `json:"scopes"`
			ClientID string   `json:"clientId"`
		}
		if err := json.Unmarshal(raw, &props); err != nil {
			return nil, fmt.Errorf("read the token this work runs for: %w", err)
		}
		if props.ClientID == "" && len(props.Scopes) == 0 {
			continue
		}
		if deleted {
			return nil, fmt.Errorf("%w: %w: the app's token this work runs for was revoked", substrate.ErrForbidden, substrate.ErrNotPermitted)
		}
		if len(props.Scopes) == 0 || scope.IsMCP(props.Scopes) {
			continue
		}
		g, err := scope.NewGrant(props.Scopes)
		if err != nil {
			return nil, fmt.Errorf("the grant of the token this work runs for: %w", err)
		}
		out = append(out, g)
	}
	return out, nil
}

// runReads is what the work in ctx may read beyond its code's allowlist, by
// the roles of the persons it acts for and the grants of its apps, read now:
// nil where nothing holds it.
func (ds *dataset) runReads(ctx context.Context) (func(kind string) bool, error) {
	var roles []scope.Roles
	for _, p := range append([]substrate.Actor{substrate.OnBehalfOfFrom(ctx)}, heldPersons(ctx)...) {
		if substrate.ActorTypeOf(p) != substrate.ActorTypePerson {
			continue
		}
		r, err := ds.rolesIn(ctx, ds.db, ds.personActorFor(ctx, p))
		if err != nil {
			return nil, err
		}
		if !r.Admin() {
			roles = append(roles, r)
		}
	}
	grants, err := ds.appGrants(ctx, ds.db, runTokens(ctx))
	if err != nil {
		return nil, err
	}
	if len(roles) == 0 && len(grants) == 0 {
		return nil, nil
	}
	reg := ds.registry()
	return func(kind string) bool {
		ty, err := ds.resolveType(kind)
		if err != nil {
			return false
		}
		k := reachKindIn(reg, ty)
		for _, r := range roles {
			if !r.Reaches(scope.Read, k) {
				return false
			}
		}
		for _, g := range grants {
			if !g.Reads(k) {
				return false
			}
		}
		return true
	}, nil
}

// heldReads is runReads as a refusal of the kinds a read names.
func (ds *dataset) heldReads(ctx context.Context, kinds ...string) error {
	held, err := ds.runReads(ctx)
	if err != nil || held == nil {
		return err
	}
	for _, k := range kinds {
		if !held(k) {
			return fmt.Errorf("%w: %w: this work may not read %s: the persons and the apps it works for do not read it", substrate.ErrForbidden, substrate.ErrNotPermitted, k)
		}
	}
	return nil
}

// mayContinue admits hand to continue the run of threadID (answer its ask,
// approve its call, decide its proposal, write its next message) only where
// hand could have started that run itself, by its rules now: a person whose
// roles cover everything the run's agent reads, an app whose grant talks to
// it and covers the same (decision 0214's rule, held here). An app decides a
// proposal by the verb its grant decides by (decision 0195: deciding is not
// talking), and the turn its decision starts is held to that grant. The
// person whose own turn it is continues it whatever the agent reads: the run
// is held to them already.
func (ds *dataset) mayContinue(ctx context.Context, hand substrate.Actor, threadID string, decides bool) error {
	switch substrate.ActorTypeOf(hand) {
	case substrate.ActorTypePerson, substrate.ActorTypeApp:
	default:
		return nil
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: typeThread, ID: threadID})
	if err != nil {
		return fmt.Errorf("read the run this continues: %w", err)
	}
	if row == nil {
		return nil
	}
	// The thread's holders are its turn's now: the person whose own turn it
	// is continues it. A thread from before this release names nobody.
	turn, err := ds.heldBy(ctx, typeThread, threadID)
	if err != nil && !errors.Is(err, errAskAgain) {
		return err
	}
	person := ds.personActorFor(ctx, substrate.ActorPerson)
	if substrate.ActorTypeOf(hand) == substrate.ActorTypePerson {
		person = ds.personActorFor(ctx, hand)
	}
	if owner := substrate.OnBehalfOfFrom(turn); owner != "" && owner == person && substrate.ActorTypeOf(hand) == substrate.ActorTypePerson {
		return nil
	}
	agentID := referenceID(row.Props["agent"])
	rec, err := ds.Get(ctx, kindAgent, agentID)
	if err != nil {
		return fmt.Errorf("read the agent of the run this continues: %w", err)
	}
	entries, err := reach.Callable(ctx, ds, rec)
	if err != nil {
		return fmt.Errorf("read the reach of the run this continues: %w", err)
	}
	reg := ds.registry()
	var grant *scope.Grant
	if substrate.ActorTypeOf(hand) == substrate.ActorTypeApp {
		grants, err := ds.appGrants(ctx, ds.db, []string{substrate.PrincipalFrom(ctx)})
		if err != nil {
			return err
		}
		if len(grants) > 0 {
			grant = &grants[0]
			if decides && grant.Decides(agentID) {
				return nil
			}
			if !grant.TalksTo(agentID) {
				return fmt.Errorf("%w: %w: the app may not continue the run of %s: its grant does not talk to it", substrate.ErrForbidden, substrate.ErrNotPermitted, agentID)
			}
		}
	}
	roles, err := ds.rolesIn(ctx, ds.db, person)
	if err != nil {
		return err
	}
	controls, internals := controlAndInternalKinds(reg)
	for _, e := range entries {
		if e.Write {
			continue
		}
		k := patternKind(reg, e.Pattern)
		if !roles.Covers(scope.Read, e.Pattern, k, controls) ||
			(grant != nil && !grant.ReachCovered(scope.Read, e.Pattern, k, internals...)) {
			return fmt.Errorf("%w: %w: %s may not continue the run of %s: it could not start it, which reads %s", substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(hand), agentID, e.Pattern)
		}
	}
	return nil
}

// controlAndInternalKinds are reg's control kinds and sign-in material, and
// its internal kinds, the two sets a pattern is held to by name.
func controlAndInternalKinds(reg *vocabulary.Registry) (controls, internals []string) {
	for _, k := range reg.Kinds() {
		rk := reachKindIn(reg, k)
		if rk.Control {
			controls = append(controls, k.Identity)
		}
		if rk.Internal {
			internals = append(internals, k.Identity)
		}
	}
	return controls, internals
}

// patternKind is a pattern as a rule reads it: the kind it names, as reg
// declares it, or for a glob or a kind reg does not hold, the pattern alone.
func patternKind(reg *vocabulary.Registry, pattern string) scope.Kind {
	if ty, err := reg.Resolve(pattern); err == nil && ty != nil {
		return reachKindIn(reg, ty)
	}
	return scope.Kind{Ref: pattern, Internal: true}
}

// continuation admits a write of a run's own record that continues it, by a
// person or an app, under mayContinue: an answer, an approval, a decision.
func (ds *dataset) continuation(ctx context.Context, hand substrate.Actor, kind, id string) error {
	switch kind {
	case vocabulary.KindLLMInteraction, vocabulary.KindLLMApproval, vocabulary.KindRecordPatchRequest:
	default:
		return nil
	}
	switch substrate.ActorTypeOf(hand) {
	case substrate.ActorTypePerson, substrate.ActorTypeApp:
	default:
		return nil
	}
	thread, err := ds.threadOf(ctx, kind, id)
	if err != nil || thread == "" {
		return err
	}
	return ds.mayContinue(ctx, hand, thread, kind == vocabulary.KindRecordPatchRequest)
}
