/** The transcript as a reader sees it, folded out of the `llm/message` rows the
 * loop wrote (`internal/engine/agentloop.go`). The wire shape is a flat list of
 * turns — a `user` row, an `assistant` row carrying prose and/or its dispatched
 * `toolCalls`, and one `tool` row per dispatch keyed back by `toolCallId`. The
 * reader wants the tool row folded INTO the call that asked for it, so a card
 * shows its own request and its own response.
 *
 * Pairing is by ID, never by name: one turn may dispatch the same tool twice,
 * and pairing those by name settles the wrong card. The same id keys the live
 * stream's `toolStarted`/`toolFinished` events, so a card that is streaming and
 * the same card replayed off the records are the same card.
 *
 * This is pure: no queries, no components. It is where the transcript's rules
 * are tested. */

import {
  APPROVAL_KIND,
  CORE_PACKAGE,
  INTERACTION_KIND,
  INTERACTION_KIND_PRE_MOVE,
} from "@/lib/core-kinds"

import type { AgentMention } from "./agents"
import { readReference, type SubstrateRecord } from "./types"

/** One changelog entry a dispatch wrote, as the engine stamps it onto the
 * row's `changes` property: the seq addresses the delta in the changelog,
 * kind and id address the record it moved. */
export interface ChangeStamp {
  seq: number
  op: string
  kind: string
  id: string
}

/** What a sub-agent call's child chain wrote, as the engine stamps it onto the
 * call's tool row (`subagentWrites`): the child thread, how many distinct
 * records the child and every agent it called wrote, and the kinds, at most 20
 * with the rest counted. The entries themselves are on the child thread's own
 * rows. */
export interface SubagentWrites {
  /** The child thread's id. */
  thread?: string
  records: number
  kinds: string[]
  /** How many kinds the chain wrote past the listed ones. */
  moreKinds: number
}

/** One dispatched tool call, with whatever has settled about it so far. */
export interface ToolCallView {
  id: string
  /** The tool name the model saw: the agent's alias for the callable. */
  name: string
  /** The callable behind the name, as the engine stamps it on the rows, in
   * the actor spelling (`function:<authority>:<package>:<name>` or
   * `agent:<authority>:<package>:<name>`, decision 0025); the live tool
   * events carry the same string. Absent on a name the agent carried no tool
   * for, and on a row written before the stamp. */
  callable?: string
  /** The arguments the model emitted, verbatim — a JSON string, usually. */
  arguments: string
  /** The dispatch's result payload; absent while the call is still running. */
  output?: string
  /** Whether the dispatch succeeded; absent while it is still running. */
  ok?: boolean
  /** The changelog entries the dispatch wrote (engine-stamped, persisted rows
   * only — the live stream does not carry them, so they appear on handover). */
  changes?: ChangeStamp[]
  /** A sub-agent call's summary of what its child chain wrote, on the same
   * terms as `changes`: persisted rows only. */
  subagentWrites?: SubagentWrites
  /** A background call's job state: `working` until the job ends, then
   * `completed`, `failed` or `canceled`. The row's `job.state`, or the live
   * event's `job`; absent on a call that held the loop. */
  job?: string
  /** On a live card: the `turn` ordinal its tool row carries or will carry,
   * so the card is dropped once the stored rows hold that row. */
  turn?: number
}

/** What a compaction folded, as far as its `summary` row or its live
 * `compacted` event says. Every field is optional: the row and the event
 * carry different halves, and an older or partial row carries less. */
export interface CompactionView {
  /** The first and last message row ids the summary stands in for,
   * inclusive (the row's `covers`). */
  from?: string
  through?: string
  /** The context size, in tokens, that set the compaction off. */
  tokensBefore?: number
  /** How many message rows the summary covers (the live event's count; a
   * row names its range instead). */
  covered?: number
  /** The model that wrote the summary, and what writing it cost. */
  model?: string
  promptTokens?: number
  completionTokens?: number
}

export interface TurnView {
  /** Stable across a re-render and across the live→persisted handover. */
  key: string
  /** `system` is the substrate's own turn — a proposal decision the engine
   * wrote into the thread, never something a model said. `summary` is the
   * engine's fold of older turns: the model reads it in place of the rows it
   * covers, which stay in the thread as they were. */
  role: "user" | "assistant" | "system" | "summary"
  content: string
  tools: ToolCallView[]
  /** On a system turn: the changelog entries the decision wrote. */
  changes?: ChangeStamp[]
  /** On a user turn: the records it is about, the row's `contextRecords`
   * (decision 0196). The engine's rendered block itself is the model's, not
   * the reader's. */
  contextRecords?: ContextRecord[]
  /** On a summary turn: what it folded. */
  compaction?: CompactionView
  /** On an assistant turn: the model's thinking, as the row keeps it (at
   * most its first 4000 runes) or as the live `reasoning` events carry it.
   * Absent when the model exposed none. */
  reasoning?: string
  /** How long the model thought, in milliseconds, from its first piece of
   * reasoning to its last: the row's `reasoningMillis`, or on a live turn the
   * same span over the `reasoning` events that arrived. Absent on a row whose
   * completion streamed nothing. */
  reasoningMillis?: number
  /** On a live turn: when its first and its latest reasoning event arrived
   * (`Date.now()`). */
  reasoningStartedAt?: number
  reasoningLastAt?: number
  /** On a live turn: the latest event was reasoning, so the model is still
   * thinking and a reader counts the seconds. */
  thinking?: boolean
  /** On a run's final assistant turn: the records the reply links, as the
   * engine read them when the run settled. Absent when it links none. */
  mentions?: AgentMention[]
  /** On an assistant turn whose reply the model did not finish: how it was
   * cut short, the row's `cutBy` (`length`, `filter`, `stop`, `failure`). */
  cutBy?: string
  /** On a live assistant turn: which stored row its text will be, and how
   * much of that row's text and reasoning the stream has placed so far. */
  live?: LivePlace
  /** On a live user turn: the id the message was sent under, which is its
   * stored row's id. */
  sentId?: string
  /** On a live user turn sent while a turn was running (decision 0318):
   * `waiting` until the model is handed it, then `taken`. A waiting message
   * stays at the bottom, and what the run does meanwhile is drawn above
   * it. */
  queued?: "waiting" | "taken"
  /** On such a turn: its row's `turn` ordinal, once the server has said. */
  sentTurn?: number
}

