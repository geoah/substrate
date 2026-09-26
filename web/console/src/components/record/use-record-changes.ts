/** A record's change rows: the live id's pages first and then, once those
 * are exhausted, every former id's slice. */

import { useMemo } from "react"
import { useInfiniteQuery, useQuery } from "@tanstack/react-query"

import {
  formerIdChangesQueryOptions,
  recordChangesInfiniteOptions,
} from "@/lib/api/records"
import type { SubstrateRecord } from "@/lib/api/types"

const PAGE = 25

/** The record's change rows, the live id's pages first and then, once those
 * are exhausted, every former id's slice. Shared by the header (who added
 * and last changed it) and the History section: one read backs both. */
export function useRecordChanges(record: SubstrateRecord) {
  const formerIds = record.formerIds ?? []
  const changes = useInfiniteQuery(
    recordChangesInfiniteOptions(record.id, record.kind, PAGE)
  )
  const former = useQuery(formerIdChangesQueryOptions(formerIds, record.kind))
  const rows = useMemo(() => {
    const live = (changes.data?.pages ?? []).flatMap((p) => p.changes)
    if (changes.hasNextPage) return live
    const seen = new Set(live.map((r) => r.seq))
    const stitched = (former.data ?? [])
      .filter((r) => !seen.has(r.seq) && (seen.add(r.seq), true))
      .sort((a, b) => b.seq - a.seq)
    return [...live, ...stitched]
  }, [changes.data, changes.hasNextPage, former.data])
  return { changes, rows }
}
