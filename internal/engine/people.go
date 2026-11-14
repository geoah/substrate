package engine

// THE PEOPLE OF A REPOSITORY (decision 0274, rows 21 to 23 of pull request
// 1313's plan). A person is in a repository while a live person actor there
// names their identity: the actor record is the truth, and the history and
// an export carry it. `identity_repositories` is the server's index of those
// actors, for the identities this server holds: what a sign-in lists and a
// token is checked against, read without opening a repository.
//
// TWO INVARIANTS hold the index to the actors.
//
//   - Never ahead. While the server admits anyone, the index names no person
//     the repository's live person actors do not name. Every derivation is
//     exact, to the empty set included, for a repository whose history has
//     been given identities (derivePeople); a removal takes the index row out
//     first, and in the erase it goes in the same transaction as the actor's
//     rows. The one exception is a repository from before identities, told
//     apart by what the server recorded in it (fromBeforeIdentities), whose
//     first person's actor names nobody yet or is not written yet: the boot
//     gave it its person in the index from its sign-in record, and its first
//     open names that person on the actor (settlePeople). Nothing recreates or re-enables a
//     person's actor that is disabled or tombstoned (namePersonActor,
//     ensureActor): new work that acts for that person is refused, what
//     they left running is settled for nobody (settling), and another
//     person's writes link their own actor (personActorFor).
//   - Behind only while the database refuses the write. The index lacks a
//     person the live actors name only between that actor's commit and the
//     end of the same request's index write, which does not end with the
//     request: it runs on a context of its own and is tried again until it
//     lands or the process stops, and the boot's derivation finishes it
//     (indexPerson). A registration writes its person's row with the
//     repository's own, in the creation's commit, before which the
//     repository does not exist.
//
// Where actors arrive another way (an import, a restore, a conversion, a
// rebuild) the index is derived again from them before the repository is
// offered to anyone: at every boot, for every repository, before the server
// listens, and at the end of a rebuild. An open repairs nothing. A reset
// reads the actors under the repository's lock (ResetUser).
//
// The repository's first person, the one its creation wrote, is the actor
// keyed `person` (substrate.ActorPerson); another person's actor is keyed by
// their identity (substrate.PersonActor), and their writes link it
// (personActorFor).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
)

// personActor is one live person actor: its key and the identity it names,
// "" for none.
type personActor struct {
	key      substrate.Actor
	identity string
	// name is the name the actor carries, the person's sign-in name when it
	// was written.
	name string
}

