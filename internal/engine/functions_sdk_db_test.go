package engine_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/geoah/substrate/internal/substrate"
)

// The buffered-effects SDK, end to end through the real runner: the
// host.effects.* builder accumulates effects the engine applies exactly like a
// returned list, host.ids.* mints deterministic ids, and put/patch effects
// honor the ifVersion optimistic precondition (the decode gap effects.go
// closed). These exercise the shared Python host (host.py); the Go substratefn
// builder is the byte-identical mirror.

func TestSDKBuilderEffectsAndIds(t *testing.T) {
	t.Parallel()
	// A body that stages its write through host.effects.put (no returned list)
	// and derives its id through host.ids.external — the reference shape.
	ds, ops := newFnDataset(t, nil, pyFn("sdkbuild", map[string]any{}, []any{taskType}, `
def main(input, host):
    a = input["args"]
    tid = host.ids.external("prov", a["account"], a["ext"])
    host.effects.put("tasks.substrate.reamde.dev/task", tid,
                     properties={"title": a["title"]}, if_absent=True)
    return {"output": {"id": tid}}
`))
	ctx := context.Background()
	fn := fnAuthority + "/sdkbuild"

	out, n, err := ops.CallFunction(ctx, fn, map[string]any{
		"account": "acct1", "ext": "rec/9", "title": "staged",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if n != 1 {
		t.Fatalf("staged effect count = %d, want 1", n)
	}
	id, _ := out.(map[string]any)["id"].(string)
	if id == "" {
		t.Fatalf("no id in output: %v", out)
	}
	if got := mustGet(t, ds, taskType, id); got.Title != "staged" {
		t.Fatalf("staged put did not apply: %+v", got)
	}

	// ids.external is deterministic: the same provider/account/external id
	// recomputes the same id, and the if_absent put is then a no-op.
	out2, _, err := ops.CallFunction(ctx, fn, map[string]any{
		"account": "acct1", "ext": "rec/9", "title": "again",
	})
	if err != nil {
		t.Fatalf("call 2: %v", err)
	}
	if out2.(map[string]any)["id"] != id {
		t.Fatalf("ids.external not deterministic: %v vs %q", out2, id)
	}
	if again := mustGet(t, ds, taskType, id); again.Title != "staged" {
		t.Fatalf("if_absent did not hold — the second put reset state: %q", again.Title)
	}
}

func TestSDKBuilderGoRuntime(t *testing.T) {
	t.Parallel()
	// The Go substratefn builder mirror, compiled and run for real: host.IDs and
	// host.Effects stage a put, and the id it mints is byte-identical to the
	// Python ids.external for the same inputs (cross-runtime consistency).
	ds, ops := newFnDataset(t, nil, goFn("gosdk", map[string]any{}, []any{taskType}, `
import "substratefn.local/substratefn"

func Main(in *substratefn.Input, host *substratefn.Host) (*substratefn.Result, error) {
	a, _ := in.Args.(map[string]any)
	id := host.IDs.External("prov", a["account"].(string), a["ext"].(string))
	host.Effects.Put(substratefn.PutEffect{
		Kind: "tasks.substrate.reamde.dev/task", ID: id,
		Properties: map[string]any{"title": a["title"]},
		IfAbsent:   true,
	})
	return &substratefn.Result{Output: map[string]any{"id": id}}, nil
}
`))
	ctx := context.Background()
	out, n, err := ops.CallFunction(ctx, fnAuthority+"/gosdk", map[string]any{
		"account": "acct1", "ext": "rec/9", "title": "go-staged",
	})
	if err != nil {
		t.Fatalf("call: %v", err)
	}
	if n != 1 {
		t.Fatalf("go staged effect count = %d, want 1", n)
	}
	id, _ := out.(map[string]any)["id"].(string)
	// The same id the Python ids.external mints for ("prov","acct1","rec/9").
	const wantID = "prov-743191e31fee28539c17cfbfe1124285"
	if id != wantID {
		t.Fatalf("Go ids.External diverged from Python: %q, want %q", id, wantID)
	}
	if got := mustGet(t, ds, taskType, id); got.Title != "go-staged" {
		t.Fatalf("go staged put did not apply: %+v", got)
	}
}

func TestSDKBuilderMixedModeRejected(t *testing.T) {
	t.Parallel()
	// One mode per invocation: a body that BOTH returns an explicit effect list
	// AND stages on the builder is refused (the two apply orders are unrelated
	// and can self-conflict under CAS). It parks once with a clear error, and
	// NOTHING lands — neither the returned nor the staged effect.
	ds, ops := newFnDataset(t, nil, pyFn("bothways", map[string]any{}, []any{taskType}, `
def main(input, host):
    k = input["args"]["k"]
    host.effects.put("tasks.substrate.reamde.dev/task", "staged-" + k, properties={"title": "staged"})
    return {"effects": [{"action": "put", "kind": "tasks.substrate.reamde.dev/task",
                         "id": "returned-" + k, "properties": {"title": "returned"}}]}
`))
	ctx := context.Background()
	_, _, err := ops.CallFunction(ctx, fnAuthority+"/bothways", map[string]any{"k": "1"})
	if err == nil || !strings.Contains(err.Error(), "not both") {
		t.Fatalf("mixed mode: want the one-mode refusal, got %v", err)
	}
	for _, id := range []string{"returned-1", "staged-1"} {
		if _, err := ds.Get(ctx, taskType, id); !errors.Is(err, substrate.ErrNotFound) {
			t.Fatalf("mixed-mode delivery leaked %s", id)
		}
	}
}

func TestSDKBuilderStagedHandleReturnDoesNotKillHost(t *testing.T) {
	t.Parallel()
	// Returning a staged handle (not a plain effect map) is a user error that
	// json.dumps cannot serialize. It must be answered as an ok:false frame —
	// never crash the SHARED python host — and a LATER ordinary delivery on the
	// same host must still succeed.
	ds, ops := newFnDataset(t, nil,
		pyFn("badreturn", map[string]any{}, []any{taskType}, `
def main(input, host):
    h = host.effects.put("tasks.substrate.reamde.dev/task", "x", properties={"title": "t"})
    return {"effects": [h]}  # a StagedEffect handle is not serializable
`),
		pyFn("goodafter", map[string]any{}, []any{taskType}, `
def main(input, host):
    host.effects.put("tasks.substrate.reamde.dev/task", "survivor", properties={"title": "ok"})
    return {}
`))
	ctx := context.Background()
	if _, _, err := ops.CallFunction(ctx, fnAuthority+"/badreturn", map[string]any{}); err == nil {
		t.Fatalf("returning a staged handle should fail the delivery")
	}
	// The shared host survived: a subsequent delivery still works.
	if _, _, err := ops.CallFunction(ctx, fnAuthority+"/goodafter", map[string]any{}); err != nil {
		t.Fatalf("shared host did not survive a bad return: %v", err)
	}
	if got := mustGet(t, ds, taskType, "survivor"); got.Title != "ok" {
		t.Fatalf("post-crash delivery did not apply: %+v", got)
	}
}

func TestSDKBuilderStagesSnapshot(t *testing.T) {
	t.Parallel()
	// Staging captures a JSON snapshot: a body reusing one properties dict across
	// a loop stages EACH iteration's value, not the last (the map is not aliased).
	ds, ops := newFnDataset(t, nil, pyFn("snap", map[string]any{}, []any{taskType}, `
def main(input, host):
    props = {}
    for i in range(3):
        props["title"] = "t%d" % i
        host.effects.put("tasks.substrate.reamde.dev/task", "snap-%d" % i, properties=props)
    return {}
`))
	ctx := context.Background()
	if _, n, err := ops.CallFunction(ctx, fnAuthority+"/snap", map[string]any{}); err != nil || n != 3 {
		t.Fatalf("snapshot call: n=%d err=%v", n, err)
	}
	for i, want := range []string{"t0", "t1", "t2"} {
		if got := mustGet(t, ds, taskType, "snap-"+string(rune('0'+i))); got.Title != want {
			t.Fatalf("staged %d title = %q, want %q (map was aliased)", i, got.Title, want)
		}
	}
}

func TestSDKBuilderIfVersionSentinel(t *testing.T) {
	t.Parallel()
	// Python if_version handling: OMITTED is an unguarded write; an explicit None
	// is a builder error (a typo'd version must not silently drop the guard); 0
	// is a distinct precondition (an absent record is version 0).
	ds, ops := newFnDataset(t, nil, pyFn("ver", map[string]any{}, []any{taskType}, `
def main(input, host):
    mode = input["args"]["mode"]
    if mode == "omitted":
        host.effects.put("tasks.substrate.reamde.dev/task", "v-omitted", properties={"title": "t"})
    elif mode == "none":
        host.effects.put("tasks.substrate.reamde.dev/task", "v-none", properties={"title": "t"}, if_version=None)
    elif mode == "zero":
        host.effects.put("tasks.substrate.reamde.dev/task", "v-zero", properties={"title": "t"}, if_version=0)
    return {}
`))
	ctx := context.Background()
	fn := fnAuthority + "/ver"

	// Omitted → unguarded, applies.
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{"mode": "omitted"}); err != nil {
		t.Fatalf("omitted if_version: %v", err)
	}
	if got := mustGet(t, ds, taskType, "v-omitted"); got.Title != "t" {
		t.Fatalf("omitted if_version did not apply: %+v", got)
	}
	// Explicit None → builder error, nothing lands.
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{"mode": "none"}); err == nil ||
		!strings.Contains(err.Error(), "if_version=None") {
		t.Fatalf("explicit None if_version: want the builder refusal, got %v", err)
	}
	if _, err := ds.Get(ctx, taskType, "v-none"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("explicit-None delivery leaked a write")
	}
	// Zero → a real precondition; an absent record is version 0, so it applies.
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{"mode": "zero"}); err != nil {
		t.Fatalf("if_version=0: %v", err)
	}
	if got := mustGet(t, ds, taskType, "v-zero"); got.Title != "t" {
		t.Fatalf("if_version=0 did not apply against an absent (v0) row: %+v", got)
	}
}

