/** The cross-collection change feed (`GET /api/v1/changes`), read
 * three ways:
 *
 * - **History**: newest-first pages addressed by `before=<seq>`. The changelog
 *   is seq-addressed, so the feed pages older by cursor rather than by an
 *   offset jump.
 * - **Time seek**: the wire has no time-range parameter, but seq order is time
 *   order, so "history until T" is a binary search over the seq axis — a dozen
 *   one-row probes finding the newest row at or before T.
 * - **Live tail**: `watch=1`, chunked HTTP ndjson — one `{"bookmark":N}` line,
 *   then one JSON row per committed change, `{}` heartbeats while idle.
 *
 * Server-side facets (parseChangeFilter): `kinds`, `actors`, `ops`,
 * `recordId`+`recordKind`, `q` — history and watch honor the same set. Time is
 * NOT on the wire: it rides the seek above. A `kind` is a reference
 * (`<authority>/<name>`); its `/` percent-encodes as `%2F` in the query. */

import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query"

import { rootPath, envelopeError, request } from "./http"
import { getToken, sessionExpired } from "./session"
import { ApiError, type ChangeRow } from "./types"

// ── filter ──────────────────────────────────────────────────────────────────

/** The wire's own facet vocabulary — every field here is server-side. */
export interface ChangeFeedFilter {
  /** Kind references (`<authority>/<name>`, or bare `<name>`). */
  kinds?: string[]
  actors?: string[]
  ops?: string[]
  /** Scope to one record's audit trail. An id is NOT unique
   * , so `recordId` REQUIRES `recordKind` — either alone is a bad_request. */
  recordId?: string
  recordKind?: string
  /** Case-insensitive substring over kind, actor, record id, payload text. */
  q?: string
}

export function changesSearch(filter: ChangeFeedFilter = {}): URLSearchParams {
  const params = new URLSearchParams()
  for (const k of filter.kinds ?? []) params.append("kinds", k)
  for (const a of filter.actors ?? []) params.append("actors", a)
  for (const o of filter.ops ?? []) params.append("ops", o)
  // recordId and recordKind travel together or not at all (the server rejects
  // either alone); only emit the pair when both are present.
  if (filter.recordId && filter.recordKind) {
    params.set("recordId", filter.recordId)
    params.set("recordKind", filter.recordKind)
  }
  if (filter.q) params.set("q", filter.q)
  return params
}

/** Stable queryKey shape: sorted lists, nulls for absent fields. */
function filterKey(filter: ChangeFeedFilter) {
  return {
    kinds: filter.kinds?.slice().sort() ?? null,
    actors: filter.actors?.slice().sort() ?? null,
    ops: filter.ops?.slice().sort() ?? null,
    recordId: filter.recordId ?? null,
    recordKind: filter.recordKind ?? null,
    q: filter.q ?? null,
  }
}

// ── history pages ───────────────────────────────────────────────────────────

export const CHANGELOG_PAGE = 200

/** One history page: the rows plus the server's CONTINUATION cursor (the
 * oldest seq on the page). The cursor is the next
 * `before` — it advances past scope-filtered rows, so a short page is NOT the
 * end; the walk continues while a cursor comes back and stops when it is
 * omitted (exhausted). */
export interface ChangesPage {
  changes: ChangeRow[]
  cursor?: number
}

export async function fetchChangesPage(opts: {
  /** Rows strictly below this seq; absent/0 = from the head. */
  before?: number
  first?: number
  filter?: ChangeFeedFilter
  signal?: AbortSignal
}): Promise<ChangesPage> {
  const params = changesSearch(opts.filter)
  params.set("first", String(opts.first ?? CHANGELOG_PAGE))
  if (opts.before && opts.before > 0) params.set("before", String(opts.before))
  const res = await request<{ changes?: ChangeRow[]; cursor?: number }>(
    "GET",
    `${rootPath("changes")}?${params}`,
    undefined,
    { signal: opts.signal }
  )
  return { changes: res.changes ?? [], cursor: res.cursor }
}

export interface ChangesFeedOpts {
  first?: number
  /** Start below this seq (a time seek's answer); 0 = the head. */
  startBefore?: number
  /** Stop paging once a page reaches rows older than this instant — the
   * client half of the time range the wire cannot express. */
  sinceMs?: number
}

