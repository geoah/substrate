package engine_test

// The restore through the recovery key: a repository directory copied to a
// host with a different SUBSTRATE_CREDENTIAL_KEY is refused by the boot import
// until RewrapRepositoryDir has opened its DEK with the user's age identity
// and rewritten the manifest under the new key. Afterwards the import runs,
// the user logs in, and the sealed store and the blobs read back.

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

// registerWithIdentity walks the registration flow with a client-minted age
// identity, the way substratectl register does, and returns the user and the
// identity string the substrate never sees.
func registerWithIdentity(t *testing.T, svc substrate.Service, username string) (*authUser, string) {
	t.Helper()
	ctx := context.Background()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	enrollment, err := svc.BeginRegistration(ctx, username)
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	u := &authUser{username: username, password: testPassword, seed: enrollment.Secret}
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Username: username, Password: u.password,
		TOTPSecret: u.seed, TOTPCode: u.code(t), Label: "cli",
		Authority:         username + ".example.com",
		RecoveryPublicKey: id.Recipient().String(),
	}); err != nil {
		t.Fatalf("register %q: %v", username, err)
	}
	return u, id.String()
}

// otherCredentialKey mints a second conforming credential key, decoded and
// encoded: the key of the host the directory is restored on.
func otherCredentialKey(t *testing.T) ([]byte, string) {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return raw, base64.StdEncoding.EncodeToString(raw)
}

// openWithKey opens a service over dsn and root under an explicit credential
// key, the shape of the restoring host's boot.
func openWithKey(t *testing.T, dsn, root, key string) (substrate.Service, error) {
	t.Helper()
	svc, err := engine.OpenForTest(t, context.Background(), dsn,
		engine.WithKindsDir(engine.CoreKindsDir),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(key))
	if err == nil {
		t.Cleanup(func() { _ = svc.Close() })
	}
	return svc, err
}

