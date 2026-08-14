package engine

// The bundle lifecycle (substrate-primitives §4, ticket 034): a bundle
// installs as one atomic schema apply of its whole closure (schemawrite.go
// replaces the owned authority whenever a batch carries a bundle document). Three
// verbs act on it afterwards:
//
//   - `disable` — execution stops, fully reversible: its triggers stop
//     delivering (cursors stand still), its functions and agents refuse
//     invocation, and its config/account records are frozen. The SCHEMA and
//     the DATA both stay — its types keep appearing in `types` and their rows
//     stay readable and writable. Recorded as `disabled: true` on the bundle
//     row; `enable` clears it and the backlog delivers.
//   - `uninstall` — the bundle goes: the owned authority's SCHEMA rows (the
//     bundle row, its record types, functions, agents, actors, traits,
//     property types and mappings) are torn down through the SAME schema-write
//     admission an apply uses (a whole-authority replace with an empty incoming
//     set), and the wiring — every trigger referencing the authority's callables —
//     goes with them in the same transaction. The registry rebuild then drops
//     the authority: its types stop appearing in `types`, its callables stop
//     running, and a read of one 404s. Governed by refuse-with-instances:
//     uninstalling while live DATA instances of the authority's types exist is a
//     guard error carrying the count — purge first. Not reversible; re-applying
//     the closure is a fresh install.
//   - `purge` — the explicit destructive verb, refused while the bundle is
//     live (disable it first). It tombstones every live data record of the
//     owned authority's types through the ordinary soft-delete path, so finalizers
//     (the OAuth facility's token revocation among them) and the GC sweep run
//     as they do for any other delete. Purge never touches schema rows — it
//     clears the DATA so a following uninstall passes the refuse-with-instances
//     guard. The destructive teardown order is: disable, purge, uninstall.
//
// The former reversible-uninstall marker is retired: uninstall removes the
// schema now, so there is no bundle row left to carry an `uninstalled` flag.
// `purging` still marks a purge in flight so `enable` refuses mid-teardown.

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The bundle row's runtime properties, owned by the lifecycle verbs.
const (
	propBundleDisabled = "disabled"
	// propBundlePurging marks a purge in flight (or interrupted): enable
	// refuses it, so a bundle can never come live in the middle of its own
	// data teardown — a completed purge clears it.
	propBundlePurging = "purging"
)

// bundleState is one bundle's runtime lifecycle, read off its record row.
// Uninstall is no longer a state — it tears the bundle row down —
// so a bundle is either live, disabled, or mid-purge.
type bundleState struct {
	Disabled bool
	Purging  bool
}

// blocked reports whether the state stops execution. Purge requires it — a
// bundle must be disabled before its data can be torn down.
func (s bundleState) blocked() bool { return s.Disabled }

