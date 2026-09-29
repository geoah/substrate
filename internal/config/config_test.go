package config

import (
	"crypto/rand"
	"encoding/base64"
	"os"
	"strings"
	"testing"
	"time"
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
	if err := (Config{CredentialKey: good, Data: data, RepositoryConnections: 16, TriggerInterval: 5 * time.Second}).Validate(); err != nil {
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

func mustRandom(t *testing.T, n int) []byte {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatalf("random: %v", err)
	}
	return b
}

func TestRepositoryConnectionsRefusesACapUnderTheFloor(t *testing.T) {
	data := Data{Root: "/srv/substrate", ChangelogSegmentBytes: MinChangelogSegmentBytes}
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	for _, n := range []int{-1, 0, 1, MinRepositoryConnections - 1} {
		if err := (Config{CredentialKey: key, Data: data, RepositoryConnections: n}).Validate(); err == nil ||
			!strings.Contains(err.Error(), "SUBSTRATE_REPOSITORY_CONNECTIONS") {
			t.Fatalf("a cap of %d: err = %v, want a refusal naming SUBSTRATE_REPOSITORY_CONNECTIONS", n, err)
		}
	}
	if err := (Config{CredentialKey: key, Data: data, RepositoryConnections: MinRepositoryConnections, TriggerInterval: 5 * time.Second}).Validate(); err != nil {
		t.Fatalf("the floor itself was refused: %v", err)
	}
}

// The dispatcher tick feeds time.NewTicker, which panics at zero or below, so
// Validate refuses both before the boot reaches it and names the variable.
func TestTriggerIntervalRefusesZeroAndNegative(t *testing.T) {
	t.Parallel()
	data := Data{Root: "/srv/substrate", ChangelogSegmentBytes: MinChangelogSegmentBytes}
	key := "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="
	for _, d := range []time.Duration{0, -time.Second} {
		err := (Config{CredentialKey: key, Data: data, RepositoryConnections: 16, TriggerInterval: d}).Validate()
		if err == nil || !strings.Contains(err.Error(), "SUBSTRATE_TRIGGER_INTERVAL") {
			t.Fatalf("a tick of %s: err = %v, want a refusal naming SUBSTRATE_TRIGGER_INTERVAL", d, err)
		}
	}
	if err := (Config{CredentialKey: key, Data: data, RepositoryConnections: 16, TriggerInterval: time.Second}).Validate(); err != nil {
		t.Fatalf("a one-second tick was refused: %v", err)
	}
}

// The server speaks plain HTTP, so where it listens is the one thing it can
// check about who reads its traffic. A loopback address needs nothing; every
// other one, every interface included, is refused with a message naming the
// address and the setting that admits it, and admitted once that setting is
// on.
func TestListenAddressAdmitsLoopbackAndRefusesTheRestUnlessCleartextIsAllowed(t *testing.T) {
	t.Parallel()
	for host, want := range map[string]string{
		"127.0.0.1": "127.0.0.1:8080",
		"127.0.0.2": "127.0.0.2:8080",
		"::1":       "[::1]:8080",
		"[::1]":     "[::1]:8080",
		"localhost": "127.0.0.1:8080",
		"LocalHost": "127.0.0.1:8080",
	} {
		got, err := (Config{BindAddress: host, Port: "8080"}).ListenAddress()
		if err != nil || got != want {
			t.Errorf("ListenAddress(%q) = %q, %v; want %q with no setting", host, got, err, want)
		}
	}

	for host, want := range map[string]string{
		"0.0.0.0":               "0.0.0.0:8080",
		"":                      ":8080",
		"::":                    "[::]:8080",
		"192.0.2.10":            "192.0.2.10:8080",
		"substrate.example.com": "substrate.example.com:8080",
	} {
		_, err := (Config{BindAddress: host, Port: "8080"}).ListenAddress()
		if err == nil {
			t.Errorf("ListenAddress(%q) admitted a non-loopback bind with no setting", host)
			continue
		}
		for _, named := range []string{"SUBSTRATE_BIND_ADDRESS", "SUBSTRATE_INSECURE_ALLOW_CLEARTEXT=true", want} {
			if !strings.Contains(err.Error(), named) {
				t.Errorf("ListenAddress(%q) refusal does not name %q: %v", host, named, err)
			}
		}

		got, err := (Config{BindAddress: host, Port: "8080", InsecureAllowCleartext: true}).ListenAddress()
		if err != nil || got != want {
			t.Errorf("ListenAddress(%q) with SUBSTRATE_INSECURE_ALLOW_CLEARTEXT = %q, %v; want %q", host, got, err, want)
		}
	}
}

// Unset, the bind is loopback and the escape is off: the default is the one
// that needs nothing in front of it. Setenv, so not parallel.
func TestLoadBindsLoopbackByDefault(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/substrate")
	t.Setenv("SUBSTRATE_DATA_ROOT", t.TempDir())
	t.Setenv("PORT", "8080")
	// Present-but-empty is not unset: the Unsetenv makes both absent, the
	// shape a fresh environment has, and t.Setenv restores them afterwards.
	t.Setenv("SUBSTRATE_BIND_ADDRESS", "")
	t.Setenv("SUBSTRATE_INSECURE_ALLOW_CLEARTEXT", "")
	_ = os.Unsetenv("SUBSTRATE_BIND_ADDRESS")
	_ = os.Unsetenv("SUBSTRATE_INSECURE_ALLOW_CLEARTEXT")
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.BindAddress != "127.0.0.1" || c.InsecureAllowCleartext {
		t.Fatalf("defaults: SUBSTRATE_BIND_ADDRESS = %q, SUBSTRATE_INSECURE_ALLOW_CLEARTEXT = %v; want 127.0.0.1 and false",
			c.BindAddress, c.InsecureAllowCleartext)
	}
	if addr, err := c.ListenAddress(); err != nil || addr != "127.0.0.1:8080" {
		t.Fatalf("default ListenAddress = %q, %v; want 127.0.0.1:8080", addr, err)
	}
}