func TestRewrapRestoresARepositoryUnderANewCredentialKey(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	u, identity := registerWithIdentity(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	writeSomeHistory(t, ds)
	ref := putProvider(t, ds, dsn, "openai", "sk-rewrapped")
	digest := putBlob(t, ds, []byte("bytes behind the rewrap"))
	before := foldOf(t, ds)
	head := maxSeq(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	otherRaw, other := otherCredentialKey(t)
	dsn2 := engine.MigratedDSN(t)

	// Without the rewrap the copy is inert on this host: the import refuses.
	if _, err := openWithKey(t, dsn2, root2, other); err == nil {
		t.Fatal("the directory imported under a key it was not written under")
	}

	// A wrong identity is refused before the manifest changes.
	manifestBefore, err := os.ReadFile(filepath.Join(dir2, changelogfile.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	wrong, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RewrapRepositoryDir(dir2, wrong.String(), other); err == nil {
		t.Fatal("a wrong identity rewrapped the directory")
	} else if !strings.Contains(err.Error(), id) {
		t.Fatalf("the refusal must name the repository: %v", err)
	}
	if after, _ := os.ReadFile(filepath.Join(dir2, changelogfile.ManifestName)); string(after) != string(manifestBefore) {
		t.Fatal("a refused rewrap changed the manifest")
	}

	// A copy that lost sealed/ is refused by name: the recovered DEK has
	// nothing to prove itself against, and the login credential is gone.
	root3 := copyRepositoryDir(t, root, id)
	dir3, err := changelogfile.RepoDir(root3, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(changelogfile.SealedDir(dir3)); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.RewrapRepositoryDir(dir3, identity, other); err == nil || !strings.Contains(err.Error(), "sealed/") {
		t.Fatalf("a copy with no sealed files was not refused by name: %v", err)
	}

	report, err := engine.RewrapRepositoryDir(dir2, identity, other)
	if err != nil {
		t.Fatalf("rewrap: %v", err)
	}
	if report.Repository != id || report.Username != "ada" || report.SealedFiles == 0 || report.RecoveryKeySeq == 0 {
		t.Fatalf("report = %+v", report)
	}
	// The manifest now opens under the new key and no longer under the old.
	m, err := changelogfile.ReadManifest(dir2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.OpenPayloadWithKey(otherRaw, m.DEK, engine.DEKAAD(id)); err != nil {
		t.Fatalf("the rewritten manifest does not open under the new key: %v", err)
	}
	if _, err := engine.OpenPayloadWithKey(engine.TestCredentialKeyBytes, m.DEK, engine.DEKAAD(id)); err == nil {
		t.Fatal("the rewritten manifest still opens under the old key")
	}

	// The boot on the new host imports the directory, and the user is back:
	// the fold, the secret, the blob, the files verify, and then a login
	// (the password hash and the TOTP seed are sealed rows). The login goes
	// last because it mints a token record, which moves the fold and the head.
	svc2, err := openWithKey(t, dsn2, root2, other)
	if err != nil {
		t.Fatalf("boot on the rewrapped directory: %v", err)
	}
	ds2, err := svc2.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the restored fold is not the original\n%s", firstDifference(before, after))
	}
	if got := openSecretUnder(t, dsn2, ref, otherRaw); got != "sk-rewrapped" {
		t.Fatalf("secret = %q", got)
	}
	if got := getBlob(t, ds2, digest); string(got) != "bytes behind the rewrap" {
		t.Fatalf("blob bytes = %q", got)
	}
	report2 := mustVerify(t, svc2, "ada")
	if !report2.OK || report2.Head != head || report2.FileHead != head {
		t.Fatalf("the restored repository does not verify: %+v", report2)
	}
	if _, _, err := svc2.Login(ctx, substrate.LoginInput{
		Username: "ada", Password: u.password, TOTPCode: u.code(t), Label: "after",
	}); err != nil {
		t.Fatalf("login after the rewrap: %v", err)
	}
}

// A directory a v0.47.0 through v0.51.0 binary wrote carries a format-1
// manifest. The rewrap reads it, writes the manifest back in format 2 with
// the vocabulary dialect that format implies and the changelog dialect it
// recorded, and the boot on the new host imports it.
func TestRewrapOpensAFormatOneDirectory(t *testing.T) {
	t.Parallel()
	svc, _ := newService(t)
	ctx := context.Background()
	u, identity := registerWithIdentity(t, svc, "ada")
	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	writeSomeHistory(t, ds)
	before := foldOf(t, ds)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	rewriteChangelogDir(t, changelogfile.ChangelogDir(dir2), unframe)
	writeFormatOneManifest(t, dir2, readManifest(t, dir2))
	otherRaw, other := otherCredentialKey(t)

	report, err := engine.RewrapRepositoryDir(dir2, identity, other)
	if err != nil {
		t.Fatalf("rewrap a format-1 directory: %v", err)
	}
	if report.Repository != id || report.Username != "ada" {
		t.Fatalf("report = %+v", report)
	}
	m := readManifest(t, dir2)
	if m.Format != changelogfile.ManifestFormat || m.ChangelogDialect != 2 || m.VocabularyDialect != formatOneVocabularyDialect {
		t.Fatalf("the rewritten manifest = %+v, want format %d, changelog dialect 2, vocabulary dialect %d", m, changelogfile.ManifestFormat, formatOneVocabularyDialect)
	}
	if _, err := engine.OpenPayloadWithKey(otherRaw, m.DEK, engine.DEKAAD(id)); err != nil {
		t.Fatalf("the rewritten manifest does not open under the new key: %v", err)
	}

	dsn2 := engine.MigratedDSN(t)
	svc2, err := openWithKey(t, dsn2, root2, other)
	if err != nil {
		t.Fatalf("boot on the rewrapped format-1 directory: %v", err)
	}
	ds2, err := svc2.Dataset(ctx, "ada")
	if err != nil {
		t.Fatal(err)
	}
	if after := foldOf(t, ds2); string(after) != string(before) {
		t.Fatalf("the restored fold is not the original\n%s", firstDifference(before, after))
	}
	if _, _, err := svc2.Login(ctx, substrate.LoginInput{
		Username: "ada", Password: u.password, TOTPCode: u.code(t), Label: "after",
	}); err != nil {
		t.Fatalf("login after the rewrap: %v", err)
	}
}

// A repository that never enrolled a recovery key has no recoverykey record,
// so no identity opens it: the rewrap refuses and says so. A directory with
// no manifest is refused too, because nothing says whose it is. Neither is
// synthesized.
func TestRewrapRefusesWithoutARecoveryKeyOrAManifest(t *testing.T) {
	t.Parallel()
	svc, ds, _ := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "no recovery"}})
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	_, other := otherCredentialKey(t)
	someone, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	root2 := copyRepositoryDir(t, root, id)
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.RewrapRepositoryDir(dir2, someone.String(), other)
	if !errors.Is(err, engine.ErrNoRecoveryKey) {
		t.Fatalf("a repository with no recoverykey record rewrapped: %v", err)
	}

	if err := os.Remove(filepath.Join(dir2, changelogfile.ManifestName)); err != nil {
		t.Fatal(err)
	}
	_, err = engine.RewrapRepositoryDir(dir2, someone.String(), other)
	if err == nil || !strings.Contains(err.Error(), changelogfile.ManifestName) {
		t.Fatalf("a directory with no manifest was not refused by name: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir2, changelogfile.ManifestName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the rewrap synthesized a manifest")
	}

	// A key that is not key material is refused before anything is read.
	if _, err := engine.RewrapRepositoryDir(dir2, someone.String(), "passphrase"); err == nil {
		t.Fatal("a passphrase was accepted as the new credential key")
	}
}
