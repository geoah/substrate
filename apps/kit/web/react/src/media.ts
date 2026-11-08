import { useSyncExternalStore } from "react"

/** Whether a media query holds, kept current: the layout a screen picks
 * (a phone's stack, a tablet's two columns, a desktop's three). */
export function useMediaQuery(query: string): boolean {
  return useSyncExternalStore(
    (fn) => {
      const m = window.matchMedia(query)
      m.addEventListener("change", fn)
      return () => m.removeEventListener("change", fn)
    },
    () => window.matchMedia(query).matches
  )
}
