/** STAND-IN for the table area's hook of the same name and signature
 * (console/r3-table owns this file): on merge, take that branch's version.
 * This one is the smallest thing the record page can call: one watch over
 * the change feed narrowed to the page's kinds and ids, a debounced
 * invalidation of every cached read those records reach, then `onChange`. */

import { useEffect, useRef } from "react"
import { useQueryClient } from "@tanstack/react-query"

import { watchChanges } from "@/lib/api/changes"
import { recordWriteReaches } from "@/lib/api/records"

export interface LiveScope {
  /** Kind references in full; the feed is narrowed to them server-side. */
  kinds?: string[]
  /** Only these records, of those kinds. */
  recordIds?: string[]
}

/** One record a watched change moved. */
export interface LiveChange {
  kind: string
  id: string
  deleted: boolean
}

export function useLiveInvalidation(
  scope: LiveScope,
  onChange?: (changed: LiveChange[]) => void
): void {
  const queryClient = useQueryClient()
  const onChangeRef = useRef(onChange)
  useEffect(() => {
    onChangeRef.current = onChange
  })
  const kindsKey = JSON.stringify([...(scope.kinds ?? [])].sort())
  const idsKey = JSON.stringify([...(scope.recordIds ?? [])].sort())
  useEffect(() => {
    const kinds = JSON.parse(kindsKey) as string[]
    const ids = new Set(JSON.parse(idsKey) as string[])
    if (!kinds.length && !ids.size) return
    let timer: ReturnType<typeof setTimeout> | undefined
    let batch: LiveChange[] = []
    const handle = watchChanges({
      filter: kinds.length ? { kinds } : {},
      onRow: (row) => {
        if (ids.size && !ids.has(row.recordId)) return
        batch.push({
          kind: row.kind,
          id: row.recordId,
          deleted: row.op === "delete",
        })
        if (timer) return
        timer = setTimeout(() => {
          timer = undefined
          const changed = batch
          batch = []
          void queryClient
            .invalidateQueries({
              predicate: (q) =>
                changed.some((c) =>
                  recordWriteReaches(q.queryKey, c.kind, c.id)
                ),
            })
            .then(() => onChangeRef.current?.(changed))
        }, 400)
      },
    })
    return () => {
      if (timer) clearTimeout(timer)
      handle.stop()
    }
  }, [kindsKey, idsKey, queryClient])
}
