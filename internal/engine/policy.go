package engine

// The recordpatchpolicy door:
// what happens between a BUNDLE-tier actor wanting a put/patch/delete and the
// write landing, strictly inside the emit ceiling. It runs for an agent's
// writes and for the effects a function body returns, wherever the function
// runs; a function run no agent loop surrounds meets only the rules whose
// `functions` arm names it, so a rule written for agents never starts
// holding a sync. Deterministic and cheap —
// no model call sits inside a tool call: `allow` lands the write (no policy
// id is recorded on it), `refuse` bounces it like an emit refusal,
// `gate` CONVERTS it into a recordpatchrequest, entered from the side into
// the whole propose flow (thread stamped when a loop is running, the request
// id derived from the dispatch's stable idempotency identity so a retried
// delivery converts to the SAME request). When several policies match, the
// most restrictive action wins: refuse over gate over allow, the composition
// every surveyed harness trains people on. No match means today's behavior.
// The one exception is an allow that names, in `overrides`, the gate it
// answers: where both match, that gate steps aside for that write
// (decision record 0108). It is held narrow at the write door (one agent,
// one kind, one verb), and it never lifts a refuse.
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

// propHeldFunction is the request property naming the function whose
// returned effect the door held. The engine stamps it when it holds one; it
// is frozen with the envelope, and a bundle-tier actor never decides a
// request carrying it (write.go), exactly as with `policy`.
const propHeldFunction = "function"

// The three door actions, most restrictive last.
const (
	policyAllow  = "allow"
	policyGate   = "gate"
	policyRefuse = "refuse"
)

// policyRule is one recordpatchpolicy record, parsed for the door. The judge
// half parses beside it so one loader serves both.
type policyRule struct {
	id      string
	version int64
	kinds   []string
	ops     []string
	agents  []string
	// functions names function references. A rule naming none never speaks
	// for a function run outside an agent loop (matches).
	functions []string
	action    string
	judge     string
	criteria  string
	context   string
	// expandReferents hands the judge every record the diff points at, one
	// hop, beside the envelope. Orthogonal to `context`, which dials the
	// proposing THREAD: a policy may opt into both.
	expandReferents bool
	autoAccept      *float64
	autoRefuse      *float64
	mode            string
	// overrides is the id of the gate this allow answers (decision record
	// 0108): where both match one write, the gate steps aside. Empty on
	// every rule that is not a narrow allow.
	overrides string
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
			rule.kinds = stringList(sel["kinds"])
			rule.ops = stringList(sel["ops"])
			rule.agents = stringList(sel["agents"])
			rule.functions = stringList(sel["functions"])
		}
		rule.judge = referenceID(rec.Properties["judge"])
		rule.criteria, _ = rec.Properties["criteria"].(string)
		rule.context, _ = rec.Properties["context"].(string)
		rule.expandReferents, _ = rec.Properties["expandReferents"].(bool)
		rule.mode, _ = rec.Properties["mode"].(string)
		if v, ok := anyFloat(rec.Properties["autoAccept"]); ok {
			rule.autoAccept = &v
		}
		if v, ok := anyFloat(rec.Properties["autoRefuse"]); ok {
			rule.autoRefuse = &v
		}
		// The write door refuses an override on anything but a narrow allow;
		// evaluation holds the same line, so a row planted past the door
		// (the engine's own writes skip it) lifts no gate either.
		if id := referenceID(rec.Properties["overrides"]); id != "" &&
			rule.action == policyAllow && narrowSelector(rule.kinds, rule.ops, rule.agents) {
			rule.overrides = id
		}
		rules = append(rules, rule)
	}
	return rules, nil
}

