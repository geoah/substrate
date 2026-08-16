package engine_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/geoah/substrate/internal/engine/enginetest"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

const (
	gmail  = substrate.Actor("connector:gmail")
	slack  = substrate.Actor("connector:slack")
	people = substrate.Actor("connector:people")
	beeper = substrate.Actor("connector:beeper")
	engram = substrate.Actor("engram")
	owner  = substrate.ActorAPI
)

func TestPutCreatesAndSuppressesNoops(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acc := mustPut(t, ds, gmail, substrate.PutInput{
		Kind:       enginetest.AccountType,
		ID:         "gmail:george@acme.com",
		Properties: map[string]any{"provider": "gmail", "label": "Work", "status": "ok"},
	})
	if acc.Properties["title"] != "Work" {
		t.Fatalf("displayTemplate should derive the title, got %v", acc.Properties["title"])
	}
	if acc.Version != 1 {
		t.Fatalf("new record version = %d", acc.Version)
	}

	before := maxSeq(t, ds)
	// Byte-identical re-put with the props in a different order.
	again := mustPut(t, ds, gmail, substrate.PutInput{
		Kind:       enginetest.AccountType,
		ID:         "gmail:george@acme.com",
		Properties: map[string]any{"status": "ok", "label": "Work", "provider": "gmail"},
	})
	if again.ID != acc.ID {
		t.Fatalf("a put at the writer's own id created a second record: %s vs %s", again.ID, acc.ID)
	}
	if again.Version != acc.Version {
		t.Fatalf("no-op bumped version %d → %d", acc.Version, again.Version)
	}
	if !again.UpdatedAt.Equal(acc.UpdatedAt) {
		t.Fatalf("no-op moved updated_at %v → %v", acc.UpdatedAt, again.UpdatedAt)
	}
	if got := changesSince(t, ds, before); len(got) != 0 {
		t.Fatalf("no-op wrote %d changelog rows: %+v", len(got), got)
	}

	// A real change bumps version and writes exactly one row.
	changed := mustPut(t, ds, gmail, substrate.PutInput{
		Kind:       enginetest.AccountType,
		ID:         "gmail:george@acme.com",
		Properties: map[string]any{"status": "erroring"},
	})
	if changed.Version != acc.Version+1 {
		t.Fatalf("version = %d, want %d", changed.Version, acc.Version+1)
	}
	rows := changesSince(t, ds, before)
	if len(rows) != 1 || rows[0].Op != substrate.OpPut || rows[0].RecordID != acc.ID {
		t.Fatalf("expected one put row, got %+v", rows)
	}
	if changed.Properties["provider"] != "gmail" {
		t.Fatal("put should merge properties, not replace them")
	}
}

func TestNoopSuppressionAcrossRecords(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acc := mustPut(t, ds, gmail, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "gmail:a",
		Properties: map[string]any{"provider": "gmail"},
	})
	conv := mustPut(t, ds, gmail, substrate.PutInput{
		Kind: "conversation", ID: "slack:t1",
		Properties: map[string]any{"kind": "direct", "name": "Alex"},
		Edges:      []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acc.ID}}},
	})

	// Labels, edges and annotations are all no-op suppressed.
	mustPatch(t, ds, owner, conv.Kind, conv.ID, substrate.PatchInput{
		Labels:      map[string]any{"owner/pinned": true},
		Annotations: map[string]any{"owner/note": map[string]any{"a": 1}},
	})
	v := mustGet(t, ds, conv.Kind, conv.ID).Version
	before := maxSeq(t, ds)

	mustPatch(t, ds, owner, conv.Kind, conv.ID, substrate.PatchInput{
		Labels:      map[string]any{"owner/pinned": true},
		Annotations: map[string]any{"owner/note": map[string]any{"a": 1}},
	})
	mustPut(t, ds, gmail, substrate.PutInput{
		Kind: "conversation", ID: "slack:t1",
		Properties: map[string]any{"kind": "direct", "name": "Alex"},
		Edges:      []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acc.ID}}},
	})
	if got := mustGet(t, ds, conv.Kind, conv.ID).Version; got != v {
		t.Fatalf("identical metadata writes bumped version %d → %d", v, got)
	}
	if rows := changesSince(t, ds, before); len(rows) != 0 {
		t.Fatalf("identical metadata writes logged %d rows: %+v", len(rows), rows)
	}

	// A new label is a real change: one row, one bump.
	mustPatch(t, ds, owner, conv.Kind, conv.ID, substrate.PatchInput{Labels: map[string]any{"owner/seen": true}})
	if got := mustGet(t, ds, conv.Kind, conv.ID).Version; got != v+1 {
		t.Fatalf("label write did not bump version: %d", got)
	}
	if rows := changesSince(t, ds, before); len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	_ = ctx
}

