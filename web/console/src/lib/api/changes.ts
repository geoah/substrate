/** The cross-collection change feed (`GET /api/v1/changes`), read
 * three ways:
 *
 * - **History**: newest-first pages addressed by `before=<seq>`. The changelog
 *   is seq-addressed, so the feed pages older by cursor rather than by an
 *   offset jump.
 * - **Time seek**: the wire has no time-range parameter, but seq order is time
 *   order, so "history until T" is a binary search over the seq axis — a dozen
 *   one-row probes finding the newest row at or before T.
 * - **Live tail**: `watch=1`, chunked HTTP ndjson: one
 *   `{"bookmark":N,"generation":"…"}` line, then one JSON row per committed
 *   change, `{}` heartbeats while idle. A resume hands back the pair: a seq
 *   is a position in ONE history generation, and the server refuses a cursor
 *   from a generation it does not hold (a restore of an older repository
 *   directory) instead of skipping the writes in between.
 *
 * Server-side facets (parseChangeFilter): `kinds`, `actors`, `ops`,
 * `recordId`+`recordKind`, `q` — history and watch honor the same set. Time is
 * NOT on the wire: it rides the seek above. A `kind` is a reference
 * (`<authority>/<name>`); its `/` percent-encodes as `%2F` in the query. */

import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query"

import { rootPath, envelopeError, request } from "./http"
import { getToken, sessionExpired } from "./session"
import {
  ApiError,
  type ChangePage,
  type ChangeRow,
  type ProblemDetail,
} from "./types"

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

export async function fetchChangesPage(opts: {
  /** Rows strictly below this seq; absent/0 = from the head. */
  before?: number
  /** The history generation `before` was read under (the previous page's).
   * A `before` above 0 without it is refused by the server. */
  generation?: string
  first?: number
  filter?: ChangeFeedFilter
  signal?: AbortSignal
}): Promise<ChangePage> {
  const params = changesSearch(opts.filter)
  params.set("first", String(opts.first ?? CHANGELOG_PAGE))
  if (opts.before && opts.before > 0) {
    params.set("before", String(opts.before))
    if (opts.generation) params.set("generation", opts.generation)
  }
  // The page is the wire shape itself: the cursor is the CONTINUATION (the
  // oldest seq on the page, the next `before`), and it advances past
  // scope-filtered rows, so a short page is NOT the end; the walk continues
  // while a cursor comes back and stops when it is omitted.
  return request<ChangePage>(
    "GET",
    `${rootPath("changes")}?${params}`,
    undefined,
    { signal: opts.signal }
  )
}

export interface ChangesFeedOpts {
  first?: number
  /** Start below this seq (a time seek's answer); 0 = the head. */
  startBefore?: number
  /** The generation `startBefore` was read under; required with one above 0. */
  startGeneration?: string
  /** Stop paging once a page reaches rows older than this instant — the
   * client half of the time range the wire cannot express. */
  sinceMs?: number
}

/** A history page's position: the `before` to read under and the history
 * generation it belongs to, which every continuation resends. */
export interface HistoryPosition {
  before: number
  generation?: string
}

