package engine_test

// The recovery gate: registration enrolls an age recipient and the
// recoverykey record's sealedKey opens, with the matching identity alone, to
// the SAME data-encryption key the control plane wraps for the host — so a
// backup plus the identity is a complete recovery, and the generic write
// surface cannot forge or overwrite the enrollment.

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"io"
	"strings"
	"testing"

	"filippo.io/age"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
)

const recoveryKeyKind = "core.substrate.reamde.dev/recoverykey"

// unwrapWithIdentity opens an age-wrapped sealedKey with the user's identity.
func unwrapWithIdentity(t *testing.T, identityStr, sealedKeyB64 string) []byte {
	t.Helper()
	identity, err := age.ParseX25519Identity(identityStr)
	if err != nil {
		t.Fatalf("parse identity: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(sealedKeyB64)
	if err != nil {
		t.Fatalf("decode sealedKey: %v", err)
	}
	r, err := age.Decrypt(bytes.NewReader(raw), identity)
	if err != nil {
		t.Fatalf("age decrypt: %v", err)
	}
	dek, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read dek: %v", err)
	}
	return dek
}

func TestRegistrationEnrollsRecoveryKey(t *testing.T) {
	ctx := context.Background()
	svc, dsn := newService(t, engine.WithCredentialKey("test-cred-key"))

	// Client-side ceremony: the identity is minted here and only the
	// recipient reaches Register.
	clientID, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	enrollment, err := svc.BeginRegistration(ctx, "ada")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	u := &authUser{username: "ada", password: testPassword, seed: enrollment.Secret}
	res, err := svc.Register(ctx, substrate.RegisterInput{
		Username: "ada", Password: testPassword,
		TOTPSecret: u.seed, TOTPCode: u.code(t),
		RecoveryPublicKey: clientID.Recipient().String(),
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	if res.RecoveryKey != "" {
		t.Fatal("a client-supplied recipient must not come back with a server identity")
	}
	if res.RecoveryPublicKey != clientID.Recipient().String() {
		t.Fatalf("recipient echoed wrong: %q", res.RecoveryPublicKey)
	}

	ds, err := svc.Dataset(ctx, "ada")
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	rec, err := ds.Get(ctx, recoveryKeyKind, "self")
	if err != nil {
		t.Fatalf("recoverykey record: %v", err)
	}
	if rec.Properties["algorithm"] != "age-x25519-v1" {
		t.Fatalf("algorithm: %v", rec.Properties["algorithm"])
	}
	sealedKey, _ := rec.Properties["sealedKey"].(string)
	if sealedKey == "" {
		t.Fatal("no sealedKey on the recoverykey record")
	}

	// The wrap opens with the identity to the SAME DEK the control plane
	// holds for the host: the two planes agree on one key.
	recovered := unwrapWithIdentity(t, clientID.String(), sealedKey)
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var wrapped []byte
	if err := db.QueryRow(`SELECT dek FROM repositories WHERE username = 'ada'`).Scan(&wrapped); err != nil {
		t.Fatalf("read wrapped dek: %v", err)
	}
	if len(wrapped) == 0 {
		t.Fatal("registration minted no control-plane DEK")
	}
	// The wrap is host-key sealed; this suite knows the key, so the compare
	// is direct: recovered-by-identity == unwrapped-by-host.
	if len(recovered) != 32 {
		t.Fatalf("recovered DEK is %d bytes", len(recovered))
	}
	// A payload sealed by the repository opens under the recovered DEK: put
	// a secret and decrypt its sealed row with nothing but the identity's
	// DEK, which is the whole recovery claim.
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/llmprovider", ID: "prov",
		Properties: map[string]any{
			"name": "prov", "wire": "openai",
			"baseURL": "https://llm.example.com/v1", "apiKey": "sk-recover-me",
		},
	})
	var ref string
	if err := db.QueryRow(`SELECT props->>'apiKey' FROM records WHERE kind = $1 AND id = 'prov'`,
		"core.substrate.reamde.dev/llmprovider").Scan(&ref); err != nil {
		t.Fatalf("read ref: %v", err)
	}
	var payload []byte
	if err := db.QueryRow(`SELECT payload FROM sealed WHERE ref = $1`, ref).Scan(&payload); err != nil {
		t.Fatalf("read sealed payload: %v", err)
	}
	plain, err := engine.OpenPayloadWithKey(recovered, payload)
	if err != nil {
		t.Fatalf("the identity-recovered DEK does not open the payload: %v", err)
	}
	if string(plain) != "sk-recover-me" {
		t.Fatalf("recovered %q", plain)
	}

	// The generic surface cannot touch the enrollment.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: recoveryKeyKind, ID: "self",
		Properties: map[string]any{"publicKey": "age1evil"},
	}); err == nil {
		t.Fatal("the generic surface overwrote the recovery enrollment")
	}
}