func TestVersionCAS(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	task := mustPut(t, ds, owner, substrate.PutInput{Kind: "task", Properties: map[string]any{"name": "Ship it"}})

	if _, err := ds.Patch(ctx, owner, task.Kind, task.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "Ship it now"}, IfVersion: ptr(int64(99)),
	}); err == nil {
		t.Fatal("expected a CAS conflict")
	} else {
		wantErr(t, err, substrate.ErrConflict, "stale ifVersion")
	}
	ok := mustPatch(t, ds, owner, task.Kind, task.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "Ship it now"}, IfVersion: ptr(task.Version),
	})
	if ok.Version != task.Version+1 || ok.Title != "Ship it now" {
		t.Fatalf("CAS patch = %+v", ok)
	}
}

// NOTHING RANKS WRITERS. The property-manager ledger records who
// last had a change accepted, for attribution; it does not decide who may
// write. Anyone overwrites anything.
func TestNoWriterOutranksAnother(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	c := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alexandros Papas"},
	})
	before := maxSeq(t, ds)

	got := mustPatch(t, ds, slack, c.Kind, c.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "alex", "displayName": "alex"},
	})
	if got.Properties["name"] != "alex" {
		t.Fatalf("a connector write was blocked by the owner's: %v", got.Properties["name"])
	}
	if got.Properties["displayName"] != "alex" {
		t.Fatal("the other property should have been accepted too")
	}
	rows := changesSince(t, ds, before)
	if len(rows) != 1 {
		t.Fatalf("expected one row, got %d", len(rows))
	}
	if _, dropped := rows[0].Payload["dropped"]; dropped {
		t.Fatalf("no write is dropped any more: %+v", rows[0].Payload)
	}

	// The owner writes back over the connector, just as freely.
	if got := mustPatch(t, ds, owner, c.Kind, c.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "Alex"},
	}); got.Properties["name"] != "Alex" {
		t.Fatalf("owner write = %v", got.Properties["name"])
	}

	// And a re-assertion of the same value is silent.
	before = maxSeq(t, ds)
	mustPatch(t, ds, slack, c.Kind, c.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "Alex"},
	})
	if rows := changesSince(t, ds, before); len(rows) != 0 {
		t.Fatalf("identical re-assert logged %d rows", len(rows))
	}
}

func TestMetadataNamespaces(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	task := mustPut(t, ds, owner, substrate.PutInput{Kind: "task", Properties: map[string]any{"name": "t"}})

	if _, err := ds.Patch(ctx, engram, task.Kind, task.ID, substrate.PatchInput{
		Labels: map[string]any{"owner/pinned": true},
	}); err == nil {
		t.Fatal("expected a namespace violation")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "foreign label namespace")
	}
	if _, err := ds.Patch(ctx, engram, task.Kind, task.ID, substrate.PatchInput{
		Annotations: map[string]any{"judge/rubric": map[string]any{"x": 1}},
	}); err == nil {
		t.Fatal("expected a namespace violation")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "foreign annotation namespace")
	}
	// Its own namespace is fine, and so is the owner touching anything.
	mustPatch(t, ds, engram, task.Kind, task.ID, substrate.PatchInput{Labels: map[string]any{"engram/interrupt": 74}})
	mustPatch(t, ds, owner, task.Kind, task.ID, substrate.PatchInput{Labels: map[string]any{"engram/interrupt": 12}})
	// A dotted connector actor owns its dotted namespace.
	mustPatch(t, ds, gmail, task.Kind, task.ID, substrate.PatchInput{
		Annotations: map[string]any{"connector:gmail/state": map[string]any{"ok": true}},
	})
	if _, err := ds.Patch(ctx, gmail, task.Kind, task.ID, substrate.PatchInput{
		Labels: map[string]any{"unnamespaced": 1},
	}); err == nil {
		t.Fatal("expected an unnamespaced key to be rejected")
	}
	e := mustGet(t, ds, task.Kind, task.ID)
	if e.Labels["engram/interrupt"].(float64) != 12 {
		t.Fatalf("labels = %v", e.Labels)
	}
}

func TestMachineInitialAndTransitions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// ONE initial state, whoever writes; a creation may NAME any declared
	// state.
	proposed := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "task", Properties: map[string]any{"name": "Send rack layout", "status": "proposed"},
	})
	if proposed.Properties["status"] != "proposed" {
		t.Fatalf("named state at birth = %v", proposed.Properties)
	}
	ownerTask := mustPut(t, ds, owner, substrate.PutInput{Kind: "task", Properties: map[string]any{"name": "Mine"}})
	if ownerTask.Properties["status"] != "open" {
		t.Fatalf("declared initial = %v", ownerTask.Properties)
	}
	// Only a DECLARED state, though.
	if _, err := ds.Put(ctx, engram, substrate.PutInput{
		Kind: "task", Properties: map[string]any{"name": "x", "status": "nosuch"},
	}); err == nil {
		t.Fatal("expected a validation error")
	} else {
		wantErr(t, err, substrate.ErrValidation, "undeclared state at birth")
	}
	// Transitions carry no guard: anyone may perform any declared one.
	accepted := mustPatch(t, ds, engram, proposed.Kind, proposed.ID, substrate.PatchInput{
		Properties: map[string]any{"status": "open"},
	})
	if accepted.Properties["status"] != "open" {
		t.Fatalf("states = %v", accepted.Properties)
	}
	// Undeclared transitions are refused.
	if _, err := ds.Patch(ctx, owner, proposed.Kind, proposed.ID, substrate.PatchInput{
		Properties: map[string]any{"status": "proposed"},
	}); err == nil {
		t.Fatal("expected a guard error for an undeclared transition")
	}
	// Stamps land as datetime properties.
	done := mustPatch(t, ds, owner, proposed.Kind, proposed.ID, substrate.PatchInput{
		Properties: map[string]any{"status": "done"},
	})
	stamp, ok := done.Properties["completedAt"].(string)
	if !ok {
		t.Fatalf("completedAt not stamped: %v", done.Properties)
	}
	if _, err := time.Parse(time.RFC3339Nano, stamp); err != nil {
		t.Fatalf("completedAt is not an instant: %q", stamp)
	}
}

