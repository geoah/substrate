package testenv_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/testenv"
	"github.com/geoah/substrate/kinds"
)

// The release kept as v0.123.1 registered its repository with an age wrap of
// the data-encryption key in the history and a record holding it. Imported
// under the current code (decision 0264), the wrap is gone from every line of
// the history, the record is purged, and the kind stays declared and empty.
// TestKeptDirectories holds every other record to what that release served.
func TestTheKeptDirectoryLosesItsRecoveryWrap(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)
	wrap := []byte(`"sealedKey":"`)
	if !historyHolds(t, filepath.Join(dir, "repositories", fixture.Repository, "changelog"), wrap) {
		t.Fatal("the kept history holds no wrap, so this test holds nothing")
	}

	root := t.TempDir()
	copyKept(t, filepath.Join(dir, "repositories"), filepath.Join(root, "repositories"))
	e := testenv.Start(t, testenv.WithoutRegistration(), testenv.WithRepository(fixture.Repository),
		testenv.WithDataRoot(root), testenv.WithCredentialKey(fixture.CredentialKey))
	ctx := context.Background()
	ds, err := e.Service.Dataset(ctx, fixture.Repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Get(ctx, "substrate.reamde.dev/core/recoverykey", "self"); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("the record holding the wrap reads %v, want purged", err)
	}
	if _, err := ds.KindByRef(ctx, "substrate.reamde.dev/core/recoverykey"); err != nil {
		t.Errorf("the kind left the stored vocabulary, which the boot never prunes: %v", err)
	}
	// The stop's last pass copies the converted history into the archive:
	// no line of it holds the wrap, and its manifest names the dialect.
	e.Stop()
	held := false
	if err := keptWalk(root, fixture.Repository, func(entry changelogfile.Entry) error {
		line, err := json.Marshal(entry)
		held = held || bytes.Contains(line, wrap)
		return err
	}); err != nil {
		t.Fatalf("read the archive the stop left: %v", err)
	}
	if held {
		t.Error("the converted history still holds the wrap")
	}
	archived := filepath.Join(root, "archive", changelogfile.RepositoriesDir, ds.Repository().ID)
	if m, _, err := changelogfile.ReadArchiveManifestDir(archived); err != nil || m.ChangelogDialect != engine.MaxChangelogDialect() {
		t.Errorf("the archive's manifest %+v (%v) after the import, want changelog dialect %d", m, err, engine.MaxChangelogDialect())
	}
}

// The same kept directory restored on a host whose key did not write it: the
// directory is from before repository ids, so the boot gives it its id and
// the listing has to reach that step as well as the import (decision 0264).
// Unlisted, it is refused naming the variable; listed, every record the
// release served arrives (the recovery and sign-in records aside, which are
// purged), the secrets it held read unset, and the sealed store is empty. Its
// sign-in record was sealed under the lost key too, so it has nobody to sign
// in as until the operator's reset gives it its person (decision 0274); then
// the token the owner held reads again, and the old password signs in
// nowhere.
// Every sealed value it held stays until the open clears it, and the open
// then deletes a spooled request sealed under the key it lost.
func TestTheKeptDirectoryImportsUnderAnotherKeyWithoutItsSecrets(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)
	var want keptRecords
	readKept(t, filepath.Join(dir, "records.json"), &want)
	other := testenv.MintCredentialKey()
	ctx := context.Background()

	refused := t.TempDir()
	copyKept(t, filepath.Join(dir, "repositories"), filepath.Join(refused, "repositories"))
	if svc, err := engine.Open(ctx, testdb.NewSchema(t), engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(refused), engine.WithCredentialKey(other)); err == nil {
		_ = svc.Close()
		t.Fatal("the kept directory imported under a key it was not written under")
	} else if !strings.Contains(err.Error(), engine.ImportWithoutSecretsVar) || !strings.Contains(err.Error(), fixture.Repository) {
		t.Fatalf("the refusal does not name %s and the repository: %v", engine.ImportWithoutSecretsVar, err)
	}

	root := t.TempDir()
	copyKept(t, filepath.Join(dir, "repositories"), filepath.Join(root, "repositories"))
	if err := changelogfile.WriteSpoolFile(filepath.Join(root, "repositories", fixture.Repository),
		changelogfile.SpoolFileName(1, "fire1"), []byte("sealed under the old key")); err != nil {
		t.Fatal(err)
	}
	dsn := testdb.NewSchema(t)
	e := testenv.Start(t, testenv.WithoutRegistration(), testenv.WithRepository(fixture.Repository),
		testenv.WithDSN(dsn), testenv.WithDataRoot(root), testenv.WithCredentialKey(other),
		testenv.WithEngineOptions(engine.WithImportWithoutSecrets(fixture.Repository)))
	e.Token = fixture.Token
	if _, err := e.Service.(engine.Operator).ResetUser(ctx, fixture.Repository, "a password after the move", ""); err != nil {
		t.Fatalf("give the repository its person: %v", err)
	}
	got := keptRead(t, e)
	// The release's records as the converted history holds them.
	keptConvert(&want, keptConversions, keptDialect(t, dir, fixture.Repository), fixture.Repository)
	for _, rec := range want.Records {
		if rec.Path == "substrate.reamde.dev/core/recoverykey/self" || rec.Path == "substrate.reamde.dev/core/credential/self" {
			continue
		}
		if _, ok := got[rec.Path]; !ok {
			t.Errorf("%s did not arrive", rec.Path)
		}
	}
	for path, props := range map[string][]string{
		"~/creds/oauthclient/00000000000000000000000015": {"clientSecret", "accessToken"},
	} {
		now, ok := got[path]
		if !ok {
			t.Errorf("%s did not arrive", path)
			continue
		}
		have, _ := now.Read["properties"].(map[string]any)
		for _, p := range props {
			if v, set := have[p]; set {
				t.Errorf("%s still reads %s = %v", path, p, v)
			}
		}
	}
	ds, err := e.Service.Dataset(ctx, fixture.Repository)
	if err != nil {
		t.Fatal(err)
	}
	scoped, err := engine.OpenScopedDB(dsn, ds.Repository().ID, engine.RoleApp)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = scoped.Close() }()
	var sealed int
	if err := scoped.QueryRowContext(ctx, `SELECT count(*) FROM sealed`).Scan(&sealed); err != nil || sealed != 0 {
		t.Errorf("the sealed store holds %d values (%v), want none", sealed, err)
	}
	if spooled, err := changelogfile.SpoolFiles(filepath.Join(root, "repositories", ds.Repository().ID)); err != nil || len(spooled) != 0 {
		t.Errorf("the spool holds %d requests (%v) after the open, want none", len(spooled), err)
	}
	if status, raw := e.Login(fixture.Repository, fixture.Password, ""); status != http.StatusUnauthorized {
		t.Errorf("a login with the password that stayed behind = %d %s, want 401", status, raw)
	}
}

