/** What a read does when the server answers `410 compacted`: it handed out a
 * position (a list's `after` token, a history page's `before` under its
 * generation, a watch's `from`) that it can no longer resume, through
 * nothing the console did (docs/api.md, "Pagination"). What was read from
 * that position is dropped and the read starts again from its first page,
 * or a watch opens again at the head; the fresh answer names the head and
 * generation to go on from, and the refused position is never sent again.
 *
 * RULE W, ONE RECOVERY A MINUTE, BY TIME ALONE. A refusal starts a
 * recovery. Another refusal within `RECOVERY_ALLOWANCE_MS` of the START of
 * the last recovery is the failure, shown as one, never a second recovery,
 * and one at that long or later recovers again. Nothing else makes the
 * allowance whole: not a bookmark, a list that lands, a delivered change or
 * the reader mounting again.
 *
 * A reader comes under the rule by registering a `ResumingReader`: its key,
 * a drop of the position it saved and a way to show its failure, all three
 * required. `recoverOrFail` is the one place a refusal is answered, and it
 * calls the drop on every refusal and the failure on the last. The
 * allowance is kept here under the reader's key, not in the reader, so it
 * outlives a mount. The readers: a walk read whole (`walkPages`,
 * `startOverWhenRefused`), an infinite query paged with Load more
 * (`restartRefusedWalks`), a watch (`watchChanges`, which `followChanges`
 * opens) and a History feed's runs (`useHistoryFeed`).
 * compacted.source.test.ts holds every place that sends a continuation to
 * one of them.
 *
 * A refusal of a first page, which resumed nothing, is thrown as it is, and
 * so is every other refusal of a continuation (a `422` for another filter, a
 * `404` for a kind that went). */

import type { Query, QueryClient, QueryKey } from "@tanstack/react-query"

import { ApiError } from "./types"

/** How long after a recovery's start another refusal is the failure. */
export const RECOVERY_ALLOWANCE_MS = 60_000

/** The rule's one clock. Tests step `Date.now` (fake timers or a spy). */
function now(): number {
  return Date.now()
}

/** What a reader registers to come under RULE W. Each part is required: a
 * reader that could not drop its position would send it again, and one
 * that could not show its failure would fail as nothing. */
export interface ResumingReader {
  /** The reader's identity, never its mount's: what it walks, follows or
   * pages (a feed's filter, a query's key). Its allowance is kept under it,
   * so the reader mounted again within the minute meets the allowance it
   * left, with the same start. Two readers that keep different positions
   * take different keys. */
  key: string
  /** Drops every position the reader saved from the refused read (a cached
   * head and generation, a query's pages, a run's position), so nothing, on
   * this mount or a later one, sends it again. Called on every refusal, the
   * failure included, before either. */
  drop: () => void
  /** Shows the failure where the reader is read, with the server's refusal.
   * Called on the refusal that is the failure, after the drop. */
  fail: (refusal: ApiError) => void
}

/** The drop of a reader that saves no position: a watch that opens at the
 * server's head, over reads that are first pages. */
export function nothingSaved(): void {}

/** When each reader's last recovery started, by key. An entry a minute old
 * is a whole allowance, so each refusal prunes those first: the map holds
 * only the readers whose recovery started within the minute before the
 * last refusal, however long the console runs. */
const recoveries = new Map<string, number>()

/** Answers a `compacted` refusal of `reader`'s position: drops what the
 * reader saved, then starts a recovery (true: the reader reads again from
 * its first page, or opens its watch at the head) or, within a minute of
 * the start of its last one, shows the failure (false). */
export function recoverOrFail(
  reader: ResumingReader,
  refusal: ApiError
): boolean {
  reader.drop()
  const at = now()
  for (const [key, startedAt] of recoveries)
    if (at - startedAt >= RECOVERY_ALLOWANCE_MS) recoveries.delete(key)
  if (recoveries.has(reader.key)) {
    reader.fail(refusal)
    return false
  }
  recoveries.set(reader.key, at)
  return true
}

/** Positions a reader dropped that its surface still holds a copy of (a
 * list read's head and generation, cached by a caller the reader does not
 * own), by reader and position: the reader never resumes one again. One
 * entry per refusal of a reader that keeps one. */
const dropped = new Set<string>()

/** Records that `position` was dropped by the reader `key`, never to be
 * resumed again. */
export function dropPosition(key: string, position: string): void {
  dropped.add(`${key} ${position}`)
}

/** Whether the reader `key` dropped `position`. */
export function wasDropped(key: string, position: string): boolean {
  return dropped.has(`${key} ${position}`)
}

/** Forgets every allowance and every dropped position: a session for
 * another repository has readers of its own (main.tsx), and each test starts
 * whole. */
export function forgetRecoveries(): void {
  recoveries.clear()
  dropped.clear()
  unshown.length = 0
}

/** A failure no surface shows yet. */
export interface UnshownFailure {
  key: string
  refusal: ApiError
  at: number
}

/** How many unshown failures are kept, newest last. */
const UNSHOWN_KEPT = 20

const unshown: UnshownFailure[] = []

/** The `fail` of a reader whose surface has no status line yet (issue
 * #1378: Home's recent changes, the collection and record pages, the Agents
 * pages): the failure is kept where the shell can read it
 * (`unshownFailures`), and nothing renders it. */
export function reportUnshown(key: string): (refusal: ApiError) => void {
  return (refusal) => {
    unshown.push({ key, refusal, at: now() })
    if (unshown.length > UNSHOWN_KEPT) unshown.shift()
  }
}

/** The newest failures no surface showed, oldest first. */
export function unshownFailures(): readonly UnshownFailure[] {
  return [...unshown]
}