func TestMutationRequestApplyDiff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	task := mustPut(t, ds, owner, substrate.PutInput{Kind: "task", Properties: map[string]any{"name": "Draft the memo"}})
	req := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"rationale": "the transcript says Friday",
			"diff": map[string]any{
				"properties": map[string]any{"description": "due Friday"},
				"ifVersion":  task.Version,
			},
		},
		// The target is an EDGE (MODEL §11.5).
		Edges: []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: "tasks.substrate.reamde.dev/task", ID: task.ID}}},
	})
	if req.Properties["decision"] != "proposed" {
		t.Fatalf("req states = %v", req.Properties)
	}
	accepted := mustPatch(t, ds, owner, req.Kind, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(req.Version),
	})
	if accepted.Properties["decision"] != "accepted" {
		t.Fatalf("req states = %v", accepted.Properties)
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.Properties["description"] != "due Friday" {
		t.Fatalf("applyDiff did not land: %v", got.Properties)
	}

	// Deciding without the request version is refused: the reviewer must accept
	// the envelope it read.
	needsVersion := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"diff": map[string]any{"properties": map[string]any{"description": "later"}, "ifVersion": task.Version + 1},
		},
		Edges: []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: "tasks.substrate.reamde.dev/task", ID: task.ID}}},
	})
	if _, err := ds.Patch(ctx, owner, needsVersion.Kind, needsVersion.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"},
	}); err == nil {
		t.Fatal("accept without ifVersion should be refused")
	} else {
		wantErr(t, err, substrate.ErrConflict, "accept without ifVersion")
	}
	// A STALE request version is refused too.
	if _, err := ds.Patch(ctx, owner, needsVersion.Kind, needsVersion.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(needsVersion.Version + 9),
	}); err == nil {
		t.Fatal("accept with a stale request version should be refused")
	} else {
		wantErr(t, err, substrate.ErrConflict, "accept with stale request version")
	}

	// A stale diff loses the CAS: the transition fails, the request stays
	// proposed, and the conflict is annotated.
	stale := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"diff": map[string]any{
				"properties": map[string]any{"description": "due Monday"},
				"ifVersion":  1,
			},
		},
		Edges: []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: "tasks.substrate.reamde.dev/task", ID: task.ID}}},
	})
	if _, err := ds.Patch(ctx, owner, stale.Kind, stale.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(stale.Version),
	}); err == nil {
		t.Fatal("expected a conflict")
	} else {
		wantErr(t, err, substrate.ErrConflict, "stale applyDiff")
	}
	after := mustGet(t, ds, stale.Kind, stale.ID)
	if after.Properties["decision"] != "proposed" {
		t.Fatalf("failed transition should roll back: %v", after.Properties)
	}
	if after.Annotations["substrate/conflict"] == nil {
		t.Fatalf("conflict annotation missing: %v", after.Annotations)
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.Properties["description"] != "due Friday" {
		t.Fatalf("target changed despite the conflict: %v", got.Properties)
	}
}

// Issue 004: an accepted request whose diff would change NOTHING must fail the
// transition visibly, never stamp decidedAt and apply nothing. The wrapper-less
// diff that used to fail here fails at ADMISSION's mercy instead: it is wrapped
// on the way in (requestadmit_db_test.go), so what is left to fail at accept is
// the diff the target already satisfies — and a diff that was already STORED
// wrapper-less, which the strict decode still refuses.
func TestAcceptedNoOpDiffFailsTransition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "task", Properties: map[string]any{"name": "Draft", "description": "already here"},
	})

	// A wrapper-less diff names `description` at the top level. Admission wraps
	// it under `properties` against the target's kind, so the accept has the
	// shape it decodes and the change applies.
	bare := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"diff": map[string]any{"description": "wrapper-less"},
		},
		Edges: []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: "tasks.substrate.reamde.dev/task", ID: task.ID}}},
	})
	if _, err := ds.Patch(ctx, owner, bare.Kind, bare.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(bare.Version),
	}); err != nil {
		t.Fatalf("an admitted bare diff did not apply: %v", err)
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.Properties["description"] != "wrapper-less" {
		t.Fatalf("the wrapped diff did not land: %+v", got.Properties)
	}

	// A well-formed diff that re-asserts the stored value applies no change. Its
	// own target, because the accept above moved the first one.
	settled := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "task", Properties: map[string]any{"name": "Settled", "description": "already here"},
	})
	noop := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"diff": map[string]any{"properties": map[string]any{"description": "already here"}},
		},
		Edges: []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: "tasks.substrate.reamde.dev/task", ID: settled.ID}}},
	})
	if _, err := ds.Patch(ctx, owner, noop.Kind, noop.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(noop.Version),
	}); err == nil {
		t.Fatal("a no-op diff accepted green")
	} else {
		wantErr(t, err, substrate.ErrConflict, "no-op diff")
	}
	if after := mustGet(t, ds, noop.Kind, noop.ID); after.Properties["decision"] != "proposed" {
		t.Fatalf("no-op request should stay proposed: %+v", after.Properties)
	}
}

