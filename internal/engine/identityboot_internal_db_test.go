package engine

// A database a release before identities ran, upgraded in place (decision
// 0274). The test makes that shape by hand from a repository this binary
// created: its sign-in as a `core/credential` record holding two refs into
// its sealed store, a token naming no person, no identity, and no ledger row
// for repository migration 12. Then a boot has to give the repository its
// person, with the same password and the same authenticator, and its first
// open has to leave nothing of the sign-in in the repository.

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const preIdentityPassword = "a-password-from-before-identities"

func TestABootGivesARepositoryFromBeforeIdentitiesItsPerson(t *testing.T) {
	ctx := context.Background()
	dsn, root := MigratedDSN(t), t.TempDir()
	open := func() *service {
		t.Helper()
		svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds))
		if err != nil {
			t.Fatalf("boot: %v", err)
		}
		return svc.(*service)
	}
	s := open()
	name, info, seed, before := preIdentityRepository(t, s, dsn, testdb.Repository(t))
	_ = s.Close()

	s = open()
	t.Cleanup(func() { _ = s.Close() })
	code, err := TOTPCode(seed, TOTPStep(time.Now()))
	if err != nil {
		t.Fatal(err)
	}
	minted, err := s.Login(ctx, substrate.LoginInput{Identity: name, Password: preIdentityPassword, TOTPCode: code, Label: "after"})
	if err != nil {
		t.Fatalf("the person from before identities does not sign in with their factors: %v", err)
	}
	if minted.Repository != name || minted.Identity != name {
		t.Fatalf("the sign-in reached %q as %q, want %s as its own name", minted.Repository, minted.Identity, name)
	}

	d, err := s.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	if _, err := ds.Get(ctx, kindCredential, credentialID); err == nil {
		t.Fatal("the credential record is still served after the first open")
	}
	var sealed int
	if err := s.maint.QueryRowContext(ctx, `SELECT count(*) FROM sealed WHERE repository = $1`, info.ID).Scan(&sealed); err != nil || sealed != 0 {
		t.Fatalf("%d sealed rows (%v) left in the repository, want the sign-in's gone", sealed, err)
	}
	person, err := s.identityByName(ctx, s.maint, name)
	if err != nil {
		t.Fatal(err)
	}
	tokens, err := ds.Tokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range tokens {
		if tok.Identity != person.ID {
			t.Fatalf("token %s (%s) acts for %q, want the person %s", tok.ID, tok.Label, tok.Identity, person.ID)
		}
	}
	if len(tokens) != 2 || (tokens[0].ID != before.ID && tokens[1].ID != before.ID) {
		t.Fatalf("tokens = %+v, want the one from before and the sign-in's", tokens)
	}
}

// Every shape in which a repository from before identities reaches this
// binary keeps its person across two boots: the first gives it its person
// from its sign-in record and its first open names that person on its
// actor, so the second boot's exact derivation finds them there. A
// directory imported into another database brings its record and its lines.
func TestEveryRepositoryFromBeforeIdentitiesKeepsItsPersonAcrossTwoBoots(t *testing.T) {
	t.Parallel()
	for _, shape := range []string{"in place", "imported into another database"} {
		t.Run(shape, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			dsn, root := MigratedDSN(t), t.TempDir()
			open := func() *service {
				t.Helper()
				svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds))
				if err != nil {
					t.Fatalf("boot: %v", err)
				}
				return svc.(*service)
			}
			s := open()
			name, info, seed, _ := preIdentityRepository(t, s, dsn, testdb.Repository(t))
			_ = s.Close()
			if shape == "imported into another database" {
				dsn = MigratedDSN(t)
			}
			step := TOTPStep(time.Now())
			for boot := range 2 {
				s := open()
				code, err := TOTPCode(seed, step+int64(boot))
				if err != nil {
					t.Fatal(err)
				}
				_, err = s.Login(ctx, substrate.LoginInput{Identity: name, Password: preIdentityPassword, TOTPCode: code, Label: "after"})
				if err != nil {
					_ = s.Close()
					t.Fatalf("boot %d: the person from before identities does not sign in: %v", boot+1, err)
				}
				// The sign-in opened the repository, whose first open named
				// the person on their actor: from here it is the actor that
				// says who may open it.
				person, err := s.identityByName(ctx, s.maint, name)
				if err != nil {
					t.Fatal(err)
				}
				first, err := firstPersonOf(ctx, s.maint, info.ID)
				_ = s.Close()
				if err != nil || !first.live || first.identity != person.ID {
					t.Fatalf("boot %d: the first person's actor %+v (%v), want it naming %s", boot+1, first, err, person.ID)
				}
			}
		})
	}
}

