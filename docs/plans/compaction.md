# Context compaction for the agent loop

Status: phase 1 is built on this branch, 2026-09-28, and not yet merged;
phases 2 and 3 are not built. Tracks issue #76. The storage choice is
[decision 0138](../decisions/0138-a-compacted-thread-keeps-its-rows-and-replays-a-summary-message-over-the-range-it-covers.md).

## The problem

A continued thread replays its whole prose history on every continuation
(`loadHistory`, `internal/engine/agentloop.go`). Nothing bounds that history:
the loop's budgets (`maxTurns`, `maxToolCalls`, `deadlineSeconds`) start at
zero on each continuation, and no code knows any model's context window. A
long chat thread costs more per turn as it grows, and a thread that outgrows
the window fails at the provider with a scrubbed string error the loop cannot
tell apart from any other error (`internal/llm/anthropic.go:57`,
`openai.go:54`).

Within one run the in-memory message list also grows with every tool result,
bounded only by `maxToolCalls` (cap 256) and the deadline. Those tool
exchanges are never replayed, so today they are lost to the next continuation
and can still overflow the current one.

Two threads readers exist besides the loop: the judge's `recentProseTurns`
(`judge.go:441`) and the console transcript. Three queries treat `role:
system` message rows as resolutions and resume the thread on them
(`recheckResolutions`, `SweepResolutions`, `systemRowsInLastHour`). A
summary cannot be a `system` row.

## What others do

Verified on 2026-09-28 against live docs and shallow clones under `/tmp`.

| System | Trigger | Kept verbatim | Summary stored as | Pending-state guard |
| --- | --- | --- | --- | --- |
| pi (badlogic/pi-mono) | `tokens > window - reserveTokens` (reserve 16384); also on provider overflow, retried once | `keepRecentTokens` (20000), cut only at user or assistant messages, never at a tool result | append-only `compaction` entry with `firstKeptEntryId`, `tokensBefore`, `usage`; rebuilt as a user message | omits the failed attempt with `context_edit` entries, keeps them in history |
| adk-go `session/compaction` (added 2026-08-31) | sliding window (`CompactionInterval`, `OverlapSize`) or tail retention (`TokenThreshold`, `EventRetentionSize`) | last N events plus the current question | one event with an `EventCompaction` range and summary; covered events dropped at prompt assembly | `longestSelfContainedPrefix` never cuts across an open call or confirmation; race check abandons the summary if rows landed inside the range |
| adk-python `EventsCompactionConfig` | same two strategies | same | event with `actions.compaction {start, end, content}` | fixed after issue 4740 removed a pending confirmation |
| Anthropic on-demand compaction (`compact-2026-09-04`) | client decides, separate request | whatever the client keeps after the block | signed `compaction` block with readable `content`; must lead `messages` byte-exact | 400 if the last assistant turn ends in an unanswered tool call |
| Anthropic context editing (`clear_tool_uses_20250919`) | `input_tokens` or tool-use count | last `keep` tool uses | nothing stored; server clears per request | none needed |
| OpenAI Responses `/responses/compact` | client, or `compact_threshold` | retained items | opaque `encrypted_content` item | undocumented |
| Claude Code | model window (about 967K on 1M models) | re-injects CLAUDE.md, up to five recent files, skills | one summary replaces the conversation | `PreCompact` hook can block |
| Codex CLI | 90% of window | last user messages up to 20000 tokens | summary as a user message | not verified |
| opencode | window minus reserve | 25% of window, clamped 2k to 15k | assistant message with `summary: true` | none |
| Gemini CLI | 50% of window | last 30% by characters, split at a user message | `<state_snapshot>` user message plus a canned model reply | will not compress past a trailing function call |

Chat Completions has no compaction feature at all, so an OpenAI-wire provider
always needs a client-side summarizer.

## Libraries: nothing to import

- adk-go's cut and range logic is under `internal/compactioninternal` and is
  typed on `genai.Content`. The public `Summarizer` interface is one method.
  adk-go still has no first-party Anthropic model, so only half of the
  re-evaluation trigger in decision 0008 has fired. The decision stands.
- cloudwego/eino's `summarization` middleware is typed on eino's schema and
  model adapters.
- langchaingo has trimming only. genkit Go has a history hook, no
  implementation.