// bundleStates reads every bundle row's lifecycle in one query, keyed by the
// owned authority (= the row id). Authorities without a row read as the zero state.
func (ds *dataset) bundleStates(ctx context.Context) (map[string]bundleState, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT COALESCE(props->>'authority', ''), COALESCE(props->>$2, ''), COALESCE(props->>$3, '')
		FROM records WHERE kind = $1 AND deleted_at IS NULL`,
		kindBundle, propBundleDisabled, propBundlePurging)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]bundleState{}
	for rows.Next() {
		var id, disabled, purging string
		if err := rows.Scan(&id, &disabled, &purging); err != nil {
			return nil, err
		}
		out[id] = bundleState{Disabled: disabled == "true", Purging: purging == "true"}
	}
	return out, rows.Err()
}

// bundleStateOf reads one bundle's lifecycle outside a transaction.
func (ds *dataset) bundleStateOf(ctx context.Context, authority string) (bundleState, error) {
	var disabled, purging string
	err := ds.db.QueryRowContext(ctx, `
		SELECT COALESCE(props->>$3, ''), COALESCE(props->>$4, '')
		FROM records WHERE kind = $2 AND props->>'authority' = $1 AND deleted_at IS NULL`,
		authority, kindBundle, propBundleDisabled, propBundlePurging).Scan(&disabled, &purging)
	if errors.Is(err, sql.ErrNoRows) {
		return bundleState{}, nil
	}
	if err != nil {
		return bundleState{}, err
	}
	return bundleState{Disabled: disabled == "true", Purging: purging == "true"}, nil
}

// bundleStateTx is bundleStateOf inside the caller's transaction.
func (t *txn) bundleStateTx(authority string) (bundleState, error) {
	var disabled, purging string
	err := t.row(`
		SELECT COALESCE(props->>$3, ''), COALESCE(props->>$4, '')
		FROM records WHERE kind = $2 AND props->>'authority' = $1 AND deleted_at IS NULL`,
		authority, kindBundle, propBundleDisabled, propBundlePurging).Scan(&disabled, &purging)
	if errors.Is(err, sql.ErrNoRows) {
		return bundleState{}, nil
	}
	if err != nil {
		return bundleState{}, err
	}
	return bundleState{Disabled: disabled == "true", Purging: purging == "true"}, nil
}

// --- the lifecycle fence -----------------------------------------------------------

// lifecycleLeaseKey marks a context whose invocation tree already holds the
// shared lifecycle fence. The root's admission stamps it; nested
// host Calls, function tools and sub-agents inherit it through the context
// and must NOT re-acquire — a second reader queued behind a pending writer
// deadlocks a recursive or cross-bundle call.
type lifecycleLeaseKey struct{}

// underLifecycleLease reports whether this call tree already holds the shared
// lifecycle fence.
func underLifecycleLease(ctx context.Context) bool {
	return ctx.Value(lifecycleLeaseKey{}) != nil
}

// admitCallable is invocation admission under the ONE dataset-wide lifecycle
// fence, generic to FUNCTIONS and AGENTS (caller passes the
// callable's authority and identity). At the ROOT of an invocation tree it takes
// the fence's SHARED side ONCE, re-checks the callable's bundle lifecycle
// UNDER it, and returns a context carrying the lease plus the release the
// caller holds until its LAST effect / message / thread-settlement /
// cursor-or-fire-state write commits. A NESTED callable — a host Call, an
// agent's function tool, a sub-agent — is detected by the inherited lease: it
// re-checks the callee's lifecycle under the already-held fence (where the
// state cannot change) but does NOT lock again and returns a no-op release.
// Either way the callee's bundle lifecycle is verified while the fence is
// held. A callable outside any bundle admits with a no-op release.
func (ds *dataset) admitCallable(ctx context.Context, authority, identity string) (context.Context, func(), error) {
	if underLifecycleLease(ctx) {
		// Nested: the root already holds the fence. Re-check under it — the
		// callee's bundle may have been disabled BEFORE the root admitted —
		// but never re-acquire.
		if err := ds.callableGroupBlocked(ctx, authority, identity); err != nil {
			return ctx, nil, err
		}
		return ctx, func() {}, nil
	}
	ds.lifecycleFence.RLock()
	if err := ds.callableGroupBlocked(ctx, authority, identity); err != nil {
		ds.lifecycleFence.RUnlock()
		return ctx, nil, err
	}
	leased := context.WithValue(ctx, lifecycleLeaseKey{}, struct{}{})
	return leased, sync.OnceFunc(ds.lifecycleFence.RUnlock), nil
}

// --- write-path enforcement -------------------------------------------------------

// checkBundleWrite is the put/patch admission for records of bundle-owned
// types: a disabled bundle's inputs and accounts are frozen. No cardinality
// is enforced on any kind — records of an input's kind are ordinary and
// unbounded, and resolution (inputs.go) picks one. (Uninstall no longer
// freezes writes: it tears the types down, so an uninstalled authority's
// types stop resolving entirely.) Internal writes (projection, lifecycle
// verbs, the OAuth facility) bypass.
func (t *txn) checkBundleWrite(ty *vocabulary.Kind, id string, create bool) error {
	if t.internal {
		return nil
	}
	b, ok := t.ds.registry().BundleOf(ty.Authority)
	if !ok {
		return nil
	}
	st, err := t.bundleStateTx(b.Authority)
	if err != nil {
		return err
	}
	if st.Disabled && (bundleInputKind(b, ty.Identity) || ty.Implements(vocabulary.TraitAccountConfigCore)) {
		return fmt.Errorf("%w: bundle %s is disabled — its configuration and accounts are frozen",
			substrate.ErrGuard, b.Authority)
	}
	return nil
}

// bundleInputKind reports whether a kind is named by any of the bundle's
// inputs — the kinds whose records configure it.
func bundleInputKind(b *vocabulary.Bundle, identity string) bool {
	for _, name := range b.InputOrder {
		if b.Inputs[name].Kind == identity {
			return true
		}
	}
	return false
}

// checkBundleDelete is the delete-side twin: a disabled bundle's config and
// accounts refuse deletion too (purge is the internal path).
func (t *txn) checkBundleDelete(ty *vocabulary.Kind) error {
	if t.internal {
		return nil
	}
	b, ok := t.ds.registry().BundleOf(ty.Authority)
	if !ok {
		return nil
	}
	st, err := t.bundleStateTx(b.Authority)
	if err != nil {
		return err
	}
	if st.Disabled && (bundleInputKind(b, ty.Identity) || ty.Implements(vocabulary.TraitAccountConfigCore)) {
		return fmt.Errorf("%w: bundle %s is disabled — its configuration and accounts are frozen",
			substrate.ErrGuard, b.Authority)
	}
	return nil
}

// callableGroupBlocked refuses invoking a callable — function OR agent —
// whose bundle is disabled. Every invocation path checks it, and admitCallable
// re-checks it under the held fence, so a disabled bundle's callable refuses on
// every entry (dispatch, wake / run / retry, the call/chat APIs, host Call,
// function tools, sub-agents). An uninstalled bundle's callable is not blocked
// here — it no longer resolves at all, so the invocation 404s before this.
func (ds *dataset) callableGroupBlocked(ctx context.Context, authority, identity string) error {
	b, ok := ds.registry().BundleOf(authority)
	if !ok {
		return nil
	}
	st, err := ds.bundleStateOf(ctx, b.Authority)
	if err != nil {
		return err
	}
	if st.Disabled {
		return fmt.Errorf("%w: bundle %s is disabled — %s refuses invocation", substrate.ErrGuard, b.Authority, identity)
	}
	return nil
}

// blockBundledCallable un-resolves a trigger's callable when its bundle is
// disabled: the dispatcher then skips the trigger loudly and its cursor stands
// still — the reversible pause disable promises. (An uninstalled bundle's
// callables are gone from the registry, so resolveCallable already left the
// trigger unresolved.) states is the bundleStates map, so a dispatcher pass
// pays one query for all triggers.
func (ds *dataset) blockBundledCallable(t *trigger, states map[string]bundleState) {
	var authority string
	switch {
	case t.Callable != nil:
		authority = t.Callable.Authority
	case t.Agent != nil:
		authority = t.Agent.Authority
	default:
		return
	}
	if _, ok := ds.registry().BundleOf(authority); !ok {
		return
	}
	if states[authority].blocked() {
		t.Callable, t.Agent = nil, nil
	}
}

// --- upgrade admission -------------------------------------------------------------

// droppedCallable is one trigger-callable a candidate registry stops
// declaring: kind AND identity, because functions and agents are BOTH
// callables now and an upgrade that only guarded functions could strand a
// bundled agent's live triggers.
type droppedCallable struct {
	kind     string // callableKindFunction | callableKindAgent
	identity string
}

// droppedBundleCallables lists the callables — functions and agents — a
// candidate registry stops declaring, for touched authorities that are (or were)
// bundle-owned: the refuse-breakage half of the atomic upgrade.
func droppedBundleCallables(current, candidate *vocabulary.Registry, touched map[string]bool) []droppedCallable {
	var out []droppedCallable
	for aname := range touched {
		cur, _ := current.AuthorityByName(aname)
		if cur == nil || cur.Bundle == nil {
			continue
		}
		cand, _ := candidate.AuthorityByName(aname)
		for _, fname := range cur.FunctionOrder {
			if cand == nil || cand.Functions[fname] == nil {
				out = append(out, droppedCallable{callableKindFunction, cur.Functions[fname].Identity()})
			}
		}
		for _, aname := range cur.AgentOrder {
			if cand == nil || cand.Agents[aname] == nil {
				out = append(out, droppedCallable{callableKindAgent, cur.Agents[aname].Identity()})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].kind != out[j].kind {
			return out[i].kind < out[j].kind
		}
		return out[i].identity < out[j].identity
	})
	return out
}

// droppedCallableGuards names, per dropped bundle callable, every live
// trigger still referencing it — an upgrade that would strand delivery fails
// admission with the full list. The kind matches on the callable reference's
// `type`: a reference always carries an explicit type, so the
// former no-kind-means-function default is gone.
func droppedCallableGuards(q sqlReader, dropped []droppedCallable) ([]string, error) {
	var out []string
	for _, d := range dropped {
		rows, err := q.query(`
			SELECT id FROM records
			WHERE kind = $1 AND deleted_at IS NULL
			  AND props->'callable'->>'id' = $2
			  AND props->'callable'->>'kind' = $3
			ORDER BY id`, typeTrigger, d.identity, vocabulary.CoreKind(d.kind))
		if err != nil {
			return nil, err
		}
		var refs []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return nil, err
			}
			refs = append(refs, id)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return nil, err
		}
		_ = rows.Close()
		if len(refs) > 0 {
			out = append(out, fmt.Sprintf("%s %s is referenced by live trigger(s) %v — remove or rewire them first",
				d.kind, d.identity, refs))
		}
	}
	return out, nil
}

// registryDepKey is the registry-dependency advisory-lock key (wave-3 review
// #11): trigger create/rewire admission holds it SHARED from callable
// validation through commit, a schema batch holds it EXCLUSIVE across its
// dropped-reference query — so a trigger can never be created "across" the
// upgrade breakage check. The repository is prefixed by lockKey, not here.
func registryDepKey(*dataset) string { return "registrydep" }

// checkTriggerCallableRow re-verifies a trigger's callable against the
// COMMITTED schema rows, inside the write transaction and under the shared
// registry-dependency lock: the in-memory registry pointer publishes after a
// schema batch commits, so a trigger admission that waited out an upgrade at
// the lock could still validate against the stale pointer — the callable's
// record row is transactionally exact.
func (t *txn) checkTriggerCallableRow(tr *trigger) error {
	var ident, rowType, kind string
	switch tr.CallableKind {
	case callableKindAgent:
		a, err := t.ds.registry().ResolveAgent(tr.CallableID)
		if err != nil {
			return fmt.Errorf("%w: trigger callable: %w", substrate.ErrValidation, err)
		}
		ident, rowType, kind = a.Identity(), kindAgent, callableKindAgent
	default:
		f, err := t.ds.registry().ResolveFunction(tr.CallableID)
		if err != nil {
			return fmt.Errorf("%w: trigger callable: %w", substrate.ErrValidation, err)
		}
		ident, rowType, kind = f.Identity(), kindFunction, callableKindFunction
	}
	var one int
	err := t.row(`SELECT 1 FROM records WHERE id = $1 AND kind = $2 AND deleted_at IS NULL`,
		ident, rowType).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: trigger callable: %s %s is not (or no longer) installed",
			substrate.ErrValidation, kind, ident)
	}
	return err
}

// --- the lifecycle verbs ------------------------------------------------------------

// bundleByID resolves a bundle by its record id ("<authority>/<name>") or by its
// owned authority — the two spellings a caller reasonably has in hand.
func (ds *dataset) bundleByID(id string) (*vocabulary.Bundle, error) {
	if b, ok := ds.registry().BundleOf(id); ok {
		return b, nil
	}
	for _, b := range ds.registry().Bundles() {
		if b.Identity() == id {
			return b, nil
		}
	}
	return nil, fmt.Errorf("%w: bundle %s", substrate.ErrNotFound, id)
}

// DisableBundle stops the bundle's execution, fully reversibly: trigger
// cursors stand still, functions refuse invocation, config and accounts
// freeze. Data stays readable. It takes the exclusive side of the lifecycle
// fence, so every ADMITTED invocation's effects have committed by the time
// this returns — nothing runs "a little longer" after a disable.
func (ds *dataset) DisableBundle(ctx context.Context, id string) error {
	b, err := ds.bundleByID(id)
	if err != nil {
		return err
	}
	ds.lifecycleFence.Lock()
	defer ds.lifecycleFence.Unlock()
	_, err = ds.patchInternal(ctx, substrate.ActorSystem, kindBundle, b.Identity(), substrate.PatchInput{
		Properties: map[string]any{propBundleDisabled: true},
	})
	return err
}

// EnableBundle reverses a disable; backlogged triggers resume from their
// standing cursors. It refuses while a purge is running (or stands
// interrupted): a bundle must not come live in the middle of its own data
// teardown.
func (ds *dataset) EnableBundle(ctx context.Context, id string) error {
	b, err := ds.bundleByID(id)
	if err != nil {
		return err
	}
	ds.lifecycleFence.Lock()
	defer ds.lifecycleFence.Unlock()
	st, err := ds.bundleStateOf(ctx, b.Authority)
	if err != nil {
		return err
	}
	if st.Purging {
		return fmt.Errorf("%w: bundle %s is purging — a purge is running or was interrupted; run purge to completion before enabling",
			substrate.ErrGuard, b.Authority)
	}
	_, err = ds.patchInternal(ctx, substrate.ActorSystem, kindBundle, b.Identity(), substrate.PatchInput{
		Properties: map[string]any{propBundleDisabled: nil},
	})
	return err
}

// BundleAuthority resolves a bundle's owned authority from its record id or authority, for
// the API's bundle-lifecycle scope gate (codex regress #1): the minimal
// authorization needs the authority before it runs the verb. It resolves a
// quarantined bundle too (the store fallback), so an uninstall of one is gated
// consistently.
func (ds *dataset) BundleAuthority(ctx context.Context, id string) (string, error) {
	return ds.bundleGroupOf(ctx, id)
}

// bundleGroupOf resolves the owned authority of a bundle addressed by its record
// id or its authority — from the live registry first, then from the stored rows.
// The store fallback is what makes a QUARANTINED bundle (issue 010: its authority
// is absent from the live registry) still uninstallable.
func (ds *dataset) bundleGroupOf(ctx context.Context, id string) (string, error) {
	if b, err := ds.bundleByID(id); err == nil {
		return b.Authority, nil
	}
	var authority string
	err := ds.db.QueryRowContext(ctx, `
		SELECT COALESCE(props->>'authority', '') FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND (id = $2 OR props->>'authority' = $2)
		LIMIT 1`, kindBundle, id).Scan(&authority)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && authority == "") {
		return "", fmt.Errorf("%w: bundle %s", substrate.ErrNotFound, id)
	}
	if err != nil {
		return "", err
	}
	return authority, nil
}

// UninstallBundle tears the bundle down: the owned authority's SCHEMA rows (the
// bundle row, its types, functions, agents, actors, traits, property types and
// mappings) go through the SAME schema-write admission an apply uses — a
// whole-authority replace with an empty incoming set — and the delivery wiring
// (every trigger referencing the authority's callables) goes with them in the same
// transaction, BEFORE the dropped-callable guard, so a full teardown never
// refuses on its own triggers. The registry rebuild then drops the authority: its
// types leave `types`, its callables stop running, and a read of one 404s.
//
// Governed by refuse-with-instances: the admission's dropped-type guard counts
// live DATA instances of the authority's types and refuses the uninstall with the
// count — purge first (disable, purge, uninstall is the destructive order). A
// QUARANTINED authority (absent from the live registry) is still uninstallable: it
// has no dropped types to count, so its rows tear down straight away.
//
// The exclusive fence drains admitted invocations FIRST, so the runner
// reconcile (inside the schema apply) never kills a process an admitted
// invocation is inside.
func (ds *dataset) UninstallBundle(ctx context.Context, id string) error {
	authority, err := ds.bundleGroupOf(ctx, id)
	if err != nil {
		return err
	}
	ds.lifecycleFence.Lock()
	defer ds.lifecycleFence.Unlock()
	callables, err := ds.groupCallableIdentities(ctx, authority)
	if err != nil {
		return err
	}
	_, err = ds.applyVocabularyBatch(ctx, substrate.ActorSystem, vocabularyBatch{
		replaceAuthorities: []string{authority},
		beforeGuards: func(t *txn) error {
			return t.tearDownCallableTriggers(callables)
		},
	})
	return err
}

// groupCallableIdentities lists the identities of an authority's functions and
// agents from the stored schema rows — the callables whose triggers the
// uninstall tears down. Reading from rows (not the registry) covers a
// quarantined authority too, whose callables never registered.
func (ds *dataset) groupCallableIdentities(ctx context.Context, authority string) (map[string]bool, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id FROM records
		WHERE kind IN ($1, $2) AND deleted_at IS NULL AND props->>'authority' = $3`,
		kindFunction, kindAgent, authority)
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

