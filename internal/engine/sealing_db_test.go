package engine_test

// The sealed store, from the outside: the secret gate, the recovery import,
// the rewrap onto a new credential key, and the history-generation rotate.
//
// The secret gate: a secret-typed property's stored value is a
// ref into the sealed store, every kind alike, and the material never
// touches the records fold or the append-only changelog. Alongside: the
// change feed redacts what it cannot know, a re-pasted secret is a no-op
// that neither mints a delta nor steals attribution, rotation erases the old
// material, a pasted ref-shaped string is material like any other, and
// `digest` redacts without indirection.

import (
	"context"
	"crypto/rand"
	"database/sql"
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
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	sgProviderKind = "substrate.reamde.dev/core/llmprovider"
	sgPlainKey     = "sk-plain-12345"
)

// newSealingDataset provisions a keyed repository plus a raw *sql.DB on the
// same schema, so a test can read what the database actually stores.
func newSealingDataset(t *testing.T) (substrate.Service, substrate.Dataset, *sql.DB) {
	t.Helper()
	svc, ds, db, _ := newSealingDatasetDSN(t)
	return svc, ds, db
}

// newSealingDatasetDSN is newSealingDataset with the DSN kept, for the tests
// that reopen the same store under a fresh service.
func newSealingDatasetDSN(t *testing.T) (substrate.Service, substrate.Dataset, *sql.DB, string) {
	t.Helper()
	svc, dsn := newService(t, engine.WithCredentialKey(engine.TestCredentialKey))
	ctx := context.Background()
	if _, err := svc.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatalf("create repository: %v", err)
	}
	ds, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return svc, ds, db, dsn
}

func sgPutProvider(t *testing.T, ds substrate.Dataset, key string) *substrate.Record {
	t.Helper()
	return mustPut(t, ds, owner, substrate.PutInput{
		Kind: sgProviderKind, ID: "prov",
		Properties: map[string]any{
			"label": "prov", "wire": "openai",
			"baseURL": "https://llm.example.com/v1", "apiKey": key,
		},
	})
}

// storedAPIKeyRef reads the raw stored value, bypassing every redaction.
func storedAPIKeyRef(t *testing.T, db *sql.DB) string {
	t.Helper()
	var v string
	if err := db.QueryRow(`SELECT props->>'apiKey' FROM records WHERE kind = $1 AND id = 'prov'`,
		sgProviderKind).Scan(&v); err != nil {
		t.Fatalf("read stored apiKey: %v", err)
	}
	return v
}

