/** One chart of the timeline, drawn by hand in SVG: a 2px line with its
 * lowest-to-highest band as a 10% wash for a level, bars (24px at most, the
 * data end rounded) for amounts and counts, and bars stacked per category
 * with a 2px surface gap for coded values. Each series says how it is drawn
 * and which axis it reads on, so one chart may hold lines and bars of
 * several metrics: the series of one unit share the left axis, a second
 * unit reads on the right, each axis captioned with its unit. Hairline grid
 * from the first axis, labels in text tokens; colour carries identity alone
 * and the legend and the table under the chart carry it too.
 *
 * The hover layer is the bucket, not the mark: a column under the pointer is
 * tinted, one crosshair marks it with a dot on every line, and one tooltip
 * lists every series at that bucket. A click zooms into the bucket and a
 * drag across several zooms into them all; from the keyboard the arrows move
 * between buckets, Enter zooms and Escape leaves. */

import {
  useLayoutEffect,
  useRef,
  useState,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
} from "react"

import {
  bandPath,
  barPath,
  bucketAt,
  linePath,
  linearScale,
  niceDomain,
  rectPath,
  tickLabels,
  type Scale,
} from "@/lib/chart"
import {
  axisTicks,
  bucketWords,
  type Bucket,
  type BucketSize,
  type SeriesPoint,
  type TimeRange,
} from "@/lib/observations"
import type { AxisSide, ChartMark } from "@/lib/overlay"
import { cn } from "@/lib/utils"

export type { ChartMark }

export interface ChartSeries {
  key: string
  /** The group value the series answers, where the chart is grouped. */
  value?: string
  label: string
  /** A CSS colour, a `--series-N` token. */
  color: string
  points: SeriesPoint[]
  /** How it is drawn; the chart's `mark` where absent. */
  mark?: ChartMark
  /** Draw its lowest-to-highest band (a level); the chart's `band` where
   * absent. */
  band?: boolean
  /** The axis it reads on; the left where absent. */
  axis?: AxisSide
  /** The stack a category belongs to (its metric): the categories of one
   * stack pile up in one bar. */
  stack?: string
  /** A newer answer is loading for this series: hold this one, quieter. */
  stale?: boolean
}

const TOP = 10
/** Room above the plot for the axes' unit captions. */
const CAPTION = 14
const PLOT = 200
const AXIS = 26
const LEFT = 52
const RIGHT = 14
/** The right margin where a second axis needs its labels. */
const RIGHT_AXIS = 52
const GAP = 2
const MAX_BAR = 24

/** One y axis: its scale, its ticks and labels, and its bars' baseline. */
interface YAxis {
  y: Scale
  ticks: number[]
  labels: string[]
  base: number
}

/** The scale of one side's series: their values (a stack's whole height,
 * a level's band), anchored at zero where any is a bar. */
function yAxisOf(
  series: readonly ChartSeries[],
  buckets: readonly Bucket[],
  mark: ChartMark,
  band: boolean,
  top: number
): YAxis {
  const values: number[] = []
  const stacks = new Map<string, ChartSeries[]>()
  for (const s of series) {
    const m = s.mark ?? mark
    if (m === "stack") {
      const key = s.stack ?? ""
      stacks.set(key, [...(stacks.get(key) ?? []), s])
      continue
    }
    const withBand = m === "line" && (s.band ?? band)
    for (const p of s.points) {
      if (p.value !== null) values.push(p.value)
      if (withBand && p.low !== null) values.push(p.low)
      if (withBand && p.high !== null) values.push(p.high)
    }
  }
  for (const members of stacks.values()) {
    buckets.forEach((_, i) => {
      let sum = 0
      for (const s of members) sum += Math.max(0, s.points[i]?.value ?? 0)
      values.push(sum)
    })
  }
  const zero = series.some((s) => (s.mark ?? mark) !== "line")
  const { domain, ticks } = niceDomain(values, { zero })
  const y = linearScale(domain, [top + PLOT, top])
  return {
    y,
    ticks,
    labels: tickLabels(ticks),
    base: zero ? y(Math.max(domain[0], 0)) : top + PLOT,
  }
}

