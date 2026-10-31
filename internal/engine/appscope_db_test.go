package engine_test

// An app's token (decision 0194) writes at the owner tier, so property
// ownership treats it as the owner's hand, but the actions the owner alone
// takes refuse it: revoking a token, and any write of an account or a bundle
// input, the connection records the owner manages.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func TestAnAppsTokenWritesNoAccountAndRevokesNoToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)
	account := mustPut(t, ds, owner, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "geo@example.com", "enabledMail": true},
	})
	app := substrate.WithScopes(ctx, []string{"write:*"})

	refused := func(what string, err error) {
		t.Helper()
		if !errors.Is(err, substrate.ErrForbidden) {
			t.Fatalf("%s with an app's token = %v, want forbidden", what, err)
		}
	}
	_, err := ds.Patch(app, substrate.ActorAPI, mbAccountType, account.ID, substrate.PatchInput{
		Properties: map[string]any{"enabledMail": false},
	})
	refused("patching an account", err)
	_, err = ds.Put(app, substrate.ActorAPI, substrate.PutInput{
		Kind: mbAccountType, Properties: map[string]any{"address": "eve@example.com"},
	})
	refused("creating an account", err)
	_, err = ds.Delete(app, substrate.ActorAPI, mbAccountType, account.ID, substrate.DeleteInput{})
	refused("deleting an account", err)
	_, err = ds.Put(app, substrate.ActorAPI, substrate.PutInput{
		Kind: mbConfigType, Properties: map[string]any{"clientId": "x"},
	})
	refused("creating a bundle input", err)

	// The owner's own hand, on the same records, is unchanged.
	if _, err := ds.Patch(ctx, owner, mbAccountType, account.ID, substrate.PatchInput{
		Properties: map[string]any{"enabledMail": false},
	}); err != nil {
		t.Fatalf("the owner's patch of the account: %v", err)
	}

	// An ordinary kind the app's write reaches still writes.
	if _, err := ds.Put(app, substrate.ActorAPI, substrate.PutInput{
		Kind: mbItemType, Properties: map[string]any{"name": "from the app"},
	}); err != nil {
		t.Fatalf("an app's write of an ordinary kind: %v", err)
	}

	tok, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "laptop"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = ds.Delete(app, substrate.ActorAPI, "substrate.reamde.dev/core/token", tok.ID, substrate.DeleteInput{})
	refused("revoking a token", err)
	if err == nil || !strings.Contains(err.Error(), "/oauth/revoke") {
		t.Fatalf("the refusal = %v, want it to name the app's own way out", err)
	}
	if _, err := ds.Delete(ctx, owner, "substrate.reamde.dev/core/token", tok.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("the owner's revoke: %v", err)
	}
}

// A scoped token round-trips through the record: the mint stores the
// grant and the app, Authenticate and the listing read them back, and a
// token without scopes is still full access.
func TestAScopedTokenCarriesItsGrantAndItsApp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	svc, ds := newDataset(t)
	scopes := []string{"read:ada.example.com/tasks/*", "write:ada.example.com/tasks/task"}
	info, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{
		Label: "notes.example.org", Scopes: scopes,
		ClientID: "https://notes.example.org/client.json", ClientName: "Notes",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, got, err := svc.Authenticate(ctx, secret)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Scoped() || strings.Join(got.Scopes, " ") != strings.Join(scopes, " ") ||
		got.ClientID != "https://notes.example.org/client.json" || got.ClientName != "Notes" || got.ID != info.ID {
		t.Fatalf("authenticated %+v", got)
	}
	list, err := ds.Tokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var listed substrate.TokenInfo
	for _, tk := range list {
		if tk.ID == info.ID {
			listed = tk
		}
	}
	if len(listed.Scopes) != 2 || listed.ClientName != "Notes" {
		t.Fatalf("listed %+v", listed)
	}

	_, plain, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "cli"})
	if err != nil {
		t.Fatal(err)
	}
	if _, got, err := svc.Authenticate(ctx, plain); err != nil || got.Scoped() {
		t.Fatalf("a token without scopes = %+v, %v: want full access", got, err)
	}

	for name, in := range map[string]substrate.MintTokenInput{
		"empty":      {Label: "x", Scopes: []string{}},
		"unresolved": {Label: "x", Scopes: []string{"read:~/tasks/*"}},
		"auth kind":  {Label: "x", Scopes: []string{"read:substrate.reamde.dev/core/token"}},
		"garbage":    {Label: "x", Scopes: []string{"everything"}},
		"write core": {Label: "x", Scopes: []string{"write:substrate.reamde.dev/core/trigger"}},
	} {
		if _, _, err := ds.MintToken(ctx, in); !errors.Is(err, substrate.ErrValidation) {
			t.Errorf("%s: mint = %v, want refused", name, err)
		}
	}
}

