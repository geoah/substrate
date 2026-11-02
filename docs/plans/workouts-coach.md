# Plan: the workout coach agent Substrate Workouts installs

Status: revised after review (Astra, Fable) and the owner's answers of
2026-11-02; being built on branch `feat/workouts-coach`. Decision record:
[0207](../decisions/0207-an-app-declares-one-agent-held-to-its-own-reach.md).
Depends on
[0205](../decisions/0205-an-app-owns-the-package-it-declares.md) (PR #1112,
app-owned packages).

## 1. The request and the owner's answers

The owner, 2026-11-02:

> Include a workout agent record for substrate that has access to read all
> the user's observations and health data, and read/write workouts, so that
> it can create and edit routines. Research and find best practices for
> workouts, find what questions it should ask the user etc, allow it to
> store and retrieve memories with a workout tag/label or something from the
> user answers, etc.

The answers to the first draft's questions, the same day:

1. **Reach.** Health data and every observation; also clinical conditions and
   medications, read-only, because injuries and medication matter for safe
   training. No other clinical kind.
2. **Memories.** A kind in the app's training package, `~/training/memory`,
   with a topic, plus the research's `response` and `change` topics. The
   general assistant may read them but not write them, if that is simple.
3. **Model and spend cap** are set in the app, not in the console (the app's
   screen for it comes later; see below).
4. **Revoking Workouts stops the coach.** At dispatch an app's agent runs
   only while its app holds a live token covering the agent's reach.
5. **Writes.** The coach writes routines directly, only after confirming the
   change with the owner in chat, and never mid-session.
6. **Spend.** $1 a day by default, $2 a day at most; the approval shows the
   actual cap.
7. **Proactive check-ins** come later.
8. **From the research:** intake is two short forms plus single free-text
   questions; a deload is a separate routine, not a flag on the normal one.

**Narrowed the same day:** "we don't need to do anything with the coach in
the workouts app for now, we just want to add the agent when installing so
we can use the agent from the substrate chat app." So the app only asks for
the coach at sign-in and installs and updates it, on default provider, model
and cap. The coach screen (model and cap controls, a memories review,
"Ask your coach", turning it off and on from the app) comes later. The owner
reaches the coach in Substrate Chat and the console's Agents page.

## 2. What substrate already has

- **Agents** are `substrate.reamde.dev/core/agent` records, id
  `<authority>/<package>/<name>`, running as `agent:<authority>:<package>:<name>`
  in the bundle tier. Reach is `permissions.reads.kinds` and
  `permissions.writes`, each a kind or a glob; a glob never reaches the four
  auth kinds (0080) and an exact entry reaches an `internal` kind. Tools are
  host functions (`query`, `write`, `ask`, `propose`, `wait`), functions,
  `toolsFrom` and sub-agents. A disabled bundle's agents refuse every entry.
- **Spend** is `budgets.spendCentsPerDay` per agent and a repository cap
  (0149): a rolling 24-hour threshold checked when a root run is admitted,
  from what threads recorded. A model the provider row has no price for
  costs 0.
- **App tokens** carry scopes (0194). `declare:~/<package>` (0203) applies
  one package's package, bundle and kind documents and nothing that runs.
  Authorization details (0202) set up packages, accounts and webhooks; a
  webhook detail is the precedent for a detail that grants something by
  name.
