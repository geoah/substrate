/** The transcript as a reader sees it, folded out of the `llmmessage` rows the
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

import type { SubstrateRecord } from "./types"

/** One changelog entry a dispatch wrote, as the engine stamps it onto the
 * row's `changes` property: the seq addresses the delta in the changelog,
 * kind and id address the record it moved. */
export interface ChangeStamp {
  seq: number
  op: string
  kind: string
  id: string
}

/** One dispatched tool call, with whatever has settled about it so far. */
export interface ToolCallView {
  id: string
  name: string
  /** The arguments the model emitted, verbatim — a JSON string, usually. */
  arguments: string
  /** The dispatch's result payload; absent while the call is still running. */
  output?: string
  /** Whether the dispatch succeeded; absent while it is still running. */
  ok?: boolean
  /** The changelog entries the dispatch wrote (engine-stamped, persisted rows
   * only — the live stream does not carry them, so they appear on handover). */
  changes?: ChangeStamp[]
}

export interface TurnView {
  /** Stable across a re-render and across the live→persisted handover. */
  key: string
  /** `system` is the substrate's own turn — a proposal decision the engine
   * wrote into the thread, never something a model said. */
  role: "user" | "assistant" | "system"
  content: string
  tools: ToolCallView[]
  /** On a system turn: the changelog entries the decision wrote. */
  changes?: ChangeStamp[]
}

function str(value: unknown): string {
  return typeof value === "string" ? value : ""
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
    const keys = Object.keys(parsed as Record<string, unknown>)
    return !(keys.length === 1 && keys[0] === "error")
  } catch {
    return true
  }
}

/** The model-facing name of the `propose` host function: the name a `tools:`
 * entry takes when it aliases nothing (`vocabulary.AgentToolPropose`, the local
 * segment of `core.substrate.reamde.dev/propose`). */
const PROPOSE_TOOL = "propose"

/** The change-request kind, as a `changes` stamp spells it. */
const REQUEST_KIND = "core.substrate.reamde.dev/recordpatchrequest"

/** The interaction kind — a batch of questions the `ask` built-in landed. */
const INTERACTION_KIND = "core.substrate.reamde.dev/llminteraction"

/** The interaction a settled ask landed, from the engine-stamped changes. */
export function interactionIdOf(call: ToolCallView): string | undefined {
  const stamped = (call.changes ?? []).find(
    (c) => c.kind === INTERACTION_KIND && c.op === "put"
  )
  return stamped?.id
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

/** A minted record id: twelve lowercase base32 characters (`engine.newID`).
 * Checked because the NAME alone cannot settle what a call was. */
const MINTED_ID = /^[a-z2-7]{12}$/

/** The change request a settled `propose` call landed, or undefined.
 *
 * THE NAME IS NOT PROVENANCE, and that is the limit of this. A transcript row
 * records the model-facing tool NAME, never the `function` behind it, so an
 * agent that aliases `{function: …/propose, name: file}` gets no link, and one
 * that aliases some other function TO `propose` would get one on any payload
 * that looked right. Carrying the tool entry's function identity on the
 * llmmessage row is what would settle it, and it is not carried yet (noted
 * follow-up).
 *
 * Everything that CAN be checked is: the call settled ok, the name is exactly
 * the built-in's, and the payload is `{"id": <minted id>}` and nothing else,
 * which is precisely what `dispatchPropose` answers on success. A payload with
 * a second key, or an id no mint could have produced, is some other tool. */
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
  if (call.ok !== true || call.name !== PROPOSE_TOOL) return undefined
  const payload = (call.output ?? "").trim()
  if (!payload.startsWith("{")) return undefined
  try {
    const parsed: unknown = JSON.parse(payload)
    if (typeof parsed !== "object" || parsed === null) return undefined
    const keys = Object.keys(parsed as Record<string, unknown>)
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
    out.push({ id, name, arguments: str(call.arguments) })
  }
  return out
}

/** Fold the rows into turns. The rows arrive in loop order (`turn` ascending,
 * then created) — this preserves that order and never sorts, because `turn` is
 * the loop's own counter and two rows of one turn are ordered by creation. */
export function transcriptOf(messages: SubstrateRecord[]): TurnView[] {
  const turns: TurnView[] = []
  // Every unsettled call in the transcript so far, so a tool row finds its
  // call even when another assistant turn has landed in between.
  const pending = new Map<string, ToolCallView>()

  for (const record of messages) {
    const role = str(record.properties.role)
    if (role === "user" || role === "assistant") {
      const tools = role === "assistant" ? callsOf(record) : []
      for (const call of tools) if (call.id) pending.set(call.id, call)
      turns.push({
        key: record.id,
        role,
        content: str(record.properties.content),
        tools,
      })
      continue
    }
    if (role === "system") {
      // The substrate's own turn (a proposal decision): its content is a
      // self-describing JSON envelope, and its `changes` say what the
      // decision wrote. Never folded into a call — nothing dispatched it.
      turns.push({
        key: record.id,
        role,
        content: str(record.properties.content),
        tools: [],
        changes: changesOf(record),
      })
      continue
    }
    if (role !== "tool") continue

    const output = str(record.properties.content)
    const ok = toolOK(record)
    const call = pending.get(str(record.properties.toolCallId))
    if (call) {
      call.output = output
      call.ok = ok
      call.changes = changesOf(record)
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
          name: str(record.properties.tool),
          arguments: "",
          output,
          ok,
          changes: changesOf(record),
        },
      ],
    })
  }
  return turns
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
    }
  } catch {
    return undefined
  }
}
