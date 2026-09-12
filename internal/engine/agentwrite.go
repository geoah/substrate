package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/strictjson"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The write built-in: put, patch and delete, the three ordinary record writes,
// as one agent tool. It is the separately granted write surface beside the
// read-only `query`: every written kind is held to the loop's EFFECTIVE emit
// by a dataset wrapper the tool cannot see around, and the policy layer
// (allow, refuse, gate) runs for each write exactly as it does for a function
// tool's effects.

// The op vocabulary the tool takes. They are the engine's own three record
// writes; merge and split are the owner's reviewed decisions and have no op.
const (
	writeOpPut    = "put"
	writeOpPatch  = "patch"
	writeOpDelete = "delete"
)

// dispatchWrite runs the write built-in: the loop's own dataset behind the
// emit-gating wrapper, the loop's actor on every write.
func (l *agentLoop) dispatchWrite(ctx context.Context, args map[string]any) (string, bool) {
	return runWriteTool(ctx, l.actor, &agentWriteDataset{Dataset: l.ds, loop: l}, args)
}

// runWriteTool executes one write call against target and shapes the answer
// as a tool result: the record as written, or the refusal the model steers
// around. `input` is decoded through the SAME strict decoder the REST body
// takes, so a misspelled `ifversion` is refused by name rather than dropped.
func runWriteTool(ctx context.Context, actor substrate.Actor, target substrate.Dataset, args map[string]any) (string, bool) {
	// The argument set is closed, and a key outside it is refused BY NAME: a
	// misspelled `ifversion` would otherwise leave the guard nil and run the
	// write unconditionally, which is the one thing a precondition must never
	// silently become.
	for _, k := range sortedKeys(args) {
		switch k {
		case "op", "kind", "id", "input", "ifVersion":
		default:
			return toolError(fmt.Sprintf("unknown argument %q; write takes op, kind, id, input, ifVersion", k)), false
		}
	}
	op, _ := args["op"].(string)
	kind, _ := args["kind"].(string)
	id, _ := args["id"].(string)
	if kind == "" {
		return toolError("kind is required — records are addressed by (kind, id)"), false
	}
	ifVersion, err := versionArg(args["ifVersion"])
	if err != nil {
		return toolError(err.Error()), false
	}
	raw, _ := json.Marshal(args["input"])
	if args["input"] == nil {
		raw = []byte("{}")
	}
	var e *substrate.Record
	switch op {
	case writeOpPut:
		var in substrate.PutInput
		if derr := strictjson.DecodeBytes(raw, &in, false); derr != nil {
			return toolError(fmt.Sprintf("input: %v. input takes %s", derr, inputKeys(substrate.PutInput{}))), false
		}
		in.Kind, in.ID = kind, id
		if ifVersion != nil {
			in.IfVersion = ifVersion
		}
		e, err = target.Put(ctx, actor, in)
	case writeOpPatch:
		if id == "" {
			return toolError("patch needs an id — it changes one existing record"), false
		}
		var in substrate.PatchInput
		if derr := strictjson.DecodeBytes(raw, &in, false); derr != nil {
			return toolError(fmt.Sprintf("input: %v. input takes %s", derr, inputKeys(substrate.PatchInput{}))), false
		}
		if ifVersion != nil {
			in.IfVersion = ifVersion
		}
		e, err = target.Patch(ctx, actor, kind, id, in)
	case writeOpDelete:
		if id == "" {
			return toolError("delete needs an id — it tombstones one existing record"), false
		}
		e, err = target.Delete(ctx, actor, kind, id, substrate.DeleteInput{IfVersion: ifVersion})
	default:
		return toolError(fmt.Sprintf("op must be %s, %s or %s", writeOpPut, writeOpPatch, writeOpDelete)), false
	}
	if err != nil {
		return toolError(err.Error()), false
	}
	return toolJSON(map[string]any{"record": e}), true
}

// versionArg reads an optional version precondition: nil when absent, the
// exact integer when whole, and an error for anything else. A version that
// cannot be read is refused rather than dropped or rounded, because either
// would turn a guarded write into an unguarded one.
func versionArg(v any) (*int64, error) {
	if v == nil {
		return nil, nil
	}
	f, ok := anyFloat(v)
	if !ok || f != float64(int64(f)) {
		return nil, fmt.Errorf("ifVersion must be a whole number, got %v", v)
	}
	n := int64(f)
	return &n, nil
}

// inputKeys names the keys one write input takes, for the refusal: the
// decoder's own set minus the two the arguments carry beside it.
func inputKeys(v any) string {
	var out []string
	for _, k := range strictjson.Keys(v) {
		if k != "kind" && k != "id" {
			out = append(out, k)
		}
	}
	return strings.Join(out, ", ")
}

// agentWriteDataset is the write built-in's gate: the tool sees an ordinary
// substrate.Dataset, and every write lands here first, where the written kind
// is held to the loop's EFFECTIVE emit (the agent's own,
// narrowed by any sub-agent ceiling) before the dataset applies it through the
// full public write path (schema-record admission, kind guards, conflict
// annotations, all of it). Merge and split refuse outright: fusing or
// splitting identities is the owner's decision, with its own reviewed flow
// (recordmergerequest), and no emit grant makes it an agent's; the tool
// never offers them, and the dataset refuses them too so nothing reaches
// them around it.
type agentWriteDataset struct {
	substrate.Dataset
	loop *agentLoop
}

