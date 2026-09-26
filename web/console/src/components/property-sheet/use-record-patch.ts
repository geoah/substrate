/** One write shape for everything the record page changes in place: a PATCH
 * naming only the properties that moved, asserting the version the page read
 * (`ifVersion`), so an edit made against a stale page is refused rather than
 * silently winning. A state move is the same patch: patch is the one write
 * that may move a state, along a declared transition (engine/write.go). */

import { useMutation, useQueryClient } from "@tanstack/react-query"

import { splitKind } from "@/lib/api/http"
import { patchRecord, recordWriteReaches } from "@/lib/api/records"
import { ApiError, type SubstrateRecord } from "@/lib/api/types"

/** What a refused write says, in words that tell the reader what to do. */
export function writeError(error: unknown): string {
  if (error instanceof ApiError && error.code === "conflict") {
    return "This record changed since you opened it. Reload the page to see the latest, then try again."
  }
  return error instanceof Error ? error.message : String(error)
}

/** The versions this console wrote, so a page that watches the change feed
 * tells its own edit from one made under the reader. Bounded: only the
 * latest few matter, since a page compares the version it just re-read. */
const written: string[] = []
const WRITTEN_KEEP = 50

export function noteWrite(kind: string, id: string, version: number) {
  written.push(`${kind}/${id}@${version}`)
  if (written.length > WRITTEN_KEEP) written.shift()
}

/** Whether this console wrote `version` of the record. */
export function wroteHere(kind: string, id: string, version: number): boolean {
  return written.includes(`${kind}/${id}@${version}`)
}

export function useRecordPatch(record: SubstrateRecord) {
  const client = useQueryClient()
  const { authority, pkg, name } = splitKind(record.kind)
  return useMutation({
    mutationFn: (properties: Record<string, unknown>) =>
      patchRecord(authority, pkg, name, record.id, {
        properties,
        ifVersion: record.version,
      }),
    // Every read that can show the record, not only its page: a collection
    // holds its rows fresh for a while, and a list keyed on the kind (the
    // Agents page's providers) would otherwise go on showing the old values.
    onSuccess: (saved) => {
      if (typeof saved?.version === "number") {
        noteWrite(record.kind, record.id, saved.version)
      }
      return client.invalidateQueries({
        predicate: (q) =>
          recordWriteReaches(q.queryKey, record.kind, record.id),
      })
    },
  })
}