// Issue 005: an accepted create request MINTS a new record through the ordinary
// write path — create-if-absent, idempotent on replay (a second create request
// naming the same id is a verified no-op, never a reset).
func TestChangeRequestCreateMints(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	const targetID = "created-task-1"
	req := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"op":         "create",
			"targetKind": "tasks.substrate.reamde.dev/task",
			"targetId":   targetID,
			"rationale":  "the transcript asks for a follow-up",
			"diff":       map[string]any{"properties": map[string]any{"name": "Follow up with Dana", "description": "before Friday"}},
		},
	})
	// The target does not exist yet.
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", targetID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("target existed before accept: %v", err)
	}
	accepted := mustPatch(t, ds, owner, req.Kind, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(req.Version),
	})
	if accepted.Properties["decision"] != "accepted" || accepted.Properties["decidedAt"] == nil {
		t.Fatalf("create request not accepted: %+v", accepted.Properties)
	}
	minted := mustGet(t, ds, "tasks.substrate.reamde.dev/task", targetID)
	if minted.Properties["name"] != "Follow up with Dana" || minted.Properties["description"] != "before Friday" {
		t.Fatalf("minted record wrong: %+v", minted.Properties)
	}
	// It is a real task, born in the initial state.
	if minted.Properties["status"] != "open" {
		t.Fatalf("minted task status = %v", minted.Properties["status"])
	}

	// Same-shape replay: a second create request naming the same id with the
	// SAME properties is a verified no-op on accept — the live record already
	// IS what the request would mint.
	replay := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"op":         "create",
			"targetKind": "tasks.substrate.reamde.dev/task",
			"targetId":   targetID,
			"diff":       map[string]any{"properties": map[string]any{"name": "Follow up with Dana", "description": "before Friday"}},
		},
	})
	if _, err := ds.Patch(ctx, owner, replay.Kind, replay.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(replay.Version),
	}); err != nil {
		t.Fatalf("same-shape replay accept errored instead of no-op: %v", err)
	}
	if again := mustGet(t, ds, "tasks.substrate.reamde.dev/task", targetID); again.Properties["name"] != "Follow up with Dana" {
		t.Fatalf("same-shape replay changed the record: %+v", again.Properties)
	}

	// Divergent replay: a second create request naming the same id with
	// DIFFERENT properties is NOT a green no-op — it collides, the request
	// stays proposed and annotated, and the live record is untouched
	//.
	diverge := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"op":         "create",
			"targetKind": "tasks.substrate.reamde.dev/task",
			"targetId":   targetID,
			"diff":       map[string]any{"properties": map[string]any{"name": "SHOULD NOT WIN"}},
		},
	})
	if _, err := ds.Patch(ctx, owner, diverge.Kind, diverge.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(diverge.Version),
	}); err == nil {
		t.Fatal("a divergent second create accepted green")
	} else {
		wantErr(t, err, substrate.ErrConflict, "divergent create collision")
	}
	if again := mustGet(t, ds, "tasks.substrate.reamde.dev/task", targetID); again.Properties["name"] != "Follow up with Dana" {
		t.Fatalf("divergent replay reset the record: %+v", again.Properties)
	}
	if after := mustGet(t, ds, diverge.Kind, diverge.ID); after.Properties["decision"] != "proposed" ||
		after.Annotations["substrate/conflict"] == nil {
		t.Fatalf("divergent create request should stay proposed + annotated: %+v", after.Properties)
	}
}

