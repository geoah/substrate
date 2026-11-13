package engine

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/geoah/substrate/internal/changelogfile"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
	"github.com/geoah/substrate/internal/vocabulary"
)

// lostAccountKind is an account kind as a provider declares one: the OAuth
// facility's token ref, status and reason.
const lostAccountKind = "lost.example.com/lost/account"

func declareLostAccountKind(t *testing.T, ds *dataset) {
	t.Helper()
	docs := []map[string]any{
		{
			"kind": kindPackage, "metadata": map[string]any{"id": "lost.example.com/lost"},
			"data": map[string]any{"authority": "lost.example.com", "package": "lost", "version": 1},
		},
		{
			"kind": kindKind, "metadata": map[string]any{"id": lostAccountKind},
			"data": map[string]any{
				"authority": "lost.example.com", "package": "lost",
				"names":           map[string]any{"singular": "account"},
				"displayTemplate": "{label}",
				"properties":      lostAccountProps(),
			},
		},
	}
	if _, err := ds.ApplyVocabularyDocuments(context.Background(), substrate.ActorAPI, docs); err != nil {
		t.Fatalf("declare the account kind: %v", err)
	}
}

func lostAccountProps() map[string]any {
	return map[string]any{
		"label":        map[string]any{"type": "string"},
		"tokenRef":     map[string]any{"type": "secret", "writer": "oauth"},
		"tokenStatus":  map[string]any{"type": "string", "writer": "oauth"},
		"tokenError":   map[string]any{"type": "string", "writer": "oauth"},
		"tokenErrorAt": map[string]any{"type": "datetime", "writer": "oauth"},
	}
}

// connectLostAccount writes an account of kind the way the OAuth callback
// leaves one: its token sealed under a ref the record names, `connected`. It
// answers the ref.
func connectLostAccount(t *testing.T, ds *dataset, kind, id string) string {
	t.Helper()
	// The engine's hand names the record, as a provider's sync does.
	if _, err := ds.Put(context.Background(), substrate.ActorSystem, substrate.PutInput{
		Kind: kind, ID: id, Properties: map[string]any{"label": id},
	}); err != nil {
		t.Fatalf("put account %s: %v", id, err)
	}
	account := eref{Kind: kind, ID: id}
	var ref string
	err := ds.inTx(context.Background(), actorOAuth, true, func(t *txn) error {
		var err error
		if ref, err = newID(); err != nil {
			return err
		}
		if err := t.putCredential(ref, account, &oauth2.Token{AccessToken: "at-" + id, Expiry: time.Now().Add(time.Hour)}); err != nil {
			return err
		}
		_, err = t.patch(account, substrate.PatchInput{Properties: map[string]any{
			propTokenRef: ref, propTokenStatus: tokenStatusConnected,
		}})
		return err
	})
	if err != nil {
		t.Fatalf("connect %s: %v", id, err)
	}
	return ref
}

// leaveBehind is what an import without secrets leaves for the open: the
// repository marked, and each value named sealed under a key nothing here
// holds, which a flipped byte stands in for.
func leaveBehind(t *testing.T, ds *dataset, refs ...string) {
	t.Helper()
	if _, err := ds.svc.maint.Exec(`UPDATE repositories SET secrets_left_behind = true WHERE id = $1`, ds.info.ID); err != nil {
		t.Fatal(err)
	}
	for _, ref := range refs {
		if _, err := ds.db.Exec(`UPDATE sealed SET payload = overlay(payload placing decode(lpad(to_hex(get_byte(payload, length(payload) - 1) # 1), 2, '0'), 'hex') from length(payload)) WHERE ref = $1`, ref); err != nil {
			t.Fatal(err)
		}
	}
}

func secretsLeftBehindMarked(t *testing.T, ds *dataset) bool {
	t.Helper()
	var marked bool
	if err := ds.svc.maint.QueryRow(`SELECT secrets_left_behind FROM repositories WHERE id = $1`, ds.info.ID).Scan(&marked); err != nil {
		t.Fatal(err)
	}
	return marked
}