// tearDownCallableTriggers tombstones every live trigger whose callable is one
// of the given identities, inside the uninstall's admission transaction and
// before its guards. softDelete drops each trigger's cursor, parked failures
// and fire state with it (write.go). A guard that then refuses the batch rolls
// these deletions back too, so the wiring outlives a refused uninstall.
func (t *txn) tearDownCallableTriggers(callables map[string]bool) error {
	if len(callables) == 0 {
		return nil
	}
	rows, err := t.query(`
		SELECT id, props->'callable'->>'id' FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND props->'callable'->>'id' IS NOT NULL
		ORDER BY id`, typeTrigger)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id, callableID string
		if err := rows.Scan(&id, &callableID); err != nil {
			_ = rows.Close()
			return err
		}
		if callables[callableID] {
			ids = append(ids, id)
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()
	for _, id := range ids {
		if _, err := t.softDelete(eref{Kind: typeTrigger, ID: id}); err != nil {
			return err
		}
	}
	return nil
}

// PurgeBundle tombstones every live data record of the bundle's owned types
// through the ordinary soft-delete path — finalizers hold what needs teardown
// (the OAuth facility revokes and releases), the GC sweep collects the rest.
// It requires the bundle disabled or uninstalled first: purge is an explicit,
// separately confirmed second verb, never part of normal operation.
//
// Ordering is deliberate: the CONNECTED ACCOUNTS tombstone
// first and their OAuth finalizers run to completion WHILE the bundle's
// configuration record is still live — revocation reads the declared
// revocation endpoint off that config, so tombstoning the config first would
// make the provider grants unrevokable. Only then does the rest of the
// authority's data (the config among it) go.
//
// The whole run holds the exclusive lifecycle fence and the `purging` marker:
// admitted work drains before deletion starts, nothing admits during it, and
// enable refuses until a purge COMPLETES — an interrupted purge leaves the
// marker standing, and re-running purge is how it clears.
func (ds *dataset) PurgeBundle(ctx context.Context, id string) (int, error) {
	b, err := ds.bundleByID(id)
	if err != nil {
		return 0, err
	}
	ds.lifecycleFence.Lock()
	defer ds.lifecycleFence.Unlock()
	st, err := ds.bundleStateOf(ctx, b.Authority)
	if err != nil {
		return 0, err
	}
	if !st.blocked() {
		return 0, fmt.Errorf("%w: bundle %s is live — disable or uninstall it before purging its data",
			substrate.ErrGuard, b.Authority)
	}
	g, ok := ds.registry().AuthorityByName(b.Authority)
	if !ok {
		return 0, fmt.Errorf("%w: bundle authority %s", substrate.ErrNotFound, b.Authority)
	}
	if _, err := ds.patchInternal(ctx, substrate.ActorSystem, kindBundle, b.Identity(), substrate.PatchInput{
		Properties: map[string]any{propBundlePurging: true},
	}); err != nil {
		return 0, err
	}
	// Accounts first, everything else after; the OAuth CLIENT input's kind is
	// deferred to the very end of the second pass as belt and braces — the
	// accounts' revocation below runs against the still-live client.
	clientKind := ""
	if b.OAuth2 != nil {
		if ci, ok := b.Inputs[b.OAuth2.ClientInput]; ok {
			clientKind = ci.Kind
		}
	}
	var accountTypes, otherTypes []string
	seenClient := false
	for _, tn := range g.KindOrder {
		t := g.Kinds[tn]
		switch {
		case t.Identity == clientKind:
			seenClient = true
		case t.Implements(vocabulary.TraitAccountConfigCore):
			accountTypes = append(accountTypes, t.Identity)
		default:
			otherTypes = append(otherTypes, t.Identity)
		}
	}
	if seenClient {
		otherTypes = append(otherTypes, clientKind)
	}
	purged, err := ds.purgeTypes(ctx, accountTypes)
	if err != nil {
		return purged, err
	}
	// The accounts' OAuth teardown — revoke against the still-live config,
	// drop credentials, release — runs NOW, deterministically, not whenever
	// the background pass gets there.
	if _, err := ds.ProcessOAuthFinalizers(ctx); err != nil {
		return purged, err
	}
	n, err := ds.purgeTypes(ctx, otherTypes)
	purged += n
	if err != nil {
		return purged, err
	}
	// Completion clears the marker — detached from the caller's context, so
	// a cancellation racing the last batch cannot leave a finished purge
	// looking interrupted.
	if _, err := ds.patchInternal(context.WithoutCancel(ctx), substrate.ActorSystem, kindBundle, b.Identity(), substrate.PatchInput{
		Properties: map[string]any{propBundlePurging: nil},
	}); err != nil {
		return purged, err
	}
	return purged, nil
}

// purgeTypes tombstones every live record of the given types, batched.
func (ds *dataset) purgeTypes(ctx context.Context, idents []string) (int, error) {
	purged := 0
	for _, ident := range idents {
		for {
			ids, err := ds.liveIDsOf(ctx, ident, gcBatch)
			if err != nil {
				return purged, err
			}
			if len(ids) == 0 {
				break
			}
			err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
				for _, eid := range ids {
					if _, err := t.softDelete(eref{Kind: ident, ID: eid}); err != nil {
						return err
					}
				}
				return nil
			})
			if err != nil {
				return purged, err
			}
			purged += len(ids)
		}
	}
	return purged, nil
}