// An accept moves a change request into the state that applies its diff to
// the target it names, which no scope bounds: an app's token never accepts
// one, whatever its scopes, while the owner's accept of the same request
// lands.
func TestAnAppsTokenAcceptsNoChangeRequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "samples.substrate.reamde.dev/tasks/task", Properties: map[string]any{"name": "before"},
	})
	req := mustPut(t, ds, owner, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchRequest, ID: "req-1",
		Properties: map[string]any{
			"diff":   map[string]any{"properties": map[string]any{"name": "after"}},
			"target": vocabulary.RecordPath(task.Kind, task.ID),
		},
	})
	app := substrate.WithScopes(ctx, []string{"write:*"})
	version := req.Version
	accept := substrate.PatchInput{Properties: map[string]any{"decision": "accepted"}, IfVersion: &version}
	if _, err := ds.Patch(app, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, "req-1", accept); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("an app's accept = %v, want forbidden", err)
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.Properties["name"] != "before" {
		t.Fatalf("the refused accept still applied its diff: %v", got.Properties)
	}
	if _, err := ds.Patch(ctx, owner, vocabulary.KindRecordPatchRequest, "req-1", accept); err != nil {
		t.Fatalf("the owner's accept: %v", err)
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.Properties["name"] != "after" {
		t.Fatalf("the owner's accept did not apply: %v", got.Properties)
	}
}

// A bundle input's settings carry no `writer:` role, so the gate is what
// refuses an app's patch of one; and a `writer: owner` property on an
// ordinary kind is the owner's alone.
func TestAnAppsTokenPatchesNoBundleInputAndNoOwnerProperty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := installMailBundle(t)
	cfg := mustPut(t, ds, owner, substrate.PutInput{Kind: mbConfigType, Properties: map[string]any{"clientId": "owner"}})
	app := substrate.WithScopes(ctx, []string{"write:*"})
	if _, err := ds.Patch(app, substrate.ActorAPI, mbConfigType, cfg.ID, substrate.PatchInput{
		Properties: map[string]any{"clientId": "app"},
	}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("an app's patch of a bundle input = %v, want forbidden", err)
	}

	const pkg = "owned.example.com/owned"
	if _, err := ds.ApplyVocabularyDocuments(ctx, owner, []map[string]any{
		vocabulary.PackageManifest(pkg, 1),
		vocabulary.KindManifest(pkg, map[string]any{"singular": "note"}, map[string]any{"properties": map[string]any{
			"text":     map[string]any{"type": "string"},
			"verified": map[string]any{"type": "bool", "writer": "owner"},
		}}),
	}); err != nil {
		t.Fatalf("declare the kind: %v", err)
	}
	kind := pkg + "/note"
	if _, err := ds.Put(app, substrate.ActorAPI, substrate.PutInput{Kind: kind, Properties: map[string]any{"text": "x"}}); err != nil {
		t.Fatalf("an app's write of a property with no role: %v", err)
	}
	if _, err := ds.Put(app, substrate.ActorAPI, substrate.PutInput{Kind: kind, Properties: map[string]any{"verified": true}}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("an app's write of a writer: owner property = %v, want forbidden", err)
	}
	if _, err := ds.Put(ctx, owner, substrate.PutInput{Kind: kind, Properties: map[string]any{"verified": true}}); err != nil {
		t.Fatalf("the owner's write of it: %v", err)
	}
}

func TestAnAppsTokenStartsNoProviderConnect(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds, _, account := installOAuthBundle(t)
	path := vocabulary.RecordPath(account.Kind, account.ID)
	if _, err := ds.StartOAuth(substrate.WithScopes(ctx, []string{"write:*"}), substrate.ActorAPI, path); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("an app's provider connect = %v, want forbidden", err)
	}
	if _, err := ds.StartOAuth(ctx, substrate.ActorAPI, path); err != nil {
		t.Fatalf("the owner's provider connect: %v", err)
	}
}

