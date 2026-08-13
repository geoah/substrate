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
}

export interface TurnView {
  /** Stable across a re-render and across the live→persisted handover. */
  key: string
  role: "user" | "assistant"
  content: string
  tools: ToolCallView[]
}

function str(value: unknown): string {
  return typeof value === "string" ? value : ""
}

/** Whether a tool row reports a failure. The `ok` property is authoritative
 * where the row carries one; older rows predate it, and for those the loop's
 * own failure envelope — `toolError` writes `{"error": …}` and nothing else —
 * is the only evidence. A SUCCESSFUL result that happens to carry an `error`
 * key alongside other keys is not mistaken for one, because the envelope has
 * exactly one key. */
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

/** The declared calls on an assistant row: `toolCalls` is a json property, so
 * it arrives as whatever was stored. Anything that is not a well-shaped call
 * is dropped rather than rendered as a nameless card. */
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
    if (role !== "tool") continue

    const output = str(record.properties.content)
    const ok = toolOK(record)
    const call = pending.get(str(record.properties.toolCallId))
    if (call) {
      call.output = output
      call.ok = ok
      pending.delete(str(record.properties.toolCallId))
      continue
    }
    // An orphan: a result whose call is not in this window (the assistant row
    // was compacted away, or the id never matched). Showing it under its own
    // name beats dropping a dispatch that really happened.
    const orphan: ToolCallView = {
      id: str(record.properties.toolCallId),
      name: str(record.properties.tool),
      arguments: "",
      output,
      ok,
    }
    const last = turns[turns.length - 1]
    if (last && last.role === "assistant") {
      last.tools.push(orphan)
    } else {
      turns.push({
        key: record.id,
        role: "assistant",
        content: "",
        tools: [orphan],
      })
    }
  }
  return turns
}

/** The assistant's settled reply: the newest assistant turn's prose. A turn
 * that only dispatched tools has none, so this looks past it. */
export function lastReply(turns: TurnView[]): string | undefined {
  for (let i = turns.length - 1; i >= 0; i--) {
    if (turns[i].role === "assistant" && turns[i].content) return turns[i].content
  }
  return undefined
}