func (ds *dataset) liveIDsOf(ctx context.Context, typeIdent string, limit int) ([]string, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT id FROM records WHERE kind = $1 AND deleted_at IS NULL ORDER BY id LIMIT $2`,
		typeIdent, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// --- status ---------------------------------------------------------------------

// BundleStatuses computes every installed bundle's runtime state: lifecycle,
// input resolution and setup steps, and what it ships. Stored nowhere. A
// bundle QUARANTINED at repository-open is not in the live registry,
// so it is listed separately from its stored rows — the console needs to show
// it needs a re-install.
func (ds *dataset) BundleStatuses(ctx context.Context) ([]substrate.BundleStatus, error) {
	bundles := ds.registry().Bundles()
	out := make([]substrate.BundleStatus, 0, len(bundles))
	for _, b := range bundles {
		st, err := ds.bundleStatus(ctx, b)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	quarantined, err := ds.quarantinedBundleStatuses(ctx)
	if err != nil {
		return nil, err
	}
	return append(out, quarantined...), nil
}

// quarantinedBundleStatuses reads the bundles whose stored closure was
// quarantined at repository-open straight from the authority row and
// bundle rows — a quarantined authority is absent from the live registry, so its
// status comes from the store.
func (ds *dataset) quarantinedBundleStatuses(ctx context.Context) ([]substrate.BundleStatus, error) {
	rows, err := ds.db.QueryContext(ctx, `
		SELECT b.id, g.id, COALESCE(g.props->>$3, '')
		FROM records g
		JOIN records b ON b.kind = $2 AND b.deleted_at IS NULL AND b.props->>'authority' = g.id
		WHERE g.kind = $1 AND g.deleted_at IS NULL AND (g.props ? $4) AND g.props->>$4 = 'true'
		ORDER BY g.id`,
		kindAuthority, kindBundle, propAuthorityQuarantineReason, propAuthorityQuarantined)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []substrate.BundleStatus
	for rows.Next() {
		var id, authority, reason string
		if err := rows.Scan(&id, &authority, &reason); err != nil {
			return nil, err
		}
		// The bundle's NAME is its id's last segment (vocabulary.BundleName is
		// the same answer from the authority): a declaration row carries no
		// id-derived name property to read it off.
		name := vocabulary.KindName(id)
		out = append(out, substrate.BundleStatus{
			ID: id, Name: name, Authority: authority,
			Installed: false, Enabled: false,
			Quarantined: true, QuarantineReason: reason,
		})
	}
	return out, rows.Err()
}

// BundleStatus computes one bundle's runtime state.
func (ds *dataset) BundleStatus(ctx context.Context, id string) (substrate.BundleStatus, error) {
	b, err := ds.bundleByID(id)
	if err != nil {
		return substrate.BundleStatus{}, err
	}
	return ds.bundleStatus(ctx, b)
}

func (ds *dataset) bundleStatus(ctx context.Context, b *vocabulary.Bundle) (substrate.BundleStatus, error) {
	st := substrate.BundleStatus{
		ID: b.Identity(), Name: b.Name, Authority: b.Authority,
		Installed: true, Enabled: true,
	}
	state, err := ds.bundleStateOf(ctx, b.Authority)
	if err != nil {
		return st, err
	}
	// Resolved from the live registry, so it is installed; an uninstalled
	// bundle has no row here — it left the registry with its schema (ticket
	// 034), and BundleStatuses simply stops listing it.
	st.Installed = true
	st.Enabled = !state.Disabled
	g, ok := ds.registry().AuthorityByName(b.Authority)
	if !ok {
		return st, fmt.Errorf("%w: bundle authority %s", substrate.ErrNotFound, b.Authority)
	}
	st.Functions = len(g.FunctionOrder)
	st.Kinds = len(g.KindOrder)
	for _, tn := range g.KindOrder {
		t := g.Kinds[tn]
		var n int64
		if err := ds.db.QueryRowContext(ctx,
			`SELECT count(*) FROM records WHERE kind = $1 AND deleted_at IS NULL`, t.Identity).Scan(&n); err != nil {
			return st, err
		}
		st.LiveRecords += n
		if t.Implements(vocabulary.TraitAccountConfigCore) {
			st.Accounts += int(n)
		}
	}
	// Setup mirrors what dispatch would refuse, and nothing else: each
	// input's resolution, the resolved OAuth client's completeness, and each
	// agent's provider row. A non-refusal (zero accounts, say) is never a
	// setup step — that inflation is how the old always-on "needs
	// configuration" badge happened.
	ris, err := ds.resolveBundleInputs(ctx, b)
	if err != nil {
		return st, err
	}
	for _, ri := range ris {
		is := substrate.InputStatus{Name: ri.Name, Kind: ri.Input.Kind, Description: ri.Input.Description}
		if ri.Row != nil {
			is.Record, is.Via = ri.Row.ID, ri.Via
		}
		st.Inputs = append(st.Inputs, is)
		switch {
		case ri.Problem != "":
			st.Setup = append(st.Setup, substrate.SetupItem{
				Code: ri.Problem, Input: ri.Name, Kind: ri.Input.Kind, Message: ri.Detail,
			})
		case b.OAuth2 != nil && ri.Name == b.OAuth2.ClientInput:
			// The client resolved; mirror oauthClientOf's completeness
			// refusal without opening the sealed secret.
			if propString(ri.Row, "clientId") == "" || propString(ri.Row, "clientSecret") == "" {
				st.Setup = append(st.Setup, substrate.SetupItem{
					Code: substrate.SetupOAuthClient, Input: ri.Name, Kind: ri.Input.Kind, Record: ri.Row.ID,
					Message: fmt.Sprintf("%s/%s is missing clientId or clientSecret", ri.Input.Kind, ri.Row.ID),
				})
			}
		}
	}
	// The agents' providers live OUTSIDE the bundle (core llmprovider rows),
	// and are still half of "will this bundle run": dry-run the same
	// resolution dispatch performs and surface its refusal verbatim.
	probed := map[string]bool{}
	for _, an := range g.AgentOrder {
		ag := g.Agents[an]
		if ag.Provider == "" || probed[ag.Provider] {
			continue
		}
		probed[ag.Provider] = true
		if _, err := ds.resolveProvider(ctx, ag.Provider); err != nil {
			st.Setup = append(st.Setup, substrate.SetupItem{
				Code: substrate.SetupProvider, Kind: typeProvider, Record: ag.Provider,
				Message: err.Error(),
			})
		}
	}
	return st, nil
}

// --- trait queries ----------------------------------------------------------------

// TypesImplementing lists every declared type implementing a trait —
// the trait-as-interface read the console's "account configs" view is built
// on. A full identity matches resolved bindings exactly; a bare name resolves
// only when it names a single declared trait — never widened to a local name,
// which would let a bundle-local look-alike answer for a core trait.
func (ds *dataset) TypesImplementing(ctx context.Context, trait string) ([]substrate.KindInfo, error) {
	types, err := ds.registry().ImplementingStrict(trait)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", substrate.ErrValidation, err)
	}
	out := make([]substrate.KindInfo, 0, len(types))
	for _, t := range types {
		out = append(out, typeInfo(t))
	}
	return out, nil
}
