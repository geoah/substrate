package engine

// The recordpatchpolicy door (docs/plans/thread-interactions.md phase 3):
// what happens between a BUNDLE-tier actor wanting a put/patch/delete and the
// write landing, strictly inside the emit ceiling. Deterministic and cheap —
// no model call sits inside a tool call: `allow` lands the write (the policy
// id rides the changelog payload), `refuse` bounces it like an emit refusal,
// `gate` CONVERTS it into a recordpatchrequest, entered from the side into
// the whole propose flow (thread stamped when a loop is running, the request
// id derived from the dispatch's stable idempotency identity so a retried
// delivery converts to the SAME request). When several policies match, the
// most restrictive action wins: refuse over gate over allow, the composition
// every surveyed harness trains people on. No match means today's behavior.
//
// Policy never runs for owner or machine writes, never gates the request
// kind itself (it IS the gate), and bundle-tier actors cannot write the
// policy kind at all: a policy an agent could edit is a gate that agent
// could open.

import (
	"context"
	"fmt"
	"strings"

	"github.com/geoah/substrate/internal/substrate"
	"github.com/geoah/substrate/internal/vocabulary"
)

// The door's op vocabulary, matching the selector's declared values.
const (
	policyOpPut    = "put"
	policyOpPatch  = "patch"
	policyOpDelete = "delete"
)

// The three door actions, most restrictive last.
const (
	policyAllow  = "allow"
	policyGate   = "gate"
	policyRefuse = "refuse"
)

// policyRule is one recordpatchpolicy record, parsed for the door. The judge
// half (phase 4) parses beside it so one loader serves both.
type policyRule struct {
	id         string
	version    int64
	kinds      []string
	ops        []string
	agents     []string
	action     string
	judge      string
	criteria   string
	context    string
	autoAccept *float64
	autoRefuse *float64
	mode       string
}