/** The drop of a reader whose position is a query's data (an infinite
 * query's pages, the page whose head and generation a watch resumes): the
 * query is emptied, and nothing reads it again; a recovery reads it from
 * its first page, and the failure holds it at the refusal (`holdAtRefusal`).
 * A read of it already in flight is cancelled, so it cannot land after the
 * failure with pages of its own. */
export function emptyQuery(client: QueryClient, queryKey: QueryKey): void {
  const query = client.getQueryCache().find({ queryKey, exact: true })
  if (!query) return
  void query.cancel({ revert: true })
  query.setState({
    ...query.state,
    data: undefined,
    dataUpdatedAt: 0,
    error: null,
    fetchStatus: "idle",
    status: "pending",
  })
}

/** The failure of a reader whose position was a query's data: the query
 * holds the refusal as its error and no data, so its surface shows the
 * error, and Load more, Try again or a mount reads the first page. */
export function holdAtRefusal(
  client: QueryClient,
  queryKey: QueryKey,
  refusal: ApiError
): void {
  const query = client.getQueryCache().find({ queryKey, exact: true })
  if (!query) return
  void query.cancel({ revert: true })
  query.setState({
    ...query.state,
    data: undefined,
    dataUpdatedAt: 0,
    error: refusal,
    errorUpdatedAt: now(),
    errorUpdateCount: query.state.errorUpdateCount + 1,
    fetchStatus: "idle",
    status: "error",
  })
}

/** Whether the server refused a position it can no longer resume. */
export function isCompacted(err: unknown): err is ApiError {
  return err instanceof ApiError && err.code === "compacted"
}

/** What a walk registers: its key and its drop. A walk shows its failure
 * by throwing the refusal, which the query that runs it shows as its
 * error. */
export type WalkReader = Omit<ResumingReader, "fail">

/** Runs `walk`, which reads from its first page and calls `resuming` before
 * it sends a position an earlier read answered (or one the console kept). A
 * `compacted` refusal after that drops what `reader` saved and runs the
 * walk again from the start while its allowance holds; a refusal within a
 * minute of the last start-over is thrown, and so are a refusal before
 * anything was resumed and every other refusal. */
export async function startOverWhenRefused<T>(
  reader: WalkReader,
  walk: (resuming: () => void) => Promise<T>
): Promise<T> {
  // The failure is the refusal thrown below.
  const registered: ResumingReader = { ...reader, fail: () => {} }
  for (;;) {
    let resumed = false
    try {
      return await walk(() => {
        resumed = true
      })
    } catch (err) {
      if (!resumed || !isCompacted(err) || !recoverOrFail(registered, err))
        throw err
    }
  }
}

/** What a walk read: every page in order, and whether it reached the list's
 * end within its page budget. */
interface Walked<P> {
  pages: P[]
  complete: boolean
}

/** Reads a list page by page from the first: `read` reads the page at a
 * cursor (`undefined` for the first), `next` names the cursor after a page
 * (`undefined` at the end), and at most `maxPages` are read. `key` names
 * the list walked, the walk's allowance. A refusal drops every page the
 * refused run read, so a walk started over keeps nothing of it: nothing is
 * counted or shown twice. */
export function walkPages<P, C>(
  key: string,
  read: (cursor: C | undefined) => Promise<P>,
  next: (page: P) => C | undefined,
  maxPages = Infinity
): Promise<Walked<P>> {
  let pages: P[] = []
  const drop = () => {
    pages = []
  }
  return startOverWhenRefused(
    { key: `walk ${key}`, drop },
    async (resuming) => {
      let cursor: C | undefined
      while (pages.length < maxPages) {
        const page = await read(cursor)
        pages.push(page)
        cursor = next(page)
        if (cursor === undefined) return { pages, complete: true }
        resuming()
      }
      return { pages, complete: false }
    }
  )
}

/** Holds every infinite query to RULE W, each under the allowance of its
 * query key. A later page refused `compacted` drops the query's pages, and
 * with them every continuation. While the allowance holds the query starts
 * over (`resetQueries`): the reader sees the list's loading state, then a
 * fresh first page, and no error. A refusal within a minute of that
 * start-over, on this mount or a later one, is the failure: the query holds
 * the refusal as its error with no pages, so neither Load more nor Try
 * again can send the refused cursor; each reads the first page. A refusal
 * of a query that holds no pages stays the error it is.
 *
 * Returns the unsubscribe. main.tsx installs it on the console's one client,
 * so every infinite query (every one walks a cursor) follows it. */
export function restartRefusedWalks(client: QueryClient): () => void {
  return client.getQueryCache().subscribe((event) => {
    if (event.type !== "updated" || event.action.type !== "error") return
    // The cache's events carry their query with its types erased.
    const query = event.query as Query
    const error: unknown = event.action.error
    if (!isCompacted(error) || !holdsPages(query)) return
    // After the refused fetch settles: a change inside it would race the
    // error it is still dispatching. The observers hear of the error on a
    // later task, by when the query has moved on.
    queueMicrotask(() => {
      if (query.state.error !== error) return
      const { queryKey } = query
      const recovers = recoverOrFail(
        {
          key: `query ${query.queryHash}`,
          drop: () => emptyQuery(client, queryKey),
          fail: (refusal) => holdAtRefusal(client, queryKey, refusal),
        },
        error
      )
      if (recovers) void client.resetQueries({ queryKey, exact: true })
    })
  })
}

/** Whether a query is an infinite one holding pages. */
function holdsPages(query: Query): boolean {
  const data = query.state.data as
    { pages?: unknown; pageParams?: unknown } | undefined
  return (
    Array.isArray(data?.pages) &&
    Array.isArray(data.pageParams) &&
    data.pages.length > 0
  )
}
