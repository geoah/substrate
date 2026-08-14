package engine_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// A change request's diff is validated at ADMISSION, so every door — a
// function's put effect, the HTTP API, an agent mutation — refuses a malformed
// proposal at the WRITE instead of discovering it when somebody accepts. What
// the agent propose built-in checked for itself now holds for the writes that
// never went through it. A request that landed BEFORE the check is untouched:
// it still accepts or annotates exactly as it did.

const requestKind = vocabulary.KindRecordPatchRequest

// requestVersion reads a request's current version, the value an owner's accept
// must carry.
func requestVersion(t *testing.T, ds substrate.Dataset, id string) int64 {
	t.Helper()
	e, err := ds.Get(context.Background(), requestKind, id)
	if err != nil {
		t.Fatalf("read request %s: %v", id, err)
	}
	return e.Version
}

// accept decides a request the owner's way: the version it read, so nothing
// slips under the decision.
func accept(t *testing.T, ds substrate.Dataset, id string) error {
	t.Helper()
	v := requestVersion(t, ds, id)
	_, err := ds.Patch(context.Background(), owner, requestKind, id, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: &v,
	})
	return err
}

// TestFunctionProposalLandsAndApplies is the loop nothing covered: a
// trigger-fired function whose EFFECT puts a change request, the request row,
// and the owner's accept applying it. Both stored diff shapes travel this way —
// the wrapper form and the bare property map admission wraps — because a model
// (and a body written by one) writes both.
func TestFunctionProposalLandsAndApplies(t *testing.T) {
	t.Parallel()
	const propose = `
def main(input, host):
    return {"effects": [
        {"action": "put", "kind": "core.substrate.reamde.dev/recordpatchrequest",
         "id": "req-wrapped",
         "properties": {"rationale": "the widget says so",
                        "diff": {"properties": {"description": "wrapped"}}},
         "edges": {"target": {"kind": "tasks.substrate.reamde.dev/task", "id": "t-wrapped"}}},
        {"action": "put", "kind": "core.substrate.reamde.dev/recordpatchrequest",
         "id": "req-bare",
         "properties": {"diff": {"description": "bare"}},
         "edges": {"target": {"kind": "tasks.substrate.reamde.dev/task", "id": "t-bare"}}}
    ]}
`
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{trigOn("proposer", map[string]any{
			"kinds": []any{widgetType}, "ops": []any{"create"},
		})},
		pyFn("proposer", map[string]any{}, []any{requestKind}, propose))

	for _, id := range []string{"t-wrapped", "t-bare"} {
		mustPut(t, ds, owner, substrate.PutInput{
			Kind: taskType, ID: id, Properties: map[string]any{"title": "draft"},
		})
	}
	mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, ID: "w-1", Properties: map[string]any{"name": "w"}})
	process(t, ops)

	for _, c := range []struct{ request, target, want string }{
		{"req-wrapped", "t-wrapped", "wrapped"},
		{"req-bare", "t-bare", "bare"},
	} {
		req := mustGet(t, ds, requestKind, c.request)
		if req.Properties["decision"] != "proposed" {
			t.Fatalf("%s: %+v", c.request, req.Properties)
		}
		// Admission stores the WRAPPER form whatever the writer sent, so the
		// accept's strict decode has one shape to read.
		diff, ok := req.Properties["diff"].(map[string]any)
		if !ok {
			t.Fatalf("%s: diff is %T", c.request, req.Properties["diff"])
		}
		props, ok := diff["properties"].(map[string]any)
		if !ok || props["description"] != c.want {
			t.Fatalf("%s: stored diff = %+v", c.request, diff)
		}
		if err := accept(t, ds, c.request); err != nil {
			t.Fatalf("accept %s: %v", c.request, err)
		}
		if got := mustGet(t, ds, taskType, c.target); got.Properties["description"] != c.want {
			t.Fatalf("%s did not apply: %+v", c.request, got.Properties)
		}
	}
}