// loadPolicies reads the live policy records. Owner-authored and few, so the
// read is per evaluation; a cache is an optimization nothing has needed yet.
func (ds *dataset) loadPolicies(ctx context.Context) ([]policyRule, error) {
	page, err := ds.List(ctx, substrate.Query{
		Filter: substrate.Filter{Kinds: []string{vocabulary.KindRecordPatchPolicy}},
		First:  200,
	})
	if err != nil {
		return nil, err
	}
	reg := ds.registry()
	rules := make([]policyRule, 0, len(page.Records))
	for _, rec := range page.Records {
		if disabled, _ := rec.Properties["disabled"].(bool); disabled {
			continue
		}
		rule := policyRule{
			id:      rec.ID,
			version: rec.Version,
		}
		rule.action, _ = rec.Properties["action"].(string)
		if rule.action == "" {
			ds.warnActionless(rec.ID)
			continue
		}
		if sel, ok := rec.Properties["selector"].(map[string]any); ok {
			rule.kinds = resolveSelectorKinds(reg, stringList(sel["kinds"]))
			rule.ops = stringList(sel["ops"])
			rule.agents = stringList(sel["agents"])
		}
		rule.judge = referenceID(rec.Properties["judge"])
		rule.criteria, _ = rec.Properties["criteria"].(string)
		rule.context, _ = rec.Properties["context"].(string)
		rule.mode, _ = rec.Properties["mode"].(string)
		if v, ok := anyFloat(rec.Properties["autoAccept"]); ok {
			rule.autoAccept = &v
		}
		if v, ok := anyFloat(rec.Properties["autoRefuse"]); ok {
			rule.autoRefuse = &v
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// warnActionless says once, per row and per process, that a policy carries no
// action. validatePolicyRow refuses such a row at the write door, so it can
// only come from a binary older than that check; loadPolicies runs per write
// evaluation, and warning every time would bury the one line that matters.
// The id is a user-authored string from a record row, so it goes through the
// same id-grammar filter every other logged id does (triggers.go logSafeID).
func (ds *dataset) warnActionless(id string) {
	if _, seen := ds.warnedPolicies.LoadOrStore(id, struct{}{}); seen {
		return
	}
	ds.svc.log.Warn("substrate: policy: no action, so the rule speaks for nothing — give it allow, gate or refuse, or delete it",
		"repository", logSafeID(ds.Repository().Name), "policy", logSafeID(id))
}

// resolveSelectorKinds canonicalizes a selector's exact patterns against the
// repository's own vocabulary, the same resolve-at-the-gate a trigger source
// gets (triggers.go resolveKinds). A kind has two spellings, `task` and
// `samples.substrate.reamde.dev/tasks/task`, and the door compares against the
// IDENTITY, so a selector written in the bare spelling would admit and then
// never match. A glob is not a reference and is left alone; so is a name the
// registry does not know, which matches nothing, which is what an undeclared
// kind should do.
func resolveSelectorKinds(reg *vocabulary.Registry, pats []string) []string {
	if reg == nil {
		return pats
	}
	for i, pat := range pats {
		if pat == "*" || strings.HasSuffix(pat, "/*") {
			continue
		}
		if ty, err := reg.Resolve(pat); err == nil && ty != nil {
			pats[i] = ty.Identity
		}
	}
	return pats
}

// validatePolicyRow admits a recordpatchpolicy at the write door, the way a
// trigger row is admitted (write.go): a rule the door cannot act on must not
// land looking live. A trigger that never fires is a liveness bug; a policy
// that never matches is an open door, so every spelling that can never match
// is a refusal here rather than a silence at evaluation.
//
// `action` is required: there is no default action, and a rule without one
// speaks for nothing at all.
//
// `selector.kinds` must hold the trigger source's spellings, so `tasks.*`,
// which is neither a reference nor `<authority>/*`, is refused. An exact
// pattern must also NAME A KIND THIS REPOSITORY KNOWS, whichever spelling it
// uses: the door compares against kind identities, so `widgets` (a plural
// typo for `widget`) or a reference to an uninstalled kind admits and then
// gates nothing at all. A glob is not checked against the vocabulary, because
// an authority's kind set changing under it is the whole point of writing one.
func validatePolicyRow(reg *vocabulary.Registry, props map[string]any) error {
	if action, _ := props["action"].(string); action == "" {
		return fmt.Errorf("%w: recordpatchpolicy: `action` is required — allow, gate or refuse",
			substrate.ErrValidation)
	}
	sel, _ := props["selector"].(map[string]any)
	if sel == nil {
		return nil
	}
	for i, pat := range stringList(sel["kinds"]) {
		if !vocabulary.ValidTypeGlob(pat) {
			return fmt.Errorf("%w: recordpatchpolicy: selector.kinds[%d]: %q is not a kind reference, `<authority>/*` or `*`",
				substrate.ErrValidation, i, pat)
		}
		if pat == "*" || strings.HasSuffix(pat, "/*") || reg == nil {
			continue
		}
		if _, err := reg.Resolve(pat); err != nil {
			return fmt.Errorf("%w: recordpatchpolicy: selector.kinds[%d]: %w — a selector that matches no write gates nothing",
				substrate.ErrValidation, i, err)
		}
	}
	return nil
}

func stringList(v any) []string {
	items, _ := v.([]any)
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out
}

// matches reports whether the rule speaks for this write: every named
// dimension must admit it, and an empty dimension admits everything.
//
// The kinds dimension is the trigger source's grammar, matched by the trigger
// source's matcher (vocabulary.MatchTypeGlob): a kind reference, every kind
// one authority publishes (`samples.substrate.reamde.dev/tasks/*`), or every kind
// (`*`). Ops and agents stay exact — an agent identity has no authority half
// to cut on, and the three ops are a closed enum where an empty list already
// says "all of them".
func (r *policyRule) matches(kind, op, agent string) bool {
	in := func(list []string, v string) bool {
		if len(list) == 0 {
			return true
		}
		for _, item := range list {
			if item == v {
				return true
			}
		}
		return false
	}
	kindMatches := func() bool {
		if len(r.kinds) == 0 {
			return true
		}
		for _, pat := range r.kinds {
			if vocabulary.MatchTypeGlob(pat, kind) {
				return true
			}
		}
		return false
	}
	return kindMatches() && in(r.ops, op) && in(r.agents, agent)
}

// severity orders the actions: the most restrictive matching policy governs.
func severity(action string) int {
	switch action {
	case policyRefuse:
		return 2
	case policyGate:
		return 1
	default:
		return 0
	}
}

// canAutoAccept reports whether this rule could land a gated write without the
// owner ever seeing it: a judge to run, `enforce` mode, and an accept floor for
// the judge to clear. A rule missing any of the three always reaches the owner.
func (r *policyRule) canAutoAccept() bool {
	return r.judge != "" && r.mode == "enforce" && r.autoAccept != nil
}

// governs reports whether a outranks b as the rule that speaks for one write.
//
// Action severity first: refuse over gate over allow. Then, among equal
// actions, THE RULE THAT CANNOT LAND THE WRITE ON A MODEL'S WORD, because the
// governing rule carries the judge (maybeJudge) and an id tie-break alone
// would let the laxer of two equally restrictive rules decide by alphabet. It
// is also what keeps this PR's wildcards safe on stored data: a selector that
// matched nothing before (a bare kind name the door never resolved, or a `*`
// nothing validated) can now start matching, and without this arm a dormant
// judged rule waking up could move a write from "the owner decides" to "a
// model may decide" from a binary upgrade alone. The lowest id breaks what is
// left, so the choice is stable.
func governs(a, b *policyRule) bool {
	if sa, sb := severity(a.action), severity(b.action); sa != sb {
		return sa > sb
	}
	if aa, ba := a.canAutoAccept(), b.canAutoAccept(); aa != ba {
		return !aa
	}
	return a.id < b.id
}

// policyVerdict evaluates the door for one bundle-tier write. The nil rule
// with policyAllow is "no match": today's behavior, nothing to audit.
func (ds *dataset) policyVerdict(ctx context.Context, kind, op, agent string) (string, *policyRule, error) {
	// The request kind is never gated or refused by policy: it IS the gate,
	// and a policy folding it in would recurse a propose into a
	// request-to-create-a-request.
	if kind == vocabulary.KindRecordPatchRequest {
		return policyAllow, nil, nil
	}
	rules, err := ds.loadPolicies(ctx)
	if err != nil {
		return "", nil, err
	}
	var governing *policyRule
	for i := range rules {
		rule := &rules[i]
		if !rule.matches(kind, op, agent) {
			continue
		}
		if governing == nil || governs(rule, governing) {
			governing = rule
		}
	}
	if governing == nil {
		return policyAllow, nil, nil
	}
	return governing.action, governing, nil
}

// gatedWrite is one write the door held: what the conversion needs to build
// the request.
type gatedWrite struct {
	// op is the door op; a put converts to create or patch by whether the
	// target exists at conversion.
	op   string
	kind *vocabulary.Kind
	id   string
	// props is the write's coerced property map — the diff's cargo.
	props map[string]any
	// ifVersion is the write's own CAS when it carried one; absent, the
	// target's version at conversion anchors the diff (stampTargetVersion).
	ifVersion *int64
	// key is the dispatch's stable idempotency identity: the request id
	// derives from it, so a retried delivery converts to the SAME request.
	key string
	// policy is the governing rule; empty policy id means a declaration
	// floor (confirmation: always) gated, with no policy record to cite.
	policyID      string
	policyVersion int64
	// thread is the proposing thread when a loop is running; empty otherwise.
	thread string
}

// convertToRequest materializes the held write as a recordpatchrequest —
// the whole propose flow, entered from the side. Create-if-absent on the
// derived id: a retry re-puts the identical envelope, which the immutable
// envelope guard admits as the no-op it is. Returns the request id.
func (ds *dataset) convertToRequest(ctx context.Context, actor substrate.Actor, causedBy int64, sink *[]changeEntry, gw *gatedWrite) (string, error) {
	requestID := derivedID("gate", gw.key)
	props := map[string]any{}
	op := gw.op
	if op == policyOpPut || op == policyOpPatch {
		existing, err := ds.loadRowDB(ctx, eref{Kind: gw.kind.Identity, ID: gw.id})
		if err != nil {
			return "", err
		}
		if existing == nil || existing.DeletedAt != nil {
			if gw.id == "" {
				return "", fmt.Errorf("%w: a gated create needs the write's own id — server-assigned ids cannot be promised by a request",
					substrate.ErrValidation)
			}
			op = opCreate
			props["targetKind"] = gw.kind.Identity
			props["targetId"] = gw.id
		} else {
			op = opPatch
			props[propTarget] = vocabulary.RecordPath(existing.Kind, existing.ID)
		}
		norm, err := normalizeDiff(gw.kind, map[string]any{"properties": gw.props}, op)
		if err != nil {
			return "", err
		}
		if gw.ifVersion != nil {
			norm["ifVersion"] = *gw.ifVersion
		}
		props["diff"] = norm
	} else {
		op = opDelete
		props[propTarget] = vocabulary.RecordPath(gw.kind.Identity, gw.id)
	}
	props["op"] = op
	if gw.thread != "" {
		// The FULL path, not the bare id the loop's propose writes: a retried
		// delivery re-puts this envelope verbatim, and the immutable guard
		// compares it against the stored (normalized) value.
		props[msgRelThread] = vocabulary.RecordPath(typeThread, gw.thread)
	}
	if gw.policyID != "" {
		props["policy"] = vocabulary.RecordPath(vocabulary.KindRecordPatchPolicy, gw.policyID)
		props["policyRevision"] = gw.policyVersion
	}
	err := ds.inTx(ctx, actor, false, func(t *txn) error {
		t.causedBy = causedBy
		if sink != nil {
			t.changeSink = sink
		}
		_, err := t.put(substrate.PutInput{
			Kind: vocabulary.KindRecordPatchRequest, ID: requestID, Properties: props,
		})
		return err
	})
	if err != nil {
		return "", err
	}
	return requestID, nil
}

// heldForReview is the message the model (and a caller) reads when the door
// gated a write: honest about what happened, carrying the one id that finds
// the request again.
func heldForReview(requestID, why string) error {
	return fmt.Errorf("%w: held for review as %s — %s",
		substrate.ErrGated, vocabulary.RecordPath(vocabulary.KindRecordPatchRequest, requestID), why)
}
