package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// A token approved under a scope a later decision retired (decision 0214's
// `declare:`) no longer authenticates, but it is not damage: the owner's
// token list carries it with the scopes it was approved with, so one such
// token never fails the list, and the owner can still revoke it.
func TestARetiredScopeTokenListsButNeverAuthenticates(t *testing.T) {
	t.Parallel()
	ds := openInternalDataset(t)
	ctx := context.Background()
	home := ds.Repository().Authority
	old, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{
		Label: "old app", Scopes: []string{"write:" + home + "/notes/*"},
		ClientID: "https://notes.example.org/client.json", ClientName: "Notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	// The grant as a server before decision 0214 stored it.
	retired := `["declare:` + home + `/notes", "write:` + home + `/notes/*"]`
	if _, err := ds.db.ExecContext(ctx, `UPDATE records SET props = jsonb_set(props, '{scopes}', $1::jsonb) WHERE kind = $2 AND id = $3`,
		retired, kindToken, old.ID); err != nil {
		t.Fatal(err)
	}
	tokens, err := ds.Tokens(ctx)
	if err != nil {
		t.Fatalf("one retired grant failed the token list: %v", err)
	}
	listed := false
	for _, tk := range tokens {
		if tk.ID == old.ID {
			listed = len(tk.Scopes) == 2 && tk.Scopes[0] == "declare:"+home+"/notes" && tk.ClientName == "Notes"
		}
	}
	if !listed {
		t.Fatalf("the retired grant was not listed with the scopes it was approved with: %+v", tokens)
	}
	if _, _, err := ds.svc.Authenticate(ctx, secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("a retired grant authenticated: %v", err)
	}
}