// personActorsOf reads a repository's live person actors, the first
// person's first, through any handle on its rows: the repository's own pool
// or the control plane's.
func personActorsOf(ctx context.Context, q dbx, repository string) ([]personActor, error) {
	rows, err := q.QueryContext(ctx, `SELECT id, props FROM records
		WHERE repository = $3 AND kind = $1 AND deleted_at IS NULL AND props->>'type' = $2
		  AND COALESCE(props->>'disabled', 'false') <> 'true'
		ORDER BY id`, kindActor, substrate.ActorTypePerson, repository)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []personActor
	for rows.Next() {
		var id string
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var props map[string]any
		if err := json.Unmarshal(raw, &props); err != nil {
			return nil, err
		}
		identity, _ := props["identity"].(string)
		name, _ := props["name"].(string)
		key := substrate.ActorPerson
		if id != actorID(substrate.ActorPerson) {
			if identity == "" {
				continue
			}
			key = substrate.PersonActor(identity)
		}
		out = append(out, personActor{key: key, identity: identity, name: name})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	slices.SortStableFunc(out, func(a, b personActor) int {
		switch {
		case a.key == substrate.ActorPerson:
			return -1
		case b.key == substrate.ActorPerson:
			return 1
		}
		return 0
	})
	return out, nil
}

// namePersonActor writes the identity a person actor names, as the
// system, creating the actor when the repository does not hold it. A
// repository whose actor kind does not declare `identity` yet (one mid
// upgrade) is named at the open after its upgrade (settlePeople).
func (t *txn) namePersonActor(key substrate.Actor, identity, name string) error {
	if ty, err := t.resolveType(kindActor); err != nil || ty.Props["identity"] == nil {
		return nil
	}
	wasScoped, wasInternal := t.scoped, t.internal
	t.scoped, t.internal = false, true
	defer func() { t.scoped, t.internal = wasScoped, wasInternal }()
	id := actorID(key)
	return t.asActor(substrate.ActorSystem, func() error {
		row, err := t.loadRow(eref{Kind: kindActor, ID: id}, true)
		if err != nil {
			return err
		}
		// A person's actor its history took out stays out: nothing
		// recreates or re-enables it.
		if row != nil && (row.DeletedAt != nil || row.Props["disabled"] == true) {
			return fmt.Errorf("%w: the person actor %s was taken out of the repository's history, and is not written again", substrate.ErrConflict, id)
		}
		if row != nil {
			_, err := t.patch(eref{Kind: kindActor, ID: id}, substrate.PatchInput{
				Properties: map[string]any{"identity": identity},
			})
			return err
		}
		props := map[string]any{"type": substrate.ActorTypePerson, "name": name, "identity": identity}
		if _, err := t.put(substrate.PutInput{Kind: kindActor, ID: id, Properties: props}); err != nil {
			return err
		}
		t.standing(id)
		return nil
	})
}

// settlePeople names, at an open, what a release from before identities
// left unnamed, and keeps the first person for personActorFor. It runs after
// the shipped upgrade, which declares the actor's identity and the token's.
//
//   - A repository that was from before identities when the open began
//     (fromBeforeIdentities), whose first person's actor is live and names
//     nobody, or is not written yet, gets the repository's one person in the
//     index written onto it. Nothing else is named here: the
//     actor of a repository whose history has been given identities says
//     who its person is, and one its history took out stays out.
//   - Every token that names no person is named after the first person.
func (ds *dataset) settlePeople(ctx context.Context) error {
	actors, err := personActorsOf(ctx, ds.db, ds.info.ID)
	if err != nil {
		return err
	}
	if !ds.svc.readOnly && len(ds.people) == 1 && ds.beforeIdentities {
		first, err := firstPersonOf(ctx, ds.db, ds.info.ID)
		if err != nil {
			return err
		}
		if !first.exists || (first.live && first.identity == "") {
			person := ds.people[0]
			// The person from before roles could do everything, and keeps
			// it as `admin`.
			if err := ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
				if err := t.namePersonActor(substrate.ActorPerson, person, ds.name()); err != nil {
					return err
				}
				return t.givePersonRoles(substrate.ActorPerson, scope.RoleAdmin)
			}); err != nil {
				return fmt.Errorf("substrate/engine: open repository %s: name its person: %w", ds.info.ID, err)
			}
			if actors, err = personActorsOf(ctx, ds.db, ds.info.ID); err != nil {
				return err
			}
		}
	}
	ds.setFirstPerson(actors)
	if ds.svc.readOnly {
		return nil
	}
	return ds.nameTokensPeople(ctx)
}

// firstPerson is the state of a repository's first person's actor: whether
// its record exists at all, whether it is live (not tombstoned, not
// disabled), and the identity it names.
type firstPerson struct {
	exists, live bool
	identity     string
}

func firstPersonOf(ctx context.Context, q dbx, repository string) (firstPerson, error) {
	var deleted bool
	var raw []byte
	err := q.QueryRowContext(ctx, `SELECT deleted_at IS NOT NULL, props FROM records
		WHERE repository = $1 AND kind = $2 AND id = $3`, repository, kindActor, actorID(substrate.ActorPerson)).Scan(&deleted, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return firstPerson{}, nil
	}
	if err != nil {
		return firstPerson{}, err
	}
	var props map[string]any
	if err := json.Unmarshal(raw, &props); err != nil {
		return firstPerson{}, err
	}
	identity, _ := props["identity"].(string)
	return firstPerson{exists: true, live: !deleted && props["disabled"] != true, identity: identity}, nil
}

