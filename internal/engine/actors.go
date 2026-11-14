package engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// ACTORS ARE RECORDS (decision 0270). Every hand that writes is a record of
// `substrate.reamde.dev/core/actor`: the repository's one person, an app the
// person approved, an installed function, agent and package, and the system.
// The engine writes each when the hand comes to be (the creation, an
// approval, an install) and disables, never deletes, one whose code is gone,
// so an old write still links to something. A history line, a change event,
// a property's mark and every stored value name the hand by the record's id
// (vocabulary.ActorID); the substrate.Actor key the engine decides a tier, a
// namespace or an echo on stays in memory.

// actorID is the id of the actor record a hand is.
func actorID(a substrate.Actor) string { return vocabulary.ActorID(a) }

// actorKeyOf is the hand a stored actor id links to: what this process
// derived, else what the actor record says it is, else a key that names the
// id alone, so the id round-trips unchanged.
func (t *txn) actorKeyOf(id string) substrate.Actor {
	return actorKeyIn(t.ctx, t.tx, id)
}

// actorKeyIn is actorKeyOf over any handle on the repository's rows.
func actorKeyIn(ctx context.Context, q dbx, id string) substrate.Actor {
	if id == "" {
		return ""
	}
	if key, ok := vocabulary.ActorKeyOf(id); ok {
		return key
	}
	// The first person's record names their identity, as another person's
	// does; its id is what makes it the first.
	if id == actorID(substrate.ActorPerson) {
		return substrate.ActorPerson
	}
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT props FROM records WHERE kind = $1 AND id = $2`, kindActor, id).Scan(&raw)
	if err == nil {
		var props map[string]any
		if json.Unmarshal(raw, &props) == nil {
			if key, ok := vocabulary.ActorKeyFrom(props); ok {
				vocabulary.RememberActor(id, key)
				return key
			}
		}
	}
	return substrate.ActorByID(id)
}

// ensureLinks writes, before an entry by actor is appended, the actor records
// the entry will link and the repository does not hold yet: the hand's own
// and the one it acts for (linksOf). The hand's first write is when it comes
// to be for most hands (an install's first declaration, a function's first
// effect); the creation and an approval write theirs ahead of that. The
// records' own entries land first, each with only its own effects, so the
// entry that follows still carries every effect of the write it records.
func (t *txn) ensureLinks(actor substrate.Actor) error {
	if t.ensuringActor {
		return nil
	}
	// The system first: every actor record is written under its hand.
	keys := []substrate.Actor{substrate.ActorSystem, actor}
	if l := t.linksOf(actor); l.onBehalfOf != "" {
		keys = append(keys, t.onBehalfOfKey(actor))
	}
	return t.ensureActors(keys...)
}

// ensureActors writes the actor record of each hand the repository does not
// hold live, each in an entry of its own ahead of whatever the transaction
// has folded and not yet appended.
func (t *txn) ensureActors(keys ...substrate.Actor) error {
	if t.ensuringActor {
		return nil
	}
	var missing []substrate.Actor
	for _, key := range keys {
		if id := actorID(key); id != "" && !t.ds.actorLive(id) && !t.actorsStanding[id] {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 || !t.actorKindDeclared() {
		return nil
	}
	outer := t.folded
	t.folded = nil
	t.ensuringActor = true
	defer func() { t.ensuringActor = false }()
	for _, key := range missing {
		if err := t.ensureActor(key); err != nil {
			return err
		}
	}
	t.folded = append(t.folded, outer...)
	return nil
}

// onBehalfOfKey is the hand a write by actor acts for, as linksOf links it.
func (t *txn) onBehalfOfKey(actor substrate.Actor) substrate.Actor {
	if substrate.ActorTypeOf(actor) == substrate.ActorTypeApp {
		return t.ds.personActorFor(t.ctx, substrate.ActorPerson)
	}
	return t.onBehalfOf
}

// actorKindDeclared reports whether the repository's actor kind is the one
// this binary writes: a repository mid-upgrade still holds the declaration
// kind an actor once was, and its first open after the boot upgrade, and
// the repository migration before it, write what is missing then.
func (t *txn) actorKindDeclared() bool {
	ty, err := t.resolveType(kindActor)
	return err == nil && ty.Props["type"] != nil
}

// ensureActor writes the actor record of a hand, under the system's own hand,
// when the repository does not hold it live: the first write of the hand, or
// the hand coming back after its code was removed. A record that stands is
// left alone. A hand no type covers has no record.
func (t *txn) ensureActor(key substrate.Actor) error {
	if key == "" || substrate.ActorTypeOf(key) == "" {
		return nil
	}
	id := actorID(key)
	person := substrate.IsPerson(key)
	row, err := t.loadRow(eref{Kind: kindActor, ID: id}, person)
	if err != nil {
		return err
	}
	if row != nil && row.DeletedAt == nil && row.Props["disabled"] != true {
		t.standing(id)
		return nil
	}
	// A person's actor the history took out stays out (people.go): the next
	// boot would let back in the person it still names. Nothing a person's
	// token writes reaches here, since the index names nobody whose actor is
	// out; work that acts for them is refused.
	if person && row != nil {
		return fmt.Errorf("%w: the person this acts for was taken out of the repository, and nothing acts for them again", substrate.ErrForbidden)
	}
	props := vocabulary.ActorProperties(key, t.ds.Repository().Authority)
	if row != nil {
		props["disabled"] = nil
	}
	wasScoped, wasInternal := t.scoped, t.internal
	t.scoped, t.internal = false, true
	defer func() { t.scoped, t.internal = wasScoped, wasInternal }()
	if err := t.asActor(substrate.ActorSystem, func() error {
		_, err := t.put(substrate.PutInput{Kind: kindActor, ID: id, Properties: props})
		return err
	}); err != nil {
		return err
	}
	t.standing(id)
	return nil
}

// standing notes an actor record the transaction found or wrote live, for
// the dataset's cache once the transaction commits.
func (t *txn) standing(id string) {
	if t.actorsStanding == nil {
		t.actorsStanding = map[string]bool{}
	}
	t.actorsStanding[id] = true
}

// settleActorCache is what a committed transaction leaves in the actor cache:
// each record it disabled invalidated again, now that the disable is visible,
// and what it found or wrote standing published, unless an invalidation came
// after the transaction began. Writes serialize on the changelog lock, but
// this runs after the lock is let go, beside the next transaction.
func (ds *dataset) settleActorCache(t *txn) {
	for _, id := range t.actorsDisabled {
		ds.forgetActorLive(id)
	}
	if len(t.actorsStanding) > 0 {
		ds.actors.publish(t.actorsStanding, t.actorEpoch)
	}
}

// disableActor marks the actor record of a hand whose code is gone disabled:
// an old write still links to it, and nothing writes as it again.
func (t *txn) disableActor(key substrate.Actor) error {
	id := actorID(key)
	row, err := t.loadRow(eref{Kind: kindActor, ID: id}, false)
	if err != nil || row == nil || row.DeletedAt != nil || row.Props["disabled"] == true {
		return err
	}
	t.ds.forgetActorLive(id)
	delete(t.actorsStanding, id)
	t.actorsDisabled = append(t.actorsDisabled, id)
	wasScoped, wasInternal := t.scoped, t.internal
	t.scoped, t.internal = false, true
	defer func() { t.scoped, t.internal = wasScoped, wasInternal }()
	return t.asActor(substrate.ActorSystem, func() error {
		_, err := t.patch(eref{Kind: kindActor, ID: id}, substrate.PatchInput{Properties: map[string]any{"disabled": true}})
		return err
	})
}

// actorLive reports whether this process has seen the actor record of id
// stand in the repository, committed: the cache that spares every append a
// read. A transaction marks what it found or wrote only once it commits
// (txn.actorsStanding), so a rollback leaves no mark behind.
func (ds *dataset) actorLive(id string) bool { return ds.actors.has(id) }

func (ds *dataset) markActorLive(id string) {
	ds.actors.publish(map[string]bool{id: true}, ds.actors.now())
}

func (ds *dataset) forgetActorLive(id string) { ds.actors.forget(id) }

// actorCache is the id of every actor record this process has read standing
// in the repository, and the count of its invalidations: a transaction
// publishes what it read only when no invalidation came after it began, so a
// disable committed while it ran is never undone by its publication. One
// lock holds a publication's check and its stores together, so an
// invalidation lands before both or after both, never between them.
type actorCache struct {
	mu    sync.RWMutex
	live  map[string]bool
	epoch uint64
	// afterCheck is a test seam: it runs inside a publication, after the
	// check and before the stores.
	afterCheck func()
}

func (c *actorCache) has(id string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.live[id]
}

// now is the invalidation count, which a transaction reads as it begins.
func (c *actorCache) now() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.epoch
}

func (c *actorCache) forget(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.epoch++
	delete(c.live, id)
}

// publish caches ids as standing, unless an invalidation came after since.
func (c *actorCache) publish(ids map[string]bool, since uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.epoch != since {
		return
	}
	if c.afterCheck != nil {
		c.afterCheck()
	}
	if c.live == nil {
		c.live = map[string]bool{}
	}
	for id := range ids {
		c.live[id] = true
	}
}

// actingFor is ctx bound to whom the work a record outside a run stands for
// acts for (decision 0270): the request a judge decides acts for whoever the
// line that created it names. A run's own records (a thread, a message, an
// ask, an approval) say whose rules hold their work in their `heldTo`
// (heldto.go heldBy). The line names its onBehalfOf, or the person where the
// person's own hand wrote it, or no one: unattended work, history from before
// the conversion, and a record whose creating line is gone act for no one. A
// failed read is an error, so no work goes on as no one's for want of it.
func (ds *dataset) actingFor(ctx context.Context, kind, id string) (context.Context, error) {
	return ds.actingForLine(ctx, kind, id, `
		SELECT actor, on_behalf_of, principal, seq FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq LIMIT 1`)
}

// actingForLine binds ctx to whom the line query reads acts for, and to the
// token that line carries, whose grant holds the work where it is an app's
// (run.go). The work of a run's own records reads their `heldTo` instead
// (heldto.go): this is for a record outside a run, such as a proposal a judge
// decides.
func (ds *dataset) actingForLine(ctx context.Context, kind, id, query string) (context.Context, error) {
	var actor, behalf, token string
	var seq int64
	err := ds.db.QueryRowContext(ctx, query, kind, id).Scan(&actor, &behalf, &token, &seq)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return substrate.WithOnBehalfOf(ctx, ""), nil
	case err != nil:
		return ctx, fmt.Errorf("read whom %s %s acts for: %w", kind, id, err)
	}
	if token != "" {
		ctx = withRunHolders(ctx, nil, token)
	}
	if behalf != "" {
		return substrate.WithOnBehalfOf(ctx, actorKeyIn(ctx, ds.db, behalf)), nil
	}
	// A person's own line acts for that person, whichever person of the
	// repository they are (people.go).
	if key := actorKeyIn(ctx, ds.db, actor); substrate.ActorTypeOf(key) == substrate.ActorTypePerson {
		return substrate.WithOnBehalfOf(ctx, key), nil
	}
	return substrate.WithOnBehalfOf(ctx, ""), nil
}

// threadOf is the thread a record of a run belongs to: the thread itself, or
// the one a message, an ask, an approval or a proposal names; "" for a record
// of no run.
func (ds *dataset) threadOf(ctx context.Context, kind, id string) (string, error) {
	if kind == typeThread {
		return id, nil
	}
	row, err := ds.loadRowDB(ctx, eref{Kind: kind, ID: id})
	if err != nil {
		return "", fmt.Errorf("read the thread of %s %s: %w", kind, id, err)
	}
	if row == nil {
		return "", nil
	}
	return referenceID(row.Props[msgRelThread]), nil
}

// writeLinks are a write's three links, as the history line and the change
// event carry them: the ids of the actor record that wrote it, of the one it
// acted for, and of the token behind it.
type writeLinks struct {
	actor, onBehalfOf, token string
}

// linksOf is what a write by the hand records (decision 0270). A person's own
// write names its sign-in's token and acts for no one. An app's acts for the
// person and names the app's token. Installed code, a package and the engine
// stand behind no token, and act for the person only when a person's request
// started them; a dispatch the trigger pass fires acts for no one.
func (t *txn) linksOf(actor substrate.Actor) writeLinks {
	l := writeLinks{actor: actorID(actor), token: t.tokenOf(actor)}
	switch substrate.ActorTypeOf(actor) {
	case substrate.ActorTypePerson:
	case substrate.ActorTypeApp:
		l.onBehalfOf = actorID(t.ds.personActorFor(t.ctx, substrate.ActorPerson))
	default:
		if t.onBehalfOf != "" && substrate.CanonicalActor(t.onBehalfOf) != substrate.CanonicalActor(actor) {
			l.onBehalfOf = actorID(t.onBehalfOf)
		}
	}
	return l
}

// tokenOf is the token a write by the hand stands behind: the request's, for a
// person and an app, whose door it is, and none for anything else.
func (t *txn) tokenOf(actor substrate.Actor) string {
	switch substrate.ActorTypeOf(actor) {
	case substrate.ActorTypePerson, substrate.ActorTypeApp:
		return t.principal
	}
	return ""
}

// lineToken is the token id a history line names, for the table's column: a
// line from before the actor links names it `principal`.
func lineToken(e changelogfile.Entry) string {
	if e.Token != "" {
		return e.Token
	}
	return e.Principal
}

// declarationActor is the hand a declaration brings: a package's own, a
// function's or an agent's, "" for any other.
func declarationActor(d declaration) substrate.Actor {
	switch d.short {
	case vocabulary.DocPackage:
		if authority, pkg := vocabulary.SplitPackageRef(d.id); authority != "" {
			return substrate.BundleActor(authority, pkg)
		}
	case vocabulary.DocFunction, vocabulary.DocAgent:
		authority, pkg, name := vocabulary.SplitKindRef(d.id)
		if authority == "" {
			return ""
		}
		if d.short == vocabulary.DocFunction {
			return substrate.FunctionActor(authority, pkg, name)
		}
		return substrate.AgentActor(authority, pkg, name)
	}
	return ""
}

// actorPackageOf is the package whose code or installs a hand is: a
// function's, an agent's and a package's own, "" for any other.
func actorPackageOf(key substrate.Actor) string {
	s := string(substrate.CanonicalActor(key))
	for _, prefix := range []string{substrate.FunctionActorPrefix, substrate.AgentActorPrefix, substrate.BundleActorPrefix} {
		if rest, ok := strings.CutPrefix(s, prefix); ok {
			if parts := strings.Split(rest, ":"); len(parts) >= 2 {
				return vocabulary.PackageRef(parts[0], parts[1])
			}
		}
	}
	return ""
}

// handWords names a hand in a sentence the engine composes, by what it is and
// never by a spelling (decision 0270): a refusal an accepted change request
// records, in its conflict and in the proposing thread, is a stored value.
func handWords(key substrate.Actor) string {
	c := substrate.CanonicalActor(key)
	s := string(c)
	switch substrate.ActorTypeOf(c) {
	case substrate.ActorTypePerson:
		return "the person"
	case substrate.ActorTypeApp:
		return "the app " + strings.TrimPrefix(s, substrate.AppActorPrefix)
	case substrate.ActorTypeFunction, substrate.ActorTypeAgent:
		kind := substrate.ActorTypeFunction
		rest, ok := strings.CutPrefix(s, substrate.FunctionActorPrefix)
		if !ok {
			kind, rest = substrate.ActorTypeAgent, strings.TrimPrefix(s, substrate.AgentActorPrefix)
		}
		return "the " + kind + " " + strings.ReplaceAll(rest, ":", "/")
	case substrate.ActorTypePackage:
		if pkg := actorPackageOf(c); pkg != "" {
			return "the package " + pkg
		}
		return "a package"
	case substrate.ActorTypeSystem:
		return "the substrate"
	}
	return "a hand"
}

// disableRevokedApp disables the actor record of the app a revoked token was
// minted for, when no live token of the repository stands for that app any
// more. A token minted for no app, the person's own sign-in, is no approval.
func (t *txn) disableRevokedApp(props map[string]any) error {
	app := tokenApp(props)
	if app == "" {
		return nil
	}
	rows, err := t.query(`SELECT props FROM records WHERE kind = $1 AND deleted_at IS NULL AND props ? 'clientId'`, kindToken)
	if err != nil {
		return err
	}
	var live bool
	for rows.Next() && !live {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			_ = rows.Close()
			return err
		}
		var other map[string]any
		if json.Unmarshal(raw, &other) == nil && tokenApp(other) == app {
			live = true
		}
	}
	_ = rows.Close()
	if err := rows.Err(); err != nil || live {
		return err
	}
	return t.disableActor(app)
}

// tokenApp is the hand a token's writes are an app's under, "" for a token
// minted for no app.
func tokenApp(props map[string]any) substrate.Actor {
	client, _ := props["clientId"].(string)
	if client == "" {
		return ""
	}
	authority, _ := props["appAuthority"].(string)
	local, _ := props["appLocal"].(bool)
	app := authorize.AppActorFor(client, authority, local)
	if !substrate.IsAppActor(app) {
		return ""
	}
	return app
}

// actorKeyFor is the hand a stored actor id links, read outside any
// transaction.
func (ds *dataset) actorKeyFor(id string) substrate.Actor {
	return actorKeyIn(context.Background(), ds.db, id)
}