// sealedRowsOf counts the sealed-store rows a record owns.
func sealedRowsOf(t *testing.T, db *sql.DB, kind, id string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM sealed WHERE record_kind = $1 AND record_id = $2`,
		kind, id).Scan(&n); err != nil {
		t.Fatalf("count sealed rows: %v", err)
	}
	return n
}

// mustHaveNoPlaintext asserts a value appears nowhere in records or changelog.
func mustHaveNoPlaintext(t *testing.T, db *sql.DB, value string) {
	t.Helper()
	for _, q := range []string{
		`SELECT count(*) FROM records WHERE props::text LIKE '%' || $1 || '%'`,
		`SELECT count(*) FROM changelog WHERE payload::text LIKE '%' || $1 || '%'`,
	} {
		var n int
		if err := db.QueryRow(q, value).Scan(&n); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if n != 0 {
			t.Fatalf("plaintext %q survives: %s", value, q)
		}
	}
}

func TestSecretMovesIntoTheStore(t *testing.T) {
	t.Parallel()
	_, ds, db := newSealingDataset(t)
	rec := sgPutProvider(t, ds, sgPlainKey)

	// The wire shows the sentinel, never the value or the ref.
	if got := rec.Properties["apiKey"]; got != "<redacted>" {
		t.Fatalf("put returned the apiKey unredacted: %v", got)
	}
	// The records fold holds a ref; the material sits sealed in the store.
	ref := storedAPIKeyRef(t, db)
	if !strings.HasPrefix(ref, "secret:") {
		t.Fatalf("stored apiKey is not a sealed-store ref: %q", ref)
	}
	var payload []byte
	if err := db.QueryRow(`SELECT payload FROM sealed WHERE ref = $1`, ref).Scan(&payload); err != nil {
		t.Fatalf("the ref has no sealed row: %v", err)
	}
	if len(payload) == 0 {
		t.Fatal("sealed payload is empty")
	}
	// The store binds a payload to its row under the 'a' framing (ADR 0023);
	// 'p' would be plaintext.
	if payload[0] != 'a' {
		t.Fatalf("sealed payload is not bound-sealed (marker %q)", payload[0])
	}
	// Neither the fold nor the append-only log ever held the material.
	mustHaveNoPlaintext(t, db, sgPlainKey)
}

func TestSecretRepasteIsANoOp(t *testing.T) {
	t.Parallel()
	_, ds, db := newSealingDataset(t)
	first := sgPutProvider(t, ds, sgPlainKey)
	refOnce := storedAPIKeyRef(t, db)

	// The same document again: the plaintext matches the stored material, so
	// the ref stays, no delta is minted, and no attribution moves.
	second := sgPutProvider(t, ds, sgPlainKey)
	if second.Version != first.Version {
		t.Fatalf("re-pasting the same secret bumped the version: %d -> %d", first.Version, second.Version)
	}
	if again := storedAPIKeyRef(t, db); again != refOnce {
		t.Fatal("re-pasting the same secret re-stored the material")
	}
	// A rotation lands, and the OLD material is erased, not retired.
	third := sgPutProvider(t, ds, "sk-rotated-67890")
	if third.Version == first.Version {
		t.Fatal("a rotated secret did not bump the version")
	}
	if after := storedAPIKeyRef(t, db); after == refOnce {
		t.Fatal("rotation kept the old ref")
	}
	if n := sealedRowsOf(t, db, sgProviderKind, "prov"); n != 1 {
		t.Fatalf("rotation left %d sealed rows, want 1 (the old material erased)", n)
	}
	mustHaveNoPlaintext(t, db, sgPlainKey)
}

func TestPastedRefShapedStringIsMaterial(t *testing.T) {
	t.Parallel()
	_, ds, db := newSealingDataset(t)
	// A writer pastes something shaped like a ref. It names no sealed row of
	// this record, so it is material like any other paste: stored under a
	// FRESH ref, never trusted as an address.
	const pasted = "secret:00000000000000000000000000000000"
	sgPutProvider(t, ds, pasted)

	ref := storedAPIKeyRef(t, db)
	if ref == pasted {
		t.Fatal("a pasted ref-shaped string was stored as an address")
	}
	var payload []byte
	if err := db.QueryRow(`SELECT payload FROM sealed WHERE ref = $1`, ref).Scan(&payload); err != nil {
		t.Fatalf("the fresh ref has no sealed row: %v", err)
	}
}

func TestDigestRedactsWithoutIndirection(t *testing.T) {
	t.Parallel()
	_, ds, db := newSealingDataset(t)
	const pkg = "digests.example.substrate.reamde.dev/digests"
	docs := []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg,
			map[string]any{"singular": "artifact"},
			map[string]any{"properties": map[string]any{
				"name":        map[string]any{"type": "string"},
				"fingerprint": map[string]any{"type": "digest"},
			}}),
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), owner, docs); err != nil {
		t.Fatalf("declare digest kind: %v", err)
	}

	// Not a SHA-256: refused at coercion.
	_, err := ds.Put(context.Background(), owner, substrate.PutInput{
		Kind: pkg + "/artifact", ID: "a1",
		Properties: map[string]any{"fingerprint": "not-a-digest"},
	})
	wantErr(t, err, substrate.ErrValidation, "malformed digest")

	sum := strings.Repeat("ab", 32)
	rec := mustPut(t, ds, owner, substrate.PutInput{
		Kind: pkg + "/artifact", ID: "a1",
		Properties: map[string]any{"name": "one", "fingerprint": sum},
	})
	if got := rec.Properties["fingerprint"]; got != "<redacted>" {
		t.Fatalf("digest not redacted on the wire: %v", got)
	}
	// Stored as the value itself: the engine compares digests in SQL.
	var stored string
	if err := db.QueryRow(`SELECT props->>'fingerprint' FROM records WHERE kind = $1 AND id = 'a1'`,
		pkg+"/artifact").Scan(&stored); err != nil {
		t.Fatalf("read stored fingerprint: %v", err)
	}
	if stored != sum {
		t.Fatalf("digest was rewritten at rest: %q", stored)
	}
}

func TestDisplayTemplateRefusesSensitiveProps(t *testing.T) {
	t.Parallel()
	_, ds, _ := newSealingDataset(t)
	const pkg = "leaky.example.substrate.reamde.dev/leaky"
	docs := []map[string]any{
		vocabulary.PackageManifest(pkg, 0),
		vocabulary.KindManifest(pkg,
			map[string]any{"singular": "leak"},
			map[string]any{
				"displayTemplate": "{apiKey}",
				"properties": map[string]any{
					"apiKey": map[string]any{"type": "secret"},
				},
			}),
	}
	_, err := ds.ApplyVocabularyDocuments(context.Background(), owner, docs)
	wantErr(t, err, substrate.ErrValidation, "secret in a display template")
}

// The clientSecret never sits in the record's JSONB: the stored value is a
// sealed-store ref, and a raw database read shows no plaintext.
func TestOAuthClientSecretIsSealedAtRest(t *testing.T) {
	t.Parallel()
	_, _, db, _, _ := installBarrierOAuthBundle(t)
	var props string
	if err := db.QueryRow(
		`SELECT props::text FROM records WHERE kind = $1 AND deleted_at IS NULL`, mbConfigType).
		Scan(&props); err != nil {
		t.Fatalf("read config props: %v", err)
	}
	if strings.Contains(props, "s3cret") {
		t.Fatalf("the client secret is plaintext at rest: %s", props)
	}
	if !strings.Contains(props, `"clientSecret": "secret:`) &&
		!strings.Contains(props, `"clientSecret":"secret:`) {
		t.Fatalf("the client secret is not a sealed-store ref: %s", props)
	}
}

