# Plan: detached chat turns, thread titles, and a reasoning stream

Status: proposed 2026-10-17, written against `origin/main` at `6a9bcc2f`.
Four pull requests, one branch each, in the order below. It is a plan, not a
contract: the code that lands is the contract, and where the two differ the
code is right.

The work serves any chat client over substrate agents. Mneme v6 is the first
one, and nothing here names it: every change is to the agent loop, the
`substrate.reamde.dev/llm` kinds, the chat routes and the console, and any
client gets the same contract ([the client contract](#the-contract-for-clients)).

## The problem

A chat turn dies with its client. `postAgentChat`
(`internal/api/agents.go:44`) calls `ds.ChatAgent(r.Context(), …)`
(`internal/engine/agents.go:255`), and the loop's deadline context derives
from that request context (`internal/engine/agentloop.go:592`,
`context.WithDeadline(ctx, deadline)`). When the client's socket closes (a
phone backgrounds the page, a tab reloads), the model call aborts, the loop
reaches `l.settle(ctx, threadError, …)` on the canceled context, the settle
write fails, and the thread row stays `running` under its lease
(`leaseUntil` = deadline plus 30 s, `agentloop.go:982`). Until the lease
expires, a new message on the thread is refused with `409 conflict: thread …
already has a running turn` (`agentloop.go:1022`); after it, the sweep
settles the thread `error` with `interrupted: the agent run holding this
thread stopped …` (`agents.go:606`). There is no way to re-attach to a turn
that is still running, and no way to stop one.

Three smaller gaps sit beside it. `llm/thread` has no heading of its own, so
the console derives one client-side from the first user message
(`web/console/src/lib/agent-chat.ts:217`). The chat stream carries `thread`,
`delta`, `toolStarted`, `toolFinished`, `compacted`, `done` and `error`
(`internal/substrate/agent.go:57`) and no reasoning: `internal/llm` folds
only `text_delta` on the anthropic wire (`anthropic.go:741`) and
`delta.content` on the openai one (`openai.go:166`). And there is no
`stopped` status, so a turn ended on purpose would have to read as an error.

## Facts the plan relies on (verified in the tree)

- `runAgent` (`agentloop.go:511`) does admission, provider resolution,
  `buildTools`, `openThread` (thread row plus the user message row),
  `ds.runningThreads.Store`, the `thread` event, the loop, `settle`, then
  the `done` event (`agentloop.go:797`). `openThread` on a continued thread
  takes the lease in `claimThread` (`agentloop.go:991`) and refuses a live
  one with `substrate.ErrConflict`.
- Every chat refusal that must keep its own HTTP status (hidden agent,
  spend cap, lease conflict, missing thread, provider row) happens before
  the first event; the handler sends the `200` only on the first event
  (`api/agents.go:57`).
- The engine already runs loops with no client: a proposal decision resumes
  a thread through `ds.spawn` (`agentdecision.go:265`), whose context is the
  service's background context, canceled by `stopBackground` at shutdown
  with a 10 s drain (`background.go:11`). `spawn` refuses once shutdown has
  begun.
- `settleLostThreads` (`agents.go:614`) skips any thread id in
  `ds.runningThreads`, so a loop this process runs is never settled under
  it. `settleInterruptedThreads` at open settles every `running` thread.
- `context.WithoutCancel` is already the repo's tool for a write that must
  outlive its caller (`runner.go:522`, `dataset.go:956`, `idempotency.go:511`).
- The fake LLM server in `internal/engine/agents_db_test.go` scripts turns
  per model with `arrived` and `release` barriers (`fakeTurn`,
  `agents_db_test.go:36`), so a test can hold a model call open while the
  client leaves, attaches, or stops.
- `title` is a reserved property name
  (`internal/vocabulary/load.go:1320`, decision 0016): a kind with a
  heading declares its own property and a `displayTemplate` renders the
  built-in `title` from it. A template token takes alternatives,
  `{subject|agent}`, the first non-blank one rendering
  (`internal/vocabulary/template.go:11`). A stored row's title is
  rendered at write time and nothing in the boot upgrade re-renders it
  (no `title` reference in `shippedupgrade.go` or `reprojection.go`).
- Every write stamps a manager row per accepted property with the writing
  actor and its tier (`write.go:1267`, `property_managers`). Tiers govern
  mapping recompute only (`internal/substrate/tier.go`); a direct write by
  any actor overwrites. The human actors are `api`, `console` and
  `substratectl` (`substrate.HumanActors`).
- The boot upgrade runs, inside its transaction, the declaration
  projection, record conversions and the refs re-derivation for moved
  reference shapes; the FTS re-derivation runs behind the open and only
  over rows that carry a moved property (`reprojection.go:204`
  `movedProperties`, `filterOf`). Adding an optional string property moves
  no reference shape and names no existing row, so the in-transaction work
  is the declaration rows alone.
- `params.reasoningEffort` on `core/agent` (`kinds/substrate.reamde.dev/core/agent.yaml:98`)
  merges under the provider row's `defaults` (`mergeParams`,
  `agentloop.go:333`) into `llm.Params.ReasoningEffort`, which the openai
  wire sends as `reasoning_effort` (`openai.go:104`) and the anthropic wire
  as `output_config.effort` (`anthropic.go:270`).
- Pricing is data: `llm/provider.pricing[]` rows of `{model, inputPer1M,
  outputPer1M, contextWindow}` keyed by the model id as sent
  (`provider.yaml:128`), read into `providerConfig.pricing`
  (`agentloop.go:293`); a model with no row prices at zero
  (`charge`, `agentloop.go:852`).
- `go-openai` v1.43.0 already carries `reasoning_content` on stream deltas
  and on messages (`chat_stream.go:19`, `chat.go:111`), which is the field
  LiteLLM emits for a reasoning model.
- Records lists order by `updatedAt` (`internal/engine/query.go:888`); a
  settle patches the thread row, so `orderBy=updatedAt:desc` is already
  "most recently active first" (`agentRunsQueryOptions` uses it).

## Conventions that apply to every pull request

- **Decision records.** `origin/main` holds up to 0152, then 0165 and 0167.
  Other live branches hold 0153 to 0164 and 0166 (`feat/observations`) and
  0168 (`feat/slack-live-bundle`). The next free numbers are **0169, 0170
  and 0171**, assigned below. Before citing one, run `git fetch --prune &&
  mise run decisions:check` from the branch; if the check names another
  free number, take that one and update this plan's reference in the same
  commit. Add each record to the index table in
  `docs/decisions/README.md` with status `proposed`.
- **Changelog.** There are no changelog fragments. release-please writes
  `CHANGELOG.md` from the merged pull request titles
  ([docs/releasing.md](../releasing.md)), so the changelog entry is the PR
  title, a conventional commit with every route and kind reference in
  backticks, and the PR body says the rest. Each PR below names its title.
  Nobody edits `CHANGELOG.md`.