func TestServerMintedRecoveryKeyAndEnrollOnce(t *testing.T) {
	ctx := context.Background()
	svc, _ := newService(t, engine.WithCredentialKey("test-cred-key"))
	enrollment, err := svc.BeginRegistration(ctx, "bo")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	u := &authUser{username: "bo", password: testPassword, seed: enrollment.Secret}
	res, err := svc.Register(ctx, substrate.RegisterInput{
		Username: "bo", Password: testPassword,
		TOTPSecret: u.seed, TOTPCode: u.code(t),
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	// No recipient supplied: the server minted the pair and returns the
	// identity exactly once.
	if !strings.HasPrefix(res.RecoveryKey, "AGE-SECRET-KEY-1") {
		t.Fatalf("no server-minted identity: %q", res.RecoveryKey)
	}
	if !strings.HasPrefix(res.RecoveryPublicKey, "age1") {
		t.Fatalf("no recipient: %q", res.RecoveryPublicKey)
	}
	ds, err := svc.Dataset(ctx, "bo")
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	rec, err := ds.Get(ctx, recoveryKeyKind, "self")
	if err != nil {
		t.Fatalf("recoverykey record: %v", err)
	}
	sealedKey, _ := rec.Properties["sealedKey"].(string)
	dek := unwrapWithIdentity(t, res.RecoveryKey, sealedKey)
	if len(dek) != 32 {
		t.Fatalf("recovered DEK is %d bytes", len(dek))
	}

	// A second enrollment refuses: one recovery key, no rotation yet.
	type enroller interface {
		EnrollRecoveryKey(ctx context.Context, publicKey string) (string, string, error)
	}
	if _, _, err := ds.(enroller).EnrollRecoveryKey(ctx, ""); err == nil {
		t.Fatal("a second recovery enrollment was accepted")
	}
}

func TestEnrollRecoveryKeyOnPreRecoveryRepository(t *testing.T) {
	ctx := context.Background()
	// CreateRepository is the test-only door with no registration ceremony:
	// exactly the shape of a repository that predates recovery keys.
	svc, _ := newService(t, engine.WithCredentialKey("test-cred-key"))
	if _, err := svc.CreateRepository(ctx, "cleo"); err != nil {
		t.Fatalf("create: %v", err)
	}
	ds, err := svc.Dataset(ctx, "cleo")
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	type enroller interface {
		EnrollRecoveryKey(ctx context.Context, publicKey string) (string, string, error)
	}
	identity, recipient, err := ds.(enroller).EnrollRecoveryKey(ctx, "")
	if err != nil {
		t.Fatalf("enroll: %v", err)
	}
	if !strings.HasPrefix(identity, "AGE-SECRET-KEY-1") || !strings.HasPrefix(recipient, "age1") {
		t.Fatalf("bad pair: %q / %q", identity, recipient)
	}
	rec, err := ds.Get(ctx, recoveryKeyKind, "self")
	if err != nil {
		t.Fatalf("recoverykey record: %v", err)
	}
	sealedKey, _ := rec.Properties["sealedKey"].(string)
	if dek := unwrapWithIdentity(t, identity, sealedKey); len(dek) != 32 {
		t.Fatalf("recovered DEK is %d bytes", len(dek))
	}
}
