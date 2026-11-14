package engine

// The index of who may open a repository is never ahead of its person
// actors, and behind them only while the database refuses its write
// (people.go). Where actors arrive without a write of the server's own, a
// restore's import or a rebuild, the index is derived again before the
// repository is offered to anyone; a reset reads the actors' word under the
// repository's lock. Every test here reaches the repository first through a
// sign-in, a token or a reset, never by opening it by hand.

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/scope"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const peoplePassword = "a-password-for-the-people-tests"

// restartable is a service over one database and one data root that a test
// stops and starts again, as a restore's boot does.
type restartable struct {
	t         *testing.T
	dsn, root string
	s         *service
	// faulted is what the reads of the rules answer while set (failRules).
	faulted atomic.Pointer[rulesFaulted]
}

func openRestartable(t *testing.T) *restartable {
	t.Helper()
	r := &restartable{t: t, dsn: MigratedDSN(t), root: t.TempDir()}
	r.start()
	t.Cleanup(func() { _ = r.s.Close() })
	return r
}

func (r *restartable) start() {
	r.t.Helper()
	svc, err := OpenForTest(r.t, context.Background(), r.dsn, WithDataRoot(r.root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds),
		WithTestRulesFault(r.rulesFault))
	if err != nil {
		r.t.Fatalf("open: %v", err)
	}
	r.s = svc.(*service)
}

// rulesFaulted is the reads failRules fails: every one, or those named.
type rulesFaulted struct {
	err   error
	whats []string
}

// rulesFault answers the error failRules set for a read it names, while it
// is set (WithTestRulesFault).
func (r *restartable) rulesFault(what string) error {
	f := r.faulted.Load()
	if f == nil || len(f.whats) > 0 && !slices.Contains(f.whats, what) {
		return nil
	}
	return f.err
}

// failRules makes the reads of the rules fail with err: those whats names
// ("token", "roles", "grant", "holders"), or every one. Nil reads again.
func (r *restartable) failRules(err error, whats ...string) {
	if err == nil {
		r.faulted.Store(nil)
		return
	}
	r.faulted.Store(&rulesFaulted{err: err, whats: whats})
}

func (r *restartable) restart() {
	r.t.Helper()
	_ = r.s.Close()
	r.start()
}

func (r *restartable) register(name string) {
	r.t.Helper()
	if _, err := r.s.Register(context.Background(), substrate.RegisterInput{
		Identity: name, Repository: name, Password: peoplePassword, SkipTOTP: true,
	}); err != nil {
		r.t.Fatalf("register %s: %v", name, err)
	}
}

func (r *restartable) signIn(name, repository string) (substrate.MintedToken, error) {
	return r.s.Login(context.Background(), substrate.LoginInput{Identity: name, Repository: repository, Password: peoplePassword, Label: "session"})
}

// forgetIndexRow takes a repository's row out of the index by hand, as an
// import into a database that never had it leaves it.
func (r *restartable) forgetIndexRow(repository string) {
	r.t.Helper()
	if _, err := r.s.maint.ExecContext(context.Background(), `
		DELETE FROM identity_repositories WHERE repository_id = (SELECT id FROM repositories WHERE authority = $1)`, repository); err != nil {
		r.t.Fatal(err)
	}
}

func (r *restartable) identities() int {
	r.t.Helper()
	var n int
	if err := r.s.maint.QueryRowContext(context.Background(), `SELECT count(*) FROM identities`).Scan(&n); err != nil {
		r.t.Fatal(err)
	}
	return n
}