- **Kind versions.** A changed declaration under `kinds/` bumps its
  `data.version` (`mise run kinds:check` refuses the merge otherwise). PR 1
  bumps `thread` to 13; PR 2 bumps `thread` again (to 14 if PR 1 landed
  first, else 13) and `provider` to 13; PR 3 bumps `message` to 17; PR 4
  bumps `thread` once more. Each branch bumps from whatever `main` holds
  when it rebases.
- **Wire mirrors.** Any new or changed struct in `internal/substrate` is
  regenerated into the golden (`go test ./internal/substrate/ -run
  TestWireGolden -update`) and mirrored by hand in
  `web/console/src/lib/api/types.ts` or the `lib/api` module that owns it,
  with the `Shape<T>` map updated (`docs/testing.md`, "The wire drift
  guard").
- **Tests.** Engine behaviour is a `*_db_test.go` in `internal/engine`
  against the fake LLM; API behaviour is `internal/api/*_test.go` against
  the fake dataset (`internal/api` never imports `internal/engine`); wire
  adapters are `internal/llm/*_test.go` against `httptest` servers; console
  behaviour is vitest beside the file. While on a branch, run engine tests
  with `-run`; the full `mise run test:db:engine` runs once, from one tree,
  at the end (`AGENTS.md`, one database suite per machine).
- **Prose.** Docs under `docs/*.md` are held by `lint:docs` (no dead words:
  tenant, relationship, singleton, configType, edge, entity, integration;
  kinds named in full). Comments carry constraints, not narration.

## PR 1: detached chat turns

**Branch:** `feat/detached-agent-turns` (this worktree).
**Title:** `feat(engine): run chat turns detached with thread attach and stop verbs`
**Decision record:** `0169-a-chat-turn-runs-detached-from-its-client-and-its-thread-is-attached-by-id.md`
**Scope:** a chat turn runs to its own end whether or not a client stays;
any client attaches to a running turn by thread id and gets replay then
live; a client stops a turn; a stopped or shut-down turn settles cleanly
with a `stopped` status. The `POST …/chat` wire is unchanged.

### Design

**A detached run.** `ChatAgent` keeps its signature and does, in order:

1. Everything it does today up to admission: resolve the agent, refuse a
   hidden one, `admitCallable`, refuse an empty message. Also refuse with
   `substrate.ErrUnavailable` when `ds.svc.bg.stopping()` is true, so a
   shutdown never opens a thread it will not run.
2. `l, err := ds.prepareAgentRun(ctx, ag, in)` under the request context:
   the first half of today's `runAgent`, through `openThread` (which claims
   the lease or refuses `409`), `ds.runningThreads.Store` and the `thread`
   event. Every refusal that today reaches the client as its own status
   still does, because nothing has been detached yet.
3. Build the run context: `runCtx := context.WithoutCancel(ctx)` keeps the
   request's values (the lifecycle lease value, the spend admission,
   the actor) and drops its cancellation; `runCtx, cancel :=
   context.WithCancel(runCtx)`, handed to the hub (`hub.setCancel`, which
   applies a stop that came first); `context.AfterFunc(bg.ctx, cancel)`
   cancels it when the service background context ends, which is what
   shutdown does.
4. `ds.spawn("chat turn", func(context.Context) { l.run(runCtx); hub.finish(…); release() })`.
   `release` is the lifecycle fence's, so a bundle disable waits for the
   detached turn exactly as it waits for an attached one today. If `spawn`
   refuses (shutdown began between steps 1 and 4), run the loop inline on
   the already canceled run context, which settles the thread `stopped`
   with the shutdown reason, and return `ErrUnavailable`.
5. `return ds.AttachAgentTurn(ctx, l.threadID, emit)`: the request's own
   emit attaches to the hub. When the client leaves, the attach returns
   `ctx.Err()` and the handler returns; the turn keeps running.

`runAgent` becomes `prepareAgentRun` followed by `(l *agentLoop) run(ctx)`,
so the trigger, call, sub-agent and resume entries are unchanged in
behaviour: they call both on the same context, as today. `run` owns what
today sits after `openThread`: `compactAtOpen`, the deadline, the loop, the
spend `finish`, `runningThreads.CompareAndDelete`,
`invocationStarted`/`Ended`, `settle`, `recheckResolutions`, the `done`
event. Only a loop with `l.hub != nil` (set by `ChatAgent`) is detached.

**The hub.** New file `internal/engine/agentturnhub.go`, type `turnHub`, one
per detached loop, reachable through `ds.runningThreads` (the value is the
`*agentLoop`, which carries `hub`). `l.in.emit` is `hub.publish`.

- `publish(ev)` appends to the replay buffer and fans out to every attached
  subscriber without blocking the loop: each subscriber has a buffered
  channel (256 events); a subscriber whose channel is full is dropped and
  its attach ends with an `error` event `stream fell behind; attach again`.
- The buffer is bounded by `agentTurnReplayBytes = 4 << 20` (4 MiB), summed
  over each event's `text`, `args`, `output` and `error`. Past it the hub
  keeps what it has, sets `overflow`, and retains nothing further; live
  subscribers still receive everything. A later attacher gets the retained
  prefix, then one `{"kind":"truncated"}` frame, then live events. The
  persisted `llm/message` rows are the truth for anything the replay
  dropped, and a client that sees `truncated` re-reads them after `done`.
- `attach(ctx, emit) (*AgentResult, error)`: under the lock, snapshot the
  buffer and register; outside it, emit the snapshot (always starting with
  the `thread` event, which is kept apart from the ring and never dropped),
  then forward live events until the hub is `done` (return its result and
  error) or `ctx` ends (return `ctx.Err()`).
- `finish(result, err)` is called by `run` after the `done` or `error`
  event: marks done, closes every subscriber, and the loop deletes itself
  from `runningThreads` as today. The hub lives exactly as long as the
  loop; there is no cache of finished turns. An attach after that answers
  "nothing running".
- `stop(actor)` records who asked (`hub.stoppedBy`, the first stopper wins)
  and cancels the run context, the parent of the loop's deadline context.

**The loop under cancellation.** `run` tells a stop from a shutdown from a
deadline by what it finds when `l.complete` returns an error or a turn's
dispatches end (`l.stopReason`):

| run context ended | `hub.stoppedBy` set | settles as |
| --- | --- | --- |
| no, `lctx` hit its deadline | no | `overbudget`, `deadline exceeded` (today) |
| yes | yes | `stopped`, reason `stopped by <actor>` |
| yes | no (the shutdown) | `stopped`, reason `stopped: the server shut down during the turn` |

Partial output is kept: `complete` already receives every text delta
through `onDelta`; the loop also appends them to `l.partial` (reset per
turn), and a stopped turn writes an assistant row `{role: assistant,
content: <partial>}` before settling when the partial is non-empty. A turn
stopped inside a tool dispatch still writes the call's tool row once the
dispatch returns (its output is whatever the canceled call answered, and its
`changes` are whatever it committed), then settles `stopped` before the next
completion.