/** Where a live assistant turn's text belongs: the `turn` ordinal its
 * events carry, and the Unicode code points of text and of reasoning the
 * stream has placed for that ordinal. A piece's `at` is counted the same
 * way, from 0 with each `turn`, so a piece is written where it belongs and a
 * replay written over what is shown changes nothing. */
export interface LivePlace {
  turn: number
  text: number
  reasoning: number
}

/** One record a user turn is about. */
export interface ContextRecord {
  kind: string
  id: string
}

/** A user row's `contextRecords`, or undefined where none rode it. */
export function contextRecordsOf(
  record: SubstrateRecord
): ContextRecord[] | undefined {
  const raw = record.properties.contextRecords
  if (!Array.isArray(raw)) return undefined
  const out: ContextRecord[] = []
  for (const entry of raw) {
    const e = entry as { kind?: unknown; id?: unknown } | null
    if (
      typeof e?.kind === "string" &&
      typeof e.id === "string" &&
      e.kind &&
      e.id
    )
      out.push({ kind: e.kind, id: e.id })
  }
  return out.length ? out : undefined
}

function str(value: unknown): string {
  return typeof value === "string" ? value : ""
}

function num(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined
}

/** A row's `mentions`, read tolerantly: an entry without a kind and an id
 * is dropped. */
export function mentionsOf(
  record: SubstrateRecord
): AgentMention[] | undefined {
  const raw = record.properties.mentions
  if (!Array.isArray(raw)) return undefined
  const out: AgentMention[] = []
  for (const entry of raw as unknown[]) {
    if (typeof entry !== "object" || entry === null) continue
    const m = entry as Record<string, unknown>
    const kind = str(m.kind)
    const id = str(m.id)
    if (!kind || !id) continue
    out.push({
      kind,
      id,
      text: str(m.text) || id,
      ...(m.missing === true ? { missing: true } : {}),
      ...(m.unread === true ? { unread: true } : {}),
    })
  }
  return out.length ? out : undefined
}

/** A summary row's own facts: its `covers` range, the context size that set
 * it off, and the summarizer's model and usage. Read tolerantly, like every
 * engine-written property here. */
export function compactionOf(record: SubstrateRecord): CompactionView {
  const p = record.properties
  const covers =
    typeof p.covers === "object" && p.covers !== null
      ? (p.covers as Record<string, unknown>)
      : {}
  return {
    from: str(covers.from) || undefined,
    through: str(covers.through) || undefined,
    tokensBefore: num(p.tokensBefore),
    model: str(p.model) || undefined,
    promptTokens: num(p.promptTokens),
    completionTokens: num(p.completionTokens),
  }
}

/** Whether a tool row reports a failure.
 *
 * The loop KNOWS the answer — it streams `ok` on the finished event — but does
 * not store it on the row, so a replayed transcript has only the payload to go
 * on: `toolError` writes `{"error": …}` and nothing else, and that envelope is
 * the evidence. It is read strictly (exactly one key, named `error`) so a
 * successful result that merely carries an `error` field is not mistaken for a
 * failed dispatch — but a failure whose payload is shaped differently reads as
 * a success on reload while the live card said `failed`. Persisting `ok` is
 * what closes that, and the declaration is read first here so it does the
 * moment the row carries one. */
export function toolOK(record: SubstrateRecord): boolean {
  const declared = record.properties.ok
  if (typeof declared === "boolean") return declared
  const content = str(record.properties.content).trim()
  if (!content.startsWith("{")) return true
  try {
    const parsed: unknown = JSON.parse(content)
    if (typeof parsed !== "object" || parsed === null) return true
    const keys = Object.keys(parsed)
    return !(keys.length === 1 && keys[0] === "error")
  } catch {
    return true
  }
}

/** The model-facing name of the `propose` host function: the name a `tools:`
 * entry takes when it aliases nothing (`vocabulary.AgentToolPropose`, the local
 * segment of `substrate.reamde.dev/core/propose`). */
const PROPOSE_TOOL = "propose"

/** The `propose` host function as a row's `callable` spells it. */
const PROPOSE_CALLABLE = "function:substrate.reamde.dev:core:propose"

/** The change-request kind, as a `changes` stamp spells it. */
const REQUEST_KIND = `${CORE_PACKAGE}/recordpatchrequest`

/** The interaction a settled ask landed, from the engine-stamped changes. A
 * stamp written before record 0077 moved the kind out of core still spells
 * the old reference, and a transcript read back years later must still find
 * its interaction. */
export function interactionIdOf(call: ToolCallView): string | undefined {
  const stamped = (call.changes ?? []).find(
    (c) =>
      (c.kind === INTERACTION_KIND || c.kind === INTERACTION_KIND_PRE_MOVE) &&
      c.op === "put"
  )
  return stamped?.id
}

/** The approval a call landed instead of running (decision record 0254),
 * from the engine-stamped changes. */
export function approvalIdOf(call: ToolCallView): string | undefined {
  return (call.changes ?? []).find(
    (c) => c.kind === APPROVAL_KIND && c.op === "put"
  )?.id
}

/** The engine-stamped `changes` of a row, or undefined where none rode it.
 * Read tolerantly: the property is engine-written, but the fold is pure and
 * a malformed entry must drop rather than throw. */