// A function effect carrying a MALFORMED diff is refused at the write: the
// delivery parks with the reason, and no request reaches the inbox for an owner
// to decide on. Two shapes, one per trigger, because the first refusal fails
// the whole delivery.
func TestFunctionMalformedProposalRefusedAtWrite(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	const badShape = `
def main(input, host):
    return {"effects": [
        {"action": "put", "kind": "core.substrate.reamde.dev/recordpatchrequest",
         "id": "req-badshape",
         "properties": {"diff": {"properties": "not a map"}},
         "edges": {"target": {"kind": "tasks.substrate.reamde.dev/task", "id": "t-victim"}}}
    ]}
`
	const badCreate = `
def main(input, host):
    return {"effects": [
        {"action": "put", "kind": "core.substrate.reamde.dev/recordpatchrequest",
         "id": "req-badcreate",
         "properties": {"op": "create", "diff": {"properties": {"title": "nameless"}}}}
    ]}
`
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{
			trigOn("badshape", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}}),
			trigOn("badcreate", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}}),
		},
		pyFn("badshape", map[string]any{}, []any{requestKind}, badShape),
		pyFn("badcreate", map[string]any{}, []any{requestKind}, badCreate))

	mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskType, ID: "t-victim", Properties: map[string]any{"title": "untouched"},
	})
	mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, ID: "w-1", Properties: map[string]any{"name": "w"}})
	process(t, ops)

	for _, c := range []struct{ fn, request, reason string }{
		{"badshape", "req-badshape", "diff.properties must be an object"},
		{"badcreate", "req-badcreate", "targetKind and targetId"},
	} {
		parked, err := ops.TriggerFailures(ctx, trigID(c.fn))
		if err != nil {
			t.Fatalf("%s failures: %v", c.fn, err)
		}
		if len(parked) == 0 {
			t.Fatalf("%s: a malformed proposal committed", c.fn)
		}
		if !strings.Contains(parked[0].LastError, c.reason) {
			t.Fatalf("%s: refusal does not name the problem: %s", c.fn, parked[0].LastError)
		}
		if _, err := ds.Get(ctx, requestKind, c.request); !errors.Is(err, substrate.ErrNotFound) {
			t.Fatalf("%s: the malformed request landed: %v", c.fn, err)
		}
	}
	if got := mustGet(t, ds, taskType, "t-victim"); got.Properties["title"] != "untouched" {
		t.Fatalf("the target moved: %+v", got.Properties)
	}
}