// A copy whose sealed file does not open under the DEK its manifest carries
// is refused by file name, before any row is created.
// The import proves the manifest's DEK opens every sealed file before it
// creates a row (decision 0059): a copy holding a payload under another key
// is refused by file name, a manifest with no DEK at all is refused before
// that, and the database is left without the row either way. A row whose
// wrap has gone missing is refused where a dataset opens it.
func TestImportRefusesADirectoryWhoseSealedFilesDoNotOpen(t *testing.T) {
	t.Parallel()
	svc, ds, dsn := newDatasetWithDSN(t)
	const kind = "substrate.reamde.dev/core/llmprovider"
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: kind, ID: "prov",
		Properties: map[string]any{
			"label": "prov", "wire": "openai",
			"baseURL": "https://llm.example.com/v1", "apiKey": "sk-marked",
		},
	})
	ref := secretRefOf(t, dsn, kind, "prov", "apiKey")
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	if m, err := changelogfile.ReadManifest(dir2); err != nil || len(m.DEK) == 0 {
		t.Fatalf("the copied manifest carries no DEK: %+v, %v", m, err)
	}
	// The secret's file, rewritten under the host key: a payload the DEK the
	// manifest carries cannot open.
	files, err := changelogfile.ReadSealed(dir2)
	if err != nil {
		t.Fatal(err)
	}
	var rec changelogfile.SealedRecord
	for _, f := range files {
		if f.Ref == ref {
			rec = f
		}
	}
	if rec.Ref == "" {
		t.Fatalf("no sealed file for %s in the copy", ref)
	}
	if rec.Payload, err = engine.SealWithKey(engine.TestCredentialKeyBytes, []byte("sk-marked"),
		engine.SealedAAD(ref, rec.RecordKind, rec.RecordID)); err != nil {
		t.Fatal(err)
	}
	if err := changelogfile.WriteSealed(dir2, rec); err != nil {
		t.Fatal(err)
	}

	dsn2 := engine.MigratedDSN(t)
	_, err = openWithKey(t, dsn2, root2, engine.TestCredentialKey)
	if err == nil {
		t.Fatal("a directory whose file does not open under the DEK imported")
	}
	for _, want := range []string{id, "does not open", changelogfile.SealedFileName(ref)} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
	var rows int
	if err := rawDB(t, dsn2).QueryRow(`SELECT count(*) FROM repositories`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("the refused import left %d row(s)", rows)
	}
}

// A manifest with no wrapped DEK describes no repository this binary can
// open: every payload of a repository seals under its DEK, so the import
// refuses before it creates the row rather than standing up a repository
// nothing can read.
func TestImportRefusesAManifestWithNoDEK(t *testing.T) {
	t.Parallel()
	svc, ds, _ := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskKind, Properties: map[string]any{"name": "written before the wrap went missing"},
	})
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	root2 := copyRepositoryDir(t, root, id)
	dir2, err := changelogfile.RepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	m, err := changelogfile.ReadManifest(dir2)
	if err != nil {
		t.Fatal(err)
	}
	m.DEK, m.DEKKeyID = nil, ""
	if err := changelogfile.WriteManifest(dir2, m); err != nil {
		t.Fatal(err)
	}

	dsn2 := engine.MigratedDSN(t)
	_, err = openWithKey(t, dsn2, root2, engine.TestCredentialKey)
	if err == nil {
		t.Fatal("a directory whose manifest carries no DEK imported")
	}
	for _, want := range []string{id, "carries no wrapped DEK", changelogfile.ManifestName} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
	var rows int
	if err := rawDB(t, dsn2).QueryRow(`SELECT count(*) FROM repositories`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("the refused import left %d row(s)", rows)
	}
}

