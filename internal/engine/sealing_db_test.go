package engine_test

// The sealed store, from the outside: the secret gate, the restore under
// another host's key, and the history-generation rotate.
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
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	sgProviderKind = "substrate.reamde.dev/llm/provider"
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
		Kind: pkg + "/artifact", ID: fid("a1"),
		Properties: map[string]any{"fingerprint": "not-a-digest"},
	})
	wantErr(t, err, substrate.ErrValidation, "malformed digest")

	sum := strings.Repeat("ab", 32)
	rec := mustPut(t, ds, owner, substrate.PutInput{
		Kind: pkg + "/artifact", ID: fid("a1"),
		Properties: map[string]any{"name": "one", "fingerprint": sum},
	})
	if got := rec.Properties["fingerprint"]; got != "<redacted>" {
		t.Fatalf("digest not redacted on the wire: %v", got)
	}
	// Stored as the value itself: the engine compares digests in SQL.
	var stored string
	if err := db.QueryRow(`SELECT props->>'fingerprint' FROM records WHERE kind = $1 AND id = $2`,
		pkg+"/artifact", fid("a1")).Scan(&stored); err != nil {
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
	const kind = "substrate.reamde.dev/llm/provider"
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
	dir2, err := archivedRepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := changelogfile.ReadArchiveManifestDir(dir2)
	if err != nil || len(m.DEK) == 0 {
		t.Fatalf("the copied manifest carries no DEK: %+v, %v", m, err)
	}
	// The secret's version the manifest selects, replaced by one sealed under
	// the host key: a payload the DEK the manifest carries cannot open.
	sum, _, ok := changelogfile.ParseSealedObjectName(ref, m.Sealed[ref])
	if !ok {
		t.Fatalf("the copy's manifest selects no version of %s", ref)
	}
	body, err := os.ReadFile(filepath.Join(dir2, m.Archive, filepath.FromSlash(m.Sealed[ref])))
	if err != nil {
		t.Fatal(err)
	}
	rec, err := changelogfile.DecodeSealedVersion(ref, sum, body)
	if err != nil {
		t.Fatal(err)
	}
	if rec.Payload, err = engine.SealWithKey(engine.TestCredentialKeyBytes, []byte("sk-marked"),
		engine.SealedAAD(ref, rec.RecordKind, rec.RecordID)); err != nil {
		t.Fatal(err)
	}
	data, sum, err := changelogfile.EncodeSealedVersion(rec)
	if err != nil {
		t.Fatal(err)
	}
	name := changelogfile.SealedObjectName(ref, sum, m.Pass)
	if err := os.WriteFile(filepath.Join(dir2, m.Archive, filepath.FromSlash(name)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	m.Sealed[ref] = name
	m.Epoch++
	if _, err := changelogfile.WriteArchiveManifestDir(dir2, m); err != nil {
		t.Fatal(err)
	}

	dsn2 := engine.MigratedDSN(t)
	_, err = openWithKey(t, dsn2, root2, engine.TestCredentialKey)
	if err == nil {
		t.Fatal("a directory whose file does not open under the DEK imported")
	}
	for _, want := range []string{id, "does not open", ref} {
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
	dir2, err := archivedRepoDir(root2, id)
	if err != nil {
		t.Fatal(err)
	}
	m, _, err := changelogfile.ReadArchiveManifestDir(dir2)
	if err != nil {
		t.Fatal(err)
	}
	m.DEK, m.DEKKeyID = nil, ""
	m.Epoch++
	if _, err := changelogfile.WriteArchiveManifestDir(dir2, m); err != nil {
		t.Fatal(err)
	}

	dsn2 := engine.MigratedDSN(t)
	_, err = openWithKey(t, dsn2, root2, engine.TestCredentialKey)
	if err == nil {
		t.Fatal("a directory whose manifest carries no DEK imported")
	}
	for _, want := range []string{id, "carries no wrapped DEK", "manifest"} {
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

// registerOwner walks the registration flow and returns the user.
func registerOwner(t *testing.T, svc substrate.Service, repository string) *authUser {
	t.Helper()
	ctx := context.Background()
	enrollment, err := svc.BeginRegistration(ctx, repository)
	if err != nil {
		t.Fatalf("begin registration: %v", err)
	}
	u := &authUser{repository: repository, password: testPassword, seed: enrollment.Secret}
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: repository, Password: u.password,
		TOTPSecret: u.seed, TOTPCode: u.code(t), Label: "cli",
	}); err != nil {
		t.Fatalf("register %q: %v", repository, err)
	}
	return u
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
func openWithKey(t *testing.T, dsn, root, key string, opts ...engine.Option) (substrate.Service, error) {
	t.Helper()
	svc, err := engine.OpenForTest(t, context.Background(), dsn, append([]engine.Option{
		engine.WithKindsFS(engine.SeedKinds),
		engine.WithDataRoot(root),
		engine.WithCredentialKey(key),
	}, opts...)...)
	if err == nil {
		t.Cleanup(func() { _ = svc.Close() })
	}
	return svc, err
}

// foldRecords is the fold's records section, keyed `<kind>/<id>`.
func foldRecords(t *testing.T, ds substrate.Dataset) map[string]map[string]any {
	t.Helper()
	var snap struct {
		Records []map[string]any `json:"records"`
	}
	if err := json.Unmarshal(foldOf(t, ds), &snap); err != nil {
		t.Fatalf("decode the fold: %v", err)
	}
	out := make(map[string]map[string]any, len(snap.Records))
	for _, r := range snap.Records {
		out[fmt.Sprint(r["kind"])+"/"+fmt.Sprint(r["id"])] = r
	}
	return out
}

// lostSecretsFixture is a repository written under the test's host key and
// its archive: one registered owner, history, a provider row whose API key is
// a secret, and a blob.
type lostSecretsFixture struct {
	user    *authUser
	id      string
	root    string
	ref     string
	digest  string
	records map[string]map[string]any
}

func newLostSecretsFixture(t *testing.T) lostSecretsFixture {
	t.Helper()
	svc, dsn := newService(t)
	ctx := context.Background()
	u := registerOwner(t, svc, "ada.example.com")
	ds, err := svc.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	importVocabulary(t, ds, "tasks")
	writeSomeHistory(t, ds)
	f := lostSecretsFixture{
		user:   u,
		ref:    putProvider(t, ds, dsn, "openai", "sk-left-behind"),
		digest: putBlob(t, ds, []byte("bytes that travel")),
		id:     repositoryIDOf(t, ds),
	}
	f.records = foldRecords(t, ds)
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	f.root = root
	return f
}

// An archive brought to a host whose key is not the one it was written under
// is refused, and the refusal says which key it wants, how to keep its
// secrets and how to accept losing them: a wrong key must not read as a
// repository to take without its secrets.
func TestARestoreUnderAnotherHostKeyIsRefusedNamingTheKey(t *testing.T) {
	t.Parallel()
	f := newLostSecretsFixture(t)
	root := copyRepositoryDir(t, f.root, f.id)
	_, other := otherCredentialKey(t)
	_, err := openWithKey(t, engine.MigratedDSN(t), root, other)
	if err == nil {
		t.Fatal("the archive restored under a key it was not written under")
	}
	for _, want := range []string{"ada.example.com", f.id, engine.ImportWithoutSecretsVar, "is not the key the DEK wrap", "user reset"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	// Listing another repository changes nothing.
	if _, err := openWithKey(t, engine.MigratedDSN(t), root, other, engine.WithImportWithoutSecrets("bob.example.com")); err == nil {
		t.Fatal("a list naming another repository restored this one")
	}
}

// A listed repository restores under another host's key without its
// secrets: every record arrives as written but the ones that named a secret,
// which read without it; the sealed store is empty and so is the spool; the
// blob and the history come through; the owner signs in again only after a
// reset.
func TestAListedRepositoryRestoresWithoutItsSecrets(t *testing.T) {
	t.Parallel()
	f := newLostSecretsFixture(t)
	ctx := context.Background()
	root := copyRepositoryDir(t, f.root, f.id)
	otherRaw, other := otherCredentialKey(t)
	dsn := engine.MigratedDSN(t)
	// A webhook request the archive holds spooled: sealed under the DEK this
	// host does not open, so it goes with the secrets.
	archived, err := archivedRepoDir(root, f.id)
	if err != nil {
		t.Fatal(err)
	}
	spoolInto(t, archived, changelogfile.SpoolFileName(1, "fire1"), []byte("sealed under the old key"))
	svc, err := openWithKey(t, dsn, root, other, engine.WithImportWithoutSecrets(f.id))
	if err != nil {
		t.Fatalf("a listed repository did not restore: %v", err)
	}
	ds, err := svc.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	// The open deleted the request, before any drain could reach it.
	dir, err := changelogfile.RepoDir(root, f.id)
	if err != nil {
		t.Fatal(err)
	}
	if spooled, err := changelogfile.SpoolFiles(dir); err != nil || len(spooled) != 0 {
		t.Fatalf("the spool holds %d requests (%v) after the open, want none", len(spooled), err)
	}

	// The records the secrets were on lost exactly them; nothing else moved
	// but their version and their time.
	cleared := map[string]string{
		typeProvider + "/openai":                    "apiKey",
		"substrate.reamde.dev/core/credential/self": "passwordRef",
	}
	got := foldRecords(t, ds)
	for key, before := range f.records {
		after, ok := got[key]
		if !ok {
			t.Errorf("%s did not arrive", key)
			continue
		}
		prop, lost := cleared[key]
		if !lost {
			if !reflect.DeepEqual(before, after) {
				t.Errorf("%s changed:\n before %v\n after  %v", key, before, after)
			}
			continue
		}
		wantProps := maps.Clone(before["props"].(map[string]any))
		delete(wantProps, prop)
		delete(wantProps, "totpRef")
		if !reflect.DeepEqual(wantProps, after["props"]) {
			t.Errorf("%s: props %v, want %v", key, after["props"], wantProps)
		}
	}
	// What appeared is the console's list of what to put back: an alert for
	// each value the restore left behind, and nothing else.
	alerts := map[string]bool{}
	for key, rec := range got {
		if _, ok := f.records[key]; ok {
			continue
		}
		if !strings.HasPrefix(key, "substrate.reamde.dev/core/alert/") {
			t.Errorf("%s appeared", key)
			continue
		}
		props, _ := rec["props"].(map[string]any)
		summary, _ := props["summary"].(string)
		alerts[summary] = props["state"] == "open"
	}
	for _, want := range []string{
		typeProvider + "/openai: apiKey",
		"substrate.reamde.dev/core/credential/self: passwordRef",
		"substrate.reamde.dev/core/credential/self: totpRef",
	} {
		found := false
		for summary, open := range alerts {
			if strings.HasPrefix(summary, want) && open {
				found = true
			}
		}
		if !found {
			t.Errorf("no open alert names %s: %v", want, alerts)
		}
	}
	if len(alerts) != 3 {
		t.Errorf("%d alerts appeared, want the three values left behind: %v", len(alerts), alerts)
	}
	var sealed int
	var wrapped []byte
	var marked bool
	if err := rawDB(t, dsn).QueryRow(`SELECT (SELECT count(*) FROM sealed), dek, secrets_left_behind FROM repositories WHERE id = $1`, f.id).
		Scan(&sealed, &wrapped, &marked); err != nil {
		t.Fatal(err)
	}
	if sealed != 0 || marked {
		t.Fatalf("%d sealed rows arrived and the mark is %v after the open, want none and lifted", sealed, marked)
	}
	// The new DEK is this host's.
	if _, err := engine.OpenPayloadWithKey(otherRaw, wrapped, engine.DEKAAD(f.id)); err != nil {
		t.Fatalf("the row's DEK does not open under this host's key: %v", err)
	}
	if got := getBlob(t, ds, f.digest); string(got) != "bytes that travel" {
		t.Fatalf("blob bytes = %q", got)
	}
	if report := mustVerify(t, svc, "ada.example.com"); !report.OK {
		t.Fatalf("the restored repository does not verify: %+v", report)
	}

	// The owner cannot sign in with what they knew: the password hash was a
	// secret. The operator's reset gives them a way back in, and the
	// repository then writes secrets under its new key.
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: "ada.example.com", Password: f.user.password, TOTPCode: f.user.code(t), Label: "after",
	}); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a login with the lost credential = %v, want refused", err)
	}
	resetter, ok := svc.(interface {
		ResetUser(context.Context, string, string) (substrate.TOTPEnrollment, error)
	})
	if !ok {
		t.Fatal("the engine service must expose ResetUser for substratectl")
	}
	enrollment, err := resetter.ResetUser(ctx, "ada.example.com", "a-new-password-for-ada")
	if err != nil {
		t.Fatalf("user reset: %v", err)
	}
	reset := &authUser{repository: "ada.example.com", password: "a-new-password-for-ada", seed: enrollment.Secret}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: "ada.example.com", Password: reset.password, TOTPCode: reset.code(t), Label: "after reset",
	}); err != nil {
		t.Fatalf("login after the reset: %v", err)
	}
	ref := putProvider(t, ds, dsn, "openai", "sk-entered-again")
	if got := openSecretUnder(t, dsn, ref, otherRaw); got != "sk-entered-again" {
		t.Fatalf("the key entered again reads %q", got)
	}
}