// fromBeforeIdentities reports whether a repository is from before
// identities, by what the server recorded in it. Any one of three facts says
// so:
//   - its stored actor kind declares no `identity`: no release since writes
//     one without it, and the first open's upgrade declares it, so the open
//     reads this before it upgrades (dataset.beforeIdentities);
//   - it holds a `core/credential` row, the sign-in a release before
//     identities kept as a record, until repository migration 12 purges it;
//   - its history holds a `core/credential` line, which that release wrote
//     with the record and the purge writes again with its own.
//
// A repository created since holds none of them, whether or not anybody is
// left in it.
func fromBeforeIdentities(ctx context.Context, q dbx, repository string) (bool, error) {
	var before bool
	err := q.QueryRowContext(ctx, `SELECT
		NOT EXISTS (SELECT 1 FROM records WHERE repository = $1 AND kind = $2 AND id = $3
			AND deleted_at IS NULL AND props->'properties' ? 'identity')
		OR EXISTS (SELECT 1 FROM records WHERE repository = $1 AND kind = $4)
		OR EXISTS (SELECT 1 FROM changelog WHERE repository = $1 AND kind = $4)`,
		repository, kindKind, kindActor, kindCredential).Scan(&before)
	return before, err
}

// derivePeople writes a repository's rows of the index from its live person
// actors, exactly: an actor naming an identity this server holds gains its
// row, and a row no live person actor names goes, under the token cache's
// fence, down to none. An identity the server does not hold (a restore from
// another server) gains none, and `user reset` gives it back. The one
// exception is a repository from before identities whose first person's
// actor names nobody yet (or is not written yet): it keeps the rows the boot
// gave it from its sign-in record, which its first open writes onto the
// actor.
func (s *service) derivePeople(ctx context.Context, repository string) error {
	actors, err := personActorsOf(ctx, s.maint, repository)
	if err != nil {
		return err
	}
	named := make([]string, 0, len(actors))
	for _, a := range actors {
		if a.identity != "" {
			named = append(named, a.identity)
		}
	}
	if len(named) == 0 {
		first, err := firstPersonOf(ctx, s.maint, repository)
		if err != nil {
			return err
		}
		before, err := fromBeforeIdentities(ctx, s.maint, repository)
		if err != nil {
			return err
		}
		if before && (!first.exists || (first.live && first.identity == "")) {
			return nil
		}
	}
	held, err := heldIdentities(ctx, s.maint, named)
	if err != nil {
		return err
	}
	current, err := repositoryIdentities(ctx, s.maint, repository)
	if err != nil {
		return err
	}
	var gone []string
	for _, id := range current {
		if !slices.Contains(held, id) {
			gone = append(gone, id)
		}
	}
	if len(gone) > 0 {
		if err := s.unlinkPeople(ctx, repository, gone); err != nil {
			return err
		}
		s.log.Info("substrate: took out of a repository the people no actor of it names", "repository", repository, "people", len(gone))
	}
	for _, id := range held {
		if !slices.Contains(current, id) {
			if err := linkIdentity(ctx, s.maint, id, repository); err != nil {
				return err
			}
		}
	}
	return nil
}

// indexPerson writes a person's row of the index after their actor
// committed. The write does not end with the request: it runs on a context
// of its own, and one the database refuses is tried again in the background
// until it lands or the process stops, when the boot's derivation finishes
// it. Its error is still the request's answer.
func (s *service) indexPerson(identity, repository string) error {
	err := s.writeIndexRow(context.Background(), identity, repository)
	if err == nil {
		return nil
	}
	s.finishIndex(repository, func(ctx context.Context) error {
		return s.writeIndexRow(ctx, identity, repository)
	})
	return fmt.Errorf("substrate/engine: index the person of repository %s: %w (tried again until it lands)", repository, err)
}