// A list cursor carries the changelog head for the next page's handoff, and
// the head counts the whole repository's writes: an app's cursor carries none.
func TestAnAppsListCursorCarriesNoHead(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	const kind = "samples.substrate.reamde.dev/tasks/task"
	for range 3 {
		mustPut(t, ds, owner, substrate.PutInput{Kind: kind, Properties: map[string]any{"name": "t"}})
	}
	headOf := func(ctx context.Context) float64 {
		t.Helper()
		page, err := ds.List(ctx, substrate.Query{Filter: substrate.Filter{Kinds: []string{kind}}, First: 1})
		if err != nil || page.Cursor == "" {
			t.Fatalf("list = %v, cursor %q", err, page.Cursor)
		}
		raw, err := base64.RawURLEncoding.DecodeString(page.Cursor)
		if err != nil {
			t.Fatal(err)
		}
		var k map[string]any
		if err := json.Unmarshal(raw, &k); err != nil {
			t.Fatal(err)
		}
		h, _ := k["h"].(float64)
		return h
	}
	if h := headOf(ctx); h == 0 {
		t.Fatal("the owner's cursor carries no head: the test proves nothing")
	}
	if h := headOf(substrate.WithScopes(ctx, []string{"read:" + kind})); h != 0 {
		t.Fatalf("an app's cursor carries head %v", h)
	}
}

// A `decide:` scope (decision 0195) opens the accept for the one change
// request the API named after holding it to the grant: the decision is the
// app's, it may adjust what it accepts as the owner may, and the accept
// applies the diff. A mark naming another request opens nothing, and the
// accept's own write still meets the owner-only gates.
func TestAnAppsDecisionAcceptsTheOneRequestItNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	const taskKind = "samples.substrate.reamde.dev/tasks/task"
	task := mustPut(t, ds, owner, substrate.PutInput{Kind: taskKind, Properties: map[string]any{"name": "before"}})
	request := func(id string, props map[string]any) *substrate.Record {
		return mustPut(t, ds, owner, substrate.PutInput{Kind: vocabulary.KindRecordPatchRequest, ID: id, Properties: props})
	}
	patchProps := map[string]any{
		"diff":   map[string]any{"properties": map[string]any{"name": "after"}},
		"target": vocabulary.RecordPath(task.Kind, task.ID),
	}
	accepted := request("req-accept", patchProps)
	other := request("req-other", patchProps)
	app := substrate.WithScopes(ctx, []string{"decide:*"})
	actor := substrate.AppActor("notes.example.org")
	path := func(id string) string { return vocabulary.RecordPath(vocabulary.KindRecordPatchRequest, id) }
	decide := func(mark, id, decision string, version int64, extra, notes map[string]any) (*substrate.Record, error) {
		props := map[string]any{"decision": decision}
		for k, v := range extra {
			props[k] = v
		}
		return ds.Patch(substrate.WithDecision(app, path(mark)), actor, vocabulary.KindRecordPatchRequest, id,
			substrate.PatchInput{Properties: props, Annotations: notes, IfVersion: &version})
	}

	if _, err := decide("req-other", "req-accept", "accepted", accepted.Version, nil, nil); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("an accept under a mark naming another request = %v, want forbidden", err)
	}
	adjusted := map[string]any{"adjustedDiff": map[string]any{"properties": map[string]any{"name": "adjusted"}}}
	note := map[string]any{"app:notes.example.org/note": "looks right"}
	if _, err := decide("req-accept", "req-accept", "accepted", accepted.Version, adjusted, note); err != nil {
		t.Fatalf("the app's accept of the request it decides: %v", err)
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.Properties["name"] != "adjusted" {
		t.Fatalf("the accept applied %v, want the adjusted diff", got.Properties)
	}
	changes, err := ds.Changes(ctx, 0, substrate.ChangeFilter{Kinds: []string{vocabulary.KindRecordPatchRequest}, RecordID: "req-accept"}, 100)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(changes); n == 0 || changes[n-1].Actor != actor {
		t.Fatalf("the decision's changelog rows = %+v, want the last under %s", changes, actor)
	}
	// Rejecting needs no gate, and the app's own namespace is the only one
	// its notes may use.
	if _, err := decide("req-other", "req-other", "rejected", other.Version, nil, map[string]any{"owner/note": "x"}); !errors.Is(err, substrate.ErrForbidden) {
		t.Fatalf("an app's note in the owner's namespace = %v, want forbidden", err)
	}
	if _, err := decide("req-other", "req-other", "rejected", other.Version, nil, nil); err != nil {
		t.Fatalf("the app's rejection: %v", err)
	}

	// The accept writes the target with the app's token behind it, so a
	// change only the owner may make stays the owner's to accept.
	tok, _, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "laptop"})
	if err != nil {
		t.Fatal(err)
	}
	revoke := request("req-revoke", map[string]any{
		"op": "delete", "target": vocabulary.RecordPath("substrate.reamde.dev/core/token", tok.ID),
	})
	if _, err := decide("req-revoke", "req-revoke", "accepted", revoke.Version, nil, nil); err == nil {
		t.Fatal("an app's accept revoked a token")
	}
	list, err := ds.Tokens(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(list, func(tk substrate.TokenInfo) bool { return tk.ID == tok.ID }) {
		t.Fatal("the refused accept revoked the token")
	}
}
