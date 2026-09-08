package commands

// The operator hat against a real database: what it refuses before it touches
// anything. Skips under -short, like every suite that wants a database.

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/testdb"
)

// credentialKey mints a key of the shape the engine demands (ADR 0024):
// standard base64 of 32 random bytes. The database is opened once, so a fresh
// key each time costs nothing.
func credentialKey(t *testing.T) string {
	t.Helper()
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		t.Fatalf("mint a credential key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// An old substratectl against a database a newer server migrated is the
// likelier downgrade: the laptop's binary lags the deployment. `repository
// verify` (the read-only open) and `repository rebuild` (the exclusive open)
// both run the migration runner first, and both refuse the database instead
// of writing to a schema this binary does not know.
func TestOperatorCommandsRefuseADatabaseANewerBinaryMigrated(t *testing.T) {
	dsn := testdb.NewSchema(t)
	root := t.TempDir()
	svc, err := engine.Open(context.Background(), dsn,
		engine.WithDataRoot(root),
		engine.WithKindsDir("../../../kinds/substrate.reamde.dev/core"),
		engine.WithCredentialKey(credentialKey(t)))
	if err != nil {
		t.Fatalf("open the engine: %v", err)
	}
	if err := svc.Close(); err != nil {
		t.Fatalf("close the engine: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open the database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO schema_migrations (version, name, sha256) VALUES (9999, '9999_from_the_future', 'unknown-to-this-binary')`); err != nil {
		t.Fatalf("record a future migration: %v", err)
	}

	h := newHarness(t)
	t.Setenv("SUBSTRATE_CREDENTIAL_KEY", "")
	t.Setenv("SUBSTRATE_DATA_ROOT", root)
	for _, verb := range []string{"verify", "rebuild"} {
		_, _, err := h.run("repository", verb, "geoah", "--dsn", dsn)
		if err == nil {
			t.Fatalf("repository %s opened a database recording migration 9999, which this binary does not carry", verb)
		}
		if !errors.Is(err, engine.ErrDatabaseNewer) {
			t.Fatalf("repository %s failed for another reason than the newer database: %v", verb, err)
		}
	}
}