export function changesOf(record: SubstrateRecord): ChangeStamp[] | undefined {
  const raw = record.properties.changes
  if (!Array.isArray(raw)) return undefined
  const out: ChangeStamp[] = []
  for (const item of raw) {
    if (typeof item !== "object" || item === null) continue
    const entry = item as Record<string, unknown>
    const seq = typeof entry.seq === "number" ? entry.seq : undefined
    const kind = str(entry.kind)
    const id = str(entry.id)
    if (seq === undefined || !kind || !id) continue
    out.push({ seq, op: str(entry.op), kind, id })
  }
  return out.length ? out : undefined
}

/** The engine-stamped `subagentWrites` of a tool row, or undefined where none
 * rode it. Read as tolerantly as `changes`: a summary without a count is
 * dropped, and a malformed kind entry is skipped. */
export function subagentWritesOf(
  record: SubstrateRecord
): SubagentWrites | undefined {
  const raw = record.properties.subagentWrites
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) {
    return undefined
  }
  const entry = raw as Record<string, unknown>
  if (typeof entry.records !== "number") return undefined
  const path = readReference(entry.thread)?.path
  const thread = path ? path.slice(path.lastIndexOf("/") + 1) : ""
  const kinds = Array.isArray(entry.kinds)
    ? entry.kinds.filter((k): k is string => typeof k === "string" && !!k)
    : []
  return {
    ...(thread ? { thread } : {}),
    records: entry.records,
    kinds,
    moreKinds: typeof entry.moreKinds === "number" ? entry.moreKinds : 0,
  }
}

/** A minted record id (`engine.newID`): a UUIDv7 as 26 lowercase Crockford
 * base32 characters, or the twelve lowercase base32 characters ids were
 * minted as before decision 0228. Checked because the NAME alone cannot
 * settle what a call was. */
const MINTED_ID = /^(?:[0-7][0-9a-hjkmnp-tv-z]{25}|[a-z2-7]{12})$/

/** The change request a settled `propose` call landed, or undefined.
 *
 * THE NAME IS NOT PROVENANCE. The name is the agent's alias, so an agent that
 * aliases `{function: …/propose, name: file}` would get no link from it, and
 * one that aliases some other function TO `propose` would get one on any
 * payload that looked right. The stamped `callable` is the identity, and
 * where the call carries one it decides; a row written before the stamp has
 * only the name.
 *
 * The rest is checked as well: the call settled ok and the payload is
 * `{"id": <minted id>}` and nothing else, which is precisely what
 * `dispatchPropose` answers on success. A payload with a second key, or an id
 * no mint could have produced, is some other tool. */
/** The change request a settled call landed, settled from PROVENANCE where the
 * row carries it: an engine-stamped `changes` entry putting the request kind is
 * the fact the payload sniff below could only approximate — an aliased propose
 * gets its link, and an impostor payload gets none. Rows written before the
 * stamp existed (and the live overlay, which never carries `changes`) fall back
 * to the payload sniff. */
export function requestIdOf(call: ToolCallView): string | undefined {
  const stamped = (call.changes ?? []).find(
    (c) => c.kind === REQUEST_KIND && c.op === "put"
  )
  if (stamped) return stamped.id
  return proposedRequestId(call)
}

export function proposedRequestId(call: ToolCallView): string | undefined {
  if (call.ok !== true) return undefined
  if (
    call.callable
      ? call.callable !== PROPOSE_CALLABLE
      : call.name !== PROPOSE_TOOL
  )
    return undefined
  const payload = (call.output ?? "").trim()
  if (!payload.startsWith("{")) return undefined
  try {
    const parsed: unknown = JSON.parse(payload)
    if (typeof parsed !== "object" || parsed === null) return undefined
    const keys = Object.keys(parsed)
    if (keys.length !== 1 || keys[0] !== "id") return undefined
    const id = (parsed as Record<string, unknown>).id
    if (typeof id !== "string" || !MINTED_ID.test(id)) return undefined
    return id
  } catch {
    return undefined
  }
}

/** The declared calls on an assistant row: `toolCalls` is a json property, so
 * it arrives as whatever was stored. An entry with neither an id nor a name is
 * dropped — there is nothing to render and nothing to pair. One with a name and
 * no id IS kept, because its request is still worth showing, but nothing can
 * settle it: pairing is by id, so it stays open and its result (if any) arrives
 * as an unattributed card. */
function callsOf(record: SubstrateRecord): ToolCallView[] {
  const raw = record.properties.toolCalls
  if (!Array.isArray(raw)) return []
  const out: ToolCallView[] = []
  for (const item of raw) {
    if (typeof item !== "object" || item === null) continue
    const call = item as Record<string, unknown>
    const id = str(call.id)
    const name = str(call.name)
    if (!id && !name) continue
    const callable = str(call.callable)
    out.push({
      id,
      name,
      arguments: str(call.arguments),
      ...(callable ? { callable } : {}),
    })
  }
  return out
}

/** Fold the rows into turns. The rows arrive in loop order (`turn` ascending,
 * then created) — this preserves that order and never sorts, because `turn` is
 * the loop's own counter and two rows of one turn are ordered by creation. */
