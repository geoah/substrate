/** Change-request reads and THE decision write. Reads ride the generic record
 * machinery (keyset cursor, bounded count, single read with propertyMeta and
 * annotations); the write is one atomic PATCH at the request: the `decision`
 * transition, CAS'd on the request's own version, with the optional note
 * riding the same write as the `owner/note` annotation.
 *
 * Accepting is what applies the change: the transition runs `applyDiff` inside
 * the same transaction, branching on `op` (patch the target, mint the named
 * record, tombstone it). A refused apply fails the whole transition, leaves the
 * request proposed, and lands the server's account on it as
 * `substrate/conflict`, so a rejected call means re-read, never retry. */

import { CORE_AUTHORITY } from "./http"
import {
  patchRecord,
  recordCountQueryOptions,
  recordQueryOptions,
  recordsQueryOptions,
} from "./records"
import type { RecordFilter, SubstrateRecord } from "./types"
import {
  decisionPatch,
  type Decision,
  type Verdict,
} from "@/lib/changerequests"

export const CR_NAME = "recordpatchrequest"
export const CR_KIND = `${CORE_AUTHORITY}/recordpatchrequest`

/** `decision` is a state property; states filter through `properties` like any
 * other, one value as `eq` and several as `in`. No decision = the whole queue,
 * decided included. THIS kind's states, not a filter borrowed from the merge
 * queue: nothing holds the two declarations to the same set of states, so
 * sharing one filter would be a coupling neither kind promises. */
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

export function changeRequestsQueryOptions(opts: {
  decision?: Decision | Decision[]
  first: number
  /** The opaque keyset cursor a previous page returned, resent verbatim. */
  after?: string
}) {
  return recordsQueryOptions({
    authority: CORE_AUTHORITY,
    name: CR_NAME,
    first: opts.first,
    after: opts.after,
    filter: decisionFilter(opts.decision),
    // Newest write first: deciding IS the update, so the queue reads fresh
    // proposals and fresh decisions alike from the top.
    orderBy: "updatedAt:desc",
  })
}

export function changeRequestCountQueryOptions(
  decision?: Decision | Decision[]
) {
  return recordCountQueryOptions(
    CORE_AUTHORITY,
    CR_NAME,
    decisionFilter(decision)
  )
}

export function pendingChangeCountQueryOptions() {
  return changeRequestCountQueryOptions("proposed")
}

/** One request whole. The single read is the only surface carrying
 * `propertyMeta` (who proposed, who decided) and `annotations` (the note, and
 * the server's conflict record after a refused apply). */
export function changeRequestQueryOptions(id: string) {
  return recordQueryOptions(CORE_AUTHORITY, CR_NAME, id)
}

/** The single atomic submit. `version` is the request AS LOADED: the write path
 * refuses a decision that carries no `ifVersion`, because a reviewer decides
 * the envelope they read and a concurrent write must not slip under it. A
 * `conflict` rejection therefore means one of two things, both answered by
 * re-reading: the request moved, or the accept's own apply was refused. */
export function submitDecision(
  id: string,
  verdict: Verdict,
  version: number,
  note?: string
): Promise<SubstrateRecord> {
  return patchRecord(
    CORE_AUTHORITY,
    CR_NAME,
    id,
    decisionPatch(verdict, version, note)
  )
}
