/** The write that points a record's ink property at a newly stored file.
 * It asserts the version drawing began from, so a save made elsewhere
 * meanwhile is not silently replaced. A refusal because the record moved is
 * retried once against the new version when the move left this property's
 * file alone: an edit to another property, a label or an agent's transcript
 * is no reason to lose a drawing. Only another save of the same ink is a
 * conflict the reader must see. */

import { useCallback } from "react"
import { useQueryClient } from "@tanstack/react-query"

import { noteWrite } from "@/components/property-sheet/use-record-patch"
import { readBlobInfo, isBlobDigest } from "@/lib/api/blobs"
import { splitKind } from "@/lib/api/http"
import { fetchRecord, patchRecord, recordWriteReaches } from "@/lib/api/records"
import { ApiError, type SubstrateRecord } from "@/lib/api/types"

/** Where a drawing began: the record's version, and the digest of the file
 * the property named then (none for a note not drawn yet). */
export interface InkBase {
  version: number
  digest?: string
}

export function inkBaseOf(record: SubstrateRecord, property: string): InkBase {
  return { version: record.version, digest: fileDigest(record, property) }
}

function fileDigest(record: SubstrateRecord, property: string) {
  const value = record.properties[property]
  return isBlobDigest(value) ? value : readBlobInfo(value)?.digest
}

export function useInkWrite(record: SubstrateRecord, property: string) {
  const client = useQueryClient()
  const { kind, id } = record
  return useCallback(
    async (digest: string, base: InkBase): Promise<SubstrateRecord> => {
      const { authority, pkg, name } = splitKind(kind)
      const write = (ifVersion: number) =>
        patchRecord(authority, pkg, name, id, {
          properties: { [property]: digest },
          ifVersion,
        })
      let saved: SubstrateRecord
      try {
        saved = await write(base.version)
      } catch (error) {
        if (!(error instanceof ApiError && error.code === "conflict"))
          throw error
        const fresh = await fetchRecord(authority, pkg, name, id)
        if (fileDigest(fresh, property) !== base.digest) throw error
        saved = await write(fresh.version)
      }
      noteWrite(kind, id, saved.version)
      await client.invalidateQueries({
        predicate: (q) => recordWriteReaches(q.queryKey, kind, id),
      })
      return saved
    },
    [client, kind, id, property]
  )
}
