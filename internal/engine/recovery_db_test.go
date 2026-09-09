package engine_test

// The recovery gate: registration enrolls an age recipient and the
// recoverykey record's sealedKey opens, with the matching identity alone, to
// the SAME data-encryption key the control plane wraps for the host — so a
// backup plus the identity is a complete recovery, and the generic write
// surface cannot forge or overwrite the enrollment.

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
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

// TestEnrollRecoveryKeyWrapsADEKThatOpensMigratedPayloads is the pre-recovery
// repository's whole story: its payloads sat sealed under the HOST key, the
// first open under a binary with the DEK-only marker re-keys them under the
// DEK (decision 0059), and enrollment then wraps that DEK to the recovery key
// and re-keys once more in the same commit, so "a backup plus the recovery
// key, no host involved" holds for every payload.
func TestEnrollRecoveryKeyWrapsADEKThatOpensMigratedPayloads(t *testing.T) {
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

	// Simulate the pre-recovery store: drop the enrollment registration
	// wrote, plant a payload sealed under the HOST key, and clear the
	// DEK-only marker, exactly what a pre-DEK release left behind. A marked
	// repository refuses a host-key payload outright (0059), so the planted
	// row is only a legacy one on an unmarked row, and the rewind has to be
	// followed by a reopen: the open dataset already holds the marker.
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
	hostKey := engine.TestCredentialKeyBytes
	if _, err := db.Exec(`UPDATE sealed SET payload = $1 WHERE ref = $2`,
		sealUnder(t, hostKey, []byte("sk-legacy-material")), ref); err != nil {
		t.Fatalf("plant host-key payload: %v", err)
	}
	if _, err := db.Exec(`UPDATE repositories SET sealed_dek_only = false WHERE id = 'cleo.example.com'`); err != nil {
		t.Fatalf("clear the DEK-only marker: %v", err)
	}
	root := engine.DataRootOf(svc)
	_ = svc.Close()
	svc = mustReopen(t, dsn, root)
	if ds, err = svc.Dataset(ctx, "cleo.example.com"); err != nil {
		t.Fatalf("reopen dataset: %v", err)
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

	// The planted host-key payload was re-keyed by the first open and the
	// enrollment wrapped that DEK: the identity-recovered DEK alone opens it.
	var payload []byte
	var kind, rid string
	if err := db.QueryRow(`SELECT payload, record_kind, record_id FROM sealed WHERE ref = $1`, ref).
		Scan(&payload, &kind, &rid); err != nil {
		t.Fatalf("read payload: %v", err)
	}
	plain, err := engine.OpenPayloadWithKey(dek, payload, engine.SealedAAD(ref, kind, rid))
	if err != nil {
		t.Fatalf("enrollment left the payload host-keyed: %v", err)
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

// sealUnder seals raw under an explicit AES-256-GCM key with the store's
// framing: the test's stand-in for a pre-DEK release's host-key writes.
func sealUnder(t *testing.T, key, raw []byte) []byte {
	t.Helper()
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		t.Fatalf("nonce: %v", err)
	}
	out := append([]byte{'s'}, nonce...)
	return aead.Seal(out, nonce, raw, nil)
}

// Enrollment requires the DEK-only marker BEFORE it writes, and writes
// nothing to the control plane: a refused enrollment leaves no record, so a
// retry is not told the slot is taken, and a granted one has no step left
// that could fail after the identity is minted (decision 0059).
func TestEnrollRecoveryKeyRefusesAnUnmarkedDatasetBeforeWriting(t *testing.T) {
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
	ds, err := svc.Dataset(ctx, "dee.example.com")
	if err != nil {
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

	engine.SetDatasetDEKOnly(ds, false)
	if identity, err := enroll(); err == nil || !strings.Contains(err.Error(), "DEK-only") {
		t.Fatalf("an unmarked dataset enrolled: identity minted=%v err=%v", identity != "", err)
	}
	if n := recoveryRecords(); n != 0 {
		t.Fatalf("a refused enrollment left %d recovery record(s)", n)
	}
	var marked bool
	if err := db.QueryRow(`SELECT sealed_dek_only FROM repositories WHERE id = 'dee.example.com'`).Scan(&marked); err != nil || !marked {
		t.Fatalf("the row's marker moved under a refused enrollment: %v %v", marked, err)
	}

	engine.SetDatasetDEKOnly(ds, true)
	// The refused enrollment verified the factors and spent its code before
	// the guard, so the retry needs the next step.
	waitStep(t)
	identity, err := enroll()
	if err != nil || !strings.HasPrefix(identity, "AGE-SECRET-KEY-1") {
		t.Fatalf("the marked dataset did not enroll: %q, %v", identity, err)
	}
	if n := recoveryRecords(); n != 1 {
		t.Fatalf("a granted enrollment left %d recovery record(s), want 1", n)
	}
}
