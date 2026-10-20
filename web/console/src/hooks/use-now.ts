/** Now by the server's clock (serverNow), read again every `everyMs` and
 * whenever the console learns a new server clock offset: for a row whose
 * words depend on how old an instant is, so it changes between refetches,
 * and so a component that mounts before the first response does not keep the
 * browser's clock until its first tick. The pure functions it feeds take
 * `now` as an argument; this hook only keeps a mounted row current. */

import { useEffect, useState } from "react"

import { onServerClock, serverNow } from "@/lib/api/http"

export function useNow(everyMs: number): number {
  const [now, setNow] = useState(() => serverNow().getTime())
  useEffect(() => {
    const read = () => setNow(serverNow().getTime())
    const tick = setInterval(read, everyMs)
    const unsubscribe = onServerClock(read)
    return () => {
      clearInterval(tick)
      unsubscribe()
    }
  }, [everyMs])
  return now
}