// ceiling is the emit set every write from this tool carries into its
// transaction — the same stamp dispatchFunction puts on a function tool's
// effects. It is what makes ACCEPTING a change request through `write` legal:
// authorizeRequestOp bounds the transitive write by this set and fails closed
// without it, so an unstamped accept would refuse while rejecting the same
// request succeeded.
func (m *agentWriteDataset) ceiling() *effectCeiling {
	return &effectCeiling{emit: m.loop.emit, changes: &m.loop.dispatchChanges}
}

// allow resolves the written kind and holds it to the effective emit set —
// and keeps every bundle hand off the policy kind, because a policy an agent
// could edit is a gate that agent could open.
func (m *agentWriteDataset) allow(kindRef, verb string) (*vocabulary.Kind, error) {
	ty, err := m.loop.ds.resolveType(kindRef)
	if err != nil {
		return nil, err
	}
	if ty.Identity == vocabulary.KindRecordPatchPolicy {
		return nil, fmt.Errorf("%w: %s is the owner's hand alone — installed code never writes the door's own rules",
			substrate.ErrForbidden, ty.Identity)
	}
	if !m.loop.emitAllows(ty.Identity) {
		return nil, fmt.Errorf("%w: %s %s: %s is not in agent %s's effective emit allowlist, nothing applied",
			substrate.ErrForbidden, verb, kindRef, ty.Identity, m.loop.ag.Identity())
	}
	return ty, nil
}

// door runs the policy layer for one write this dataset is about to apply:
// allow proceeds, refuse errors, gate converts the write into a request and
// answers ErrGated naming it. Deterministic — no model call sits here.
func (m *agentWriteDataset) door(ctx context.Context, ty *vocabulary.Kind, op, id string, props map[string]any, ifVersion *int64) error {
	l := m.loop
	verdict, rule, err := l.ds.policyVerdict(ctx, ty.Identity, op, l.ag.Identity())
	if err != nil {
		return err
	}
	switch verdict {
	case policyRefuse:
		return fmt.Errorf("%w: policy %s refuses %s %s for agent %s",
			substrate.ErrForbidden, rule.id, op, ty.Identity, l.ag.Identity())
	case policyGate:
		l.gateOrdinal++
		gw := &gatedWrite{
			op: op, kind: ty, id: id, props: props, ifVersion: ifVersion,
			key:      fmt.Sprintf("%s/agent/%s/%d/gate/%d", l.in.delivery, l.ag.Identity(), l.toolCalls, l.gateOrdinal),
			policyID: rule.id, policyVersion: rule.version,
			thread: l.threadID,
		}
		requestID, err := l.ds.convertToRequest(ctx, l.actor, l.in.causedBy, &l.dispatchChanges, gw)
		if err != nil {
			return err
		}
		l.in.tally.effects["gate"]++
		l.ds.maybeJudge(requestID, rule)
		return heldForReview(requestID, fmt.Sprintf("policy %s gates %s %s for agent %s",
			rule.id, op, ty.Identity, l.ag.Identity()))
	default:
		return nil
	}
}

func (m *agentWriteDataset) tally(action string) {
	m.loop.in.tally.effects[action]++
}

func (m *agentWriteDataset) Put(ctx context.Context, actor substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
	ty, err := m.allow(in.Kind, "put")
	if err != nil {
		return nil, err
	}
	if err := m.door(ctx, ty, policyOpPut, in.ID, in.Properties, in.IfVersion); err != nil {
		return nil, err
	}
	e, err := m.loop.ds.putBounded(ctx, actor, in, m.ceiling())
	if err == nil {
		m.tally("put")
	}
	return e, err
}

func (m *agentWriteDataset) Patch(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error) {
	ty, err := m.allow(typ, "patch")
	if err != nil {
		return nil, err
	}
	if err := m.door(ctx, ty, policyOpPatch, id, in.Properties, in.IfVersion); err != nil {
		return nil, err
	}
	e, err := m.loop.ds.patchBounded(ctx, actor, typ, id, in, m.ceiling())
	if err == nil {
		m.tally("patch")
	}
	return e, err
}

func (m *agentWriteDataset) Delete(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.DeleteInput) (*substrate.Record, error) {
	ty, err := m.allow(typ, "delete")
	if err != nil {
		return nil, err
	}
	if err := m.door(ctx, ty, policyOpDelete, id, nil, in.IfVersion); err != nil {
		return nil, err
	}
	e, err := m.loop.ds.deleteBounded(ctx, actor, typ, id, in, m.ceiling())
	if err == nil {
		m.tally("delete")
	}
	return e, err
}

func (m *agentWriteDataset) Merge(context.Context, substrate.Actor, substrate.MergeInput) (*substrate.Record, error) {
	return nil, fmt.Errorf("%w: merge is the owner's decision: its reviewed flow is a recordmergerequest, not an agent mutation", substrate.ErrForbidden)
}

func (m *agentWriteDataset) Split(context.Context, substrate.Actor, substrate.SplitInput) (*substrate.Record, error) {
	return nil, fmt.Errorf("%w: split is the owner's decision: it reverses a reviewed merge, not an agent mutation", substrate.ErrForbidden)
}