export function changesInfiniteOptions(
  filter: ChangeFeedFilter = {},
  opts: ChangesFeedOpts = {}
) {
  const first = opts.first ?? CHANGELOG_PAGE
  const startBefore = opts.startBefore ?? 0
  const start: HistoryPosition = {
    before: startBefore,
    generation: opts.startGeneration,
  }
  return infiniteQueryOptions({
    queryKey: [
      "changes",
      "feed",
      filterKey(filter),
      {
        first,
        startBefore,
        startGeneration: opts.startGeneration ?? null,
        sinceMs: opts.sinceMs ?? null,
      },
    ],
    queryFn: ({ pageParam, signal }) =>
      fetchChangesPage({
        before: pageParam.before > 0 ? pageParam.before : undefined,
        generation: pageParam.generation,
        first,
        filter,
        signal,
      }),
    initialPageParam: start,
    getNextPageParam: (last): HistoryPosition | undefined => {
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
      return { before: last.cursor, generation: last.generation }
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

/** Immutable answer: history never moves under a fixed instant. The answer
 * carries the generation the probes ran under, so the pages that follow it
 * resume in the same history. */
export function seekQueryOptions(untilMs: number) {
  return queryOptions({
    queryKey: ["changes", "seek", untilMs],
    queryFn: async ({ signal }): Promise<HistoryPosition> => {
      const headPage = await fetchChangesPage({ first: 1, signal })
      const generation = headPage.generation
      const one = async (before: number) =>
        (await fetchChangesPage({ before, generation, first: 1, signal }))
          .changes[0]
      const before = await seekBoundary(
        async (maxSeq) => one(maxSeq + 1),
        headPage.changes[0],
        untilMs
      )
      return { before, generation }
    },
    staleTime: Infinity,
  })
}

// ── the live tail ───────────────────────────────────────────────────────────

/** `compacted` = the resume cursor no longer addresses this changelog (it fell
 * below the retention horizon, or the history was replaced and its generation
 * is not the server's); the client must re-list from a fresh head. `stopped`
 * = a terminal error frame (or a non-retriable open failure) ended the
 * stream, with no silent reconnect loop. */
export type WatchStatus =
  "connecting" | "live" | "retrying" | "compacted" | "stopped" | "off"

/** The problem object the substrate carries in a REST envelope AND the watch
 * terminal error frame. */
export interface WatchError {
  code: string
  message: string
  problems?: string[]
  problemDetails?: ProblemDetail[]
}

export interface WatchLine {
  row?: ChangeRow
  bookmark?: number
  /** The history generation the bookmark's seq belongs to; a resume sends
   * both back. */
  generation?: string
  /** The reserved terminal error control frame — a mid-stream failure travels
   * as this one problem object rather than a silent EOF. */
  error?: WatchError
}

/** One ndjson line: a line WITH a `seq` is a change row;
 * a line WITHOUT is a CONTROL frame keyed by its field: `bookmark` opens
 * (with `generation` beside it), `{}` is the idle heartbeat, `error` is the
 * terminal failure. Blank lines, heartbeats and garbage read as nothing. */
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
  if (typeof obj.bookmark === "number") {
    return {
      bookmark: obj.bookmark,
      generation:
        typeof obj.generation === "string" ? obj.generation : undefined,
    }
  }
  if (obj.error && typeof obj.error === "object") {
    const e = obj.error as Record<string, unknown>
    return {
      error: {
        code: typeof e.code === "string" ? e.code : "internal",
        message: typeof e.message === "string" ? e.message : "stream failed",
        problems: Array.isArray(e.problems)
          ? (e.problems.filter((p) => typeof p === "string") as string[])
          : undefined,
        problemDetails: Array.isArray(e.problemDetails)
          ? (e.problemDetails.filter(
              (p) => typeof p === "object" && p !== null
            ) as ProblemDetail[])
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
 * - HTTP 410 `compacted`: the resume cursor no longer addresses this
 *   changelog (below the retention horizon, above the head, or from a history
 *   generation the server does not hold).
 * - a terminal error control frame — a mid-stream failure sent as a problem
 *   object. The status flips to `stopped` with the problem's message. */
export function watchChanges(opts: {
  /** Resume above this seq; absent = the server starts at the head. */
  from?: number
  /** The history generation `from` was read under (a list page's or a
   * history page's `generation`, or an earlier bookmark's). A `from` above 0
   * without it is refused by the server. */
  generation?: string
  filter?: ChangeFeedFilter
  onRow: (row: ChangeRow) => void
  onStatus?: (status: WatchStatus, detail?: string) => void
  /** Fired on a `compacted` signal — the caller re-lists and re-subscribes
   * from the fresh head. */
  onCompacted?: () => void
}): WatchHandle {
  const ctrl = new AbortController()
  let cursor = opts.from
  let generation = opts.generation

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
        if (cursor !== undefined) {
          params.set("from", String(cursor))
          if (generation !== undefined) params.set("generation", generation)
        }
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
        // An unresumable cursor is refused before the stream opens (a 410, not
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
            if (line.bookmark !== undefined) {
              // The bookmark names the generation every later reconnect
              // resumes under; the seq is taken only when none was supplied.
              if (cursor === undefined) cursor = line.bookmark
              if (line.generation !== undefined) generation = line.generation
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
