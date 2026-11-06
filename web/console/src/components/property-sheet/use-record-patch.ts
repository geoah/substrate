/** One write shape for everything the record page changes in place: a PATCH
 * naming only the properties that moved, asserting the version the page read
 * (`ifVersion`), so an edit made against a stale page is refused rather than
 * silently winning. A state move is the same patch: patch is the one write
 * that may move a state, along a declared transition (engine/write.go).
 *
 * Inside a `SheetDraftContext` the record is not stored yet: the same write
 * lands in the draft and nothing reaches the server. */

import { useRef, useState } from "react"
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

/** The key every in-place write to one record runs under, so a surface can
 * hold its next edit until the write before it has landed and the page has
 * re-read the version that write made. */
export function recordPatchKey(record: SubstrateRecord) {
  return ["record-patch", record.kind, record.id] as const
}

/** Every read that can show the record, not only its page: a collection
 * holds its rows fresh for a while, and a list keyed on the kind (the Agents
 * page's providers) would otherwise go on showing the old values. */
function afterWrite(
  client: ReturnType<typeof useQueryClient>,
  record: SubstrateRecord,
  saved: SubstrateRecord | undefined
) {
  if (typeof saved?.version === "number") {
    noteWrite(record.kind, record.id, saved.version)
  }
  return client.invalidateQueries({
    predicate: (q) => recordWriteReaches(q.queryKey, record.kind, record.id),
  })
}

/** `base`: the version the edit began from (see useEditBase); without one, a
 * write asserts the version on the page now, which is right only for a
 * one-gesture write that shows no draft (a checkbox). */
export function useRecordPatch(record: SubstrateRecord, base?: number) {
  const client = useQueryClient()
  const draft = useSheetDraft()
  const { authority, pkg, name } = splitKind(record.kind)
  return useMutation({
    mutationKey: recordPatchKey(record),
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
    onSuccess: (saved) => {
      if (draft) return
      return afterWrite(client, record, saved)
    },
  })
}

/** One secret property (an API key) written in place, under `ifVersion:
 * base` like useRecordPatch, but the value never enters the mutation cache:
 * TanStack keeps a mutation's variables for its gc time after the form that
 * sent them closed, so a key passed to `mutate` would sit there in plain
 * text. The value rides a ref the request reads once and clears, the
 * mutation takes no variables, and the settled mutation is removed from the
 * cache whether the write succeeded or failed. It writes straight to the
 * server: a secret is never drafted. */
export function useSecretPatch(
  record: SubstrateRecord,
  property: string,
  base: number
) {
  const client = useQueryClient()
  const secret = useRef<string | null>(null)
  const { authority, pkg, name } = splitKind(record.kind)
  // Under the record's patch key, so a surface that holds its next edit
  // until a write lands sees this one; one level deeper, so removing it
  // never touches another editor's write.
  const mutationKey = [...recordPatchKey(record), "secret", property] as const
  const mutation = useMutation({
    mutationKey,
    mutationFn: async () => {
      const value = secret.current
      secret.current = null
      if (value === null) throw new Error("There is no value to save.")
      return patchRecord(authority, pkg, name, record.id, {
        properties: { [property]: value },
        ifVersion: base,
      })
    },
    onSuccess: (saved) => afterWrite(client, record, saved),
  })
  async function save(value: string): Promise<SubstrateRecord> {
    secret.current = value
    try {
      return await mutation.mutateAsync()
    } finally {
      secret.current = null
      const cache = client.getMutationCache()
      for (const m of cache.findAll({ mutationKey, exact: true })) {
        if (m.state.status !== "pending") cache.remove(m)
      }
    }
  }
  return {
    save,
    pending: mutation.isPending,
    error: mutation.error,
    reset: mutation.reset,
  }
}