// The HTTP/generic door is held to the same admission, and says which part of
// the diff is wrong. The envelope keys the accept path reads — a diff's own
// `ifVersion` — still pass: they are part of the shape it decodes.
func TestAPIProposalDiffAdmission(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskType, Properties: map[string]any{"title": "draft"},
	})
	target := []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: taskType, ID: task.ID}}}

	for _, c := range []struct {
		what  string
		in    substrate.PutInput
		names string
	}{
		{
			what: "properties is not an object",
			in: substrate.PutInput{
				Kind: requestKind, Edges: target,
				Properties: map[string]any{"diff": map[string]any{"properties": "nope"}},
			},
			names: "diff.properties must be an object",
		},
		{
			what: "an undeclared property",
			in: substrate.PutInput{
				Kind: requestKind, Edges: target,
				Properties: map[string]any{"diff": map[string]any{"properties": map[string]any{"bogus": "x"}}},
			},
			names: "is not a property of",
		},
		{
			what: "an unknown top-level key",
			in: substrate.PutInput{
				Kind: requestKind, Edges: target,
				Properties: map[string]any{"diff": map[string]any{
					"properties": map[string]any{"description": "x"}, "sideEffects": true,
				}},
			},
			names: "unknown top-level key",
		},
		{
			what: "a create naming no target",
			in: substrate.PutInput{
				Kind: requestKind,
				Properties: map[string]any{
					"op":   "create",
					"diff": map[string]any{"properties": map[string]any{"title": "nameless"}},
				},
			},
			names: "targetKind and targetId",
		},
		{
			what: "a wrong-typed value",
			in: substrate.PutInput{
				Kind: requestKind, Edges: target,
				Properties: map[string]any{"diff": map[string]any{
					"properties": map[string]any{"dueAt": "not a date"},
				}},
			},
			names: "dueAt",
		},
		{
			what: "a diff that is not an object",
			in: substrate.PutInput{
				Kind: requestKind, Edges: target,
				Properties: map[string]any{"diff": "description: x"},
			},
			names: "the diff must be an object",
		},
		{
			what: "a diff naming no property",
			in: substrate.PutInput{
				Kind: requestKind, Edges: target,
				Properties: map[string]any{"diff": map[string]any{
					"properties": map[string]any{}, "labels": map[string]any{"seen": true},
				}},
			},
			names: "names no property to change",
		},
		{
			what: "a delete carrying a diff",
			in: substrate.PutInput{
				Kind: requestKind, Edges: target,
				Properties: map[string]any{
					"op":   "delete",
					"diff": map[string]any{"properties": map[string]any{"description": "why"}},
				},
			},
			names: "op delete proposes no values",
		},
		{
			// Presence, not content: an empty diff on a delete is still a claim
			// about the proposal.
			what: "a delete carrying an empty diff",
			in: substrate.PutInput{
				Kind: requestKind, Edges: target,
				Properties: map[string]any{"op": "delete", "diff": map[string]any{}},
			},
			names: "op delete proposes no values",
		},
		{
			what: "a delete naming several targets",
			in: substrate.PutInput{
				Kind: requestKind,
				Edges: []substrate.EdgeInput{
					{Rel: "target", To: substrate.EdgeRef{Kind: taskType, ID: task.ID}},
					{Rel: "target", To: substrate.EdgeRef{Kind: taskType, ID: "t-other"}},
				},
				Properties: map[string]any{"op": "delete"},
			},
			names: "names ONE target",
		},
		{
			// A create's target does not exist yet, so a target EDGE on one points
			// at something else entirely.
			what: "a create carrying a target edge",
			in: substrate.PutInput{
				Kind: requestKind, Edges: target,
				Properties: map[string]any{
					"op": "create", "targetKind": taskType, "targetId": "t-fresh",
					"diff": map[string]any{"properties": map[string]any{"title": "new"}},
				},
			},
			names: "never by a target edge",
		},
		{
			// A create is create-if-absent: a precondition inside its diff would
			// be admitted and then never enforced, so it is a patch key alone.
			what: "a create diff naming ifVersion",
			in: substrate.PutInput{
				Kind: requestKind,
				Properties: map[string]any{
					"op": "create", "targetKind": taskType, "targetId": "t-precondition",
					"diff": map[string]any{
						"properties": map[string]any{"title": "guarded"}, "ifVersion": 0,
					},
				},
			},
			names: "unknown top-level key \"ifVersion\"",
		},
		{
			// A TARGETLESS patch request is legal storage, but only as a diff:
			// the shape holds even where no kind resolves to check the properties
			// against.
			what: "a targetless patch with an unknown envelope key",
			in: substrate.PutInput{
				Kind: requestKind,
				Properties: map[string]any{"diff": map[string]any{
					"properties": map[string]any{"description": "orphaned"}, "sideEffects": true,
				}},
			},
			names: "unknown top-level key",
		},
		{
			what: "a targetless patch whose envelope value is wrong-typed",
			in: substrate.PutInput{
				Kind: requestKind,
				Properties: map[string]any{"diff": map[string]any{
					"properties": map[string]any{"description": "orphaned"}, "labels": "seen",
				}},
			},
			names: "the patch shape is invalid",
		},
		{
			what: "a targetless patch naming no property",
			in: substrate.PutInput{
				Kind:       requestKind,
				Properties: map[string]any{"diff": map[string]any{"properties": map[string]any{}}},
			},
			names: "names no property to change",
		},
	} {
		if _, err := ds.Put(ctx, engram, c.in); err == nil {
			t.Fatalf("%s: admitted", c.what)
		} else {
			wantErr(t, err, substrate.ErrValidation, c.what)
			if !strings.Contains(err.Error(), c.names) {
				t.Fatalf("%s: refusal does not name the problem: %v", c.what, err)
			}
		}
	}

	// A diff carrying its OWN precondition is well-formed: the accept path
	// decodes ifVersion, so admission admits it and the stale one still loses
	// its CAS at accept.
	fresh := mustPut(t, ds, engram, substrate.PutInput{
		Kind: requestKind, Edges: target,
		Properties: map[string]any{"diff": map[string]any{
			"properties": map[string]any{"description": "with a precondition"},
			"ifVersion":  task.Version,
		}},
	})
	if err := accept(t, ds, fresh.ID); err != nil {
		t.Fatalf("a diff with its own ifVersion did not apply: %v", err)
	}
	if got := mustGet(t, ds, taskType, task.ID); got.Properties["description"] != "with a precondition" {
		t.Fatalf("the patch did not land: %+v", got.Properties)
	}
}