// The first token check after a restore whose history names another person
// follows the actors, not the index the database still held: the boot
// derived the index before the server listened, so the token of the person
// no actor names is refused on its first use, and the person the actor names
// signs in.
func TestTheFirstTokenCheckAfterARestoreFollowsTheActors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openRestartable(t)
	label := testdb.RepositoryLabel(t)
	ada, bo := label+"a.example.com", label+"b.example.com"
	r.register(ada)
	r.register(bo)
	session, err := r.signIn(ada, ada)
	if err != nil {
		t.Fatal(err)
	}
	// What a restore brings: the repository's history, in which its person's
	// actor names Bo.
	d, err := r.s.Dataset(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	boID := identityIDOf(t, r.s, bo)
	if err := d.(*dataset).inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		return tx.namePersonActor(substrate.ActorPerson, boID, ada)
	}); err != nil {
		t.Fatal(err)
	}
	r.restart()
	if _, _, err := r.s.Authenticate(ctx, session.Secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("the first check of a token whose person no actor names = %v, want ErrAuth", err)
	}
	if _, err := r.signIn(bo, ada); err != nil {
		t.Fatalf("the person the actor names, signing in: %v", err)
	}
}

// An imported repository whose index row is missing: the boot derives it
// from the actor, so the person's token and sign-in work before anything
// opens the repository.
func TestATokenAndASignInOfAnImportedRepositoryWhoseIndexRowIsMissing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openRestartable(t)
	ada := testdb.Repository(t)
	r.register(ada)
	session, err := r.signIn(ada, "")
	if err != nil {
		t.Fatal(err)
	}
	r.forgetIndexRow(ada)
	r.restart()
	if _, _, err := r.s.Authenticate(ctx, session.Secret); err != nil {
		t.Fatalf("the person's token after the import: %v", err)
	}
	if _, err := r.signIn(ada, ""); err != nil {
		t.Fatalf("the person's sign-in after the import: %v", err)
	}
}

// A reset of a repository whose actor names a person this server holds
// makes no other person: the repository has its person, whatever the index
// says, and they sign in with their own factors.
func TestAResetOfARepositoryWhoseActorNamesAPersonHereMakesNoOther(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openRestartable(t)
	ada := testdb.Repository(t)
	r.register(ada)
	repo, err := r.s.repositoryByName(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	r.forgetIndexRow(ada)
	before := r.identities()
	// Under another name, as an operator who did not know the person is here
	// would give it.
	if _, err := r.s.ResetUser(ctx, repo.ID, "an-operator-issued-passphrase", testdb.RepositoryLabel(t)+"2.example.com"); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a reset of a repository whose actor names a person here = %v, want ErrConflict", err)
	}
	if n := r.identities(); n != before {
		t.Fatalf("the refused reset left %d identities from %d", n, before)
	}
	r.restart()
	if _, err := r.signIn(ada, ""); err != nil {
		t.Fatalf("the person's sign-in after the import: %v", err)
	}
}

// The same after a rebuild: it derives the index again from the actors it
// replayed, before anything is admitted.
func TestARebuildGivesTheIndexItsActorsBack(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openRestartable(t)
	ada := testdb.Repository(t)
	r.register(ada)
	repo, err := r.s.repositoryByName(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	session, err := r.signIn(ada, "")
	if err != nil {
		t.Fatal(err)
	}
	r.forgetIndexRow(ada)
	if _, err := r.s.RebuildRepository(ctx, ada); err != nil {
		t.Fatal(err)
	}
	if _, _, err := r.s.Authenticate(ctx, session.Secret); err != nil {
		t.Fatalf("the person's token after the rebuild: %v", err)
	}
	if _, err := r.signIn(ada, ""); err != nil {
		t.Fatalf("the person's sign-in after the rebuild: %v", err)
	}
	before := r.identities()
	if _, err := r.s.ResetUser(ctx, repo.ID, "an-operator-issued-passphrase", ""); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("a reset after the rebuild = %v, want ErrConflict", err)
	}
	if n := r.identities(); n != before {
		t.Fatalf("the refused reset left %d identities from %d", n, before)
	}
}

