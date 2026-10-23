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
 * between buckets, Enter zooms and Escape leaves.
 *
 * A line breaks where a bucket holds no value. With `connect` each break is
 * bridged by a dashed segment, so the line runs on and the gap still shows. */

import {
  memo,
  useEffect,
  useMemo,
  useState,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
} from "react"

import { useWidth } from "@/hooks/use-width"
import {
  bandPath,
  barPath,
  bucketAt,
  gapPath,
  linePath,
  linearScale,
  niceDomain,
  rectPath,
  tickLabels,
  type Scale,
} from "@/lib/chart"
import type { BucketSize, TimeRange } from "@/lib/observations-time"
import { AXIS_LEFT, timeAxis } from "@/lib/time-axis"
import type { Bucket, SeriesPoint } from "@/lib/observations-answer"
import type { AxisSide, ChartMark } from "@/lib/overlay"

export type { ChartMark }

export interface ChartSeries {
  key: string
  /** A CSS colour, a `--series-N` token. */
  color: string
  points: SeriesPoint[]
  /** How it is drawn; a line where absent. */
  mark?: ChartMark
  /** Draw its lowest-to-highest band (a level). */
  band?: boolean
  /** The axis it reads on; the left where absent. */
  axis?: AxisSide
  /** The stack a category belongs to (its metric): the categories of one
   * stack pile up in one bar. */
  stack?: string
  /** A newer answer is loading for this series: hold this one, quieter. */
  stale?: boolean
  /** Draw its line dashed (a rolling average), with no end dots. */
  dash?: boolean
}

/** A labelled value range washed behind the plot on the left axis: from
 * `low` to `high`, an absent bound reaching the plot's edge. `color` is
 * drawn as given, a soft status token. */
export interface ChartBand {
  low?: number
  high?: number
  color: string
  label?: string
}

const TOP = 10
/** Room above the plot for the axes' unit captions. */
const CAPTION = 14
/** The plot's height where the caller names none; a strip is 120. */
const PLOT = 200
const AXIS = 26
const LEFT = AXIS_LEFT
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
 * a level's band) and the `bounds` that must stay on it (the bands'),
 * anchored at zero where any is a bar. */