func TestSDKBuilderLocalValidation(t *testing.T) {
	t.Parallel()
	// The builder rejects deterministic body mistakes locally (a clear error that
	// parks once), rather than staging them for the engine to park on: an illegal
	// id, a self-merge, and a None page continuation.
	_, ops := newFnDataset(t, nil,
		pyFn("badid", map[string]any{}, []any{taskType}, `
def main(input, host):
    host.effects.put("tasks.substrate.reamde.dev/task", "people 123", properties={"title": "t"})
    return {}
`),
		pyFn("selfmerge", map[string]any{"permissions": map[string]any{"mutations": []any{"merge"}}},
			[]any{taskType}, `
def main(input, host):
    host.effects.merge("tasks.substrate.reamde.dev/task", "x", "x")
    return {}
`),
		pyFn("nilmore", map[string]any{}, []any{taskType}, `
def main(input, host):
    return {"more": host.page.more(None)}
`))
	ctx := context.Background()
	cases := []struct{ fn, want string }{
		{"badid", "not a record id"},
		{"selfmerge", "same id"},
		{"nilmore", "cursor is required"},
	}
	for _, tc := range cases {
		if _, _, err := ops.CallFunction(ctx, fnAuthority+"/"+tc.fn, map[string]any{}); err == nil ||
			!strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%s: want %q, got %v", tc.fn, tc.want, err)
		}
	}
}