func sealedRows(t *testing.T, ds *dataset, ref string) int {
	t.Helper()
	var n int
	if err := ds.db.QueryRow(`SELECT count(*) FROM sealed WHERE ref = $1`, ref).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// The open's clearing takes out what the import left behind and nothing it
// did not: an account whose token does not open reads `erroring` with the
// reason the console shows beside its reconnect, a provider's key reads
// unset, a value that opens stays, each value taken is an alert and is
// removed with the write that takes its ref, and the mark is lifted. A
// second open writes nothing; putting a key back resolves its alert.
func TestALostSecretIsClearedAndItsAccountAsksToBeReconnected(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	ctx := context.Background()
	declareLostAccountKind(t, ds)
	goneToken := connectLostAccount(t, ds, lostAccountKind, "gone")
	connectLostAccount(t, ds, lostAccountKind, "kept")
	keyRef := putProviderSecret(t, ds, "openai", "sk-gone")
	keptRef := putProviderSecret(t, ds, "anthropic", "sk-kept")
	leaveBehind(t, ds, goneToken, keyRef)

	if err := ds.clearSecretsLeftBehind(ctx); err != nil {
		t.Fatalf("clear what the import left behind: %v", err)
	}
	gone, err := ds.Get(ctx, lostAccountKind, "gone")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := gone.Properties[propTokenRef]; ok || gone.Properties[propTokenStatus] != tokenStatusErroring ||
		gone.Properties[propTokenError] != lostTokenError || gone.Properties[propTokenErrorAt] == nil {
		t.Fatalf("the account whose token is gone reads %v", gone.Properties)
	}
	kept, err := ds.Get(ctx, lostAccountKind, "kept")
	if err != nil {
		t.Fatal(err)
	}
	if kept.Properties[propTokenStatus] != tokenStatusConnected || kept.Properties[propTokenRef] == nil {
		t.Fatalf("the account whose token is held reads %v", kept.Properties)
	}
	var openaiKey, anthropicKey *string
	if err := ds.db.QueryRow(`SELECT props->>'apiKey' FROM records WHERE kind = $1 AND id = 'openai'`, bindingProviderKind).Scan(&openaiKey); err != nil {
		t.Fatal(err)
	}
	if err := ds.db.QueryRow(`SELECT props->>'apiKey' FROM records WHERE kind = $1 AND id = 'anthropic'`, bindingProviderKind).Scan(&anthropicKey); err != nil {
		t.Fatal(err)
	}
	if openaiKey != nil || anthropicKey == nil || *anthropicKey != keptRef {
		t.Fatalf("after the clearing the lost key reads %v and the held one %v", openaiKey, anthropicKey)
	}
	wantLeftBehindAlert(t, ds, vocabulary.RecordPath(lostAccountKind, "gone"), propTokenRef, "reconnect it")
	wantLeftBehindAlert(t, ds, vocabulary.RecordPath(bindingProviderKind, "openai"), "apiKey", "enter it again")
	if sealedRows(t, ds, goneToken) != 0 || sealedRows(t, ds, keyRef) != 0 || sealedRows(t, ds, keptRef) != 1 {
		t.Fatal("the clearing did not remove exactly the values that do not open")
	}
	if secretsLeftBehindMarked(t, ds) {
		t.Fatal("the mark stands after the clearing")
	}

	head := maxSeqInternal(t, ds)
	if err := ds.clearSecretsLeftBehind(ctx); err != nil {
		t.Fatal(err)
	}
	if again := maxSeqInternal(t, ds); again != head {
		t.Fatalf("a second clearing wrote %d entries", again-head)
	}
	// The owner enters the key again: its alert resolves.
	putProviderSecret(t, ds, "openai", "sk-entered-again")
	if a := leftBehindAlert(t, ds, vocabulary.RecordPath(bindingProviderKind, "openai"), "apiKey"); a.Properties["state"] != alertStateResolved {
		t.Fatalf("the key entered again left its alert %v", a.Properties["state"])
	}
}

// A value that does not open in a repository no import marked is left as it
// is: damage, never a loss the operator accepted, and the clearing is never
// a repair.
func TestALostSecretNoImportListedIsLeftAsItIs(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	ref := putProviderSecret(t, ds, "openai", "sk-gone")
	leaveBehind(t, ds, ref)
	if _, err := ds.svc.maint.Exec(`UPDATE repositories SET secrets_left_behind = false WHERE id = $1`, ds.info.ID); err != nil {
		t.Fatal(err)
	}
	head := maxSeqInternal(t, ds)
	if err := ds.clearSecretsLeftBehind(context.Background()); err != nil {
		t.Fatal(err)
	}
	if again := maxSeqInternal(t, ds); again != head || sealedRows(t, ds, ref) != 1 {
		t.Fatalf("the clearing wrote %d entries, or removed the value, over a repository no import marked", again-head)
	}
}

func maxSeqInternal(t *testing.T, ds *dataset) int64 {
	t.Helper()
	var seq int64
	if err := ds.db.QueryRow(`SELECT coalesce(max(seq), 0) FROM changelog`).Scan(&seq); err != nil {
		t.Fatal(err)
	}
	return seq
}

// leftBehindAlert is the alert raised for one property of one record.
func leftBehindAlert(t *testing.T, ds *dataset, path, property string) *substrate.Record {
	t.Helper()
	kind, id, _ := vocabulary.SplitRecordPath(path)
	a, err := ds.Get(context.Background(), kindAlert, alertID(secretLeftBehindKey(eref{Kind: kind, ID: id}, property)))
	if err != nil {
		t.Fatalf("the alert for %s %s: %v", path, property, err)
	}
	return a
}

// wantLeftBehindAlert holds the console's list to naming what to put back:
// an open alert whose summary names the record, the property and what to do.
func wantLeftBehindAlert(t *testing.T, ds *dataset, path, property, todo string) {
	t.Helper()
	a := leftBehindAlert(t, ds, path, property)
	summary, _ := a.Properties["summary"].(string)
	if a.Properties["state"] != alertStateOpen || !strings.Contains(summary, path) || !strings.Contains(summary, todo) ||
		(property != propTokenRef && !strings.Contains(summary, property)) {
		t.Fatalf("the alert for %s %s reads %v", path, property, a.Properties)
	}
}

// THE RULE, over every state a package can be in and every spelling of a
// secret type. A repository written under one host's key, its package
// holding a property of type `secret`, one of a property type based on it,
// and the OAuth facility's token, is imported under another host's key
// without its secrets. Its package, at the import or before it, is admitted,
// parked because it does not admit, parked because it does not parse, or
// uninstalled with its records left; it is then installed again where it
// was out of the registry. Whatever the state, each read is unset, the
// account asks to be reconnected, and the console's list of alerts names
// every value to put back.
func TestASecretAnImportLeftBehindReadsUnsetInEveryStateAndSpelling(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"admitted", "parked: does not admit", "parked: does not parse", "uninstalled with records left"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			name := testdb.Repository(t)
			srcRoot, srcDSN := t.TempDir(), MigratedDSN(t)
			src, err := OpenForTest(t, ctx, srcDSN, WithDataRoot(srcRoot))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := src.CreateRepository(ctx, name); err != nil {
				t.Fatal(err)
			}
			d, err := src.Dataset(ctx, name)
			if err != nil {
				t.Fatal(err)
			}
			ds := d.(*dataset)
			if _, err := ds.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(spellingsPackage)), spellingsDocs(), nil,
				substrate.BundleInstall{}); err != nil {
				t.Fatalf("install the closure: %v", err)
			}
			mustPutInternal(t, ds, substrate.PutInput{Kind: spellingsKey, ID: fid("k1"), Properties: map[string]any{
				"name": "one", "apiKey": "sk-builtin", "token": "tok-based",
			}})
			connectLostAccount(t, ds, spellingsAccount, fid("a1"))
			id := ds.info.ID
			_ = src.Close()
			if state == "uninstalled with records left" {
				// Quarantined, it uninstalls with its records left.
				q, err := OpenForTest(t, ctx, srcDSN, WithDataRoot(srcRoot), WithTestInadmissible(spellingsPackage))
				if err != nil {
					t.Fatal(err)
				}
				qd, err := q.Dataset(ctx, name)
				if err != nil {
					t.Fatal(err)
				}
				if err := qd.UninstallBundle(ctx, spellingsPackage); err != nil {
					t.Fatalf("uninstall the quarantined package: %v", err)
				}
				_ = q.Close()
			}

			root := copyRepository(t, srcRoot, id)
			dsn := MigratedDSN(t)
			opts := []Option{WithDataRoot(root), WithCredentialKey(otherHostKey(t)), WithImportWithoutSecrets(name)}
			if state == "parked: does not admit" {
				opts = append(opts, WithTestInadmissible(spellingsPackage))
			}
			dst, err := OpenForTest(t, ctx, dsn, opts...)
			if err != nil {
				t.Fatalf("import without secrets: %v", err)
			}
			t.Cleanup(func() { _ = dst.Close() })
			if state == "parked: does not parse" {
				// The pre-refactor agent shape a later binary no longer parses.
				if _, err := rawDBOf(t, dsn).Exec(
					`UPDATE records SET props = (props - 'provider' - 'model') || '{"llm": "cheap"}'::jsonb
					 WHERE kind = $1 AND id = $2`, kindAgent, spellingsPackage+"/keeper"); err != nil {
					t.Fatal(err)
				}
			}
			d2, err := dst.Dataset(ctx, name)
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			ds2 := d2.(*dataset)
			if state != "admitted" {
				if _, ok := ds2.registry().ByIdentity(spellingsKey); ok {
					t.Fatalf("the package is in the registry, so this case holds nothing of %q", state)
				}
				if _, err := ds2.InstallBundleClosure(ctx, substrate.BundleActor(vocabulary.SplitPackageRef(spellingsPackage)), spellingsDocs(), nil,
					substrate.BundleInstall{}); err != nil {
					t.Fatalf("install the package again: %v", err)
				}
			}

			key, err := ds2.Get(ctx, spellingsKey, fid("k1"))
			if err != nil {
				t.Fatal(err)
			}
			for _, prop := range []string{"apiKey", "token"} {
				if v, set := key.Properties[prop]; set && v != "" {
					t.Errorf("%s reads %v, want unset", prop, v)
				}
			}
			account, err := ds2.Get(ctx, spellingsAccount, fid("a1"))
			if err != nil {
				t.Fatal(err)
			}
			if v, set := account.Properties[propTokenRef]; (set && v != "") || account.Properties[propTokenStatus] != tokenStatusErroring {
				t.Errorf("the account reads %v, want no token and erroring", account.Properties)
			}
			var raw string
			if err := ds2.db.QueryRow(`SELECT props::text FROM records WHERE kind = $1 AND id = $2`, spellingsKey, fid("k1")).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(raw, "apiKey") || strings.Contains(raw, `"token"`) {
				t.Errorf("the stored key still names a ref: %s", raw)
			}
			wantLeftBehindAlert(t, ds2, vocabulary.RecordPath(spellingsKey, fid("k1")), "apiKey", "enter it again")
			wantLeftBehindAlert(t, ds2, vocabulary.RecordPath(spellingsKey, fid("k1")), "token", "enter it again")
			wantLeftBehindAlert(t, ds2, vocabulary.RecordPath(spellingsAccount, fid("a1")), propTokenRef, "reconnect it")
		})
	}
}