// Each of the three facts alone says a repository is from before
// identities, and a repository created now holds none of them, so its
// derivation is exact. The record's row stands where an archive took its
// lines out of the table, which a boot here cannot show, since it imports a
// line the table lacks from the segment files.
func TestEachFactSaysARepositoryIsFromBeforeIdentities(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	r := openRestartable(t)
	s := r.s
	before := func(repository string) bool {
		t.Helper()
		got, err := fromBeforeIdentities(ctx, s.maint, repository)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	modern, err := s.CreateRepository(ctx, testdb.RepositoryLabel(t)+"m.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if before(modern.ID) {
		t.Fatal("a repository created now reads as from before identities")
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := s.maint.ExecContext(ctx, q, args...); err != nil {
			t.Fatal(err)
		}
	}
	_, record, _, _ := preIdentityRepository(t, s, r.dsn, testdb.RepositoryLabel(t)+"r.example.com")
	exec(`DELETE FROM changelog WHERE repository = $1 AND kind = $2`, record.ID, kindCredential)
	if !before(record.ID) {
		t.Fatal("a repository holding its sign-in record, its lines gone, reads as created since")
	}
	_, line, _, _ := preIdentityRepository(t, s, r.dsn, testdb.RepositoryLabel(t)+"l.example.com")
	exec(`DELETE FROM records WHERE repository = $1 AND kind = $2`, line.ID, kindCredential)
	if !before(line.ID) {
		t.Fatal("a repository whose history holds a sign-in line, its record purged, reads as created since")
	}
	vocabulary, err := s.CreateRepository(ctx, testdb.RepositoryLabel(t)+"v.example.com")
	if err != nil {
		t.Fatal(err)
	}
	exec(`UPDATE records SET props = props #- '{properties,identity}' WHERE repository = $1 AND kind = $2 AND id = $3`,
		vocabulary.ID, kindKind, kindActor)
	if !before(vocabulary.ID) {
		t.Fatal("a repository whose actor kind declares no identity reads as created since")
	}
}

// preIdentityRepository makes, in s, a repository of the shape a release
// before identities left: created by this binary, its sign-in written as a
// `core/credential` record holding two refs into its sealed store, a token
// naming no person, no identity, and repository migration 12 pending. It
// answers the name, the row, the authenticator's seed and the token.
func preIdentityRepository(t *testing.T, s *service, dsn, name string) (string, substrate.RepositoryInfo, string, substrate.TokenInfo) {
	t.Helper()
	ctx := context.Background()
	info, err := s.CreateRepository(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	before, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "minted before identities"})
	if err != nil {
		t.Fatal(err)
	}
	hash, err := hashPassword(ctx, preIdentityPassword)
	if err != nil {
		t.Fatal(err)
	}
	seed, err := NewTOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	material := map[string]struct {
		ref string
		raw []byte
	}{
		"passwordRef": {"auth:password:00112233445566778899aabbccddeeff", []byte(hash)},
		"totpRef":     {"auth:totp:00112233445566778899aabbccddeeff", mustJSON(totpMaterial{Secret: seed})},
	}
	if err := ds.inTx(ctx, substrate.ActorSystem, true, func(tx *txn) error {
		props := map[string]any{"repository": name}
		for prop, m := range material {
			ref, raw := m.ref, m.raw
			payload, err := ds.sealPayload(raw, sealedAAD(ref, kindCredential, credentialID))
			if err != nil {
				return err
			}
			if _, err := tx.exec(`INSERT INTO sealed (ref, record_kind, record_id, payload, updated_at)
				VALUES ($1, $2, $3, $4, now())`, ref, kindCredential, credentialID, payload); err != nil {
				return err
			}
			props[prop] = ref
		}
		if _, err := tx.foldRow(nil, &erow{ID: credentialID, Kind: kindCredential, Props: props, KindVersion: 1}, false, false); err != nil {
			return err
		}
		return tx.appendChange(substrate.ActorSystem, substrate.OpPut, credentialID, kindCredential, map[string]any{"created": true})
	}); err != nil {
		t.Fatalf("write the sign-in as a record: %v", err)
	}
	MakeMigrationPending(t, dsn, info.ID, 12)

	if people, err := repositoryIdentities(ctx, s.maint, info.ID); err != nil || len(people) != 0 {
		t.Fatalf("the repository made by hand has people %v (%v), want none", people, err)
	}
	return name, info, seed, before
}

// openForPeople boots the substrate at dsn and root for the people pass's
// tests, with the given options beside the usual ones.
func openForPeople(t *testing.T, dsn, root string, opts ...Option) *service {
	t.Helper()
	svc, err := OpenForTest(t, context.Background(), dsn, append([]Option{
		WithDataRoot(root), WithCredentialKey(TestCredentialKey), WithKindsFS(SeedKinds),
	}, opts...)...)
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	return svc.(*service)
}

// heldIdentityNamed reports whether this server holds an identity named name.
func heldIdentityNamed(t *testing.T, s *service, name string) bool {
	t.Helper()
	_, err := s.identityByName(context.Background(), s.maint, name)
	if err != nil && !errors.Is(err, substrate.ErrNotFound) {
		t.Fatal(err)
	}
	return err == nil
}