function useWidth(fallback: number) {
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

export function TimelineChart({
  mark = "line",
  band = false,
  buckets,
  series,
  domain,
  size,
  label,
  tooltip,
  onZoom,
  stale = false,
  units,
}: {
  /** How a series that does not say is drawn. */
  mark?: ChartMark
  /** Draw the band of a line series that does not say (a level). */
  band?: boolean
  buckets: Bucket[]
  series: ChartSeries[]
  /** The window read, the x axis's extent. */
  domain: TimeRange
  size: BucketSize
  /** The chart's accessible name. */
  label: string
  /** What the tooltip says about one bucket. */
  tooltip: (bucket: number) => ReactNode
  onZoom: (range: TimeRange) => void
  /** A newer answer is loading for every series: hold this one, quieter. */
  stale?: boolean
  /** Each axis's unit in words, captioned above it. */
  units?: { left?: string; right?: string }
}) {
  const { ref, width } = useWidth(720)
  const [held, setActive] = useState<number | null>(null)
  const [heldAnchor, setAnchor] = useState<number | null>(null)
  // A bucket index outlives the answer it pointed into: a zoom or a new read
  // brings fewer buckets while the pointer still rests on the chart.
  const active = held !== null && held < buckets.length ? held : null
  const anchor =
    heldAnchor !== null && heldAnchor < buckets.length ? heldAnchor : null

  const lefts = series.filter((s) => (s.axis ?? "left") === "left")
  const rights = series.filter((s) => s.axis === "right")
  const top = units?.left || units?.right ? TOP + CAPTION : TOP
  const height = top + PLOT + AXIS
  const right = Math.max(
    LEFT + 40,
    width - (rights.length ? RIGHT_AXIS : RIGHT)
  )
  const x = linearScale([domain.start, domain.end], [LEFT, right])
  const windows = buckets.map((b) => {
    const x0 = x(Math.max(b.start, domain.start))
    const x1 = x(Math.min(b.end, domain.end))
    return { x0, x1: Math.max(x1, x0 + 1), mid: (x0 + x1) / 2 }
  })

  // The left axis reads its series, or, with every left series hidden,
  // the grid follows the right one.
  const leftAxis =
    lefts.length || !rights.length
      ? yAxisOf(lefts, buckets, mark, band, top)
      : undefined
  const rightAxis = rights.length
    ? yAxisOf(rights, buckets, mark, band, top)
    : undefined
  const grid = (leftAxis ?? rightAxis)!
  const scaleOf = (s: ChartSeries) =>
    (s.axis === "right" ? rightAxis : leftAxis)!

  // Each bucket holds one slot per bar series and one per stack, side by
  // side, whichever axis each reads on.
  const slots: string[] = []
  for (const s of series) {
    const m = s.mark ?? mark
    if (m === "bars") slots.push(s.key)
    else if (m === "stack") {
      const key = `stack:${s.stack ?? ""}`
      if (!slots.includes(key)) slots.push(key)
    }
  }
  function slotOf(b: number, slot: string): { left: number; bar: number } {
    const w = windows[b]
    const n = slots.length
    const room = (w.x1 - w.x0) * 0.8
    const bar = Math.max(1, Math.min(MAX_BAR, (room - GAP * (n - 1)) / n))
    const group = n * bar + (n - 1) * GAP
    return {
      left: w.mid - group / 2 + slots.indexOf(slot) * (bar + GAP),
      bar,
    }
  }
  const stacks = new Map<string, ChartSeries[]>()
  for (const s of series) {
    if ((s.mark ?? mark) !== "stack") continue
    const key = s.stack ?? ""
    stacks.set(key, [...(stacks.get(key) ?? []), s])
  }
  const lines = series.filter((s) => (s.mark ?? mark) === "line")

  // The x axis is the whole range, its size's boundaries labelled whether a
  // bucket holds anything there or not. A label needs about 84px, so every
  // k-th boundary is labelled.
  const marks = axisTicks(domain, size)
  const spacing = marks.length > 1 ? x(marks[1]) - x(marks[0]) : right - LEFT
  const every = Math.max(1, Math.ceil(84 / Math.max(1, spacing)))
  const xLabels = marks.flatMap((t, i) => {
    if (i % every) return []
    const previous = i >= every ? marks[i - every] : undefined
    const at = Math.min(Math.max(x(t), LEFT + 28), right - 28)
    return [{ i, at, text: bucketWords(t, size, previous) }]
  })

  function bucketOf(event: PointerEvent<SVGSVGElement>): number {
    const box = event.currentTarget.getBoundingClientRect()
    return bucketAt(event.clientX - box.left, windows)
  }

  function zoom(lo: number, hi: number) {
    const a = buckets[Math.min(lo, hi)]
    const b = buckets[Math.max(lo, hi)]
    if (a && b) onZoom({ start: a.start, end: b.end })
  }

  function onKey(event: KeyboardEvent<HTMLDivElement>) {
    if (!buckets.length) return
    const last = buckets.length - 1
    let next: number | null
    if (event.key === "ArrowRight") {
      next = active === null ? 0 : Math.min(last, active + 1)
    } else if (event.key === "ArrowLeft") {
      next = active === null ? last : Math.max(0, active - 1)
    } else if (event.key === "Home") next = 0
    else if (event.key === "End") next = last
    else if (event.key === "Escape") next = null
    else if (event.key === "Enter" && active !== null) {
      event.preventDefault()
      zoom(active, active)
      return
    } else return
    event.preventDefault()
    setActive(next)
  }

  const selection =
    anchor !== null && active !== null
      ? { lo: Math.min(anchor, active), hi: Math.max(anchor, active) }
      : null
  const tipWidth = 288
  const tipLeft =
    active !== null
      ? windows[active].mid > width - tipWidth - 16
        ? Math.max(0, windows[active].mid - tipWidth - 8)
        : windows[active].mid + 14
      : 0

  return (
    <div
      ref={ref}
      role="group"
      aria-label={label}
      aria-roledescription="chart"
      tabIndex={0}
      onKeyDown={onKey}
      onBlur={() => setActive(null)}
      data-slot="timeline-chart"
      className={cn(
        "relative w-full rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
        stale && "opacity-60 transition-opacity"
      )}
    >
      <svg
        width={width}
        height={height}
        aria-hidden
        className="block touch-none select-none"
        onPointerMove={(e) => {
          const at = bucketOf(e)
          if (at >= 0) setActive(at)
        }}
        onPointerLeave={() => {
          if (anchor === null) setActive(null)
        }}
        onPointerDown={(e) => {
          const at = bucketOf(e)
          if (at < 0) return
          e.currentTarget.setPointerCapture?.(e.pointerId)
          setAnchor(at)
          setActive(at)
        }}
        onPointerUp={(e) => {
          const at = bucketOf(e)
          if (anchor !== null) zoom(anchor, at >= 0 ? at : anchor)
          setAnchor(null)
        }}
      >
        {grid.ticks.map((t) => (
          <line
            key={t}
            x1={LEFT}
            x2={right}
            y1={grid.y(t)}
            y2={grid.y(t)}
            stroke="var(--border)"
            shapeRendering="crispEdges"
          />
        ))}
        {leftAxis && (
          <g data-axis="left">
            {units?.left && (
              <text
                x={LEFT - 8}
                y={top - 10}
                textAnchor="end"
                className="fill-muted-foreground text-[11.5px]"
              >
                {units.left}
              </text>
            )}
            {leftAxis.ticks.map((t, i) => (
              <text
                key={t}
                x={LEFT - 8}
                y={leftAxis.y(t)}
                textAnchor="end"
                dominantBaseline="middle"
                className="fill-faint text-[11.5px] tabular-nums"
              >
                {leftAxis.labels[i]}
              </text>
            ))}
          </g>
        )}
        {rightAxis && (
          <g data-axis="right">
            {units?.right && (
              <text
                x={right + 8}
                y={top - 10}
                textAnchor="start"
                className="fill-muted-foreground text-[11.5px]"
              >
                {units.right}
              </text>
            )}
            {rightAxis.ticks.map((t, i) => (
              <text
                key={t}
                x={right + 8}
                y={rightAxis.y(t)}
                textAnchor="start"
                dominantBaseline="middle"
                className="fill-faint text-[11.5px] tabular-nums"
              >
                {rightAxis.labels[i]}
              </text>
            ))}
          </g>
        )}
        <line
          x1={LEFT}
          x2={right}
          y1={grid.base}
          y2={grid.base}
          stroke="var(--border-strong)"
          shapeRendering="crispEdges"
        />
        {active !== null && (
          <rect
            x={windows[active].x0}
            y={top}
            width={windows[active].x1 - windows[active].x0}
            height={PLOT}
            fill="var(--hover)"
          />
        )}
        {selection && selection.hi > selection.lo && (
          <rect
            data-slot="chart-selection"
            x={windows[selection.lo].x0}
            y={top}
            width={windows[selection.hi].x1 - windows[selection.lo].x0}
            height={PLOT}
            fill="var(--selection)"
          />
        )}
        {series.map((s) => {
          if ((s.mark ?? mark) !== "bars") return null
          const { y, base } = scaleOf(s)
          return (
            <g
              key={s.key}
              data-series={s.key}
              opacity={s.stale ? 0.45 : undefined}
            >
              {s.points.map((p) => {
                if (p.value === null) return null
                const { left, bar } = slotOf(p.bucket, s.key)
                return (
                  <path
                    key={p.bucket}
                    d={barPath(left, bar, base, y(p.value))}
                    fill={s.color}
                  />
                )
              })}
            </g>
          )
        })}
        {[...stacks].map(([stack, members]) => {
          const { y } = scaleOf(members[0])
          return buckets.map((_, b) => {
            const { left, bar } = slotOf(b, `stack:${stack}`)
            let sum = 0
            const parts = members.flatMap((s) => {
              const v = Math.max(0, s.points[b]?.value ?? 0)
              if (!v) return []
              const from = sum
              sum += v
              return [{ s, from, to: sum }]
            })
            return (
              <g key={`${stack}-${b}`}>
                {parts.map(({ s, from, to }, i) => {
                  const lower = from === 0 ? y(from) : y(from) - GAP / 2
                  const upper = y(to) + (i === parts.length - 1 ? 0 : GAP / 2)
                  const d =
                    i === parts.length - 1
                      ? barPath(left, bar, lower, upper)
                      : rectPath(left, bar, lower, upper)
                  return d ? (
                    <path
                      key={s.key}
                      d={d}
                      fill={s.color}
                      data-series={s.key}
                      opacity={s.stale ? 0.45 : undefined}
                    />
                  ) : null
                })}
              </g>
            )
          })
        })}
        {lines.map((s) => (
          <LineSeries
            key={s.key}
            series={s}
            band={s.band ?? band}
            mids={windows.map((w) => w.mid)}
            y={scaleOf(s).y}
          />
        ))}
        {active !== null && (
          <line
            data-slot="chart-crosshair"
            x1={windows[active].mid}
            x2={windows[active].mid}
            y1={top}
            y2={top + PLOT}
            stroke="var(--border-strong)"
          />
        )}
        {active !== null &&
          lines.map((s) => {
            const value = s.points[active]?.value ?? null
            if (value === null) return null
            return (
              <circle
                key={s.key}
                data-slot="chart-marker"
                cx={windows[active].mid}
                cy={scaleOf(s).y(value)}
                r={4}
                fill={s.color}
                stroke="var(--background)"
                strokeWidth={2}
              />
            )
          })}
        {xLabels.map((l) => (
          <text
            key={l.i}
            x={l.at}
            y={top + PLOT + 17}
            textAnchor="middle"
            className="fill-faint text-[11.5px] tabular-nums"
          >
            {l.text}
          </text>
        ))}
      </svg>
      {buckets.length === 0 && (
        <p className="absolute inset-x-0 top-[90px] text-center text-muted-foreground">
          Nothing measured in this range.
        </p>
      )}
      {active !== null && buckets[active] && (
        <div
          role="status"
          data-slot="chart-tooltip"
          className="pointer-events-none absolute top-2 z-10 rounded-md border border-border bg-popover p-2.5 text-[12.5px] text-popover-foreground shadow-card"
          style={{ left: tipLeft, width: tipWidth }}
        >
          {tooltip(active)}
        </div>
      )}
    </div>
  )
}

function LineSeries({
  series,
  band,
  mids,
  y,
}: {
  series: ChartSeries
  band: boolean
  mids: number[]
  y: (v: number) => number
}) {
  const points = series.points
  const line = points.map((p) =>
    p.value === null ? null : { x: mids[p.bucket], y: y(p.value) }
  )
  const spans = points.map((p) =>
    p.low === null || p.high === null
      ? null
      : { x: mids[p.bucket], y0: y(p.low), y1: y(p.high) }
  )
  // A point with no neighbour draws no segment, so it is a dot; the last
  // point is a dot too, the line's end.
  const lastIndex = points.findLastIndex((p) => p.value !== null)
  const dots = points.filter(
    (p, i) =>
      p.value !== null &&
      (i === lastIndex ||
        ((points[i - 1]?.value ?? null) === null &&
          (points[i + 1]?.value ?? null) === null))
  )
  return (
    <g data-series={series.key} opacity={series.stale ? 0.45 : undefined}>
      {band && (
        <path d={bandPath(spans)} fill={series.color} fillOpacity={0.1} />
      )}
      <path
        d={linePath(line)}
        fill="none"
        stroke={series.color}
        strokeWidth={2}
        strokeLinejoin="round"
        strokeLinecap="round"
      />
      {dots.map((p) => (
        <circle
          key={p.bucket}
          cx={mids[p.bucket]}
          cy={y(p.value!)}
          r={4}
          fill={series.color}
          stroke="var(--background)"
          strokeWidth={2}
        />
      ))}
    </g>
  )
}
