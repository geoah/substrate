package engine

// Revoking a token is the owner's alone (issue #135). The loader refuses the
// token kind in `permissions.writes`; these tests hold the engine to the same
// answer for every bundle-tier hand, whatever its grant names, so a
// declaration admitted before that refusal cannot lock the owner out either.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// mustAuthenticate asserts a token secret still signs a request in.
func mustAuthenticate(t *testing.T, ds *dataset, secret, what string) {
	t.Helper()
	if _, _, err := ds.svc.Authenticate(context.Background(), secret); err != nil {
		t.Fatalf("%s: the token no longer authenticates: %v", what, err)
	}
}

// mustNotAuthenticate asserts a revoked token secret is refused as spent.
func mustNotAuthenticate(t *testing.T, ds *dataset, secret, what string) {
	t.Helper()
	if _, _, err := ds.svc.Authenticate(context.Background(), secret); !errors.Is(err, substrate.ErrAuth) {
		t.Fatalf("%s: a revoked token must fail authentication, got %v", what, err)
	}
}

// A function whose grant names the token kind, as a manifest stored before the
// loader refused it carries, cannot revoke a token: its delete effect is
// refused forbidden (a 403 at the HTTP door), the transaction rolls back, and
// the token still signs in. Every other bundle-tier hand that reaches the
// delete path (the agent `write` built-in and a policy judge's accept both
// stamp a ceiling), a purge included, gets the same answer. The owner's own
// revoke, from each door actor, still works.
func TestOnlyTheOwnerRevokesAToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds := openCoreDataset(t)

	tok, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "laptop"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}

	fn := &vocabulary.Function{
		Name: "revoker", Package: "legacy.test.dev/legacy",
		Caps: vocabulary.FunctionCaps{Emit: []string{kindToken}},
	}
	effects, err := ds.decodeEffects(fn, []any{map[string]any{
		"action": "delete", "kind": kindToken, "id": tok.ID,
	}})
	if err != nil {
		t.Fatalf("the grant this binary refuses at load must still decode here: %v", err)
	}
	err = ds.inTx(ctx, substrate.Actor(fn.Actor()), false, func(tx *txn) error {
		return tx.applyEffects(fn.Caps.Emit, effects)
	})
	if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), "may not write "+kindToken) {
		t.Fatalf("a function's delete of a token must be refused forbidden, got %v", err)
	}
	mustAuthenticate(t, ds, secret, "after the function's delete")

	hands := map[string]struct {
		actor   substrate.Actor
		ceiling *effectCeiling
		purge   bool
	}{
		"the agent write built-in": {
			actor:   "agent:legacy.test.dev:legacy:janitor",
			ceiling: &effectCeiling{emit: []string{kindToken}},
		},
		"a policy judge's accept": {
			actor:   "policy:tidy-tokens",
			ceiling: &effectCeiling{emit: []string{kindToken}, policyDecision: true},
		},
		"a bundle-tier purge": {
			actor:   "agent:legacy.test.dev:legacy:janitor",
			ceiling: &effectCeiling{emit: []string{kindToken}},
			purge:   true,
		},
	}
	for name, h := range hands {
		_, err := ds.deleteBounded(ctx, h.actor, kindToken, tok.ID, substrate.DeleteInput{Purge: h.purge}, h.ceiling)
		if !errors.Is(err, substrate.ErrForbidden) || !strings.Contains(err.Error(), "may not write "+kindToken) {
			t.Fatalf("%s: a bundle-tier delete of a token must be refused forbidden, got %v", name, err)
		}
		mustAuthenticate(t, ds, secret, "after "+name)
	}

	// The owner revokes from every door: the generic DELETE (`api`), the
	// console's sign-out and `substratectl logout`.
	for _, door := range []substrate.Actor{substrate.ActorPerson, substrate.ActorPerson, substrate.ActorPerson} {
		tok, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "door " + string(door)})
		if err != nil {
			t.Fatalf("mint for %s: %v", door, err)
		}
		if _, err := ds.Delete(ctx, door, kindToken, tok.ID, substrate.DeleteInput{}); err != nil {
			t.Fatalf("%s: the owner's revoke was refused: %v", door, err)
		}
		mustNotAuthenticate(t, ds, secret, string(door))
	}
	if _, err := ds.Delete(ctx, substrate.ActorPerson, kindToken, tok.ID, substrate.DeleteInput{}); err != nil {
		t.Fatalf("the owner's revoke of the token the hands could not reach: %v", err)
	}
	mustNotAuthenticate(t, ds, secret, "the first token")
}

