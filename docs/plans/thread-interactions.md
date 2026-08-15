# Plan: thread interactions: asks, gates, and policy

Status: design hardened through two adversarial reviews (an independent Codex
pass on the same brief, then a Codex review and a code-grounded subagent
review of the draft; their surviving findings are integrated and the
discarded ones recorded). This is a plan, not a contract: the code that lands
is the contract, and every shape here is a sketch the implementer holds to
the house rules (one short description sentence, versions bumped, live
words).

## What this is

The decision loop that just landed (PR #72) taught threads one interaction:
an agent proposes a mutation, somebody decides it, the decision reports back
into the proposing thread as a `system` message, and the thread resumes.
This plan generalizes that into the interaction model the substrate needs
for real agent workflows, in four pieces:

1. **Asks** (soft): an agent asks the user something mid-thread. Yes/no,
   pick-one, pick-many, and batches of independent questions asked together.
   The answer feeds back into the thread and the agent reacts to it.
2. **Gates** (hard): a mutation waits for a decision before it lands. This
   is the existing `recordpatchrequest` flow, kept as the one first-class
   gate and finished rather than reinvented.
3. **Policy**: what happens between an agent wanting a write and the write
   landing. Static rules first (allow, gate, refuse), an optional LLM judge
   with confidence thresholds behind them, every verdict audited.
4. **Function escalation** (sketch only): a function body saying "I need a
   human before I do this". Designed here, ticketed, not built yet.

It also takes a position on migrating the loop to Google's ADK for Go.

The end goal: a thread is a complete, self-describing account of a run that
ANY reader (the console, a GraphQL consumer, another harness) can render and
drive, including its open questions and pending gates, without bespoke
endpoints or payload sniffing.

## Principles

These fall out of the substrate's own rules and PR #72's experience:

- **Interactions are records.** A question to the user is a record with a
  state machine, exactly as a proposed mutation is. No side-channel queues,
  no in-memory pending-prompt table: the changelog is the truth, an inbox is
  a query, and a crashed server forgets nothing that was decided.
- **The thread never blocks.** The loop settles when it has asked; the
  answer RESUMES it. A run waiting on a human costs nothing while it waits.
  This is the shape everything durable converges on (LangGraph interrupts,
  A2A `input-required`, OpenAI Agents SDK interruptions, Temporal signals):
  persist the pending decision, end the turn, resume on resolution.
- **One resolution primitive.** "A record's transition reports back into a
  thread and resumes it" is the single mechanism under asks and gates alike.
  PR #72 hardcoded it for `recordpatchrequest`; this plan promotes it to a
  declared transition marker.
- **Policy narrows, never widens, and cannot be talked out of the way.**
  The emit allowlist stays the hard capability ceiling. Policy decides what
  happens INSIDE it (allow, gate, refuse); no judge verdict, however
  confident, and no agent's own hand, reopens what policy closed.
- **The model sees everything as content.** Answers and verdicts re-enter
  the model as replayable rows (the `system` role landed in PR #72), never
  as out-of-band state the transcript cannot explain, and always as typed
  data, never interpolated into instruction-shaped prose.

## Prior art (research digest)

Where the pending ask LIVES, across the ecosystem: MCP elicitation is an
in-flight RPC (nothing persists); A2A parks the task in an `input-required`
state with a message attached; the OpenAI Agents SDK serializes the whole
run state as a blob the caller stores; LangGraph checkpoints an interrupt
with an id and resumes with a value keyed by it; Google ADK records the ask
as an EVENT in the session log (`adk_request_confirmation` as a synthetic
function call, answered by a function response with the same call id);
HumanLayer and Restate mint a standalone addressable object (a call id, an
awakeable id) that anybody completes later with an idempotent write. The
durable systems all converge on the same lesson this plan builds on: the
pending question is a persisted, addressable object with a stable id, the
answer is an idempotent (CAS'd) write against that id, and the model reads
the answer back as ordinary content. ADK's event-log shape and HumanLayer's
addressable approvals are the closest cousins of interactions-as-records.

The answer taxonomy worth borrowing: MCP elicitation's three-action model
(accept with content, decline, cancel) is the most carefully specified, and
its `requestedSchema` (a flat object of primitives, `enum` + `enumNames`
for choices) is the most portable form shape. This plan's
`answered`/`dismissed` merges decline and cancel deliberately: an ask here
is a durable card in an inbox, not a modal dialog, so "dismissed it without
choosing" is not a state the UI can produce by accident; a dismissal is the
user's explicit act and may carry a note (the `owner/note` annotation,
exactly as decisions do). The questions/options shape maps cleanly onto MCP
elicitation (options are `enum` values with `enumNames` labels) if a bridge
is ever wanted.

How answers re-enter the model, everywhere: as a tool result (OpenAI's
rejection message, AG-UI's tool message, ADK's function response,
HumanLayer's mandatory rejection comment) or as the next user message
(A2A). This plan's `system` row replayed as user content is the second
shape, chosen in PR #72 because the resolution can arrive long after the
tool call's turn is gone.

On judged auto-approval: NO surveyed protocol or framework has a
first-class confidence primitive; the spec-level story is binary human
approve/deny everywhere. What exists is a seam (ADK's
`before_tool_callback` and `RequireConfirmationProvider`, OpenAI's
`needsApproval` predicate that fails closed on malformed arguments) where a
policy or judge composes in, plus third-party confidence-escalation
middleware. The `recordpatchpolicy` judge below is therefore a design, not a
port; the fail-closed posture is the one thing everybody agrees on.

The interactive harnesses (Claude Code, Codex CLI, Gemini CLI) agree on
more than they differ:

- **The question shape converges** on exactly what the interaction kind declares:
  an array of 1 to 4 questions, each a prompt with 2 to 4 options of
  `{label, description}` and a per-question multi-select flag (Claude
  Code's AskUserQuestion, Codex's `request_user_input`, Gemini's
  `ask_user`). Codex adds a non-blocking flag (keep working while the
  question is out), which is this plan's default posture rather than an
  option.
- **Who-decides is a pipeline everywhere**, in the same order this plan
  layers: deny rules outrank ask rules outrank allow rules, a mode sets the
  baseline, the model escalates with a stated reason, and an LLM judge is
  increasingly the default approver (Claude Code's auto-mode classifier,
  Codex's `auto_review` guardian with risk levels and an explicit timed-out
  decision, Cursor's classifier steered by plain-English allow/block
  instructions). Everybody fails closed on the judge's failure.
- **The decision object carries its own remedies**: Codex approval requests
  embed the exact "always allow" rule the client can echo back
  (`proposed_execpolicy_amendment`), Claude Code prompts carry
  `suggestions` that become persisted permission rules, Gemini's
  `proceed_always_and_save` writes settings. Approving and never being
  asked again is ONE gesture, not a settings hunt.
- **Async runners externalize the decision as state** (Jules
  `AWAITING_PLAN_APPROVAL`, Devin's `blocked` session status plus a message
  endpoint, Claude Code headless deferring a persisted tool call to re-fire
  on resume), while interactive ones block the turn. This substrate is the
  async case, and its "state" is the pending records themselves.
- **Timers exist and default off is the right call**: Jules auto-approves
  plans on a timer, Devin waits 30 seconds then proceeds, Claude Code's
  question timeout is opt-in. Auto-proceed is an ecosystem convenience with
  a real failure smell (the decision nobody made); expiry here stays a
  future additive (`expiresAt`, an `expired` transition), never a default.

One divergence to own: interactive harnesses feed the answer back as the
pending TOOL CALL's result, because the call is still open on their stack.
Here the resolution can arrive hours after the turn ended, so it re-enters
as a durable row (the A2A/Jules shape), which PR #72 already chose.

## The `llminteraction` kind

One new core kind, `core.substrate.reamde.dev/llminteraction` (plural
`llminteractions`), named beside `llmthread`/`llmmessage` as the agent
runtime's kinds are, and generic on purpose: today it carries a batch of
questions, and a future interaction shape (a form, a file request) extends
the same record rather than minting a sibling. One record per BATCH,
because the user answers a batch as a unit and the thread resumes once, not
once per question. There is no forking wizard: the questions are fixed at
creation, mutually independent, and answered together.

`questions` and `answers` are deliberately TWO properties rather than one
merged list: they have different writers (the agent, then the user) and
different freeze times (creation, then resolution), so the immutability
guards stay whole-property checks instead of per-field diffs inside a
repeated object. The question `id` is the join, and the card renders them
as one form either way.

```yaml
kind: core.substrate.reamde.dev/llminteraction
properties:
  thread:      # reference core/llmthread, required, inverse: interactions — ENGINE-stamped
  questions:   # repeated object, the reviewed envelope, immutable after create
    fields:
      id:          # string: the agent's key for the question, echoed by its answer
      prompt:      # text: the question itself
      options:     # repeated object: the offered answers, ALWAYS materialized
        fields:
          value:       # string: what an answer selects, unique per question
          label:       # string: what the user reads (optional; value renders otherwise)
          description: # string: what picking it means (optional)
      multi:       # bool: many-of-many; absent/false is exactly one
  answers:     # repeated object, admitted ONLY on the answering patch
    fields:
      question:    # string: the question id this answers
      selected:    # repeated string: chosen option values
  state:       # state property, named `state`
    states: [pending, answered, dismissed]
    initial: pending
    transitions:
      - {from: pending, to: answered,  notifies: thread, stamps: {resolvedAt: now}}
      - {from: pending, to: dismissed, notifies: thread, stamps: {resolvedAt: now}}
```

Rules. Being honest about the mechanism: these are per-kind engine
admission and guards, symmetric to the ones `recordpatchrequest` carries
(admitRequestDiff, guardImmutableEnvelope), not something the generic
object machinery gives away (declared `required` is a form hint, coercion
checks field shape only). Asks earn their hardcoding the same way requests
did.

- **Admission** (the creating write): at least one question; question ids
  unique; option values unique per question; a yes/no question is STORED
  with materialized `yes`/`no` options (admission writes them; labels stay
  presentation), so answer validation and replay never depend on a
  convention that can drift. Bounded on purpose (8 questions per batch, 32
  options per question): past that a form stops being a question. A
  malformed ask is refused at every door.
- **Immutable envelope**: `questions` and `thread` are frozen after create;
  `answers` are refused on every patch EXCEPT the one that performs the
  answering transition, and frozen after resolution, so the record and the
  notification row can never disagree.
- **The answer** (the transition): every question answered exactly once;
  selections are stored option VALUES, never labels or typed text;
  single-select carries one. Answering and dismissing require `ifVersion`
  at EVERY tier (no bundle-tier exemption, unlike a request decision
  today), and are OWNER-TIER transitions: a bundle actor answering an ask,
  its own or anybody's, is refused by an engine guard. Asks are always the
  user's; that sentence is enforced, not hoped.
- **Engine-stamped thread**: `thread` is written by the loop (and later the
  runner), never by a generic write, the same writer-restriction machinery
  provider properties already use. Nothing can point an ask (or any
  notifying record) at somebody else's thread from the outside.
- No partial answers and no free text in v1. A batch the user cannot fully
  answer is a batch the agent asked wrong, and `dismissed` is the honest
  out; an agent needing prose asks for it in chat, where prose already
  lives. Partial resolution is a wizard, and a wizard is a sequence of
  asks. Both are additive later (a `text` answer field, an
  `answeredPartially` transition) if real usage demands them.
- **An ordinary chat message never answers an ask.** The answer is the
  CAS'd transition and nothing else; a user who replies in prose instead
  has said something the agent reads NEXT TO the still-pending ask, not an
  answer to it. This keeps "what did the user authorize" a record, not an
  inference.

### The `ask` built-in

A fifth host function beside query/propose/graphql/mutate:
`core.substrate.reamde.dev/ask` (a distinct name from the kind, so one
spelling never means two things in an error message). Arguments mirror the
questions envelope. It requires `permissions.writes` to name the interaction kind
(the same load-time rule propose has), lands the record with `thread`
stamped by the loop, and returns `{"id": ...}`. The card teaches the model
the contract: the answer arrives in a later turn; ask, then either continue
other work or end the turn; never poll for the answer.

Asking does not block the loop and does not force the turn to end: an agent
may ask and keep working, or ask and settle. When the loop settles with the
ask still pending, the thread is simply not running; the ANSWER is what
resumes it, through the same machinery as a decision.

## One resolution primitive: `notifies: thread`

PR #72's write-back lives in `internal/engine/agentdecision.go` and fires on
a hardcoded kind (`recordpatchrequest`, the `decision` machine). This plan
replaces the hardcoding with a declared transition marker whose value NAMES
the reference property carrying the thread (for core kinds, `thread`):

```yaml
transitions:
  - from: proposed
    to: accepted
    onEnter: applyDiff
    notifies: thread     # NEW: report this transition into the named reference
```

Semantics, all inside the deciding transaction (mechanism unchanged from
PR #72):

- The named property must be a reference pinned to `llmthread`; the loader
  refuses the marker otherwise. The property is engine-stamped
  (writer-restricted) on every kind that carries the marker; a record whose
  thread is empty notifies nothing.
- The transition writes ONE `system` llmmessage into that thread: a JSON
  envelope carrying `event` plus the kind's own fields, and the
  transition's changelog entries in `changes`; then registers the
  after-commit resume. Rolled-back transitions notify nothing. The only
  UNIVERSAL envelope key is `event` (plus `changes` on the row): each
  event's shape is its kind's contract, and the request's existing
  `proposalDecision` shape (request, decision, op, target, version) is
  grandfathered unchanged; asks add `interactionAnswered`/`interactionDismissed` carrying
  the answers verbatim, so the model reads them without a second query.
- The deciding-`ifVersion` requirement (today hardcoded to the request
  kind) generalizes: a marker-carrying transition requires `ifVersion` on
  the owner path. Asks additionally require it at every tier (above).
- Resume is bounded, because a resume is paid compute: per-agent resume
  opt-in (issue #69) is a PREREQUISITE of this marker shipping beyond the
  request kind, a transition performed by the thread's OWN agent never
  resumes that thread (the self-actor exclusion precedent), and the engine
  holds a per-thread resume budget (a small hourly cap, refusals logged and
  visible on the thread) so a bundle kind declaring a notifying transition
  cycle cannot become a token pump. Bundle-declared kinds may carry the
  marker only once those bounds exist; in v1 the loader admits it on core
  kinds alone.

`recordpatchrequest` migrates onto the marker with behavior identical.
`recordmergerequest` does NOT move in phase 1: it has no thread property
today, so putting it on the marker is new behavior (an additive later step),
not a refactor. One loader consequence stated plainly: a closure using the
new transition key does not load on older binaries, the ordinary skew rule
for any loader widening.

## Gates stay `recordpatchrequest`

Gates need no new kind: a gate IS a proposed mutation, and the request kind
already carries the reviewed envelope, admission at every door, the CAS'd
decision, conflict annotations, the thread reference, and the console card.
The decision transition and the accept's apply are ONE transaction (request
and target locked, the apply CAS'd on `targetVersion`), so two deciders,
human or judge, serialize on the row and the loser observes the committed
outcome; the plan adds no second decision machinery. What finishes the
picture:

- The `notifies: thread` migration above (no behavior change, one less
  hardcoded kind).
- **Self-decision is refused.** A bundle-tier actor never decides a request
  it proposed, and a bundle-tier actor never decides a POLICY-GATED request
  at all (the policy's own judge path is the engine's, below). Without
  this, an agent whose emit covers the target kind could accept its own
  gate and the whole policy layer would be decorative. The rationale joins
  the frozen envelope (today it stays mutable after propose), so what a
  judge or human read cannot be reworded under a pending request.
- **A conflicted accept reports back too.** When an accept fails because
  the target moved (the diffConflict path), the proposing thread today
  learns nothing. The conflict's annotation transaction gains the same
  notification (`event: "proposalConflicted"`), and the inbox surfaces the
  request as needs-re-propose, so an agent told "held for review" is never
  left waiting on a request that can no longer land.
- The inbox treats pending asks and pending requests as one "needs you"
  surface, grouped by thread: both are records, so this is a query, not a
  feature. A thread's open interactions are `interactions(state: pending)` plus
  `proposals(decision: proposed)` through the reference inverses (issue
  #71).
- The card and the review page keep resolving live state, never snapshots
  (the #72 rule), so the transcript and the inbox cannot disagree.
- **The decision carries its own remedy** (the pattern every harness
  converged on): the gate card's accept can also offer "accept, and stop
  gating writes like this", minting a `recordpatchpolicy` allow rule. The remedy
  is server-PROPOSED and deliberately narrow: exactly this agent, this
  kind, this op, offered target-scoped first, never a wildcard, shown as
  the exact rule text in a second confirmation, and the minted rule carries
  provenance (an annotation naming the request that minted it). One click
  reviews one diff; turning a diff into a standing grant is its own,
  explicit act.

## Policy: the space between wanting and landing

Today an agent-actor write has exactly two outcomes: inside the emit
ceiling it lands, outside it refuses. Policy adds the middle: writes that
land only as gates, and a judge that decides gated requests within bounds
the owner set. It layers deterministically, narrowest authority first:

1. **The emit ceiling** (exists): the hard capability boundary, intersected
   per sub-agent hop. Policy runs strictly inside it.
2. **The declaration's own floor** (new, on the tool/function): objective
   facts the AUTHOR states. A function declares its effect class (`read`,
   `write`, `external`, `irreversible`) and optionally a confirmation floor
   (`always`: never auto-applied, whatever any policy or judge says).
   Floors only tighten.
3. **The owner's rules** (new, `recordpatchpolicy` records): which agent writes
   land, gate, or refuse.
4. **The judge** (new, optional): an LLM deciding GATED requests between
   the owner's confidence thresholds, through the engine.

### The `recordpatchpolicy` kind

Policy is records, so it is per-repository data the owner edits and the
changelog versions, not configuration in a file. One guard keeps it from
being fail-open data an agent can edit: bundle-tier writes to
`recordpatchpolicy` refuse unconditionally (policy is the owner's hand
alone, like a bundle's owner-gated records).

```yaml
kind: core.substrate.reamde.dev/recordpatchpolicy
properties:
  selector:      # object: which writes this policy speaks for
    fields:
      kinds:       # repeated string: kind references; empty means every kind
      ops:         # repeated string: put/patch/delete; empty means all three
      agents:      # repeated string: agent identities; empty means every agent
  action:        # enum: allow | gate | refuse
  judge:         # reference core/agent: judges what this policy GATES; optional
  criteria:      # text: the owner's instructions to the judge, in prose
  context:       # enum: envelope | thread — how much the judge sees; envelope is the default
  autoAccept:    # float 0..1: judge confidence at or above accepts the gated request
  autoRefuse:    # float 0..1: judge confidence at or above rejects it
  mode:          # enum: enforce | advise (advise: the judge recommends, never decides)
  disabled:      # bool
```

Composition, when several policies match one write: the most restrictive
action wins (`refuse` over `gate` over `allow`), the rule every surveyed
harness follows (deny outranks ask outranks allow). No priority integer to
misorder; the audit records every match and which one governed. Edge writes
(`link`/`unlink`) are not in the selector on purpose: the request kind
cannot express them, so they stay governed by the emit ceiling alone until
an edge op exists (a policy wanting to stop an agent's links names the
kind's writes generally, or the grant comes off the agent).

### At the write door: allow, gate, or refuse (deterministic)

For every put/patch/delete under a BUNDLE-tier actor (an agent's mutate, a
function tool's effect) the engine, after the emit ceiling admits it:

- Match policies; no match means today's behavior (the write lands). Policy
  never runs for owner or machine writes.
- `allow`: the write lands; the changelog payload carries the policy id, so
  the audit says why it needed nobody.
- `refuse`: a tool error naming the policy, exactly like an emit refusal.
- `gate`: the write CONVERTS into a `recordpatchrequest`: op and target
  from the write, the diff as the write's normalised shape, `targetVersion`
  from the write's own CAS (its `ifVersion` when it carried one, else the
  target's version at conversion, which TIGHTENS the write's semantics from
  apply-to-current to apply-to-what-was-reviewed, stated here on purpose),
  the thread stamped when a loop is running, the governing policy
  annotated. The request's id derives from the dispatch's stable
  idempotency identity (delivery, call path, ordinal), so a retried
  delivery converts to the SAME request, create-if-absent, instead of
  minting duplicates.
- A declaration whose confirmation floor is `always` gates regardless of
  any `allow`.

How the model hears it, honestly per surface: a gated MUTATE resolver fails
typed: a `gated` error code carrying the request's path in extensions,
never a fake success, and the console renders that tool chip as held, not
failed. Mutations in one document stay independently transactional (they
already are today), so one document can land one write and gate another;
the result names each. A gated function EFFECT fails the whole effect batch
the way an emit refusal does today, with the request path in the message:
effects are all-or-nothing, and half-applying around a gate would lie about
what the function did.

The door decision is deterministic and cheap: no model call sits inside a
tool call, so tool latency does not grow and a judge outage cannot wedge a
run.

### The judge: gated requests, decided by the engine within thresholds

When a policy that gated a request (or a `propose` matching its selector)
names a `judge`, the ENGINE runs the judge and the ENGINE decides; the
judge only ever recommends. Order of operations, fail-closed at every gap:

1. The engine reads the pending request at a version and invokes the judge
   agent with a typed projection of exactly what a human reviewer reads: the
   frozen envelope (op, target kind and id, the normalised diff, the now
   equally frozen rationale), the proposing agent's identity, and the
   policy's `criteria`. How much context the judge gets is the OWNER's
   dial, not the judge's: the default is that envelope and nothing else,
   and a policy may widen it with `context: thread` (the proposing thread's
   recent turns, delimited as data) where the domain needs it — wider
   context is more signal AND more injection surface, so it is an explicit
   choice per policy. The judge is TOOL-LESS and emits nothing: its whole
   contract is the structured reply
   `{verdict: accept|reject|escalate, confidence: 0..1, rationale}`.
   It never sees sealed values or secret-typed properties (a diff cannot
   carry them anyway).
2. The engine routes, in `enforce` mode: `accept` at or above `autoAccept`
   accepts; `reject` at or above `autoRefuse` rejects with the rationale as
   the note; everything else, `escalate`, sub-threshold confidence,
   malformed output, transport failure, blown judge budget, a crash between
   verdict and decision, leaves the request PROPOSED for the human,
   carrying the verdict. The decision is CAS'd on the request version read
   in step 1, so a request that moved under the judge escalates instead of
   deciding stale.
3. The engine's decision runs under the policy's own actor
   (`policy:<id>`), bounded by the POLICY's selector kinds, not by the
   judge's emit. This is the coherence fix the reviews converged on: a
   judge with target kinds in its emit would be a broad direct writer (the
   accumulation the ceiling exists to prevent), and a judge without them
   could never act. The owner authored the policy naming those kinds; the
   policy IS the grant, the judge is advice, and `authorizeRequestOp` grows
   one arm that says so.

In `advise` mode the judge never decides: the verdict lands beside the
pending request (and on its console card) as a recommendation the human
reads. This is the flexibility knob: the same policy record moves from
"judge advises me" to "judge decides within these bounds" by one property.
Unset thresholds mean advise, whatever the mode says: conservative is the
default, not an option.

Confidence is a routing signal, not a probability: model-reported numbers
are uncalibrated and drift across models and prompts. The verdict pins what
produced each number (the judge agent's declaration version and model),
thresholds stay the owner's data, and nothing above a declaration's
`always` floor is ever model-decided.

### The audit lives on the records it concerns

No separate audit kind. Every judge invocation, failures included, writes
its verdict onto the REQUEST as an engine-owned annotation
(`policy/verdict`: the policy id AND the changelog seq of its evaluated
revision, because the policy record is mutable and the audit must not be;
the request version read; verdict, confidence, rationale; the outcome,
`accepted`/`rejected`/`escalated`/`advised`/`error`; and the judge's thread
path). Three existing surfaces make that a complete audit with no new kind:
the CHANGELOG already versions every annotation write immutably (a
re-judged request's history holds both verdicts), the judge's THREAD is the
full run record with its cost tallied, and a door-allowed write's changelog
payload already names the policy that allowed it. "Why did this land
without me" is always answerable from records alone, and nothing needs
garbage collection because nothing was minted to expire.

### What policy is not

- Not a widening: no verdict lands a write the emit ceiling or a
  declaration's floor refuses; those run first and policy runs inside them.
- Not self-serviceable: the gated agent cannot decide its own gate, cannot
  edit `recordpatchpolicy`, and cannot forge the verdict annotation (the
  `policy/` namespace is the engine's, the way `substrate/conflict` already
  is).
- Not a second judge machinery: the arbiter-by-trigger pattern (an agent
  watching request creation) keeps working for people who want it; a
  policy's judge is the engine running the same shape of agent with
  thresholds, CAS and audit attached.
- Not per-question routing for asks: asks are always the user's. A policy
  that answers questions FOR the user is an agent talking to itself.

### The flow, end to end

One worked example, so nobody reverse-engineers it from the sections above.
The owner has one policy: selector `{kinds: [people/person], agents:
[assistant]}`, action `gate`, a judge with `autoAccept: 0.9`, mode
`enforce`.

1. The assistant tries `mutate` on a person record. The emit ceiling admits
   it (person is in its emit); the policy matches and says `gate`; the
   write does NOT land. It converts into a `recordpatchrequest` (diff,
   target, `targetVersion`, the thread, the policy id), and the tool result
   tells the model "held for review as <id>". Had the agent used `propose`
   instead, the request simply exists already and the door step never runs:
   both roads meet at the same pending request.
2. The engine sees a gated request under a judge-bearing policy and runs
   the judge over the request's envelope. Verdict `accept` at 0.95: the
   engine accepts under `policy:<id>`, the diff applies, the verdict lands
   on the request as `policy/verdict`. Verdict at 0.7, or `escalate`, or
   the judge fell over: the request stays PROPOSED, the verdict rides the
   card as a recommendation, and the inbox shows it. The user decides.
3. Whoever decided (judge or user), the decision transition writes the
   `system` row into the proposing thread (verdict, target, new version)
   and the thread resumes: the agent's next turn reacts to it, in the
   transcript, where every reader sees it. The user is asked exactly when
   the judge lacked either the verdict or the confidence to act, and never
   learns about it from a side channel: the thread and the inbox are the
   same records.

## Resolution delivery: at-least-once, and honest about it

The resume that PR #72 shipped is fire-and-forget: an in-process goroutine
that a restart or a busy thread drops (the message always lands; the
REACTION could be lost). Asks make that gap user-visible (a person answered
and nobody came back), so the marker work hardens delivery, still without
any new infrastructure:

- **Settle-time re-check**: when a loop settles, it re-reads for resolution
  rows newer than the last turn it consumed and immediately continues
  (budgets still bound the run). A resolution landing MID-turn is therefore
  consumed at settle instead of dropped with the lease refusal.
- **A sweep**: on boot and on a slow tick, threads whose newest resolution
  row postdates their `finishedAt` and whose status is settled get resumed.
  Both facts are already in the records; the sweep is one query. Resume
  storms coalesce here too: N resolutions on one thread produce one
  continuation that reads all N rows.
- The `turn` ordinal a resolution row takes may collide with a concurrent
  continuation's counter; ordering stays honest because replay orders by
  creation, and the ordinal is display. Stated so nobody chases it as a
  bug.

## Considered and set aside

The independent pass and the two reviews converged with this plan's spine:
interactions as records, batches answered as a unit, settle-and-resume,
`recordpatchrequest` kept distinct from questions, the deterministic door
with the judge behind it, no ADK migration. Divergences, and the rulings:

- **A `waiting` thread state and a single `waitsFor` barrier per thread.**
  Set aside: threads already carry several pending proposals at once, so a
  single-barrier rule would regress the shipped gate flow, and a `waiting`
  status duplicates what a query over pending interactions answers. "Is
  anything waiting on me" stays DERIVED (the inbox); the delivery
  hardening above is what actually guarantees consumption, not a status.
- **`originMessage`/`answerMessage` correlation edges.** Set aside: the
  tool row's engine-stamped `changes` already names the interaction record
  it created (that is how proposal cards attach today), and the resolution
  row is the notification itself.
- **One record per question, answers as separate records.** Set aside for
  one record per batch: a single respondent (the repository's one user)
  answers a batch as a unit and the thread resumes once. Rich per-question
  options survive because declared fields nest (verified: repeated objects
  in fields, depth 3 of the vocabulary's 4).
- **Judging at the tool-call door.** Set aside for judging the
  MATERIALIZED request: nothing model-shaped sits inside a tool call's
  latency, a judge outage degrades to "a human decides", and what was
  judged is verbatim what would land.
- **Priority integers on policies.** Set aside for most-restrictive-wins:
  deterministic without bookkeeping, and the composition every harness
  already trained users on.

## Functions asking for a decision (sketch, ticketed)

Function bodies are one-shot: they return effects, the engine applies them.
Two honest ways a function participates, neither built now:

1. **Gated effects** (cheap): a function marks an effect `review: true`, or
   a `recordpatchpolicy` matching function actors gates it. The effect becomes a
   request; the function has already returned. Fits functions whose write
   IS the last step (most of them). No new runtime.
2. **Ask-and-continue** (real design work): a function returns early with
   an `ask` effect (questions, same envelope) plus a `resume` marker. The
   answer transition re-invokes the function with the answers in its input
   and the same idempotency key, so the function is written as "called
   again, now knowing". This needs the runner's invocation contract to grow
   a durable request state machine (canonical arguments, execution lease,
   result records, timeout and cancellation), which the ticket specifies at
   the interface level even though implementation waits.

Either way the function never writes interaction records directly and never
escapes its effect and emit ceilings: the engine materializes the ask or
the gate from the returned shape, exactly as it applies every other effect.
And when a function's held step is an EXTERNAL side effect rather than a
record write (send the email, charge the card), `recordpatchrequest` is the
wrong envelope: that wants a distinct request kind carrying the exact call
preview and an idempotency key. Out of scope here; noted so nobody
overloads the patch request with non-record effects.

## The loop stays ours: the ADK position

Assessed against adk-go as of 2026-08 (module `google.golang.org/adk/v2`,
v2.2.0; a v1 line still maintained beside it, and a module-path break seven
months after v1.0). What Go actually ships today: llmagent with
before/after model/tool callbacks, workflow agents
(sequential/parallel/loop) plus a graph engine with pause/resume, tool
confirmation HITL (`toolconfirmation`, `IsLongRunning`), MCP toolsets,
OTel, and session persistence via GORM with its own tables. What it does
NOT ship in Go: context compaction (python-only, roadmap in adk-go
discussion #589), LiteLLM-style multi-provider (python-only; Gemini and
OpenAI are native, Anthropic is a third-party adapter), budgets or cost
accounting of any kind, and any permission model.

**Position: steal the patterns, keep the loop.** Not as a framework, not
as an orchestration-only library (workflow agents run through the
Runner/Session machinery, so "orchestration only" still drags the
session/event model in).

The reasoning, condensed:

- Everything hard in `agentloop.go` is exactly what ADK has no expression
  for: the transcript as records through the changelog fold
  (crash-readable, RLS-scoped, `changes`-stamped), emit ceilings
  intersected per sub-agent hop, read budgets with non-oracle absence
  answers, turn/tool/deadline/cost budgets with the synthetic-tool-error
  pattern, the per-thread lease, and idempotency keys from stable delivery
  identity. A migration keeps all of that as adapters around ADK and
  deletes nothing.
- The two features that would justify the dependency are the two that do
  not exist in Go: compaction and provider maturity. The GORM session store
  is a second source of truth outside the changelog and outside row-level
  security, which this design cannot accept.
- The honest counterarguments, recorded: the graph/workflow engine is real,
  GA, and expensive to hand-build, and ecosystem gravity (MCP, A2A,
  registry, release velocity) compounds monthly. If deterministic
  multi-agent pipelines become core, or Go gains compaction plus
  first-party Anthropic, re-evaluate; the workflows ticket carries this.

What gets stolen, concretely, on this plan's phases: callback seams in the
loop (one before-model, one before/after-tool hook point, which is where
policy evaluation attaches, rather than a bespoke arm per feature), and a
compaction design cribbed from adk-python's `EventsCompactionConfig`
(summarize a window of turns into a `system` row at an interval, with
overlap), which fits the existing prose-history replay and is its own
ticket.

## Console and API surface

- **Transcript cards**: an ask renders as a form card in the thread (radio
  or checkboxes per question, dismissal beside submit), answered inline
  with one CAS'd patch, the same shape the proposal card set. Question and
  option text render as UNTRUSTED content: plain text, no markdown-to-
  widget rendering, the submit names stored option values only. Answered
  asks render the chosen values; the `system` resolution row renders as the
  event line it already is.
- **Inbox**: the review page grows from "change requests" to "needs you":
  pending asks and pending requests (including conflicted ones needing
  re-propose), grouped by thread, newest first. Both are record queries.
- **GraphQL**: no bespoke fields. Asks and requests reach consumers through
  the generic record surface today and through reference inverses
  (`thread { interactions, proposals, messages }`) once issue #71 lands, which this
  plan makes load-bearing.
- **Streaming**: the live overlay gets one new `AgentEvent` kind, `ask`, so
  a running chat renders the form the moment the tool settles instead of on
  refetch. Persisted rows stay the truth on reload, as with tool cards.

## Versioning and rollout

Additive throughout; no narrowing, no data migration:

1. **Phase 1, the primitive, hardened**: the `notifies` transition marker
   (loader + the engine hook generalized from agentdecision.go, core kinds
   only); `recordpatchrequest` alone migrates onto it, behavior identical;
   per-agent resume opt-in (#69) lands here as a prerequisite, with the
   self-agent exclusion and the per-thread resume budget; the settle-time
   re-check and the resolution sweep; the conflicted-accept notification.
2. **Phase 2, asks**: the `ask` kind with its admission and guards, the
   `ask` built-in, engine-stamped `thread`, owner-only CAS'd answers,
   the console form card, the inbox growth. The llm example bundle grows an
   asking demo.
3. **Phase 3, policy, static**: the `recordpatchpolicy` kind (owner-only), door
   evaluation with allow/gate/refuse and most-restrictive-wins, the gate
   conversion with deterministic request ids and the typed `gated` result,
   declaration-level effect class and confirmation floor on functions.
4. **Phase 4, the judge**: engine-run judging with CAS and thresholds,
   enforce/advise, the policy-actor decision arm in `authorizeRequestOp`,
   the engine-owned `policy/verdict` annotation, the remedy flow on the
   gate card. Ships with a judged example in the llm bundle.
5. **Ticketed beside**: sub-agent change aggregation (#70), the GraphQL
   thread surface (#71, load-bearing for the inbox), function escalation,
   workflows, compaction (which must never fold away turns that pending
   interactions still reference; adk-python issue 4740 is the cautionary
   precedent).

## Risks that remain open

- **Prompt-injected asks**: a malicious document the agent read can phrase
  a question that tricks the user ("select yes to continue"). Untrusted
  rendering and value-only answers narrow it; they do not close it. The
  gate beside it always shows the REAL diff, and nothing an ask answers
  authorizes a write by itself: only gates write.
- **Judge cost**: a judge run per gated write is an agent run with its own
  budgets and tally; a policy gating every write of a chatty agent
  multiplies cost. Accepted: policy is opt-in data, and the door itself
  never waits on a model.
- **Batch ergonomics**: one record per batch means one resume per batch and
  no partial answers. If real usage wants "answer what you can", that is a
  new transition added additively, not a redesign.
- **The remedy's residual risk**: even narrow, provenance-stamped rules
  accumulate. The policy list is one page the owner can read and prune;
  whether that is enough attention is a product question this plan defers
  to usage.