// A history that takes the repository's last person out (their actor
// disabled, or tombstoned) leaves nobody in the index, whatever row the
// database still held: their token stops at its first check after the
// boot, the open recreates and re-enables nothing, and only a reset that
// writes a person's actor gives the repository a person again. A reset
// beside a running server writes no repository, so it is refused.
func TestAHistoryThatTakesTheLastPersonOutLeavesNobodyIn(t *testing.T) {
	t.Parallel()
	for _, how := range []string{"disabled", "tombstoned"} {
		t.Run(how, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := openRestartable(t)
			ada := testdb.RepositoryLabel(t) + ".example.com"
			r.register(ada)
			session, err := r.signIn(ada, "")
			if err != nil {
				t.Fatal(err)
			}
			d, err := r.s.Dataset(ctx, ada)
			if err != nil {
				t.Fatal(err)
			}
			ref := eref{Kind: kindActor, ID: actorID(substrate.ActorPerson)}
			if err := d.(*dataset).inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
				if how == "disabled" {
					_, err := tx.patch(ref, substrate.PatchInput{Properties: map[string]any{"disabled": true}})
					return err
				}
				_, err := tx.softDelete(ref)
				return err
			}); err != nil {
				t.Fatal(err)
			}
			repo, err := r.s.repositoryByName(ctx, ada)
			if err != nil {
				t.Fatal(err)
			}
			r.restart()
			if _, _, err := r.s.Authenticate(ctx, session.Secret); !errors.Is(err, substrate.ErrAuth) {
				t.Fatalf("the first check of the token of a person the history took out = %v, want ErrAuth", err)
			}
			if people, err := repositoryIdentities(ctx, r.s.maint, repo.ID); err != nil || len(people) != 0 {
				t.Fatalf("the index after the boot: %v (%v), want nobody", people, err)
			}
			if _, err := r.s.Dataset(ctx, ada); err != nil {
				t.Fatal(err)
			}
			if first, err := firstPersonOf(ctx, r.s.maint, repo.ID); err != nil || first.live {
				t.Fatalf("the person's actor after the open: %+v (%v), want it still out", first, err)
			}

			// Beside a running server, a reset writes no repository, and the
			// writer's open that `user reset` falls back to is refused.
			ro, err := OpenForTest(t, ctx, r.dsn, WithDataRoot(r.root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds), WithDirectoryReadOnly())
			if err != nil {
				t.Fatal(err)
			}
			_, err = ro.(*service).ResetUser(ctx, repo.ID, "an-operator-issued-passphrase", testdb.RepositoryLabel(t)+"3.example.com")
			_ = ro.Close()
			if !errors.Is(err, ErrResetNeedsTheWriter) || !errors.Is(err, substrate.ErrConflict) {
				t.Fatalf("a reset beside a running server of a repository naming no person = %v, want ErrResetNeedsTheWriter", err)
			}
			if people, err := repositoryIdentities(ctx, r.s.maint, repo.ID); err != nil || len(people) != 0 {
				t.Fatalf("the index after the refused reset: %v (%v), want nobody", people, err)
			}
			if w, err := OpenForTest(t, ctx, r.dsn, WithDataRoot(r.root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds)); !errors.Is(err, ErrRepositoryHasAnotherWriter) {
				if err == nil {
					_ = w.Close()
				}
				t.Fatalf("the writer's open beside a running server = %v, want ErrRepositoryHasAnotherWriter", err)
			}

			// With the server stopped, the reset opens as the repository's
			// writer and gives it a person with an actor of their own, the
			// old one left as the history leaves it.
			_ = r.s.Close()
			w, err := OpenForTest(t, ctx, r.dsn, WithDataRoot(r.root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds))
			if err != nil {
				t.Fatal(err)
			}
			newcomer := testdb.RepositoryLabel(t) + "2.example.com"
			enrollment, err := w.(*service).ResetUser(ctx, repo.ID, "an-operator-issued-passphrase", newcomer)
			_ = w.Close()
			if err != nil {
				t.Fatalf("reset: %v", err)
			}
			r.start()
			signIn := func(step int64) error {
				code, err := TOTPCode(enrollment.Secret, step)
				if err != nil {
					t.Fatal(err)
				}
				_, err = r.s.Login(ctx, substrate.LoginInput{Identity: newcomer, Password: "an-operator-issued-passphrase", TOTPCode: code, Label: "after"})
				return err
			}
			step := TOTPStep(time.Now())
			if err := signIn(step); err != nil {
				t.Fatalf("the person the reset gave the repository, signing in: %v", err)
			}
			person, err := r.s.identityByName(ctx, r.s.maint, newcomer)
			if err != nil {
				t.Fatal(err)
			}

			// The new person's writes link their own actor, and work that acts
			// for the person taken out is refused: neither writes the old
			// actor again.
			d, err = r.s.Dataset(ctx, ada)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := d.Put(substrate.WithIdentity(ctx, person.ID), substrate.ActorPerson, substrate.PutInput{
				Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "Ada"},
			}); err != nil {
				t.Fatalf("a write of the person the reset gave the repository: %v", err)
			}
			if _, err := d.Put(substrate.WithOnBehalfOf(ctx, substrate.ActorPerson), substrate.ActorSystem, substrate.PutInput{
				Kind: "substrate.reamde.dev/core/contact", Properties: map[string]any{"name": "Bo"},
			}); !errors.Is(err, substrate.ErrForbidden) {
				t.Fatalf("work acting for the person taken out = %v, want ErrForbidden", err)
			}
			if first, err := firstPersonOf(ctx, r.s.maint, repo.ID); err != nil || first.live {
				t.Fatalf("the old person's actor after the reset and the writes: %+v (%v), want it still out", first, err)
			}
			r.restart()
			if people, err := repositoryIdentities(ctx, r.s.maint, repo.ID); err != nil || len(people) != 1 || people[0] != person.ID {
				t.Fatalf("the index after a boot: %v (%v), want only the person the reset gave the repository", people, err)
			}
			if _, _, err := r.s.Authenticate(ctx, session.Secret); !errors.Is(err, substrate.ErrAuth) {
				t.Fatalf("the token of the person taken out, after the writes and a boot = %v, want ErrAuth", err)
			}
			if err := signIn(step + 1); err != nil {
				t.Fatalf("the reset's person after a boot: %v", err)
			}
		})
	}
}

