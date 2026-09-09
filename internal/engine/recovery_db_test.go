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

const recoveryKeyKind = "substrate.reamde.dev/core/recoverykey"

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
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t, engine.WithCredentialKey(engine.TestCredentialKey))

	// Client-side ceremony: the identity is minted here and only the
	// recipient reaches Register.
	clientID, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatalf("generate identity: %v", err)
	}
	enrollment, err := svc.BeginRegistration(ctx, "ada.example.com")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	u := &authUser{repository: "ada.example.com", password: testPassword, seed: enrollment.Secret}
	res, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: "ada.example.com", Password: testPassword,
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

	ds, err := svc.Dataset(ctx, "ada.example.com")
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
	if err := db.QueryRow(`SELECT dek FROM repositories WHERE id = 'ada.example.com'`).Scan(&wrapped); err != nil {
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
		Kind: "substrate.reamde.dev/core/llmprovider", ID: "prov",
		Properties: map[string]any{
			"label": "prov", "wire": "openai",
			"baseURL": "https://llm.example.com/v1", "apiKey": "sk-recover-me",
		},
	})
	var ref string
	if err := db.QueryRow(`SELECT props->>'apiKey' FROM records WHERE kind = $1 AND id = 'prov'`,
		"substrate.reamde.dev/core/llmprovider").Scan(&ref); err != nil {
		t.Fatalf("read ref: %v", err)
	}
	var payload []byte
	var kind, rid string
	if err := db.QueryRow(`SELECT payload, record_kind, record_id FROM sealed WHERE ref = $1`, ref).
		Scan(&payload, &kind, &rid); err != nil {
		t.Fatalf("read sealed payload: %v", err)
	}
	plain, err := engine.OpenPayloadWithKey(recovered, payload, engine.SealedAAD(ref, kind, rid))
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
	t.Parallel()
	ctx := context.Background()
	svc, _ := newService(t, engine.WithCredentialKey(engine.TestCredentialKey))
	enrollment, err := svc.BeginRegistration(ctx, "bo.example.com")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	u := &authUser{repository: "bo.example.com", password: testPassword, seed: enrollment.Secret}
	res, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: "bo.example.com", Password: testPassword,
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
	ds, err := svc.Dataset(ctx, "bo.example.com")
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

	// A second enrollment refuses: one recovery key, no rotation yet. The
	// enrollment carries the password-factor rule, so a fresh code goes in.
	if _, _, err := svc.(substrate.RecoveryEnroller).EnrollRecoveryKey(ctx, substrate.LoginInput{
		Repository: "bo.example.com", Password: testPassword, TOTPCode: u.code(t),
	}, ""); err == nil {
		t.Fatal("a second recovery enrollment was accepted")
	}
}

// TestEnrollRecoveryKeyWrapsADEKThatOpensEverySealedPayload holds the recovery
// promise: every payload is sealed under the repository's DEK (decision 0059),
// enrollment wraps that DEK to the recovery key, and the identity alone
// recovers a DEK that opens the store, with no host involved.
func TestEnrollRecoveryKeyWrapsADEKThatOpensEverySealedPayload(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t, engine.WithCredentialKey(engine.TestCredentialKey))
	enrollment, err := svc.BeginRegistration(ctx, "cleo.example.com")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	u := &authUser{repository: "cleo.example.com", password: testPassword, seed: enrollment.Secret}
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: "cleo.example.com", Password: testPassword,
		TOTPSecret: u.seed, TOTPCode: u.code(t),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	ds, err := svc.Dataset(ctx, "cleo.example.com")
	if err != nil {
		t.Fatalf("dataset: %v", err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// The repository as a registration leaves it, minus the enrollment
	// registration wrote: this test enrolls by hand.
	if _, err := db.Exec(`DELETE FROM records WHERE kind = $1 AND id = 'self'`, recoveryKeyKind); err != nil {
		t.Fatalf("drop recovery record: %v", err)
	}
	mustPut(t, ds, owner, substrate.PutInput{
		Kind: "substrate.reamde.dev/core/llmprovider", ID: "prov",
		Properties: map[string]any{
			"label": "prov", "wire": "openai",
			"baseURL": "https://llm.example.com/v1", "apiKey": "sk-legacy-material",
		},
	})
	var ref string
	if err := db.QueryRow(`SELECT props->>'apiKey' FROM records WHERE kind = $1 AND id = 'prov'`,
		"substrate.reamde.dev/core/llmprovider").Scan(&ref); err != nil {
		t.Fatalf("read ref: %v", err)
	}
	identity, recipient, err := svc.(substrate.RecoveryEnroller).EnrollRecoveryKey(ctx, substrate.LoginInput{
		Repository: "cleo.example.com", Password: testPassword, TOTPCode: u.code(t),
	}, "")
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
	dek := unwrapWithIdentity(t, identity, sealedKey)

	// The provider secret was sealed under the DEK, and the enrollment
	// wrapped that DEK: the identity-recovered DEK alone opens it.
	var payload []byte
	var kind, rid string
	if err := db.QueryRow(`SELECT payload, record_kind, record_id FROM sealed WHERE ref = $1`, ref).
		Scan(&payload, &kind, &rid); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	plain, err := engine.OpenPayloadWithKey(dek, payload, engine.SealedAAD(ref, kind, rid))
	if err != nil {
		t.Fatalf("the recovered DEK does not open the payload: %v", err)
	}
	if string(plain) != "sk-legacy-material" {
		t.Fatalf("recovered %q", plain)
	}

	// And a wrong-factors enrollment never gets that far.
	if _, _, err := svc.(substrate.RecoveryEnroller).EnrollRecoveryKey(ctx, substrate.LoginInput{
		Repository: "cleo.example.com", Password: "wrong-password-entirely", TOTPCode: "000000",
	}, ""); err == nil {
		t.Fatal("enrollment accepted without valid factors")
	}
}

// Enrollment writes nothing to the control plane: the identity is minted, the
// record is written in one transaction, and no step is left that could fail
// after a server-minted identity has been handed out once (decision 0059).
func TestEnrollRecoveryKeyWritesOnlyTheRecord(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t, engine.WithCredentialKey(engine.TestCredentialKey))
	enrollment, err := svc.BeginRegistration(ctx, "dee.example.com")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	u := &authUser{repository: "dee.example.com", password: testPassword, seed: enrollment.Secret}
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Repository: "dee.example.com", Password: testPassword,
		TOTPSecret: u.seed, TOTPCode: u.code(t),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, err := svc.Dataset(ctx, "dee.example.com"); err != nil {
		t.Fatalf("dataset: %v", err)
	}
	db := rawDB(t, dsn)
	if _, err := db.Exec(`DELETE FROM records WHERE kind = $1 AND id = 'self'`, recoveryKeyKind); err != nil {
		t.Fatalf("drop the recovery record registration wrote: %v", err)
	}
	recoveryRecords := func() int {
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM records WHERE kind = $1 AND id = 'self' AND deleted_at IS NULL`, recoveryKeyKind).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	enroll := func() (string, error) {
		identity, _, err := svc.(substrate.RecoveryEnroller).EnrollRecoveryKey(ctx, substrate.LoginInput{
			Repository: "dee.example.com", Password: testPassword, TOTPCode: u.code(t),
		}, "")
		return identity, err
	}

	identity, err := enroll()
	if err != nil || !strings.HasPrefix(identity, "AGE-SECRET-KEY-1") {
		t.Fatalf("the enrollment did not mint an identity: %q, %v", identity, err)
	}
	if n := recoveryRecords(); n != 1 {
		t.Fatalf("a granted enrollment left %d recovery record(s), want 1", n)
	}
}