// review-p0 #3: a create collision is idempotent ONLY when the live record is
// exactly what the request would mint. Identity is the (type, id) PAIR
// , so an id held by another type does not collide at all; a
// tombstoned row of the same type still conflicts — a create never resurrects.
func TestChangeRequestCreateDivergence(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// An id held by ANOTHER type names nothing under (type, id) identity: the
	// task create lands beside the project, and neither touches the other.
	proj := mustPut(t, ds, owner, substrate.PutInput{Kind: "project", ID: "occupied-1", Properties: map[string]any{"name": "a project"}})
	otherType := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"op": "create", "targetKind": "tasks.substrate.reamde.dev/task", "targetId": "occupied-1",
			"diff": map[string]any{"properties": map[string]any{"name": "a task"}},
		},
	})
	if _, err := ds.Patch(ctx, owner, otherType.Kind, otherType.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(otherType.Version),
	}); err != nil {
		t.Fatalf("a create beside another type's id should land: %v", err)
	}
	if got := mustGet(t, ds, "tasks.substrate.reamde.dev/task", "occupied-1"); got.Properties["name"] != "a task" {
		t.Fatalf("per-type create did not mint: %+v", got.Properties)
	}
	if got := mustGet(t, ds, proj.Kind, proj.ID); got.Properties["name"] != "a project" {
		t.Fatalf("the other type's row was touched: %+v", got.Properties)
	}

	// A tombstoned row at the id: a create neither resurrects nor overwrites.
	live := mustPut(t, ds, owner, substrate.PutInput{Kind: "task", ID: "gone-1", Properties: map[string]any{"name": "was here"}})
	if _, err := ds.Delete(ctx, owner, live.Kind, live.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	tomb := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"op": "create", "targetKind": "tasks.substrate.reamde.dev/task", "targetId": "gone-1",
			"diff": map[string]any{"properties": map[string]any{"name": "was here"}},
		},
	})
	if _, err := ds.Patch(ctx, owner, tomb.Kind, tomb.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(tomb.Version),
	}); err == nil {
		t.Fatal("create against a tombstone accepted green")
	} else {
		wantErr(t, err, substrate.ErrConflict, "tombstoned create collision")
	}
	if got := mustGet(t, ds, live.Kind, "gone-1"); got.DeletedAt == nil {
		t.Fatalf("create resurrected a tombstone: %+v", got)
	}
}

// Issue 005: an accepted delete request tombstones its target, idempotent on
// replay (an already-gone target is a verified no-op).
func TestChangeRequestDeleteTombstones(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	task := mustPut(t, ds, owner, substrate.PutInput{Kind: "task", Properties: map[string]any{"name": "throwaway"}})
	req := mustPut(t, ds, engram, substrate.PutInput{
		Kind:       "recordpatchrequest",
		Properties: map[string]any{"op": "delete", "rationale": "duplicate"},
		Edges:      []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: "tasks.substrate.reamde.dev/task", ID: task.ID}}},
	})
	if _, err := ds.Patch(ctx, owner, req.Kind, req.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(req.Version),
	}); err != nil {
		t.Fatalf("accept delete: %v", err)
	}
	if got := mustGet(t, ds, task.Kind, task.ID); got.DeletedAt == nil {
		t.Fatalf("target not tombstoned: %+v", got)
	}
	// Replay: a second delete request on the already-gone target is a no-op.
	replay := mustPut(t, ds, engram, substrate.PutInput{
		Kind:       "recordpatchrequest",
		Properties: map[string]any{"op": "delete"},
		Edges:      []substrate.EdgeInput{{Rel: "target", To: substrate.EdgeRef{Kind: "tasks.substrate.reamde.dev/task", ID: task.ID}}},
	})
	if _, err := ds.Patch(ctx, owner, replay.Kind, replay.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(replay.Version),
	}); err != nil {
		t.Fatalf("replay delete accept errored instead of no-op: %v", err)
	}
}

func TestSecretsRedacted(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)
	info, secret, err := ds.MintToken(context.Background(), "cli", nil)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if secret == "" {
		t.Fatal("secret not returned")
	}
	e := mustGet(t, ds, "core.substrate.reamde.dev/token", info.ID)
	if e.Properties["hash"] != "<redacted>" {
		t.Fatalf("secret property leaked: %v", e.Properties["hash"])
	}
	page, err := ds.List(context.Background(), substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/token"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, ent := range page.Records {
		if ent.Properties["hash"] != "<redacted>" {
			t.Fatalf("secret leaked through list: %v", ent.Properties)
		}
	}
}

func TestSystemTypesRejectGenericWrites(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	// A schema kind is a REAL row now (schema is records), but it only
	// writes through admission: an id-less, definition-less put is refused by
	// the admission path, not silently stored.
	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "core.substrate.reamde.dev/kind", Properties: map[string]any{"name": "evil"},
	}); err == nil {
		t.Fatal("expected a refusal")
	} else {
		wantErr(t, err, substrate.ErrValidation, "schema write without an identity")
	}
	info, _, err := ds.MintToken(ctx, "cli", nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ds.Patch(ctx, owner, "core.substrate.reamde.dev/token", info.ID, substrate.PatchInput{Properties: map[string]any{"title": "x"}}); err == nil {
		t.Fatal("expected forbidden")
	}
	// Revoking a token is a delete.
	if _, err := ds.Delete(ctx, owner, "core.substrate.reamde.dev/token", info.ID); err != nil {
		t.Fatalf("token revoke: %v", err)
	}
	// A repository's lifecycle machine is the one system transition the generic
	// surface may drive.
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"core.substrate.reamde.dev/repository"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 {
		t.Fatalf("repository projection = %v", ids(page.Records))
	}
	repository := page.Records[0]
	if _, err := ds.Patch(ctx, substrate.ActorSystem, repository.Kind, repository.ID, substrate.PatchInput{
		Properties: map[string]any{"lifecycle": "active"},
	}); err != nil {
		t.Fatalf("repository lifecycle transition: %v", err)
	}
	if _, err := ds.Patch(ctx, substrate.ActorSystem, repository.Kind, repository.ID, substrate.PatchInput{
		Properties: map[string]any{"name": "hijack"},
	}); err == nil {
		t.Fatal("expected forbidden for a non-transition repository patch")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "repository property write")
	}
	// Deleting a merge record is not a generic operation either.
	if _, err := ds.Delete(ctx, owner, repository.Kind, repository.ID); err == nil {
		t.Fatal("expected forbidden")
	}
}

