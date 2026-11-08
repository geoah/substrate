/** Live updates for a surface that lists records of a few kinds: one watch
 * over the change feed, filtered to those kinds, that invalidates the
 * queries named whenever a row lands. The surface re-reads through its
 * ordinary queries, so the watch carries no state of its own and a dropped
 * stream costs nothing but freshness (lib/api/changes.ts reconnects). */

import { useEffect, useRef } from "react"
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

/** Where a watch resumes: the `head` and `generation` of the list read whose
 * rows the surface shows. Without it the stream starts at the server's head
 * when it opens, and a row committed after the read and before that is never
 * heard. */
export interface LiveFrom {
  head: number
  generation: string
}

export function useLiveRecords(
  kinds: string[],
  invalidate: QueryKey[],
  onStatus?: (status: WatchStatus, detail?: string) => void,
  from?: LiveFrom
) {
  const queryClient = useQueryClient()
  const kindsKey = JSON.stringify([...kinds].sort())
  const invalidateKey = JSON.stringify(invalidate)
  // Read when the stream opens, never a reason to reopen it: every re-read
  // the watch causes answers a newer head.
  const fromRef = useRef(from)
  useEffect(() => {
    fromRef.current = from
  })
  useEffect(() => {
    if (!kinds.length) return
    const keys = JSON.parse(invalidateKey) as QueryKey[]
    const reread = () => {
      for (const key of keys)
        void queryClient.invalidateQueries({ queryKey: key })
    }
    const batcher = createChangeBatcher<true>(reread)
    const filter = { kinds: JSON.parse(kindsKey) as string[] }
    const onRow = () => batcher.push(true)
    let stopped = false
    const start = fromRef.current
    let handle = watchChanges({
      from: start?.head,
      generation: start?.generation,
      filter,
      onRow,
      onStatus,
      onCompacted: start ? () => recover() : undefined,
    })
    // A position the server no longer holds (a restored repository): follow
    // from the head, and re-read once that stream is live. The server fixes
    // a stream's head before it answers, so a read sent after misses nothing,
    // where one sent beside the stream could answer first and leave a row
    // committed between the two unheard.
    function recover() {
      if (stopped) return
      let relisted = false
      handle = watchChanges({
        filter,
        onRow,
        onStatus: (status, detail) => {
          onStatus?.(status, detail)
          if (status !== "live" || stopped || relisted) return
          relisted = true
          reread()
        },
      })
    }
    return () => {
      stopped = true
      batcher.cancel()
      handle.stop()
    }
    // The kinds and keys travel as strings so a fresh array each render does
    // not reopen the stream.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kindsKey, invalidateKey, queryClient])
}