- anthropic-sdk-go v1.73.0 added on-demand compaction (`BetaCompactionConfigUnionParam`,
  `BetaCompactionBlock`, `AnthropicBetaCompact2026_09_04`). The repo is on
  v1.63.1, which has context editing and the threshold edit type only.
- go-openai v1.42.0 has `CompactResponse` for the Responses API, with untyped
  output. Third-party OpenAI-wire gateways may not implement it.

The algorithm is small. The parts that matter here (records, the changelog,
row-level security, the lease, pending interactions) are the parts no library
can own. Build it in the loop, copying adk-go's cut rules and pi's prompt and
failure handling.

## Design

### The summary row

A new `llm/message` role `summary` (message kind v14). Properties:

- `content`: the summary text.
- `covers`: `{from, through}`, the ids of the first and last message rows the
  summary replaces. Ids, not `turn` ordinals, because ordinals can collide
  (`agentdecision.go:243`) and replay orders by `created_at, id`.
- `tokensBefore`: the context size in tokens that triggered the compaction.
- `model`, `promptTokens`, `completionTokens`: the summarizer call.
- `turn`, `thread` as every message row.

Written by `putMessage` under the agent's actor, through `inTx` and `t.put`,
so it lands in the changelog and replays on rebuild, and the self-trigger
exclusion still applies. The covered rows stay untouched: they are the audit.

A new role rather than a `system` row with a marker, because the three
resolution queries then need no change and cannot mis-resume a thread.

### Replay

`loadHistory` becomes two steps. `loadThreadRows` reads every message row of
the thread in `created_at, id` order, keeping each row's id and properties.
`replayView` turns those rows into the message list:

1. Index each `summary` row by the positions of its `covers.from` and
   `covers.through` in the row list. A summary whose ids are not both found
   is ignored, with one warning.
2. Walk the rows by position. Where one or more summaries start at the
   current position, emit the one created latest as one user message with
   pi's framing ("The conversation history before this point was compacted
   into the following summary:", then the text in `<summary>` tags) and
   continue after its `through`. Otherwise apply today's role filters. A
   summary row is never emitted at its own position.
3. Record, for each emitted message, the row it came from (`origin`), which
   cut selection needs.

A later summary's range starts at the earlier one's `from` (see cut
selection), so the latest summary at a position contains the earlier ones and
the jump past its `through` skips them.

Rows created between `covers.through` and the summary row (the kept tail, or a
resolver's `system` row landing during summarization) are after the range, so
they replay. That is the whole race guard: the range is explicit ids, and only
the lease holder writes non-system rows.

The judge's `recentProseTurns` filters on user and assistant roles, so it
ignores summary rows by construction. The console renders a summary row as a
collapsed "Earlier conversation compacted" line that expands to the summary
text.

### Window size and trigger

Add `contextWindow` (tokens) to each `pricing[]` entry on the `llm/provider`
row, next to the prices, and seed it for the shipped models. An unpriced or
unsized model never compacts.

Agent declaration, under `data.compaction`, added to `agentDataKeys` and
`core/agent.yaml`:

- `enabled` (default true)
- `reserveTokens` (default 16384)
- `keepRecentTokens` (default 20000)

The two token budgets are declared independently, so the loop clamps
`keepRecentTokens` to half of `contextWindow - reserveTokens`. A keep at or
above the usable window is a legal declaration under which every plan would
find the whole history inside the kept tail, and an overflow could never
recover.

Trigger: `contextTokens > contextWindow - reserveTokens`. `llm.Usage` gains
`ContextTokens`, the whole input the model saw: on the Anthropic wire
`input_tokens` plus cache read and cache creation tokens, which
`PromptTokens` leaves out (`anthropic.go:233`); on the OpenAI wire
`prompt_tokens`, which already counts cached tokens. `PromptTokens` stays as
it is, for cost. Nothing new is stored on the thread row. Where no usage
applies, the loop estimates four characters per token, plus four tokens per
message and the system prompt.

Three check points, as in pi:

1. At settle of a chat thread or a continuation, after the final assistant
   row is written and before `settle` patches the thread, so the
   summarizer's tokens land in the same tally patch. The size is the
   `ContextTokens` (else `PromptTokens`) of the run's FIRST completion, which
   measured the system prompt, the replayed history and the new user message
   exactly, plus the estimate of the final reply. Later completions in the run
   also carry tool exchanges that the next continuation does not replay.
   This is the primary point; it keeps the user's next turn fast. A failure
   is logged and the thread still settles `ok`.
2. At `openThread` of a continuation, if the estimate of the replayed history
   is over the threshold (a failed compaction at settle, or a thread written
   before compaction existed). On success the loop reloads the rows and
   rebuilds the history before the first completion; on failure it logs and
   continues uncompacted.
3. On `llm.ErrContextTooLong` from a completion during any run: compact over
   the current rows, rebuild the history view followed by the run's in-memory
   tail, and retry the completion once. The refused call is handed its turn
   back: the provider generated nothing for it, and counting it would end a
   run on its last allowed turn as `overbudget` with the compacted request
   never sent. A second overflow, or nothing to compact, settles `error` as
   today.

Before compacting at any check point the loop extends the lease to 60
seconds plus the lease slack from now, and the summarizer call runs under its
own 60-second timeout taken from the loop's base context, not the run's
deadline. After a compaction at open, whose time the run's deadline does not
include, the loop moves the lease past the whole run again. The summarizer call does not count against `maxTurns` and is
charged to the thread and the root tally at the provider row's prices.

Only chat threads and continuations replay, so check points 1 and 2 leave
trigger, call, sub-agent and judge runs alone.

### Cut selection

Work on the replay view (the message list `replayView` produces).
Walk back from the newest message adding estimated tokens until
`keepRecentTokens` is reached, then cut at the nearest `user` row at or after
that point. A user row starts a run, so the tail always begins at a run
boundary and never separates a question from its answer. If the walk reaches
the start without hitting the budget there is nothing to compact.

Open obligations: adk-go's rule (`internal/compactioninternal/window.go`,
`longestSelfContainedPrefix` and `skipBlockedHead`), applied to substrate's
rows. Walk every row of the thread in stored order, tool rows included, and
track a set of open obligations keyed by id:

- An assistant row's `toolCalls[].id` opens one. A `tool` row with that
  `toolCallId` closes it.
- A `tool` row whose `changes` lists an `llm/interaction` or a
  `recordpatchrequest` row opens one keyed by that record's path (fall back
  to the path in a `heldForReview` result). A `system` row whose envelope
  names that path with `interactionAnswered`, `interactionDismissed`,
  `proposalDecision`, `proposalConflicted` or `recordResolved` closes it.
  The resolver writes that row inside the resolving transaction, so the
  transcript alone is complete; the record's own `thread` field, bare id or
  full path, is never consulted.

A cut is valid only at a `user` row where the open set is empty. The
longest such prefix is summarized. If the window's head itself holds an open
obligation (a pending ask in the oldest run), do not stall: skip past the
head to the first boundary after a row that changed the open set, and
summarize the longest self-contained run after it, leaving the head raw. An
earlier summary that starts after the head is where that run begins, so it
folds into the new summary instead of stacking beside it. A
run that closes an obligation opened in the skipped head is refused, since
the model would otherwise see a question whose answer was summarized away.
No self-contained run means skip this compaction. This is the issue 4740 bug
class the plan already named.

Because a skipped head leaves an uncovered range before the summary, `covers`
stays a single contiguous `{from, through}` and replay emits the raw head,
then the summary, then the tail.

The next compaction's range starts at `covers.from` of the previous summary,
so the previous summary and its old tail fold into the new one (pi's fix
#2608; adk-go's tail retention does the same).

### The summarizer call

A bare `client.Complete` on the thread's provider and model, no tools, no
`systemSuffix`. Request: the covered messages serialized as text, one line per
message with `[User]`, `[Assistant]` and `[Substrate]` labels (for replayed
`system` envelopes), inside `<conversation>` tags, then the previous summary
in `<previous-summary>` tags when one exists, then the prompt. Each message
is capped at 2000 characters for the summarizer's input only, and the whole
transcript at 200000 characters (adk-go's caps).

Prompt: pi's checkpoint format (Goal, Constraints and preferences, Progress
with Done, In progress and Blocked, Key decisions, Next steps, Critical
context) plus adk-go's "Durable facts" section copied verbatim, with the
instruction that a fact present in a previous summary must be carried forward
unchanged and narrative dropped first. The update variant carries pi's
PRESERVE, ADD, UPDATE rules. Both keep pi's system prompt: "Do NOT continue
the conversation. ONLY output the structured summary."

Output cap: `max(256, 0.8 * reserveTokens)`. `llm.Result` gains a `Stop`
field (`end`, `length`, `toolCall`, `other`) filled by both adapters on the
one-shot and the streaming paths. A `length` stop, a tool
call, or empty content rejects the summary. A partial summary is never saved.

### Failure handling

- A failed summarizer call is non-fatal. The thread settles as it would have,
  the failure is logged with the thread id, and check point 2 retries at the
  next continuation.
- Provider overflow needs a typed error. Both adapters already rebuild errors
  as scrubbed strings; add an `llm.ErrContextTooLong` sentinel wrapped around
  the scrubbed message when the provider's text matches (pi's pattern list:
  `prompt is too long`, `prompt too long`, `context_length_exceeded`,
  `context length exceeded`, `exceeds the context window`, `maximum context
  length`, `too many tokens`, `input is too long`, `request too large`),
  excluding rate-limit texts (`rate limit`, `rate_limit`, `too many
  requests`, `429`, `throttl`).