// warnActionless says once, per row and per process, that a policy carries no
// action. The declaration requires `action` and validatePolicyRow refuses a
// row without one, so it can only come from a binary older than those checks,
// and while it lives the boot upgrade that declares `required` is withheld
// (schemadiff.go); loadPolicies runs per write evaluation, and warning every
// time would bury the one line that matters.
// The id is a user-authored string from a record row, so it goes through the
// same id-grammar filter every other logged id does (triggers.go logSafeID).
func (ds *dataset) warnActionless(id string) {
	if _, seen := ds.warnedPolicies.LoadOrStore(id, struct{}{}); seen {
		return
	}
	ds.svc.log.Warn("substrate: policy: no action, so the rule speaks for nothing — give it allow, gate or refuse, or delete it",
		"repository", logSafeID(ds.Repository().ID), "policy", logSafeID(id))
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
	if err := validatePolicyOverride(props, sel); err != nil {
		return err
	}
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
	return validatePolicyFunctions(reg, stringList(sel["functions"]))
}

// validatePolicyFunctions admits `selector.functions`: each entry is the full
// reference of a function this repository declares, because the door compares
// against identities and a typo would admit and then hold nothing. A host
// function is refused too: the engine runs it under the calling agent's
// grants, so its writes are that agent's and `agents` is what matches them.
func validatePolicyFunctions(reg *vocabulary.Registry, refs []string) error {
	for i, ref := range refs {
		if !vocabulary.Qualified(ref) {
			return fmt.Errorf("%w: recordpatchpolicy: selector.functions[%d]: %q is not a function reference, `<authority>/<package>/<name>`",
				substrate.ErrValidation, i, ref)
		}
		if reg == nil {
			continue
		}
		fn, err := reg.ResolveFunction(ref)
		if err != nil {
			return fmt.Errorf("%w: recordpatchpolicy: selector.functions[%d]: %w; a selector that matches no write gates nothing",
				substrate.ErrValidation, i, err)
		}
		if fn.IsHost() {
			return fmt.Errorf("%w: recordpatchpolicy: selector.functions[%d]: %s is a built-in whose writes are the calling agent's; name the agent under `agents`",
				substrate.ErrValidation, i, ref)
		}
	}
	return nil
}

// validatePolicyOverride holds `overrides` to the one shape it exists for:
// "this agent may make this write without asking me". Only an allow may name
// a gate to override, and its selector must name exactly one agent, one kind
// reference (no glob) and one verb, so an override can never grow into a
// blanket exemption. That the named policy exists and is a gate is checked
// against the stored row (txn.admitPolicyOverride), because this function
// sees only the row being written.
func validatePolicyOverride(props, sel map[string]any) error {
	if referenceID(props["overrides"]) == "" {
		return nil
	}
	if action, _ := props["action"].(string); action != policyAllow {
		return fmt.Errorf("%w: recordpatchpolicy: `overrides` is only for an allow: a %s cannot lift a gate",
			substrate.ErrValidation, action)
	}
	if !narrowSelector(stringList(sel["kinds"]), stringList(sel["ops"]), stringList(sel["agents"])) {
		return fmt.Errorf("%w: recordpatchpolicy: an allow with `overrides` must name exactly one kind reference (no glob), one op and one agent in its selector",
			substrate.ErrValidation)
	}
	return nil
}

// admitPolicyOverride refuses an allow whose `overrides` names no live gate:
// a missing policy, a deleted one, itself, or a rule that is not a gate. Such
// a row would sit in the list reading as "this agent may do this without
// asking" while the gate it meant still held every write. A gate edited into
// something else later leaves the allow standing and lifting nothing; the
// door re-reads both at every evaluation. A disabled allow lifts nothing, so
// it is admitted whatever it names: disabling a stale allow must not be
// refused because its gate is gone.
func (t *txn) admitPolicyOverride(id string, props map[string]any) error {
	target := referenceID(props["overrides"])
	if target == "" {
		return nil
	}
	if disabled, _ := props["disabled"].(bool); disabled {
		return nil
	}
	if target == id {
		return fmt.Errorf("%w: recordpatchpolicy: `overrides` names this policy itself; it must name the gate it lifts",
			substrate.ErrValidation)
	}
	row, err := t.loadRow(eref{Kind: vocabulary.KindRecordPatchPolicy, ID: target}, false)
	if err != nil {
		return err
	}
	if row == nil || row.DeletedAt != nil {
		return fmt.Errorf("%w: recordpatchpolicy: `overrides` names %s, which does not exist",
			substrate.ErrValidation, vocabulary.RecordPath(vocabulary.KindRecordPatchPolicy, target))
	}
	if action, _ := row.Props["action"].(string); action != policyGate {
		return fmt.Errorf("%w: recordpatchpolicy: `overrides` names %s, whose action is %q, and only a gate can be overridden",
			substrate.ErrValidation, vocabulary.RecordPath(vocabulary.KindRecordPatchPolicy, target), action)
	}
	return nil
}