export function transcriptOf(
  messages: SubstrateRecord[],
  /** The calls a running turn's stream has settled, by call id: a stored
   * call whose result row is not read yet takes its result from here, so
   * the one card a call has shows what the stream already said. */
  live?: ReadonlyMap<string, ToolCallView>
): TurnView[] {
  const turns: TurnView[] = []
  // Every unsettled call in the transcript so far, so a tool row finds its
  // call even when another assistant turn has landed in between.
  const pending = new Map<string, ToolCallView>()
  // Every call so far, settled or not: a background job's end settles its
  // call a second time.
  const located = new Map<string, ToolCallView>()

  for (const record of messages) {
    const role = str(record.properties.role)
    if (role === "user" || role === "assistant") {
      const tools = role === "assistant" ? callsOf(record) : []
      for (const call of tools) {
        if (!call.id) continue
        pending.set(call.id, call)
        located.set(call.id, call)
      }
      const reasoning =
        role === "assistant" ? str(record.properties.reasoning) : ""
      const reasoningMillis =
        role === "assistant"
          ? num(record.properties.reasoningMillis)
          : undefined
      const mentions = role === "assistant" ? mentionsOf(record) : undefined
      const about = role === "user" ? contextRecordsOf(record) : undefined
      const cutBy = role === "assistant" ? str(record.properties.cutBy) : ""
      turns.push({
        key: record.id,
        role,
        content: str(record.properties.content),
        tools,
        ...(cutBy ? { cutBy } : {}),
        ...(about ? { contextRecords: about } : {}),
        ...(reasoning ? { reasoning } : {}),
        ...(reasoning && reasoningMillis !== undefined
          ? { reasoningMillis }
          : {}),
        ...(mentions ? { mentions } : {}),
      })
      continue
    }
    if (role === "summary") {
      // The engine's fold of older turns. It stands where the compaction
      // happened, and never joins an assistant turn: no model said it to
      // the reader.
      turns.push({
        key: record.id,
        role,
        content: str(record.properties.content),
        tools: [],
        compaction: compactionOf(record),
      })
      continue
    }
    if (role === "system") {
      // The substrate's own turn (a proposal decision, an answer, a
      // background job's end): its content is a self-describing JSON
      // envelope, and its `changes` say what it wrote. Never folded into a
      // call — nothing dispatched it.
      const turn: TurnView = {
        key: record.id,
        role,
        content: str(record.properties.content),
        tools: [],
        changes: changesOf(record),
      }
      turns.push(turn)
      // A job's end settles its call a second time: the call's own row keeps
      // the handle it answered with, and the result is here, so a background
      // search keeps its results on a re-read.
      const ended = jobNoticeOf(turn)
      const call = ended && located.get(ended.callId)
      if (ended && call) {
        if (ended.output !== undefined) call.output = ended.output
        call.ok = ended.ok
      }
      continue
    }
    if (role !== "tool") continue

    const output = str(record.properties.content)
    const ok = toolOK(record)
    // The tool row carries the same stamp as its call, so either side names
    // the callable.
    const callable = str(record.properties.callable)
    const call = pending.get(str(record.properties.toolCallId))
    if (call) {
      call.output = output
      call.ok = ok
      call.changes = changesOf(record)
      const job = jobStateOf(record)
      if (job) call.job = job
      const wrote = subagentWritesOf(record)
      if (wrote) call.subagentWrites = wrote
      if (callable && !call.callable) call.callable = callable
      pending.delete(str(record.properties.toolCallId))
      continue
    }
    // An orphan: a result whose call is not in this window (the assistant row
    // was compacted away, the id never matched, a duplicate result arrived).
    // It gets its OWN turn rather than being appended to whichever assistant
    // turn happens to be last — that turn did not make this call, and showing
    // it there would attribute a dispatch to the wrong one. Dropping it would
    // hide a dispatch that really happened, so neither.
    turns.push({
      key: record.id,
      role: "assistant",
      content: "",
      tools: [
        {
          id: str(record.properties.toolCallId),
          name: str(record.properties.name),
          ...(callable ? { callable } : {}),
          arguments: "",
          output,
          ok,
          changes: changesOf(record),
          subagentWrites: subagentWritesOf(record),
          ...(jobStateOf(record) ? { job: jobStateOf(record) } : {}),
        },
      ],
    })
  }
  // A call still waiting for its result row: the stream may have its result.
  if (live) {
    for (const [id, call] of pending) {
      const settled = live.get(id)
      if (!settled || settled.ok === undefined) continue
      call.output = settled.output
      call.ok = settled.ok
      if (settled.job) call.job = settled.job
    }
  }
  return turns
}

/** A background job's end, decoded from the system row the engine writes
 * when one finishes (internal/engine/agentjobs.go, event "toolCompleted"):
 * the call it answers, the name the model called it by, the callable
 * behind it, whether it worked and what it answered. */
export interface JobNotice {
  callId: string
  name: string
  callable?: string
  ok: boolean
  output?: string
}

export function jobNoticeOf(turn: TurnView): JobNotice | undefined {
  if (turn.role !== "system" || !turn.content.trim().startsWith("{"))
    return undefined
  try {
    const parsed: unknown = JSON.parse(turn.content)
    if (typeof parsed !== "object" || parsed === null) return undefined
    const env = parsed as Record<string, unknown>
    if (env.event !== "toolCompleted") return undefined
    const callId = str(env.toolCallId)
    if (!callId) return undefined
    const callable = str(env.callable)
    return {
      callId,
      name: str(env.name),
      ...(callable ? { callable } : {}),
      ok: env.ok === true,
      ...(typeof env.output === "string" ? { output: env.output } : {}),
    }
  } catch {
    return undefined
  }
}

/** An approval's outcome, as it reports into the conversation
 * (internal/engine/approvals.go, event "approvalDecided"): which approval,
 * whether the owner approved it, the tool and the agent, and, for an
 * approved call, whether it ran and what it answered or why it failed. */
export interface ApprovalNotice {
  approvalId: string
  decision: "approved" | "denied"
  tool: string
  agent?: string
  ok?: boolean
  error?: string
}

export function approvalNoticeOf(turn: TurnView): ApprovalNotice | undefined {
  if (turn.role !== "system" || !turn.content.trim().startsWith("{"))
    return undefined
  try {
    const parsed: unknown = JSON.parse(turn.content)
    if (typeof parsed !== "object" || parsed === null) return undefined
    const env = parsed as Record<string, unknown>
    if (env.event !== "approvalDecided") return undefined
    const decision = env.decision
    if (decision !== "approved" && decision !== "denied") return undefined
    const path = str(env.approval)
    const approvalId = path.slice(path.lastIndexOf("/") + 1)
    if (!approvalId) return undefined
    const agent = str(env.agent)
    const error = str(env.error)
    return {
      approvalId,
      decision,
      tool: str(env.tool),
      ...(agent ? { agent } : {}),
      ...(typeof env.ok === "boolean" ? { ok: env.ok } : {}),
      ...(error ? { error } : {}),
    }
  } catch {
    return undefined
  }
}