// A change request names ONE target. Two `target` entries in one write would
// have the diff validated against the first kind and stored against the last —
// the write loop keeps the last entry for a single-valued edge — which is how a
// raw secret value would reach a stored request's json diff past the
// sensitive-property check. Both doors refuse it, and nothing lands.
func TestTwoTargetsSmuggleNothing(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// Two gauges: one where `apiKey` is an ordinary string (so a diff naming it
	// validates), one where it is a SECRET (so a diff naming it must not be
	// stored at all).
	const gaugeAuthority = "gauge.example.com"
	docs := []map[string]any{
		vocabulary.AuthorityManifest(gaugeAuthority, ""),
		vocabulary.KindManifest(gaugeAuthority, map[string]any{"singular": "safegauge", "plural": "safegauges"},
			map[string]any{"properties": map[string]any{"apiKey": map[string]any{"type": "string"}}}),
		vocabulary.KindManifest(gaugeAuthority, map[string]any{"singular": "secretgauge", "plural": "secretgauges"},
			map[string]any{"properties": map[string]any{"apiKey": map[string]any{"type": "secret"}}}),
	}
	if _, err := applier(t, ds).ApplyVocabularyDocuments(ctx, owner, docs); err != nil {
		t.Fatalf("install the gauge authority: %v", err)
	}
	safe := mustPut(t, ds, owner, substrate.PutInput{
		Kind: gaugeAuthority + "/safegauge", ID: "g-safe", Properties: map[string]any{"apiKey": "public"},
	})
	secret := mustPut(t, ds, owner, substrate.PutInput{
		Kind: gaugeAuthority + "/secretgauge", ID: "g-secret", Properties: map[string]any{"apiKey": "sk-stored"},
	})

	_, err := ds.Put(ctx, engram, substrate.PutInput{
		Kind: requestKind, ID: "req-smuggle",
		Properties: map[string]any{"diff": map[string]any{
			"properties": map[string]any{"apiKey": "sk-smuggled"},
		}},
		Edges: []substrate.EdgeInput{
			{Rel: "target", To: substrate.EdgeRef{Kind: safe.Kind, ID: safe.ID}},
			{Rel: "target", To: substrate.EdgeRef{Kind: secret.Kind, ID: secret.ID}},
		},
	})
	if err == nil {
		t.Fatal("a request naming two targets was admitted")
	}
	wantErr(t, err, substrate.ErrValidation, "two targets")
	if !strings.Contains(err.Error(), "names ONE target") {
		t.Fatalf("refusal does not name the rule: %v", err)
	}
	if _, err := ds.Get(ctx, requestKind, "req-smuggle"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the smuggling request landed: %v", err)
	}

	// The single-target write it was hiding behind is refused on its own terms:
	// the secret gauge's apiKey may never sit in a request's diff.
	if _, err := ds.Put(ctx, engram, substrate.PutInput{
		Kind: requestKind, ID: "req-direct",
		Properties: map[string]any{"diff": map[string]any{
			"properties": map[string]any{"apiKey": "sk-smuggled"},
		}},
		Edges: []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: secret.Kind, ID: secret.ID}}},
	}); err == nil {
		t.Fatal("a raw secret in a proposed diff was admitted")
	} else if !strings.Contains(err.Error(), "sensitive") {
		t.Fatalf("refusal does not name the secret: %v", err)
	}
}

// A REPLAYED delivery re-proposes the same request: same id, same bare diff. The
// stored diff is the wrapper form admission normalised it into, so the re-put is
// canonicalized before the envelope guard sees it — an identical proposal is a
// verified no-op, not an ErrForbidden park. This is the SDK's replay promise:
// ids are the writer's, and a replay writes nothing new.
func TestIdenticalReproposalIsANoOp(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, ops := newFnDataset(t, nil, pyFn("reproposer", map[string]any{}, []any{requestKind}, `
def main(input, host):
    host.effects.propose("req-replayed", "tasks.substrate.reamde.dev/task",
                         input["args"]["target"], diff={"description": "same as ever"},
                         rationale="steady")
    return {}
`))
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskType, ID: "t-replayed", Properties: map[string]any{"title": "draft"},
	})
	fn := fnAuthority + "/reproposer"

	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{"target": task.ID}); err != nil {
		t.Fatalf("first delivery: %v", err)
	}
	first := mustGet(t, ds, requestKind, "req-replayed")

	// The replay: the same body, the same staged effect, the same request id.
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{"target": task.ID}); err != nil {
		t.Fatalf("the replayed delivery parked: %v", err)
	}
	again := mustGet(t, ds, requestKind, "req-replayed")
	if again.Version != first.Version {
		t.Fatalf("the replay rewrote the request: version %d -> %d", first.Version, again.Version)
	}
	if !reflect.DeepEqual(again.Properties, first.Properties) {
		t.Fatalf("the replay changed the envelope: %+v -> %+v", first.Properties, again.Properties)
	}

	// And the request is still the one an owner can accept.
	if err := accept(t, ds, "req-replayed"); err != nil {
		t.Fatalf("accept after replay: %v", err)
	}
	if got := mustGet(t, ds, taskType, task.ID); got.Properties["description"] != "same as ever" {
		t.Fatalf("the replayed proposal did not apply: %+v", got.Properties)
	}
}