// indexPersonInDoubt finishes an addition whose actor commit is in doubt
// (decision 0271): it may have landed, or rolled back. Never a blind write:
// once the database has proved that commit ended (the changelog lock its
// transaction held is granted), the live person actors are read, and the
// row is written only where the actor names the identity, after named runs.
func (s *service) indexPersonInDoubt(key substrate.Actor, identity, repository string, named func()) {
	s.finishIndex(repository, func(ctx context.Context) error {
		if err := s.awaitLockFree(ctx, Scope{Repository: repository}.lockKey(changelogLockKey)); err != nil {
			return err
		}
		actors, err := personActorsOf(ctx, s.maint, repository)
		if err != nil {
			return err
		}
		landed := slices.ContainsFunc(actors, func(a personActor) bool { return a.key == key && a.identity == identity })
		if s.testPersonInDoubt != nil {
			s.testPersonInDoubt(landed)
		}
		if !landed {
			return nil
		}
		named()
		return s.writeIndexRow(ctx, identity, repository)
	})
}

func (s *service) writeIndexRow(ctx context.Context, identity, repository string) error {
	if s.testIndexFault != nil {
		if err := s.testIndexFault(); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return linkIdentity(ctx, s.maint, identity, repository)
}

// finishIndex runs try as a supervised background task on a context of its
// own until it succeeds or the process stops, pausing longer after each
// failure; the boot's derivation decides what a stop leaves.
func (s *service) finishIndex(repository string, try func(context.Context) error) {
	s.spawn("index a person", repository, func(ctx context.Context) {
		pause := 50 * time.Millisecond
		for ctx.Err() == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(pause):
			}
			if try(ctx) == nil {
				return
			}
			pause = min(2*pause, tokenProofRetryMax)
		}
	})
}

// heldIdentities are the identities of ids this server holds.
func heldIdentities(ctx context.Context, q dbx, ids []string) ([]string, error) {
	held := []string{}
	if len(ids) == 0 {
		return held, nil
	}
	rows, err := q.QueryContext(ctx, `SELECT id FROM identities WHERE id = ANY($1::text[]) ORDER BY id`, ids)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		held = append(held, id)
	}
	return held, rows.Err()
}

// unlinkPeople takes identities out of a repository's index, under the token
// cache's rule: the repository is fenced from before the COMMIT until its
// outcome is proved, so none of their tokens there answers from the cache
// after.
func (s *service) unlinkPeople(ctx context.Context, repository string, identities []string) error {
	tx, err := s.maint.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := lockTokens(ctx, tx, repository); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM identity_repositories WHERE repository_id = $1 AND identity = ANY($2::text[])`,
		repository, identities); err != nil {
		return err
	}
	return s.commitFencingTokens(tx, repository)
}

// setFirstPerson keeps the identity the first person's actor names, which
// every person's write is told apart by (personActorFor).
func (ds *dataset) setFirstPerson(actors []personActor) {
	first := ""
	if len(actors) > 0 && actors[0].key == substrate.ActorPerson {
		first = actors[0].identity
	}
	ds.firstPerson.Store(first)
}

// settling is the context a settlement of work a stop or a lost run left
// behind runs on: acting for whom the work acted for, unless that is a person
// whose actor the history took out. Settling their work is the engine's own
// act, for nobody: it revives no actor and runs nothing further for them, so
// an open never fails on the work of somebody who is no longer there. New
// work for them is still refused (ensureActor).
func (ds *dataset) settling(ctx context.Context, err error) (context.Context, error) {
	if err != nil {
		return ctx, err
	}
	behalf := substrate.OnBehalfOfFrom(ctx)
	if !substrate.IsPerson(behalf) {
		return ctx, nil
	}
	var out bool
	err = ds.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM records
		WHERE kind = $1 AND id = $2 AND (deleted_at IS NOT NULL OR props->'disabled' = 'true'))`,
		kindActor, actorID(behalf)).Scan(&out)
	if err != nil {
		return ctx, fmt.Errorf("read whether the person work acted for is still here: %w", err)
	}
	if out {
		return substrate.WithOnBehalfOf(ctx, ""), nil
	}
	return ctx, nil
}