const (
	spellingsPackage = "spellings.bundles.substrate.reamde.dev/vault"
	spellingsKey     = spellingsPackage + "/key"
	spellingsAccount = spellingsPackage + "/account"
)

// spellingsDocs is a bundle closure holding a secret three ways: a property
// of type `secret`, one of a property type based on it, and an account's
// OAuth token; beside them an agent a test can age into a shape the loader
// no longer parses.
func spellingsDocs() []map[string]any {
	authority, pkg := vocabulary.SplitPackageRef(spellingsPackage)
	return []map[string]any{
		vocabulary.PackageManifest(spellingsPackage, 0),
		vocabulary.ActorManifest(spellingsPackage, vocabulary.PackageActor(spellingsPackage)),
		vocabulary.BundleManifest(spellingsPackage, map[string]any{
			"description": "a bundle whose kinds hold secrets",
			"installs":    []any{spellingsPackage + "/apitoken", spellingsKey, spellingsAccount, spellingsPackage + "/keeper"},
		}),
		{
			"kind": "substrate.reamde.dev/core/propertytype", "metadata": map[string]any{"id": spellingsPackage + "/apitoken"},
			"data": map[string]any{"authority": authority, "package": pkg, "base": "secret"},
		},
		vocabulary.KindManifest(spellingsPackage, map[string]any{"singular": "key"},
			map[string]any{"displayTemplate": "{name}", "properties": map[string]any{
				"name":   map[string]any{"type": "string"},
				"apiKey": map[string]any{"type": "secret"},
				"token":  map[string]any{"type": "apitoken"},
			}}),
		vocabulary.KindManifest(spellingsPackage, map[string]any{"singular": "account"},
			map[string]any{"displayTemplate": "{label}", "properties": lostAccountProps()}),
		vocabulary.AgentManifest(spellingsPackage, "keeper", map[string]any{
			"description": "keeps keys",
			"prompt":      "You keep keys.",
			"provider":    "default",
			"model":       "anthropic/claude-haiku-4-5",
		}),
	}
}

