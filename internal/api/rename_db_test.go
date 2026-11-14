package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/testdb"
)

const renamePassword = "correct-horse-battery-staple"

// After a rename every address that names the repository works under its old
// name and its new, over HTTP on the real engine (decision record 0287,
// validation item 9 of the 1.0 review): a record address in the full-name
// spelling, the records list, an app's token issued before the rename (its
// scopes, its check, a code it exchanges after), a webhook addressed by name,
// with its key and with an app's token, and a sign-in that picks it by
// either name, its person's own name unmoved. The domain document
// speaks for the new name alone: the verification of the old one is not
// claimed, and the repository's record names the new name.
func TestARenamedRepositoryAnswersUnderItsOldNameAndItsNew(t *testing.T) {
	t.Parallel()
	e := newAppDeliveryEnv(t, false, withPassword(renamePassword))
	ctx := context.Background()
	old := e.repository
	renamed := testdb.RepositoryLabel(t) + "-renamed.example.com"
	app := &appEnv{t: t, h: e.h, owner: e.token}
	own := func(name string) string { return name + "/media/photo" }

	photo := mintedID("renamed-photo")
	wantStatus(t, e.do(t, http.MethodPut, "/api/v1/~/media/photo/"+photo, e.token,
		map[string]any{"properties": map[string]any{"caption": "before the rename"}}), http.StatusCreated)

	// An app approved under the old name, one code exchanged before the
	// rename and one after, and a webhook token.
	approved := []string{"read:" + own(old)}
	first := app.park(appRequest(approved[0]))
	before := decodeJSON[tokenResponse](t, app.exchange(tokenForm(codeFrom(t,
		decodeJSON[substrate.AuthorizationAnswer](t, app.approve(first, approved, 30)).Redirect)))).AccessToken
	second := app.park(appRequest(approved[0]))
	pending := codeFrom(t, decodeJSON[substrate.AuthorizationAnswer](t, app.approve(second, approved, 30)).Redirect)
	hook := appScopedToken(t, e.dbEnv, "webhook:"+appHook)
	if _, err := e.ds.MarkDomainVerified(ctx, ""); err != nil {
		t.Fatalf("verify the old name: %v", err)
	}

	rec := e.do(t, http.MethodPost, "/repository/rename", "", substrate.RenameRepositoryRequest{
		Identity: old, Repository: old, Password: renamePassword, Name: renamed,
	})
	wantStatus(t, rec, http.StatusOK)
	names := decodeJSON[substrate.RepositoryNames](t, rec)
	if names.Name != renamed || len(names.FormerNames) != 1 || names.FormerNames[0].Name != old || names.ID != e.ds.Repository().ID {
		t.Fatalf("the rename answered %+v, want %s named %s and holding %s", names, e.ds.Repository().ID, renamed, old)
	}
	exchanged := app.exchange(tokenForm(pending))
	wantStatus(t, exchanged, http.StatusOK)
	after := decodeJSON[tokenResponse](t, exchanged).AccessToken

	for _, name := range []string{old, renamed} {
		for who, bearer := range map[string]string{"the owner": e.token, "the app's token from before": before, "the app's token from after": after} {
			rec := e.do(t, http.MethodGet, "/api/v1/"+own(name)+"/"+photo, bearer, nil)
			wantStatus(t, rec, http.StatusOK)
			if got := decodeJSON[substrate.Record](t, rec); got.Kind != "~/media/photo" || got.Properties["caption"] != "before the rename" {
				t.Errorf("%s reading %s's record by its full name: %+v", who, name, got)
			}
			list := e.do(t, http.MethodGet, "/api/v1/records?filter="+url.QueryEscape(`{"kinds":["`+own(name)+`"]}`), bearer, nil)
			wantStatus(t, list, http.StatusOK)
			if page := decodeJSON[substrate.Page](t, list); len(page.Records) != 1 || page.Records[0].ID != photo {
				t.Errorf("%s listing %s: %d records, want the photo", who, own(name), len(page.Records))
			}
		}
		for _, bearer := range []string{before, after} {
			rec := e.do(t, http.MethodPost, "/api/v1/authorizations/check", bearer, json.RawMessage(checkRequest("read:"+own(name), "[]")))
			wantStatus(t, rec, http.StatusOK)
			if missing := decodeJSON[substrate.AuthorizationCheckResult](t, rec).Scopes.Missing; len(missing) != 0 {
				t.Errorf("the app's check under %s finds %v missing", name, missing)
			}
		}
		wantStatus(t, postHook(e.h, "/webhooks/"+name+"/"+appHook+"/"+appHookKey, nil, nil), http.StatusAccepted)
		wantStatus(t, postHook(e.h, "/webhooks/"+name+"/"+appHook, []byte(`{"n":1}`), map[string]string{
			"Content-Type": "application/json", "Authorization": "Bearer " + hook,
		}), http.StatusAccepted)
		// The sign-in door paces each name's attempts on the handler's clock.
		e.clock.advance(2 * time.Minute)
		login := e.do(t, http.MethodPost, "/login", "", map[string]any{"identity": old, "repository": name, "password": renamePassword})
		wantStatus(t, login, http.StatusCreated)
		if got := decodeJSON[substrate.MintedToken](t, login).Repository; got != renamed {
			t.Errorf("a sign-in under %s answers the repository as %q, want its name now, %s", name, got, renamed)
		}
	}

	domain := e.do(t, http.MethodPost, "/api/v1/repository/domain", e.token, nil)
	wantStatus(t, domain, http.StatusOK)
	if proof := decodeJSON[substrate.DomainProof](t, domain); proof.Name != "_substrate."+renamed || proof.VerifiedAt != nil {
		t.Errorf("the domain document after the rename is %+v, want the record for %s, not verified", proof, renamed)
	}
	repo := e.do(t, http.MethodGet, "/api/v1/substrate.reamde.dev/core/repository/"+names.ID, e.token, nil)
	wantStatus(t, repo, http.StatusOK)
	if props := decodeJSON[substrate.Record](t, repo).Properties; props["name"] != renamed || props["authority"] != renamed || props["domainVerifiedAt"] != nil {
		t.Errorf("the repository's record after the rename: %v", props)
	}
}