// Listing a repository loses nothing this host can open: under the key it was
// written under it restores with its secrets.
func TestAListedRepositoryUnderItsOwnKeyKeepsItsSecrets(t *testing.T) {
	t.Parallel()
	f := newLostSecretsFixture(t)
	ctx := context.Background()
	root := copyRepositoryDir(t, f.root, f.id)
	dsn := engine.MigratedDSN(t)
	svc, err := openWithKey(t, dsn, root, engine.TestCredentialKey, engine.WithImportWithoutSecrets("ada.example.com"))
	if err != nil {
		t.Fatalf("boot: %v", err)
	}
	ds, err := svc.Dataset(ctx, "ada.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got := foldRecords(t, ds); !reflect.DeepEqual(got, f.records) {
		t.Fatal("the records moved under the key they were written under")
	}
	if got := openSecretUnder(t, dsn, f.ref, engine.TestCredentialKeyBytes); got != "sk-left-behind" {
		t.Fatalf("secret = %q", got)
	}
	if _, _, err := svc.Login(ctx, substrate.LoginInput{
		Repository: "ada.example.com", Password: f.user.password, TOTPCode: f.user.code(t), Label: "after",
	}); err != nil {
		t.Fatalf("login with the credential that traveled: %v", err)
	}
}

// A host with no credential key refuses a listed repository before it
// restores anything: the DEK it would mint would be stored unsealed. The
// archive stays as it was, and no row is created.
func TestAListedRestoreOnAHostWithNoKeyRestoresNothing(t *testing.T) {
	t.Parallel()
	f := newLostSecretsFixture(t)
	root := copyRepositoryDir(t, f.root, f.id)
	archived, err := archivedRepoDir(root, f.id)
	if err != nil {
		t.Fatal(err)
	}
	spoolInto(t, archived, changelogfile.SpoolFileName(1, "fire1"), []byte("sealed under the old key"))
	before := filesUnder(t, archived)
	dsn := engine.MigratedDSN(t)
	_, err = openWithKey(t, dsn, root, "", engine.WithImportWithoutSecrets(f.id))
	if err == nil {
		t.Fatal("a host with no credential key restored a repository without its secrets")
	}
	for _, want := range []string{"ada.example.com", "SUBSTRATE_CREDENTIAL_KEY", "nothing was restored"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("the refusal does not name %q: %v", want, err)
		}
	}
	if after := filesUnder(t, archived); !reflect.DeepEqual(before, after) {
		t.Fatalf("the refused restore changed the archive:\n before %v\n after  %v", keysOf(before), keysOf(after))
	}
	var rows int
	if err := rawDB(t, dsn).QueryRow(`SELECT count(*) FROM repositories`).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 0 {
		t.Fatalf("the refused restore left %d row(s)", rows)
	}
}

// spoolInto adds a spooled request to the newest manifest of the archive
// under repoDir, in a manifest of the next epoch, as a pass that found it
// would.
func spoolInto(t *testing.T, repoDir, file string, data []byte) {
	t.Helper()
	m, _, err := changelogfile.ReadArchiveManifestDir(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	name := changelogfile.SpoolObjectName(file, m.Pass)
	path := filepath.Join(repoDir, m.Archive, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	m.Spool = append(m.Spool, name)
	m.Epoch++
	if _, err := changelogfile.WriteArchiveManifestDir(repoDir, m); err != nil {
		t.Fatal(err)
	}
}

// filesUnder is every file under dir, by its path there, with its bytes.
func filesUnder(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		out[rel] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func keysOf(m map[string]string) []string {
	return slices.Sorted(maps.Keys(m))
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

	report, err := svc.(engine.Operator).RotateHistoryGeneration(ctx, testdb.Repository(t))
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
