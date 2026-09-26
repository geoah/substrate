/** Live updates for a surface that lists records of a few kinds: one watch
 * over the change feed, filtered to those kinds, that invalidates the
 * queries named whenever a row lands. The surface re-reads through its
 * ordinary queries, so the watch carries no state of its own and a dropped
 * stream costs nothing but freshness (lib/api/changes.ts reconnects). */

import { useEffect } from "react"
import { useQueryClient, type QueryKey } from "@tanstack/react-query"

import { watchChanges, type WatchStatus } from "@/lib/api/changes"

/** A sync writes many rows in one breath: one re-read covers them all. The
 * batch flushes `wait` ms after the LAST item, and never later than
 * `maxWait` ms after the first, so a long burst still shows progress. */
export interface ChangeBatcher<T> {
  push(...items: T[]): void
  cancel(): void
}

export function createChangeBatcher<T>(
  flush: (items: T[]) => void,
  { wait = 300, maxWait = 1200 }: { wait?: number; maxWait?: number } = {}
): ChangeBatcher<T> {
  let items: T[] = []
  let timer: ReturnType<typeof setTimeout> | undefined
  let firstAt = 0
  const fire = () => {
    timer = undefined
    const out = items
    items = []
    if (out.length) flush(out)
  }
  return {
    push(...more) {
      if (!more.length) return
      const now = Date.now()
      if (!items.length) firstAt = now
      items.push(...more)
      if (timer) clearTimeout(timer)
      timer = setTimeout(
        fire,
        Math.max(0, Math.min(wait, firstAt + maxWait - now))
      )
    },
    cancel() {
      if (timer) clearTimeout(timer)
      timer = undefined
      items = []
    },
  }
}

export function useLiveRecords(
  kinds: string[],
  invalidate: QueryKey[],
  onStatus?: (status: WatchStatus, detail?: string) => void
) {
  const queryClient = useQueryClient()
  const kindsKey = JSON.stringify([...kinds].sort())
  const invalidateKey = JSON.stringify(invalidate)
  useEffect(() => {
    if (!kinds.length) return
    const keys = JSON.parse(invalidateKey) as QueryKey[]
    const batcher = createChangeBatcher<true>(() => {
      for (const key of keys)
        void queryClient.invalidateQueries({ queryKey: key })
    })
    const handle = watchChanges({
      filter: { kinds: JSON.parse(kindsKey) as string[] },
      onRow: () => batcher.push(true),
      onStatus,
    })
    return () => {
      batcher.cancel()
      handle.stop()
    }
    // The kinds and keys travel as strings so a fresh array each render does
    // not reopen the stream.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kindsKey, invalidateKey, queryClient])
}
