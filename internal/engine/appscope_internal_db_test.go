package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A token whose stored grant is damaged fails closed: read as absent, it
// would be full access. Only a MISSING `scopes` property is full access; a
// JSON null, an empty list, a non-string entry or a scope that does not
// parse are all refused at Authenticate, and the listing refuses rather than
// showing the token as full access.
func TestADamagedGrantNeverReadsAsFullAccess(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)
	for name, damage := range map[string]string{
		"null":       `null`,
		"empty":      `[]`,
		"not a list": `"read:*"`,
		"non-string": `[null]`,
		"unparsed":   `["everything"]`,
		"unresolved": `["read:~/tasks/*"]`,
		"auth kind":  `["read:substrate.reamde.dev/core/token"]`,
	} {
		t.Run(name, func(t *testing.T) {
			tok, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{
				Label: "app", Scopes: []string{"read:ada.example.com/tasks/*"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := ds.svc.maint.ExecContext(ctx,
				`UPDATE records SET props = jsonb_set(props, '{scopes}', $1::jsonb) WHERE kind = $2 AND id = $3`,
				damage, kindToken, tok.ID); err != nil {
				t.Fatalf("damage the grant: %v", err)
			}
			if _, _, err := ds.svc.Authenticate(ctx, secret); !errors.Is(err, substrate.ErrAuth) {
				t.Fatalf("a token whose scopes are %s authenticated: %v", damage, err)
			}
			if _, err := ds.Tokens(ctx); !errors.Is(err, substrate.ErrCorrupt) {
				t.Fatalf("the listing with a damaged grant = %v, want it refused as corrupt", err)
			}
			if _, err := ds.Delete(ctx, substrate.ActorAPI, kindToken, tok.ID, substrate.DeleteInput{}); err != nil {
				t.Fatalf("the owner revokes the damaged token: %v", err)
			}
		})
	}
}