// The index write after a person's actor does not end with the request: one
// the database refuses, with the request's context gone, is tried again
// until it lands, and the person signs in.
func TestAnAdditionWhoseIndexWriteFailsIsFinished(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := openRestartable(t)
	label := testdb.RepositoryLabel(t)
	ada, bo := label+"a.example.com", label+"b.example.com"
	r.register(ada)
	r.register(bo)
	var failed atomic.Bool
	r.s.testIndexFault = func() error {
		if failed.CompareAndSwap(false, true) {
			cancel()
			return errors.New("the database refused the write")
		}
		return nil
	}
	if err := r.s.AddPerson(ctx, ada, bo, scope.RoleAdmin); err == nil {
		t.Fatal("an addition whose index write failed answered success")
	}
	eventually(t, "the person signs in to the repository they were added to", func() bool {
		_, err := r.signIn(ada, bo)
		return err == nil
	})
}

// A registration whose client goes away between the person's actor and the
// index leaves no repository nobody can sign in to: the person's row is
// written with the repository's own, in the creation's commit, and a
// creation that cannot make it erases what it wrote.
func TestARegistrationWhoseClientLeavesBeforeItsCommitLeavesNothing(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := openRestartable(t)
	ada := testdb.Repository(t)
	r.s.testFailAfterSeed = func() error {
		cancel()
		return nil
	}
	if _, err := r.s.Register(ctx, substrate.RegisterInput{Identity: ada, Repository: ada, Password: peoplePassword, SkipTOTP: true}); err == nil {
		t.Fatal("a registration whose client left before its commit answered success")
	}
	if _, err := r.s.repositoryByName(context.Background(), ada); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the left registration's repository: %v, want none", err)
	}
	if _, err := r.s.identityByName(context.Background(), r.s.maint, ada); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the left registration's person: %v, want none", err)
	}
	r.s.testFailAfterSeed = nil
	r.register(ada)
	if _, err := r.signIn(ada, ""); err != nil {
		t.Fatalf("the name registered again, signing in: %v", err)
	}
}

