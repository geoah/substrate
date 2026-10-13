/** Change-request reads and THE decision write. The read is the generic
 * single record read, with propertyMeta and annotations; the write is one
 * atomic PATCH at the request: the `decision` transition, CAS'd on the
 * request's own version, with the optional note riding the same write as the
 * `owner/note` annotation.
 *
 * Accepting is what applies the change: the transition runs `applyDiff` inside
 * the same transaction, branching on `op` (patch the target, mint the named
 * record, tombstone it). A refused apply fails the whole transition, leaves the
 * request proposed, and lands the server's account on it as
 * `substrate/conflict`, so a rejected call means re-read, never retry. An
 * accept may carry the owner's adjusted values as `adjustedDiff`, applied
 * instead of the proposed `diff` (decision 0115). */

import { CORE_AUTHORITY, CORE_PACKAGE_NAME } from "./http"
import { patchRecord, recordQueryOptions } from "./records"
import type { SubstrateRecord } from "./types"
import { decisionPatch, type Verdict } from "@/lib/changerequests"

export const CR_NAME = "recordpatchrequest"

/** One request whole. The single read is the only surface carrying
 * `propertyMeta` (who proposed, who decided) and `annotations` (the note, and
 * the server's conflict record after a refused apply). */
export function changeRequestQueryOptions(id: string) {
  return recordQueryOptions(CORE_AUTHORITY, CORE_PACKAGE_NAME, CR_NAME, id)
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
  note?: string,
  adjustedDiff?: Record<string, unknown>
): Promise<SubstrateRecord> {
  return patchRecord(
    CORE_AUTHORITY,
    CORE_PACKAGE_NAME,
    CR_NAME,
    id,
    decisionPatch(verdict, version, note, adjustedDiff)
  )
}