- Compaction is idempotent per range: if a summary row already covers
  `through`, skip.

### Surfacing

- New `AgentEvent` kind `compacted` with `thread`, `tokensBefore` and
  `covered` (the number of message rows covered), mirrored in
  `web/console/src/lib/api/agents.ts` (pinned by `wire_test.go`).
- Console: collapsed block in the transcript; the summary text on expand.
- `docs/agents.md` "Threads, messages, and cost" gets a paragraph, and a
  `BREAKING CHANGE:` footer naming the new role and provider field.

## Phases

1. **Replay compaction** (issue #76, built): summary role, `covers` replay,
   `contextWindow` on providers, `data.compaction`, the settle-time,
   open-time and overflow checks, the typed overflow error with one retry,
   the summarizer with the prompt above, the `compacted` event, the console
   line. Tests: unit tests for the obligation walk, the cut and the replay
   view in `internal/engine/agentcompaction_test.go`, and database tests in
   `internal/engine/agentcompaction_db_test.go` on a scripted `fakeLLM`
   (compaction at settle and at open, a pending ask kept raw, a truncated
   summary rejected, an overflow retried once).
2. **In-run pressure**: clear old tool results in the in-memory list when the
   last `ContextTokens` crosses the threshold (keep the last 3 tool uses,
   replace older content with a one-line placeholder; rows untouched). On the
   Anthropic wire this can be `clear_tool_uses_20250919` server-side instead,
   which the current SDK already supports.
3. **Native summarizer on Anthropic** (optional): bump anthropic-sdk-go to
   1.73.0 or later, and let the Anthropic adapter summarize with on-demand
   compaction. Store the signed block on the summary row as `native` and its
   readable `content` as `content`, so a provider switch keeps the text.
   Not on Bedrock, not on Haiku 4.5. Only worth it once phase 1 is measured.

## Open decisions

- Should in-run tool work be summarized into a row that later continuations
  replay? Today tool exchanges are dropped from replay on purpose ("audit,
  not context"). A phase 2 in-run summary row would change that and carry
  tool findings forward. Recommend yes, as a separate decision record.
- Decided 2026-09-28 (decision 0138): the summarizer is the thread's own
  model (pi, adk-go). Add `compaction.model` for a cheaper one only if cost
  shows up.
- Decided 2026-09-28: open obligations follow adk-go, keyed on the record
  ids stamped in the transcript, so the `thread` field's two forms
  (`policy.go:452` full path, `agentloop.go:1434` bare id) do not matter.
- Whether `contextWindow` belongs on the provider row or an engine table keyed
  by model. Provider row keeps it as data next to prices, editable without a
  release.

## Sources

- pi: `packages/coding-agent/src/core/compaction/compaction.ts`,
  `docs/compaction.md` at commit c90d9ea5 (github.com/badlogic/pi-mono, now
  earendil-works/pi).
- adk-go: `session/compaction/`, `internal/compactioninternal/` at 967bfab
  (2026-09-28).
- Anthropic: platform.claude.com/docs/en/build-with-claude/compaction-on-demand,
  compaction-threshold, context-editing, compaction-keep-recent-turns.
- OpenAI: developers.openai.com/api/docs/guides/compaction.
- Claude Code: code.claude.com/docs/en/context-window, hooks.
- Decision 0008 and the retired `docs/plans/thread-interactions.md` (lines
  651 to 687 at `af352b62^`).
- Anthropic, "Effective context engineering for AI agents" (2025-09-29).
