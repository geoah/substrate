package engine

// The vocabulary built-in: an agent declares or changes kinds the way the
// apply door does, as one batch of declaration documents admitted whole or
// not at all, and previews a batch first with plan. A declaration is the
// substrate's own record, so the batch carries every guard the door does
// (the package's source, the repository's authority for a new package, the
// refuse-breakage guards, a lossy plan's confirmation), and the agent adds
// the two of its own: each document's kind is held to the effective emit,
// and the policy door speaks before anything lands.

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
)

// dispatchVocabulary runs one vocabulary call. A lossy batch is refused with
// the plan's own reason: the confirmation it needs is the owner's, so the
// tool never carries one.
func (l *agentLoop) dispatchVocabulary(ctx context.Context, c *toolRun, args map[string]any) (string, bool) {
	for _, k := range sortedKeys(args) {
		switch k {
		case "documents", "plan":
		default:
			return toolError(fmt.Sprintf("unknown argument %q; vocabulary takes documents, plan", k)), false
		}
	}
	docs, err := vocabularyDocumentsArg(args["documents"])
	if err != nil {
		return toolError(err.Error()), false
	}
	// A preview that is not a boolean must never fall through to an apply.
	plan := false
	if v, present := args["plan"]; present {
		b, ok := v.(bool)
		if !ok {
			return toolError(fmt.Sprintf("plan must be true or false, got %v; nothing applied", v)), false
		}
		plan = b
	}
	for i, d := range docs {
		kind, _ := d["kind"].(string)
		if _, ok := vocabularyRecordKinds[kind]; !ok {
			return toolError(fmt.Sprintf("documents[%d]: kind %q is not a declaration kind; vocabulary takes %s, and a data record is the write tool's",
				i, kind, strings.Join(declarationKindRefs(), ", "))), false
		}
		if !l.emitAllows(kind) {
			return toolError(fmt.Sprintf("documents[%d]: %s is not in agent %s's effective emit allowlist, nothing applied",
				i, kind, l.ag.Identity())), false
		}
		if err := l.declarationNamed(kind); err != nil {
			return toolError(fmt.Sprintf("documents[%d]: %v", i, err)), false
		}
		if plan {
			continue
		}
		// A declaration is admission, not a record a request can hold, so a
		// gate refuses here: the owner applies a gated batch the agent planned.
		verdict, rule, err := l.ds.policyVerdict(ctx, kind, policyOpPut, l.ag.Identity())
		if err != nil {
			return toolError(err.Error()), false
		}
		switch verdict {
		case policyRefuse:
			return toolError(fmt.Sprintf("policy %s refuses put %s for agent %s, nothing applied", rule.id, kind, l.ag.Identity())), false
		case policyGate:
			return toolError(fmt.Sprintf("policy %s gates put %s for agent %s, nothing applied: hand the planned batch to the owner to apply",
				rule.id, kind, l.ag.Identity())), false
		}
	}
	if plan {
		p, err := l.ds.PlanVocabularyApply(ctx, l.actor, docs)
		if err != nil {
			return toolError(err.Error()), false
		}
		return toolJSON(map[string]any{"plan": p}), true
	}
	written, err := l.ds.applyVocabularyDocuments(ctx, l.actor, docs, substrate.VocabularyApply{}, &c.changes, true)
	if err != nil {
		return toolError(err.Error()), false
	}
	applied := make([]map[string]any, 0, len(written))
	for _, r := range written {
		applied = append(applied, map[string]any{"kind": r.Kind, "id": r.ID, "version": r.Version})
		l.in.tally.addEffect("put")
	}
	return toolJSON(map[string]any{"applied": applied}), true
}

// vocabularyDocumentsArg reads `documents`: a list of objects, or the same
// list encoded as a JSON string, which models send with long descriptions.
func vocabularyDocumentsArg(v any) ([]map[string]any, error) {
	if s, ok := v.(string); ok {
		if err := json.Unmarshal([]byte(s), &v); err != nil {
			return nil, fmt.Errorf("documents must be a list of declaration documents: %w", err)
		}
	}
	list, ok := v.([]any)
	if !ok || len(list) == 0 {
		return nil, fmt.Errorf("documents is required: a list of {kind, metadata: {id}, data} declaration documents")
	}
	docs := make([]map[string]any, 0, len(list))
	for i, item := range list {
		d, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("documents[%d] must be an object {kind, metadata: {id}, data}", i)
		}
		docs = append(docs, d)
	}
	return docs, nil
}

// declarationNamed holds a declaration kind to the agent's OWN writes, spelled
// out: a glob never reaches one (decision 0239), as it never reaches auth
// material (0080). A declaration changes what code runs and what records
// hold, so an agent whose `*` covers the owner's data does not by that grant
// also reshape the vocabulary. The effective emit is checked beside it, so a
// caller's ceiling still narrows a sub-agent that names the kind.
func (l *agentLoop) declarationNamed(kind string) error {
	if _, ok := vocabularyRecordKinds[kind]; !ok || slices.Contains(l.ag.Emit, kind) {
		return nil
	}
	return fmt.Errorf("%w: %s is a declaration kind, written only by an agent whose permissions.writes names it; a glob never reaches one, nothing applied",
		substrate.ErrForbidden, kind)
}

// declarationKindRefs lists the kinds the vocabulary tool takes, sorted, for
// the refusal that names them.
func declarationKindRefs() []string {
	out := make([]string, 0, len(vocabularyRecordKinds))
	for k := range vocabularyRecordKinds {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