/** A system turn's decision envelope, decoded — `undefined` for a system turn
 * that is not one (or not parseable), which renders as plain content. The
 * shape is the engine's (internal/engine/agentdecision.go): event
 * "proposalDecision", the request's record path, the verdict, and — on an
 * accept — the target's path and new version. */
export interface DecisionNotice {
  /** The request's record id (the path's last segment). */
  requestId: string
  decision: "accepted" | "rejected"
  op: string
  /** The target's record path (`{kind}/{id}`), where the request named one. */
  target?: string
  /** The target's version after an accepted patch or create. */
  version?: number
  deleted?: boolean
  /** The owner applied their own values instead of the proposal's
   * (decision 0115): the envelope carries `adjustedDiff`. */
  adjusted?: boolean
}

/** A system turn's interaction resolution, decoded — how the transcript says
 * "the questions were answered" without rendering raw JSON. */
export interface InteractionNotice {
  interactionId: string
  event: "interactionAnswered" | "interactionDismissed"
  answers?: { question: string; selected: string[] }[]
}

export function interactionNoticeOf(
  turn: TurnView
): InteractionNotice | undefined {
  if (turn.role !== "system" || !turn.content.trim().startsWith("{"))
    return undefined
  try {
    const parsed: unknown = JSON.parse(turn.content)
    if (typeof parsed !== "object" || parsed === null) return undefined
    const env = parsed as Record<string, unknown>
    if (
      env.event !== "interactionAnswered" &&
      env.event !== "interactionDismissed"
    )
      return undefined
    const path = str(env.interaction)
    const interactionId = path.slice(path.lastIndexOf("/") + 1)
    if (!interactionId) return undefined
    const answers: InteractionNotice["answers"] = []
    if (Array.isArray(env.answers)) {
      for (const item of env.answers) {
        if (typeof item !== "object" || item === null) continue
        const a = item as Record<string, unknown>
        const question = str(a.question)
        const selected = Array.isArray(a.selected)
          ? a.selected.filter((v): v is string => typeof v === "string")
          : []
        if (question) answers.push({ question, selected })
      }
    }
    return {
      interactionId,
      event: env.event,
      answers: answers.length ? answers : undefined,
    }
  } catch {
    return undefined
  }
}

/** A triggered thread's opening message, decoded — the delivery envelope the
 * trigger wrote as the first user turn (`{change, record, repository}`,
 * internal/engine's trigger delivery). `undefined` for a user turn that is
 * plain chat, which renders as the message it is. */
export interface DeliveryNotice {
  /** What fired: the changelog entry the trigger matched. */
  change: {
    op: string
    kind: string
    id: string
    seq?: number
    actor?: string
  }
  /** Which record was delivered, with its display title where the snapshot
   * carried one. */
  record: {
    kind: string
    id: string
    title?: string
  }
}

export function deliveryNoticeOf(turn: TurnView): DeliveryNotice | undefined {
  if (turn.role !== "user" || !turn.content.trim().startsWith("{"))
    return undefined
  try {
    const parsed: unknown = JSON.parse(turn.content)
    if (typeof parsed !== "object" || parsed === null) return undefined
    const env = parsed as Record<string, unknown>
    if (typeof env.change !== "object" || env.change === null) return undefined
    if (typeof env.record !== "object" || env.record === null) return undefined
    const change = env.change as Record<string, unknown>
    const record = env.record as Record<string, unknown>
    const op = str(change.op)
    const changeKind = str(change.kind)
    const changeId = str(change.id)
    const recordKind = str(record.kind)
    const recordId = str(record.id)
    if (!op || !changeKind || !changeId || !recordKind || !recordId)
      return undefined
    const properties =
      typeof record.properties === "object" && record.properties !== null
        ? (record.properties as Record<string, unknown>)
        : {}
    return {
      change: {
        op,
        kind: changeKind,
        id: changeId,
        seq: typeof change.seq === "number" ? change.seq : undefined,
        actor: str(change.actor) || undefined,
      },
      record: {
        kind: recordKind,
        id: recordId,
        title: str(properties.title) || str(properties.name) || undefined,
      },
    }
  } catch {
    return undefined
  }
}

export function decisionNoticeOf(turn: TurnView): DecisionNotice | undefined {
  if (turn.role !== "system" || !turn.content.trim().startsWith("{"))
    return undefined
  try {
    const parsed: unknown = JSON.parse(turn.content)
    if (typeof parsed !== "object" || parsed === null) return undefined
    const env = parsed as Record<string, unknown>
    if (env.event !== "proposalDecision") return undefined
    const decision = env.decision
    if (decision !== "accepted" && decision !== "rejected") return undefined
    const request = str(env.request)
    const requestId = request.slice(request.lastIndexOf("/") + 1)
    if (!requestId) return undefined
    return {
      requestId,
      decision,
      op: str(env.op) || "patch",
      target: str(env.target) || undefined,
      version: typeof env.version === "number" ? env.version : undefined,
      deleted: env.deleted === true || undefined,
      adjusted:
        (typeof env.adjustedDiff === "object" && env.adjustedDiff !== null) ||
        undefined,
    }
  } catch {
    return undefined
  }
}

// ── the live overlay ────────────────────────────────────────────────────────

/** A run's turns while it streams, folded into the same `TurnView` shape the
 * persisted rows fold into. `closed` marks the assistant turn as finished with
 * its tools: the loop dispatches every call of a turn in sequence, so a
 * finished call does NOT end the turn — but the next prose delta does begin a
 * new one. */
export interface LiveOverlay {
  turns: TurnView[]
  closed: boolean
}

export const EMPTY_OVERLAY: LiveOverlay = { turns: [], closed: false }

