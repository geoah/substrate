/** The console's height, held to the VISIBLE viewport. iOS and iPadOS draw the
 * keyboard, and with a hardware keyboard the small form-navigation bar, over
 * the page instead of shrinking it: `100svh` still reaches behind them, so
 * Safari scrolls the whole document up to uncover the focused field, which
 * clips the top of the shell and leaves a blank strip under it. Shrinking the
 * shell to `visualViewport.height` keeps the field in view without that
 * scroll, and the scroll Safari already made is put back.
 *
 * `--visual-viewport-height` is set on the root only while something covers
 * the page, so every other browser keeps `100svh`. */

import { useEffect } from "react"

const VAR = "--visual-viewport-height"

export function useVisualViewportHeight() {
  useEffect(() => {
    const vv = window.visualViewport
    if (!vv) return
    const root = document.documentElement
    let frame = 0
    const sync = () => {
      cancelAnimationFrame(frame)
      frame = requestAnimationFrame(() => {
        // The console does not zoom (lib/no-zoom.ts), so a visible height
        // short of the window's is a keyboard or its bar.
        const covered = window.innerHeight - vv.height > 1
        if (covered) {
          root.style.setProperty(VAR, `${vv.height}px`)
          if (window.scrollY !== 0) window.scrollTo(0, 0)
        } else {
          root.style.removeProperty(VAR)
        }
      })
    }
    vv.addEventListener("resize", sync)
    vv.addEventListener("scroll", sync)
    sync()
    return () => {
      cancelAnimationFrame(frame)
      vv.removeEventListener("resize", sync)
      vv.removeEventListener("scroll", sync)
      root.style.removeProperty(VAR)
    }
  }, [])
}
