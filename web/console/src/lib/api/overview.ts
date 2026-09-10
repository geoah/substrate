/** The dashboard's aggregation seams — the home page composes the other
 * surfaces' queries (MR queue, registry) and adds only what nothing else
 * serves:
 *
 * - a single recent-changes page (no watch, no paging — a 60s refetch keeps
 *   the dashboard honest without the changelog's live machinery), and
 * - per-authority record counts, walked ONE AT A TIME. A count is a bounded
 *   keyset walk (countRecords), and the browser gives an origin six HTTP/1.1
 *   connections; a repository with dozens of kinds firing them all at once
 *   stampedes the pipe and the other zones queue behind its probes. So each
 *   authority's zone holds one connection, and the ceiling is the number of
 *   zones on the page. Counts cache for minutes — the dashboard is a glance,
 *   not a ledger. */

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

// ── per-authority record counts ─────────────────────────────────────────────

export interface KindCount {
  kind: KindInfo
  /** Undefined when the substrate refused this one collection — one forbidden
   * kind must not blank its whole authority's counts. A bounded keyset walk,
   * so `capped` collections read as `N+`. */
  count?: RecordCount
}

/** All of one authority's kind counts as one cached answer, name-sorted like
 * the sidebar. Cached for minutes: counts back a glanceable zone, and every
 * tile is a door into the browse where the count is exact and fresher. */
export function authorityCountsQueryOptions(
  authority: string,
  kinds: KindInfo[]
) {
  const sorted = kinds
    .filter((k) => k.authority === authority)
    .sort(
      (a, b) =>
        a.package.localeCompare(b.package) || a.name.localeCompare(b.name)
    )
  return queryOptions({
    queryKey: [
      "overview",
      "counts",
      authority,
      sorted.map((k) => `${k.package}/${k.name}`),
    ],
    queryFn: async ({ signal }) => {
      const counts: KindCount[] = []
      // One probe at a time, in the sorted order: a zone holds one connection
      // however many kinds its authority has.
      for (const k of sorted) {
        try {
          counts.push({
            kind: k,
            count: await countRecords(
              k.authority,
              k.package,
              k.name,
              undefined,
              signal
            ),
          })
        } catch (cause) {
          // An API refusal is that kind's answer ("—"), not the authority's
          // failure; anything else (network, abort) stays an error.
          if (cause instanceof ApiError && cause.status >= 400) {
            counts.push({ kind: k })
            continue
          }
          throw cause
        }
      }
      return counts
    },
    staleTime: 5 * 60_000,
  })
}
