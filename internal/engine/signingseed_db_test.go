package engine_test

// Registration's one disclosure of the signing seed: the seed comes back in
// hex exactly once, the key derived from it is the key the store signs with,
// and nothing after registration can produce it again.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestRegistrationDisclosesSigningSeedOnce(t *testing.T) {
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

	seed, err := hex.DecodeString(res.SigningSeed)
	if err != nil || len(seed) != ed25519.SeedSize {
		t.Fatalf("the signing seed is not %d hex-encoded bytes: %q (%v)",
			ed25519.SeedSize, res.SigningSeed, err)
	}
	key := ed25519.NewKeyFromSeed(seed)
	public := key.Public().(ed25519.PublicKey)
	if got := hex.EncodeToString(public); got != res.SigningPublicKey {
		t.Fatalf("the returned public key %q is not the seed's own %q", res.SigningPublicKey, got)
	}

	// The derived key IS the repository's: the control plane holds its public
	// half, and every changelog signature verifies against it.
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

	// No second showing: the username is taken, so the one call that carries
	// the seed can never run again for this repository, and login returns a
	// token and nothing else.
	if _, err := svc.Register(ctx, substrate.RegisterInput{
		Username: "ada", Password: testPassword,
		TOTPSecret: u.seed, TOTPCode: u.code(t),
	}); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("a second registration did not refuse on the taken username: %v", err)
	}
}