func TestSDKBuilderGoGuardedPatch(t *testing.T) {
	t.Parallel()
	// The Go typed read → guarded write idiom end to end: Records.Get returns a
	// *ReadRecord whose int64 Version feeds substratefn.Version(e.Version) on a patch.
	// A matching version applies; the same stale version then conflicts.
	ds, ops := newFnDataset(t, nil, goFn("goguard",
		map[string]any{"permissions": map[string]any{"reads": map[string]any{"kinds": []any{taskType}}}},
		[]any{taskType}, `
import "substratefn.local/substratefn"

func Main(in *substratefn.Input, host *substratefn.Host) (*substratefn.Result, error) {
	a, _ := in.Args.(map[string]any)
	id, _ := a["id"].(string)
	e, err := host.Records.Get("tasks.substrate.reamde.dev/task", id)
	if err != nil {
		return nil, err
	}
	if e == nil {
		return &substratefn.Result{Output: map[string]any{"found": false}}, nil
	}
	host.Effects.Patch(substratefn.PatchEffect{
		Kind: "tasks.substrate.reamde.dev/task", ID: id,
		Properties: map[string]any{"title": "guarded"},
		IfVersion:  substratefn.Version(e.Version),
	})
	return &substratefn.Result{Output: map[string]any{"version": e.Version}}, nil
}
`))
	ctx := context.Background()
	fn := fnAuthority + "/goguard"
	task := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"title": "v"}})

	// The typed read's version feeds a matching guarded patch — it applies.
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{"id": task.ID}); err != nil {
		t.Fatalf("guarded patch: %v", err)
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.Title != "guarded" {
		t.Fatalf("guarded patch did not apply: %+v", got)
	}

	// Concurrently advance the row, then re-run: the body reads the FRESH
	// version, so a second guarded patch also applies (proving it read the real
	// int64 version, not a rounded/zero one that would conflict or clobber).
	if _, err := ds.Patch(ctx, owner, task.Kind, task.ID, substrate.PatchInput{Properties: map[string]any{"title": "moved"}}); err != nil {
		t.Fatalf("owner patch: %v", err)
	}
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{"id": task.ID}); err != nil {
		t.Fatalf("guarded patch after move: %v", err)
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.Title != "guarded" {
		t.Fatalf("second guarded patch did not apply: %+v", got)
	}
}