// Nothing but a repository's erase takes a person's actor out while the
// server runs: the record routes, a merge, and a function's or an agent's
// effects, which write through the same guard, refuse the actor kind.
func TestNoRouteTakesAPersonsActorOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openRestartable(t)
	ada := testdb.Repository(t)
	r.register(ada)
	d, err := r.s.Dataset(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	id := actorID(substrate.ActorPerson)
	if _, err := d.Patch(ctx, substrate.ActorPerson, kindActor, id, substrate.PatchInput{Properties: map[string]any{"disabled": true}}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a patch disabling the person's actor = %v, want ErrForbidden", err)
	}
	if _, err := d.Delete(ctx, substrate.ActorPerson, kindActor, id, substrate.DeleteInput{}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a delete of the person's actor = %v, want ErrForbidden", err)
	}
	if _, err := d.Merge(ctx, substrate.ActorPerson, substrate.MergeInput{Kind: kindActor, Winner: actorID(substrate.ActorSystem), Loser: id}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("a merge taking the person's actor out = %v, want ErrForbidden", err)
	}
	if first, err := firstPersonOf(ctx, r.s.maint, d.Repository().ID); err != nil || !first.live {
		t.Fatalf("the person's actor after the refused writes: %+v (%v), want it live", first, err)
	}
}