// otherHostKey is a second conforming credential key: the key of the host
// the copy is restored on.
func otherHostKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// modeCopy copies a repository directory byte for byte, with the modes a
// server's own directory has.
func modeCopy(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o700)
		}
		if strings.HasSuffix(d.Name(), ".lock") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
}

// stopLeftBehind stops a boot as a process dying right after its import left
// the secrets behind, before anything is keyed or written.
func stopLeftBehind(stage string) error {
	if stage == leftBehindStage {
		return errors.New("stopped after the secrets were left behind")
	}
	return nil
}

// sourceWithKey is a repository written under the test's host key holding an
// LLM provider's key, its id and its data root.
func sourceWithKey(t *testing.T) (root, id, ref string) {
	t.Helper()
	ctx := context.Background()
	root = t.TempDir()
	src, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := src.CreateRepository(ctx, testdb.Repository(t)); err != nil {
		t.Fatal(err)
	}
	d, err := src.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	ref = putProviderSecret(t, ds, "openai", "sk-carried")
	id = ds.info.ID
	_ = src.Close()
	return root, id, ref
}

// copyRepository copies what a restore on another host boots on into a data
// root of its own: the repository's archive, which the source's last pass
// left at its head, and a 0.x directory under the data root where key names
// one (a manifest is there).
func copyRepository(t *testing.T, fromRoot, key string) string {
	t.Helper()
	root := t.TempDir()
	archived := filepath.Join("archive", changelogfile.RepositoriesDir, key)
	if _, err := os.Stat(filepath.Join(fromRoot, archived)); err == nil {
		modeCopy(t, filepath.Join(fromRoot, archived), filepath.Join(root, archived))
	}
	from, err := changelogfile.RepoDir(fromRoot, key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := changelogfile.ReadManifest(from); err == nil {
		to, err := changelogfile.RepoDir(root, key)
		if err != nil {
			t.Fatal(err)
		}
		modeCopy(t, from, to)
	}
	return root
}

// wantLeftBehind holds an opened repository to what the import left behind
// being cleared: the provider's key unset with its alert open, no value left
// that does not open, and the mark lifted.
func wantLeftBehind(t *testing.T, ds *dataset) {
	t.Helper()
	var key *string
	if err := ds.db.QueryRow(`SELECT props->>'apiKey' FROM records WHERE kind = $1 AND id = 'openai'`, bindingProviderKind).Scan(&key); err != nil {
		t.Fatal(err)
	}
	if key != nil {
		t.Errorf("the provider's key still names %s", *key)
	}
	wantLeftBehindAlert(t, ds, vocabulary.RecordPath(bindingProviderKind, "openai"), "apiKey", "enter it again")
	var sealed int
	if err := ds.db.QueryRow(`SELECT count(*) FROM sealed`).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if sealed != 0 || secretsLeftBehindMarked(t, ds) {
		t.Errorf("%d sealed values stand and the mark is %v after the open", sealed, secretsLeftBehindMarked(t, ds))
	}
}

// A directory from before ids, listed, whose boot stops after its keying
// minted an id and left the secrets behind and before the id is written
// anywhere: the retry mints another id and finds every value still there,
// and its open clears them. Nothing is keyed by the id the first boot minted.
func TestAKeyingStoppedAfterLeavingTheSecretsBehindIsFinishedByTheRetry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	k := writeKeyedByNameSeeded(t, func(ds *dataset) { putProviderSecret(t, ds, "openai", "sk-format-one") })
	root := copyRepository(t, k.root, k.name)
	dsn, other := MigratedDSN(t), otherHostKey(t)
	if _, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(other),
		WithImportWithoutSecrets(k.name), WithTestImportFault(0, stopLeftBehind)); err == nil {
		t.Fatal("the boot did not stop where the seam stops it")
	}
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(other), WithImportWithoutSecrets(k.name))
	if err != nil {
		t.Fatalf("the retry: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	d, err := svc.Dataset(ctx, k.name)
	if err != nil {
		t.Fatal(err)
	}
	wantLeftBehind(t, d.(*dataset))
}

// A listed import stopped after it left the secrets behind, retried under the
// key the archive was written under: nothing was removed, so the retry is
// an ordinary restore that keeps every secret, marks nothing and raises no
// alert.
func TestAnImportStoppedAfterLeavingTheSecretsBehindKeepsThemUnderTheirOwnKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srcRoot, id, ref := sourceWithKey(t)
	root := copyRepository(t, srcRoot, id)
	dsn := MigratedDSN(t)
	if _, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(otherHostKey(t)),
		WithImportWithoutSecrets(testdb.Repository(t)), WithTestImportFault(0, stopLeftBehind)); err == nil {
		t.Fatal("the boot did not stop where the seam stops it")
	}
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
	if err != nil {
		t.Fatalf("the retry under the archive's own key: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	d, err := svc.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	var held []byte
	if err := ds.inTx(ctx, substrate.ActorSystem, false, func(t *txn) error {
		var err error
		held, err = t.openSealedRef(ref)
		return err
	}); err != nil || string(held) != "sk-carried" {
		t.Fatalf("the key reads %q, %v under its own key, want it kept", held, err)
	}
	if secretsLeftBehindMarked(t, ds) {
		t.Error("a restore under the archive's own key marked secrets left behind")
	}
	kind, alertKey := kindAlert, alertID(secretLeftBehindKey(eref{Kind: bindingProviderKind, ID: "openai"}, "apiKey"))
	if _, err := ds.Get(ctx, kind, alertKey); !errors.Is(err, substrate.ErrNotFound) {
		t.Errorf("an alert says the kept key did not move: %v", err)
	}
}

// A listed restore whose open never ran leaves the repository marked and its
// values that do not open in the table. The next boot, unlisted, opens it and
// clears them: what follows the restore is the mark, not the listing. The
// same archive restored into another database is refused unlisted, and
// listed it goes again from the start.
func TestARestoreWhoseOpenNeverRanIsClearedByTheNextOpenUnlisted(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srcRoot, id, _ := sourceWithKey(t)
	other := otherHostKey(t)
	root, dsn := copyRepository(t, srcRoot, id), MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(other),
		WithImportWithoutSecrets(testdb.Repository(t)))
	if err != nil {
		t.Fatalf("the listed restore: %v", err)
	}
	_ = svc.Close()
	var marked bool
	var unopened int
	if err := rawDBOf(t, dsn).QueryRow(`SELECT secrets_left_behind, (SELECT count(*) FROM sealed WHERE repository = $1) FROM repositories WHERE id = $1`, id).
		Scan(&marked, &unopened); err != nil {
		t.Fatal(err)
	}
	if !marked || unopened == 0 {
		t.Fatalf("after the restore the mark is %v and %d sealed values stand, so this test holds nothing", marked, unopened)
	}
	again, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(other))
	if err != nil {
		t.Fatalf("the next boot, unlisted: %v", err)
	}
	t.Cleanup(func() { _ = again.Close() })
	d, err := again.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	wantLeftBehind(t, d.(*dataset))

	if _, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(copyRepository(t, srcRoot, id)), WithCredentialKey(other)); err == nil ||
		!strings.Contains(err.Error(), ImportWithoutSecretsVar) {
		t.Fatalf("an unlisted restore into another database = %v, want refused naming %s", err, ImportWithoutSecretsVar)
	}
	second, err := OpenForTest(t, ctx, MigratedDSN(t), WithDataRoot(copyRepository(t, srcRoot, id)), WithCredentialKey(other),
		WithImportWithoutSecrets(testdb.Repository(t)))
	if err != nil {
		t.Fatalf("the listed restore into another database: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })
	d2, err := second.Dataset(ctx, testdb.Repository(t))
	if err != nil {
		t.Fatal(err)
	}
	wantLeftBehind(t, d2.(*dataset))
}

// nestPackage declares a kind that holds a value below a property's top: in
// an object, and in a list of objects that each hold a list.
const (
	nestPackage = "nest.example.com/nest"
	nestKind    = nestPackage + "/holder"
)

// nestDocs is the package; changed is its declaration after an upgrade that
// gave the object a field and the kind a property.
func nestDocs(changed bool) []map[string]any {
	object := map[string]any{"type": "object", "fields": map[string]any{
		"token": map[string]any{"type": "string"},
		"label": map[string]any{"type": "string"},
	}}
	props := map[string]any{
		"name": map[string]any{"type": "string"},
		"slots": map[string]any{"type": "object", "repeated": true, "fields": map[string]any{
			"refs": map[string]any{"type": "string", "repeated": true},
			"note": map[string]any{"type": "string"},
		}},
	}
	if changed {
		object["fields"].(map[string]any)["scope"] = map[string]any{"type": "string"}
		props["added"] = map[string]any{"type": "string"}
	}
	props["config"] = object
	return []map[string]any{
		{
			"kind": kindPackage, "metadata": map[string]any{"id": nestPackage},
			"data": map[string]any{"authority": "nest.example.com", "package": "nest", "version": 1},
		},
		{
			"kind": kindKind, "metadata": map[string]any{"id": nestKind},
			"data": map[string]any{
				"authority": "nest.example.com", "package": "nest",
				"names": map[string]any{"singular": "holder"}, "displayTemplate": "{name}",
				"properties": props,
			},
		},
	}
}

// sealUnder seals a value under the repository's key, bound to the record
// that holds ref, as a write that stored it leaves it.
func sealUnder(t *testing.T, ds *dataset, ref, kind, id string) {
	t.Helper()
	payload, err := ds.sealPayload([]byte("held below the top"), sealedAAD(ref, kind, id))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.db.Exec(`INSERT INTO sealed (ref, record_kind, record_id, payload, updated_at) VALUES ($1, $2, $3, $4, now())`,
		ref, kind, id, payload); err != nil {
		t.Fatal(err)
	}
}

// A ref below a property's top is cleared as one at the top is (the GC's
// ownership predicate finds it at any depth, so the clearing does): the ref
// leaves the object and the list that hold it, everything else they hold
// stays, the alert names the property, and the value goes in the same
// transaction. So it is whatever state the record's package is in at the
// open, and whatever the declaration became since the value was written.
func TestALostSecretBelowAPropertysTopIsCleared(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"admitted", "parked: does not admit", "declaration changed"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			name := testdb.Repository(t)
			dsn, root := MigratedDSN(t), t.TempDir()
			svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.CreateRepository(ctx, name); err != nil {
				t.Fatal(err)
			}
			d, err := svc.Dataset(ctx, name)
			if err != nil {
				t.Fatal(err)
			}
			ds := d.(*dataset)
			if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, nestDocs(false)); err != nil {
				t.Fatalf("declare the kind: %v", err)
			}
			inObject, inList := "secret:"+strings.Repeat("a", 32), "secret:"+strings.Repeat("b", 32)
			id := fid("holder")
			mustPutInternal(t, ds, substrate.PutInput{Kind: nestKind, ID: id, Properties: map[string]any{
				"name":   "holder",
				"config": map[string]any{"token": inObject, "label": "kept"},
				"slots":  []any{map[string]any{"refs": []any{"kept", inList}, "note": "kept too"}},
			}})
			sealUnder(t, ds, inObject, nestKind, id)
			sealUnder(t, ds, inList, nestKind, id)
			object := "config"
			if state == "declaration changed" {
				if _, err := ds.ApplyVocabularyDocuments(ctx, substrate.ActorAPI, nestDocs(true)); err != nil {
					t.Fatalf("change the declaration: %v", err)
				}
			}
			leaveBehind(t, ds, inObject, inList)
			_ = svc.Close()

			opts := []Option{WithDataRoot(root)}
			if state == "parked: does not admit" {
				opts = append(opts, WithTestInadmissible(nestPackage))
			}
			svc2, err := OpenForTest(t, ctx, dsn, opts...)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = svc2.Close() })
			d2, err := svc2.Dataset(ctx, name)
			if err != nil {
				t.Fatalf("the open that clears: %v", err)
			}
			ds2 := d2.(*dataset)
			if state == "parked: does not admit" {
				if _, ok := ds2.registry().ByIdentity(nestKind); ok {
					t.Fatal("the package is in the registry, so this case holds nothing of a parked one")
				}
			}
			var raw []byte
			if err := ds2.db.QueryRow(`SELECT props FROM records WHERE kind = $1 AND id = $2`, nestKind, id).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			var props map[string]any
			if err := json.Unmarshal(raw, &props); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{
				"name":  "holder",
				object:  map[string]any{"label": "kept"},
				"slots": []any{map[string]any{"refs": []any{"kept"}, "note": "kept too"}},
			}
			for k, v := range want {
				if !reflect.DeepEqual(props[k], v) {
					t.Errorf("%s reads %v, want %v", k, props[k], v)
				}
			}
			if strings.Contains(string(raw), "secret:") {
				t.Errorf("the record still names a ref: %s", raw)
			}
			if sealedRows(t, ds2, inObject) != 0 || sealedRows(t, ds2, inList) != 0 || secretsLeftBehindMarked(t, ds2) {
				t.Error("the values or the mark stand after the clearing")
			}
			path := vocabulary.RecordPath(nestKind, id)
			wantLeftBehindAlert(t, ds2, path, object, "enter it again")
			wantLeftBehindAlert(t, ds2, path, "slots", "enter it again")
		})
	}
}

