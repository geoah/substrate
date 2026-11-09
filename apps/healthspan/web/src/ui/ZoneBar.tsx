import { cx } from "@substrate/react"
import {
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type HTMLAttributes,
  type KeyboardEvent,
  type PointerEvent,
} from "react"
import { Bubble } from "./Bubble"
import { FILL, type Tone } from "./tone"

/** Intensity of a segment within its tone, 1 faint to 4 full. */
export type ZoneBarLevel = 1 | 2 | 3 | 4

export interface ZoneBarSegment {
  /** `success`, `warning`, `danger` for a judged scale; `accent` or `info`
   * levels for a categorical one (strain's bands); `neutral` for a span
   * with no effect. */
  tone: Tone
  /** Intensity within the tone, 1 faint to 4 full. Default 4. `neutral`
   * has one level. */
  level?: ZoneBarLevel
  /** What the segment means ("7:00 to 7:30 h, adds 0.1 years"). When any
   * segment has one, hovering, tapping or arrowing through the track shows
   * it, and a hidden list reads every one. */
  label?: string
}

export interface ZoneBarMarker {
  /** Position in the scale's units. A value beyond `min` or `max` is not
   * clamped onto the track: it draws an outward pointer past that end,
   * with its real value, and the text alternative says "beyond the
   * scale". */
  at: number
  /** `above` points down onto the track with its text above it; `below`
   * points up from under it with its text below. */
  side: "above" | "below"
  /** The marker's name in the text alternative ("6-month average"). */
  label: string
  /** The value as text ("7:20 h"), shown beside the pointer. */
  valueText?: string
  /** A short muted word before `valueText` ("6mo", "30d", "latest"), so a
   * bar reads without a key. */
  prefix?: string
}

/** A labelled position on the scale: its label in the row under the
 * track, a notch on the track. */
export interface ZoneBarTick {
  at: number
  label: string
  /** `target` (default): a solid notch, a goal or threshold. `baseline`: a
   * hollow notch, the person's own reference (a rolling mean). */
  variant?: "target" | "baseline"
}

export interface ZoneBarProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  "role"
> {
  /** The bar's accessible name. */
  label: string
  min: number
  max: number
  /** Left to right, equal widths. */
  segments: ZoneBarSegment[]
  /** At most one per side: the 6-month pointer above, the 30-day below. */
  markers?: ZoneBarMarker[]
  minLabel?: string
  maxLabel?: string
  /** Small labels under the track at scale positions, on the row of the
   * end labels; an end label a tick would overlap is hidden, and a tick
   * label never leaves the bar's width. */
  ticks?: ZoneBarTick[]
  /** Track thickness: `sm` 6 px, `md` 8 px. Default `md`. */
  size?: "sm" | "md"
}

/** Text within this share of either end hugs that end instead of
 * centring. */
const HUG = 0.14
const LEVEL_MIX = [40, 60, 80, 100]

/** Position on the scale as a fraction of the track, clamped to [0, 1]. */
function fraction(at: number, min: number, max: number): number {
  if (!(max > min)) return 0.5
  return Math.min(1, Math.max(0, (at - min) / (max - min)))
}

function anchorOf(f: number): "start" | "end" | "middle" {
  return f < HUG ? "start" : f > 1 - HUG ? "end" : "middle"
}

function segmentColor(seg: ZoneBarSegment): string {
  if (seg.tone === "neutral") return "var(--hs-zone-neutral)"
  const mix = LEVEL_MIX[(seg.level ?? 4) - 1]
  return `color-mix(in srgb, ${FILL[seg.tone]} ${mix}%, var(--hs-chart-bg))`
}

/** After layout, slide every tick label back inside the bar and clear of
 * its neighbour, and hide an end label a tick overlaps (the tick says
 * more). Written to the DOM directly, so fitting costs no render. */
