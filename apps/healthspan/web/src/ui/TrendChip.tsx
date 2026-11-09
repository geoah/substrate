import { cx } from "@substrate/react"
import { ArrowDownRight, ArrowRight, ArrowUpRight } from "lucide-react"
import { useEffect, useId, useRef, useState } from "react"
import { Bubble } from "./Bubble"

export type TrendDirection = "up" | "down" | "flat"

/** What a trend chip says. */
export interface TrendText {
  /** The chip's words: "0.5%/month", or "Too few results for a trend". */
  label: string
  /** The sentence it opens on hover, focus or a tap. */
  tooltip: string
  /** Its accessible name, in words. */
  aria: string
}

/** The trend of a metric over the shown range: an arrow and the rate as a
 * share of the average per month, coloured by whether it moves the better
 * way. Hovering, focusing or tapping it opens its sentence. Only the
 * direction of `fit` is read, so a screen passes `{ direction }`. */
export function TrendChip({
  fit,
  tone,
  text,
}: {
  fit?: { direction: TrendDirection }
  tone: "success" | "danger" | "neutral"
  text: TrendText
}) {
  const [open, setOpen] = useState(false)
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const id = useId()
  useEffect(() => () => clearTimeout(timer.current), [])
  const Arrow = !fit
    ? undefined
    : fit.direction === "up"
      ? ArrowUpRight
      : fit.direction === "down"
        ? ArrowDownRight
        : ArrowRight
  const hide = () => {
    clearTimeout(timer.current)
    setOpen(false)
  }
  return (
    <span className="relative inline-flex max-w-full min-w-0">
      <button
        type="button"
        aria-label={text.aria}
        aria-describedby={open ? id : undefined}
        data-direction={fit?.direction ?? "none"}
        onPointerEnter={(e) => {
          if (e.pointerType !== "mouse") return
          clearTimeout(timer.current)
          timer.current = setTimeout(() => setOpen(true), 400)
        }}
        onPointerLeave={hide}
        onFocus={(e) => {
          // Keyboard focus opens it at once; a click that focuses does not.
          if (e.currentTarget.matches(":focus-visible")) setOpen(true)
        }}
        onBlur={hide}
        onClick={() => setOpen((v) => !v)}
        onKeyDown={(e) => {
          if (e.key === "Escape" && open) {
            e.stopPropagation()
            hide()
          }
        }}
        className={cx(
          "inline-flex min-h-6 max-w-full min-w-0 items-center gap-1 rounded-[6px] px-1 text-[13px] hover:bg-fill",
          !fit || tone === "neutral"
            ? "text-secondary"
            : tone === "success"
              ? "text-ok-ink"
              : "text-danger-ink"
        )}
      >
        {Arrow && <Arrow className="size-3.5 shrink-0" aria-hidden />}
        <span className={cx("truncate", fit && "tabular-nums")}>
          {text.label}
        </span>
      </button>
      {open && (
        <Bubble left="50%" top={0} id={id} role="tooltip">
          <span className="w-max max-w-[220px] whitespace-normal">
            {text.tooltip}
          </span>
        </Bubble>
      )}
    </span>
  )
}
