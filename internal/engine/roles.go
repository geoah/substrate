package engine

// THE ONE CHECK (decision 0276). A write is allowed when the rules of the hand
// that writes allow it and, where it acts for another, the rules of the actor
// it acts for allow it too: never more than both. Every hand's rules are read
// here, in the write's own transaction:
//
//   - a person's are the roles their actor names (scope.Roles), so a role
//     changed or taken away holds from the next request;
//   - an app's are the scopes its approval gave its token, held by the API's
//     scoped view, which every request of an app's token passes; here it
//     writes no sign-in material, and it always acts for its person;
//   - a function's and an agent's are the writes its declaration lists,
//     acting for whom the work that started it names, and for nobody when a
//     trigger started it. Where the run's intent is applied (an effect
//     decoded, the agent's write tool) the dispatcher holds every write to
//     those writes; here the run is held to the control kinds its
//     declaration names exactly, as it carries them (txn.effEmit) or as the
//     declaration says, and to no sign-in material at all;
//   - a package's own hand writes what its install ships, the triggers and
//     settings it declares among them, and never sign-in material: the
//     install is a door only an admin opens;
//   - the system's, the engine's own, are everything.
//
// A control kind (vocabulary.ControlClassOf) and sign-in material are reached
// only by naming them exactly, or by holding `admin`. The engine's own
// writes, internal ones, decide for themselves and are not held here.

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const kindRole = "substrate.reamde.dev/core/role"

// controlClassIn is the control class of ty under reg's bundles, "" for a
// kind that is no control kind.
func controlClassIn(reg *vocabulary.Registry, ty *vocabulary.Kind) vocabulary.ControlClass {
	b, ok := reg.BundleOf(ty.Package)
	return vocabulary.ControlClassOf(ty, ok && bundleInputKind(b, ty.Identity))
}

// reachKindIn is ty as a rule is held to it, under reg's bundles.
func reachKindIn(reg *vocabulary.Registry, ty *vocabulary.Kind) scope.Kind {
	k := scope.Kind{Ref: ty.Identity, Internal: ty.Purpose == vocabulary.PurposeInternal}
	for _, b := range ty.Traits {
		k.Traits = append(k.Traits, b.Identity)
	}
	k.Control = controlClassIn(reg, ty) != "" || vocabulary.SignInMaterial(ty.Identity)
	return k
}

// reachKind is ty as a rule is held to it in this transaction.
func (t *txn) reachKind(ty *vocabulary.Kind) scope.Kind { return reachKindIn(t.declarations(), ty) }

// authorizeWrite is the one check for a write of ty: the hand's own rules,
// those of the person it acts for, and for code, those of every person and
// app the work it runs is held to (run.go), each read now. The record of a
// run is held to its writing hand's transaction alone (exemptOp).
func (t *txn) authorizeWrite(ty *vocabulary.Kind) error {
	if t.internal || t.exempts(ty.Identity) {
		return nil
	}
	k := t.reachKind(ty)
	if err := t.handWrites(t.actor, k); err != nil {
		return err
	}
	behalf := t.onBehalfOfKey(t.actor)
	if behalf != "" && substrate.CanonicalActor(behalf) != substrate.CanonicalActor(t.actor) {
		if err := t.handWrites(behalf, k); err != nil {
			return err
		}
	}
	switch substrate.ActorTypeOf(t.actor) {
	case substrate.ActorTypeFunction, substrate.ActorTypeAgent:
	default:
		return nil
	}
	for _, p := range heldPersons(t.ctx) {
		if err := t.handWrites(p, k); err != nil {
			return err
		}
	}
	var q dbx = t.ds.db
	if t.tx != nil {
		q = t.tx
	}
	grants, err := t.ds.appGrants(t.ctx, q, runTokens(t.ctx))
	if err != nil {
		return err
	}
	for _, g := range grants {
		if !g.Writes(k) {
			return fmt.Errorf("%w: %w: %s may not write %s: the app it works for does not write it", substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(t.actor), k.Ref)
		}
	}
	return nil
}

