package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/graphql-go/graphql"
	"github.com/graphql-go/graphql/language/ast"
	"github.com/graphql-go/graphql/language/parser"
	"github.com/graphql-go/graphql/language/source"

	"github.com/geoah/substrate/internal/gql"
	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The graphql and mutate built-ins: the SAME schema and resolvers the API's
// /graphql endpoint executes (internal/gql), run in-process against the
// loop's own dataset. The v4 split holds: the chat-grade `graphql` tool is
// read-only at the AST (a mutation in its document is a tool error the model
// sees, pointing at propose), and `mutate` is the separately granted write
// surface, every written kind held to the loop's EFFECTIVE emit by a dataset
// wrapper the resolvers cannot see around.

// agentGQLMaxBytes caps one tool result. The number is v4's: past 64KB a
// result stops informing the model and starts evicting its context, so the
// tool refuses with the narrowing hint instead.
const agentGQLMaxBytes = 64 << 10

// The card shape of the graphql and mutate built-ins — v4's two-field contract,
// a document plus its variables — is no longer a literal here: it is the
// `arguments:` of the `core.substrate.reamde.dev/graphql` and `…/mutate`
// declarations, compiled by the loader like any other function's.

// dispatchGraphQL runs the read-only built-in.
func (l *agentLoop) dispatchGraphQL(ctx context.Context, args map[string]any) (string, bool) {
	return l.execGraphQL(ctx, args, false)
}

// dispatchMutate runs the write built-in: the same executor, mutations
// admitted, writes emit-gated.
func (l *agentLoop) dispatchMutate(ctx context.Context, args map[string]any) (string, bool) {
	return l.execGraphQL(ctx, args, true)
}

func (l *agentLoop) execGraphQL(ctx context.Context, args map[string]any, mutate bool) (string, bool) {
	// The mutate wrapper is what holds every write to the effective emit set; the
	// read-only tool passes the dataset bare, since its document is refused if it
	// names a mutation at all.
	var target substrate.Dataset = l.ds
	if mutate {
		target = &agentMutateDataset{Dataset: l.ds, loop: l}
	}
	return l.ds.runGraphQLTool(ctx, l.actor, target, args, mutate)
}

// runGraphQLTool executes one GraphQL document against a repository and shapes
// the answer as a tool result. `target` is the Dataset the resolvers see — the
// bare dataset for a read, the emit-gating wrapper for an agent's mutate — and
// `actor` is the hand a write would be attributed to.
//
// It is a dataset method rather than a loop one because
// `core.substrate.reamde.dev/graphql` is callable directly too, where the caller
// is a token that owns the repository and there is no loop.
func (ds *dataset) runGraphQLTool(ctx context.Context, actor substrate.Actor, target substrate.Dataset, args map[string]any, mutate bool) (string, bool) {
	query, _ := args["query"].(string)
	if strings.TrimSpace(query) == "" {
		return toolError("query is required"), false
	}
	variables, _ := args["variables"].(map[string]any)
	if err := checkGraphQLOperations(query, mutate); err != nil {
		return toolError(err.Error()), false
	}
	types, err := ds.Kinds(ctx)
	if err != nil {
		return toolError(err.Error()), false
	}
	schema, err := ds.svc.gqlSchemas.SchemaFor(ds.Repository().Name, types)
	if err != nil {
		return toolError(err.Error()), false
	}
	res := graphql.Do(graphql.Params{
		Schema:         *schema,
		RequestString:  query,
		VariableValues: variables,
		Context:        gql.WithRequest(ctx, target, actor),
	})
	out, err := json.Marshal(res)
	if err != nil {
		return toolError("marshal result: " + err.Error()), false
	}
	if len(out) > agentGQLMaxBytes {
		return toolError(fmt.Sprintf(
			"response is %d bytes (cap %d): narrow the query with fewer fields, a smaller first, or one record instead of a list",
			len(out), agentGQLMaxBytes)), false
	}
	// Resolver errors are a RESULT the model steers around, but they are
	// still a failed call: ok is stored on the message row and drives the
	// transcript's red chip, exactly like a refused function tool.
	return string(out), !res.HasErrors()
}

// checkGraphQLOperations parses the document and holds it to the tool's
// grant BEFORE execution: fragments pass, anonymous operations count as
// queries, and the refusals tell the model where the verb it wanted lives.
func checkGraphQLOperations(query string, allowMutation bool) error {
	doc, err := parser.Parse(parser.ParseParams{
		Source: source.NewSource(&source.Source{Body: []byte(query)}),
	})
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	for _, def := range doc.Definitions {
		op, ok := def.(*ast.OperationDefinition)
		if !ok {
			continue
		}
		switch op.Operation {
		case "", "query":
		case "mutation":
			if !allowMutation {
				return errors.New("mutations are not allowed via graphql: propose the change instead, or use the mutate tool where the agent declares it")
			}
		case "subscription":
			return errors.New("subscriptions are not supported: poll with a query, or read changelog(from, first)")
		default:
			return fmt.Errorf("operation %q is not supported", op.Operation)
		}
	}
	return nil
}