function fitTicks(row: HTMLElement): void {
  const box = row.getBoundingClientRect()
  const W = box.width
  const tickEls = [...row.querySelectorAll<HTMLElement>("[data-tick]")]
  const gap = 6
  for (const el of tickEls) el.style.setProperty("--shift", "0px")
  const raw = tickEls.map((el, i) => {
    const r = el.getBoundingClientRect()
    return { i, base: r.left - box.left, w: r.width }
  })
  const order = [...raw].sort((p, q) => p.base - q.base)
  const left = new Map<number, number>()
  let edge = Number.NEGATIVE_INFINITY
  for (const t of order) {
    const x = Math.max(t.base, 0, edge + gap)
    left.set(t.i, x)
    edge = x + t.w
  }
  let limit = W
  for (const t of [...order].reverse()) {
    const x = Math.min(left.get(t.i) ?? t.base, limit - t.w)
    left.set(t.i, Math.max(0, x))
    limit = Math.max(0, x) - gap
  }
  const placed = raw.map((t) => {
    const x = left.get(t.i) ?? t.base
    tickEls[t.i].style.setProperty("--shift", `${Math.round(x - t.base)}px`)
    return { left: x, right: x + t.w }
  })
  const end = (sel: string) => row.querySelector<HTMLElement>(sel)
  const minEl = end("[data-end=start]")
  const maxEl = end("[data-end=end]")
  const minW = minEl?.getBoundingClientRect().width ?? 0
  const maxW = maxEl?.getBoundingClientRect().width ?? 0
  minEl?.toggleAttribute(
    "data-hidden",
    minW > 0 && placed.some((p) => p.left < minW + gap)
  )
  maxEl?.toggleAttribute(
    "data-hidden",
    maxW > 0 && placed.some((p) => p.right > W - maxW - gap)
  )
}

/**
 * A value on a target scale: a track of equal segments (a tone and a level
 * each) with up to two pointers, the 6-month one above and the 30-day one
 * below, each with its value; ticks and end labels name the scale under
 * it. Where a reading sits between costly and rewarding zones.
 *
 * Status is never colour alone: the pointers' text and the bar's
 * accessible name say where the value is. A bar whose segments carry
 * labels is a named group with a focusable scale: the pointer, a tap or
 * the arrow keys show each segment's range.
 */