// ── a message sent while the turn runs ─────────────────────────────────────

/** Whether a live turn is a message the model has not been handed yet. */
function isWaiting(turn: TurnView): boolean {
  return turn.role === "user" && turn.queued === "waiting"
}

/** Applies `change` to the overlay without its trailing waiting messages,
 * then puts them back under the result: whatever a run does while a message
 * waits (the rest of the reply that was streaming, its tool calls) happened
 * before the model read it, and is drawn above it. */
function aboveWaiting(
  live: LiveOverlay,
  change: (head: LiveOverlay) => LiveOverlay
): LiveOverlay {
  let cut = live.turns.length
  while (cut > 0 && isWaiting(live.turns[cut - 1])) cut--
  if (cut === live.turns.length) return change(live)
  const next = change({ turns: live.turns.slice(0, cut), closed: live.closed })
  return {
    turns: [...next.turns, ...live.turns.slice(cut)],
    closed: next.closed,
  }
}

/** Adds a message sent while the turn runs, waiting at the bottom, or, for
 * one the overlay already holds, notes the ordinal the server gave its
 * row. */
export function pushQueued(
  live: LiveOverlay,
  message: {
    id: string
    content: string
    turn?: number
    contextRecords?: ContextRecord[]
  },
  seq: number
): LiveOverlay {
  const at = live.turns.findIndex(
    (t) => t.role === "user" && t.sentId === message.id
  )
  if (at >= 0) {
    if (message.turn === undefined || live.turns[at].sentTurn !== undefined)
      return live
    const turns = [...live.turns]
    turns[at] = { ...turns[at], sentTurn: message.turn }
    return { ...live, turns }
  }
  return {
    ...live,
    turns: [
      ...live.turns,
      {
        key: `live-u${seq}`,
        role: "user",
        content: message.content,
        tools: [],
        sentId: message.id,
        queued: "waiting",
        ...(message.turn !== undefined ? { sentTurn: message.turn } : {}),
        ...(message.contextRecords?.length
          ? { contextRecords: message.contextRecords }
          : {}),
      },
    ],
  }
}

/** Marks a waiting message as handed to the model: it keeps its place, and
 * the reply that reads it is drawn under it. Every waiting message above it
 * was handed over with it: the model reads them in order. */
export function takeQueued(live: LiveOverlay, id: string): LiveOverlay {
  const at = live.turns.findIndex((t) => isWaiting(t) && t.sentId === id)
  if (at < 0) return live
  return {
    turns: live.turns.map((t, i) =>
      i <= at && isWaiting(t) ? { ...t, queued: "taken" } : t
    ),
    // The reply that reads it is a new turn, whatever the last one was doing.
    closed: true,
  }
}

/** Takes a message that the server never took out of the overlay. */
export function dropQueued(live: LiveOverlay, id: string): LiveOverlay {
  if (!live.turns.some((t) => t.queued && t.sentId === id)) return live
  return {
    ...live,
    turns: live.turns.filter((t) => !(t.queued && t.sentId === id)),
  }
}

/** The ids of the messages the overlay holds that were sent while the turn
 * ran. The overlay shows each where the run read it, so the stored row of
 * one is left out of the transcript while the overlay is up. */
export function queuedIds(live: LiveOverlay): Set<string> {
  const ids = new Set<string>()
  for (const t of live.turns) if (t.queued && t.sentId) ids.add(t.sentId)
  return ids
}

/** Whether the overlay holds a message the model has not been handed. */
export function hasWaiting(live: LiveOverlay): boolean {
  return live.turns.some(isWaiting)
}

/** A reply or thinking of ordinal `turn` belongs to a call that read every
 * message whose row's ordinal is below it, so those are taken, whether or
 * not the event that says so arrived (an attach whose replay was cut). A
 * tool event says nothing of the kind, and is never passed here. */
function readThrough(live: LiveOverlay, turn: number | undefined): LiveOverlay {
  if (turn === undefined) return live
  const read = live.turns.filter(
    (t) => isWaiting(t) && t.sentTurn !== undefined && t.sentTurn < turn
  )
  return read.reduce((next, t) => takeQueued(next, t.sentId ?? ""), live)
}

/** Text or a tool call after reasoning: the model is no longer thinking. */
function doneThinking(turn: TurnView): TurnView {
  return turn.thinking ? { ...turn, thinking: false } : turn
}

/** Where a streamed piece belongs: the `turn` and `at` its event carries.
 * Absent from a server that does not send positions, where a piece is
 * appended to the turn in flight. */
export interface Placement {
  turn?: number
  at?: number
}

/** How many Unicode code points a string holds: what `at` counts. A
 * string's `length` counts UTF-16 units, two for most emoji. */
function codePoints(text: string): number {
  // A string iterates by code point, so its spread counts them.
  return Array.from(text).length
}

/** Writes `piece` into `existing` at code point `at`, where `held` code
 * points of this text have been placed so far. A piece at the end is
 * appended. One that starts inside what is held is written over it, which
 * is what makes a replay safe to apply twice: the text it writes is the
 * text that is there. One that starts past the end (the replay was
 * truncated, so a span is missing until the rows are read) is appended,
 * and the count follows the server's so later pieces still line up. */
function place(
  existing: string,
  held: number,
  at: number | undefined,
  piece: string
): { text: string; held: number } {
  const n = codePoints(piece)
  if (at === undefined) return { text: existing + piece, held: held + n }
  if (at >= held) return { text: existing + piece, held: at + n }
  const points = Array.from(existing)
  return {
    text: points.slice(0, at).join("") + piece + points.slice(at + n).join(""),
    held: Math.max(held, at + n),
  }
}

/** The live turn holding the text of `turn`, or -1. */
function placedTurn(turns: TurnView[], turn: number | undefined): number {
  if (turn === undefined) return -1
  return turns.findIndex((t) => t.live?.turn === turn)
}

