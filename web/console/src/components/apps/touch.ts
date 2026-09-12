/** `html.touch` on the root while an app or view route is open under 768 px:
 * Tailwind's rem utilities resolve against the root, so the touch density
 * (16 px base, wider spacing) must be set there, and portaled sheets inherit
 * it for the same reason. The ThemeProvider toggles `.light`/`.dark` the
 * same way; the rest of the console keeps its 13.5 px. */

import { useEffect } from "react"

import { useIsMobile } from "@/hooks/use-mobile"

export function useTouchRoot(): void {
  const isMobile = useIsMobile()
  useEffect(() => {
    if (!isMobile) return
    const root = document.documentElement
    root.classList.add("touch")
    return () => root.classList.remove("touch")
  }, [isMobile])
}
