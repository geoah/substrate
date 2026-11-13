package engine

// A token is read from the table once and answered from memory after, and
// a revoke or a delete through the engine stops it at once.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestATokenIsCachedAndARevokeStopsItAtOnce(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	mint := func(label string) (substrate.TokenInfo, string) {
		t.Helper()
		info, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: label})
		if err != nil {
			t.Fatalf("mint %s: %v", label, err)
		}
		return info, secret
	}
	authenticates := func(secret string) error {
		_, _, err := ds.svc.Authenticate(ctx, secret)
		return err
	}

	// Answered from memory: a change the process did not make goes unseen
	// until the cache empties.
	cached, secret := mint("cached")
	if err := authenticates(secret); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := ds.db.ExecContext(ctx, `UPDATE records SET deleted_at = now() WHERE kind = $1 AND id = $2`, kindToken, cached.ID); err != nil {
		t.Fatalf("tombstone the token behind the engine: %v", err)
	}
	if err := authenticates(secret); err != nil {
		t.Fatalf("a cached token read the table: %v", err)
	}
	ds.svc.tokens.empty()
	if err := authenticates(secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a token gone from the table after the cache emptied = %v, want ErrAuth", err)
	}

	// A revoke through the engine.
	_, revoked := mint("revoked")
	if err := authenticates(revoked); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if err := ds.svc.RevokeToken(ctx, revoked, ""); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if err := authenticates(revoked); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a revoked token = %v, want ErrAuth at once", err)
	}

	// A delete of the token record.
	deleted, secret := mint("deleted")
	if err := authenticates(secret); err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, kindToken, deleted.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("delete the token: %v", err)
	}
	if err := authenticates(secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a deleted token = %v, want ErrAuth at once", err)
	}

	// A lookup that began before the cache emptied is not kept.
	_, gen := func() (bool, uint64) { _, ok, g := ds.svc.tokens.get("stale", nowUTC()); return ok, g }()
	ds.svc.tokens.empty()
	ds.svc.tokens.put("stale", gen, tokenEntry{at: nowUTC()})
	if _, ok, _ := ds.svc.tokens.get("stale", nowUTC()); ok {
		t.Fatal("a lookup from before the cache emptied was cached")
	}
}
