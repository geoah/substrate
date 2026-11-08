import { SubstrateClient } from "./client"
import { failure } from "./errors"

export interface WatchCursor {
  seq: number
  generation: string
}

export interface WatchRow {
  seq: number
  kind: string
  recordId: string
  op: string
}

export type WatchEvent =
  | { type: "opened"; cursor: WatchCursor }
  | { type: "row"; row: WatchRow }
  /** The cursor addresses no history the server holds: read everything again. */
  | { type: "relist" }
  | { type: "interrupted"; words: string }
  | { type: "signedOut" }
  | { type: "ended"; words: string }

const pauses = [1000, 3000, 8000, 15000]

/** Follows `GET /api/v1/changes?watch=1` over a set of kinds, from a cursor
 * (or the live head), through heartbeats, again from the last cursor when the
 * connection drops, and to a relist when the cursor no longer addresses the
 * history. The kit's WatchFollower, for a browser. */
export function follow(
  client: SubstrateClient,
  kinds: string[],
  from: WatchCursor | null,
  onEvent: (e: WatchEvent) => void,
  signal: AbortSignal
): void {
  let cursor = from
  let failures = 0

  const connect = async (): Promise<"again" | "stop"> => {
    const query: Record<string, string | string[]> = { kinds, watch: "1" }
    if (cursor) {
      query.from = String(cursor.seq)
      if (cursor.generation) query.generation = cursor.generation
    }
    let res: Response
    try {
      res = await fetch(client.url("api/v1/changes", query), {
        headers: client.headers("application/x-ndjson"),
        signal,
      })
    } catch {
      if (signal.aborted) return "stop"
      onEvent({
        type: "interrupted",
        words: "Offline. Changes will show when the connection is back.",
      })
      return "again"
    }
    if (res.status === 410) {
      onEvent({ type: "relist" })
      return "stop"
    }
    if (res.status === 401) {
      onEvent({ type: "signedOut" })
      return "stop"
    }
    if (!res.ok || !res.body) {
      const err = await failure(res)
      if (res.status >= 400 && res.status < 500) {
        onEvent({ type: "ended", words: err.words })
        return "stop"
      }
      onEvent({ type: "interrupted", words: err.words })
      return "again"
    }
    const reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
    let buffer = ""
    try {
      for (;;) {
        const { value, done } = await reader.read()
        if (done) break
        buffer += value
        let nl: number
        while ((nl = buffer.indexOf("\n")) >= 0) {
          const line = buffer.slice(0, nl).trim()
          buffer = buffer.slice(nl + 1)
          if (!line) continue
          let frame: Record<string, unknown>
          try {
            frame = JSON.parse(line)
          } catch {
            continue
          }
          failures = 0
          if (typeof frame.seq === "number" && typeof frame.ts === "string") {
            cursor = { seq: frame.seq, generation: cursor?.generation ?? "" }
            onEvent({
              type: "row",
              row: {
                seq: frame.seq,
                kind: String(frame.kind ?? ""),
                recordId: String(frame.recordId ?? ""),
                op: String(frame.op ?? ""),
              },
            })
          } else if (typeof frame.bookmark === "number") {
            const generation =
              typeof frame.generation === "string" ? frame.generation : ""
            cursor = cursor
              ? { seq: cursor.seq, generation: generation || cursor.generation }
              : { seq: frame.bookmark, generation }
            onEvent({ type: "opened", cursor })
          } else if (frame.error && typeof frame.error === "object") {
            const e = frame.error as { code?: string; message?: string }
            if (e.code === "compacted") {
              onEvent({ type: "relist" })
              return "stop"
            }
            onEvent({
              type: "interrupted",
              words: e.message ?? "The watch stopped.",
            })
            return "again"
          }
        }
      }
    } catch {
      if (signal.aborted) return "stop"
    }
    onEvent({ type: "interrupted", words: "Reconnecting…" })
    return "again"
  }

  void (async () => {
    while (!signal.aborted) {
      const outcome = await connect()
      if (outcome === "stop" || signal.aborted) return
      const wait = pauses[Math.min(failures, pauses.length - 1)]
      failures++
      await new Promise((r) => setTimeout(r, wait))
    }
  })()
}