- **App-owned packages (0205, #1112).** A package whose row says
  `declaredBy: app:<host>` changes only through that app; the owner may take
  it over. Bundle enable and disable stay the owner's.
- **Memory.** `llm/memory` is the owner's general memory; no app scope
  writes the `llm` package.

## 3. Options

1. The approval installs the agent from a manifest in the details. Rejected:
   a prompt is several times the 2 KB details cap, and every instruction
   update would need a browser round.
2. Ship it as a sample or a catalog bundle. Rejected: it becomes the owner's
   or the server's copy, and a catalog package cannot name `~/training`.
3. A write scope on `core/agent`. Rejected: it reaches every agent and lets
   an app grant itself reach.
4. **An `agent` detail approves one named agent, its reach and its cap; the
   app declares it under its `declare:` scope; the engine holds it to a live
   approval at every run.** Chosen.
5. The owner applies a manifest the app hands over. Kept as nothing more
   than what an owner can always do.

The first draft of option 4 held the agent to the app's own token scopes,
checked once at apply. Both reviews showed why that is not enough: the check
drifted after approval (expiry, revocation, narrowing), it used pattern
coverage rather than effective permission, it widened the phone token to
every health read, and its spend promise was void for an unpriced model.
The design below answers each (§13).

## 4. The design

### 4.1 Consent: the `agent` detail

The app asks for its coach with one more authorization detail:

```json
{"type": "agent",
 "agent": "~/trainingcoach/workoutCoach",
 "reads": ["~/training/exercise", "~/training/setup", "~/training/routine",
           "~/training/session", "~/training/set", "~/training/memory",
           "substrate.reamde.dev/health/workout", "substrate.reamde.dev/health/sleep",
           "substrate.reamde.dev/observations/observation", "…/metric", "…/source",
           "…/sourcelink", "…/trace",
           "substrate.reamde.dev/clinical/condition",
           "substrate.reamde.dev/clinical/medicationrequest",
           "substrate.reamde.dev/clinical/medicationdispense"],
 "writes": ["~/training/exercise", "~/training/setup", "~/training/routine",
            "~/training/memory"],
 "spendCentsPerDay": 100}
```

Rules, checked strictly where every detail is read (`internal/authorize`):

- `agent` is `~/<package>/<name>`: the repository's own authority, a package
  word and a camelCase name. The same request's `scope` must carry
  `declare:~/<package>`; a request without it is refused at
  `/oauth/authorize`. An approval without that scope, because the owner
  unchecked it or because 0205 refuses it at approval (the owner or another
  client id keeps the package), approves the rest of the request and leaves
  the agent off the token; the agent row then says "Left out" and why.
- `reads` (1 to 24) and `writes` (0 to 16) are exact kinds named in full
  (`~` allowed as the authority), never a glob, never an auth kind, and never
  a kind of `substrate.reamde.dev/core` or `…/llm`, read or written. That is
  the **effective-permission rule**: an app's agent reaches no machinery,
  however its token is worded (Astra 1). Every write must also be a read.
- `spendCentsPerDay` is a whole number from 1 to 200, the ceiling of every
  app's agent. The cap is a rolling 24-hour threshold, so the page says the
  agent "stops for the day once it has spent about $1.00".
- One agent detail per request: one app keeps one agent, so caps never add
  up past one ceiling.

**The approval row.** Under "It will set up on your server" the page shows
"Add an AI agent: Workout coach" with three labeled lists: "It reads" and
"It changes", each kind by its label (one the server lacks reads "not on
this server yet"), and "It spends", the approved cap ("At most $1.00 a day
of your AI provider's credit"). Then "It can ask you questions in the chat",
and that what it reads is sent to the AI provider it runs on, the app
writes and updates its instructions, it stops when the app's access ends,
and the owner can turn it off on its package's page. The technical view
lists the agent id, every kind id, the interaction kind its questions
write, the cap in cents and its tools. Nothing is written at approval: the
row's action is `none` and approving runs no set up.

**What approving stores.** The token record gains `agents`, a repeated object
(`agent`, `reads`, `writes`, `spendCentsPerDay`), the approved details with
`~` resolved. The app's own `scope` is unchanged by the agent's reach: the
phone token does not read health records it never uses (Fable 4). No scope
verb is added, so no older binary refuses the token (Fable 12), and no
three-segment `declare:` exists to strand (Fable 8, 17). The token response
echoes each approved agent detail.

`/authorizations/check` answers an agent row `none` when the calling token
holds an approval covering it (same agent, reads and writes at least as
wide, cap at least as high), and also when the request leaves out the
agent's `declare:` scope or 0205 would refuse it (the row carries the reason
in `withheld`), since approving again would withhold the agent again. It
answers `enable` otherwise, so an app knows when a widened reach or a
raised cap needs the owner again. SubstrateKit leaves the agent detail out
of its check when the owner left that `declare:` scope out, so a declined
coach is never asked about again.

**Who else reaches the agent (the coverage rule).** Under 0195 an `agents:`
scope talks to the matching agents and reads their chats. An app's agent
answers another token (chat, its row, its threads, messages and
interactions) only when that token's own read scopes cover every kind the
agent reads, so the agent is never a channel to data the caller could not
read itself. This is stricter than naming the agent exactly, and simpler:
Substrate Chat's `read:*` covers the coach's kinds (none of them is
internal), so Chat sees and chats with it with no new approval, while an
app that reads only its notes sees nothing of it. The approval row says
"Apps you let talk to your agents can ask it questions and read its chats
only if they can read everything it reads", and names the apps that already
can (`talkedToBy`).

### 4.2 The scoped apply

`declare:` admits a `core/agent` document only when every rule holds:

- **Identity.** The id is an agent the token's `agents` approve, and the
  token declares its package.
- **Keys.** `authority`, `package`, `description`, `purpose` (absent or
  `primary`), `prompt`, `provider`, `model`, `params`, `tools`, `context`,
  `compaction`, `budgets`, `permissions`. Never `toolsFrom`, `subagents`,
  `hiddenFromChat` or `resume`.
- **Tools.** Exactly references to `core/query`, `core/write` and `core/ask`,
  without `background`.
- **Reach.** Reads and writes are exact kinds, each in the approved
  `reads`/`writes`, each passing the effective-permission rule again (a
  damaged token never widens). `llm/interaction` is the one extra write, and
  only beside `ask`. A pinned kind must be a read (the loader holds that).
- **Spend.** `budgets.spendCentsPerDay` is required, at least 1 and at most
  the approved figure (itself at most 200).
- **Pricing.** The provider row exists and prices the model, with input and
  output rates above zero; otherwise the apply is refused, because spend
  would count as 0 (Fable 2).
- **Guidance.** `params.engineGuidance` is not `false`.

A bundle may list or drop the approved agent; it never drops a kind (the
existing `widensOnly` rule). The app removes its whole coach package through
`UninstallBundle`, which the scoped dataset now admits for a package the
token declares that holds no kinds (Fable 10).

### 4.3 At every run: the live-token ceiling

The engine admits a run of an agent whose package is app-owned
(`declaredBy: app:<host>`) only when all hold, on every entry (chat, call,
trigger, wake, retry, judge):

1. **A live approval.** The repository holds a token of the app that owns
   the package, compared as 0205 compares it (the full client id the row
   stamps; a host-only stamp from an earlier binary admits its host),
   unexpired and not deleted, that approves this agent, and **every** such
   live token's approval covers the agent's reads and writes (interaction
   aside) and cap. The lowest cap and the narrowest reach hold, so an older,
   wider token never outlives the owner's newer, narrower approval. A token
   of the app that approves no agent does not count. A stored approval that
   cannot be read, or is no longer admissible, is dropped from its token
   alone (logged): the coach stops, and the token and the token list keep
   working.
2. **A priced model**, as at apply: an owner who removes the price stops the
   coach rather than uncapping it.
3. **A root run, from a person.** An app's agent never runs as another
   agent's sub-agent, whose cap would hide its spend, and runs only from a
   chat, its resume after a question, or the owner's direct call; a
   trigger, a function's call or a policy's judge would hand its reply to
   code that may read far less, so each is refused until an app's agent
   needs one.

A refusal is a guard (403) with words the owner can act on: "Workout coach
is stopped: Substrate Workouts holds no live approval for it. Sign in to
Substrate Workouts again, or turn the coach off on the Training coach
package." An unpriced model's refusal points to the console's Providers page
and says the app picks a priced model again at its next sync, which it does.
Signing in again restores it, and nothing flips state (Fable 1;
owner answer 4). A package the owner took over is theirs, and none of this
applies to it.

**Within a run** the loop re-admits an app's agent before every model call
after the first: the bundle still enabled, the approval still live, its own
cap not reached. A failure settles the thread `stopped` with the reason. So
turning the coach off, revoking the app or reaching the cap stops a running
turn at its next model call (Astra 7).

### 4.4 What the cap means

The coach's cap is its own rolling 24 hours of recorded spend, checked when
a run starts and again before each model call. A model call already running
is charged when it returns, so a run overshoots by at most one call and its
tool calls. It never runs as a sub-agent, and its model is always priced, so
nothing it spends counts as zero. The repository cap still applies on top.
The approval and the app state that wording; neither promises a hard ceiling
to the cent (Astra 2).

### 4.5 What the app may read

Each of these is what install and update need, even without a coach screen:

- **Its agent row.** `Get` of `core/agent` for an approved agent of a
  package the token declares, and nothing else of that kind (Fable 6).
  Update needs it: the app reads the stored prompt's revision line, so an
  older build never writes over a newer one's coach; it keeps the stored
  cap, and the stored provider and model unless its own choice ranks
  strictly better (§8); and it skips an apply when the row already says
  what it would declare.
- **The definitions of the kinds its agent is approved to read** (`Get` of
  the `core/kind` row, or `KindByRef`), so it declares only kinds the server
  holds: an unknown exact kind is refused at load, and the clinical catalogue
  may not be installed. Definitions only; records stay held to the scopes.
- **`llm/provider` rows**, by the exact scope
  `read:substrate.reamde.dev/llm/provider`, which Substrate Chat already
  asks for, to choose a keyed provider that prices a model. The secret
  `apiKey` is redacted by the engine on every read; the end-to-end test pins
  it for an app's token (Fable 16).

### 4.6 Owner control

- **Turn off and on: bundle disable**, Pause on the package page and on the
  agent page's "Managed by" section, and `substratectl bundle disable`. It
  is the owner's under 0205, the app cannot undo it (the scoped dataset
  refuses enable and disable), and the app's re-apply keeps it disabled. A
  test pins each.
