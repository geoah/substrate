/** Merge-request reads and THE verdict write — the console's only mutation
 * besides login. The read is the generic single record read, with
 * propertyMeta; the write is one atomic PATCH at the request: the `decision`
 * transition, with the optional note riding the same write as the
 * `owner/note` annotation. Accepting runs `applyMerge` inside
 * the same transaction — a stale request (already merged, deleted, kind
 * mismatch) fails the transition whole and annotates the request. */

import { recordQueryOptions } from "./records"
import {
  collectionPath,
  CORE_AUTHORITY,
  CORE_PACKAGE_NAME,
  request,
  seg,
} from "./http"
import type { SubstrateRecord } from "./types"
import { verdictPatch, type MergeVerdict } from "@/lib/mergerequests"

export const MR_NAME = "recordmergerequest"

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
