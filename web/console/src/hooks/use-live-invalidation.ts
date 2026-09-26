/** Live updates for a page that shows records it did not write: the open
 * collection, the record page. One watch over the change feed, narrowed to
 * the page's kinds (and, where given, its record ids); each batch of changes
 * invalidates exactly the cached reads those records reach — the record, its
 * collection's pages and counts, the title reads naming it — so the page
 * re-reads through its ordinary queries. `onChange` then hears which records
 * moved, once that re-read has landed, so a page can mark them. */

import { useCallback, useEffect, useRef, useState } from "react"
import { useQueryClient } from "@tanstack/react-query"

import { createChangeBatcher } from "@/hooks/use-live-records"
import { watchChanges } from "@/lib/api/changes"
import { joinKind } from "@/lib/api/http"
import { recordWriteReaches } from "@/lib/api/records"
import type { ChangeRow } from "@/lib/api/types"

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
  /** The change tombstoned or purged it. */
  deleted: boolean
}

/** The records a feed row moved that the scope watches, once each. A row's
 * `affected` list is every record the entry moved (a merge's loser beside
 * its survivor); a server that predates it names the addressed record alone. */
export function changedInScope(row: ChangeRow, scope: LiveScope): LiveChange[] {
  const moved: LiveChange[] = row.affected?.length
    ? row.affected.map((a) => ({
        kind: a.kind,
        id: a.id,
        deleted: Boolean(a.deleted),
      }))
    : [
        {
          kind: row.kind,
          id: row.recordId,
          deleted: row.op === "delete",
        },
      ]
  const kinds = scope.kinds?.length ? new Set(scope.kinds) : undefined
  const ids = scope.recordIds?.length ? new Set(scope.recordIds) : undefined
  return moved.filter(
    (c) => (!kinds || kinds.has(c.kind)) && (!ids || ids.has(c.id))
  )
}

/** Merge a batch: one entry per record, deleted if any change deleted it. */
export function dedupeChanges(changes: LiveChange[]): LiveChange[] {
  const out = new Map<string, LiveChange>()
  for (const c of changes) {
    const key = `${c.kind}/${c.id}`
    const had = out.get(key)
    out.set(key, had ? { ...had, deleted: had.deleted || c.deleted } : c)
  }
  return [...out.values()]
}

/** Whether a cached read can show a record that changed under the page:
 * everything a console write reaches, and the collection's bounded counts,
 * whose key spells the kind in three segments. */
export function liveChangeReaches(
  queryKey: readonly unknown[],
  change: LiveChange
): boolean {
  if (queryKey[0] === "records-count")
    return (
      joinKind(
        String(queryKey[1]),
        String(queryKey[2]),
        String(queryKey[3])
      ) === change.kind
    )
  return recordWriteReaches(queryKey, change.kind, change.id)
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
    const recordIds = JSON.parse(idsKey) as string[]
    // An empty scope would watch the whole repository.
    if (!kinds.length && !recordIds.length) return
    const watched: LiveScope = { kinds, recordIds }
    let stopped = false
    const batcher = createChangeBatcher<LiveChange>((batch) => {
      const changed = dedupeChanges(batch)
      // The page hears of a change once its re-read has landed, so a mark
      // it draws lands on the new value, not the old one.
      void queryClient
        .invalidateQueries({
          predicate: (q) =>
            changed.some((c) => liveChangeReaches(q.queryKey, c)),
        })
        .catch(() => undefined)
        .then(() => {
          if (!stopped) onChangeRef.current?.(changed)
        })
    })
    const handle = watchChanges({
      filter: kinds.length ? { kinds } : {},
      onRow: (row) => batcher.push(...changedInScope(row, watched)),
    })
    return () => {
      stopped = true
      batcher.cancel()
      handle.stop()
    }
  }, [kindsKey, idsKey, queryClient])
}

/** How long a changed row stays marked, then how long the mark fades. */
const MARK_HOLD_MS = 1200
const MARK_FADE_MS = 1500

export type ChangeMark = "fresh" | "fading"

/** Brief marks on records that changed under the reader: `mark(ids)` holds
 * each one `fresh`, then `fading` while the tint eases out, then drops it.
 * A record marked again starts over. */
export function useChangeMarks(): {
  marks: ReadonlyMap<string, ChangeMark>
  mark: (ids: string[]) => void
} {
  const [marks, setMarks] = useState<ReadonlyMap<string, ChangeMark>>(
    () => new Map()
  )
  const timers = useRef(new Map<string, ReturnType<typeof setTimeout>>())
  useEffect(() => {
    const pending = timers.current
    return () => {
      for (const t of pending.values()) clearTimeout(t)
      pending.clear()
    }
  }, [])
  const mark = useCallback((ids: string[]) => {
    if (!ids.length) return
    const set = (id: string, to: ChangeMark | undefined) =>
      setMarks((prev) => {
        const next = new Map(prev)
        if (to) next.set(id, to)
        else next.delete(id)
        return next
      })
    for (const id of ids) {
      clearTimeout(timers.current.get(id))
      set(id, "fresh")
      timers.current.set(
        id,
        setTimeout(() => {
          set(id, "fading")
          timers.current.set(
            id,
            setTimeout(() => {
              timers.current.delete(id)
              set(id, undefined)
            }, MARK_FADE_MS)
          )
        }, MARK_HOLD_MS)
      )
    }
  }, [])
  return { marks, mark }
}