export function ZoneBar({
  label,
  min,
  max,
  segments,
  markers,
  minLabel,
  maxLabel,
  ticks,
  size = "md",
  className,
  style,
  ...rest
}: ZoneBarProps) {
  const above = markers?.find((m) => m.side === "above")
  const below = markers?.find((m) => m.side === "below")
  const hasTicks = !!ticks && ticks.length > 0
  const isLabelled = segments.some((seg) => !!seg.label)
  const tri = size === "sm" ? 3 : 4
  const trackH = size === "sm" ? 6 : 8
  const uid = useId()

  const scaleRef = useRef<HTMLDivElement>(null)
  useLayoutEffect(() => {
    const row = scaleRef.current
    if (!row) return
    fitTicks(row)
    if (typeof ResizeObserver === "undefined") return
    // The labels too: a font arriving changes their width without
    // resizing the row.
    const ro = new ResizeObserver(() => fitTicks(row))
    ro.observe(row)
    for (const el of row.children) ro.observe(el)
    return () => ro.disconnect()
  })

  // The segment under the pointer, the finger or the arrow keys.
  const [activeSeg, setActiveSeg] = useState<number | null>(null)
  const trackRef = useRef<HTMLDivElement>(null)
  const tapped = useRef(false)
  const touchStart = useRef<{ id: number; x: number; y: number } | null>(null)
  const n = segments.length
  const segmentAt = (clientX: number): number | null => {
    const box = trackRef.current?.getBoundingClientRect()
    if (!box || !box.width || !n) return null
    return Math.min(
      n - 1,
      Math.max(0, Math.floor(((clientX - box.left) / box.width) * n))
    )
  }
  // A tapped segment stays shown until a tap elsewhere.
  useEffect(() => {
    if (activeSeg === null || !tapped.current) return
    const onDown = (e: globalThis.PointerEvent) => {
      if (!trackRef.current?.contains(e.target as Node)) {
        tapped.current = false
        setActiveSeg(null)
      }
    }
    document.addEventListener("pointerdown", onDown)
    return () => document.removeEventListener("pointerdown", onDown)
  }, [activeSeg])
  const onTrackMove = (e: PointerEvent<HTMLDivElement>) => {
    if (e.pointerType === "touch") return
    tapped.current = false
    setActiveSeg(segmentAt(e.clientX))
  }
  const onTrackLeave = (e: PointerEvent<HTMLDivElement>) => {
    if (e.pointerType === "touch" || tapped.current) return
    setActiveSeg(null)
  }
  // A touch shows a segment only on a tap (a lift without movement), so a
  // scroll that starts on the bar leaves nothing behind.
  const onTrackDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.pointerType !== "touch") return
    touchStart.current = { id: e.pointerId, x: e.clientX, y: e.clientY }
  }
  const onTrackUp = (e: PointerEvent<HTMLDivElement>) => {
    const start = touchStart.current
    touchStart.current = null
    if (e.pointerType !== "touch" || !start || start.id !== e.pointerId) return
    if (Math.hypot(e.clientX - start.x, e.clientY - start.y) >= 10) return
    tapped.current = true
    setActiveSeg(segmentAt(e.clientX))
  }
  const onTrackCancel = (e: PointerEvent<HTMLDivElement>) => {
    if (e.pointerType !== "touch") return
    touchStart.current = null
    tapped.current = false
    setActiveSeg(null)
  }
  const onTrackKey = (e: KeyboardEvent<HTMLDivElement>) => {
    if (!n) return
    const next =
      e.key === "ArrowRight"
        ? Math.min(n - 1, (activeSeg ?? -1) + 1)
        : e.key === "ArrowLeft"
          ? Math.max(0, (activeSeg ?? n) - 1)
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? n - 1
              : e.key === "Escape"
                ? null
                : undefined
    if (next === undefined) return
    if (next === null && activeSeg === null) return
    e.preventDefault()
    tapped.current = false
    setActiveSeg(next)
  }
  const active = activeSeg !== null ? segments[activeSeg] : undefined

  const notches = hasTicks ? (
    <div className="pointer-events-none absolute inset-0" aria-hidden>
      {ticks.map((t) => (
        <span
          key={`${t.at}-${t.label}`}
          className={cx(
            "absolute -top-0.5 box-border -translate-x-1/2",
            t.variant === "baseline"
              ? "w-[7px] rounded-[2px] border-[1.5px] border-label bg-transparent"
              : "w-0.5 rounded-[1px] bg-label"
          )}
          style={{
            height: trackH + 4,
            left: `clamp(4px, ${fraction(t.at, min, max) * 100}%, calc(100% - 4px))`,
          }}
        />
      ))}
    </div>
  ) : null

  const overflowOf = (m: ZoneBarMarker): "start" | "end" | undefined =>
    m.at > max ? "end" : m.at < min ? "start" : undefined
  const describe = (m: ZoneBarMarker) =>
    `${m.label} ${m.valueText ?? String(m.at)}${overflowOf(m) ? " beyond the scale" : ""}`
  const ariaLabel = [
    label,
    ...[above, below].filter((m): m is ZoneBarMarker => !!m).map(describe),
    `scale from ${minLabel ?? String(min)} to ${maxLabel ?? String(max)}`,
  ].join(", ")

  const marker = (m: ZoneBarMarker) => {
    const overflow = overflowOf(m)
    const f = fraction(m.at, min, max)
    const anchor = overflow ?? anchorOf(f)
    const isAbove = m.side === "above"
    const pointer: CSSProperties = overflow
      ? {
          position: "absolute",
          borderTop: `${tri}px solid transparent`,
          borderBottom: `${tri}px solid transparent`,
          ...(isAbove
            ? { bottom: -(trackH / 2 + tri) }
            : { top: -(trackH / 2 + tri) }),
          ...(overflow === "end"
            ? {
                left: "calc(100% + 4px)",
                borderLeft: `${tri + 1}px solid var(--sx-label)`,
              }
            : {
                right: "calc(100% + 4px)",
                borderRight: `${tri + 1}px solid var(--sx-label)`,
              }),
        }
      : {
          position: "relative",
          borderLeft: `${tri}px solid transparent`,
          borderRight: `${tri}px solid transparent`,
          [isAbove ? "borderTop" : "borderBottom"]:
            `${tri + 1}px solid var(--sx-label)`,
          left: `clamp(${tri}px, ${f * 100}%, calc(100% - ${tri}px))`,
          translate: "-50% 0",
        }
    return (
      <div
        className={cx(
          "relative flex",
          isAbove ? "flex-col" : "flex-col-reverse"
        )}
        data-overflow={overflow}
      >
        {(m.valueText || m.prefix) && (
          <span
            className={cx(
              "relative w-max max-w-full leading-[1.2] whitespace-nowrap tabular-nums",
              anchor === "end" && "self-end"
            )}
            style={
              anchor === "middle"
                ? { left: `${f * 100}%`, translate: "-50% 0" }
                : undefined
            }
          >
            {m.prefix && (
              <>
                <span className="text-secondary">{m.prefix}</span>{" "}
              </>
            )}
            {m.valueText}
          </span>
        )}
        <span className="size-0" style={pointer} />
      </div>
    )
  }

  const track = (
    <div
      ref={trackRef}
      className={cx(
        "flex gap-0.5 rounded-full",
        isLabelled ? "overflow-visible" : "overflow-hidden"
      )}
      style={{ height: trackH }}
    >
      {segments.map((seg, i) => (
        <span
          // Segments are positional and equal-width: the index is their
          // identity.
          key={i}
          data-tone={seg.tone}
          className={cx(
            "min-w-0 flex-1",
            isLabelled && "first:rounded-l-full last:rounded-r-full",
            i === activeSeg &&
              "relative z-[1] outline-[1.5px] outline-offset-1 outline-label outline-solid"
          )}
          style={{ background: segmentColor(seg) }}
        />
      ))}
    </div>
  )

  return (
    <div
      {...rest}
      className={cx(
        "flex w-full min-w-0 flex-col text-label",
        size === "sm" ? "text-[11px]" : "text-[12px]",
        className
      )}
      style={style}
      // A labelled bar holds a focusable scale, which an `img` would hide.
      role={isLabelled ? "group" : "img"}
      aria-label={ariaLabel}
    >
      {above && marker(above)}
      {isLabelled ? (
        <div
          className="relative rounded-[4px] focus-visible:outline-offset-[3px]"
          tabIndex={0}
          role="group"
          aria-roledescription="scale"
          aria-label={`${label}, segments`}
          aria-describedby={`${uid}-segments`}
          onPointerMove={onTrackMove}
          onPointerLeave={onTrackLeave}
          onPointerDown={onTrackDown}
          onPointerUp={onTrackUp}
          onPointerCancel={onTrackCancel}
          onKeyDown={onTrackKey}
          onBlur={() => {
            tapped.current = false
            setActiveSeg(null)
          }}
        >
          {track}
          {notches}
          {active?.label && activeSeg !== null && (
            <Bubble left={`${((activeSeg + 0.5) / n) * 100}%`} top={0}>
              {active.label}
            </Bubble>
          )}
        </div>
      ) : (
        <div className="relative">
          {track}
          {notches}
        </div>
      )}
      {isLabelled && (
        <>
          <span className="sr-only" aria-live="polite">
            {active?.label ?? ""}
          </span>
          <ol id={`${uid}-segments`} className="sr-only">
            {segments.map((seg, i) =>
              seg.label ? (
                // Positional, as the segments.
                <li key={i}>{`${i + 1} of ${n}: ${seg.label}`}</li>
              ) : null
            )}
          </ol>
        </>
      )}
      {below && marker(below)}
      {(hasTicks || minLabel || maxLabel) && (
        <div
          ref={scaleRef}
          className="relative mt-0.5 leading-[1.2] text-secondary"
          style={{ height: "1.2em" }}
        >
          {minLabel && (
            <span
              data-end="start"
              className="absolute top-0 left-0 whitespace-nowrap data-hidden:invisible"
            >
              {minLabel}
            </span>
          )}
          {ticks?.map((t) => (
            <span
              key={`${t.at}-${t.label}`}
              data-tick
              className="absolute top-0 whitespace-nowrap tabular-nums"
              style={{
                left: `${fraction(t.at, min, max) * 100}%`,
                translate: "calc(-50% + var(--shift, 0px)) 0",
              }}
            >
              {t.label}
            </span>
          ))}
          {maxLabel && (
            <span
              data-end="end"
              className="absolute top-0 right-0 whitespace-nowrap data-hidden:invisible"
            >
              {maxLabel}
            </span>
          )}
        </div>
      )}
    </div>
  )
}
