package engine

// The policy door (docs/plans/thread-interactions.md phase 3): owner rules
// gate, refuse or allow bundle-tier writes inside the emit ceiling; a gated
// write converts into a recordpatchrequest with the thread and the governing
// policy stamped; the author's confirmation floor gates whatever policy
// says; and neither the door's rules nor a gated (or self-proposed) request
// are a bundle actor's to touch.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

func putPolicy(t *testing.T, ds *dataset, id string, props map[string]any) *substrate.Record {
	t.Helper()
	e, err := ds.Put(context.Background(), substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: id, Properties: props,
	})
	if err != nil {
		t.Fatalf("put policy %s: %v", id, err)
	}
	return e
}

func TestPolicyGatesAMutateIntoARequest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	policy := putPolicy(t, ds, "gate-widgets", map[string]any{
		"selector": map[string]any{
			"kinds":  []any{crewAuthority + "/widget"},
			"agents": []any{crewAuthority + "/editor"},
		},
		"action": "gate",
	})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/widget", id: "w-held", properties: {name: "wanted"}}) { id } }`,
		})}}},
		fakeTurn{content: "held, waiting."},
		// The decision's resume.
		fakeTurn{content: "landed at last."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/editor", "make a widget")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// The write did NOT land; the tool result is honest about the hold.
	if _, err := ds.Get(ctx, crewAuthority+"/widget", "w-held"); err == nil {
		t.Fatal("a gated write landed")
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] == true {
		t.Fatal("a gated mutate reported ok")
	}
	if content, _ := tool["content"].(string); !strings.Contains(content, "held for review as") {
		t.Fatalf("the result does not name the hold: %s", content)
	}
	// The request carries the whole story: op create (the target was absent),
	// the thread, the governing policy pinned at its revision.
	req := onlyPatchRequest(t, ds)
	if req.Properties["op"] != "create" || req.Properties["targetId"] != "w-held" {
		t.Fatalf("request: %+v", req.Properties)
	}
	if got := req.Properties["thread"]; got != vocabulary.RecordPath(typeThread, res.Thread) {
		t.Fatalf("request thread = %v", got)
	}
	if got := req.Properties["policy"]; got != vocabulary.RecordPath(vocabulary.KindRecordPatchPolicy, policy.ID) {
		t.Fatalf("request policy = %v", got)
	}
	if rev, _ := anyFloat(req.Properties["policyRevision"]); int64(rev) != policy.Version {
		t.Fatalf("request policyRevision = %v, policy version %d", req.Properties["policyRevision"], policy.Version)
	}
	// The gated request's changelog entry rides the tool row.
	if entry, ok := changesName(changesOfRow(tool), vocabulary.KindRecordPatchRequest); !ok || entry["id"] != req.ID {
		t.Fatalf("the tool row carries no request change: %+v", tool["changes"])
	}
	// The owner accepts; the held write lands; the thread hears it.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got, err := ds.Get(ctx, crewAuthority+"/widget", "w-held"); err != nil || got.Properties["name"] != "wanted" {
		t.Fatalf("the accepted gate did not land: %+v %v", got, err)
	}
	waitUntil(t, "the gate's resume", func() bool {
		for _, c := range assistantContents(t, ds, res.Thread) {
			if c == "landed at last." {
				return true
			}
		}
		return false
	})
}

// GATING `patch` IS NOT EDIT-ONLY REVIEW. convertToRequest resolves `put` and
// `patch` the same way, so a patch of a record that is not there converts to a
// request whose accept MINTS it. An owner reading the selector as "review the
// edits" would be handing over creation, so the behavior is pinned here and
// said out loud on the property.
func TestPolicyGatesAPatchOfAnAbsentTargetIntoACreate(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	putPolicy(t, ds, "gate-widget-patches", map[string]any{
		"selector": map[string]any{
			"kinds": []any{crewAuthority + "/widget"},
			"ops":   []any{"patch"},
		},
		"action": "gate",
	})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { patch(kind: "crew.test.dev/widget", id: "w-absent", input: {properties: {name: "wanted"}}) { id } }`,
		})}}},
		fakeTurn{content: "held, waiting."},
	)
	if _, err := ds.CallAgent(ctx, crewAuthority+"/editor", "edit a widget"); err != nil {
		t.Fatalf("call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	if req.Properties["op"] != "create" || req.Properties["targetId"] != "w-absent" {
		t.Fatalf("a gated patch of an absent target: %+v", req.Properties)
	}
}

// AN EDGE WRITE IS NOT GATEABLE. Link and Unlink check the emit ceiling and
// write (agentgql.go); policyVerdict never runs for them, so the widest
// selector there is does not hold one. The declaration and
// docs/changelog.md#change-verbs both say so, and this is what holds them to it.
func TestPolicyNeverGatesAnEdgeWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	putPolicy(t, ds, "gate-everything", map[string]any{"action": "gate"})
	mustPutInternal(t, ds, substrate.PutInput{Kind: crewAuthority + "/widget", ID: "w-a"})
	mustPutInternal(t, ds, substrate.PutInput{Kind: crewAuthority + "/widget", ID: "w-b"})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { link(rel: "related", srcKind: "crew.test.dev/widget", src: "w-a",
				dstKind: "crew.test.dev/widget", dst: "w-b") { id } }`,
		})}}},
		fakeTurn{content: "linked."},
	)
	if _, err := ds.CallAgent(ctx, crewAuthority+"/editor", "link the widgets"); err != nil {
		t.Fatalf("call: %v", err)
	}
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{vocabulary.KindRecordPatchRequest}}, First: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 0 {
		t.Fatalf("an edge write minted %d request(s): the door does not see link", len(page.Records))
	}
}

