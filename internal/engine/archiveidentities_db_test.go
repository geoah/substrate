package engine_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// copyArchive copies repository id's archive from under srcRoot to dstRoot.
func copyArchive(t *testing.T, srcRoot, dstRoot, id string) {
	t.Helper()
	rel := filepath.Join("archive", changelogfile.RepositoriesDir, id)
	copyDir(t, filepath.Join(srcRoot, rel), filepath.Join(dstRoot, rel))
}

// archivedManifest is the manifest the archive under root holds for id.
func archivedManifest(t *testing.T, root, id string) changelogfile.Manifest {
	t.Helper()
	dir, _ := archivedRepoDir(root, id)
	m, _, err := changelogfile.ReadArchiveManifestDir(dir)
	if err != nil {
		t.Fatalf("the archive of %s holds no manifest: %v", id, err)
	}
	return m
}

func mustArchivePass(t *testing.T, ds substrate.Dataset) {
	t.Helper()
	if err := engine.ArchivePass(context.Background(), ds); err != nil {
		t.Fatalf("archive pass: %v", err)
	}
}

// changePassword moves u's password on, proving the factors it has.
func changePassword(t *testing.T, svc substrate.Service, u *authUser, to string) {
	t.Helper()
	if err := svc.ChangePassword(context.Background(), substrate.LoginInput{
		Identity: u.repository, Password: u.password, TOTPCode: u.code(t),
	}, to); err != nil {
		t.Fatalf("change the password of %s: %v", u.repository, err)
	}
	u.password = to
}

// signIn signs u in to repository with password, on a fresh step.
func signIn(t *testing.T, svc substrate.Service, u *authUser, repository, password string) (substrate.MintedToken, error) {
	t.Helper()
	waitStep(t)
	return svc.Login(context.Background(), substrate.LoginInput{
		Identity: u.repository, Repository: repository, Password: password, TOTPCode: u.code(t), Label: "after the restore",
	})
}

// A person in two repositories is in two manifests, taken by passes at
// different times. Restored into one database, they sign in with the factors
// they changed to last, whichever of the two the restore takes first: the
// later copy replaces the one this process's restore wrote, and an earlier
// one does not replace a later.
func TestARestoredPersonSignsInWithTheirLatestCopy(t *testing.T) {
	t.Parallel()
	for _, later := range []string{"restored first", "restored second"} {
		t.Run("the later copy "+later, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			svc, _ := newService(t, engine.WithArchiveInterval(time.Hour))
			registerUser(t, svc, "ada.example.com")
			bo, _, _ := registerUser(t, svc, "bo.example.com")
			if err := svc.(engine.Operator).AddPerson(ctx, "bo.example.com", "ada.example.com"); err != nil {
				t.Fatal(err)
			}
			ada, boRepo := mustDataset(t, svc, "ada.example.com"), mustDataset(t, svc, "bo.example.com")
			// The boot restores the repositories in the order of their ids.
			first, second := ada, boRepo
			if repositoryIDOf(t, boRepo) < repositoryIDOf(t, ada) {
				first, second = boRepo, ada
			}
			older, newer := second, first
			if later == "restored second" {
				older, newer = first, second
			}
			mustArchivePass(t, older)
			before := bo.password
			changePassword(t, svc, bo, "bo's password, changed since")
			mustArchivePass(t, newer)

			root := t.TempDir()
			for _, ds := range []substrate.Dataset{ada, boRepo} {
				copyArchive(t, engine.DataRootOf(svc), root, repositoryIDOf(t, ds))
			}
			o, n := archivedManifest(t, root, repositoryIDOf(t, older)), archivedManifest(t, root, repositoryIDOf(t, newer))
			copyOf := func(m changelogfile.Manifest) time.Time {
				for _, p := range m.Identities {
					if p.Name == "bo.example.com" {
						return p.UpdatedAt
					}
				}
				t.Fatalf("the manifest of %s carries no copy of bo: %+v", m.Authority, m.Identities)
				return time.Time{}
			}
			if !copyOf(n).After(copyOf(o)) {
				t.Fatalf("the two manifests carry bo at %s and %s: the test needs the later one in %s", copyOf(o), copyOf(n), n.Authority)
			}

			restored := reopenWith(t, engine.MigratedDSN(t), root, engine.WithArchiveInterval(time.Hour))
			if _, err := signIn(t, restored, bo, "bo.example.com", bo.password); err != nil {
				t.Fatalf("bo with the password they changed to: %v", err)
			}
			if _, err := signIn(t, restored, bo, "ada.example.com", before); !errors.Is(err, substrate.ErrAuth) {
				t.Fatalf("bo with the password they had = %v, want refused", err)
			}
		})
	}
}