**Writes outlive the cancellation.** A detached loop writes its message rows
on `context.WithoutCancel(runCtx)`, so a stop or a shutdown that lands
between a dispatch and its tool row does not turn into a failed write that
leaves the thread `running`.

**Settle never runs on a canceled context.** `settle` and the
partial-output write of a detached loop run on
`l.settleContext(ctx)` = `context.WithTimeout(context.WithoutCancel(ctx),
30*time.Second)`. This is scoped to detached loops (`l.hub != nil`): a
triggered run keeps today's behaviour, where a settle that fails at
shutdown leaves the claim for the next open to rerun once
(decision 0151), and widening that is a separate decision.

**Lease and 409.** Unchanged and now correct: while a detached turn runs,
the thread is `running` with a live lease, a second `POST …/chat` on it is
`409 conflict`, and the client's right move is to attach or stop. After a
settle the status is no longer `running`, so `claimThread` admits the next
turn whatever the lease says. `settleLostThreads` keeps skipping ids in
`runningThreads`, so it never races a detached turn; a crashed process
leaves the thread for the next open's `settleInterruptedThreads` as today.

**Shutdown.** `stopBackground` cancels the background context, which
cancels every detached run context; the model call aborts, the loop
settles `stopped` within the 30 s settle budget, and `stopBackground`'s
10 s drain is what bounds the wait. A settle that misses the drain is a
thread the next open settles as interrupted, as today; say so in the
record. A chat request arriving after `stopping()` is `503 unavailable`.

**Single replica.** `runningThreads` and the hubs are process memory. A
repository has one writer, refused at open on a second process
(decision 0083), so every turn of a repository runs in the one process that
serves it, and the attach and stop verbs answer only there. Two replicas
behind one load balancer would be a change to 0083 first. Written into
0169.

### Wire contract

Unchanged: `POST /api/v1/substrate.reamde.dev/core/agent/{name}/chat` with
`{"thread"?: "<id>", "message": "<text>"}`, `application/x-ndjson`, the
events below. New: the stream also carries the idle heartbeat `{}` every
30 s like the changes feed, and ends cleanly when the server drains (the
turn keeps running; the client attaches again after the restart finds it
settled).

New routes, verbs on the thread because the thread is the run and a
client holds its id, not its agent:

```
GET  /api/v1/substrate.reamde.dev/llm/thread/{id}/events
POST /api/v1/substrate.reamde.dev/llm/thread/{id}/stop
```

`GET …/events`: `200 application/x-ndjson`. If a detached turn is running:
the replay (the `thread` event first, then every retained event, then
`{"kind":"truncated"}` if the buffer overflowed), then live events, ending
with `done` or `error`. If nothing is running: one line
`{"kind":"idle","thread":"<id>","status":"<the thread row's status>"}` and
the stream ends. `status` is `running` when the thread is held by a turn
this process is not streaming (a triggered run, or a lease the sweep has
not yet expired), which tells the client to read the record rather than
wait. `404 not_found` when the thread record does not exist. Heartbeat and
drain as above.

`POST …/stop`: no body. Cancels the running detached turn, waits for its
settle, answers `200` with the settled `AgentResult` (the same object the
`done` event carries, `status: "stopped"`). `409 conflict` with message
`thread <id> has no running chat turn` when nothing this process is
streaming runs on it, including a triggered run (a trigger's turn ends with
its own budget). `404 not_found` for an unknown thread.

`AgentEvent` (`internal/substrate/agent.go`) changes:

```json
{"kind": "thread", "thread": "<id>"}
{"kind": "delta", "text": "…", "turn": 3}
{"kind": "toolStarted", "id": "call_0", "tool": "query", "callable": "function:…", "args": "{…}", "turn": 3}
{"kind": "toolFinished", "id": "call_0", "tool": "query", "callable": "function:…", "ok": true, "output": "…", "turn": 4}
{"kind": "compacted", "tokensBefore": 120000, "covered": 40}
{"kind": "truncated"}
{"kind": "idle", "thread": "<id>", "status": "ok"}
{"kind": "done", "result": {"reply": "…", "thread": "<id>", "status": "ok", …}}
{"kind": "error", "error": "…"}
```

- `turn` (new, `omitempty`) rides `delta`, `toolStarted`, `toolFinished`
  (and `reasoning` from PR 3): the `turn` ordinal the `llm/message` row for
  that event carries or will carry (`l.turn` at emit time). A client that
  re-attaches after reading the rows drops every replayed event whose
  `turn` is at or below the highest `turn` it already holds, which is exact
  because `turn` is the row's own ordering key. The `thread`, `compacted`,
  `done` and `error` events carry none.
- `status` (new, `omitempty`) rides `idle` alone.
- `AgentResult.status` gains the value `stopped`; `reason` says who or
  what stopped it.

New `substrate.Dataset` methods (`internal/substrate/dataset.go`, the fake in
`internal/api/fake_test.go` gets stubs):

```go
// AttachAgentTurn streams the running detached turn of threadID through emit,
// replay then live, and returns its settled result. ErrNoRunningTurn when no
// turn this process streams runs on the thread; ErrNotFound when the thread
// does not exist; ctx.Err() when the client left first.
AttachAgentTurn(ctx context.Context, threadID string, emit func(AgentEvent)) (*AgentResult, error)
// StopAgentTurn cancels the running detached turn of threadID on behalf of
// actor and returns its settled result. ErrNoRunningTurn, ErrNotFound as above.
StopAgentTurn(ctx context.Context, actor Actor, threadID string) (*AgentResult, error)
```

`substrate.ErrNoRunningTurn` is a new sentinel wrapping `ErrConflict`, so
`writeSubstrateError` already maps it to `409 conflict`; the `events`
handler tests for it to write the `idle` frame instead.

### Files and functions

- `internal/engine/agentloop.go`: split `runAgent` into
  `prepareAgentRun` and `(*agentLoop).run`; add `hub *turnHub` (on the
  invocation and the loop), `partial strings.Builder` and `opening` (the
  history `openThread` returned) to `agentLoop`; the run context's cancel and
  `stoppedBy` live on the hub; add `threadStopped = "stopped"`,
  `reasonStoppedBy`, `reasonShutdown`; `complete` appends deltas to
  `partial`; the loop classifies per the table (`stopReason`); add
  `settleContext`; the delta and tool events set `Turn`.
- `internal/engine/agentturnhub.go` (new): `turnHub`, `agentTurnReplayBytes`
  (a constant; the bound is tested on the hub directly, which sets its own
  `limit`).