- **Model, provider and cap:** the app's defaults for now (owner answer 3;
  the app's screen to change them comes later). The agent page shows them
  read-only, with the app named as the one that keeps them.
- **Take over (0205)** makes the coach the owner's own agent, with no
  ceiling and no app updates. After it, the app's next sign-in has its
  `declare:~/trainingcoach` refused at approval and the agent left out, and
  the app reads the package row and stops applying, as it does for training.
- **Purge** of the coach's package stays locked until a takeover (0205); it
  holds no records anyway. The owner's uninstall of training, which the coach
  requires, is refused by 0205's dependency guard while the coach is the
  app's.
- **Revoking** the app, or letting its token expire, stops the coach at its
  next model call. The coach's row, threads and memories stay.
- **Remove:** the scoped dataset lets an app uninstall a package it keeps
  that declares no kinds (agent, bundle and package rows; memories and
  threads stay). The app does not call it yet; its "Remove coach" comes
  with the coach screen. The owner removes the coach by taking the package
  over and uninstalling it.
- **Rollback.** A binary without this change ignores the token's `agents`
  property (the token still authenticates), refuses no token, and runs the
  coach without the live-token ceiling. Its kind's closure stays loadable:
  the agent document is an ordinary agent. The release notes say so.

## 5. The agent record

