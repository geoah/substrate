/** The dashboard's aggregation seams — the home page composes the other
 * surfaces' queries (MR queue, registry) and adds only what nothing else
 * serves:
 *
 * - a single recent-changes page (no watch, no paging — a 60s refetch keeps
 *   the dashboard honest without the changelog's live machinery), and
 * - per-authority record counts behind ONE shared concurrency gate. A count is
 *   a bounded keyset walk (countRecords), and the browser gives an origin six
 *   HTTP/1.1 connections — ungated, a repository with dozens of kinds stampedes
 *   the pipe and the other zones queue behind its probes. The gate is
 *   module-level so the ceiling holds across authorities; counts cache for
 *   minutes — the dashboard is a glance, not a ledger. */

import { queryOptions } from "@tanstack/react-query"

import { fetchChangesPage } from "./changes"
import { countRecords, type RecordCount } from "./records"
import { ApiError, type KindInfo } from "./types"

// ── the recent changelog slice ─────────────────────────────────────────────────────────────────────────────────────────────

/** The activity card is FLAT (the fold died with the 2026-08-06 ruling), so
 * its depth is measured in rows: one page of the newest changes. */
export const RECENT_CHANGES_PAGE = 30

export function recentChangesQueryOptions() {
  return queryOptions({
    queryKey: ["overview", "recent-changes"],
    queryFn: async ({ signal }) =>
      (await fetchChangesPage({ first: RECENT_CHANGES_PAGE, signal })).changes,
    staleTime: 30_000,
    // The dashboard never watches — a minute of staleness is the deal.
    refetchInterval: 60_000,
  })
}

// ── bounded concurrency ─────────────────────────────────────────────────────

/** A counting semaphore: at most `limit` tasks run at once; the rest wait
 * their turn in FIFO order. One instance shared across queries is what makes
 * the ceiling global — TanStack Query fires every mounted queryFn eagerly. */
export class Semaphore {
  private active = 0
  private readonly waiters: Array<() => void> = []
  private readonly limit: number

  constructor(limit: number) {
    this.limit = limit
  }

  async run<T>(task: () => Promise<T>): Promise<T> {
    if (this.active >= this.limit) {
      await new Promise<void>((resolve) => this.waiters.push(resolve))
    }
    this.active++
    try {
      return await task()
    } finally {
      this.active--
      this.waiters.shift()?.()
    }
  }
}

// ── per-authority record counts ─────────────────────────────────────────────

export interface KindCount {
  kind: KindInfo
  /** Undefined when the substrate refused this one collection — one forbidden
   * kind must not blank its whole authority's counts. A bounded keyset walk,
   * so `capped` collections read as `N+`. */
  count?: RecordCount
}

/** Probe walks in flight at once, across the whole dashboard. Each walk is
 * itself serial, so this is also the count zone's whole connection budget —
 * three of the browser's six, leaving the pipe open for the other zones. */
export const COUNT_CONCURRENCY = 3

const countGate = new Semaphore(COUNT_CONCURRENCY)

/** All of one authority's kind counts as one cached answer, name-sorted like
 * the sidebar. Cached for minutes: counts back a glanceable zone, and every
 * tile is a door into the browse where the count is exact and fresher. */
export function authorityCountsQueryOptions(
  authority: string,
  kinds: KindInfo[],
  gate: Semaphore = countGate
) {
  const sorted = kinds
    .filter((k) => k.authority === authority)
    .sort((a, b) => a.name.localeCompare(b.name))
  return queryOptions({
    queryKey: ["overview", "counts", authority, sorted.map((k) => k.plural)],
    queryFn: ({ signal }) =>
      Promise.all(
        sorted.map(async (k): Promise<KindCount> => {
          try {
            return {
              kind: k,
              count: await gate.run(() =>
                countRecords(k.authority, k.plural, undefined, signal)
              ),
            }
          } catch (cause) {
            // An API refusal is that kind's answer ("—"), not the authority's
            // failure; anything else (network, abort) stays an error.
            if (cause instanceof ApiError && cause.status >= 400) {
              return { kind: k }
            }
            throw cause
          }
        })
      ),
    staleTime: 5 * 60_000,
  })
}