/** Whether a piece of `turn` that no live turn holds yet opens a turn of its
 * own: after a closed round, after anything that is not the assistant, and
 * after another row's text. A live turn holds the text of one stored row,
 * which is what lets the rows take it over one row at a time. */
function opensTurn(live: LiveOverlay, turn: number | undefined): boolean {
  const last = live.turns[live.turns.length - 1]
  if (!last || last.role !== "assistant" || live.closed) return true
  return (
    turn !== undefined && last.live !== undefined && last.live.turn !== turn
  )
}

/** The place a new live turn starts counting from. */
function placeOf(turn: number | undefined): { live?: LivePlace } {
  return turn === undefined ? {} : { live: { turn, text: 0, reasoning: 0 } }
}

/** Writes a piece of the model's thinking into the completion in flight, at
 * its position when it carries one. A completion may interleave thinking
 * with its text, so only a closed round (its tools settled, or a
 * compaction) or no assistant turn at all opens a new turn. The time spent
 * runs from the first piece to the latest, the span the row's
 * `reasoningMillis` measures. */
export function pushReasoning(
  live: LiveOverlay,
  text: string,
  seq: number,
  now: number = Date.now(),
  where: Placement = {}
): LiveOverlay {
  return aboveWaiting(readThrough(live, where.turn), (head) =>
    reasoningInto(head, text, seq, now, where)
  )
}

function reasoningInto(
  live: LiveOverlay,
  text: string,
  seq: number,
  now: number,
  where: Placement
): LiveOverlay {
  const turns = [...live.turns]
  let at = placedTurn(turns, where.turn)
  if (at < 0 && opensTurn(live, where.turn)) {
    turns.push({
      key: `live-a${seq}`,
      role: "assistant",
      content: "",
      tools: [],
      ...placeOf(where.turn),
    })
  }
  if (at < 0) at = turns.length - 1
  const turn = turns[at]
  const placed = place(
    turn.reasoning ?? "",
    turn.live?.reasoning ?? 0,
    turn.live?.turn === where.turn ? where.at : undefined,
    text
  )
  // A replayed piece is thinking the reader already saw: it adds nothing,
  // so neither the clock nor "still thinking" moves for it.
  const fresh = !turn.live || placed.held > turn.live.reasoning
  const startedAt = turn.reasoningStartedAt ?? now
  turns[at] = {
    ...turn,
    reasoning: placed.text,
    ...(fresh
      ? {
          reasoningStartedAt: startedAt,
          reasoningLastAt: now,
          reasoningMillis: Math.max(0, now - startedAt),
          thinking: true,
        }
      : {}),
    ...(turn.live ? { live: { ...turn.live, reasoning: placed.held } } : {}),
  }
  return { turns, closed: at === turns.length - 1 ? false : live.closed }
}

/** Writes a delta into its turn, at its position when it carries one,
 * starting a new assistant turn when the previous one has already
 * dispatched and settled its tools. */
export function pushDelta(
  live: LiveOverlay,
  text: string,
  seq: number,
  where: Placement = {}
): LiveOverlay {
  return aboveWaiting(readThrough(live, where.turn), (head) =>
    deltaInto(head, text, seq, where)
  )
}

function deltaInto(
  live: LiveOverlay,
  text: string,
  seq: number,
  where: Placement
): LiveOverlay {
  const turns = [...live.turns]
  let at = placedTurn(turns, where.turn)
  if (at < 0 && opensTurn(live, where.turn)) {
    turns.push({
      key: `live-a${seq}`,
      role: "assistant",
      content: "",
      tools: [],
      ...placeOf(where.turn),
    })
  }
  if (at < 0) at = turns.length - 1
  const turn = turns[at]
  const placed = place(
    turn.content,
    turn.live?.text ?? 0,
    turn.live?.turn === where.turn ? where.at : undefined,
    text
  )
  // A replayed piece changes nothing a reader sees, the thinking line
  // included: the model may be thinking again after that text.
  const fresh = !turn.live || placed.held > turn.live.text
  turns[at] = {
    ...(fresh ? doneThinking(turn) : turn),
    content: placed.text,
    ...(turn.live ? { live: { ...turn.live, text: placed.held } } : {}),
  }
  return { turns, closed: at === turns.length - 1 ? false : live.closed }
}

/** Attaches a started call to the assistant turn in flight, opening one when
 * the turn dispatched before it said anything. A call the overlay already
 * shows (a replay) is left as it is. */
export function pushToolStart(
  live: LiveOverlay,
  call: ToolCallView,
  seq: number
): LiveOverlay {
  return aboveWaiting(live, (head) => toolStartInto(head, call, seq))
}

function toolStartInto(
  live: LiveOverlay,
  call: ToolCallView,
  seq: number
): LiveOverlay {
  if (call.id && live.turns.some((t) => t.tools.some((c) => c.id === call.id)))
    return live
  const turns = [...live.turns]
  const last = turns[turns.length - 1]
  if (!last || last.role !== "assistant") {
    turns.push({
      key: `live-a${seq}`,
      role: "assistant",
      content: "",
      tools: [call],
    })
    return { ...live, turns }
  }
  turns[turns.length - 1] = {
    ...doneThinking(last),
    tools: [...last.tools, call],
  }
  return { ...live, turns }
}

/** The calls a live overlay has settled, by call id, for `transcriptOf`. */
export function liveCalls(live: LiveOverlay): Map<string, ToolCallView> {
  const calls = new Map<string, ToolCallView>()
  for (const turn of live.turns) {
    for (const call of turn.tools) {
      if (call.id && call.ok !== undefined) calls.set(call.id, call)
    }
  }
  return calls
}

/** The live turns the stored rows do not show yet. The rows are the truth
 * for whatever they hold, so a live user message whose row is stored, the
 * text of a turn whose row is stored, a tool card whose call or whose result
 * is stored and a compaction whose summary row is stored are left to them. Everything else
 * stays as it is, under the keys it had: an attach that reads the rows again
 * never empties what is still only live. */