func TestEffectIfVersionPrecondition(t *testing.T) {
	t.Parallel()
	// A body that stages one put or patch with an ifVersion precondition drawn
	// from its args — proving the effect decode threads ifVersion into the
	// write path: a matching version applies, a stale one conflicts, and a
	// conflicted delivery leaks no write.
	ds, ops := newFnDataset(t, nil, pyFn("verwrite", map[string]any{}, []any{taskType}, `
def main(input, host):
    a = input["args"]
    if a["action"] == "put":
        host.effects.put("tasks.substrate.reamde.dev/task", a["id"],
                         properties=a.get("properties"), if_version=a.get("ifVersion"))
    else:
        host.effects.patch("tasks.substrate.reamde.dev/task", a["id"],
                           properties=a.get("properties"), if_version=a.get("ifVersion"))
    return {}
`))
	ctx := context.Background()
	fn := fnAuthority + "/verwrite"
	call := func(args map[string]any) error {
		_, _, err := ops.CallFunction(ctx, fn, args)
		return err
	}

	task := mustPut(t, ds, owner, substrate.PutInput{Kind: taskType, Properties: map[string]any{"title": "v"}})
	v1 := task.Version

	// A patch effect with the MATCHING version applies and advances the version.
	if err := call(map[string]any{
		"action": "patch", "id": task.ID,
		"ifVersion": v1, "properties": map[string]any{"title": "patched"},
	}); err != nil {
		t.Fatalf("matching-version patch: %v", err)
	}
	after := mustGet(t, ds, task.Kind, task.ID)
	if after.Title != "patched" {
		t.Fatalf("matching patch did not apply: %q", after.Title)
	}
	v2 := after.Version
	if v2 == v1 {
		t.Fatalf("version did not advance past %d", v1)
	}

	// The SAME (now stale) version conflicts, and nothing lands.
	if err := call(map[string]any{
		"action": "patch", "id": task.ID,
		"ifVersion": v1, "properties": map[string]any{"title": "stale"},
	}); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("stale-version patch: want ErrConflict, got %v", err)
	}
	if unchanged := mustGet(t, ds, task.Kind, task.ID); unchanged.Title != "patched" {
		t.Fatalf("stale patch leaked a write: %q", unchanged.Title)
	}

	// A put effect (upsert) honors ifVersion the same way.
	if err := call(map[string]any{
		"action": "put", "id": task.ID,
		"ifVersion": v2, "properties": map[string]any{"title": "reput"},
	}); err != nil {
		t.Fatalf("matching-version put: %v", err)
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.Title != "reput" {
		t.Fatalf("matching put did not apply: %q", got.Title)
	}
	if err := call(map[string]any{
		"action": "put", "id": task.ID,
		"ifVersion": v2, "properties": map[string]any{"title": "reput2"},
	}); !errors.Is(err, substrate.ErrConflict) {
		t.Fatalf("stale-version put: want ErrConflict, got %v", err)
	}
}