// narrowSelector reports whether a selector speaks for exactly one agent, one
// exact kind and one verb: the only selector an override may carry.
func narrowSelector(kinds, ops, agents []string) bool {
	if len(kinds) != 1 || len(ops) != 1 || len(agents) != 1 {
		return false
	}
	return !vocabulary.IsTypeGlob(kinds[0])
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
// (`*`). Ops, agents and functions stay exact: an agent or function identity
// has no authority half to cut on, and the three ops are a closed enum where
// an empty list already says "all of them".
//
// agent is the agent whose loop wants the write, empty when none does.
// functions are the functions whose run produced it: the function whose body
// returned the effect and the root of its call chain, whose actor the write
// lands under. A rule matches when its `functions` names any of them. A write
// no agent wants meets only rules that name a function, so `{}` stays "every
// agent write" and a rule written for agents never starts holding a sync.
func (r *policyRule) matches(kind, op, agent string, functions ...string) bool {
	in := func(list []string, vs ...string) bool {
		if len(list) == 0 {
			return true
		}
		for _, item := range list {
			for _, v := range vs {
				if v != "" && item == v {
					return true
				}
			}
		}
		return false
	}
	if agent == "" && len(r.functions) == 0 {
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
	return kindMatches() && in(r.ops, op) && in(r.agents, agent) && in(r.functions, functions...)
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
func (ds *dataset) policyVerdict(ctx context.Context, kind, op, agent string, functions ...string) (string, *policyRule, error) {
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
	action, rule := verdictOf(rules, kind, op, agent, functions...)
	return action, rule, nil
}

// verdictOf is policyVerdict over rules already loaded, so a batch of effects
// reads the policy records once.
func verdictOf(rules []policyRule, kind, op, agent string, functions ...string) (string, *policyRule) {
	if kind == vocabulary.KindRecordPatchRequest {
		return policyAllow, nil
	}
	matched := make([]*policyRule, 0, len(rules))
	// lifted holds the gates a matching allow overrides for this write
	// (decision record 0108). Only a gate is lifted: a refuse still wins, and
	// a gate no matching allow names still holds the write.
	lifted := map[string]bool{}
	for i := range rules {
		rule := &rules[i]
		if !rule.matches(kind, op, agent, functions...) {
			continue
		}
		matched = append(matched, rule)
		if rule.overrides != "" {
			lifted[rule.overrides] = true
		}
	}
	var governing *policyRule
	for _, rule := range matched {
		if rule.action == policyGate && lifted[rule.id] {
			continue
		}
		if governing == nil || governs(rule, governing) {
			governing = rule
		}
	}
	if governing == nil {
		return policyAllow, nil
	}
	return governing.action, governing
}

// doorOp is the door verb for an effect action. Merge and split have none,
// because a request cannot carry them.
func doorOp(action string) string {
	switch action {
	case effectPut:
		return policyOpPut
	case effectPatch:
		return policyOpPatch
	case effectDelete:
		return policyOpDelete
	}
	return ""
}

// effectVerdict is the door's answer for one effect a function body returned,
// under the agent whose loop ran the function (empty when none did). root is
// the function the run started from; the effect's own function is ef.by. It
// adds the declaration's floor: an effect whose function, or the root of its
// call chain, declares `confirmation: always` is gated whatever the policies
// say. A merge or split under that floor is refused, since no request can
// hold it and the floor says the effect never applies by itself.
func effectVerdict(rules []policyRule, root *vocabulary.Function, ef effect, agent string) (string, *policyRule, error) {
	floor := confirmsAlways(root) || confirmsAlways(ef.by)
	op := doorOp(ef.Action)
	if op == "" {
		if floor {
			return "", nil, fmt.Errorf("%w: %s %s: the function declares `confirmation: always`, and a %s cannot be held as a recordpatchrequest",
				substrate.ErrForbidden, ef.Action, ef.Type, ef.Action)
		}
		return policyAllow, nil, nil
	}
	verdict, rule := verdictOf(rules, ef.Type, op, agent, callableIdentity(ef.by), callableIdentity(root))
	if verdict == policyAllow && floor && ef.Type != vocabulary.KindRecordPatchRequest {
		// The author's floor. No policy governs it, so none is cited.
		return policyGate, nil, nil
	}
	return verdict, rule, nil
}

func confirmsAlways(fn *vocabulary.Function) bool {
	return fn != nil && fn.Confirmation == vocabulary.FunctionConfirmAlways
}

func callableIdentity(fn *vocabulary.Function) string {
	if fn == nil {
		return ""
	}
	return fn.Identity()
}

// holdEffects runs the door over the effects of one function run that no
// agent loop surrounds: a trigger delivery, a schedule or webhook fire, one
// page of a drain, a direct call. A refused effect fails the whole run. A
// gated one stays in the list marked `hold`, and applyEffects writes its
// recordpatchrequest in its place, in list order and in the transaction that
// applies the rest, so the request commits with the run's settlement or not
// at all. The other effects still apply: the function has returned, so there
// is no caller left to re-plan the batch around the held one.
//
// key is the run's stable idempotency identity. Each held effect's request id
// derives from it and the effect's position, so a retried delivery holds the
// same effect as the same request.
func (ds *dataset) holdEffects(ctx context.Context, root *vocabulary.Function, effects []effect, key string) ([]effect, error) {
	if len(effects) == 0 {
		return effects, nil
	}
	rules, err := ds.loadPolicies(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]effect, len(effects))
	for i, ef := range effects {
		verdict, rule, err := effectVerdict(rules, root, ef, "")
		if err != nil {
			return nil, err
		}
		switch verdict {
		case policyRefuse:
			return nil, fmt.Errorf("%w: policy %s refuses %s %s for function %s, nothing applied",
				substrate.ErrForbidden, rule.id, ef.Action, ef.Type, callableIdentity(ef.by))
		case policyGate:
			ty, err := ds.resolveType(ef.Type)
			if err != nil {
				return nil, err
			}
			gw := &gatedWrite{
				op: doorOp(ef.Action), kind: ty, id: ef.ID, props: ef.Properties,
				ifVersion: ef.IfVersion, ifAbsent: ef.IfAbsent,
				key:      fmt.Sprintf("%s/effect/%d", key, i),
				function: callableIdentity(ef.by),
				once:     true,
				rule:     rule,
			}
			if rule != nil {
				gw.policyID, gw.policyVersion = rule.id, rule.version
			}
			ef.hold = gw
		}
		out[i] = ef
	}
	return out, nil
}

// judgeHeld hands each request holdEffects produced, once committed, to the
// governing policy's judge. A floor has no policy and so no judge.
func (ds *dataset) judgeHeld(effects []effect) {
	for _, ef := range effects {
		if ef.held() {
			ds.maybeJudge(ef.hold.requestID, ef.hold.rule)
		}
	}
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
	// ifAbsent is a create-only put's marker: a live target makes the held
	// effect the no-op the put would have been, and no request is written.
	ifAbsent bool
	// key is the dispatch's stable idempotency identity: the request id
	// derives from it, so a retried delivery converts to the SAME request.
	key string
	// policy is the governing rule; empty policy id means a declaration
	// floor (confirmation: always) gated, with no policy record to cite.
	policyID      string
	policyVersion int64
	// thread is the proposing thread when a loop is running; empty otherwise.
	thread string
	// function is the identity of the function whose returned effect this
	// is; empty for an agent's own write.
	function string
	// once makes the write create-if-absent on its derived id: set on a
	// function's held effect, whose request commits with the run's settlement
	// (putGatedRequest).
	once bool
	// rule is the governing policy, whose judge runs once the request
	// commits; nil for a floor.
	rule *policyRule
	// requestID is set once the request is written.
	requestID string
}

// convertToRequest materializes the held write as a recordpatchrequest in a
// transaction of its own: the whole propose flow, entered from the side.
// Returns the request id.
func (ds *dataset) convertToRequest(ctx context.Context, actor substrate.Actor, causedBy int64, sink *[]changeEntry, gw *gatedWrite) (string, error) {
	var requestID string
	err := ds.inTx(ctx, actor, false, func(t *txn) error {
		t.causedBy = causedBy
		if sink != nil {
			t.changeSink = sink
		}
		var err error
		requestID, err = t.putGatedRequest(gw)
		return err
	})
	if err != nil {
		return "", err
	}
	return requestID, nil
}

// putGatedRequest writes the held write's recordpatchrequest in this
// transaction, under the derived id. An agent's retry re-puts the identical
// envelope, which the immutable envelope guard admits as the no-op it is and
// refuses when the retry wants a different write. A function's held effect
// (`once`) is create-if-absent instead: its request commits with the run's
// settlement, so a request already stored under the key is this effect held
// by an earlier run of the same delivery (a replay, a manual run), perhaps
// decided since, and writing it again could only fail the guard. A
// create-only put whose target is live writes nothing and answers the empty
// id, since the put itself would have been a no-op.
func (t *txn) putGatedRequest(gw *gatedWrite) (string, error) {
	gw.requestID = ""
	requestID := derivedID("gate", gw.key)
	if gw.once {
		stored, err := t.loadRow(eref{Kind: vocabulary.KindRecordPatchRequest, ID: requestID}, false)
		if err != nil {
			return "", err
		}
		if stored != nil {
			gw.requestID = requestID
			return requestID, nil
		}
	}
	props := map[string]any{}
	op := gw.op
	if op == policyOpPut || op == policyOpPatch {
		ref := eref{Kind: gw.kind.Identity, ID: gw.id}
		if gw.id != "" {
			// A former id resolves onto its canonical winner, as the effect
			// itself would have (effects.go applyEffect).
			canon, err := t.canonicalOf(ref)
			if err != nil {
				return "", err
			}
			ref = canon
		}
		existing, err := t.loadRow(ref, false)
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
			props["targetId"] = ref.ID
		} else {
			if gw.ifAbsent {
				return "", nil
			}
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
		// A delete has no diff to carry its precondition in, so the held
		// write's own ifVersion goes on the request, where the accept reads
		// it (applyDeleteRequest). Dropping it here would let the accept
		// delete a record that moved while the request waited.
		if gw.ifVersion != nil {
			props[propIfVersion] = *gw.ifVersion
		}
	}
	props["op"] = op
	if gw.thread != "" {
		// The FULL path, not the bare id the loop's propose writes: a retried
		// delivery re-puts this envelope verbatim, and the immutable guard
		// compares it against the stored (normalized) value.
		props[msgRelThread] = vocabulary.RecordPath(typeThread, gw.thread)
	}
	if gw.function != "" {
		props[propHeldFunction] = vocabulary.RecordPath(kindFunction, gw.function)
	}
	if gw.policyID != "" {
		props["policy"] = vocabulary.RecordPath(vocabulary.KindRecordPatchPolicy, gw.policyID)
		props["policyRevision"] = gw.policyVersion
	}
	if _, err := t.put(substrate.PutInput{
		Kind: vocabulary.KindRecordPatchRequest, ID: requestID, Properties: props,
	}); err != nil {
		return "", err
	}
	gw.requestID = requestID
	return requestID, nil
}

// heldForReview is the message the model (and a caller) reads when the door
// gated a write: honest about what happened, carrying the one id that finds
// the request again.
func heldForReview(requestID, why string) error {
	return fmt.Errorf("%w: held for review as %s — %s",
		substrate.ErrGated, vocabulary.RecordPath(vocabulary.KindRecordPatchRequest, requestID), why)
}
