import { SubstrateClient } from "./client"
import { failure } from "./errors"

/** Where a watch resumes: after `seq`, in the history `generation` names. A
 * seq means nothing without its generation; a watch answered with another
 * relists. */
export interface WatchCursor {
  seq: number
  generation: string
}

/** One committed change as the watch reports it. */
export interface WatchRow {
  seq: number
  kind: string
  recordId: string
  /** `put`, `patch`, `delete` or `purge`. */
  op: string
  /** The record is gone after this change. */
  removes: boolean
}

/** One line of the watch's ndjson. */
export type WatchFrame =
  /** The opening frame: the seq the stream starts after, and the history
   * generation every later resume names beside it. */
  | { type: "bookmark"; cursor: WatchCursor }
  | { type: "row"; row: WatchRow }
  /** The reserved terminal frame: the stream failed mid-way. `compacted`
   * means its cursor no longer addresses this history. */
  | { type: "failed"; code: string; message: string }

/** A line as a frame, or undefined for a blank line, the `{}` heartbeat or
 * anything else. */
export function parseWatchFrame(line: string): WatchFrame | undefined {
  const text = line.trim()
  if (!text) return undefined
  let frame: unknown
  try {
    frame = JSON.parse(text)
  } catch {
    return undefined
  }
  if (!frame || typeof frame !== "object" || Array.isArray(frame))
    return undefined
  const f = frame as Record<string, unknown>
  if (typeof f.seq === "number" && typeof f.ts === "string") {
    const op = typeof f.op === "string" ? f.op : ""
    return {
      type: "row",
      row: {
        seq: f.seq,
        kind: typeof f.kind === "string" ? f.kind : "",
        recordId: typeof f.recordId === "string" ? f.recordId : "",
        op,
        removes: op === "delete" || op === "purge",
      },
    }
  }
  if (typeof f.bookmark === "number") {
    return {
      type: "bookmark",
      cursor: {
        seq: f.bookmark,
        generation: typeof f.generation === "string" ? f.generation : "",
      },
    }
  }
  if (f.error && typeof f.error === "object") {
    const e = f.error as { code?: unknown; message?: unknown }
    return {
      type: "failed",
      code: typeof e.code === "string" ? e.code : "internal",
      message: typeof e.message === "string" ? e.message : "The watch stopped.",
    }
  }
  return undefined
}

export type WatchEvent =
  | { type: "opened"; cursor: WatchCursor }
  | { type: "row"; row: WatchRow }
  /** The cursor addresses no history the server holds: read everything again. */
  | { type: "relist" }
  | { type: "interrupted"; words: string }
  | { type: "signedOut" }
  /** The token lacks a scope the watch needs: only a new approval helps. */
  | { type: "needsApproval"; words: string }
  | { type: "ended"; words: string }

export interface FollowOptions {
  /** The waits between attempts after a connection drops, the last one
   * repeated. */
  pauses?: number[]
  /** How long a stream may stay silent before it is taken for dead: the
   * server sends a heartbeat every 30 s. */
  idleTimeout?: number
}

const defaultPauses = [1000, 3000, 8000, 15000]
const defaultIdle = 75_000

/** Follows `GET /api/v1/changes?watch=1` over a set of kinds, from a cursor
 * (or the live head), through heartbeats, again from the last cursor when the
 * connection drops or falls silent, and to a relist when the cursor no longer
 * addresses the history. The kit's WatchFollower, for a browser. */
export function follow(
  client: SubstrateClient,
  kinds: string[],
  from: WatchCursor | null,
  onEvent: (e: WatchEvent) => void,
  signal: AbortSignal,
  options: FollowOptions = {}
): void {
  const pauses = options.pauses ?? defaultPauses
  const idleTimeout = options.idleTimeout ?? defaultIdle
  let cursor = from
  let failures = 0

  const connect = async (): Promise<"again" | "stop"> => {
    const query: Record<string, string | string[]> = { kinds, watch: "1" }
    if (cursor) {
      query.from = String(cursor.seq)
      if (cursor.generation) query.generation = cursor.generation
    }
    // One connection's own abort, so a silent stream can be dropped without
    // stopping the follow.
    const connection = new AbortController()
    const stop = () => connection.abort()
    signal.addEventListener("abort", stop)
    let silent = false
    let idle: ReturnType<typeof setTimeout> | undefined
    let reader: ReadableStreamDefaultReader<string> | undefined
    const arm = () => {
      clearTimeout(idle)
      idle = setTimeout(() => {
        silent = true
        connection.abort()
        // Not every body errors when its request is aborted: a read still
        // waiting ends here either way.
        void reader?.cancel().catch(() => {})
      }, idleTimeout)
    }
    try {
      let res: Response
      try {
        arm()
        res = await fetch(client.url("api/v1/changes", query), {
          headers: client.headers("application/x-ndjson"),
          signal: connection.signal,
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
        if (err.insufficientScope) {
          onEvent({ type: "needsApproval", words: err.words })
          return "stop"
        }
        if (res.status >= 400 && res.status < 500 && res.status !== 429) {
          onEvent({ type: "ended", words: err.words })
          return "stop"
        }
        onEvent({ type: "interrupted", words: err.words })
        return "again"
      }
      reader = res.body.pipeThrough(new TextDecoderStream()).getReader()
      let buffer = ""
      try {
        for (;;) {
          const { value, done } = await reader.read()
          if (done) break
          arm()
          buffer += value
          let nl: number
          while ((nl = buffer.indexOf("\n")) >= 0) {
            const frame = parseWatchFrame(buffer.slice(0, nl))
            buffer = buffer.slice(nl + 1)
            if (!frame) continue
            failures = 0
            switch (frame.type) {
              case "row":
                cursor = {
                  seq: frame.row.seq,
                  generation: cursor?.generation ?? "",
                }
                onEvent({ type: "row", row: frame.row })
                break
              case "bookmark":
                cursor = cursor
                  ? {
                      seq: cursor.seq,
                      generation: frame.cursor.generation || cursor.generation,
                    }
                  : frame.cursor
                onEvent({ type: "opened", cursor })
                break
              case "failed":
                if (frame.code === "compacted") {
                  onEvent({ type: "relist" })
                  return "stop"
                }
                // The stream's token ended (revoked, or the person signed
                // out everywhere): sign in again, as on a 401.
                if (frame.code === "auth") {
                  onEvent({ type: "signedOut" })
                  return "stop"
                }
                onEvent({ type: "interrupted", words: frame.message })
                return "again"
            }
          }
        }
      } catch {
        if (signal.aborted) return "stop"
      }
      onEvent({
        type: "interrupted",
        words: silent
          ? "The connection went quiet. Reconnecting…"
          : "Reconnecting…",
      })
      return "again"
    } finally {
      clearTimeout(idle)
      signal.removeEventListener("abort", stop)
    }
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