// A `repositories` row whose wrap has gone missing is damage: nothing opens
// its sealed store, so the dataset open refuses by name instead of serving a
// repository whose credential and secrets cannot be read.
func TestOpenRefusesARowWithNoWrappedDEK(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	id := repositoryIDOf(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()

	// Only the control plane can clear it; no code path does, which is why the
	// row is damage and the refusal is what the open owes the operator.
	if _, err := rawDB(t, dsn).Exec(`UPDATE repositories SET dek = NULL, dek_key_id = NULL WHERE id = $1`, id); err != nil {
		t.Fatalf("clear the wrap: %v", err)
	}

	svc2 := mustReopen(t, dsn, root)
	_, err := svc2.Dataset(ctx, id)
	if err == nil {
		t.Fatal("a repository with no wrapped DEK opened")
	}
	for _, want := range []string{id, "holds no wrapped DEK", "sealed store"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("the refusal does not name %q: %v", want, err)
		}
	}
}

// registerWithIdentity walks the registration flow with a client-minted age
// identity, the way substratectl register does, and returns the user and the
// identity string the substrate never sees.
func registerWithIdentity(t *testing.T, svc substrate.Service, repository string) (*authUser, string) {
	t.Helper()
	ctx := context.Background()
	id, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	enrollment, err := svc.BeginRegistration(ctx, repository)
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	u := &authUser{repository: repository, password: testPassword, seed: enrollment.Secret}
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: repository, Password: u.password,
		TOTPSecret: u.seed, TOTPCode: u.code(t), Label: "cli",
		RecoveryPublicKey: id.Recipient().String(),
	}); err != nil {
		t.Fatalf("register %q: %v", repository, err)
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

// The restore through the recovery key: a repository directory copied to a
// host with a different SUBSTRATE_CREDENTIAL_KEY is refused by the boot import
// until RewrapRepositoryDir has opened its DEK with the user's age identity
// and rewritten the manifest under the new key. Afterwards the import runs,
// the user logs in, and the sealed store and the blobs read back.
func TestRewrapRestoresARepositoryUnderANewCredentialKey(t *testing.T) {
	t.Parallel()
	svc, dsn := newService(t)
	ctx := context.Background()
	u, identity := registerWithIdentity(t, svc, "ada.example.com")
	ds, err := svc.Dataset(ctx, "ada.example.com")
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
	if report.Repository != id || report.SealedFiles == 0 || report.RecoveryKeySeq == 0 {
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
	ds2, err := svc2.Dataset(ctx, "ada.example.com")
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
	report2 := mustVerify(t, svc2, "ada.example.com")
	if !report2.OK || report2.Head != head || report2.FileHead != head {
		t.Fatalf("the restored repository does not verify: %+v", report2)
	}
	if _, _, err := svc2.Login(ctx, substrate.LoginInput{
		Repository: "ada.example.com", Password: u.password, TOTPCode: u.code(t), Label: "after",
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

// A database restored from a dump keeps the row and its generation, so the
// operator rotates by hand (decision 0056): the new generation is on the row,
// a restart reads it back, and a cursor saved under the old one no longer
// matches the head clients are held to.
func TestRotateHistoryGenerationHoldsTheHeadAndSurvivesARestart(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds, dsn := newDatasetWithDSN(t)
	mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "before the rotation"}})
	before, err := ds.Head(ctx)
	if err != nil {
		t.Fatal(err)
	}
	root := engine.DataRootOf(svc)

	report, err := svc.(engine.GenerationRotator).RotateHistoryGeneration(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if report.Previous != before.Generation || report.Generation == "" || report.Generation == before.Generation {
		t.Fatalf("report = %+v, want a new generation replacing %q", report, before.Generation)
	}
	// The open dataset serves the new generation at once, at the same head:
	// a rotation rewrites no history.
	after, err := ds.Head(ctx)
	if err != nil || after.Generation != report.Generation || after.Seq != before.Seq {
		t.Fatalf("head after the rotation = %+v (%v), want %q at seq %d", after, err, report.Generation, before.Seq)
	}

	_ = svc.Close()
	svc2 := mustReopen(t, dsn, root)
	ds2, err := svc2.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	if restarted, err := ds2.Head(ctx); err != nil || restarted.Generation != report.Generation {
		t.Fatalf("head after a restart = %+v (%v), want the rotated generation %q", restarted, err, report.Generation)
	}
}

// A running server holds the directory lock and has the old generation
// cached, so a second process refuses to rotate under it, naming the lock.
func TestRotateHistoryGenerationRefusesWhileTheServerHoldsTheLock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, _, dsn := newDatasetWithDSN(t)
	second, err := reopen(t, dsn, engine.DataRootOf(svc))
	if err != nil {
		t.Fatalf("a second process could not boot beside the server: %v", err)
	}
	_, err = second.(engine.GenerationRotator).RotateHistoryGeneration(ctx, testdb.Repository(t))
	if err == nil {
		t.Fatal("a rotation landed beside a running server")
	}
	if !errors.Is(err, engine.ErrChangelogLocked) || !errors.Is(err, changelogfile.ErrLocked) {
		t.Fatalf("the refusal must be the lock's: %v", err)
	}
}
