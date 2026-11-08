/** A collection's shipped saved views (decision record 0229): the
 * `console/collectionview` records read for one kind, and the writes a tab's
 * menu makes to one. A write is the owner's, so the record seed leaves the
 * view as the owner left it from then on. */

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query"

import { splitKind } from "@/lib/api/http"
import {
  patchRecord,
  recordWriteReaches,
  recordsQueryOptions,
} from "@/lib/api/records"
import { deleteRecord } from "@/lib/api/sync"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  COLLECTION_VIEW_KIND,
  collectionViewsParams,
} from "@/lib/collection-views"

const NONE: SubstrateRecord[] = []

/** The view records shipped for one collection. `settled` once the read
 * answered or failed: a repository that does not declare the kind has none,
 * and the page opens on All.
 *
 * Never the previous read's records as a placeholder: the page stays mounted
 * from one collection to the next, and the last collection's views taken as
 * this one's would open it on the last one's default, or settle it before
 * its own arrive and miss its own. */
export function useCollectionViewRecords(kind: KindInfo | undefined): {
  records: SubstrateRecord[]
  settled: boolean
} {
  const read = useQuery({
    ...recordsQueryOptions(collectionViewsParams(kind?.identity ?? "")),
    placeholderData: undefined,
    enabled: Boolean(kind),
  })
  return {
    records: read.data?.records ?? NONE,
    settled: read.isSuccess || read.isError,
  }
}

/** Patch a shipped view's record, or delete it with `properties` null. */
export function useCollectionViewWrite() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async ({
      record,
      properties,
    }: {
      record: string
      properties: Record<string, unknown> | null
    }) => {
      if (properties === null) {
        await deleteRecord({ kind: COLLECTION_VIEW_KIND, id: record })
        return
      }
      const { authority, pkg, name } = splitKind(COLLECTION_VIEW_KIND)
      await patchRecord(authority, pkg, name, record, { properties })
    },
    onSettled: (_data, _error, { record }) =>
      client.invalidateQueries({
        predicate: (q) =>
          recordWriteReaches(q.queryKey, COLLECTION_VIEW_KIND, record),
      }),
  })
}