// An agent with only the `propose` grant cannot propose a token's delete: a
// proposal's target is a read, and no agent reads sign-in material. Nor can
// a judge's accept revoke one: over a request to revoke a token, judged as
// an agent's proposal is, the judge-bearing policy's accept is refused,
// since a policy's decision writes no sign-in material, and escalates into
// the owner's review with the refusal as its note, and the token still
// signs in. The owner's own accept of the same request then revokes it.
func TestAJudgedProposalCannotRevokeAToken(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	tok, secret, err := ds.MintToken(ctx, substrate.MintTokenInput{Label: "laptop"})
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	putPolicy(t, ds, "judged-tokens", map[string]any{
		"selector":   map[string]any{"kinds": []any{kindToken}},
		"action":     "gate",
		"judge":      crewPackage + "/verdictor",
		"criteria":   "revoke stale tokens",
		"mode":       "enforce",
		"autoAccept": 0.9,
	})
	fake.script("stoic",
		fakeTurn{calls: []fakeCall{{"propose", `{"op":"delete","kind":"` + kindToken + `","target":"` + tok.ID + `","rationale":"stale"}`}}},
		fakeTurn{content: "proposed."},
	)
	fake.script("vjudge",
		fakeTurn{content: `{"verdict":"accept","confidence":0.97,"rationale":"stale token"}`},
	)
	res, err := ds.CallAgent(ctx, crewPackage+"/stoic", "revoke the laptop token")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if rows := toolRows(t, ds, res.Thread); len(rows) != 1 || !strings.Contains(fmt.Sprint(rows[0]["content"]), "not in the reads allowlist") {
		t.Fatalf("an agent's proposal of a token's delete: %v, want refused as a read it may not make", rows)
	}
	if _, err := ds.Put(ctx, substrate.ActorPerson, substrate.PutInput{Kind: vocabulary.KindRecordPatchRequest, ID: fid("revoke-laptop"), Properties: map[string]any{
		"op": "delete", "rationale": "stale",
		"target": map[string]any{vocabulary.ReferenceValueKey: vocabulary.RecordPath(kindToken, tok.ID)},
	}}); err != nil {
		t.Fatalf("the request to revoke the token: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	verdict, rule, err := ds.policyVerdict(ctx, kindToken, policyOpDelete, crewPackage+"/stoic")
	if err != nil || verdict != policyGate {
		t.Fatalf("the policy over tokens: %s %v", verdict, err)
	}
	ds.maybeJudge(req.ID, rule)
	audit := judgedAnnotation(t, ds, req.ID)
	if audit["outcome"] != judgedEscalated {
		t.Fatalf("the judge's accept of a token delete must escalate, audit: %+v", audit)
	}
	if note, _ := audit["note"].(string); !strings.Contains(note, "may not write "+kindToken) {
		t.Fatalf("the escalation must carry the refusal, note: %q", note)
	}
	mustAuthenticate(t, ds, secret, "after the judged accept")

	fresh, err := ds.Get(ctx, vocabulary.KindRecordPatchRequest, req.ID)
	if err != nil || fresh.Properties["decision"] != "proposed" {
		t.Fatalf("request after the escalation: %+v %v", fresh, err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorPerson, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &fresh.Version,
	}); err != nil {
		t.Fatalf("the owner's accept of the token delete: %v", err)
	}
	mustNotAuthenticate(t, ds, secret, "after the owner's accept")
}
