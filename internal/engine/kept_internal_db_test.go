package engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/vocabulary"
)

// keptSecrets is, for each kept directory, the secrets its fixture driver
// sealed (cmd/keptdir), by record and property, each as the SHA-256 of its
// value: what the record holds is compared without the test holding or
// printing the value.
var keptSecrets = map[string]map[string]map[string]string{
	"v0.123.1": {
		"kept.example.com/creds/oauthclient/00000000000000000000000015": {
			"clientSecret": "2f6b56d1ecd4d5a37aa858be17d839d3fdcbae487c3eed19f1ccff40fddb3335",
			"accessToken":  "23049d74665822ab428d2f41df4e91c2d724deacac2ed26b7e70943370fc0518",
		},
	},
}

// Every kept directory's secrets, imported under the current code, open to
// the values its release sealed, read through the engine's resolution of a
// sealed ref (openSecretValue). No consumer of a secret, a function's input
// or the OAuth facility, is run here. The release served them as
// <redacted>, so TestKeptDirectories holds only that each opens. A failure
// names the property and what failed, never a value (finding 4 of step 3's
// second review).
func TestTheKeptDirectoriesSecretsOpenToWhatTheirReleasesSealed(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "testenv", "testdata", "kept")
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			secrets, ok := keptSecrets[e.Name()]
			if !ok {
				t.Fatalf("kept directory %s has no entry in keptSecrets: record the digests of the secrets its fixture driver sealed", e.Name())
			}
			checkKeptSecrets(t, filepath.Join(root, e.Name()), secrets)
		})
	}
}

func checkKeptSecrets(t *testing.T, kept string, secrets map[string]map[string]string) {
	ctx := context.Background()
	raw, err := os.ReadFile(filepath.Join(kept, "fixture.json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Repository    string `json:"repository"`
		CredentialKey string `json:"credentialKey"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := copyTree(filepath.Join(kept, "repositories"), filepath.Join(root, "repositories")); err != nil {
		t.Fatal(err)
	}
	svc, err := OpenForTest(t, ctx, MigratedDSN(t), WithKindsFS(SeedKinds), WithDataRoot(root), WithCredentialKey(fixture.CredentialKey))
	if err != nil {
		t.Fatalf("import the kept directory: %v", err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	d, err := svc.Dataset(ctx, fixture.Repository)
	if err != nil {
		t.Fatal(err)
	}
	ds := d.(*dataset)
	for path, properties := range secrets {
		// The release sealed them under its own name; the conversion moved
		// its own kinds to `~` and sealed them again there (decision 0259).
		i := strings.LastIndexByte(path, '/')
		kind, id := vocabulary.LocalizeRef(path[:i], fixture.Repository), path[i+1:]
		for property, want := range properties {
			var stored string
			if err := ds.db.QueryRowContext(ctx, `SELECT coalesce(props->>$1, '') FROM records WHERE kind = $2 AND id = $3`,
				property, kind, id).Scan(&stored); err != nil {
				t.Errorf("%s %s: the record does not read: %v", path, property, err)
				continue
			}
			if !strings.HasPrefix(stored, secretRefPrefix) {
				t.Errorf("%s %s: not stored as a sealed ref", path, property)
				continue
			}
			got, err := ds.openSecretValue(ctx, stored)
			switch {
			case errors.Is(err, errSecretUnreadable):
				t.Errorf("%s %s: its sealed row is missing or does not open under the data key", path, property)
			case err != nil:
				t.Errorf("%s %s: the sealed row does not read: %v", path, property, err)
			default:
				sum := sha256.Sum256([]byte(got))
				if hex.EncodeToString(sum[:]) != want {
					t.Errorf("%s %s: opens to other bytes than its release sealed", path, property)
				}
			}
		}
	}
}
