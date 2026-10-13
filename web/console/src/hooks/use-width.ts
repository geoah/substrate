/** An element's width as it lays out, following it as it resizes (the
 * sidebar folding, the window narrowing), for an SVG that draws to it. */

import { useLayoutEffect, useRef, useState } from "react"

export function useWidth(fallback: number) {
  const ref = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(fallback)
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    if (el.clientWidth) setWidth(el.clientWidth)
    if (typeof ResizeObserver === "undefined") return
    const ro = new ResizeObserver(() => {
      if (el.clientWidth) setWidth(el.clientWidth)
    })
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return { ref, width }
}