// A value whose ref its record does not hold anywhere is not removed by the
// clearing, which removes a value only with the write that takes its ref
// out, and the mark stays, so nothing says the clearing finished while a
// value it could not account for is there. The GC's sweep of values no record
// holds takes it, and the open after lifts the mark.
func TestALostSecretItsRecordDoesNotHoldStaysWithTheMark(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openInternalDataset(t)
	held := putProviderSecret(t, ds, "openai", "sk-held")
	unheld := "secret:" + strings.Repeat("c", 32)
	sealUnder(t, ds, unheld, bindingProviderKind, "openai")
	leaveBehind(t, ds, held, unheld)

	if err := ds.clearSecretsLeftBehind(ctx); err != nil {
		t.Fatal(err)
	}
	if sealedRows(t, ds, held) != 0 {
		t.Error("the value whose ref the record held was not removed")
	}
	if sealedRows(t, ds, unheld) != 1 || !secretsLeftBehindMarked(t, ds) {
		t.Fatal("the clearing removed a value it could not find the ref of, or lifted the mark beside it")
	}
	if n, err := ds.sweepUnheldSealed(ctx); err != nil || n != 1 {
		t.Fatalf("the GC's sweep took %d values (%v), want the one no record holds", n, err)
	}
	if err := ds.clearSecretsLeftBehind(ctx); err != nil {
		t.Fatal(err)
	}
	if secretsLeftBehindMarked(t, ds) {
		t.Fatal("the mark stands with nothing left to clear")
	}
}