// The people pass goes through the start's one gate. A repository set aside
// at the boot check, with a sign-in record that opens, is left as it is: no
// identity is made from a fold nothing checked. The whole one beside it gets
// its person, and the start serves.
func TestThePeoplePassLeavesASetAsideRepositoryAsItIs(t *testing.T) {
	ctx := context.Background()
	dsn, root := MigratedDSN(t), t.TempDir()
	s := openForPeople(t, dsn, root)
	whole, _, _, _ := preIdentityRepository(t, s, dsn, testdb.RepositoryLabel(t)+"w.example.com")
	aside, info, _, _ := preIdentityRepository(t, s, dsn, testdb.RepositoryLabel(t)+"a.example.com")
	_ = s.Close()
	// An import a start began, with nothing to finish it from: the boot
	// check sets the repository aside.
	raw, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = raw.Close() }()
	if _, err := raw.ExecContext(ctx, `
		INSERT INTO import_progress (repository, file_head)
		SELECT $1, coalesce(max(seq), 0) + 1 FROM changelog WHERE repository = $1
		ON CONFLICT (repository) DO UPDATE SET file_head = EXCLUDED.file_head`, info.ID); err != nil {
		t.Fatal(err)
	}

	s = openForPeople(t, dsn, root)
	t.Cleanup(func() { _ = s.Close() })
	if r := s.setAsideOf(info.ID, ""); r == nil || r.stage != setAsideAtCheck {
		t.Fatalf("the repository with an unfinished import: %+v, want it set aside at %q", r, setAsideAtCheck)
	}
	if !heldIdentityNamed(t, s, whole) {
		t.Error("the whole repository beside it got no person")
	}
	if heldIdentityNamed(t, s, aside) {
		t.Error("the set-aside repository's sign-in record became an identity: the pass read a fold the start did not check")
	}
	if people, err := repositoryIdentities(ctx, s.maint, info.ID); err != nil || len(people) != 0 {
		t.Errorf("the set-aside repository's index holds %v (%v), want it as it was", people, err)
	}
}

// One repository's failure in the people pass sets that one aside, at its
// own stage, and the start serves the others; the next start runs the pass
// again and gives it its person.
func TestAFailedPeoplePassSetsItsRepositoryAside(t *testing.T) {
	ctx := context.Background()
	dsn, root := MigratedDSN(t), t.TempDir()
	s := openForPeople(t, dsn, root)
	whole, _, _, _ := preIdentityRepository(t, s, dsn, testdb.RepositoryLabel(t)+"w.example.com")
	failing, info, _, _ := preIdentityRepository(t, s, dsn, testdb.RepositoryLabel(t)+"f.example.com")
	_ = s.Close()

	s = openForPeople(t, dsn, root, WithTestPeopleFault(func(repository string) error {
		if repository == info.ID {
			return errors.New("the people pass fails here")
		}
		return nil
	}))
	if r := s.setAsideOf(info.ID, ""); r == nil || r.stage != setAsideAtPeople {
		t.Fatalf("the repository whose pass failed: %+v, want it set aside at %q", r, setAsideAtPeople)
	}
	if !heldIdentityNamed(t, s, whole) {
		t.Error("the whole repository beside it got no person")
	}
	if heldIdentityNamed(t, s, failing) {
		t.Error("the repository whose pass failed got an identity anyway")
	}
	_ = s.Close()

	s = openForPeople(t, dsn, root)
	t.Cleanup(func() { _ = s.Close() })
	if r := s.setAsideOf(info.ID, ""); r != nil {
		t.Fatalf("the next start set it aside again: %+v", r)
	}
	if !heldIdentityNamed(t, s, failing) {
		t.Error("the next start did not give the repository its person")
	}
	if _, err := s.Dataset(ctx, failing); err != nil {
		t.Errorf("the repository after the next start: %v", err)
	}
}

// A token from before identities whose first request is an export, with no
// sign-in and no other call before it: the open that request makes names the
// token's person, the request acts for that person, and the export carries
// them, so it restores with a sign-in.
func TestAnOldTokensFirstRequestActsForItsPerson(t *testing.T) {
	ctx := context.Background()
	dsn, root := MigratedDSN(t), t.TempDir()
	s := openForPeople(t, dsn, root)
	name, _, _, _ := preIdentityRepository(t, s, dsn, testdb.Repository(t))
	d, err := s.Dataset(ctx, name)
	if err != nil {
		t.Fatal(err)
	}
	old, secret, err := d.(*dataset).MintToken(ctx, substrate.MintTokenInput{Label: "an export's, from before identities"})
	if err != nil || old.Identity != "" {
		t.Fatalf("mint a token from before identities: %+v (%v), want one naming nobody", old, err)
	}
	_ = s.Close()

	s = openForPeople(t, dsn, root)
	t.Cleanup(func() { _ = s.Close() })
	person, err := s.identityByName(ctx, s.maint, name)
	if err != nil {
		t.Fatalf("the boot gave the repository no person: %v", err)
	}
	ds, info, err := s.Authenticate(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	if info.Identity != person.ID {
		t.Fatalf("the token's first request acts for %q, want its person %s", info.Identity, person.ID)
	}
	ex, err := ds.Export(substrate.WithIdentity(ctx, info.Identity))
	if err != nil {
		t.Fatal(err)
	}
	defer ex.Close()
	if got := ex.(*export).manifest.Identities; len(got) != 1 || got[0].ID != person.ID {
		t.Fatalf("the export carries %+v, want the token's person", got)
	}
}