// The same kept directory, listed, on a host with no credential key: the
// boot refuses before its keying changes anything, because the DEK it would
// mint would be stored unsealed. Its sealed files and a spooled request stay
// as they were.
func TestTheKeptDirectoryListedOnAHostWithNoKeyKeepsItsSecrets(t *testing.T) {
	dir := filepath.Join(keptRoot, "v0.123.1")
	var fixture keptFixture
	readKept(t, filepath.Join(dir, "fixture.json"), &fixture)
	root := t.TempDir()
	copyKept(t, filepath.Join(dir, "repositories"), filepath.Join(root, "repositories"))
	repoDir := filepath.Join(root, "repositories", fixture.Repository)
	if err := changelogfile.WriteSpoolFile(repoDir, changelogfile.SpoolFileName(1, "fire1"), []byte("sealed under the old key")); err != nil {
		t.Fatal(err)
	}
	before := sealedAndSpooledFiles(t, repoDir)
	if len(before) < 2 {
		t.Fatalf("the directory holds %d sealed and spooled files, so this test holds nothing", len(before))
	}
	svc, err := engine.Open(context.Background(), testdb.NewSchema(t), engine.WithKindsFS(kinds.Seed()),
		engine.WithDataRoot(root), engine.WithImportWithoutSecrets(fixture.Repository))
	if err == nil {
		_ = svc.Close()
		t.Fatal("a host with no credential key imported the kept directory without its secrets")
	}
	if !strings.Contains(err.Error(), "SUBSTRATE_CREDENTIAL_KEY") || !strings.Contains(err.Error(), "nothing was restored") {
		t.Fatalf("the refusal does not name the key and what it left: %v", err)
	}
	if after := sealedAndSpooledFiles(t, repoDir); !maps.Equal(before, after) {
		t.Fatalf("the refused boot changed the directory: %d files before, %d after", len(before), len(after))
	}
}

// sealedAndSpooledFiles is every file under a repository directory's sealed
// and spool directories, by its path there, with its bytes.
func sealedAndSpooledFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, sub := range []string{changelogfile.SealedDir(dir), changelogfile.SpoolDir(dir)} {
		entries, err := os.ReadDir(sub)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(sub, e.Name()))
			if err != nil {
				t.Fatal(err)
			}
			out[filepath.Join(filepath.Base(sub), e.Name())] = string(raw)
		}
	}
	return out
}

// historyHolds reports whether any changelog segment under dir holds needle.
// It reads the kept directory itself, before any boot gives its history up.
func historyHolds(t *testing.T, dir string, needle []byte) bool {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".ndjson") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(raw, needle) {
			return true
		}
	}
	return false
}