The coach lives in its own app-owned package, `~/trainingcoach`, whose
bundle requires `~/training` at version 2 or later (`requiresAtLeast`,
Fable 9) and installs only the agent. Training's version state machine stays
untouched, the coach turns off alone, and memories stay training data.

Agent id `<authority>/trainingcoach/workoutCoach`, shown as "Workout coach".

```yaml
kind: substrate.reamde.dev/core/agent
metadata:
  id: ~/trainingcoach/workoutCoach
data:
  authority: "~"
  package: trainingcoach
  purpose: primary
  description: >-
    Your strength coach: plans and edits your workout routines from your
    goals, your training history and your health data, and remembers what
    you tell it. Installed and updated by Substrate Workouts.
  prompt: <rendered by the app, §6>
  provider: <chosen in the app>
  model: <chosen in the app>
  params: {reasoningEffort: <per wire>}
  tools:
    - function: substrate.reamde.dev/core/query
    - function: substrate.reamde.dev/core/write
    - function: substrate.reamde.dev/core/ask
  context:
    pinned:
      - kind: ~/training/memory
        filter: '{"properties": {"active": {"eq": true}}}'
        orderBy: createdAt:desc,id
        first: 60
        fields: [text, topic, exercise, until]
    maxBytes: 12288
  permissions:
    reads:
      kinds: <the approved reads the server holds>
      budgets: {calls: 40, rows: 5000}
    writes: <the approved writes> + substrate.reamde.dev/llm/interaction
  budgets:
    maxTurns: 16
    maxToolCalls: 48
    maxParallelTools: 4
    deadlineSeconds: 300
    maxToolResultBytes: 65536
    spendCentsPerDay: <the owner's cap, 100 by default>
```

Newest memories win the pin (Fable 13); 60 one-sentence rows fit 12 KB, and
the prompt says to query when the block reports it was cut.

