package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/authorize"
	"github.com/geoah/substrate/internal/substrate"
)

// traitscopeClient is the app every token here is minted for.
const traitscopeClient = "https://notes.example.org/client.json"

// traitscopeEnv is a repository holding the task and messaging samples, whose
// task and conversation message bind the temporal trait and whose person
// binds nothing.
func traitscopeEnv(t *testing.T, opts ...dbOption) *dbEnv {
	t.Helper()
	return newDBEnv(t, append([]dbOption{withSamples("tasks", "messaging")}, opts...)...)
}

// traitscopeApp mints an app's token holding scopes, as an approval would.
func traitscopeApp(t *testing.T, env *dbEnv, scopes ...string) string {
	t.Helper()
	return env.mint(t, substrate.MintTokenInput{Label: "notes", ClientID: traitscopeClient, ClientName: "Notes", Scopes: scopes})
}

// traitscopeRedeclare applies kind's stored declaration with edit made to it,
// as an upgrade of the kind would.
func traitscopeRedeclare(t *testing.T, env *dbEnv, kind string, edit func(def map[string]any)) {
	t.Helper()
	info, err := env.ds.KindByRef(t.Context(), kind)
	if err != nil {
		t.Fatalf("read %s: %v", kind, err)
	}
	def := maps.Clone(info.Definition)
	edit(def)
	if _, err := env.ds.ApplyVocabularyDocuments(t.Context(), substrate.ActorPerson, []map[string]any{{
		"kind": corePackage + "/kind", "metadata": map[string]any{"id": kind}, "data": def,
	}}); err != nil {
		t.Fatalf("redeclare %s: %v", kind, err)
	}
}

func TestATraitScopeReachesTheKindsThatBindItWhenEachRequestIsChecked(t *testing.T) {
	t.Parallel()
	env := traitscopeEnv(t)
	scopes := "write:implements(" + temporalTrait + ")"
	app := traitscopeApp(t, env, scopes)

	// The task binds the trait with a variant and a remap, which say how it
	// binds it, not what.
	wantStatus(t, env.do(t, http.MethodPut, "/api/v1/"+taskKind+"/"+mintedID("t1"), app, map[string]any{"properties": map[string]any{"name": "x"}}), http.StatusCreated)
	wantStatus(t, env.do(t, http.MethodGet, recordsOf(t, taskKind), app, nil), http.StatusOK)
	wantStatus(t, env.do(t, http.MethodGet, recordsOf(t, conversationKind), app, nil), http.StatusOK)
	// A person binds nothing, so the token does not know the kind.
	wantStatus(t, env.do(t, http.MethodGet, recordsOf(t, personKind), app, nil), http.StatusNotFound)
	if rec := env.do(t, http.MethodPut, "/api/v1/"+personKind+"/"+mintedID("p1"), app, map[string]any{"properties": map[string]any{"name": "x"}}); rec.Code == http.StatusCreated || rec.Code == http.StatusOK {
		t.Fatalf("a trait scope wrote a kind that binds nothing: %d", rec.Code)
	}
	if got := env.get(t, personKind, mintedID("p1")); got != nil {
		t.Fatalf("a trait scope stored a person: %+v", got)
	}

	// Declared to bind the trait after the token was minted, the person is
	// reached from the next request on.
	traitscopeRedeclare(t, env, personKind, func(def map[string]any) { def["traits"] = []any{temporalTrait + "(point)"} })
	wantStatus(t, env.do(t, http.MethodGet, recordsOf(t, personKind), app, nil), http.StatusOK)
	wantStatus(t, env.do(t, http.MethodPut, "/api/v1/"+personKind+"/"+mintedID("p1"), app, map[string]any{"properties": map[string]any{"name": "x"}}), http.StatusCreated)

	// An internal kind is reached by no trait scope, whatever it binds.
	traitscopeRedeclare(t, env, personKind, func(def map[string]any) { def["purpose"] = "internal" })
	wantStatus(t, env.do(t, http.MethodGet, recordsOf(t, personKind), app, nil), http.StatusNotFound)
}

// traitscopeDocuments serves the one client document the approval page reads.
type traitscopeDocuments struct{}

func (traitscopeDocuments) Fetch(_ context.Context, c authorize.Client) (authorize.Document, error) {
	if c.ID != traitscopeClient {
		return authorize.Document{}, fmt.Errorf("no document for %s", c.ID)
	}
	return authorize.Document{
		ClientID: traitscopeClient, RedirectURIs: []string{"https://notes.example.org/callback"},
		Name: "Notes", URI: "https://notes.example.org",
	}, nil
}