// handWrites reports, as an error, whether hand's own rules write k.
func (t *txn) handWrites(hand substrate.Actor, k scope.Kind) error {
	refuse := func(why string) error {
		return fmt.Errorf("%w: %w: %s may not write %s: %s", substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(hand), k.Ref, why)
	}
	switch substrate.ActorTypeOf(hand) {
	case substrate.ActorTypeSystem:
		// A policy's decision runs as the system under the ceiling of the
		// code it decides for, and writes no sign-in material either.
		if t.effEmitSet && vocabulary.SignInMaterial(k.Ref) {
			return refuse("installed code and a policy's decision write no sign-in material")
		}
		return nil
	case substrate.ActorTypePerson:
		roles, err := t.rolesOf(t.ds.personActorFor(t.ctx, hand))
		if err != nil {
			return err
		}
		if !roles.Reaches(scope.Write, k) {
			return refuse("no rule of the roles they hold covers it")
		}
		return nil
	case substrate.ActorTypeApp:
		// An app's own scopes are held by the API's scoped view, which every
		// request of an app's token passes and which alone reads the rows
		// its agent verbs reach. No scope reaches a control kind (scope's
		// unwritable packages, and the settings an app never writes), so an
		// app's token writes none here either; an assistant's, which reaches
		// what the console does (decision 0230), writes every one but
		// sign-in material. The person it acts for is held below.
		if vocabulary.SignInMaterial(k.Ref) {
			return refuse("an app writes no sign-in material")
		}
		if scopes, scoped := substrate.ScopesFrom(t.ctx); scoped && !scope.IsMCP(scopes) && k.Control {
			return refuse("an app's token writes no control kind")
		}
		return nil
	case substrate.ActorTypeFunction, substrate.ActorTypeAgent:
		if !k.Control {
			return nil
		}
		writes := t.effEmit
		if !t.effEmitSet {
			writes = t.declaredWrites(hand)
		}
		if !declaredWritesReach(writes, k) {
			return refuse("a control kind is written only where its declaration names it, and sign-in material never")
		}
		return nil
	case substrate.ActorTypePackage:
		if vocabulary.SignInMaterial(k.Ref) {
			return refuse("a package's own hand writes no sign-in material")
		}
		return nil
	}
	return refuse("it is no hand this repository knows")
}

// declaredWritesReach reports whether a declaration's writes reach a control
// kind k: where one names it, and never sign-in material, which the loader
// refuses a declaration to name (decision 0080) and a declaration stored
// before that refusal is held to here.
func declaredWritesReach(writes []string, k scope.Kind) bool {
	return !vocabulary.SignInMaterial(k.Ref) && slices.Contains(writes, k.Ref)
}

// rolesOf are the roles person's actor names, read in the transaction and
// kept for its length.
func (t *txn) rolesOf(person substrate.Actor) (scope.Roles, error) {
	if r, ok := t.roles[person]; ok {
		return r, nil
	}
	var q dbx = t.ds.db
	if t.tx != nil {
		q = t.tx
	}
	r, err := t.ds.rolesIn(t.ctx, q, person)
	if err != nil {
		return scope.Roles{}, err
	}
	if t.roles == nil {
		t.roles = map[substrate.Actor]scope.Roles{}
	}
	t.roles[person] = r
	return r, nil
}