// A row the database held before a restore began is the live one: a restore
// whose manifest carries a later copy of the person leaves it as it is.
func TestARestoreLeavesAPersonTheDatabaseHeld(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t, engine.WithArchiveInterval(time.Hour))
	registerUser(t, svc, "ada.example.com")
	bo, _, _ := registerUser(t, svc, "bo.example.com")
	if err := svc.(engine.Operator).AddPerson(ctx, "bo.example.com", "ada.example.com"); err != nil {
		t.Fatal(err)
	}
	ada, boRepo := mustDataset(t, svc, "ada.example.com"), mustDataset(t, svc, "bo.example.com")
	mustArchivePass(t, boRepo)
	root := t.TempDir()
	copyArchive(t, engine.DataRootOf(svc), root, repositoryIDOf(t, boRepo))
	held := bo.password
	changePassword(t, svc, bo, "bo's password, changed since")
	mustArchivePass(t, ada)

	// The first start restores bo's repository, and bo with it.
	dsn := engine.MigratedDSN(t)
	first, err := reopen(t, dsn, root)
	if err != nil {
		t.Fatalf("the first start: %v", err)
	}
	if _, err := signIn(t, first, bo, "bo.example.com", held); err != nil {
		t.Fatalf("bo after the first restore: %v", err)
	}
	_ = first.Close()
	// The second restores ada's repository, whose copy of bo is later.
	copyArchive(t, engine.DataRootOf(svc), root, repositoryIDOf(t, ada))
	second := mustReopen(t, dsn, root)
	if _, err := signIn(t, second, bo, "ada.example.com", held); err != nil {
		t.Fatalf("bo with the sign-in the database held, in the repository restored second: %v", err)
	}
	if _, err := signIn(t, second, bo, "bo.example.com", bo.password); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("bo with the restored manifest's copy = %v, want refused: the row the database held stands", err)
	}
}

// An export carries the sign-in of the person who took it and nobody else's.
// Restored under the same key, the taker signs in with the factors they had
// and their token opens the repository; another person of the repository is
// not in it, and the operator's reset, naming the repository by its id since
// its name is the taker's sign-in name, gives them back under the identity
// their actor names.
func TestAnExportCarriesThePersonWhoTookIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t, engine.WithArchiveInterval(time.Hour))
	ada, adaToken, adaSecret := registerUser(t, svc, "ada.example.com")
	bo, boToken, _ := registerUser(t, svc, "bo.example.com")
	if err := svc.(engine.Operator).AddPerson(ctx, "bo.example.com", "ada.example.com"); err != nil {
		t.Fatal(err)
	}
	ds := mustDataset(t, svc, "ada.example.com")
	id := repositoryIDOf(t, ds)
	ex, err := ds.Export(substrate.WithIdentity(ctx, adaToken.Identity))
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	var buf bytes.Buffer
	if _, err := ex.WriteTo(&buf); err != nil {
		t.Fatalf("stream the export: %v", err)
	}
	root := t.TempDir()
	extractExport(t, root, buf.Bytes())
	if m := exportManifest(t, root, id); len(m.Identities) != 1 || m.Identities[0].ID != adaToken.Identity {
		t.Fatalf("the export carries %+v, want ada alone", m.Identities)
	}

	restored := mustReopen(t, engine.MigratedDSN(t), root)
	if _, info, err := restored.Authenticate(ctx, adaSecret); err != nil || info.ID != adaToken.ID {
		t.Fatalf("the taker's token on the restore: %v (%+v)", err, info)
	}
	if _, err := signIn(t, restored, ada, "ada.example.com", ada.password); err != nil {
		t.Fatalf("the taker with the factors they had: %v", err)
	}
	if _, err := signIn(t, restored, bo, "ada.example.com", bo.password); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("bo on the restore = %v, want refused: the export is not theirs", err)
	}
	enrollment, err := restored.(engine.Operator).ResetUser(ctx, id, "bo's password after the restore", "")
	if err != nil {
		t.Fatalf("give bo back: %v", err)
	}
	bo.password, bo.seed, bo.step = "bo's password after the restore", enrollment.Secret, 0
	minted, err := signIn(t, restored, bo, "ada.example.com", bo.password)
	if err != nil {
		t.Fatalf("bo after the reset: %v", err)
	}
	if minted.Token.Identity != boToken.Identity {
		t.Fatalf("bo came back as identity %s, want %s, the one their actor names", minted.Token.Identity, boToken.Identity)
	}
}

