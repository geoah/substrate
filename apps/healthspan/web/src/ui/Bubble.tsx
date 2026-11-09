import { cx } from "@substrate/react"
import { useLayoutEffect, useRef, type ReactNode } from "react"

/** A small label floating over a point: a chart's readout, a zone's range,
 * a chip's sentence. Text only, never controls. It sits above or below its
 * anchor (`left` and `top` in the positioned parent's px, or a percentage)
 * and slides back inside the window's sides when it would cross one. */
export function Bubble(props: {
  left: number | string
  top: number | string
  side?: "top" | "bottom"
  id?: string
  role?: "tooltip"
  className?: string
  children: ReactNode
}) {
  const inner = useRef<HTMLSpanElement>(null)
  // Measured after every render and moved by hand, so the slide costs no
  // second render.
  useLayoutEffect(() => {
    const el = inner.current
    if (!el) return
    el.style.marginLeft = ""
    const r = el.getBoundingClientRect()
    const vw = document.documentElement.clientWidth
    const margin = 8
    const shift =
      r.left < margin
        ? margin - r.left
        : r.right > vw - margin
          ? vw - margin - r.right
          : 0
    if (Math.abs(shift) > 0.5) el.style.marginLeft = `${Math.round(shift)}px`
  })
  const below = props.side === "bottom"
  return (
    <span
      className="pointer-events-none absolute z-30 size-0"
      style={{ left: props.left, top: props.top }}
    >
      <span
        ref={inner}
        id={props.id}
        role={props.role}
        className={cx(
          "absolute left-0 flex w-max max-w-[240px] -translate-x-1/2 flex-col gap-0.5 rounded-[8px] bg-card/95 px-2.5 py-1.5 text-[12px] leading-snug text-label shadow-[0_2px_12px_rgb(0_0_0/0.18)] ring-1 ring-separator/40 backdrop-blur-xl",
          below ? "top-1.5" : "bottom-1.5",
          props.className
        )}
      >
        {props.children}
      </span>
    </span>
  )
}