export function changesInfiniteOptions(
  filter: ChangeFeedFilter = {},
  opts: ChangesFeedOpts = {}
) {
  const first = opts.first ?? CHANGELOG_PAGE
  const startBefore = opts.startBefore ?? 0
  return infiniteQueryOptions({
    queryKey: [
      "changes",
      "feed",
      filterKey(filter),
      { first, startBefore, sinceMs: opts.sinceMs ?? null },
    ],
    queryFn: ({ pageParam, signal }) =>
      fetchChangesPage({
        before: pageParam > 0 ? pageParam : undefined,
        first,
        filter,
        signal,
      }),
    initialPageParam: startBefore,
    getNextPageParam: (last) => {
      // The server cursor is the continuation (it advances past scope-filtered
      // rows); its absence — not a short page — is the feed's beginning.
      if (last.cursor === undefined) return undefined
      // A page that crossed the range's floor is the last one worth reading.
      const oldest = last.changes[last.changes.length - 1]
      if (
        opts.sinceMs !== undefined &&
        oldest &&
        Date.parse(oldest.ts) < opts.sinceMs
      ) {
        return undefined
      }
      return last.cursor
    },
  })
}

// ── the time seek ───────────────────────────────────────────────────────────

/** The newest row at or below a seq, or undefined past the feed's floor. */
export type SeekProbe = (maxSeq: number) => Promise<ChangeRow | undefined>

/** The `before` value whose page holds only rows at or before `targetMs`:
 * binary search over seq positions (seq order is commit order is time order).
 * Returns 0 when the whole feed qualifies — read from the head. */
export async function seekBoundary(
  probe: SeekProbe,
  head: ChangeRow | undefined,
  targetMs: number
): Promise<number> {
  if (!head) return 0
  if (Date.parse(head.ts) <= targetMs) return 0
  // Invariant: rows at or below lo are ≤ target (vacuous at 0), the row at hi
  // is above it. probe(mid) sees the newest row ≤ mid, so gc gaps are safe.
  let lo = 0
  let hi = head.seq
  while (hi - lo > 1) {
    const mid = lo + Math.floor((hi - lo) / 2)
    const row = await probe(mid)
    if (!row || Date.parse(row.ts) <= targetMs) lo = mid
    else hi = mid
  }
  return lo + 1
}

/** Immutable answer — history never moves under a fixed instant. */
export function seekQueryOptions(untilMs: number) {
  return queryOptions({
    queryKey: ["changes", "seek", untilMs],
    queryFn: async ({ signal }) => {
      const one = async (before?: number) =>
        (await fetchChangesPage({ before, first: 1, signal })).changes[0]
      const head = await one()
      return seekBoundary(async (maxSeq) => one(maxSeq + 1), head, untilMs)
    },
    staleTime: Infinity,
  })
}

// ── the live tail ───────────────────────────────────────────────────────────

/** `compacted` = a resume fell below the retention horizon; the client must
 * re-list from a fresh head. `stopped` = a terminal error frame (or a
 * non-retriable open failure) ended the stream — no silent reconnect loop. */
export type WatchStatus =
  "connecting" | "live" | "retrying" | "compacted" | "stopped" | "off"

/** The problem object the substrate carries in a REST envelope, a GraphQL
 * error's bundles, AND the watch terminal error frame. */
export interface WatchError {
  code: string
  message: string
  problems?: string[]
}

export interface WatchLine {
  row?: ChangeRow
  bookmark?: number
  /** The reserved terminal error control frame — a mid-stream failure travels
   * as this one problem object rather than a silent EOF. */
  error?: WatchError
}

/** One ndjson line: a line WITH a `seq` is a change row;
 * a line WITHOUT is a CONTROL frame keyed by its single field — `bookmark`
 * opens, `{}` is the idle heartbeat, `error` is the terminal failure. Blank
 * lines, heartbeats and garbage read as nothing. */
export function parseWatchLine(line: string): WatchLine | null {
  const trimmed = line.trim()
  if (!trimmed) return null
  let parsed: unknown
  try {
    parsed = JSON.parse(trimmed)
  } catch {
    return null
  }
  if (typeof parsed !== "object" || parsed === null) return null
  const obj = parsed as Record<string, unknown>
  if (typeof obj.seq === "number" && typeof obj.ts === "string") {
    return { row: parsed as ChangeRow }
  }
  if (typeof obj.bookmark === "number") return { bookmark: obj.bookmark }
  if (obj.error && typeof obj.error === "object") {
    const e = obj.error as Record<string, unknown>
    return {
      error: {
        code: typeof e.code === "string" ? e.code : "internal",
        message: typeof e.message === "string" ? e.message : "stream failed",
        problems: Array.isArray(e.problems)
          ? (e.problems.filter((p) => typeof p === "string") as string[])
          : undefined,
      },
    }
  }
  return null
}

