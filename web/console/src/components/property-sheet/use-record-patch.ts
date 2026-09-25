/** One write shape for everything the record page changes in place: a PATCH
 * naming only the properties that moved, asserting the version the page read
 * (`ifVersion`), so an edit made against a stale page is refused rather than
 * silently winning. A state move is the same patch: patch is the one write
 * that may move a state, along a declared transition (engine/write.go). */

import { useMutation, useQueryClient } from "@tanstack/react-query"

import { splitKind } from "@/lib/api/http"
import { patchRecord } from "@/lib/api/records"
import { ApiError, type SubstrateRecord } from "@/lib/api/types"

/** What a refused write says, in words that tell the reader what to do. */
export function writeError(error: unknown): string {
  if (error instanceof ApiError && error.code === "conflict") {
    return "This record changed since you opened it. Reload the page to see the latest, then try again."
  }
  return error instanceof Error ? error.message : String(error)
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
    onSuccess: async () => {
      await Promise.all([
        client.invalidateQueries({
          queryKey: ["record", authority, pkg, name, record.id],
        }),
        client.invalidateQueries({
          queryKey: ["changes", "record", record.kind, record.id],
        }),
      ])
    },
  })
}