// A SELECTOR SPEAKS THE DOOR'S OWN VERBS. `ops` used to be a free string, so a
// reader who saw `op: create` on the request a gate produced and wrote that
// word back into the selector got a rule that admitted and then matched
// nothing. The declared enum makes it a refusal that names the three words.
func TestPolicySelectorRefusesAVerbFromAnotherVocabulary(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	for _, op := range []string{"create", "update"} {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-" + op,
			Properties: map[string]any{
				"selector": map[string]any{"ops": []any{op}},
				"action":   "gate",
			},
		})
		if err == nil {
			t.Fatalf("a selector spelled %q admitted", op)
		}
		// The whole phrase, not the words: "input" contains "put" and
		// "recordpatchpolicy" contains "patch", so a substring check on either
		// passes on an error that names no verb at all.
		if !strings.Contains(err.Error(), "expected one of put, patch, delete") {
			t.Fatalf("the refusal for %q does not name the declared verbs: %v", op, err)
		}
	}
	// The door's own words still admit, and match the write they name.
	putPolicy(t, ds, "gate-puts", map[string]any{
		"selector": map[string]any{"ops": []any{"put"}},
		"action":   "gate",
	})
	verdict, _, err := ds.policyVerdict(ctx, crewAuthority+"/widget", policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyGate {
		t.Fatalf("verdict for a put = %s %v", verdict, err)
	}
	verdict, _, err = ds.policyVerdict(ctx, crewAuthority+"/widget", policyOpDelete, crewAuthority+"/editor")
	if err != nil || verdict != policyAllow {
		t.Fatalf("verdict for a delete = %s %v", verdict, err)
	}
}

// A SELECTOR COVERS AN AUTHORITY WITHOUT ENUMERATING IT. `selector.kinds`
// takes the trigger source's grammar, so "gate everything this authority
// publishes" is one pattern that also covers the kind installed tomorrow —
// the alternative was an enumerated snapshot or an empty list meaning every
// kind in the repository.
func TestPolicySelectorKindsGlobCoversAnAuthority(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	putPolicy(t, ds, "gate-crew", map[string]any{
		"selector": map[string]any{"kinds": []any{crewAuthority + "/*"}},
		"action":   "gate",
	})
	verdict, _, err := ds.policyVerdict(ctx, crewAuthority+"/widget", policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyGate {
		t.Fatalf("verdict for the globbed authority = %s %v", verdict, err)
	}
	verdict, _, err = ds.policyVerdict(ctx, "tasks.substrate.reamde.dev/task", policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyAllow {
		t.Fatalf("the glob reached another authority = %s %v", verdict, err)
	}
	// A pattern outside the grammar is refused at the write door rather than
	// admitted as a rule that matches nothing.
	_, err = ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-dotstar",
		Properties: map[string]any{
			"selector": map[string]any{"kinds": []any{"crew.*"}},
			"action":   "gate",
		},
	})
	if err == nil || !strings.Contains(err.Error(), "is not a kind reference") {
		t.Fatalf("a selector spelled `crew.*` admitted: %v", err)
	}
	// A rule with no action speaks for nothing, so it never lands.
	_, err = ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-actionless",
		Properties: map[string]any{
			"selector": map[string]any{"kinds": []any{crewAuthority + "/widget"}},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "`action` is required") {
		t.Fatalf("an actionless policy admitted: %v", err)
	}
}

