package config

import (
	"crypto/rand"
	"encoding/base64"
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
	if err := (Config{CredentialKey: good}).Validate(); err != nil {
		t.Fatalf("Config.Validate rejected a good key: %v", err)
	}
	if err := (Config{CredentialKey: ""}).Validate(); err == nil {
		t.Fatal("Config.Validate accepted an unset credential key")
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