func TestTheApprovalPageListsTheKindsATraitScopeReachesAndSaysItGrows(t *testing.T) {
	t.Parallel()
	env := traitscopeEnv(t, withConfig(func(c *Config) {
		c.ClientDocuments = traitscopeDocuments{}
		c.AuthInterval = time.Nanosecond
	}))
	sum := sha256.Sum256([]byte("dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk-the-verifier"))
	q := url.Values{
		"response_type": {"code"}, "client_id": {traitscopeClient}, "redirect_uri": {"https://notes.example.org/callback"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"scope": {"read:implements(" + temporalTrait + ")"}, "state": {"1.0"},
	}
	rec := env.do(t, http.MethodGet, "/oauth/authorize?"+q.Encode(), "", nil)
	wantStatus(t, rec, http.StatusSeeOther)
	id, ok := strings.CutPrefix(rec.Header().Get("Location"), "/authorize/")
	if !ok || id == "" {
		t.Fatalf("authorize redirected to %q, want /authorize/{request}", rec.Header().Get("Location"))
	}
	view := env.do(t, http.MethodGet, "/api/v1/authorizations/"+id, env.token, nil)
	wantStatus(t, view, http.StatusOK)
	page := decodeJSON[substrate.AuthorizationRequest](t, view)
	if len(page.Scopes) != 1 {
		t.Fatalf("scopes = %+v, want the trait scope alone", page.Scopes)
	}
	row := page.Scopes[0]
	if row.Scope != "read:implements("+temporalTrait+")" || row.Pattern != "implements("+temporalTrait+")" || !row.Glob {
		t.Fatalf("row = %+v, want the trait scope, marked as covering kinds added later", row)
	}
	var kinds []string
	for _, k := range row.Kinds {
		kinds = append(kinds, k.Identity)
	}
	if !slices.Contains(kinds, taskKind) || !slices.Contains(kinds, conversationKind) || slices.Contains(kinds, personKind) {
		t.Fatalf("kinds = %v, want the kinds that bind the trait today and no other", kinds)
	}
}

// traitscopeImplementorsPath is the implementors route of the temporal trait,
// named in full as every surface names a trait (decision 0101).
var traitscopeImplementorsPath = "/api/v1/substrate.reamde.dev/core/trait/" + url.PathEscape(temporalTrait) + "/implementors"

// traitscopeImplementors is the implementors route's answer to bearer, sorted,
// leaving out the internal kinds when public is set.
func traitscopeImplementors(t *testing.T, env *dbEnv, bearer string, public ...bool) []string {
	t.Helper()
	rec := env.do(t, http.MethodGet, traitscopeImplementorsPath, bearer, nil)
	wantStatus(t, rec, http.StatusOK)
	var ids []string
	for _, k := range decodeJSON[substrate.OperationalList[substrate.KindInfo]](t, rec).Items {
		if k.Definition == nil {
			t.Fatalf("%s came without its declaration", k.Identity)
		}
		if len(public) > 0 && public[0] && internalKind(k) {
			continue
		}
		ids = append(ids, k.Identity)
	}
	slices.Sort(ids)
	return ids
}

// An app's token is answered the implementors of a trait that its grant
// reaches, each with its declaration, and no other: a trait scope reaches
// every implementor the owner's token lists that is not internal.
func TestAnAppTokenListsTheImplementorsItsGrantReaches(t *testing.T) {
	t.Parallel()
	env := traitscopeEnv(t)
	traitScope := "write:implements(" + temporalTrait + ")"
	app := traitscopeApp(t, env, traitScope)

	owner := traitscopeImplementors(t, env, env.token, true)
	if !slices.Contains(owner, taskKind) || !slices.Contains(owner, conversationKind) || slices.Contains(owner, personKind) {
		t.Fatalf("the owner's implementors %v, want the task and the message and no person", owner)
	}
	if got := traitscopeImplementors(t, env, app); !slices.Equal(got, owner) {
		t.Fatalf("implementors %v, want %v", got, owner)
	}

	// The declaration it answers is the kind's own data map.
	rec := env.do(t, http.MethodGet, traitscopeImplementorsPath, app, nil)
	for _, k := range decodeJSON[substrate.OperationalList[substrate.KindInfo]](t, rec).Items {
		if k.Identity == taskKind && k.Definition["traits"] == nil {
			t.Fatalf("the task's declaration lost its traits: %v", k.Definition)
		}
	}

	// Declared to bind the trait later, the person is listed from the next
	// request on; made internal, no trait scope lists it again.
	traitscopeRedeclare(t, env, personKind, func(def map[string]any) { def["traits"] = []any{temporalTrait + "(point)"} })
	withPerson := append(slices.Clone(owner), personKind)
	slices.Sort(withPerson)
	if got := traitscopeImplementors(t, env, app); !slices.Equal(got, withPerson) {
		t.Fatalf("after the person binds the trait: %v, want %v", got, withPerson)
	}
	traitscopeRedeclare(t, env, personKind, func(def map[string]any) { def["purpose"] = "internal" })
	if got := traitscopeImplementors(t, env, app); !slices.Equal(got, owner) {
		t.Fatalf("an internal implementor was listed: %v", got)
	}

	// A read-only trait scope lists the same kinds; a grant over an unrelated
	// kind lists none.
	readScope := "read:implements(" + temporalTrait + ")"
	if got := traitscopeImplementors(t, env, traitscopeApp(t, env, readScope)); !slices.Equal(got, owner) {
		t.Fatalf("a read-only trait scope's implementors: %v", got)
	}
	unrelated := "write:" + env.repository + "/notes/note"
	if got := traitscopeImplementors(t, env, traitscopeApp(t, env, unrelated)); len(got) != 0 {
		t.Fatalf("an unrelated grant's implementors: %v", got)
	}

	exact := traitscopeApp(t, env, "write:"+taskKind)
	if got := traitscopeImplementors(t, env, exact); !slices.Equal(got, []string{taskKind}) {
		t.Fatalf("an exact scope's implementors: %v", got)
	}
}