// A put onto a TOMBSTONE restores that record. A provider that cancels an
// event and then un-cancels it, or a contact restored from Trash, re-syncs
// onto the id it always had — and without this the row stays invisible
// forever while every later sync reports success.
func TestPutResurrectsATombstone(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acc := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "gcal:acct",
		Properties: map[string]any{"provider": "gcal", "label": "Work"},
	})
	cal := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendar", ID: "gcal:primary",
		Properties: map[string]any{"name": "Primary", "timezone": "Europe/Athens"},
		Edges:      []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acc.ID}}},
	})
	event := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendarevent", ID: "gcal:evt-1",
		Properties: map[string]any{
			"summary": "Standup", "at": "2026-08-05T13:00:00Z", "endsAt": "2026-08-05T13:30:00Z",
		},
		Edges: []substrate.EdgeInput{{Rel: "calendar", To: substrate.EdgeRef{ID: cal.ID}}},
	})

	// The provider cancels it.
	if _, err := ds.Delete(ctx, gcal, event.Kind, event.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got := mustGet(t, ds, event.Kind, event.ID); got.DeletedAt == nil {
		t.Fatal("delete did not tombstone")
	}

	// The user restores it and the next sync re-puts the same record.
	before := maxSeq(t, ds)
	back := mustPut(t, ds, gcal, substrate.PutInput{
		Kind: "calendarevent", ID: "gcal:evt-1",
		Properties: map[string]any{
			"summary": "Standup", "at": "2026-08-05T13:00:00Z", "endsAt": "2026-08-05T13:30:00Z",
		},
		Edges: []substrate.EdgeInput{{Rel: "calendar", To: substrate.EdgeRef{ID: cal.ID}}},
	})
	if back.ID != event.ID {
		t.Fatalf("restore minted %s, want the same record %s", back.ID, event.ID)
	}
	if back.DeletedAt != nil {
		t.Fatalf("the returned record still claims to be deleted: %+v", back.DeletedAt)
	}
	if got := mustGet(t, ds, event.Kind, event.ID); got.DeletedAt != nil {
		t.Fatalf("the stored row is still tombstoned: %+v", got.DeletedAt)
	}
	if back.Version <= event.Version+1 {
		t.Fatalf("restore did not bump the version past the delete: %d", back.Version)
	}
	rows := changesSince(t, ds, before)
	if len(rows) != 1 || rows[0].Op != substrate.OpPut || rows[0].RecordID != event.ID {
		t.Fatalf("expected one put row for the restore, got %+v", rows)
	}
	if rows[0].Payload["restored"] != true {
		t.Fatalf("the feed should say the record was restored: %+v", rows[0].Payload)
	}
	// And the restored record is an ordinary live record again.
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{"calendarevent"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Records) != 1 || page.Records[0].ID != event.ID {
		t.Fatalf("restored event missing from the live list: %v", ids(page.Records))
	}
}

// Restoring one record restores THAT record. Anything else the owner deleted
// stays deleted until its own put arrives.
func TestResurrectDoesNotCascade(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	if err := enginetest.InstallAccountType(context.Background(), ds, substrate.ActorAPI); err != nil {
		t.Fatalf("install account type: %v", err)
	}

	acc := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: enginetest.AccountType, ID: "beeper:acct",
		Properties: map[string]any{"provider": "beeper", "label": "Personal"},
	})
	conv := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversation", ID: "beeper:c1", Properties: map[string]any{"kind": "direct"},
		Edges: []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acc.ID}}},
	})
	author := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "person", Properties: map[string]any{"name": "Alex"},
	})
	msg := mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversationmessage", ID: "beeper:m1",
		Properties: map[string]any{"text": "hi", "at": "2026-08-05T09:00:00Z"},
		Edges: []substrate.EdgeInput{
			{Rel: "conversation", To: substrate.EdgeRef{ID: conv.ID}},
			{Rel: "author", To: substrate.EdgeRef{ID: author.ID}},
		},
	})
	for _, e := range []*substrate.Record{msg, conv} {
		if _, err := ds.Delete(ctx, owner, e.Kind, e.ID); err != nil {
			t.Fatalf("delete %s: %v", e.ID, err)
		}
	}
	mustPut(t, ds, beeper, substrate.PutInput{
		Kind: "conversation", ID: conv.ID, Properties: map[string]any{"kind": "direct"},
		Edges: []substrate.EdgeInput{{Rel: "account", To: substrate.EdgeRef{Kind: enginetest.AccountType, ID: acc.ID}}},
	})
	if got := mustGet(t, ds, conv.Kind, conv.ID); got.DeletedAt != nil {
		t.Fatal("the conversation should be live again")
	}
	if got := mustGet(t, ds, msg.Kind, msg.ID); got.DeletedAt == nil {
		t.Fatal("restoring a conversation must not resurrect its messages")
	}
}

