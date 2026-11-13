package engine

// A token minted on another's word stands on that token: once it is
// revoked, or its person signed out everywhere, a request it authenticated
// before mints nothing, whenever its mint reaches the transaction.

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

func TestAMintStandsOnTheTokenThatAskedForIt(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := newRaceDataset(t)
	mint := func(ctx context.Context, label string) (substrate.TokenInfo, error) {
		info, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: label})
		return info, err
	}
	asker, err := mint(ctx, "asker")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	onAsker := substrate.WithPrincipal(ctx, asker.ID)
	if _, err := mint(onAsker, "while it stands"); err != nil {
		t.Fatalf("a mint on a standing token: %v", err)
	}
	if _, err := ds.Delete(ctx, substrate.ActorAPI, kindToken, asker.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := mint(onAsker, "after the revoke"); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a mint on a revoked token = %v, want ErrAuth", err)
	}

	other, err := mint(ctx, "other")
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if _, err := ds.endTokens(ctx, substrate.PersonOf(ctx, ds.scope.Repository), nil); err != nil {
		t.Fatalf("sign out everywhere: %v", err)
	}
	if _, err := mint(substrate.WithPrincipal(ctx, other.ID), "after the sign-out"); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a mint on a token a sign-out ended = %v, want ErrAuth", err)
	}
}