// A SELECTOR MUST NAME A KIND THE REPOSITORY HAS. The door compares kind
// identities, so a plural typo (`widgets` for `widget`) or a reference to an
// uninstalled kind admits and then gates nothing: a trigger that never fires
// is a liveness bug, a policy that never matches is an open door.
func TestPolicySelectorRefusesAKindTheRepositoryDoesNotHave(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	for _, pat := range []string{"widgets", "crew.test.dev/widgets", "gadget"} {
		_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
			Kind: vocabulary.KindRecordPatchPolicy, ID: "gate-typo",
			Properties: map[string]any{
				"selector": map[string]any{"kinds": []any{pat}},
				"action":   "refuse",
			},
		})
		if err == nil || !strings.Contains(err.Error(), "matches no write") {
			t.Fatalf("a selector naming %q admitted: %v", pat, err)
		}
	}
	// A glob is NOT held to the vocabulary: an authority's kind set changing
	// under it is why an owner writes one.
	putPolicy(t, ds, "gate-elsewhere", map[string]any{
		"selector": map[string]any{"kinds": []any{"nobody.test.dev/*"}},
		"action":   "gate",
	})
}

// GOVERNANCE DOES NOT MOVE TO THE LAXER RULE. Among matches of equal severity
// the governing rule carries the judge, so a rule that can accept on a model's
// word must not outrank one that always reaches the owner — otherwise the id
// alphabet decides who reviews, and a selector that starts matching (a bare
// name now resolved, a stored `*` now honored) silently hands review to a
// judge.
func TestPolicyGovernanceStaysWithTheRuleThatReachesTheOwner(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	// Written before the wildcards landed, in the bare spelling that matched
	// nothing then and matches now. It sorts first by id.
	putPolicy(t, ds, "a-widgets", map[string]any{
		"selector":   map[string]any{"kinds": []any{"widget"}},
		"action":     "gate",
		"judge":      crewAuthority + "/verdictor",
		"criteria":   "small honest changes yes",
		"mode":       "enforce",
		"autoAccept": 0.6,
	})
	putPolicy(t, ds, "b-editor", map[string]any{
		"selector": map[string]any{"agents": []any{crewAuthority + "/editor"}},
		"action":   "gate",
	})
	verdict, rule, err := ds.policyVerdict(ctx, crewAuthority+"/widget", policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyGate {
		t.Fatalf("verdict = %s %v", verdict, err)
	}
	if rule.id != "b-editor" {
		t.Fatalf("the judged rule %s governs — an accept verdict would land the write with no owner", rule.id)
	}
	// The arm only breaks ties: where the judged rule is the only match it
	// still governs and its judge still runs.
	verdict, rule, err = ds.policyVerdict(ctx, crewAuthority+"/widget", policyOpPut, crewAuthority+"/ghost")
	if err != nil || verdict != policyGate {
		t.Fatalf("verdict for the unmatched agent = %s %v", verdict, err)
	}
	if rule.id != "a-widgets" || rule.judge == "" {
		t.Fatalf("the sole matching rule did not govern: %s judge=%q", rule.id, rule.judge)
	}
}

// THE ACTIONLESS ROW AN OLDER BINARY LEFT. The write door refuses one now, so
// the fixture plants it the way the engine's own machinery writes; evaluation
// skips it and says so once, not once per agent write.
func TestActionlessPolicyIsSkippedAndWarnedOnce(t *testing.T) {
	ctx := context.Background()
	var logs syncBuffer
	ds := openInternalDataset(t, WithLogger(slog.New(slog.NewTextHandler(&logs, nil))))
	if err := ds.inTx(ctx, substrate.ActorAPI, true, func(tx *txn) error {
		_, err := tx.put(substrate.PutInput{
			Kind: vocabulary.KindRecordPatchPolicy, ID: "legacy-actionless",
			Properties: map[string]any{"selector": map[string]any{"kinds": []any{"*"}}},
		})
		return err
	}); err != nil {
		t.Fatalf("plant the row: %v", err)
	}
	for i := range 2 {
		verdict, rule, err := ds.policyVerdict(ctx, typeThread, policyOpPut, "crew.test.dev/editor")
		if err != nil || verdict != policyAllow || rule != nil {
			t.Fatalf("evaluation %d saw the actionless rule: %s %v %v", i, verdict, rule, err)
		}
	}
	if n := strings.Count(logs.String(), "legacy-actionless"); n != 1 {
		t.Fatalf("the actionless rule warned %d times, want 1", n)
	}
	// The only ways out are an action or a delete: a write that leaves it
	// actionless is refused like any other.
	_, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: "legacy-actionless",
		Properties: map[string]any{"criteria": "anything"},
	})
	if err == nil || !strings.Contains(err.Error(), "`action` is required") {
		t.Fatalf("a write that left the row actionless admitted: %v", err)
	}
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: vocabulary.KindRecordPatchPolicy, ID: "legacy-actionless",
		Properties: map[string]any{"action": "gate"},
	}); err != nil {
		t.Fatalf("giving the row an action: %v", err)
	}
}