func TestSDKProposeStagesChangeRequest(t *testing.T) {
	t.Parallel()
	// propose is sugar over the ordinary put effect: the body names a target, a
	// diff and its reason, and what lands is a change request the OWNER decides
	// on — nothing on the target until then. All three ops travel the same
	// helper, and the function's emit names the request kind alone: a proposing
	// body needs no write grant on what it proposes about.
	ds, ops := newFnDataset(t, nil, pyFn("proposer", map[string]any{}, []any{requestKind}, `
def main(input, host):
    a = input["args"]
    host.effects.propose(a["id"], "tasks.substrate.reamde.dev/task", a["target"],
                         diff=a.get("diff"), op=a.get("op", "patch"),
                         rationale=a.get("rationale"))
    return {}
`))
	ctx := context.Background()
	fn := fnAuthority + "/proposer"

	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskType, ID: "t-proposed", Properties: map[string]any{"title": "draft"},
	})

	// A patch proposal: the request carries the target edge, the diff and the
	// rationale, and the task is untouched until the owner accepts.
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{
		"id": "req-sdk-patch", "target": task.ID, "rationale": "the transcript says Friday",
		"diff": map[string]any{"description": "due Friday"},
	}); err != nil {
		t.Fatalf("propose patch: %v", err)
	}
	req := mustGet(t, ds, requestKind, "req-sdk-patch")
	if req.Properties["op"] != "patch" || req.Properties["rationale"] != "the transcript says Friday" {
		t.Fatalf("proposed request: %+v", req.Properties)
	}
	if req.Properties["decision"] != "proposed" {
		t.Fatalf("request is not proposed: %+v", req.Properties)
	}
	if got := mustGet(t, ds, taskType, task.ID); got.Properties["description"] != nil {
		t.Fatalf("a proposal wrote the target: %+v", got.Properties)
	}
	if err := accept(t, ds, "req-sdk-patch"); err != nil {
		t.Fatalf("accept the proposed patch: %v", err)
	}
	if got := mustGet(t, ds, taskType, task.ID); got.Properties["description"] != "due Friday" {
		t.Fatalf("the accepted proposal did not apply: %+v", got.Properties)
	}

	// A create proposal names the kind and id the accept would mint, so the
	// record is born only once somebody agrees.
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{
		"id": "req-sdk-create", "op": "create", "target": "t-minted",
		"diff": map[string]any{"title": "Follow up"},
	}); err != nil {
		t.Fatalf("propose create: %v", err)
	}
	if _, err := ds.Get(ctx, taskType, "t-minted"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("a create proposal minted the record early: %v", err)
	}
	if err := accept(t, ds, "req-sdk-create"); err != nil {
		t.Fatalf("accept the proposed create: %v", err)
	}
	if got := mustGet(t, ds, taskType, "t-minted"); got.Properties["title"] != "Follow up" {
		t.Fatalf("the accepted create did not mint: %+v", got.Properties)
	}

	// A delete proposal carries no diff, and tombstones on accept.
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{
		"id": "req-sdk-delete", "op": "delete", "target": "t-minted",
	}); err != nil {
		t.Fatalf("propose delete: %v", err)
	}
	if err := accept(t, ds, "req-sdk-delete"); err != nil {
		t.Fatalf("accept the proposed delete: %v", err)
	}
	if got := mustGet(t, ds, taskType, "t-minted"); got.DeletedAt == nil {
		t.Fatalf("the accepted delete did not tombstone: %+v", got)
	}

	// The builder validates locally: a patch proposal with no diff is a body
	// error, not an engine park.
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{
		"id": "req-sdk-empty", "target": task.ID,
	}); err == nil || !strings.Contains(err.Error(), "needs a diff") {
		t.Fatalf("a diffless patch proposal: %v", err)
	}
	// And a delete carrying a diff is refused on PRESENCE, so an empty one is
	// a body error here rather than a write the engine's admission parks on.
	if _, _, err := ops.CallFunction(ctx, fn, map[string]any{
		"id": "req-sdk-emptydel", "op": "delete", "target": task.ID,
		"diff": map[string]any{},
	}); err == nil || !strings.Contains(err.Error(), "proposes no values") {
		t.Fatalf("a delete proposal carrying an empty diff: %v", err)
	}
}

func TestSDKProposeGoRuntime(t *testing.T) {
	t.Parallel()
	// The Go mirror, compiled and run for real: the same helper, the same
	// request, the same accept.
	ds, ops := newFnDataset(t, nil, goFn("goproposer", map[string]any{}, []any{requestKind}, `
import "substratefn.local/substratefn"

func Main(in *substratefn.Input, host *substratefn.Host) (*substratefn.Result, error) {
	a, _ := in.Args.(map[string]any)
	host.Effects.Propose(substratefn.ProposeEffect{
		ID: "req-go", TargetKind: "tasks.substrate.reamde.dev/task",
		TargetID:  a["target"].(string),
		Diff:      map[string]any{"description": "from Go"},
		Rationale: "the mirror",
	})
	return &substratefn.Result{}, nil
}
`))
	ctx := context.Background()
	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: taskType, ID: "t-go-proposed", Properties: map[string]any{"title": "draft"},
	})
	if _, _, err := ops.CallFunction(ctx, fnAuthority+"/goproposer", map[string]any{"target": task.ID}); err != nil {
		t.Fatalf("call: %v", err)
	}
	req := mustGet(t, ds, requestKind, "req-go")
	if req.Properties["rationale"] != "the mirror" {
		t.Fatalf("go proposal: %+v", req.Properties)
	}
	if err := accept(t, ds, "req-go"); err != nil {
		t.Fatalf("accept the go proposal: %v", err)
	}
	if got := mustGet(t, ds, taskType, task.ID); got.Properties["description"] != "from Go" {
		t.Fatalf("the accepted go proposal did not apply: %+v", got.Properties)
	}
}
