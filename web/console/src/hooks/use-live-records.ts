/** Live updates for a surface that lists records of a few kinds: one watch
 * over the change feed, filtered to those kinds, that invalidates the
 * queries named whenever a row lands. The surface re-reads through its
 * ordinary queries, so the watch carries no state of its own and a dropped
 * stream costs nothing but freshness (lib/api/changes.ts reconnects). */

import { useEffect } from "react"
import { useQueryClient, type QueryKey } from "@tanstack/react-query"

import { watchChanges, type WatchStatus } from "@/lib/api/changes"

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
    let timer: ReturnType<typeof setTimeout> | undefined
    const keys = JSON.parse(invalidateKey) as QueryKey[]
    const handle = watchChanges({
      filter: { kinds: JSON.parse(kindsKey) as string[] },
      onRow: () => {
        // A sync writes several rows in one breath; one re-read a moment
        // later covers them all.
        if (timer) return
        timer = setTimeout(() => {
          timer = undefined
          for (const key of keys)
            void queryClient.invalidateQueries({ queryKey: key })
        }, 400)
      },
      onStatus,
    })
    return () => {
      if (timer) clearTimeout(timer)
      handle.stop()
    }
    // The kinds and keys travel as strings so a fresh array each render does
    // not reopen the stream.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [kindsKey, invalidateKey, queryClient])
}
