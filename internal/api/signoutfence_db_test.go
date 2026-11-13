package api

// What a sign out everywhere leaves behind mints nothing: an app's code is
// exchanged standing on the token that approved it, so a code approved
// before the sign-out and exchanged after it is refused, and no token is
// minted (decision 0272).

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

func TestAnAppsCodeIsExchangedStandingOnTheTokenThatApprovedIt(t *testing.T) {
	t.Parallel()
	e := newAuthorizeEnv(t, withoutRepository())
	acct := authRegister(t, e.db, testdb.Repository(t), true)
	ctx := context.Background()
	ds, err := e.db.svc.Dataset(ctx, acct.repository)
	if err != nil {
		t.Fatalf("open dataset: %v", err)
	}
	if err := enginetest.ImportVocabulary(ctx, ds, "people"); err != nil {
		t.Fatalf("import people: %v", err)
	}
	e.owner = acct.token

	// A code exchanged while the token that approved it stands mints.
	e.grant("read:"+personKind, []string{"read:" + personKind})

	// A code approved, then a sign out everywhere, then the exchange.
	id := e.park(appRequest("read:" + personKind))
	rec := e.approve(id, []string{"read:" + personKind}, 30)
	wantStatus(t, rec, http.StatusOK)
	code := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, rec).Redirect)
	body, _ := json.Marshal(map[string]string{"repository": acct.repository, "password": authPassword, "totpCode": e.db.code(t, acct.seed)})
	wantStatus(t, e.do(http.MethodPost, "/signout/everywhere", "", body, "application/json"), http.StatusOK)

	tok := e.exchange(tokenForm(code))
	if tok.Code != http.StatusBadRequest || !strings.Contains(tok.Body.String(), "invalid_grant") {
		t.Fatalf("a code approved before the sign-out exchanged after it answered %d %s, want 400 invalid_grant", tok.Code, tok.Body.String())
	}
	if live, err := ds.Tokens(ctx); err != nil || len(live) != 0 {
		t.Fatalf("after the sign-out and the refused exchange the repository holds %d tokens (%v), want none", len(live), err)
	}
}
