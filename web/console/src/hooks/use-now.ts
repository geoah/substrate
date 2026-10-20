/** Now by the server's clock (serverNow), read again every `everyMs`: for a
 * row whose words depend on how old an instant is, so it changes between
 * refetches. The pure functions it feeds take `now` as an argument; this hook
 * only keeps a mounted row current. */

import { useEffect, useState } from "react"

import { serverNow } from "@/lib/api/http"

export function useNow(everyMs: number): number {
  const [now, setNow] = useState(() => serverNow().getTime())
  useEffect(() => {
    const tick = setInterval(() => setNow(serverNow().getTime()), everyMs)
    return () => clearInterval(tick)
  }, [everyMs])
  return now
}