export function liveBeyond(
  live: LiveOverlay,
  rows: SubstrateRecord[]
): TurnView[] {
  if (!live.turns.length) return live.turns
  const ids = new Set(rows.map((r) => r.id))
  const held = new Set<number>()
  const compactions = new Set<number>()
  // The calls the stored assistant rows declare: each has its card there,
  // result row or not, so a live card of the same call would be a second.
  const declared = new Set<string>()
  for (const r of rows) {
    const turn = r.properties.turn
    if (typeof turn === "number") held.add(turn)
    if (r.properties.role === "assistant") {
      for (const call of callsOf(r)) if (call.id) declared.add(call.id)
    }
    if (r.properties.role === "summary") {
      const before = num(r.properties.tokensBefore)
      if (before !== undefined) compactions.add(before)
    }
  }
  return live.turns.flatMap((turn) => {
    if (turn.role === "user") {
      return turn.sentId && ids.has(turn.sentId) ? [] : [turn]
    }
    if (turn.role === "summary") {
      const before = turn.compaction?.tokensBefore
      return before !== undefined && compactions.has(before) ? [] : [turn]
    }
    const tools = turn.tools.filter(
      (c) =>
        !(c.id && declared.has(c.id)) &&
        (c.turn === undefined || !held.has(c.turn))
    )
    if (turn.live && held.has(turn.live.turn)) {
      // Its text is a row now; calls of its that are still only live go on
      // as their own turn, as a replay would draw them.
      if (!tools.length) return []
      return [
        {
          key: turn.key,
          role: turn.role,
          content: "",
          tools,
        },
      ]
    }
    if (tools.length === turn.tools.length) return [turn]
    if (!tools.length && !turn.content && !turn.reasoning) return []
    return [{ ...turn, tools }]
  })
}

/** Puts the done event's mentions on the reply in flight, so a live reply
 * shows what its row will hold before the rows are read again. */
export function settleMentions(
  live: LiveOverlay,
  mentions: AgentMention[] | undefined
): LiveOverlay {
  return aboveWaiting(live, (head) => mentionsInto(head, mentions))
}

function mentionsInto(
  live: LiveOverlay,
  mentions: AgentMention[] | undefined
): LiveOverlay {
  const last = live.turns[live.turns.length - 1]
  if (!mentions?.length || !last || last.role !== "assistant") return live
  const turns = [...live.turns]
  turns[turns.length - 1] = { ...last, mentions }
  return { ...live, turns }
}

/** Marks where the loop compacted the thread mid-run. The event carries no
 * summary text (the row does, and replaces this line at handover), and the
 * next delta or tool call opens a new assistant turn after it. */
export function pushCompacted(
  live: LiveOverlay,
  compaction: Pick<CompactionView, "tokensBefore" | "covered">,
  seq: number
): LiveOverlay {
  return aboveWaiting(live, (head) => compactedInto(head, compaction, seq))
}

function compactedInto(
  live: LiveOverlay,
  compaction: Pick<CompactionView, "tokensBefore" | "covered">,
  seq: number
): LiveOverlay {
  // A compaction the overlay already marks (a replay) is left as it is.
  if (
    compaction.tokensBefore !== undefined &&
    live.turns.some(
      (t) =>
        t.role === "summary" &&
        t.compaction?.tokensBefore === compaction.tokensBefore &&
        t.compaction?.covered === compaction.covered
    )
  ) {
    return live
  }
  return {
    turns: [
      ...live.turns,
      {
        key: `live-c${seq}`,
        role: "summary",
        content: "",
        tools: [],
        compaction: {
          tokensBefore: compaction.tokensBefore,
          covered: compaction.covered,
        },
      },
    ],
    closed: true,
  }
}

/** Settles a call BY ID: one turn may dispatch the same tool twice, and
 * settling by name would close the wrong card.
 *
 * A finish with no card is not a no-op. The loop refuses a dispatch past the
 * tool-call budget WITHOUT starting it — the refusal is a finished event and
 * nothing else — so the one card a reader most needs ("stop calling tools")
 * would never appear live, only on reload. An unclaimed finish opens its own
 * card, already settled. */
/** The `job.state` a background call's tool row carries, or undefined. */
function jobStateOf(record: SubstrateRecord): string | undefined {
  const job = record.properties.job
  if (!job || typeof job !== "object") return undefined
  const state = (job as Record<string, unknown>).state
  return typeof state === "string" && state ? state : undefined
}

export function settleTool(
  live: LiveOverlay,
  call: ToolCallView,
  output: string,
  ok: boolean,
  seq: number,
  job?: string
): LiveOverlay {
  return aboveWaiting(live, (head) =>
    toolSettledInto(head, call, output, ok, seq, job)
  )
}

function toolSettledInto(
  live: LiveOverlay,
  call: ToolCallView,
  output: string,
  ok: boolean,
  seq: number,
  job?: string
): LiveOverlay {
  const settled = { ...call, output, ok, ...(job ? { job } : {}) }
  // An empty id cannot identify anything, so it settles the OLDEST card still
  // running rather than every id-less card at once.
  const match = (c: ToolCallView) =>
    call.id ? c.id === call.id : c.ok === undefined
  let claimed = false
  const turns = live.turns.map((turn) => {
    if (claimed || !turn.tools.some(match)) return turn
    claimed = true
    let done = false
    return {
      ...turn,
      tools: turn.tools.map((c) => {
        if (done || !match(c)) return c
        done = true
        // A job's end settles its card a second time: the output it brings
        // replaces the handle the call answered with.
        return { ...c, output, ok, ...(job ? { job } : {}) }
      }),
    }
  })
  if (claimed) return { turns, closed: true }
  return {
    turns: [
      ...turns,
      { key: `live-a${seq}`, role: "assistant", content: "", tools: [settled] },
    ],
    closed: true,
  }
}
