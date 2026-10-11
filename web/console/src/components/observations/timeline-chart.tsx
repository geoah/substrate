/** One chart of the timeline, drawn by hand in SVG: a 2px line with its
 * lowest-to-highest band as a 10% wash for a level, bars (24px at most, the
 * data end rounded) for amounts and counts, and bars stacked per category
 * with a 2px surface gap for coded values. One axis, hairline grid, labels in
 * text tokens; colour carries identity alone and the legend and the table
 * under the chart carry it too.
 *
 * The hover layer is the bucket, not the mark: a column under the pointer is
 * tinted (a crosshair marks a line's), and one tooltip lists every series at
 * that bucket. A click zooms into the bucket and a drag across several zooms
 * into them all; from the keyboard the arrows move between buckets, Enter
 * zooms and Escape leaves. */

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
} from "@/lib/chart"
import {
  axisTicks,
  bucketWords,
  type Bucket,
  type BucketSize,
  type SeriesPoint,
  type TimeRange,
} from "@/lib/observations"
import { cn } from "@/lib/utils"

export type ChartMark = "line" | "bars" | "stack"

export interface ChartSeries {
  key: string
  /** The group value the series answers, where the chart is grouped. */
  value?: string
  label: string
  /** A CSS colour, a `--series-N` token. */
  color: string
  points: SeriesPoint[]
}

const TOP = 10
const PLOT = 200
const AXIS = 26
const LEFT = 52
const RIGHT = 14
const HEIGHT = TOP + PLOT + AXIS
const GAP = 2
const MAX_BAR = 24

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
  mark,
  band = false,
  buckets,
  series,
  domain,
  size,
  label,
  tooltip,
  onZoom,
  stale = false,
}: {
  mark: ChartMark
  /** Draw each series' lowest-to-highest band (a level). */
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
  /** A newer answer is loading: hold this one, quieter. */
  stale?: boolean
}) {
  const { ref, width } = useWidth(720)
  const [held, setActive] = useState<number | null>(null)
  const [heldAnchor, setAnchor] = useState<number | null>(null)
  // A bucket index outlives the answer it pointed into: a zoom or a new read
  // brings fewer buckets while the pointer still rests on the chart.
  const active = held !== null && held < buckets.length ? held : null
  const anchor =
    heldAnchor !== null && heldAnchor < buckets.length ? heldAnchor : null

  const right = Math.max(LEFT + 40, width - RIGHT)
  const x = linearScale([domain.start, domain.end], [LEFT, right])
  const windows = buckets.map((b) => {
    const x0 = x(Math.max(b.start, domain.start))
    const x1 = x(Math.min(b.end, domain.end))
    return { x0, x1: Math.max(x1, x0 + 1), mid: (x0 + x1) / 2 }
  })

  const values: number[] = []
  if (mark === "stack") {
    buckets.forEach((_, i) => {
      let sum = 0
      for (const s of series) sum += Math.max(0, s.points[i]?.value ?? 0)
      values.push(sum)
    })
  } else {
    for (const s of series) {
      for (const p of s.points) {
        if (p.value !== null) values.push(p.value)
        if (band && p.low !== null) values.push(p.low)
        if (band && p.high !== null) values.push(p.high)
      }
    }
  }
  const { domain: yDomain, ticks } = niceDomain(values, {
    zero: mark !== "line",
  })
  const y = linearScale(yDomain, [TOP + PLOT, TOP])
  const base = mark === "line" ? TOP + PLOT : y(Math.max(yDomain[0], 0))

  const yLabels = tickLabels(ticks)
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
  const tipLeft =
    active !== null
      ? windows[active].mid > width - 280
        ? Math.max(0, windows[active].mid - 272)
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
        height={HEIGHT}
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
        {ticks.map((t, i) => (
          <g key={t}>
            <line
              x1={LEFT}
              x2={right}
              y1={y(t)}
              y2={y(t)}
              stroke="var(--border)"
              shapeRendering="crispEdges"
            />
            <text
              x={LEFT - 8}
              y={y(t)}
              textAnchor="end"
              dominantBaseline="middle"
              className="fill-faint text-[11.5px] tabular-nums"
            >
              {yLabels[i]}
            </text>
          </g>
        ))}
        <line
          x1={LEFT}
          x2={right}
          y1={base}
          y2={base}
          stroke="var(--border-strong)"
          shapeRendering="crispEdges"
        />
        {active !== null && (
          <rect
            x={windows[active].x0}
            y={TOP}
            width={windows[active].x1 - windows[active].x0}
            height={PLOT}
            fill="var(--hover)"
          />
        )}
        {selection && selection.hi > selection.lo && (
          <rect
            data-slot="chart-selection"
            x={windows[selection.lo].x0}
            y={TOP}
            width={windows[selection.hi].x1 - windows[selection.lo].x0}
            height={PLOT}
            fill="var(--selection)"
          />
        )}
        {mark === "line" &&
          series.map((s) => (
            <LineSeries
              key={s.key}
              series={s}
              band={band}
              mids={windows.map((w) => w.mid)}
              y={y}
            />
          ))}
        {mark === "bars" &&
          series.map((s, i) => (
            <g key={s.key} data-series={s.key}>
              {s.points.map((p) => {
                if (p.value === null) return null
                const w = windows[p.bucket]
                const n = series.length
                const slot = (w.x1 - w.x0) * 0.8
                const bar = Math.max(
                  1,
                  Math.min(MAX_BAR, (slot - GAP * (n - 1)) / n)
                )
                const group = n * bar + (n - 1) * GAP
                const left = w.mid - group / 2 + i * (bar + GAP)
                return (
                  <path
                    key={p.bucket}
                    d={barPath(left, bar, base, y(p.value))}
                    fill={s.color}
                  />
                )
              })}
            </g>
          ))}
        {mark === "stack" &&
          buckets.map((_, b) => {
            const w = windows[b]
            const bar = Math.max(1, Math.min(MAX_BAR, (w.x1 - w.x0) * 0.8))
            const left = w.mid - bar / 2
            let sum = 0
            const parts = series.flatMap((s) => {
              const v = Math.max(0, s.points[b]?.value ?? 0)
              if (!v) return []
              const from = sum
              sum += v
              return [{ s, from, to: sum }]
            })
            return (
              <g key={b}>
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
                    />
                  ) : null
                })}
              </g>
            )
          })}
        {active !== null && mark === "line" && (
          <line
            x1={windows[active].mid}
            x2={windows[active].mid}
            y1={TOP}
            y2={TOP + PLOT}
            stroke="var(--border-strong)"
          />
        )}
        {xLabels.map((l) => (
          <text
            key={l.i}
            x={l.at}
            y={TOP + PLOT + 17}
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
          className="pointer-events-none absolute top-2 z-10 w-64 rounded-md border border-border bg-popover p-2.5 text-[12.5px] text-popover-foreground shadow-card"
          style={{ left: tipLeft }}
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
    <g data-series={series.key}>
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