// personActorFor is the actor a person's write links: the first person's
// when the request's token names them, or nobody (one from before
// identities); every other person's own, the person a reset gave a
// repository whose first person's actor its history took out included, so
// no write of theirs links the actor taken out.
func (ds *dataset) personActorFor(ctx context.Context, actor substrate.Actor) substrate.Actor {
	if actor != substrate.ActorPerson {
		return actor
	}
	identity := substrate.IdentityFrom(ctx)
	first, _ := ds.firstPerson.Load().(string)
	if identity == "" || identity == first {
		return actor
	}
	return substrate.PersonActor(identity)
}

// nameTokensPeople gives every live token that names no person the
// repository's first person, as an ordinary write of the system, validated
// against the token kind this binary ships. A release before identities
// minted tokens naming no person. It finds nothing once each is named. A
// repository whose first person names nobody has nobody to name: sign out
// everywhere ends those tokens with the repository's (signout.go). A token
// naming a person stays theirs: one a restore brought back naming a person
// this server does not hold is refused while nobody of that identity may open
// the repository (tokenByHash), and never handed to another.
func (ds *dataset) nameTokensPeople(ctx context.Context) error {
	person, _ := ds.firstPerson.Load().(string)
	if person == "" {
		return nil
	}
	var unnamed bool
	if err := ds.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM records
		WHERE kind = $1 AND deleted_at IS NULL AND COALESCE(props->>'identity', '') = '')`, kindToken).Scan(&unnamed); err != nil {
		return err
	}
	if !unnamed {
		return nil
	}
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		ids, err := t.unnamedTokens()
		if err != nil {
			return err
		}
		for _, id := range ids {
			if _, err := t.patch(eref{Kind: kindToken, ID: id}, substrate.PatchInput{
				Properties: map[string]any{"identity": person},
			}); err != nil {
				return err
			}
		}
		ds.svc.log.Info("substrate: named the person of a repository's tokens", "repository", ds.info.ID, "tokens", len(ids))
		return nil
	})
}

// AddPerson lets the identity named identityName open the repository named
// repository, holding roles, as its first person when its first person names
// nobody, else as another, with an actor of their own: the actor first, then
// the index. Nothing on the network adds a person; this is the operator's
// door and the tests'.
func (s *service) AddPerson(ctx context.Context, identityName, repository string, roles ...string) error {
	if len(roles) == 0 {
		return fmt.Errorf("%w: a person is added holding at least one role", substrate.ErrValidation)
	}
	person, err := s.identityByName(ctx, s.maint, identityName)
	if err != nil {
		return err
	}
	repo, err := s.repositoryByKey(ctx, repository)
	if err != nil {
		return err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return err
	}
	actors, err := personActorsOf(ctx, ds.db, repo.ID)
	if err != nil {
		return err
	}
	key := substrate.PersonActor(person.ID)
	if len(actors) > 0 && actors[0].key == substrate.ActorPerson && (actors[0].identity == "" || actors[0].identity == person.ID) {
		key = substrate.ActorPerson
	}
	err = ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		if err := t.namePersonActor(key, person.ID, person.Name); err != nil {
			return err
		}
		return t.setPersonRoles(key, roles)
	})
	named := func() {
		if key == substrate.ActorPerson {
			ds.firstPerson.Store(person.ID)
		}
	}
	if errors.Is(err, substrate.ErrWriteInDoubt) {
		s.indexPersonInDoubt(key, person.ID, repo.ID, named)
		return err
	}
	if err != nil {
		return err
	}
	named()
	return s.indexPerson(person.ID, repo.ID)
}

// SetPersonRoles replaces the roles the person named identityName holds in
// the repository named repository. It is the operator's door and the tests':
// nothing on the network changes a role at 1.0.
func (s *service) SetPersonRoles(ctx context.Context, identityName, repository string, roles ...string) error {
	if len(roles) == 0 {
		return fmt.Errorf("%w: a person holds at least one role; take them out of the repository instead", substrate.ErrValidation)
	}
	person, err := s.identityByName(ctx, s.maint, identityName)
	if err != nil {
		return err
	}
	repo, err := s.repositoryByKey(ctx, repository)
	if err != nil {
		return err
	}
	ds, err := s.open(ctx, repo)
	if err != nil {
		return err
	}
	return ds.inTx(ctx, substrate.ActorSystem, true, func(t *txn) error {
		actors, err := personActorsOf(ctx, t.tx, repo.ID)
		if err != nil {
			return err
		}
		i := slices.IndexFunc(actors, func(a personActor) bool { return a.identity == person.ID })
		if i < 0 {
			return fmt.Errorf("%w: %s is no person of repository %s", substrate.ErrNotFound, person.Name, repo.label())
		}
		return t.setPersonRoles(actors[i].key, roles)
	})
}

// setPersonRoles writes the roles a person's actor holds, as the engine's own
// write: each a role the repository holds, and never leaving it without a
// person who holds `admin` (the fixed check on roles, decision 0275). Who may
// ask is the door's to decide; at 1.0 only the operator's and the engine's
// own do.
func (t *txn) setPersonRoles(key substrate.Actor, roles []string) error {
	if ty, err := t.resolveType(kindActor); err != nil || ty.Props["roles"] == nil {
		return nil
	}
	for _, id := range roles {
		row, err := t.loadRow(eref{Kind: kindRole, ID: id}, false)
		if err != nil {
			return err
		}
		if row == nil || row.DeletedAt != nil {
			return fmt.Errorf("%w: the repository holds no role %q", substrate.ErrValidation, id)
		}
	}
	if !slices.Contains(roles, scope.RoleAdmin) {
		admins, err := t.adminsBut(key)
		if err != nil {
			return err
		}
		if admins == 0 {
			if held, err := t.rolesOf(key); err != nil {
				return err
			} else if held.Admin() {
				return fmt.Errorf("%w: the repository's last admin keeps the role: give it to another person first", substrate.ErrConflict)
			}
		}
	}
	wasScoped, wasInternal := t.scoped, t.internal
	t.scoped, t.internal = false, true
	defer func() { t.scoped, t.internal = wasScoped, wasInternal }()
	delete(t.roles, key)
	return t.asActor(substrate.ActorSystem, func() error {
		_, err := t.patch(eref{Kind: kindActor, ID: actorID(key)}, substrate.PatchInput{
			Properties: map[string]any{"roles": roleRefs(roles...)},
		})
		return err
	})
}

// givePersonRoles gives a person's live actor roles where it holds none: the
// person a repository is created for, and the person from before roles,
// who could do everything and keeps that as `admin`.
func (t *txn) givePersonRoles(key substrate.Actor, roles ...string) error {
	if ty, err := t.resolveType(kindActor); err != nil || ty.Props["roles"] == nil {
		return nil
	}
	row, err := t.loadRow(eref{Kind: kindActor, ID: actorID(key)}, true)
	if err != nil || row == nil || row.DeletedAt != nil || row.Props["disabled"] == true {
		return err
	}
	if raw, err := json.Marshal(row.Props["roles"]); err == nil && len(roleIDs(raw)) > 0 {
		return nil
	}
	return t.setPersonRoles(key, roles)
}

// adminsBut counts the live person actors other than key that hold `admin`.
func (t *txn) adminsBut(key substrate.Actor) (int, error) {
	actors, err := personActorsOf(t.ctx, t.tx, t.ds.info.ID)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, a := range actors {
		if a.key == key {
			continue
		}
		roles, err := t.rolesOf(a.key)
		if err != nil {
			return 0, err
		}
		if roles.Admin() {
			n++
		}
	}
	return n, nil
}