// agentMutateDataset is the mutate built-in's write gate: the resolvers see
// an ordinary substrate.Dataset, and every mutation lands here first, where
// the written kind is held to the loop's EFFECTIVE emit (the agent's own,
// narrowed by any sub-agent ceiling) before the dataset applies it through the
// full public write path (schema-record admission, kind guards, conflict
// annotations, all of it). Merge and split refuse outright: fusing or
// splitting identities is the owner's decision, with its own reviewed flow
// (recordmergerequest), and no emit grant makes it an agent's.
type agentMutateDataset struct {
	substrate.Dataset
	loop *agentLoop
}

// ceiling is the emit set every write from this tool carries into its
// transaction — the same stamp dispatchFunction puts on a function tool's
// effects. It is what makes ACCEPTING a change request through `mutate` legal:
// authorizeRequestOp bounds the transitive write by this set and fails closed
// without it, so an unstamped accept would refuse while rejecting the same
// request succeeded.
func (m *agentMutateDataset) ceiling() *effectCeiling {
	return &effectCeiling{emit: m.loop.emit, changes: &m.loop.dispatchChanges}
}

// allow resolves the written kind and holds it to the effective emit set —
// and keeps every bundle hand off the policy kind, because a policy an agent
// could edit is a gate that agent could open.
func (m *agentMutateDataset) allow(kindRef, verb string) (*vocabulary.Kind, error) {
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
func (m *agentMutateDataset) door(ctx context.Context, ty *vocabulary.Kind, op, id string, props map[string]any, ifVersion *int64) error {
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

func (m *agentMutateDataset) tally(action string) {
	m.loop.in.tally.effects[action]++
}

func (m *agentMutateDataset) Put(ctx context.Context, actor substrate.Actor, in substrate.PutInput) (*substrate.Record, error) {
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

func (m *agentMutateDataset) Patch(ctx context.Context, actor substrate.Actor, typ, id string, in substrate.PatchInput) (*substrate.Record, error) {
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

func (m *agentMutateDataset) Delete(ctx context.Context, actor substrate.Actor, typ, id string) (*substrate.Record, error) {
	ty, err := m.allow(typ, "delete")
	if err != nil {
		return nil, err
	}
	if err := m.door(ctx, ty, policyOpDelete, id, nil, nil); err != nil {
		return nil, err
	}
	e, err := m.loop.ds.deleteBounded(ctx, actor, typ, id, m.ceiling())
	if err == nil {
		m.tally("delete")
	}
	return e, err
}

// Link and Unlink gate on the SOURCE kind: an edge is part of its source
// record, so writing one is writing that record. They carry no effect ceiling
// because an edge write drives no state machine: only a patch can enter the
// accepted state whose transition materializes a change request.
func (m *agentMutateDataset) Link(ctx context.Context, actor substrate.Actor, srcType, src, rel string, to substrate.EdgeRef, props map[string]any) error {
	if _, err := m.allow(srcType, "link"); err != nil {
		return err
	}
	if err := m.loop.ds.linkBounded(ctx, actor, srcType, src, rel, to, props, m.ceiling()); err != nil {
		return err
	}
	m.tally("link")
	return nil
}

func (m *agentMutateDataset) Unlink(ctx context.Context, actor substrate.Actor, srcType, src, rel string, to substrate.EdgeRef) error {
	if _, err := m.allow(srcType, "unlink"); err != nil {
		return err
	}
	if err := m.loop.ds.unlinkBounded(ctx, actor, srcType, src, rel, to, m.ceiling()); err != nil {
		return err
	}
	m.tally("unlink")
	return nil
}

func (m *agentMutateDataset) Merge(context.Context, substrate.Actor, string, string, string) (*substrate.Record, error) {
	return nil, fmt.Errorf("%w: merge is the owner's decision: its reviewed flow is a recordmergerequest, not an agent mutation", substrate.ErrForbidden)
}

func (m *agentMutateDataset) Split(context.Context, substrate.Actor, string) (*substrate.Record, error) {
	return nil, fmt.Errorf("%w: split is the owner's decision: it reverses a reviewed merge, not an agent mutation", substrate.ErrForbidden)
}
