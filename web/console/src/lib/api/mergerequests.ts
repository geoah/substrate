/** Merge-request reads and THE verdict write — the console's only mutation
 * besides login. Reads ride the generic record machinery (keyset cursor,
 * bounded count, single read with propertyMeta); the write is one atomic PATCH
 * at the request: the `decision` transition, with the optional note riding the
 * same write as the `owner/note` annotation. Accepting runs `applyMerge` inside
 * the same transaction — a stale request (already merged, deleted, kind
 * mismatch) fails the transition whole and annotates the request. */

import {
  recordsQueryOptions,
  recordCountQueryOptions,
  recordQueryOptions,
} from "./records"
import {
  collectionPath,
  CORE_AUTHORITY,
  CORE_PACKAGE,
  CORE_PACKAGE_NAME,
  request,
  seg,
} from "./http"
import type { SubstrateRecord, RecordFilter } from "./types"
import {
  verdictPatch,
  type Decision,
  type MergeVerdict,
} from "@/lib/mergerequests"

export const MR_NAME = "recordmergerequest"
export const MR_KIND = `${CORE_PACKAGE}/recordmergerequest`

/** `decision` is a state property; states filter through `properties` like any
 * other (verified live) — one value as `eq`, several as `in`. No decision =
 * the whole queue, resolved included. */
export function decisionFilter(
  decision?: Decision | Decision[]
): RecordFilter | undefined {
  const list = Array.isArray(decision) ? decision : decision ? [decision] : []
  if (!list.length) return undefined
  return {
    properties: {
      decision: list.length === 1 ? { eq: list[0] } : { in: list },
    },
  }
}

export function mergeRequestsQueryOptions(opts: {
  decision?: Decision | Decision[]
  first: number
  /** The opaque keyset cursor a previous page returned, resent verbatim. */
  after?: string
}) {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    package: CORE_PACKAGE_NAME,
    name: MR_NAME,
    first: opts.first,
    after: opts.after,
    filter: decisionFilter(opts.decision),
    // Newest write first: deciding IS the update, so the queue reads fresh
    // suggestions and fresh verdicts alike from the top.
    orderBy: "updatedAt:desc",
  })
}

export function mergeRequestCountQueryOptions(
  decision?: Decision | Decision[]
) {
  return recordCountQueryOptions(
    CORE_AUTHORITY,
    CORE_PACKAGE_NAME,
    MR_NAME,
    decisionFilter(decision)
  )
}

/** The sidebar badge and the queue's default segment share this count. */
export function pendingMergeCountQueryOptions() {
  return mergeRequestCountQueryOptions("proposed")
}

/** One request whole — the single read is the only surface carrying
 * `propertyMeta` (who proposed, who decided) and `annotations` (the note, and
 * the server's conflict record after a refused apply). */
export function mergeRequestQueryOptions(id: string) {
  return recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, MR_NAME, id)
}

/** The single atomic submit. On accept the substrate applies the merge in this
 * same transaction; a 409/guard rejection means the request moved under us —
 * re-read it, the server annotates why. */
export function submitVerdict(
  id: string,
  verdict: MergeVerdict,
  note?: string
): Promise<SubstrateRecord> {
  return request<SubstrateRecord>(
    "PATCH",
    `${collectionPath(CORE_AUTHORITY, CORE_PACKAGE_NAME, MR_NAME)}/${seg(id)}`,
    verdictPatch(verdict, note)
  )
}