- `internal/engine/agents.go`: `ChatAgent` as designed; add
  `AttachAgentTurn`, `StopAgentTurn`; `settleInterruptedAgentRuns` and
  `settleLostThreads` unchanged. `SweepResolutions` resumes `stopped`
  threads beside `ok` and `overbudget` ones (`agentdecision.go`).
- `internal/substrate/agent.go`: `AgentEventTruncated`, `AgentEventIdle`,
  `Turn`, `Status` on `AgentEvent`; `AgentResult.Status` doc lists
  `stopped`. `internal/substrate/errors.go`: `ErrNoRunningTurn`.
  `internal/substrate/dataset.go`: the two methods.
- `internal/api/api.go`: `llmPackage = "substrate.reamde.dev/llm"`; mount
  `r.Get("/"+llmPackage+"/thread/{id}/events", h.getThreadEvents)` and
  `r.Post("/"+llmPackage+"/thread/{id}/stop", h.postThreadStop)` beside the
  agent routes (five segments, so they never shadow the four-segment
  record path).
- `internal/api/agents.go`: `streamAgentEvents(w, r, start func(ctx,
  emit func(substrate.AgentEvent)) (*substrate.AgentResult, error)) (bool,
  error)` shared by `postAgentChat` and `getThreadEvents`: first event sets
  headers and `200`, events go through a channel so one writer interleaves
  them with the 30 s heartbeat and ends on `h.draining`; a refusal before
  the first event comes back to the caller. `getThreadEvents` answers
  `ErrNoRunningTurn` by reading the thread record and writing the `idle`
  line. `postThreadStop` calls `StopAgentTurn` and `writeJSON`.
  `heartbeatInterval` (`watch.go`) becomes a variable so a test can shorten
  it.
- `internal/api/discovery.go`: no new feature; the routes belong to
  `agents` (alpha). `discovery_errors_watch_test.go`'s roster gains the two
  routes if it enumerates agent routes.
- `kinds/substrate.reamde.dev/llm/thread.yaml`: `status` enum gains
  `stopped`; `reason` description mentions it; `version: 13`.
- `web/console/src/lib/api/agents.ts`: `AgentEvent.kind` union gains
  `truncated` and `idle`; `turn?`, `status?`; `streamChat`'s body reader
  becomes `readAgentStream(res, handlers)`; new `attachThread({thread,
  onEvent, onError, onDone})` (GET events) and `stopThread(thread):
  Promise<AgentResult>`.
- `web/console/src/lib/api/types.ts` and the golden: `AgentEvent`,
  `AgentResult` (no new struct; `stop` answers `AgentResult`).