// rolesIn reads the roles person's actor names: none for an actor that is
// absent, deleted or disabled, and a role whose record is gone holds nothing
// but `admin`. A repository from before actors holds no role to read: its
// person could do everything, and is `admin`, as repository migration 13
// makes every person from before roles (preActorRoles).
func (ds *dataset) rolesIn(ctx context.Context, q dbx, person substrate.Actor) (scope.Roles, error) {
	if err := ds.svc.ruleFault("roles"); err != nil {
		return scope.Roles{}, fmt.Errorf("read the roles of %s: %w", handWords(person), err)
	}
	spellings := ds.names()
	ty, err := ds.resolveType(kindActor)
	if roles, before, err := preActorRoles(ty, err, spellings); err != nil || before {
		return roles, err
	}
	var raw []byte
	err = q.QueryRowContext(ctx, `SELECT props->'roles' FROM records
		WHERE kind = $1 AND id = $2 AND deleted_at IS NULL AND COALESCE(props->>'disabled', '') <> 'true'`,
		kindActor, actorID(person)).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return scope.NewRoles(nil, spellings...), nil
	}
	if err != nil {
		return scope.Roles{}, fmt.Errorf("read the roles of %s: %w", handWords(person), err)
	}
	ids := roleIDs(raw)
	if len(ids) == 0 {
		return scope.NewRoles(nil, spellings...), nil
	}
	rows, err := q.QueryContext(ctx, `SELECT id, props->'rules' FROM records
		WHERE kind = $1 AND id = ANY($2) AND deleted_at IS NULL`, kindRole, ids)
	if err != nil {
		return scope.Roles{}, fmt.Errorf("read the roles of %s: %w", handWords(person), err)
	}
	defer func() { _ = rows.Close() }()
	byID := map[string]scope.Role{}
	for rows.Next() {
		var id string
		var rules []byte
		if err := rows.Scan(&id, &rules); err != nil {
			return scope.Roles{}, err
		}
		byID[id] = scope.Role{ID: id, Rules: rulesOf(rules)}
	}
	if err := rows.Err(); err != nil {
		return scope.Roles{}, err
	}
	held := make([]scope.Role, 0, len(ids))
	for _, id := range ids {
		if role, ok := byID[id]; ok {
			held = append(held, role)
		} else if id == scope.RoleAdmin {
			// `admin` is held by being named: what it reaches is no rule of
			// its record's, so an admin keeps it where the record is not
			// written yet (a vocabulary from before roles, an upgrade still
			// withheld).
			held = append(held, scope.Role{ID: id})
		}
	}
	return scope.NewRoles(held, spellings...), nil
}

// preActorRoles answers, from the actor kind as it resolved, whether the
// repository is from before actors: its actor kind is the declared-name kind
// (no `type`, decision 0270), read as such, whose person is `admin`. An actor
// kind that does not resolve is an error, never a wider answer: a vocabulary
// that failed to load is not one from before actors.
func preActorRoles(ty *vocabulary.Kind, err error, spellings []string) (scope.Roles, bool, error) {
	if err != nil {
		return scope.Roles{}, false, fmt.Errorf("read the roles: the actor kind does not resolve: %w", err)
	}
	if ty.Props["type"] == nil {
		return scope.NewRoles([]scope.Role{{ID: scope.RoleAdmin}}, spellings...), true, nil
	}
	return scope.Roles{}, false, nil
}

// roleIDs are the role record ids a stored `roles` value names.
func roleIDs(raw []byte) []string {
	var refs []any
	if len(raw) == 0 || json.Unmarshal(raw, &refs) != nil {
		return nil
	}
	var ids []string
	for _, r := range refs {
		if id := strings.TrimPrefix(propRefID(r), kindRole+"/"); id != "" && !slices.Contains(ids, id) {
			ids = append(ids, id)
		}
	}
	return ids
}