// A restored person whose sign-in name another person here holds is left
// out, and the repository restores without them. The reset, naming the
// repository by its id, since the name is the other person's, gives them
// back under another name and the identity their tokens name.
func TestARestoredPersonWhoseNameIsTakenIsLeftOut(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t, engine.WithArchiveInterval(time.Hour))
	ada, _, secret := registerUser(t, svc, "ada.example.com")
	ds := mustDataset(t, svc, "ada.example.com")
	id := repositoryIDOf(t, ds)
	mustArchivePass(t, ds)
	root := t.TempDir()
	copyArchive(t, engine.DataRootOf(svc), root, id)

	// The other database: another person signs in as ada.example.com there.
	dsn := engine.MigratedDSN(t)
	other := mustReopen(t, dsn, t.TempDir())
	enrollment, err := other.BeginRegistration(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	stranger := &authUser{repository: "ada.example.com", password: "the stranger's password", seed: enrollment.Secret}
	if _, err := other.Register(ctx, substrate.RegisterInput{
		Identity: "ada.example.com", Repository: "stranger.example.com", Password: stranger.password,
		TOTPSecret: stranger.seed, TOTPCode: stranger.code(t), Label: "cli",
	}); err != nil {
		t.Fatal(err)
	}
	_ = other.Close()

	restored := mustReopen(t, dsn, root)
	if _, err := restored.Dataset(ctx, "ada.example.com"); err != nil {
		t.Fatalf("the repository did not restore: %v", err)
	}
	if _, err := signIn(t, restored, ada, "ada.example.com", ada.password); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("the left-out person signed in: %v", err)
	}
	if _, _, err := restored.Authenticate(ctx, secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("the left-out person's token = %v, want refused", err)
	}
	reset, err := restored.(engine.Operator).ResetUser(ctx, id, "ada's password after the restore", "ada2.example.com")
	if err != nil {
		t.Fatalf("give ada back under another name: %v", err)
	}
	ada.repository, ada.password, ada.seed, ada.step = "ada2.example.com", "ada's password after the restore", reset.Secret, 0
	if _, err := signIn(t, restored, ada, "ada.example.com", ada.password); err != nil {
		t.Fatalf("ada under the new name: %v", err)
	}
	if _, _, err := restored.Authenticate(ctx, secret); err != nil {
		t.Fatalf("ada's token after the reset: %v", err)
	}
}

// A change of a person's factors moves no line of history, and the next pass
// publishes it: a new manifest at the same head. A manifest without the
// people, as every archive written before they were carried, restores the
// repository with nobody, and the reset gives the person back.
func TestAChangeOfAPersonsFactorsIsArchived(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t, engine.WithArchiveInterval(time.Hour))
	ada, _, secret := registerUser(t, svc, "ada.example.com")
	ds := mustDataset(t, svc, "ada.example.com")
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	mustArchivePass(t, ds)
	was := archivedManifest(t, root, id)
	if len(was.Identities) != 1 || was.Identities[0].Name != "ada.example.com" {
		t.Fatalf("the manifest carries %+v, want ada", was.Identities)
	}
	mustArchivePass(t, ds)
	if idle := archivedManifest(t, root, id); idle.Epoch != was.Epoch {
		t.Fatalf("an idle pass published epoch %d over %d", idle.Epoch, was.Epoch)
	}
	changePassword(t, svc, ada, "ada's password, changed")
	mustArchivePass(t, ds)
	now := archivedManifest(t, root, id)
	if now.Epoch != was.Epoch+1 || now.Head != was.Head || !now.Identities[0].UpdatedAt.After(was.Identities[0].UpdatedAt) {
		t.Fatalf("after the change: epoch %d head %d at %s, want epoch %d head %d past %s",
			now.Epoch, now.Head, now.Identities[0].UpdatedAt, was.Epoch+1, was.Head, was.Identities[0].UpdatedAt)
	}

	copied := t.TempDir()
	copyArchive(t, root, copied, id)
	dir, _ := archivedRepoDir(copied, id)
	bare := now
	bare.Identities, bare.Epoch = nil, now.Epoch+1
	if _, err := changelogfile.WriteArchiveManifestDir(dir, bare); err != nil {
		t.Fatal(err)
	}
	restored := mustReopen(t, engine.MigratedDSN(t), copied)
	if _, _, err := restored.Authenticate(ctx, secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a token on a restore of a manifest without people = %v, want refused", err)
	}
	reset, err := restored.(engine.Operator).ResetUser(ctx, "ada.example.com", "ada's password after the restore", "")
	if err != nil {
		t.Fatalf("give ada back: %v", err)
	}
	ada.password, ada.seed, ada.step = "ada's password after the restore", reset.Secret, 0
	if _, err := signIn(t, restored, ada, "ada.example.com", ada.password); err != nil {
		t.Fatalf("ada after the reset: %v", err)
	}
	if _, _, err := restored.Authenticate(ctx, secret); err != nil {
		t.Fatalf("ada's token after the reset: %v", err)
	}
}
