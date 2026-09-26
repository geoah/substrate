/** One write shape for everything the record page changes in place: a PATCH
 * naming only the properties that moved, asserting the version the page read
 * (`ifVersion`), so an edit made against a stale page is refused rather than
 * silently winning. A state move is the same patch: patch is the one write
 * that may move a state, along a declared transition (engine/write.go).
 *
 * Inside a `SheetDraftContext` the record is not stored yet: the same write
 * lands in the draft and nothing reaches the server. */

import { useState } from "react"
import { useMutation, useQueryClient } from "@tanstack/react-query"

import { useSheetDraft } from "./draft"
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

/** The version an editor opened on, held for as long as it stays open. The
 * record page re-reads a record that moves under the reader, so the record
 * an open editor is handed can be newer than the one its draft began from;
 * writing that draft against the newer version would silently overwrite the
 * other write, where asserting the one it began from is refused as a
 * conflict. For an editor that mounts when editing begins. */
export function useEditBase(record: SubstrateRecord): number {
  const [base] = useState(record.version)
  return base
}

/** `base`: the version the edit began from (see useEditBase); without one, a
 * write asserts the version on the page now, which is right only for a
 * one-gesture write that shows no draft (a checkbox). */
export function useRecordPatch(record: SubstrateRecord, base?: number) {
  const client = useQueryClient()
  const draft = useSheetDraft()
  const { authority, pkg, name } = splitKind(record.kind)
  return useMutation({
    mutationFn: async (properties: Record<string, unknown>) => {
      if (draft) {
        draft.write(properties)
        return record
      }
      return patchRecord(authority, pkg, name, record.id, {
        properties,
        ifVersion: base ?? record.version,
      })
    },
    // Every read that can show the record, not only its page: a collection
    // holds its rows fresh for a while, and a list keyed on the kind (the
    // Agents page's providers) would otherwise go on showing the old values.
    onSuccess: (saved) => {
      if (draft) return
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