export interface WatchHandle {
  stop(): void
}

const RETRY_DELAY_MS = 3_000

/** The tail: opens `watch=1` (same facets as history), reports rows and the
 * connection's state, and reconnects from the last seen seq when the server or
 * a proxy drops the stream — no gap, rows arrive exactly once per seq.
 * `request` cannot carry it (the body never ends); this reads the stream.
 *
 * Two terminal signals stop the loop instead of reconnecting silently:
 * - HTTP 410 `compacted` — the resume seq fell below the retention horizon.
 * - a terminal error control frame — a mid-stream failure sent as a problem
 *   object. The status flips to `stopped` with the problem's message. */
export function watchChanges(opts: {
  /** Resume above this seq; absent = the server starts at the head. */
  from?: number
  filter?: ChangeFeedFilter
  onRow: (row: ChangeRow) => void
  onStatus?: (status: WatchStatus, detail?: string) => void
  /** Fired on a `compacted` signal — the caller re-lists and re-subscribes
   * from the fresh head. */
  onCompacted?: () => void
}): WatchHandle {
  const ctrl = new AbortController()
  let cursor = opts.from

  const compacted = (detail: string) => {
    opts.onStatus?.("compacted", detail)
    opts.onCompacted?.()
  }

  void (async () => {
    let opened = false
    while (!ctrl.signal.aborted) {
      opts.onStatus?.(opened ? "retrying" : "connecting")
      try {
        const params = changesSearch(opts.filter)
        params.set("watch", "1")
        if (cursor !== undefined) params.set("from", String(cursor))
        const headers: Record<string, string> = {
          Accept: "application/x-ndjson",
          "X-Substrate-Actor": "console",
        }
        const token = getToken()
        if (token) headers.Authorization = `Bearer ${token}`
        const res = await fetch(`${rootPath("changes")}?${params}`, {
          headers,
          signal: ctrl.signal,
        })
        if (res.status === 401) {
          sessionExpired()
          opts.onStatus?.("off", "session expired")
          return
        }
        // A below-horizon resume is refused before the stream opens (a 410, not
        // a frame). Re-listing is the only recovery — do not reconnect in a loop.
        if (res.status === 410) {
          const err = envelopeError(res.status, await parseBody(res))
          compacted(err.message)
          return
        }
        if (!res.ok || !res.body) {
          throw envelopeError(res.status, await parseBody(res))
        }
        opened = true
        opts.onStatus?.("live")

        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ""
        let terminated = false
        for (;;) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          let nl = buffer.indexOf("\n")
          for (; nl >= 0; nl = buffer.indexOf("\n")) {
            const line = parseWatchLine(buffer.slice(0, nl))
            buffer = buffer.slice(nl + 1)
            if (!line) continue
            if (line.error) {
              // The reserved terminal frame: surface it and stop — a silent
              // reconnect would hammer a stream the server just closed.
              if (line.error.code === "compacted") compacted(line.error.message)
              else opts.onStatus?.("stopped", line.error.message)
              ctrl.abort()
              terminated = true
              break
            }
            if (line.bookmark !== undefined && cursor === undefined) {
              cursor = line.bookmark
            }
            if (line.row) {
              cursor = line.row.seq
              opts.onRow(line.row)
            }
          }
          if (terminated) break
        }
        if (terminated) return
        // The server ended a healthy stream — reconnect from the cursor.
      } catch (cause) {
        if (ctrl.signal.aborted) return
        const detail =
          cause instanceof ApiError
            ? cause.message
            : ((cause as Error).message ?? "stream failed")
        opts.onStatus?.("retrying", detail)
      }
      if (ctrl.signal.aborted) return
      await new Promise((resolve) => setTimeout(resolve, RETRY_DELAY_MS))
    }
  })()

  return {
    stop: () => {
      ctrl.abort()
      opts.onStatus?.("off")
    },
  }
}

/** Parse a non-streaming error body without throwing — the envelope may be a
 * problem object or, from a proxy, plain text. */
async function parseBody(res: Response): Promise<unknown> {
  try {
    const text = await res.text()
    return text ? JSON.parse(text) : undefined
  } catch {
    return undefined
  }
}