// A BARE KIND NAME IN A SELECTOR RESOLVES, like a trigger source's does. The
// door compares against kind identities, so `widget` would otherwise admit and
// then match nothing — the same silent no-op the glob exists to remove.
func TestPolicySelectorResolvesABareKindName(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	putPolicy(t, ds, "gate-bare-widget", map[string]any{
		"selector": map[string]any{"kinds": []any{"widget"}},
		"action":   "gate",
	})
	verdict, _, err := ds.policyVerdict(ctx, crewAuthority+"/widget", policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyGate {
		t.Fatalf("verdict for a bare-named kind = %s %v", verdict, err)
	}
}

func TestPolicyRefusesAndComposesMostRestrictive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, _ := openAgentDataset(t)
	putPolicy(t, ds, "allow-widgets", map[string]any{
		"selector": map[string]any{"kinds": []any{crewAuthority + "/widget"}},
		"action":   "allow",
	})
	putPolicy(t, ds, "refuse-widgets", map[string]any{
		"selector": map[string]any{"kinds": []any{crewAuthority + "/widget"}},
		"action":   "refuse",
	})
	// Most restrictive wins: refuse over allow.
	verdict, rule, err := ds.policyVerdict(ctx, crewAuthority+"/widget", policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyRefuse || rule == nil || rule.id != "refuse-widgets" {
		t.Fatalf("verdict = %s %v %v", verdict, rule, err)
	}
	// A disabled policy matches nothing; the allow remains.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchPolicy, "refuse-widgets", substrate.PatchInput{
		Properties: map[string]any{"disabled": true},
	}); err != nil {
		t.Fatal(err)
	}
	verdict, rule, err = ds.policyVerdict(ctx, crewAuthority+"/widget", policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyAllow || rule == nil || rule.id != "allow-widgets" {
		t.Fatalf("verdict after disable = %s %v %v", verdict, rule, err)
	}
	// No match at all is allow with nothing to audit.
	verdict, rule, err = ds.policyVerdict(ctx, taskKind, policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyAllow || rule != nil {
		t.Fatalf("unmatched verdict = %s %v %v", verdict, rule, err)
	}
	// The request kind is never policed: it IS the gate.
	putPolicy(t, ds, "gate-everything", map[string]any{"action": "gate"})
	verdict, _, err = ds.policyVerdict(ctx, vocabulary.KindRecordPatchRequest, policyOpPut, crewAuthority+"/editor")
	if err != nil || verdict != policyAllow {
		t.Fatalf("the gate gated itself: %s %v", verdict, err)
	}
}

func TestBundleHandsStayOffThePolicyKind(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("med",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "core.substrate.reamde.dev/recordpatchpolicy", id: "backdoor", properties: {action: "allow"}}) { id } }`,
		})}}},
		fakeTurn{content: "tried."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/meddler", "open the door")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] == true {
		t.Fatalf("installed code wrote a policy: %v", tool["content"])
	}
	if content, _ := tool["content"].(string); !strings.Contains(content, "owner's hand alone") {
		t.Fatalf("the refusal does not say whose hand: %s", content)
	}
	if _, err := ds.Get(ctx, vocabulary.KindRecordPatchPolicy, "backdoor"); err == nil {
		t.Fatal("the policy landed")
	}
}

func TestConfirmationFloorGatesFunctionEffects(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	fake.script("burn",
		fakeTurn{calls: []fakeCall{{"burn", `{}`}}},
		fakeTurn{content: "held, as declared."},
		fakeTurn{content: "burned at last."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/burner", "burn one")
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	// The floor held the effect: nothing landed, the batch failed honestly,
	// and the request exists with the thread stamped and NO policy (the
	// declaration itself demanded review).
	if _, err := ds.Get(ctx, taskKind, "t-burned"); err == nil {
		t.Fatal("a floored effect landed")
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] == true {
		t.Fatal("a floored effect reported ok")
	}
	req := onlyPatchRequest(t, ds)
	if req.Properties["op"] != "create" || req.Properties["targetId"] != "t-burned" {
		t.Fatalf("request: %+v", req.Properties)
	}
	if req.Properties["policy"] != nil {
		t.Fatalf("a floor cited a policy: %v", req.Properties["policy"])
	}
	if got := req.Properties["thread"]; got != vocabulary.RecordPath(typeThread, res.Thread) {
		t.Fatalf("request thread = %v", got)
	}
	// Accepting materializes the held effect.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("accept: %v", err)
	}
	if got, err := ds.Get(ctx, taskKind, "t-burned"); err != nil || got.Properties["name"] != "burned" {
		t.Fatalf("the accepted effect did not land: %+v %v", got, err)
	}
}

func TestVoluntaryProposalsStaySelfAcceptable(t *testing.T) {
	t.Parallel()
	// The documented contract the gated guard deliberately does NOT change
	// (core_db_test.go's ceiling test): a bundle actor whose emit covers the
	// target may accept its own VOLUNTARY proposal, because it could have
	// written the target directly and nothing escalates. The gate is where
	// self-decision is refused (TestGatedRequestsRefuseBundleDecisions).
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	if _, err := ds.Put(ctx, substrate.ActorAPI, substrate.PutInput{
		Kind: crewAuthority + "/widget", ID: "w-mine", Properties: map[string]any{"name": "raw"},
	}); err != nil {
		t.Fatal(err)
	}
	arrived := make(chan struct{})
	release := make(chan struct{})
	fake.script("self",
		fakeTurn{calls: []fakeCall{{"propose", `{"kind":"crew.test.dev/widget","target":"w-mine","diff":{"properties":{"name":"mine"}}}`}}},
		fakeTurn{content: "waiting for the id.", arrived: arrived, release: release},
	)
	done := make(chan error, 1)
	go func() {
		_, err := ds.CallAgent(ctx, crewAuthority+"/selfjudge", "take the widget")
		done <- err
	}()
	<-arrived
	req := onlyPatchRequest(t, ds)
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("call: %v", err)
	}
	fake.script("self",
		fakeTurn{calls: []fakeCall{{"mutate", decideArgs(t, req.ID, "accepted")}}},
		fakeTurn{content: "approved my own."},
	)
	res2, err := ds.CallAgent(ctx, crewAuthority+"/selfjudge", "approve your proposal")
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	tool := lastToolMessage(t, ds, res2.Thread)
	if tool["ok"] != true {
		t.Fatalf("a within-emit self-accept refused: %v", tool["content"])
	}
	if got, err := ds.Get(ctx, crewAuthority+"/widget", "w-mine"); err != nil || got.Properties["name"] != "mine" {
		t.Fatalf("the self-accepted diff did not land: %+v %v", got, err)
	}
}

func TestGatedRequestsRefuseBundleDecisions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, fake := openAgentDataset(t)
	putPolicy(t, ds, "gate-widgets", map[string]any{
		"selector": map[string]any{"kinds": []any{crewAuthority + "/widget"}},
		"action":   "gate",
	})
	fake.script("mut",
		fakeTurn{calls: []fakeCall{{"mutate", gqlToolArgs(t, map[string]any{
			"query": `mutation { put(input: {kind: "crew.test.dev/widget", id: "w-gated", properties: {name: "wanted"}}) { id } }`,
		})}}},
		fakeTurn{content: "held."},
	)
	if _, err := ds.CallAgent(ctx, crewAuthority+"/editor", "make a widget"); err != nil {
		t.Fatalf("editor call: %v", err)
	}
	req := onlyPatchRequest(t, ds)
	// The arbiter's emit covers widgets and requests, and it is NOT the
	// proposer — but the request is policy-gated, so installed code keeps out.
	fake.script("arbiter",
		fakeTurn{calls: []fakeCall{{"mutate", decideArgs(t, req.ID, "accepted")}}},
		fakeTurn{content: "tried."},
	)
	res, err := ds.CallAgent(ctx, crewAuthority+"/arbiter", "work the inbox")
	if err != nil {
		t.Fatalf("arbiter call: %v", err)
	}
	tool := lastToolMessage(t, ds, res.Thread)
	if tool["ok"] == true {
		t.Fatal("installed code decided a policy-gated request")
	}
	if content, _ := tool["content"].(string); !strings.Contains(content, "a policy gated this request") {
		t.Fatalf("the refusal does not name the gate: %s", content)
	}
	// The owner still can.
	if _, err := ds.Patch(ctx, substrate.ActorAPI, vocabulary.KindRecordPatchRequest, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &req.Version,
	}); err != nil {
		t.Fatalf("owner accept: %v", err)
	}
	if _, err := ds.Get(ctx, crewAuthority+"/widget", "w-gated"); err != nil {
		t.Fatalf("the accepted gate did not land: %v", err)
	}
}