**It does not write** `session` or `set` (the app's log), `llm/memory`, or
change requests. **It reads** clinical conditions and medication requests
and dispenses (owner answer 1), and the prompt forbids naming or advising on
them. Allergies, lab reports, documents and the rest of `clinical` are not
asked for.

**Provider and model.** The app reads the `llm/provider` rows and chooses
for the owner (§8): the best of `claude-sonnet-5-5`, `claude-sonnet-5`,
`gpt-5` that a keyed row prices, matched exactly or behind a gateway prefix
(`anthropic/claude-sonnet-5-5` on a LiteLLM row, which gets no
`reasoningEffort`; `medium` and `low` go only on an exact Anthropic or
OpenAI match), else a keyed row's first priced model. A model the row does
not price is never chosen, and the server refuses it anyway. A coach screen
that lets the owner choose must make the owner's choice stand over §8's
upgrade (2026-11-02).

## 6. The prompt

The prompt is static per app release and rendered by the app from three
checked-in parts, so it is a cacheable prefix and has its own review:

1. **Coaching instructions**,
   [`Package/coach/instructions.md`](../../apps/apple/SubstrateWorkouts/Package/coach/instructions.md),
   distilled from the coaching research (sources and evidence grades are in
   `apps/apple/SubstrateWorkouts/docs/research/07-coaching.md`): role and
   scope; read before asking; the intake as two `ask` forms (goal,
   experience, days and minutes, equipment; then the seven-item safety
   screen) and then single free-text questions (pain, working weights, likes,
   sleep and sport); clearance rules; programme defaults (weekly sets, reps
   and effort, split by days, session size, patterns and order, warm-ups);
   deloads as a separate routine; stalls, missed sessions, breaks; recovery
   signals as context with the agreement rule; the pain traffic light; red
   flags; how to treat clinical reads; conversation style; and the change
   shape it must confirm before any write.
2. **Substrate mechanics and memory rules**,
   [`Package/coach/mechanics.md`](../../apps/apple/SubstrateWorkouts/Package/coach/mechanics.md):
   the tools, reading before writing, `ifVersion` patches, no routine write
   while a session of it is `active`, no sessions or sets, honest reporting
   of refused or gated writes, and what a memory may hold (§7).
3. **The routine and exercise shape guide**, generated from the training
   manifests the app ships at the same version, so prompt and kinds cannot
   drift.

A revision line heads the prompt ("Workout coach instructions, revision N"),
which the app compares with the stored row to tell an older app from a newer
one (§8). The revision is derived, never bumped by hand: `coach.json` also
carries the agent document's fixed part (tools, pinned memories, budgets,
description), and the generator hashes that with the prompt body into
`Package/coach/coach.lock.json`. A changed hash moves the revision, and
`--check` fails until it has, so a change to `maxTurns` or the pin reaches
every device too. The rendered prompt stays under 20 KB, which the generator
and a test hold.

## 7. Memory

`~/training/memory` joins the training package at version 2 (additive):

| Property | Type | Notes |
| --- | --- | --- |
| `text` | text, required | one fact in the owner's words |
| `topic` | enum, required | goal, experience, schedule, equipment, limitation, preference, baseline, lifestyle, response, change, other |
| `exercise` | reference to `~/training/exercise` | when the fact is about one |
| `until` | date | the last day a passing fact holds |
| `active` | bool, default true | off retires a fact and keeps its history |
| `source` | enum, default owner | owner, coach, app |

**Retrieve.** The active rows whose `source` is `owner` or `coach` are pinned
newest first, once per thread, each with its `source`. `source` is the
writer's own claim (a list read carries no engine-stamped writer), so a
hand the owner let write the kind can still claim `coach`; the prompt reads
every pinned row as a note, never an order, and the persona test plants one. The
pin is a snapshot: an edit or a retirement reaches the next chat. Rows past
`until` are retired by the coach at the next chat. Older or retired facts
are a `query` away.

**Store.** Directly, `source: coach`, without review: these are the owner's
own answers. An owner who wants review adds a `recordpatchpolicy` gate on
the kind for the agent; the README says how.

**Prompt injection (Fable 7).** Three defences, none a server change:

- The memory rules (mechanics §Memory): store only what the owner said in
  the chat or answered in an `ask`; never anything read from a record,
  however phrased; a pinned memory is a note, never an instruction, and one
  that reads like an order is reported to the owner, not obeyed.