- `web/console/src/components/agent/conversation.tsx`: when the open thread
  record is `running` and no stream is in flight, load the rows, then
  `attachThread`, feeding `onEvent` with replayed events filtered by
  `turn` against the highest persisted `turn` (pure helpers
  `maxPersistedTurn(rows)` and `isReplayed(event, maxTurn)` in
  `lib/agent-chat.ts`, tested); an
  `idle` frame with `status: running` shows "Working in the background"
  and keeps polling the thread record (the existing live invalidation
  hook); a Stop button in the header `actions` while `streaming`, calling
  `stopThread` (the stream's own `done` settles the overlay as today).
- `web/console/src/components/agent/thread-list.tsx`: a "Working" pill on
  rows whose `status` is `running`; `lib/agent-runs.ts` status words gain
  `stopped` ("Stopped"), and the agent page's status tone.
- `web/console/src/components/agent/composer.tsx`: the send button becomes
  Stop while a turn runs (same `stopThread`).

### Tests

Engine (`internal/engine/agentdetached_db_test.go`, new, using
`openAgentDataset` and `fakeTurn` barriers):

- `TestAChatTurnOutlivesTheClientThatStartedIt`: script one turn with
  `arrived` and `release`; call `ChatAgent` under a cancellable context;
  after `arrived`, cancel; `ChatAgent` returns `context.Canceled`; release
  the model; `AttachAgentTurn` with a fresh context returns `done` with
  `status: ok`; the thread row is `ok`, its assistant row is present, and
  `runningThreads` no longer holds it.
- `TestAttachReplaysTheRunningTurnThenStreamsLive`: script a tool-calling
  turn then a reply held on `release`; attach after the tool events; the
  attach's first events are `thread`, `toolStarted`, `toolFinished` with
  `turn` set, then the live deltas and `done`; a second attach after `done`
  returns `ErrNoRunningTurn`.
- `TestStopSettlesAChatTurnWithItsPartialOutput`: add a `fakeTurn.hold`
  barrier the fake server waits on after writing the first content chunk;
  `StopAgentTurn(ctx, ActorAPI, id)` returns `status: stopped`, `reason:
  stopped by api`; the thread row is `stopped`; the newest assistant row's
  `content` is the first chunk; a following `ChatAgent` on the thread is
  admitted (no `409`).
- `TestStopRefusesAThreadWithNoRunningTurn`: `ErrNoRunningTurn` on a
  settled thread and on a thread a triggered run holds.
- `TestAShutdownSettlesADetachedTurnStopped`: hold the model call, call
  `ds.svc.stopBackground`, assert the row settles `stopped` with the
  shutdown reason before the drain returns.
- `TestTheReplayBufferIsBounded` (`agentturnhub_test.go`, no database):
  a hub with a small `limit`, content past it, a late attach gets `thread`,
  the retained prefix, `truncated`, then `done`. Beside it: a live
  subscriber gets everything past the bound, a slow subscriber is dropped
  with `errAttachFellBehind`, and a stop before the run context exists
  still cancels it.
- `TestChatThreadSingleActiveTurn` (existing): extend with a detached turn
  holding the lease; a second `ChatAgent` is `ErrConflict`.
- `TestTheThreadStatusEnumAdmitsStopped`: a put of a thread with `status:
  stopped` validates (covers the kind bump).

API (`internal/api/agents_test.go`): `events` on a thread the fake reports
idle answers one `idle` line with the status; unknown thread `404`; `stop`
answers `200` with the result and `409` when idle; both streams carry the
no-buffer headers (`TestStreamsAskProxiesNotToBuffer` gains the events
route); the heartbeat `{}` appears on a chat stream held open past the
interval (shorten it through the existing `heartbeatInterval` var).

Wire: `TestWireGolden` regenerated; `wire.golden.test.ts` passes.

Console (vitest): `isReplayed` keeps events above the persisted turn and
drops the rest; `parseAgentEvent` admits `truncated`, `idle` and a numeric
`turn`; thread-list shows the running mark; conversation shows Stop while
streaming and calls `stopThread`.

### Docs

- `docs/agents.md`, "Calling an agent": the chat turn is detached; the two
  thread verbs with their answers; `turn`, `truncated` and `idle`; the
  `stopped` status and its reasons; the replay bound; the single-process
  note linking 0083 and 0169. "Threads, messages, and cost": `status` lists
  `stopped`.
- `docs/api.md`, the REST resources table at the top: the two routes.
- `docs/console.md`, "Agents": a running chat keeps running when the page
  closes and is picked up again on return; Stop.
- `docs/decisions/0169-…md`: context (the canceled-settle failure),
  options (keep the turn request-bound and shorten the lease; run the turn
  in a Postgres-backed queue so any process can serve it; run it detached
  in the serving process with an in-memory hub), outcome (the third,
  because 0083 already makes the process the repository's one writer and a
  queue would be a second runtime for one surface), consequences (a
  replica cannot attach; a crash still leaves the thread for the next open;
  memory per running turn is bounded at 4 MiB), confirmation (the tests
  above by name).

### Risks

- The `runAgent` split touches every entry; a missed `defer` (spend
  `finish`, `runningThreads` delete, `release`) leaks. Keep the
  non-chat entries calling `prepareAgentRun` then `run` in one function
  `runAgent` so their diff is mechanical, and run `TestAgentTriggerDispatch`,
  `TestAgentCallerContextEndingFirstStaysAnError` and the interrupt suite.
- A detached loop holds the lifecycle fence read lock for the whole turn
  (as an attached one did), so `DisableBundle` waits up to the agent's
  deadline. Unchanged exposure, now with no client to time out first.
- A proxy that buffers the `events` stream shows nothing until `done`;
  the no-buffer headers already cover nginx-style ingresses.

### Verify

```bash
mise run fmt && mise run lint && mise run fmt:check
go test -count=1 -run 'Detached|Attach|Stop|TestChatThreadSingleActiveTurn|TestAgentChatRoundTrip|TestAgentTriggerDispatch|TestAgentCallerContextEndingFirstStaysAnError|Interrupted' ./internal/engine/
go test ./internal/api/ ./internal/substrate/
go test ./internal/substrate/ -run TestWireGolden -update
mise run console:lint && mise run console:typecheck && mise run console:fmt:check && mise run console:test
mise run kinds:check && mise run decisions:check && mise run frozen:check && mise run commits:check
mise run test:race
mise run dev:restart   # then: POST …/chat, close the client mid-turn, GET …/thread/<id>/events, POST …/stop
mise run test:db:engine   # once, from one tree, before the PR
```

## PR 2: thread titles

**Branch:** `feat/thread-titles`
**Title:** `feat(llm): title a thread from its first message, then from its model`
**Decision record:** `0170-a-chat-thread-is-titled-by-its-first-message-then-by-its-model-and-an-owner-rename-holds.md`
**Scope:** a chat thread has a heading from the moment it opens, a model
replaces it with a short title after the first turn, the owner can rename
it and nothing overwrites a rename, and the console shows it.

### Design

**The property is `subject`, not `title`.** `title` is reserved on every
kind (`load.go:1320`, decision 0016): the built-in `title` is derived
storage rendered from a `displayTemplate`, and a declared `title` property
is refused. So `llm/thread` declares `subject` (string, optional, the word
the repo's other headed kinds use) and its template becomes
`{subject|agent}`: a thread with a subject renders it, and every other
thread (older rows, triggered runs) renders its agent's title, as the
`{name|participants}` precedent does. The old `"{agent}: {status}"` loses
the status suffix on purpose: a status in a heading reads as noise in a
chat list, and the row carries `status` beside it. Every client that reads
`record.title` therefore gets the heading with no change; a client that
wants the raw property reads `properties.subject`.

**Provisional at open.** `openThread`, minting a new thread in mode `chat`
with a user message, sets `subject` to `provisionalSubject(in.user)`: the
first non-blank line, whitespace collapsed, cut to 80 runes. Written in the
same transaction as the thread row, under the agent's actor (`l.actor`), so
the thread is never heading-less and its `subject` manager row is the
agent's. Other modes write none: a trigger's envelope is JSON and the
console already words those threads from the envelope ("When X changed").

**Generated after the first turn.** When a detached or attached chat turn
settles `ok` on a thread that had no replayed history (`l.historyLen ==
0`), `run` calls `ds.spawn("title thread", …)` after the `done` event,
never before, so the turn's stream and result are unaffected. The task:

- Model: `provider.titleModel` when the row sets one, else `l.model`.
  `llm/provider` gains `titleModel` (string, optional, "the model id on
  this row that titles threads; absent means the thread's own"). The
  justification: the row already keys pricing and the context window by
  model id, so a gateway row names a small model there and the call stays
  priced; a per-agent knob would be a second place to configure one model
  and most agents share a row. A title costs about 300 input and 15 output
  tokens: well under a tenth of a cent on a sonnet-class model, less on a
  small one.
- Request: `System` = "You name conversations. Reply with a title for the
  conversation: at most eight words, no quotes, no trailing period, in the
  conversation's own language." `Messages` = the user message and the
  assistant reply, each cut to 2000 runes. No tools. `MaxTokens` 32,
  `Temperature` nil, `ReasoningEffort` the merged params' (a model that
  needs the word said still gets it). Context: the background context with
  a 20 s timeout. The client is `l.client` (the wrapped one, so the live
  test's meter sees it).
- Result: first line, surrounding quotes stripped, whitespace collapsed,
  cut to 80 runes. Empty content, a `length` stop with no content, or an
  error: the task logs at `Warn` and the provisional subject stays. Nothing
  retries.
- Write: one `inTx` under the thread row lock. Load the row; read the
  `subject` manager from `property_managers` (the query shape at
  `mapping.go:1401`); if the row is gone, tombstoned, or the manager actor
  is not `l.actor`, skip. Else `patch` `subject` and add the call's
  `promptTokens`, `completionTokens`, `totalTokens` and `costUSD` (priced
  by the row's pricing for the title model) onto the row's tallies, the
  way `settle` and the compaction summarizer do, so `costUSD` stays the
  thread's whole cost. Actor `l.actor`.

**An owner rename holds by its manager row.** An owner renames with an
ordinary `PATCH …/llm/thread/{id}` `{"properties":{"subject":"…"}}` under
`api`, `console` or `substratectl`. That write stamps the `subject` manager
with the human actor, and the generator's "manager is still `l.actor`"
check is what tells a rename from the provisional value, whether the
rename landed before or after the model answered. No flag property, no
second write path, and the fact is visible on every read as
`propertyMeta.subject.manager`. A later turn never re-titles (the task
runs only when `historyLen == 0`).

**Reaching the client.** The generated title lands as a record write after
`done`. A client that wants it live tails
`GET /api/v1/changes?watch=1&kinds=substrate.reamde.dev/llm/thread` or
re-reads the thread shortly after `done`; the console's live invalidation
hook already re-reads records on changes, and the chats list re-renders
from `record.title`.

**Upgrade cost.** `subject` is a string with no reference shape, so the
boot upgrade's transaction projects the new declaration rows and converts
nothing (`convertedRecords=0`); the FTS pass runs behind the open and
names no existing row (none carries `subject`). The template change
re-renders no stored title: an existing thread keeps its stored
`"<agent>: <status>"` title until its next write re-renders it. Expected
in-transaction time: the declaration rows alone, seconds. The implementer
confirms it with the boot-upgrade test (`docs/testing.md`, "Testing the
boot upgrade") and quotes the `upgraded … took` line in the PR body. The 28
minute upgrade the owner saw is the shape this design avoids: an
in-transaction refs re-derivation over every row, which only a moved
reference shape triggers; nothing here adds a reference.

### Wire contract

No new route. `llm/thread` gains `subject` (string, optional); `record.title`
renders it. `llm/provider` gains `titleModel` (string, optional). A rename:

```http
PATCH /api/v1/substrate.reamde.dev/llm/thread/<id>
{"properties": {"subject": "Trip to Berlin"}}
```

### Files and functions

- `kinds/substrate.reamde.dev/llm/thread.yaml`: `subject` property (string,
  description "the thread's heading: the first message, then a model's
  title, then whatever its owner renames it to"), `displayTemplate:
  "{subject|agent}"`, version bump.
- `kinds/substrate.reamde.dev/llm/provider.yaml`: `titleModel`, version 13.
- `internal/engine/agentloop.go`: `provisionalSubject`, `openThread` writes
  it for mode `chat`; `run` spawns `l.titleThread` after `done`.
- `internal/engine/agenttitle.go` (new): `titleThread`, the system prompt
  constant, `cleanTitle`, the manager check, the tally patch.
  `providerConfig` gains `titleModel` (`resolveProvider`).
- `internal/engine/agents_db_test.go` `fakeLLM`: nothing new; the title
  call is one more scripted turn on the title model's queue.
- `web/console/src/lib/agent-chat.ts`: `threadTitle(thread, opening)`
  prefers `thread.properties.subject`, keeps the "When X changed" and
  first-line derivations for threads without one; `chatRows` passes the
  record. `pages/agents.tsx` and `conversation.tsx` header use it.
- `web/console/src/pages/agents.tsx` or `conversation.tsx`: a Rename action
  in the header that `patchRecord`s `subject` (console actor), inline
  input, Enter saves, Escape cancels.
- `docs/agents.md`, "Threads, messages, and cost": `subject`, how it is
  set, the rename rule, `titleModel`. "Providers": `titleModel`.
  `docs/console.md`, "Agents": chats are titled by the agent after the
  first reply; rename in the header. `docs/builtin-kinds.md` if it lists
  the llm kinds' properties.

### Tests

- `TestANewChatThreadOpensWithASubjectFromItsMessage`: `subject` equals the
  first line cut to 80 runes; `title` renders it; a `record` mode thread
  has no `subject` and titles as its agent.
- `TestTheFirstTurnTitlesTheThreadAndChargesTheRow`: script the title
  turn; after `done`, poll the row (bounded) for the model's title; the
  tallies grew by the title call's usage; `propertyMeta.subject.manager` is
  the agent actor; a second turn scripts no title call and the fake's
  queue proves none was made.
- `TestAnOwnerRenameIsNeverOverwritten`: rename under `ActorConsole`
  while the title call is held on `release`; release; the subject is the
  rename and the manager is `console`. And the reverse order: rename after
  the generated title; a further turn leaves it.
- `TestATitleCallThatFailsKeepsTheProvisionalSubject`: a scripted 500 and
  an empty reply both leave the provisional value and the row `ok`.
- `TestTitleModelOnTheProviderRowIsUsed`: with `titleModel: small`, the
  title request goes to `small`'s queue and is priced by `small`'s row.
- Boot upgrade: extend the shipped-upgrade test that opens a repository
  seeded by the previous binary's `kinds/` to assert the `thread`
  declaration lands at the new version with zero conversions.
- Console: `threadTitle` prefers the subject; the rename input patches
  `subject`.

### Risks

- The template change alters the rendered title of untouched threads only
  at their next write, so a list mixes old "`<agent>: <status>`" headings
  with new ones until then. Acceptable; say so in the PR body.
- A title call on the thread's own model after a long first reply costs
  the input of that reply; the 2000-rune cut bounds it.
- The spend cap (decision 0149) counts the thread row's `costUSD` in its
  window, so the title's cost is counted at the next admission, never
  refused mid-way; a title call never checks the cap itself.

### Verify

```bash
mise run fmt && mise run lint && mise run fmt:check && mise run kinds:check
go test -count=1 -run 'Subject|Title|Rename' ./internal/engine/
go test ./internal/vocabulary/ -run Template
mise run console:test && mise run console:lint && mise run console:typecheck
mise run decisions:check && mise run frozen:check && mise run commits:check
mise run dev:restart   # chat once; GET the thread; PATCH subject; chat again; GET again
mise run test:db:engine
```

## PR 3: reasoning stream

**Branch:** `feat/reasoning-stream`
**Title:** `feat(llm): stream the model's reasoning and keep a summary on the turn`
**Decision record:** `0171-a-reasoning-delta-is-its-own-event-and-a-turn-keeps-a-bounded-summary-never-the-chain.md`
**Scope:** when a wire streams thinking, the chat stream carries it as
`reasoning` events, the assistant `llm/message` row keeps a bounded
summary and the time spent, and the console shows "Thought for 12 s" with
the text behind a disclosure. A model with thinking off changes nothing.

### Design

**The contract.** `llm.Client.Complete`'s callback becomes
`onDelta func(Delta)` with `type Delta struct { Text, Reasoning string }`,
exactly one of them set per call. `llm.Result` gains `Reasoning string`,
the whole reasoning text as the wire returned it, so a one-shot call (no
client attached) still has it. Both adapters, the loop, the compaction
summarizer (`nil`) and every test fake follow.

**The openai wire (LiteLLM and every gateway on it).** In `stream`, a chunk
whose `delta.reasoning_content` is non-empty calls
`onDelta(Delta{Reasoning: …})` and appends to `reasoning`; `oneShot` reads
`message.reasoning_content`. `go-openai` v1.43.0 carries both fields
already.

**The anthropic wire.** `fold` learns the `thinking` and
`redacted_thinking` block types: `content_block_start` appends the block,
`thinking_delta` appends to the block's text and calls `onDelta`,
`signature_delta` is stored on the block (`anthropicContent.Signature`),
`redacted_thinking` carries `data` and no text. `anthropicResult`
concatenates thinking text into `Result.Reasoning`. The native wire sends
no `thinking` request key today, so no block arrives until a row or agent
asks for thinking; that request key is out of this PR (below).

**The loop.** `complete` routes `Delta.Text` to the `delta` event and
`l.partial` as before, and `Delta.Reasoning` to a new `reasoning` event
and `l.reasoning` (a bounded builder, 4000 runes, the first ones kept).
It records `l.reasoningStart` at the first reasoning delta and
`l.reasoningEnd` at the last; `reasoningMillis` is their difference, or,
with no deltas (one-shot) absent. After the completion, the assistant row
written for the turn (both the tool-calling and the final one) carries
`reasoning` (the bounded text) and `reasoningMillis` when there was any.
The reasoning is never replayed to the model on a later run: `replayView`
ignores the property, and compaction's summarizer never sees it.

**Why a bounded summary and not the chain.** The row is an audit and a
transcript, both read by people; the raw chain is large (thousands of
tokens per turn), may be redacted by the provider, and would be replayed
nowhere. 4000 runes is enough for a disclosure to read; a client that needs
every word keeps the live events.

**Thinking blocks inside the tool loop.** When thinking is on and a turn
calls tools, Anthropic requires the assistant turn passed back on the next
request to carry its signed thinking block ahead of `tool_use`; a request
without it is refused. Through LiteLLM the equivalent is the assistant
message's `thinking_blocks`. Whether LiteLLM tolerates its absence on the
openai wire must be verified live against the gateway the client uses
(`mise run test:llm` covers the real openai and anthropic endpoints only):
run an agent with one tool and `reasoningEffort: medium` on an
`anthropic/…` model through a LiteLLM row and read the second completion's
status. Two outcomes:

- It is accepted: nothing more to do in this PR.
- It is refused: the in-run tail must carry the provider's opaque blocks.
  `llm.Message` gains `ReasoningBlocks json.RawMessage` (never persisted,
  never replayed across runs); the anthropic adapter fills it from the
  thinking blocks (with signatures) and emits them back verbatim ahead of
  the turn's text and tool blocks; the openai adapter needs the raw
  `thinking_blocks` array, which `go-openai`'s stream types drop, so
  `openai.go` reads the SSE stream itself (the shape `anthropic.go` took in
  #946) and marshals its own request messages. That is a second, larger
  step; land the stream and the summary first and open the passthrough as
  its own PR with the live evidence in its body.

**Enabling thinking on the native anthropic wire** (the `thinking` request
key, `{"type": "enabled", "budget_tokens": N}` or adaptive) is out of this
PR: it needs the passthrough above and a declared knob. Note it in 0171 as
the trigger for revisiting.

### Wire contract

```json
{"kind": "reasoning", "text": "…", "turn": 3}
```

`reasoning` events arrive before the turn's `delta` events, interleaved
only as the provider interleaves them. `llm/message` assistant rows gain
`reasoning` (text, `fts: false`, at most 4000 runes) and `reasoningMillis`
(int, `min: 0`). Both absent when the model streamed none.

### Files and functions

- `internal/llm/llm.go`: `Delta`, `Client.Complete(ctx, req, onDelta
  func(Delta))`, `Result.Reasoning`.
- `internal/llm/openai.go`: `stream` and `oneShot` as above.
- `internal/llm/anthropic.go`: `anthropicContent.Signature`, `Data`;
  `fold` cases `thinking_delta`, `signature_delta`; `anthropicResult`.
- `internal/llm/anthropic_test.go`, `openai_test.go`, `live_test.go`,
  `internal/engine/agents_live_db_test.go` (the wrapper): the new callback
  type; `fakeTurn.reasoning` in `agents_db_test.go` makes the fake emit
  `reasoning_content` deltas before the content (streaming) or on the
  message (one-shot).
- `internal/engine/agentloop.go`: `complete`, the two assistant-row writes,
  `l.reasoning`, `reasoningStart/End`, `agentReasoningRunes = 4000`.
- `internal/engine/agentcompaction.go:671`: `nil` callback type.
- `internal/substrate/agent.go`: `AgentEventReasoning = "reasoning"`.
- `kinds/substrate.reamde.dev/llm/message.yaml`: `reasoning`,
  `reasoningMillis`; `version: 17`.
- `web/console/src/lib/api/agents.ts`: `reasoning` kind;
  `lib/api/transcript.ts`: the assistant turn view carries `reasoning` and
  `reasoningMillis` from the row or from live events;
  `components/agent/message-text.tsx` or a new
  `components/agent/reasoning.tsx`: a disclosure "Thought for 12 s" (live:
  "Thinking…" with a running count) above the assistant text, the text
  inside, collapsed by default; `conversation.tsx` `onEvent` handles
  `reasoning` like `delta` into the overlay's assistant turn.
- Docs: `docs/agents.md` ("Calling an agent": the event; "Threads,
  messages, and cost": the two properties; "The manifest": `reasoningEffort`
  now also decides whether reasoning streams on wires that honour it);
  `docs/console.md` ("Agents": the disclosure).

### Tests

- `internal/llm`: anthropic stream with a `thinking` block, a
  `signature_delta`, a `redacted_thinking` block, then text and a tool
  call: `Reasoning` holds the text, deltas arrive in order, the signature
  is kept on the block; openai stream and one-shot with `reasoning_content`;
  a stream with none calls no reasoning delta and leaves `Reasoning` empty.
- `internal/engine`: `TestAReasoningModelStreamsItsThinkingAndTheRowKeepsASummary`
  (events `reasoning` before `delta`, both with `turn`; the assistant row's
  `reasoning` and `reasoningMillis > 0`; a replayed second request carries
  no reasoning text); `TestAModelWithoutReasoningWritesNoReasoningFields`;
  `TestReasoningIsBoundedOnTheRow` (a scripted 10,000-rune reasoning keeps
  4000).
- Console: the disclosure renders from a row and from live events; a turn
  without reasoning renders none.
- Wire golden regenerated for `AgentEvent`.

### Risks

- The `Complete` signature change touches every adapter test; mechanical
  but wide. Do it in one commit on its own.
- The LiteLLM tool-loop refusal above. Verified live before the PR is
  opened, with the result in the PR body.
- Reasoning deltas inflate the replay buffer of PR 1; they count toward
  its 4 MiB like text.

### Verify

```bash
mise run fmt && mise run lint && mise run fmt:check && mise run kinds:check
go test ./internal/llm/
go test -count=1 -run 'Reasoning' ./internal/engine/
go test ./internal/substrate/ -run TestWireGolden -update
mise run console:test && mise run console:lint && mise run console:typecheck
mise run test:llm            # once; spends under a cent; confirms both real wires still fold
mise run decisions:check && mise run frozen:check && mise run commits:check
mise run test:db:engine
```

## PR 4: small things a chat list needs

**Branch:** `feat/chat-list-facts`
**Title:** `feat(llm): keep a thread's last reply and order chats by last activity`
**Decision record:** none; nothing here binds code beyond itself.
**Scope:** two cheap, generic facts for a thread list, and the console
using them.

- **Order by last activity exists.** `orderBy=updatedAt:desc` on
  `GET /api/v1/records` with `filter.kinds` =
  `substrate.reamde.dev/llm/thread` lists threads most recently settled
  first, because every settle patches the row. Document it in
  `docs/agents.md` beside the chat route, and switch the console's
  `conversationsQueryOptions` from `startedAt:desc` to `updatedAt:desc` so
  a continued chat rises.
- **`lastReply` on the thread.** `settle` writes `lastReply`: the final
  tool-free reply cut to 240 runes, when there is one; a `stopped` turn
  writes its partial text. A list shows a preview from the thread rows
  alone, with no second read over messages. `llm/thread` version bump;
  `AgentResult` unchanged (it already carries `reply`).
- Tests: `TestSettleKeepsTheLastReplyOnTheThread` (ok, overbudget with an
  empty reply leaves the previous value, stopped keeps the partial).
  Console: the chats list shows the preview under the title.
- Docs: `docs/agents.md` "Threads, messages, and cost".

Out of scope, named so nobody re-derives them: unread marks and pins (a
client's own state, `consolepreference`-style, not a thread fact); a
per-thread message count (`count=1` on a filtered messages list already
answers it); listing every thread's newest message in one read (the
`thread: {in: [...]}` filter on `llm/message` already does it for a page of
ids, and `lastReply` covers the list case); push delivery of a turn's end
to a backgrounded phone (a client concern over the changes feed).

## Answers to the two checks

**`params.reasoningEffort` through LiteLLM.** Yes, today. The agent
declares it (`core/agent.yaml:98`, enum `none, minimal, low, medium, high,
xhigh, max`), `mergeParams` merges it under the provider row's `defaults`,
and the openai wire sends it verbatim as `reasoning_effort`
(`openai.go:104`), which LiteLLM maps onto Anthropic's thinking for an
`anthropic/…` model. The row's `defaults.reasoningEffort` can set it for
every agent on the row. Nothing in this plan changes that.

**Pricing for a new model.** `costUSD` is right only when the provider
row's `pricing` carries a row for the model id exactly as the agent sends
it (`anthropic/claude-sonnet-5-5` on a LiteLLM row). A missing row prices
the turn at zero and the thread's `costUSD` stays 0. It is data: a `PATCH`
of the `llm/provider` record adding
`{model, inputPer1M, outputPer1M, contextWindow}` to `pricing`, no release.
`contextWindow` also gates compaction, so set it.

## The contract for clients

What a chat client codes against after PRs 1 to 4. Paths are under
`/api/v1`, every request carries `Authorization: Bearer <token>`, every
kind is spelled in full, and an agent name in a path has each `/` as `%2F`.

### Start or continue a turn

`POST /substrate.reamde.dev/core/agent/{agent}/chat` with
`{"thread": "<id>" | omitted, "message": "<text>"}`.

- Refusals come as an error envelope with their own status before any
  stream: `409 conflict` when the thread already has a running turn (attach
  to it or stop it), `403 guard` at a spend cap, `404 not_found` for an
  unknown thread, `422 validation` for a hidden agent, an empty message or
  a provider row that cannot resolve, `503 unavailable` while the server is
  shutting down.
- Otherwise `200 application/x-ndjson`, one JSON object per line. The turn
  keeps running if you close the connection: nothing is lost, and you
  re-attach by thread id.

### Attach to a running turn

`GET /substrate.reamde.dev/llm/thread/{id}/events`: `200
application/x-ndjson`. If a turn is running you get everything it has
emitted so far, then live events, ending in `done` or `error`. If nothing
is running you get one line `{"kind":"idle","thread":"<id>","status":
"<thread status>"}` and the stream ends; `status: "running"` there means a
turn no client can stream holds the thread (a triggered run), so read the
record and try later. `404 not_found` for an unknown thread.

### Stop a turn

`POST /substrate.reamde.dev/llm/thread/{id}/stop`, no body. `200` with the
settled result (`status: "stopped"`, partial reply kept) once the turn has
settled; `409 conflict` when no stoppable turn runs on the thread; `404
not_found` for an unknown thread. Any attached stream ends with the same
`done`.

### Events

Each line has `kind`. A line without `kind` (the `{}` heartbeat every 30 s)
is dropped.

| `kind` | fields | meaning |
| --- | --- | --- |
| `thread` | `thread` | always first; the thread id, minted or continued |
| `delta` | `text`, `turn` | a piece of the assistant's text |
| `reasoning` | `text`, `turn` | a piece of the model's thinking, before the turn's text; only when the model streams it |
| `toolStarted` | `id`, `tool`, `callable`, `args`, `turn` | a tool call began; `id` pairs it with its finish |
| `toolFinished` | `id`, `tool`, `callable`, `ok`, `output`, `turn` | the call ended |
| `compacted` | `tokensBefore`, `covered` | older turns were folded into a `summary` row |
| `truncated` | none | the replay could not carry everything; read the thread's `llm/message` rows after `done` |
| `idle` | `thread`, `status` | nothing is running (attach only); the stream ends |
| `done` | `result` | the turn settled; `result.status` is `ok`, `overbudget`, `error` or `stopped`, `result.reason` says why, `result.reply` is the final text |
| `error` | `error` | the turn failed after the stream opened; treat as settled with an error |

`turn` is the ordinal the matching `llm/message` row carries. When you
attach after reading the rows, drop every replayed event whose `turn` is at
or below the highest `turn` you already hold; everything else is new.

### The records you read

- A thread is `GET /substrate.reamde.dev/llm/thread/{id}`. Its `title` is
  the heading (the first message at first, a short model-written title
  after the first turn, or the owner's rename); `properties.status` is
  `running`, `ok`, `overbudget`, `error` or `stopped`; `properties.reason`
  says why a run ended early; `properties.lastReply` is a preview of the
  last reply; the token and `costUSD` tallies are the thread's whole cost.
- The chat list is `GET /records?filter={"kinds":["substrate.reamde.dev/llm/thread"],"properties":{"mode":{"eq":"chat"}}}&orderBy=updatedAt:desc&first=50`.
- A thread's transcript is `GET /records?filter={"kinds":["substrate.reamde.dev/llm/message"],"properties":{"thread":{"eq":"<id>"}}}&orderBy=turn:asc,createdAt:asc`.
  An assistant row carries `content`, and, when the model thought,
  `reasoning` (a bounded summary) and `reasoningMillis`; a tool row
  carries `toolCallId`, `name`, `ok`, `content` and the engine-stamped
  `changes`.
- Rename a thread: `PATCH /substrate.reamde.dev/llm/thread/{id}` with
  `{"properties":{"subject":"<title>"}}`. A rename is never overwritten.
- To learn a generated title without polling, tail
  `GET /changes?watch=1&kinds=substrate.reamde.dev/llm/thread` and re-read
  the rows it names.

### What a client should do on resume

Read the thread record. If `status` is `running`, open `…/events`: an
`idle` line with `status: "running"` means wait and re-read; anything else
is the live turn. If `status` is anything else, read the transcript rows;
there is nothing to attach to.