// Naming a NEW record of a projection target is refused; addressing one that
// already exists is not naming it, so the console's Save and `substrate apply`
// work on every type.
func TestClientIDIsACreateRule(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)
	installShelf(t, ds) // `book` is a mapping target: its id is server-assigned

	if _, err := ds.Put(ctx, owner, substrate.PutInput{
		Kind: "book", ID: "book-i-named", Properties: map[string]any{"title": "Piranesi"},
	}); err == nil {
		t.Fatal("a writer must not name a new book")
	} else {
		wantErr(t, err, substrate.ErrValidation, "create at a client id")
	}

	for _, tc := range []struct{ ty, prop string }{
		// `person` derives its title from a displayTemplate, so the round trip
		// goes through a declared property; `book` has none.
		{"person", "name"}, {"book", "title"},
	} {
		created := mustPut(t, ds, owner, substrate.PutInput{
			Kind: tc.ty, Properties: map[string]any{tc.prop: "x"},
		})
		// PUT …/{plural}/{id} — the console's Save, and `substrate apply`.
		saved := mustPut(t, ds, owner, substrate.PutInput{
			Kind: tc.ty, ID: created.ID, Properties: map[string]any{tc.prop: "edited"},
		})
		if saved.ID != created.ID || saved.Properties[tc.prop] != "edited" {
			t.Fatalf("%s save = %+v", tc.ty, saved.Properties)
		}
	}
}

// The substrate's own machinery is not editable through the generic graph
// verbs: rewriting a recordmerge's edges would make a split tombstone the
// wrong record and lose the real loser.
func TestLinkUnlinkRefuseSystemTypes(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	a := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "A"}})
	b := mustPut(t, ds, owner, substrate.PutInput{Kind: "person", Properties: map[string]any{"name": "B"}})
	rec, err := ds.Merge(ctx, owner, a.Kind, a.ID, b.ID)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if err := ds.Link(ctx, owner, rec.Kind, rec.ID, "loser", substrate.EdgeRef{Kind: a.Kind, ID: a.ID}, nil); err == nil {
		t.Fatal("link rewrote a merge record")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "link on a system type")
	}
	if err := ds.Unlink(ctx, owner, rec.Kind, rec.ID, "loser", substrate.EdgeRef{Kind: b.Kind, ID: b.ID}); err == nil {
		t.Fatal("unlink stripped a merge record's edge")
	} else {
		wantErr(t, err, substrate.ErrForbidden, "unlink on a system type")
	}
	// The record is intact, so the merge is still reversible.
	if _, err := ds.Split(ctx, owner, rec.ID); err != nil {
		t.Fatalf("split: %v", err)
	}
	if got := mustGet(t, ds, b.Kind, b.ID); got.DeletedAt != nil || got.ID != b.ID {
		t.Fatalf("split did not restore the loser: %+v", got)
	}
}

// A null DELETES, on the column-backed properties exactly as on the rest.
// `body` and `dueAt` are the column-backed pair a task still authors: its
// `title` is rendered from `name` (decision record 0016), and what a null
// leaves behind there is TestClearingTheHeadingKeepsTheRenderedTitle's.
func TestNullClearsEveryProperty(t *testing.T) {
	t.Parallel()
	_, ds := newDataset(t)

	task := mustPut(t, ds, owner, substrate.PutInput{
		Kind: "task",
		Properties: map[string]any{
			"name": "Ship it", "body": "the long version",
			"dueAt": "2026-08-08T00:00:00Z", "description": "notes",
		},
	})
	if task.Properties["dueAt"] == nil || task.Properties["body"] == nil {
		t.Fatalf("setup = %v", task.Properties)
	}
	cleared := mustPatch(t, ds, owner, task.Kind, task.ID, substrate.PatchInput{
		Properties: map[string]any{
			"name": nil, "body": nil, "dueAt": nil, "description": nil,
		},
	})
	for _, name := range []string{"name", "body", "dueAt", "description"} {
		if v, still := cleared.Properties[name]; still {
			t.Fatalf("%s survived a null: %v", name, v)
		}
	}
	if cleared.Version <= task.Version {
		t.Fatalf("clearing did not bump the version: %d", cleared.Version)
	}
	// And clearing again is a no-op.
	before := maxSeq(t, ds)
	mustPatch(t, ds, owner, task.Kind, task.ID, substrate.PatchInput{
		Properties: map[string]any{"name": nil, "dueAt": nil},
	})
	if rows := changesSince(t, ds, before); len(rows) != 0 {
		t.Fatalf("clearing an already-clear property wrote %d rows", len(rows))
	}
}