// rulesOf reads a stored `rules` value. A rule that does not read is
// dropped: rules only add, so a damaged one never reads as wider.
func rulesOf(raw []byte) []scope.Rule {
	var stored []struct {
		Verbs []string `json:"verbs"`
		Kinds []string `json:"kinds"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &stored) != nil {
		return nil
	}
	out := make([]scope.Rule, 0, len(stored))
	for _, s := range stored {
		rule := scope.Rule{Kinds: s.Kinds}
		for _, v := range s.Verbs {
			if v == string(scope.Read) || v == string(scope.Write) {
				rule.Verbs = append(rule.Verbs, scope.Verb(v))
			}
		}
		out = append(out, rule)
	}
	return out
}

// roleRefs is the stored `roles` value naming ids.
func roleRefs(ids ...string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = map[string]any{"ref": vocabulary.RecordPath(kindRole, id)}
	}
	return out
}

// shippedRoleProps are a shipped role's stored properties.
func shippedRoleProps(r scope.ShippedRole) map[string]any {
	rules := make([]any, len(r.Rules))
	for i, rule := range r.Rules {
		verbs := make([]any, len(rule.Verbs))
		for j, v := range rule.Verbs {
			verbs[j] = string(v)
		}
		kinds := make([]any, len(rule.Kinds))
		for j, k := range rule.Kinds {
			kinds[j] = k
		}
		rules[i] = map[string]any{"verbs": verbs, "kinds": kinds}
	}
	return map[string]any{"name": r.Name, "description": r.Description, "rules": rules}
}

// ensureShippedRoles writes each shipped role the repository lacks, or holds
// in another shape, as the engine's own write: at 1.0 no other hand writes a
// role. A repository whose vocabulary does not declare roles yet writes
// none; the open after its upgrade does.
func (t *txn) ensureShippedRoles() error {
	if _, err := t.resolveType(kindRole); err != nil {
		return nil
	}
	wasScoped, wasInternal := t.scoped, t.internal
	t.scoped, t.internal = false, true
	defer func() { t.scoped, t.internal = wasScoped, wasInternal }()
	return t.asActor(substrate.ActorSystem, func() error {
		for _, r := range scope.ShippedRoles {
			want := shippedRoleProps(r)
			row, err := t.loadRow(eref{Kind: kindRole, ID: r.ID}, false)
			if err != nil {
				return err
			}
			if row != nil && jsonEqual(row.Props, want) {
				continue
			}
			if _, err := t.put(substrate.PutInput{Kind: kindRole, ID: r.ID, Properties: want}); err != nil {
				return err
			}
		}
		return nil
	})
}

// ensureShippedRoles is the open's step: the three roles, after the upgrade
// that declares their kind.
func (ds *dataset) ensureShippedRoles(ctx context.Context) error {
	if ds.svc.readOnly {
		return nil
	}
	for _, r := range scope.ShippedRoles {
		var raw []byte
		err := ds.db.QueryRowContext(ctx, `SELECT props FROM records WHERE kind = $1 AND id = $2 AND deleted_at IS NULL`, kindRole, r.ID).Scan(&raw)
		if err == nil {
			var props map[string]any
			if json.Unmarshal(raw, &props) == nil && jsonEqual(props, shippedRoleProps(r)) {
				continue
			}
		} else if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error { return t.ensureShippedRoles() })
	}
	return nil
}

// ownToken reports whether a token is the sign-in of the person this write
// is, whose own it is to revoke.
func (t *txn) ownToken(row *erow) bool {
	if substrate.ActorTypeOf(t.actor) != substrate.ActorTypePerson {
		return false
	}
	identity := substrate.IdentityFrom(t.ctx)
	named, _ := row.Props["identity"].(string)
	return identity != "" && named == identity
}

// requestMayWrite is the one check outside a write, for a door a person's
// request opens and nothing else: the request's hand, a person or an app,
// and the person an app acts for, may write ty. Installed code opens no such
// door.
func (ds *dataset) requestMayWrite(ctx context.Context, actor substrate.Actor, ty *vocabulary.Kind) error {
	k := reachKindIn(ds.registry(), ty)
	switch substrate.ActorTypeOf(actor) {
	case substrate.ActorTypePerson, substrate.ActorTypeApp:
	default:
		return fmt.Errorf("%w: %w: %s may not write %s: a person's door", substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(actor), ty.Identity)
	}
	person := ds.personActorFor(ctx, substrate.ActorPerson)
	if substrate.ActorTypeOf(actor) == substrate.ActorTypePerson {
		person = ds.personActorFor(ctx, actor)
	}
	roles, err := ds.rolesIn(ctx, ds.db, person)
	if err != nil {
		return err
	}
	if !roles.Reaches(scope.Write, k) {
		return fmt.Errorf("%w: %w: %s may not write %s: no rule of the roles they hold covers it", substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(person), ty.Identity)
	}
	return nil
}

// declaredWrites are the writes the declaration of a function's or an
// agent's hand lists, for a write its run did not carry them on: none for a
// hand whose declaration is gone.
func (t *txn) declaredWrites(hand substrate.Actor) []string {
	key := string(substrate.CanonicalActor(hand))
	reg := t.declarations()
	for _, prefix := range []string{substrate.FunctionActorPrefix, substrate.AgentActorPrefix} {
		rest, ok := strings.CutPrefix(key, prefix)
		if !ok {
			continue
		}
		parts := strings.SplitN(rest, ":", 3)
		if len(parts) != 3 {
			return nil
		}
		ref := parts[0] + "/" + parts[1] + "/" + parts[2]
		if prefix == substrate.FunctionActorPrefix {
			if fn, err := reg.ResolveFunction(ref); err == nil {
				return fn.Caps.Emit
			}
			return nil
		}
		if ag, err := reg.ResolveAgent(ref); err == nil {
			return ag.Emit
		}
	}
	return nil
}

// PersonRoles are the roles of the person the request acts for.
func (ds *dataset) PersonRoles(ctx context.Context) ([]substrate.RoleRules, error) {
	roles, err := ds.rolesIn(ctx, ds.db, ds.personActorFor(ctx, substrate.ActorPerson))
	if err != nil {
		return nil, err
	}
	held := roles.Held()
	out := make([]substrate.RoleRules, len(held))
	for i, r := range held {
		out[i] = substrate.RoleRules{ID: r.ID, Rules: make([]substrate.RoleRule, len(r.Rules))}
		for j, rule := range r.Rules {
			verbs := make([]string, len(rule.Verbs))
			for k, v := range rule.Verbs {
				verbs[k] = string(v)
			}
			out[i].Rules[j] = substrate.RoleRule{Verbs: verbs, Kinds: slices.Clone(rule.Kinds)}
		}
	}
	return out, nil
}

// behalfMayWrite is the half of the one check for a door that writes in a
// transaction of its own on a run's behalf (the vocabulary door) that is not
// the run's code's: every person and every app the run works for (run.go)
// may write ty. Work that acts for nobody, a trigger's, passes.
func (ds *dataset) behalfMayWrite(ctx context.Context, ty *vocabulary.Kind) error {
	k := reachKindIn(ds.registry(), ty)
	for _, p := range append([]substrate.Actor{substrate.OnBehalfOfFrom(ctx)}, heldPersons(ctx)...) {
		if substrate.ActorTypeOf(p) != substrate.ActorTypePerson {
			continue
		}
		person := ds.personActorFor(ctx, p)
		roles, err := ds.rolesIn(ctx, ds.db, person)
		if err != nil {
			return err
		}
		if !roles.Reaches(scope.Write, k) {
			return fmt.Errorf("%w: %w: %s may not write %s: no rule of the roles they hold covers it", substrate.ErrForbidden, substrate.ErrNotPermitted, handWords(person), ty.Identity)
		}
	}
	grants, err := ds.appGrants(ctx, ds.db, runTokens(ctx))
	if err != nil {
		return err
	}
	for _, g := range grants {
		if !g.Writes(k) {
			return fmt.Errorf("%w: %w: this run may not write %s: the app it works for does not write it", substrate.ErrForbidden, substrate.ErrNotPermitted, ty.Identity)
		}
	}
	return nil
}

// mayWrite is the one check for a door that writes in a transaction of its
// own, the declaration door: ty held as a write by actor would be, with the
// run's ceiling where a run carries one, before the door's transaction.
func (ds *dataset) mayWrite(ctx context.Context, actor substrate.Actor, ty *vocabulary.Kind, ceiling *effectCeiling) error {
	_, scoped := substrate.ScopesFrom(ctx)
	t := &txn{ds: ds, ctx: ctx, actor: actor, onBehalfOf: substrate.OnBehalfOfFrom(ctx), scoped: scoped}
	if ceiling != nil {
		t.effEmit, t.effEmitSet = ceiling.emit, true
	}
	return t.authorizeWrite(ty)
}