function yAxisOf(
  series: readonly ChartSeries[],
  buckets: readonly Bucket[],
  top: number,
  plot: number,
  bounds: readonly number[] = []
): YAxis {
  const values: number[] = bounds.filter((b) => Number.isFinite(b))
  const stacks = new Map<string, ChartSeries[]>()
  for (const s of series) {
    const m = s.mark ?? "line"
    if (m === "stack") {
      const key = s.stack ?? ""
      stacks.set(key, [...(stacks.get(key) ?? []), s])
      continue
    }
    const withBand = m === "line" && s.band === true
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
  const zero = series.some((s) => !["line", "level"].includes(s.mark ?? "line"))
  const { domain, ticks } = niceDomain(values, { zero })
  const y = linearScale(domain, [top + plot, top])
  return {
    y,
    ticks,
    labels: tickLabels(ticks),
    base: zero ? y(Math.max(domain[0], 0)) : top + plot,
  }
}

export function TimelineChart({
  buckets,
  series,
  domain,
  size,
  label,
  tooltip,
  onZoom,
  units,
  autoFocus = false,
  bands,
  highlight,
  height: plotHeight = PLOT,
  connect = false,
}: {
  buckets: Bucket[]
  series: ChartSeries[]
  /** The window read, the x axis's extent. */
  domain: TimeRange
  size: BucketSize
  /** The chart's accessible name. */
  label: string
  /** What the tooltip says about one bucket. */
  tooltip: (bucket: number) => ReactNode
  /** A window to read, and whether the keyboard asked for it. */
  onZoom: (range: TimeRange, keyboard: boolean) => void
  /** Each axis's unit in words, captioned above it. */
  units?: { left?: string; right?: string }
  /** Take the focus on mount: the chart a keyboard zoom asked for. */
  autoFocus?: boolean
  /** Value ranges washed behind the plot on the left axis. */
  bands?: ChartBand[]
  /** A bucket index whose column is tinted under the figures. */
  highlight?: number
  /** The plot's height in pixels, 200 by default. */
  height?: number
  /** Bridge each line's breaks with a dashed segment. */
  connect?: boolean
}) {
  const { ref, width } = useWidth(720)
  useEffect(() => {
    if (autoFocus) ref.current?.focus()
    // On mount alone: a later render never takes the focus back.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])
  const [held, setActive] = useState<number | null>(null)
  const [heldAnchor, setAnchor] = useState<number | null>(null)
  // A bucket index outlives the answer it pointed into: a zoom or a new read
  // brings fewer buckets while the pointer still rests on the chart.
  const active = held !== null && held < buckets.length ? held : null
  const anchor =
    heldAnchor !== null && heldAnchor < buckets.length ? heldAnchor : null

  // Laid out once per answer and width: hovering moves the crosshair and
  // the tooltip, never the marks.
  const layout = useMemo(
    () =>
      layoutOf(
        buckets,
        series,
        domain,
        size,
        width,
        Boolean(units?.left || units?.right),
        plotHeight,
        bands ?? NO_BANDS
      ),
    [
      buckets,
      series,
      domain,
      size,
      width,
      units?.left,
      units?.right,
      plotHeight,
      bands,
    ]
  )
  const { top, height, plot, windows, lines, scaleOf } = layout
  const lit =
    highlight !== undefined &&
    Number.isInteger(highlight) &&
    highlight >= 0 &&
    highlight < windows.length
      ? windows[highlight]
      : undefined

  function bucketOf(event: PointerEvent<SVGSVGElement>): number {
    const box = event.currentTarget.getBoundingClientRect()
    return bucketAt(event.clientX - box.left, windows)
  }

  // A recorded-local date zooms to the instants its rows cover: the day as
  // it was lived, wherever its slot sits on the reader's axis.
  function zoom(lo: number, hi: number, keyboard = false) {
    const a = buckets[Math.min(lo, hi)]
    const b = buckets[Math.max(lo, hi)]
    if (a && b) {
      onZoom(
        {
          start: a.instants?.start ?? a.start,
          end: b.instants?.end ?? b.end,
        },
        keyboard
      )
    }
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
      zoom(active, active, true)
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
      className="relative w-full rounded-md outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
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
          // Released outside every window, the run is the one highlighted:
          // the anchor to the last bucket the drag reached.
          if (anchor !== null) zoom(anchor, at >= 0 ? at : (active ?? anchor))
          setAnchor(null)
        }}
      >
        <Ground layout={layout} units={units} />
        {active !== null && (
          <rect
            x={windows[active].x0}
            y={top}
            width={windows[active].x1 - windows[active].x0}
            height={plot}
            fill="var(--hover)"
          />
        )}
        {lit && (
          <rect
            data-slot="chart-highlight"
            x={lit.x0}
            y={top}
            width={lit.x1 - lit.x0}
            height={plot}
            fill="var(--selection)"
          />
        )}
        {selection && selection.hi > selection.lo && (
          <rect
            data-slot="chart-selection"
            x={windows[selection.lo].x0}
            y={top}
            width={windows[selection.hi].x1 - windows[selection.lo].x0}
            height={plot}
            fill="var(--selection)"
          />
        )}
        <Figures
          layout={layout}
          series={series}
          buckets={buckets}
          connect={connect}
        />
        {active !== null && (
          <line
            data-slot="chart-crosshair"
            x1={windows[active].mid}
            x2={windows[active].mid}
            y1={top}
            y2={top + plot}
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
      </svg>
      {buckets.length === 0 && (
        <p
          className="absolute inset-x-0 text-center text-muted-foreground"
          style={{ top: top + plot / 2 - 20 }}
        >
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

/** Everything a chart draws that hovering does not move: its size, the x
 * scale and the buckets' windows on it, the y axes, the bar slots and
 * stacks, and the x axis's labels. */
interface Layout {
  top: number
  /** The plot's own height, between the top and the x axis. */
  plot: number
  height: number
  right: number
  windows: { x0: number; x1: number; mid: number }[]
  leftAxis?: YAxis
  rightAxis?: YAxis
  /** The axis the grid follows. */
  grid: YAxis
  scaleOf: (s: ChartSeries) => YAxis
  lines: ChartSeries[]
  stacks: Map<string, ChartSeries[]>
  slotOf: (b: number, slot: string) => { left: number; bar: number }
  xLabels: { i: number; at: number; text: string }[]
  /** The bands, scaled on the left axis (or the grid's). */
  bands: { y0: number; y1: number; color: string; label?: string }[]
}

const NO_BANDS: ChartBand[] = []

function layoutOf(
  buckets: readonly Bucket[],
  series: readonly ChartSeries[],
  domain: TimeRange,
  size: BucketSize,
  width: number,
  captioned: boolean,
  plot: number = PLOT,
  bands: readonly ChartBand[] = NO_BANDS
): Layout {
  const lefts = series.filter((s) => (s.axis ?? "left") === "left")
  const rights = series.filter((s) => s.axis === "right")
  const top = captioned ? TOP + CAPTION : TOP
  const height = top + plot + AXIS
  const axis = timeAxis(domain, size, width, rights.length > 0)
  const { x, right } = axis
  const windows = buckets.map((b) => {
    const x0 = x(Math.max(b.start, domain.start))
    const x1 = x(Math.min(b.end, domain.end))
    return { x0, x1: Math.max(x1, x0 + 1), mid: (x0 + x1) / 2 }
  })

  // The left axis reads its series, or, with every left series hidden,
  // the grid follows the right one.
  const bounds = bands.flatMap((b) => [b.low, b.high]).filter(isNumber)
  const leftAxis =
    lefts.length || !rights.length
      ? yAxisOf(lefts, buckets, top, plot, bounds)
      : undefined
  const rightAxis = rights.length
    ? yAxisOf(rights, buckets, top, plot)
    : undefined
  const grid = (leftAxis ?? rightAxis)!
  const scaleOf = (s: ChartSeries) =>
    (s.axis === "right" ? rightAxis : leftAxis)!

  // Each bucket holds one slot per bar series and one per stack, side by
  // side, whichever axis each reads on.
  const slots: string[] = []
  for (const s of series) {
    const m = s.mark ?? "line"
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
    if (s.mark !== "stack") continue
    const key = s.stack ?? ""
    stacks.set(key, [...(stacks.get(key) ?? []), s])
  }
  const lines = series.filter((s) =>
    ["line", "level"].includes(s.mark ?? "line")
  )

  const xLabels = axis.labels
  const bandAxis = leftAxis ?? grid
  const scaledBands = bands.map((b) => {
    const edge = (v: number | undefined, open: number) =>
      v === undefined || !Number.isFinite(v)
        ? open
        : Math.min(top + plot, Math.max(top, bandAxis.y(v)))
    const lowY = edge(b.low, top + plot)
    const highY = edge(b.high, top)
    return {
      y0: Math.min(lowY, highY),
      y1: Math.max(lowY, highY),
      color: b.color,
      label: b.label,
    }
  })

  return {
    top,
    plot,
    height,
    right,
    windows,
    leftAxis,
    rightAxis,
    grid,
    scaleOf,
    lines,
    stacks,
    slotOf,
    xLabels,
    bands: scaledBands,
  }
}

function isNumber(v: number | undefined): v is number {
  return v !== undefined && Number.isFinite(v)
}

/** The grid, the y axes with their captions, and the baseline: under the
 * hovered column. */
const Ground = memo(function Ground({
  layout,
  units,
}: {
  layout: Layout
  units?: { left?: string; right?: string }
}) {
  const { top, right, grid, leftAxis, rightAxis, bands } = layout
  return (
    <>
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
      {bands.map((b, i) => (
        <g key={i}>
          <rect
            data-slot="chart-band"
            x={LEFT}
            y={b.y0}
            width={Math.max(0, right - LEFT)}
            height={b.y1 - b.y0}
            fill={b.color}
          />
          {b.label && b.y1 - b.y0 >= 12 && (
            <text
              x={right - 4}
              y={(b.y0 + b.y1) / 2}
              textAnchor="end"
              dominantBaseline="middle"
              className="fill-faint text-[11.5px]"
            >
              {b.label}
            </text>
          )}
        </g>
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
    </>
  )
})

/** The bars, the stacks, the lines and the x axis's labels: over the
 * hovered column, under the crosshair. */
const Figures = memo(function Figures({
  layout,
  series,
  buckets,
  connect,
}: {
  layout: Layout
  series: readonly ChartSeries[]
  buckets: readonly Bucket[]
  connect: boolean
}) {
  const { top, plot, windows, scaleOf, slotOf, stacks, lines, xLabels } = layout
  const mids = windows.map((w) => w.mid)
  return (
    <>
      {series.map((s) => {
        if (s.mark !== "bars") return null
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
      {lines.map((s) =>
        s.mark === "level" ? (
          <LevelSeries
            key={s.key}
            series={s}
            windows={layout.windows}
            y={scaleOf(s).y}
          />
        ) : (
          <LineSeries
            key={s.key}
            series={s}
            band={s.band === true}
            mids={mids}
            y={scaleOf(s).y}
            connect={connect}
          />
        )
      )}
      {xLabels.map((l) => (
        <text
          key={l.i}
          x={l.at}
          y={top + plot + 17}
          textAnchor="middle"
          className="fill-faint text-[11.5px] tabular-nums"
        >
          {l.text}
        </text>
      ))}
    </>
  )
})

/** A level series: each value a horizontal stroke across its bucket's
 * whole width, a run of one value in one stroke, nothing where a bucket
 * holds none and no stroke joining two values. */
function LevelSeries({
  series,
  windows,
  y,
}: {
  series: ChartSeries
  windows: readonly { x0: number; x1: number }[]
  y: (v: number) => number
}) {
  let d = ""
  let run: { x0: number; x1: number; value: number } | null = null
  const flush = () => {
    if (run) d += `M${run.x0} ${y(run.value)}H${run.x1}`
    run = null
  }
  for (const p of series.points) {
    const w = windows[p.bucket]
    if (p.value === null || !w) {
      flush()
      continue
    }
    if (run && run.value === p.value && run.x1 === w.x0) run.x1 = w.x1
    else {
      flush()
      run = { x0: w.x0, x1: w.x1, value: p.value }
    }
  }
  flush()
  return (
    <path
      data-series={series.key}
      data-slot="chart-level"
      d={d}
      fill="none"
      stroke={series.color}
      strokeWidth={2}
      strokeLinecap="round"
      opacity={series.stale ? 0.45 : undefined}
    />
  )
}

function LineSeries({
  series,
  band,
  mids,
  y,
  connect,
}: {
  series: ChartSeries
  band: boolean
  mids: number[]
  y: (v: number) => number
  connect: boolean
}) {
  // A bucket the answer covered with no reading of its own (a weigh-in's
  // level holding between weigh-ins: a row, its count 0, no value) joins
  // the line across it; a bucket with no row, or a gap the read filled,
  // still breaks it.
  const points = series.points.filter(
    (p) => !(p.value === null && p.row && p.row.count === 0 && !p.row.synthetic)
  )
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
      !series.dash &&
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
        strokeDasharray={series.dash ? "4 2" : undefined}
      />
      {connect && (
        <path
          data-slot="chart-gap"
          d={gapPath(line)}
          fill="none"
          stroke={series.color}
          strokeWidth={1.5}
          strokeOpacity={0.7}
          strokeDasharray="3 3"
          strokeLinecap="round"
        />
      )}
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
