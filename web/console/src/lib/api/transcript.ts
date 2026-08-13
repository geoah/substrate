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
        },
      ],
    })
  }
  return turns
}