// A name taken by another repository never comes back to the one that held
// it, whatever the names clock does, and a webhook token is held to the
// repository its token opens, by id (decision record 0287). A renames old to
// new; 91 days on, B registers old and declares a keyed trigger of the same
// id as A's. With the clock stepped back inside A's hold, A answers to old no
// more, and A's webhook token addressing old is refused as a wrong key is,
// while it still delivers to A's own trigger under new.
func TestATakenFormerNameNeverComesBackWhateverTheClock(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	var clock atomic.Pointer[time.Time]
	at := func(when time.Time) { clock.Store(&when) }
	start := time.Now().UTC().Truncate(time.Second)
	at(start)
	e := newAppDeliveryEnv(t, false, withPassword(renamePassword),
		withEngine(engine.WithTestNamesClock(func() time.Time { return *clock.Load() })))
	old, renamed := e.repository, testdb.RepositoryLabel(t)+"-renamed.example.com"
	a := e.ds.Repository().ID
	hook := appScopedToken(t, e.dbEnv, "webhook:"+appHook)
	wantStatus(t, e.do(t, http.MethodPost, "/repository/rename", "", substrate.RenameRepositoryRequest{
		Identity: old, Repository: old, Password: renamePassword, Name: renamed,
	}), http.StatusOK)

	// B is another person: A's person keeps the sign-in name old.
	at(start.Add(91 * 24 * time.Hour))
	if _, err := e.svc.Register(ctx, substrate.RegisterInput{
		Identity: testdb.RepositoryLabel(t) + "-b.example.com", Repository: old, Password: renamePassword, SkipTOTP: true,
	}); err != nil {
		t.Fatalf("B registers the lapsed name: %v", err)
	}
	b, err := e.svc.Dataset(ctx, old)
	if err != nil {
		t.Fatal(err)
	}
	if b.Repository().ID == a {
		t.Fatalf("%s still opens A after B registered it", old)
	}
	appDeliveryInstall(t, b, false)

	at(start.Add(24 * time.Hour))
	dsA, err := e.svc.Dataset(ctx, a)
	if err != nil {
		t.Fatal(err)
	}
	if info := dsA.Repository(); info.Authority == old || slices.Contains(info.FormerNames, old) {
		names := append([]string{info.Authority}, info.FormerNames...)
		t.Errorf("with the clock back inside the hold, A answers to %v, the name B took among them", names)
	}
	if id, err := e.svc.RepositoryIDOf(ctx, old); err != nil || id != b.Repository().ID {
		t.Errorf("%s resolves to %q (%v), want B", old, id, err)
	}
	bearer := map[string]string{"Content-Type": "application/json", "Authorization": "Bearer " + hook}
	wantStatus(t, postHook(e.h, "/webhooks/"+old+"/"+appHook, []byte(`{"n":1}`), bearer), http.StatusNotFound)
	wantStatus(t, postHook(e.h, "/webhooks/"+renamed+"/"+appHook, []byte(`{"n":1}`), bearer), http.StatusAccepted)
}