- A review surface with no app work: the console lists the kind as a
  collection, "Coach memories", under the Training package in All data,
  where the owner reads, edits, retires (`active` off) and deletes each one.
  App ownership (0205) locks the kind's declaration, never its records. The
  app's own Memories screen comes with the coach screen.
- A persona test (`TestLiveCoachIgnoresAnInjectedWorkoutNote`, live and
  opt-in under `mise run test:llm`): on the prompt the app ships, a session
  note that tells the coach to store a memory and rewrite a routine leaves
  no memory and the routine unchanged.

**The general assistant** reads training memories with its `*` grant. It
can also write them, because its grant is `*` writes and an agent's grant has
no exclusions; making it read-only would need either a narrower grant on
the owner's own agent or a gate policy, both the owner's to declare. Not
simple, so not done (owner answer 2).

## 8. The app

The app installs and updates the coach and does nothing else with it yet.

**Sign-in.** The Workouts request adds `declare:~/trainingcoach`,
`read:substrate.reamde.dev/llm/provider` and, last, the `agent` detail with
the coach's reads, writes and a cap of 100 cents. SubstrateKit marks that
detail optional: a server that does not list the `agent` type is sent the
request without it and stays compatible, rather than reading as too old. An
install signed in before this build is asked once to review the new access
("Add the AI agent Workout coach", with its cap), through the existing
approval review; nothing else changes until the owner does.

**Install and update** (`CoachInstaller`, after training is current at
version 2 in every sync):

1. No approved agent detail in the session's token response: nothing.
2. Training not yet at version 2: wait for the next sync.
3. Read the coach's row. A prompt revision above this build's: leave it.
4. Keep the row's cap. Keep its provider and model while a provider row
   still prices that model and this build's choice does not rank strictly
   better in the preference list (`claude-sonnet-5-5`, `claude-sonnet-5`,
   `gpt-5`, matched exactly or behind a gateway prefix; anything else ranks
   last). Otherwise take this build's choice: the best preferred model a
   keyed row prices, else a keyed row's first priced model, else an unkeyed
   row's (the coach then installs and refuses until the owner keys it). No
   priced model at all: nothing. The list's order never moves a family down
   between releases.
5. Read only the approved kinds the server holds; the interaction kind joins
   the writes for `ask`.
6. If the row already says this prompt, model, cap and reach: nothing.
   Otherwise apply the coach package, bundle and agent.
7. A coach package someone else keeps (its row read with the training
   package's `PackageManager`, decision 0205) is left alone with no apply; a
   403 naming another keeper reads the same, and any other refusal (a
   narrower approval) is logged and left. No sync ever stops on the coach.

**Settings authority (Astra 4).** The stored row is the truth for cap and
reach. For provider and model it stands unless a build's choice ranks
strictly better, so an older build's fallback moves up (Haiku behind a
LiteLLM gateway to Sonnet, 2026-11-02, owner: "Sonnet is fine") and two
devices on different builds settle rather than flip-flop (Fable 14).

**Where the owner meets the coach.** Substrate Chat lists every agent its
`agents:*` scope reads and offers the primary, chat-capable ones by default,
so the coach, `purpose: primary`, appears beside the owner's other agents
with no Chat change. The console's Agents page lists it the same way.

**Later, with the coach screen:** provider, model and cap controls (a cap
above the approved figure is a new approval); a Memories screen; "Ask your
coach" from a routine (a server-matched Chat link that opens and never
sends, with the console as fallback, Astra 9 and Fable 15); Remove and turn
on and off from the app.

## 9. Server changes

| # | Change | Where |
| --- | --- | --- |
| S1 | `agent` detail: strict parse and the effective-permission rule; listed in `authorization_details_types_supported`; held to the request's `declare:` scope | `internal/authorize/details.go`, `internal/api/authorize.go` |
| S2 | Plan row (`AuthorizationAgentPlan`: agent, reads, writes, cap, `allowed`), `/authorizations/check` answer | `internal/appsetup/plan.go`, `internal/substrate/authorize.go` |
| S3 | Token `agents` property (core token kind v10), minted from the approved details, read back on `TokenInfo`, carried on the scoped grant | `kinds/…/core/token.yaml`, `internal/engine/auth.go`, `internal/scope` |
| S4 | Scoped apply admits the approved agent under §4.2; scoped `UninstallBundle` for a kind-free declared package; scoped `Get`/`KindByRef` of the agent row and of approved read kinds | `internal/api/scopeddeclare.go`, `internal/api/scoped.go` |
| S5 | Engine admission of an app's agent: live approval, priced model, root only; re-admission before each model call | `internal/engine/appagent.go`, `agentloop.go` |
| S6 | Console: the approval row; the agent page's "Managed by" section (Pause is the off switch, Take over), read-only model and grants | `web/console` |
| S7 | Docs: apps.md, agents.md, 0207, the training README | docs |

