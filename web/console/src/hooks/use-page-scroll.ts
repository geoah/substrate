/** Where the shell's page container is scrolled across a route change. Pages
 * scroll inside that container, not the window, so neither the browser's own
 * restoration nor the router's reset (the window alone) reaches it: left
 * alone, a page opens at the offset the last one was left at.
 *
 * A change of pathname opens the page at the top, or, back or forward, where
 * that history entry was left. A change of search params alone is the same
 * page (a tab, a filter) and keeps its place, and an address with a hash is
 * scrolled to its element by whoever handles the hash. */

import { useEffect, type RefObject } from "react"
import { useRouter, type ParsedLocation } from "@tanstack/react-router"

/** One history entry's key, as the router's own scroll restoration keys it:
 * a back or forward returns to the entry, and so to its key. */
function entryKey(location: ParsedLocation): string {
  return location.state.__TSR_key ?? location.href
}

export function usePageScroll(ref: RefObject<HTMLElement | null>) {
  const router = useRouter()
  useEffect(() => {
    const left = new Map<string, number>()
    // Before the next page draws, while the container still holds the one
    // the reader is leaving: after, its offset may be clamped to the new one.
    const offBefore = router.subscribe("onBeforeNavigate", (event) => {
      const el = ref.current
      if (el && event.fromLocation)
        left.set(entryKey(event.fromLocation), el.scrollTop)
    })
    const offRendered = router.subscribe("onRendered", (event) => {
      const el = ref.current
      if (!el || !event.pathChanged || event.toLocation.hash) return
      el.scrollTop = left.get(entryKey(event.toLocation)) ?? 0
    })
    return () => {
      offBefore()
      offRendered()
    }
  }, [router, ref])
}
