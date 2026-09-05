package config

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

// The credential key is the AES-256 key that unwraps every repository's DEK, so
// its strength must be a property the code checks, not a passphrase an operator
// promises (ADR 0024). Validate accepts standard-base64 of exactly 32 bytes and
// nothing else, and every refusal names the command that generates one.
func TestValidateCredentialKey(t *testing.T) {
	t.Parallel()

	good := base64.StdEncoding.EncodeToString(mustRandom(t, 32))

	refused := map[string]string{
		"empty":              "",
		"short passphrase":   "hunter2",
		"non-base64":         "not base64 at all!!",
		"base64 of 16 bytes": base64.StdEncoding.EncodeToString(mustRandom(t, 16)),
		"base64 of 31 bytes": base64.StdEncoding.EncodeToString(mustRandom(t, 31)),
		"base64 of 33 bytes": base64.StdEncoding.EncodeToString(mustRandom(t, 33)),
		"hex of 32 bytes":    "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff",
	}
	for name, key := range refused {
		t.Run("refuses "+name, func(t *testing.T) {
			t.Parallel()
			err := ValidateCredentialKey(key)
			if err == nil {
				t.Fatalf("ValidateCredentialKey(%q) accepted a bad key", key)
			}
			if !strings.Contains(err.Error(), "openssl rand -base64 32") {
				t.Fatalf("refusal does not name the generator command: %v", err)
			}
			if !strings.Contains(err.Error(), "SUBSTRATE_CREDENTIAL_KEY") {
				t.Fatalf("refusal does not name the variable: %v", err)
			}
		})
	}

	if err := ValidateCredentialKey(good); err != nil {
		t.Fatalf("ValidateCredentialKey rejected base64 of 32 bytes: %v", err)
	}
	data := Data{Root: t.TempDir(), ChangelogSegmentBytes: MinChangelogSegmentBytes}
	if err := (Config{CredentialKey: good, Data: data}).Validate(); err != nil {
		t.Fatalf("Config.Validate rejected a good key: %v", err)
	}
	if err := (Config{CredentialKey: "", Data: data}).Validate(); err == nil {
		t.Fatal("Config.Validate accepted an unset credential key")
	}
}

// The data root is where every repository directory lives, so there is no
// default and no relative form: a relative root would follow the working
// directory and lose the store on a restart from elsewhere.
func TestDataValidate(t *testing.T) {
	t.Parallel()
	abs := t.TempDir()
	refused := map[string]Data{
		"empty root":        {Root: "", ChangelogSegmentBytes: MinChangelogSegmentBytes},
		"relative root":     {Root: "data", ChangelogSegmentBytes: MinChangelogSegmentBytes},
		"dot-relative root": {Root: "./data", ChangelogSegmentBytes: MinChangelogSegmentBytes},
	}
	for name, d := range refused {
		err := d.Validate()
		if err == nil {
			t.Fatalf("%s: Validate accepted %+v", name, d)
		}
		if !strings.Contains(err.Error(), "SUBSTRATE_DATA_ROOT") {
			t.Fatalf("%s: refusal does not name the variable: %v", name, err)
		}
	}
	small := Data{Root: abs, ChangelogSegmentBytes: MinChangelogSegmentBytes - 1}
	err := small.Validate()
	if err == nil {
		t.Fatal("Validate accepted a segment size under 1 MiB")
	}
	if !strings.Contains(err.Error(), "SUBSTRATE_CHANGELOG_SEGMENT_BYTES") {
		t.Fatalf("refusal does not name the variable: %v", err)
	}
	if err := (Data{Root: abs, ChangelogSegmentBytes: MinChangelogSegmentBytes}).Validate(); err != nil {
		t.Fatalf("Validate rejected an absolute root: %v", err)
	}
	if err := (Config{Data: Data{Root: "relative", ChangelogSegmentBytes: MinChangelogSegmentBytes}}).Validate(); err == nil ||
		!strings.Contains(err.Error(), "SUBSTRATE_DATA_ROOT") {
		t.Fatalf("Config.Validate did not refuse a relative data root: %v", err)
	}
}

// LoadData reads the environment: the root is required and the segment size
// defaults to 256 MiB. Setenv, so not parallel.
func TestLoadData(t *testing.T) {
	// t.Setenv restores both at the end; the Unsetenv makes them absent
	// rather than empty, which is the shape a fresh environment has.
	t.Setenv("SUBSTRATE_CHANGELOG_SEGMENT_BYTES", "")
	t.Setenv("SUBSTRATE_DATA_ROOT", "")
	_ = os.Unsetenv("SUBSTRATE_CHANGELOG_SEGMENT_BYTES")
	_ = os.Unsetenv("SUBSTRATE_DATA_ROOT")
	if _, err := LoadData(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_DATA_ROOT") {
		t.Fatalf("LoadData without a root: err = %v, want a refusal naming SUBSTRATE_DATA_ROOT", err)
	}
	t.Setenv("SUBSTRATE_DATA_ROOT", "relative/data")
	if _, err := LoadData(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_DATA_ROOT") {
		t.Fatalf("LoadData with a relative root: err = %v, want a refusal naming SUBSTRATE_DATA_ROOT", err)
	}
	root := t.TempDir()
	t.Setenv("SUBSTRATE_DATA_ROOT", root)
	d, err := LoadData()
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if d.Root != root {
		t.Fatalf("Root = %q, want %q", d.Root, root)
	}
	if d.ChangelogSegmentBytes != 268435456 {
		t.Fatalf("ChangelogSegmentBytes = %d, want the 256 MiB default", d.ChangelogSegmentBytes)
	}
	t.Setenv("SUBSTRATE_CHANGELOG_SEGMENT_BYTES", "4096")
	if _, err := LoadData(); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_CHANGELOG_SEGMENT_BYTES") {
		t.Fatalf("LoadData with a 4 KiB segment: err = %v, want a refusal naming SUBSTRATE_CHANGELOG_SEGMENT_BYTES", err)
	}
}

// The `blobs` column is a migration source, never a store the server runs on:
// asking for it is refused with the one command that still reads it. The
// default is fs under the data root.
func TestBlobsBackend(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	for _, store := range []string{"", "fs"} {
		b, err := (Blobs{Store: store}).Backend(root)
		if err != nil {
			t.Fatalf("Store %q: %v", store, err)
		}
		if b.Name() != "fs" {
			t.Fatalf("Store %q built the %s backend, want fs", store, b.Name())
		}
	}
	_, err := (Blobs{Store: "postgres"}).Backend(root)
	if err == nil {
		t.Fatal("Backend accepted postgres as a runtime store")
	}
	if !strings.Contains(err.Error(), "substratectl blobs migrate --from postgres") {
		t.Fatalf("the refusal does not name the migration: %v", err)
	}
	if _, err := (Blobs{Store: "disk"}).Backend(root); err == nil || !strings.Contains(err.Error(), "SUBSTRATE_BLOB_STORE") {
		t.Fatalf("an unknown store was not refused by name: %v", err)
	}
	if _, err := (Blobs{Store: "fs"}).Backend("relative"); err == nil {
		t.Fatal("the fs backend accepted a relative data root")
	}
}

func mustRandom(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return b
}