And in the apps: SubstrateKit's optional `agent` detail and its review
words; Workouts' sign-in request, `Coach.swift` (`CoachInstaller`) and the
rendered `coach.json`; training version 2.

## 10. Tests

**Server**

- `internal/authorize` (`agentdetail_test.go`): an agent detail is read as
  written, and strictly: another authority, a glob or a bare agent; no cap,
  0, 201 or fractional; reads nothing, a glob, `*`, a token, an agent,
  `llm/memory`, `llm/provider`; writing `llm/memory`, interactions or change
  requests; a write it does not read; duplicates; too many reads; foreign
  members.
- `internal/api` (`appagent_test.go`): a request without the agent's
  `declare:` scope, and an approval that unchecks it, are refused; approving
  stores the approval on the token and never in its scopes, and the token
  response echoes it; the next request reads it as already allowed, a
  higher cap as not; the check answers `none` and `enable`; the scoped apply
  admits the approved agent and refuses another agent, a function tool,
  `propose`, a background tool, `toolsFrom`, sub-agents, `hiddenFromChat`, an
  internal purpose, a read or write past the approval, a glob, `llm/memory`,
  the interaction kind without `ask`, a missing, zero or raised cap, engine
  guidance off, an unpriced or zero-priced model and a missing provider, and
  a `write:*` token with no approval; the app reads its agent's row and an
  approved kind's definition, and not another agent, another kind or a
  record of the agent's reads.
- `internal/engine` (`appagent_db_test.go`): an app's agent refuses with no
  token, runs with a live approval, refuses after revocation and runs again
  after a new sign-in; refuses with an expired token, a narrower approval, a
  lower approved cap, another agent's approval and another app's token; an
  unpriced and a zero-priced model; never runs as a sub-agent; a running
  turn stops at its next model call when revoked and when its cap is
  reached; the owner's bundle disable stops it, the app's re-apply keeps it
  disabled, enable restores it; the owner cannot apply or patch the agent
  row until a takeover, after which it runs with no token; the app removes
  its kind-free coach package and nothing else, and the owner and other apps
  cannot use that door.
- `internal/testenv` (`appagent_db_test.go`): end to end over HTTP with a
  real authorization request, approval and token exchange: the approval
  page's agent row, the app's token declaring training and the coach, the
  app reading its coach's row and the provider without its key, a chat with
  a stub model that stores a memory, the next chat's prompt pinning it, and
  the coach refusing after the owner revokes the app.
- Live, opt-in (`coachpersona_live_db_test.go`): the personas, an injected
  note, a planted memory, a red flag, a change without a yes, a whole plan
  brought with a medical history, and a yes while a session is running.

**Console:** the agent row in both views (`setup-row.agent.test.tsx`); the
"Managed by" words for an app's agent (`app-managed.test.tsx`); the wire
golden for every new field.