// repositoryState is how far a repository's history, fold and recorded take
// reach in the database, and the take's choice.
func repositoryState(t *testing.T, dsn, id string) string {
	t.Helper()
	db := rawDBOf(t, dsn)
	var lines, head, records int64
	if err := db.QueryRow(`SELECT count(*), coalesce(max(seq), 0) FROM changelog WHERE repository = $1`, id).Scan(&lines, &head); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM records WHERE repository = $1`, id).Scan(&records); err != nil {
		t.Fatal(err)
	}
	var take string
	if err := db.QueryRow(`SELECT coalesce(max(archive || '/' || manifest), '') FROM archive_takes WHERE repository_id = $1`, id).Scan(&take); err != nil {
		t.Fatal(err)
	}
	return fmt.Sprintf("%d lines to %d, %d records, take %q", lines, head, records, take)
}

// A take is held to the restore's one admission again before the boot
// removes anything, with the listing as it is then: an archive under another
// host's key, taken while the repository was listed, is refused at the next
// boot once the listing is gone, and the repository is as it was, its choice
// still recorded, and served.
func TestATakeTheListingNoLongerAdmitsRemovesNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srcRoot, id, _ := sourceWithKey(t)
	other, name := otherHostKey(t), testdb.Repository(t)
	root, dsn := copyRepository(t, srcRoot, id), MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(other), WithImportWithoutSecrets(name))
	if err != nil {
		t.Fatalf("the listed restore: %v", err)
	}
	// The repository is not opened, so no pass publishes an archive under
	// this host's key: the newest the take can choose is the source's.
	res, err := svc.(*service).ResolveArchive(ctx, name, false)
	if err != nil || res.Take == "" {
		t.Fatalf("the listed take = %+v, %v, want recorded", res, err)
	}
	_ = svc.Close()
	before := repositoryState(t, dsn, id)

	svc2, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(other))
	if err != nil {
		t.Fatalf("the start without the listing: %v", err)
	}
	t.Cleanup(func() { _ = svc2.Close() })
	if after := repositoryState(t, dsn, id); after != before {
		t.Fatalf("the refused take changed the repository: %s before, %s after", before, after)
	}
	if !strings.Contains(before, res.Take) {
		t.Fatalf("the take's choice is not recorded: %s", before)
	}
	if _, err := svc2.Dataset(ctx, name); err != nil {
		t.Fatalf("the repository the refused take left: %v", err)
	}
}

// The admission refuses a take before its choice is recorded on a host with
// no credential key, as it refuses the restore: a listed repository's new
// data key would be stored unsealed. Nothing is recorded, and the repository
// is as it was.
func TestATakeOnAHostWithNoKeyIsRefusedBeforeItIsRecorded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	srcRoot, id, _ := sourceWithKey(t)
	name := testdb.Repository(t)
	root, dsn := copyRepository(t, srcRoot, id), MigratedDSN(t)
	svc, err := OpenForTest(t, ctx, dsn, WithDataRoot(root))
	if err != nil {
		t.Fatalf("the restore under its own key: %v", err)
	}
	_ = svc.Close()
	before := repositoryState(t, dsn, id)

	keyless, err := OpenForTest(t, ctx, dsn, WithDataRoot(root), WithCredentialKey(""), WithImportWithoutSecrets(name))
	if err != nil {
		t.Fatalf("the keyless operator: %v", err)
	}
	t.Cleanup(func() { _ = keyless.Close() })
	_, err = keyless.(*service).ResolveArchive(ctx, name, false)
	if err == nil || !strings.Contains(err.Error(), "SUBSTRATE_CREDENTIAL_KEY") {
		t.Fatalf("a take on a host with no key = %v, want refused naming SUBSTRATE_CREDENTIAL_KEY", err)
	}
	if after := repositoryState(t, dsn, id); after != before {
		t.Fatalf("the refused take changed the repository: %s before, %s after", before, after)
	}
	if staged, err := os.ReadDir(filepath.Join(root, "archive-take", id)); err == nil && len(staged) > 0 {
		t.Fatalf("the refused take left %d staged copies", len(staged))
	}
}