// review-p0 #1: accepting a change request is a TRANSITIVE write, so it is
// held to the accepting bundle actor's effective emit ceiling. A function
// whose only capability is to emit the REQUEST type cannot become a confused
// deputy — proposing a create/delete and self-accepting it — while a function
// that may emit the target type can.
func TestChangeRequestAcceptHeldToEmitCeiling(t *testing.T) {
	t.Parallel()
	const selfAccept = `
def main(input, host):
    c = input["envelope"]["change"]
    rid = "req-%s-" + c["id"]
    return {"effects": [
        {"action": "put", "kind": "core.substrate.reamde.dev/recordpatchrequest", "id": rid,
         "properties": {"op": "create", "targetKind": "tasks.substrate.reamde.dev/task",
                        "targetId": "%s-" + c["id"],
                        "diff": {"properties": {"name": "smuggled"}}}},
        {"action": "patch", "kind": "core.substrate.reamde.dev/recordpatchrequest", "id": rid,
         "properties": {"decision": "accepted"}}
    ]}
`
	ds, ops := newFnDataset(t,
		[]enginetest.Trigger{
			trigOn("deputy", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}}),
			trigOn("allowed", map[string]any{"kinds": []any{widgetType}, "ops": []any{"create"}}),
		},
		// The deputy may emit ONLY the request type: it cannot materialize a task.
		pyFn("deputy", map[string]any{}, []any{vocabulary.KindRecordPatchRequest},
			fmt.Sprintf(selfAccept, "deputy", "deputy-task")),
		// The authorized function may emit the task type too: its self-accept lands.
		pyFn("allowed", map[string]any{}, []any{vocabulary.KindRecordPatchRequest, taskType},
			fmt.Sprintf(selfAccept, "allowed", "allowed-task")),
	)
	ctx := context.Background()

	w := mustPut(t, ds, fnActor, substrate.PutInput{Kind: widgetType, Properties: map[string]any{"name": "w"}})
	process(t, ops)

	// The confused deputy is refused: no task was smuggled in, and its delivery
	// parked instead of committing.
	if _, err := ds.Get(ctx, taskType, "deputy-task-"+w.ID); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the deputy smuggled a task past its emit ceiling: %v", err)
	}
	if parked, err := ops.TriggerFailures(ctx, trigID("deputy")); err != nil || len(parked) == 0 {
		t.Fatalf("the deputy's confused-deputy delivery should have parked: parked=%d err=%v", len(parked), err)
	}

	// The authorized function's self-accept lands: it could have written the
	// task directly, so accepting its own request is within its ceiling.
	if got := mustGet(t, ds, taskType, "allowed-task-"+w.ID); got.Title != "smuggled" {
		t.Fatalf("the authorized create did not land: %+v", got)
	}
}

// review-p0 #6: deterministic accept failures that are NOT a diff CAS — a
// targetless patch and a create whose edge target does not exist — must still
// annotate the still-proposed request, so the inbox shows why.
func TestAcceptFailuresAnnotateConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	_, ds := newDataset(t)

	// A targetless patch request is valid storage (target is not required), but
	// accepting it has nothing to patch — it annotates, never fails bare.
	targetless := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"op":   "patch",
			"diff": map[string]any{"properties": map[string]any{"description": "orphaned"}},
		},
	})
	if _, err := ds.Patch(ctx, owner, targetless.Kind, targetless.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(targetless.Version),
	}); err == nil {
		t.Fatal("a targetless patch accepted green")
	} else {
		wantErr(t, err, substrate.ErrConflict, "targetless patch")
	}
	if after := mustGet(t, ds, targetless.Kind, targetless.ID); after.Properties["decision"] != "proposed" ||
		after.Annotations["substrate/conflict"] == nil {
		t.Fatalf("targetless patch should stay proposed + annotated: %+v", after.Properties)
	}

	// A create whose edge target does not exist: ErrNotFound at accept must
	// annotate too, not roll back silently.
	missingEdge := mustPut(t, ds, engram, substrate.PutInput{
		Kind: "recordpatchrequest",
		Properties: map[string]any{
			"op": "create", "targetKind": "tasks.substrate.reamde.dev/task", "targetId": "orphan-task",
			"diff": map[string]any{
				"properties": map[string]any{"name": "needs a project"},
				"edges":      []any{map[string]any{"rel": "project", "to": map[string]any{"id": "ghost-project"}}},
			},
		},
	})
	if _, err := ds.Patch(ctx, owner, missingEdge.Kind, missingEdge.ID, substrate.PatchInput{
		Properties: map[string]any{"decision": "accepted"}, IfVersion: ptr(missingEdge.Version),
	}); err == nil {
		t.Fatal("a create with a missing edge target accepted green")
	} else {
		wantErr(t, err, substrate.ErrConflict, "missing edge target")
	}
	if after := mustGet(t, ds, missingEdge.Kind, missingEdge.ID); after.Properties["decision"] != "proposed" ||
		after.Annotations["substrate/conflict"] == nil {
		t.Fatalf("missing-edge create should stay proposed + annotated: %+v", after.Properties)
	}
	if _, err := ds.Get(ctx, "tasks.substrate.reamde.dev/task", "orphan-task"); !errors.Is(err, substrate.ErrNotFound) {
		t.Fatalf("the orphan task should not exist: %v", err)
	}
}