// A targetless patch request stays legal storage — the target edge is not
// required, and its accept annotates "no target" — but its diff is still held to
// the wrapper SHAPE, and a bare one lands wrapped like any other.
func TestTargetlessRequestKeepsItsShape(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	req := mustPut(t, ds, engram, substrate.PutInput{
		Kind: requestKind, Properties: map[string]any{"diff": map[string]any{"description": "orphaned"}},
	})
	diff, ok := req.Properties["diff"].(map[string]any)
	if !ok {
		t.Fatalf("stored diff is %T", req.Properties["diff"])
	}
	props, ok := diff["properties"].(map[string]any)
	if !ok || props["description"] != "orphaned" {
		t.Fatalf("a targetless bare diff did not land wrapped: %+v", diff)
	}
	// Accepting it still annotates rather than failing bare: nothing to patch.
	if err := accept(t, ds, req.ID); err == nil {
		t.Fatal("a targetless patch accepted green")
	} else {
		wantErr(t, err, substrate.ErrConflict, "targetless patch")
	}
	if after := mustGet(t, ds, requestKind, req.ID); after.Properties["decision"] != "proposed" ||
		after.Annotations["substrate/conflict"] == nil {
		t.Fatalf("targetless request should stay proposed + annotated: %+v", after.Properties)
	}
}

// Back-compat: a request that was ALREADY STORED with a malformed diff is not
// re-judged. Nothing refuses it on read, and accepting it fails exactly as it
// did before admission existed — a rolled-back transition, the request still
// proposed, the conflict annotated for the owner to read.
func TestStoredMalformedRequestStillFailsAtAccept(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	ds, raw, _ := newDatasetWithDB(t)

	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskType, Properties: map[string]any{"title": "draft", "description": "as it was"},
	})
	req := mustPut(t, ds, engram, substrate.PutInput{
		Kind: requestKind,
		Properties: map[string]any{"diff": map[string]any{
			"properties": map[string]any{"description": "well-formed at propose"},
		}},
		Edges: []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: taskType, ID: task.ID}}},
	})
	// The row a pre-admission binary could leave behind: a diff the accept
	// path's strict decode refuses. Written straight to `records` because no
	// door writes it any more — that is the point of the test.
	if _, err := raw.ExecContext(ctx,
		`UPDATE records SET props = jsonb_set(props, '{diff}', '{"description": "legacy"}')
		 WHERE kind = $1 AND id = $2`, requestKind, req.ID); err != nil {
		t.Fatalf("plant the legacy diff: %v", err)
	}

	// Reads are untouched: admission judges writes, never stored rows.
	stored := mustGet(t, ds, requestKind, req.ID)
	if stored.Properties["decision"] != "proposed" {
		t.Fatalf("legacy request reads as %+v", stored.Properties)
	}

	if err := accept(t, ds, req.ID); err == nil {
		t.Fatal("a legacy malformed diff accepted green")
	} else {
		wantErr(t, err, substrate.ErrConflict, "legacy malformed diff")
	}
	after := mustGet(t, ds, requestKind, req.ID)
	if after.Properties["decision"] != "proposed" {
		t.Fatalf("the failed transition did not roll back: %+v", after.Properties)
	}
	if after.Annotations["substrate/conflict"] == nil {
		t.Fatalf("conflict annotation missing: %+v", after.Annotations)
	}
	if got := mustGet(t, ds, taskType, task.ID); got.Properties["description"] != "as it was" {
		t.Fatalf("the target changed: %+v", got.Properties)
	}
}