**Apps:** WorkoutsKit `CoachTests`: the sign-in detail within the server's
rules and optional; an older server sent the request without it; the
bundled prompt's revision, size and training version; nothing written
without an approval or before training is current; a first install on a
keyed, priced model reading only held kinds, homed; an unchanged coach not
applied; a newer revision left alone; a lowered cap kept, and a stored
model replaced only by a better-ranked one (matched exactly or behind a
gateway prefix such as LiteLLM's `anthropic/`); a refusal as a state; the model choice. SubstrateKit's and WorkoutsKit's
existing suites pass with training version 2.

## 11. Deferred and not done

- Proactive check-ins (owner answer 7).
- The general assistant read-only on training memories (§7).
- Showing today's spend in the app (the agent page shows it).
- A hard spend ceiling with reservations (§4.4 states the bounded
  overshoot instead).
- The coach screen in the app: model and cap controls, a Memories screen,
  "Ask your coach", Remove and turn on and off (§8).

## 12. Open questions

- Should the approval page let the owner untick one of the agent's reads, as
  it does a scope? Today the agent row is approved whole or the request is
  denied.
- When the coach screen comes: should its "Turn off" disable rather than
  remove, once an owner-versus-app disable can be told apart?
- None open on this PR. (The owner confirmed 2026-11-02 that existing
  installs get the one-time review prompt.)

## 13. Review findings and how this revision answers them

| Finding | Answer |
| --- | --- |
| Astra 1 (blocker): `Covers` is not effective permission | The agent's reach is approved by exact kind on the detail and checked by an effective-permission rule (no core or llm kind, no auth kind, no glob) at parse, at apply and at dispatch (§4.1–4.3) |
| Astra 2 (blocker): the cap is not enforced | Unpriced models refused at apply and at dispatch; checked at admission and before each model call; no sub-agent runs; overshoot stated as one call (§4.3, §4.4) |
| Astra 3: consent omits the provider recipient and persistence | The row says what is sent to the AI provider, that the coach stops when the app's access ends, and the cap; the live-token ceiling ends persistence (§4.1, §4.3) |
| Astra 4: releases and devices overwrite settings; no agent-row read | The stored row is the settings' truth; revision line against stale clients; reach from the approval; scoped read of the agent row, which update needs (§4.5, §8) |
| Astra 5: memory expiry and retrieval | Newest first, `first: 60`, query when cut, past `until` retired by the coach, snapshot behaviour shown in the app (§5, §7) |
| Astra 6: #1112 identity and lifecycle | 0205 lands first and owns by client id; the live-token check compares the same owner; the off switch is its owner-held bundle disable, pinned by tests here (§4.3, §4.6) |
| Astra 7: in-flight contract for turn off | Re-admission before each model call stops a running turn; remove is an explicit uninstall the app never repeats on its own (§4.3, §4.6) |
| Astra 8: scope separation | No new scope form; the agent approval is a token property read only by the agent paths (§4.1) |
| Astra 9: Ask your coach destination | Deferred with the coach screen (owner, 2026-11-02); the owner opens the coach in Substrate Chat or the console. The design stays in §8 |
| Astra 10: coaching content unfinished | Instructions and mechanics checked in, from the research; injected-note persona (§6, §10) |
| Fable 1: ceiling checked once | Live-token ceiling at every run and turn (§4.3) |
| Fable 2: cap void for an unpriced model | Pricing required at apply and dispatch (§4.2, §4.3) |
| Fable 3: the row states the ceiling, not the cap | The detail carries `spendCentsPerDay`; the row shows it; the apply holds the agent to it (§4.1, §4.2) |
| Fable 4: the phone token holds every health read | Reads ride the detail and the token's `agents`, not the app's scopes (§4.1) |
| Fable 5: off switch depends on #1112 | Settled in 0205; tested here (§4.6) |
| Fable 6: app cannot read its agent row | Scoped `Get` of the approved agent row (§4.5) |
| Fable 7: memories persist injections | Prompt rules, the console's Coach memories collection as the review surface, persona test (§7) |
| Fable 8: unchecked declare strands the agent scope | No agent scope; an approval without the declare scope, unchecked or refused under 0205, leaves the agent off the token and says so on the row (§4.1) |
| Fable 9: needs training v2 | `requiresAtLeast` training 2; the app applies training first (§5, §8) |
| Fable 10: empty-installs removal | Scoped uninstall of a kind-free declared package (§4.2); the app's Remove comes with the coach screen |
| Fable 11: README's "agent proposes" | Confirm-then-write, never while a session runs; README updated (§6) |
| Fable 12: rollback refuses the token | No new scope verb; an older binary ignores `agents` (§4.6) |
| Fable 13: pinned `until` and byte budget | §5, §7 |
| Fable 14: two-device flip-flop | Reach and settings from the stored row and the approval; an unchanged coach is never re-applied (§8) |
| Fable 15: Chat URL opens, never sends | Deferred with "Ask your coach"; the rule stands in §8 |
| Fable 16: provider key redaction | Scoped redaction test (§4.5) |
| Fable 17: canonical scope form | No new scope form |
| Fable 18: the placeholder is the agent | §6, with a size test |