// Work a person left running when the history took them out (a thread
// whose run a stop cut off, a background job) is settled at the next open as
// the engine's own act, for nobody: the open does not fail on it, whether a
// reset, an open or another person's sign-in reaches the repository first,
// and their actor stays out.
func TestWorkLeftByAPersonTakenOutIsSettledForNobody(t *testing.T) {
	t.Parallel()
	for _, door := range []string{"reset", "open", "another person's sign-in"} {
		t.Run(door, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := openRestartable(t)
			label := testdb.RepositoryLabel(t)
			ada, bo := label+"a.example.com", label+"b.example.com"
			r.register(ada)
			if door == "another person's sign-in" {
				r.register(bo)
				if err := r.s.AddPerson(ctx, bo, ada, scope.RoleAdmin); err != nil {
					t.Fatal(err)
				}
			}
			repo, err := r.s.repositoryByName(ctx, ada)
			if err != nil {
				t.Fatal(err)
			}
			d, err := r.s.Dataset(ctx, ada)
			if err != nil {
				t.Fatal(err)
			}
			ds := d.(*dataset)
			importVocabulary(t, ds, "tasks")
			fake := provisionAgents(t, ds)
			fake.script("stoic", fakeTurn{content: "ready"}, fakeTurn{content: "hello back"})
			thread := continuedByThePerson(t, ds, crewPackage+"/stoic", "hello")
			ag, err := ds.registry().ResolveAgent(crewPackage + "/stoic")
			if err != nil {
				t.Fatal(err)
			}
			person := substrate.WithOnBehalfOf(ctx, substrate.ActorPerson)
			// The run the person started holds the thread, and a job of theirs
			// is working, when the history takes them out.
			if err := ds.inTx(person, substrate.Actor(ag.Actor()), false, func(tx *txn) error {
				_, err := tx.patch(eref{Kind: typeThread, ID: thread}, substrate.PatchInput{Properties: map[string]any{
					"status": threadRunning, "leaseUntil": nowUTC().Add(time.Hour).Format(time.RFC3339Nano),
				}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			var job string
			if err := ds.db.QueryRowContext(ctx, `
				SELECT record_id FROM changelog WHERE kind = $1 AND (on_behalf_of = $2 OR actor = $2) ORDER BY seq LIMIT 1`,
				typeMessage, actorID(substrate.ActorPerson)).Scan(&job); err != nil {
				t.Fatalf("a message the person wrote: %v", err)
			}
			if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
				if _, err := tx.patch(eref{Kind: typeMessage, ID: job}, substrate.PatchInput{Properties: map[string]any{
					"job": jobProps(jobWorking, nowUTC(), nowUTC()),
				}}); err != nil {
					return err
				}
				_, err := tx.patch(eref{Kind: kindActor, ID: actorID(substrate.ActorPerson)}, substrate.PatchInput{Properties: map[string]any{"disabled": true}})
				return err
			}); err != nil {
				t.Fatal(err)
			}
			r.restart()

			switch door {
			case "reset":
				_ = r.s.Close()
				w, err := OpenForTest(t, ctx, r.dsn, WithDataRoot(r.root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds))
				if err != nil {
					t.Fatal(err)
				}
				_, err = w.(*service).ResetUser(ctx, repo.ID, "an-operator-issued-passphrase", label+"c.example.com")
				_ = w.Close()
				if err != nil {
					t.Fatalf("a reset over the work the person left: %v", err)
				}
				r.start()
			case "open":
				if _, err := r.s.Dataset(ctx, ada); err != nil {
					t.Fatalf("an open over the work the person left: %v", err)
				}
			default:
				if _, err := r.signIn(bo, ada); err != nil {
					t.Fatalf("another person's sign-in over the work the person left: %v", err)
				}
			}
			d, err = r.s.Dataset(ctx, ada)
			if err != nil {
				t.Fatal(err)
			}
			ds = d.(*dataset)
			lastFor := func(kind, id string) (map[string]any, string) {
				t.Helper()
				row, err := ds.loadRowDB(ctx, eref{Kind: kind, ID: id})
				if err != nil || row == nil {
					t.Fatalf("read %s %s: %v", kind, id, err)
				}
				var behalf string
				if err := ds.db.QueryRowContext(ctx, `
					SELECT on_behalf_of FROM changelog WHERE kind = $1 AND record_id = $2 ORDER BY seq DESC LIMIT 1`, kind, id).Scan(&behalf); err != nil {
					t.Fatal(err)
				}
				return row.Props, behalf
			}
			if props, behalf := lastFor(typeThread, thread); props["status"] != threadError || behalf != "" {
				t.Fatalf("the thread after the open: status %v, settled for %q; want error, for nobody", props["status"], behalf)
			}
			if props, behalf := lastFor(typeMessage, job); jobStateOf(props) != jobFailed || behalf != "" {
				t.Fatalf("the job after the open: %v, settled for %q; want failed, for nobody", props["job"], behalf)
			}
			if first, err := firstPersonOf(ctx, r.s.maint, repo.ID); err != nil || first.live {
				t.Fatalf("the person's actor after the settlement: %+v (%v), want it still out", first, err)
			}
		})
	}
}

// An addition whose actor commit is in doubt indexes the person once the
// database has proved the commit landed, and never writes the row for one
// that rolled back.
func TestAnAdditionInDoubtIsIndexedOnlyWhereItsActorLanded(t *testing.T) {
	t.Parallel()
	for _, landed := range []bool{true, false} {
		t.Run(map[bool]string{true: "landed", false: "rolled back"}[landed], func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			r := openRestartable(t)
			label := testdb.RepositoryLabel(t)
			ada, bo := label+"a.example.com", label+"b.example.com"
			r.register(ada)
			r.register(bo)
			if _, err := r.s.Dataset(ctx, ada); err != nil {
				t.Fatal(err)
			}
			decided := make(chan bool, 1)
			r.s.testPersonInDoubt = func(named bool) { decided <- named }
			var once atomic.Bool
			if landed {
				r.s.testCommitFault = func(stage string) error {
					if stage == commitInDoubt && once.CompareAndSwap(false, true) {
						return errors.New("the commit's answer was lost")
					}
					return nil
				}
			} else {
				r.s.testCommitTx = func(commit func() error) error {
					if once.CompareAndSwap(false, true) {
						return errors.New("the commit's answer was lost before it was sent")
					}
					return commit()
				}
			}
			if err := r.s.AddPerson(ctx, bo, ada, scope.RoleAdmin); !errors.Is(err, substrate.ErrWriteInDoubt) {
				t.Fatalf("an addition whose commit is in doubt = %v, want ErrWriteInDoubt", err)
			}
			select {
			case named := <-decided:
				if named != landed {
					t.Fatalf("the reconciliation found the actor named = %v, want %v", named, landed)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("the reconciliation never decided")
			}
			if landed {
				eventually(t, "the added person signs in", func() bool {
					_, err := r.signIn(bo, ada)
					return err == nil
				})
				return
			}
			repo, err := r.s.repositoryByName(ctx, ada)
			if err != nil {
				t.Fatal(err)
			}
			if people, err := repositoryIdentities(ctx, r.s.maint, repo.ID); err != nil || len(people) != 1 {
				t.Fatalf("the index after an addition that rolled back: %v (%v), want ada alone", people, err)
			}
			if _, err := r.signIn(bo, ada); err == nil {
				t.Fatal("a person whose addition rolled back signs in")
			}
		})
	}
}

// A reset cut short after the person's actor committed and before their
// identity did says to run it again, and the second run finishes under the
// identity the actor names, beside a running server.
func TestAResetCutShortAfterItsActorFinishesWhenRunAgain(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openRestartable(t)
	label := testdb.RepositoryLabel(t)
	ada := label + "a.example.com"
	r.register(ada)
	repo, err := r.s.repositoryByName(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	d, err := r.s.Dataset(ctx, ada)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.(*dataset).inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		_, err := tx.softDelete(eref{Kind: kindActor, ID: actorID(substrate.ActorPerson)})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	_ = r.s.Close()
	var once atomic.Bool
	w, err := OpenForTest(t, ctx, r.dsn, WithDataRoot(r.root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds),
		WithTestCommitFault(func(stage string) error {
			if stage == commitAfterCommit && once.CompareAndSwap(false, true) {
				return errors.New("the reset's process was stopped")
			}
			return nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	newcomer := label + "n.example.com"
	_, err = w.(*service).ResetUser(ctx, repo.ID, "an-operator-issued-passphrase", newcomer)
	if err == nil || !strings.Contains(err.Error(), "run the reset again") {
		t.Fatalf("a reset cut short after its actor = %v, want a refusal saying to run it again", err)
	}
	actors, err := personActorsOf(ctx, w.(*service).maint, repo.ID)
	_ = w.Close()
	if err != nil || len(actors) != 1 || actors[0].identity == "" {
		t.Fatalf("the person actors after the cut: %+v (%v), want one naming the new identity", actors, err)
	}
	named := actors[0].identity
	r.start()
	if n := r.identities(); n != 1 {
		t.Fatalf("identities after the cut: %d, want ada's alone", n)
	}
	ro, err := OpenForTest(t, ctx, r.dsn, WithDataRoot(r.root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds), WithDirectoryReadOnly())
	if err != nil {
		t.Fatal(err)
	}
	enrollment, err := ro.(*service).ResetUser(ctx, repo.ID, "an-operator-issued-passphrase", newcomer)
	_ = ro.Close()
	if err != nil {
		t.Fatalf("the reset run again, beside the running server: %v", err)
	}
	if got := identityIDOf(t, r.s, newcomer); got != named {
		t.Fatalf("the second run gave the repository %s, want the identity its actor names, %s", got, named)
	}
	code, err := TOTPCode(enrollment.Secret, TOTPStep(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.s.Login(ctx, substrate.LoginInput{Identity: newcomer, Password: "an-operator-issued-passphrase", TOTPCode: code, Label: "after"}); err != nil {
		t.Fatalf("the reset's person signs in: %v", err)
	}
}
