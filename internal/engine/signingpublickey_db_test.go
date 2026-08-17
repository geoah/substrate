package engine_test

// What registration discloses about signing: the PUBLIC key, in hex, and no
// private key material. The key that comes back is the one the store signs
// with, so `repository verify --expect-public-key` can be pinned to it, and the
// seed that mints those signatures never leaves the server.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestRegistrationDisclosesTheSigningPublicKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, dsn := newService(t)

	enrollment, err := svc.BeginRegistration(ctx, "ada")
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	u := &authUser{username: "ada", password: testPassword, seed: enrollment.Secret}
	res, err := svc.Register(ctx, substrate.RegisterInput{
		Username: "ada", Password: testPassword,
		TOTPSecret: u.seed, TOTPCode: u.code(t),
	})
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	public, err := hex.DecodeString(res.SigningPublicKey)
	if err != nil || len(public) != ed25519.PublicKeySize {
		t.Fatalf("the signing public key is not %d hex-encoded bytes: %q (%v)",
			ed25519.PublicKeySize, res.SigningPublicKey, err)
	}

	// The returned key IS the repository's: the control plane holds the same
	// bytes, and every changelog signature verifies against it.
	db := rawDB(t, dsn)
	var repoID string
	var storedPublic []byte
	if err := db.QueryRow(
		`SELECT id, signing_public FROM repositories WHERE username = 'ada'`).
		Scan(&repoID, &storedPublic); err != nil {
		t.Fatalf("read the control-plane row: %v", err)
	}
	if !bytes.Equal(storedPublic, public) {
		t.Fatalf("the stored public key %x is not the one the seed derives %x", storedPublic, public)
	}
	rows, err := db.Query(
		`SELECT seq, hash, sig FROM changelog WHERE repository = $1 ORDER BY seq`, repoID)
	if err != nil {
		t.Fatalf("read the changelog: %v", err)
	}
	defer func() { _ = rows.Close() }()
	entries := 0
	for rows.Next() {
		var seq int64
		var hash, sig []byte
		if err := rows.Scan(&seq, &hash, &sig); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if !ed25519.Verify(public, hash, sig) {
			t.Fatalf("seq %d does not verify against the key the returned seed derives", seq)
		}
		entries++
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	if entries == 0 {
		t.Fatal("registration left an empty changelog; nothing was verified")
	}

	// Registering the same username again refuses, so the response that carries
	// the public key is a one-off per repository either way.
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Username: "ada", Password: testPassword,
		TOTPSecret: u.seed, TOTPCode: u.code(t),
	}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a second registration did not refuse on the taken username: %v", err)
	}
}
