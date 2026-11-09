import { cx } from "@substrate/react"
import {
  memo,
  useEffect,
  useId,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type CSSProperties,
  type FocusEvent,
  type HTMLAttributes,
  type KeyboardEvent,
  type PointerEvent,
  type ReactNode,
  type RefObject,
} from "react"
import {
  defaultTick,
  emptyLabelFits,
  emptySpan,
  gutterWidth,
  isTap,
  markerRadius,
  medianSpacing,
  nearestIndex,
  readoutAnchor,
  readoutSide,
  tickStep,
  timeScale,
  timeTickCount,
  toMs,
  valueDomain,
  yearBoundaries,
  type TrendMarker,
  type TrendTime,
} from "../lib/trendMath"
import { Bubble } from "./Bubble"
import { ChartKey, type ChartKeyItem } from "./ChartKey"
import { MARKER_R, MarkerShape } from "./MarkerShape"
import { FILL, INK, type Tone } from "./tone"
import type { ZoneBarLevel } from "./ZoneBar"

export type { TrendMarker, TrendTime }

/** One reading of the series. */
export interface TrendPoint {
  at: TrendTime
  value: number
  /** Tones this point's marker and rings it: a reading out of range
   * (`danger`) or off target (`warning`). The line keeps the chart's
   * `tone`; status is never colour alone, so a toned point says why in
   * `detail` or in the caller's readout. */
  tone?: Tone
  /** Extra lines for the default readout, under the value and date ("Lab
   * A", "High"). Text only. */
  detail?: string[]
  /** Marker shape. Default `circle`. */
  marker?: TrendMarker
  /** The series or source key a `legend` item names; the legend item of
   * the active point is emphasised. */
  series?: string
}

/** A horizontal band of values behind the line: a target zone, a lab's
 * range, a cut-off. `from` and `to` are in the series' unit; leave one out
 * for an open band ("above 3.0"). A `fill` band washes its tone (a
 * `neutral` one is the lab range's grey); an `outline` draws its edges
 * only, dashed by default. */
export interface TrendBand {
  from?: number
  to?: number
  tone: Tone
  /** Names the band in the chart's text alternative. */
  label: string
  variant?: "fill" | "outline"
  /** Wash strength of a `fill` band, 1 faint to 4 strong, as a ZoneBar
   * segment's level, so a detail chart carries a bar's zones in the bar's
   * colours. Omitted: the plain wash. */
  level?: ZoneBarLevel
  /** Edge pattern: `dashed` (4 3) or `dotted` (1 3, round caps). A fill
   * band draws its finite edges too when this is set (the lab range: a
   * grey wash with dotted edges). */
  stroke?: "dashed" | "dotted"
}

/** A reference series drawn muted under the line: a rolling mean, with an
 * optional `low` and `high` envelope drawn as a ribbon. */
export interface TrendBaselinePoint {
  at: TrendTime
  value?: number
  low?: number
  high?: number
}

export interface TrendChartProps extends Omit<
  HTMLAttributes<HTMLDivElement>,
  "children"
> {
  /** The series, in any order; it is drawn sorted by time. */
  data: TrendPoint[]
  /** Names the chart for assistive technology ("LDL cholesterol over
   * time"). */
  label: string
  bands?: TrendBand[]
  baseline?: TrendBaselinePoint[]
  /** Unit shown after values in the readout and the text alternative
   * ("mmol/L"). */
  unit?: string
  /** Formats a value for the readout and the text alternative. Default:
   * up to two decimals. */
  formatValue?: (value: number) => string
  /** Formats a value-axis tick, given the step between ticks. Default: the
   * step's own decimals with en-GB grouping ("10,000", "2.5"). */
  formatTick?: (value: number, step: number) => string
  /** Formats a moment for the axis and the readout. Default "12 Mar
   * 2026". */
  formatTime?: (at: Date) => string
  /** The span the time axis covers; the data's own span by default.
   * Points outside it are not drawn. A span of 18 months or more draws a
   * hairline at each 1 January, so stacked charts align visibly. */
  timeRange?: { start?: TrendTime; end?: TrendTime }
  /** The line's colour. Default `accent`; `neutral` is the label's ink, for
   * a chart whose bands carry the colour. */
  tone?: Tone
  /** Plot height in px, axes included; the legend sits below it. Default
   * 200. The cards use 160 (128 on a phone). */
  height?: number
  /** Shown over an empty plot. Default "No data". */
  empty?: ReactNode
  /** Hatches the span from `timeRange.start` to the first point (a day or
   * more) and labels it, inside it when it fits; the label is always in the
   * text alternative. `true` labels it "No data before Mar 2025". */
  emptyBefore?: boolean | { label: (first: Date) => string }
  /** A dashed straight line between two points, over the bands and under
   * the series: a least-squares fit the caller computed. `label` joins the
   * text alternative. */
  trendline?: { from: TrendPoint; to: TrendPoint; label?: string }
  /** A key under the plot; the item whose `key` is the active point's
   * `series` is emphasised. A screen of many charts shows one ChartKey
   * instead. */
  legend?: ChartKeyItem[]
  /** Replaces the default readout (value, date, detail). Text only. */
  renderReadout?: (point: TrendPoint) => ReactNode
  /** `tooltip` (default) floats a readout over the plot; `none` draws
   * none, so the caller shows the point itself through `onActiveChange`
   * (the cards swap their headline). Crosshair, keys, touch and pinning
   * work the same either way. */
  readout?: "tooltip" | "none"
  /** Fires when this chart's own active point or pin changes (pointer,
   * arrow keys, touch, blur, Escape), with `null` when it clears; never for
   * a crosshair another chart drew through `syncKey`. */
  onActiveChange?: (point: TrendPoint | null, isPinned: boolean) => void
  /** A fixed value-axis gutter in px, so charts with different tick labels
   * share one plot start. Default: the widest tick label, at least 45 px. */
  gutter?: number
  /** Charts sharing a key show a crosshair at the hovered date together;
   * only the chart under the pointer reports its point. */
  syncKey?: string
  /** Forces the value axis to cover at least this range. */
  valueRange?: { min?: number; max?: number }
}

const PAD = { top: 12, end: 12, bottom: 24 }
/** First guess at the readout's size, before it is measured. */
const BUBBLE_GUESS = { w: 144, h: 40 }

const defaultValue = (v: number) =>
  v.toLocaleString("en-GB", { maximumFractionDigits: 2 })
const defaultTime = (d: Date) =>
  d.toLocaleDateString("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
  })
const defaultEmptyLabel = (d: Date) =>
  `No data before ${d.toLocaleDateString("en-GB", { month: "short", year: "numeric" })}`

// Charts that share a `syncKey`. Each listener receives the hovered time
// (or null when the pointer leaves) and the token of the chart that moved.
type SyncListener = (t: number | null, from: object) => void
const syncCharts = new Map<string, Set<SyncListener>>()

/** Width of the element, following resizes. */
function useWidth<T extends HTMLElement>(): [RefObject<T | null>, number] {
  const ref = useRef<T>(null)
  const [width, setWidth] = useState(0)
  // Measured before the first paint, so a chart never shows a frame
  // without its marks; then kept by the observer.
  useLayoutEffect(() => {
    const el = ref.current
    if (!el) return
    setWidth(el.clientWidth)
    if (typeof ResizeObserver === "undefined") return
    const ro = new ResizeObserver(([entry]) =>
      setWidth(entry.contentRect.width)
    )
    ro.observe(el)
    return () => ro.disconnect()
  }, [])
  return [ref, width]
}

/** A drawn point: the caller's point plus its time in ms. */
interface Pt {
  src: TrendPoint
  t: number
  value: number
  tone?: Tone
  marker?: TrendMarker
}

/** Everything that places marks, computed once per data or size change. */
interface Geometry {
  width: number
  height: number
  start: number
  plotW: number
  plotH: number
  tStart?: number
  tEnd?: number
  vLo: number
  vHi: number
  ticks: number[]
  tickLabels: string[]
  x: (t: number) => number
  y: (v: number) => number
  /** x of every point, in time order. */
  xs: number[]
  /** Points closer than 10 px draw as 2 px dots, not 4 px markers. */
  isDense: boolean
}

const LEVEL_MIX = [8, 11, 14, 18]

function bandFill(b: TrendBand): string {
  if (b.tone === "neutral")
    return b.level
      ? "color-mix(in srgb, var(--hs-zone-neutral) 35%, transparent)"
      : "var(--hs-range)"
  const mix = b.level ? LEVEL_MIX[b.level - 1] : 12
  return `color-mix(in srgb, ${FILL[b.tone]} ${mix}%, transparent)`
}

function edgeStyle(tone: Tone, stroke: "dashed" | "dotted"): CSSProperties {
  return {
    stroke: tone === "neutral" ? "var(--hs-range-edge)" : INK[tone],
    strokeWidth: stroke === "dotted" ? 1 : 1.5,
    strokeDasharray: stroke === "dotted" ? "1 3" : "4 3",
    strokeLinecap: stroke === "dotted" ? "round" : "butt",
  }
}

function BandEdges(props: {
  band: TrendBand
  stroke: "dashed" | "dotted"
  x0: number
  x1: number
  top: number
  bottom: number
}) {
  const { band: b, x0, x1, top, bottom } = props
  const style = edgeStyle(b.tone, props.stroke)
  return (
    <g style={style}>
      {b.to !== undefined && <line x1={x0} x2={x1} y1={top} y2={top} />}
      {b.from !== undefined && <line x1={x0} x2={x1} y1={bottom} y2={bottom} />}
    </g>
  )
}

interface StaticProps {
  geo: Geometry
  points: Pt[]
  base: { t: number; value?: number; low?: number; high?: number }[]
  bands?: TrendBand[]
  tone: Tone
  gap?: { from: number; to: number }
  gapLabel?: string
  trend?: { t1: number; v1: number; t2: number; v2: number }
  formatTime: (at: Date) => string
  uid: string
}

/** The marks that do not move with the crosshair: bands, grid, axes, the
 * empty span, year hairlines, trendline, baseline, line and markers.
 * Memoised, so hovering re-renders only the overlay. */
const TrendStatic = memo(function TrendStatic({
  geo,
  points,
  base,
  bands,
  tone,
  gap,
  gapLabel,
  trend,
  formatTime,
  uid,
}: StaticProps) {
  const {
    width,
    height,
    start,
    plotW,
    plotH,
    tStart,
    tEnd,
    vLo,
    vHi,
    ticks,
    tickLabels,
    x,
    y,
  } = geo
  const clampY = (v: number) =>
    Math.min(PAD.top + plotH, Math.max(PAD.top, y(v)))
  const line = points
    .map((p) => `${x(p.t).toFixed(1)},${y(p.value).toFixed(1)}`)
    .join(" ")
  const baseLine = base
    .filter((p) => typeof p.value === "number")
    .map((p) => `${x(p.t).toFixed(1)},${y(p.value as number).toFixed(1)}`)
    .join(" ")
  const ribbonPts = base.filter(
    (p) => typeof p.low === "number" && typeof p.high === "number"
  )
  const ribbon = ribbonPts.length
    ? [
        ...ribbonPts.map(
          (p) => `${x(p.t).toFixed(1)},${y(p.high as number).toFixed(1)}`
        ),
        ...[...ribbonPts]
          .reverse()
          .map((p) => `${x(p.t).toFixed(1)},${y(p.low as number).toFixed(1)}`),
      ].join(" ")
    : ""
  const tSpan = tEnd !== undefined && tStart !== undefined ? tEnd - tStart : 0
  // Time labels: start, middle and end, or start and end when three would
  // not fit the plot.
  const threeTicks =
    tStart !== undefined && tEnd !== undefined && tSpan > 0
      ? [tStart, tStart + tSpan / 2, tEnd]
      : tStart !== undefined
        ? [tStart]
        : []
  const timeLabels = threeTicks.map((t) => formatTime(new Date(t)))
  const timeTicks =
    threeTicks.length === 3 &&
    timeTickCount(plotW, Math.max(...timeLabels.map((l) => l.length))) === 2
      ? [threeTicks[0], threeTicks[2]]
      : threeTicks
  const years =
    tStart !== undefined && tEnd !== undefined && points.length
      ? yearBoundaries(tStart, tEnd)
      : []
  const gapW = gap ? x(gap.to) - x(gap.from) : 0

  return (
    <svg
      className="block overflow-visible"
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      aria-hidden
    >
      <defs>
        <clipPath id={`${uid}-clip`}>
          <rect x={start} y={PAD.top} width={plotW} height={plotH} />
        </clipPath>
        <pattern
          id={`${uid}-hatch`}
          width={6}
          height={6}
          patternUnits="userSpaceOnUse"
          patternTransform="rotate(45)"
        >
          <line
            x1={0}
            y1={0}
            x2={0}
            y2={6}
            style={{ stroke: "var(--hs-hatch)", strokeWidth: 1 }}
          />
        </pattern>
      </defs>
      {(bands ?? []).map((b, i) => {
        const top = clampY(b.to ?? vHi)
        const bottom = clampY(b.from ?? vLo)
        // Two bands may share a label and edges (a target and a lab cut-off
        // both called "High" from 5), so the position keeps keys unique.
        const key = `${i}-${b.label}-${b.from}-${b.to}`
        const edges = (stroke: "dashed" | "dotted") => (
          <BandEdges
            band={b}
            stroke={stroke}
            x0={start}
            x1={start + plotW}
            top={top}
            bottom={bottom}
          />
        )
        if (b.variant === "outline")
          return <g key={key}>{edges(b.stroke ?? "dashed")}</g>
        return (
          <g key={key} data-band={b.tone}>
            <rect
              x={start}
              y={top}
              width={plotW}
              height={Math.max(0, bottom - top)}
              style={{ fill: bandFill(b) }}
            />
            {b.stroke && edges(b.stroke)}
          </g>
        )
      })}
      {points.length > 0 &&
        ticks.map((v, i) => (
          <g key={v}>
            <line
              x1={start}
              x2={start + plotW}
              y1={y(v)}
              y2={y(v)}
              className="stroke-separator/50"
              strokeWidth={1}
            />
            <text
              x={start - 6}
              y={y(v)}
              dy="0.32em"
              textAnchor="end"
              className="fill-secondary text-[11px] tabular-nums"
            >
              {tickLabels[i]}
            </text>
          </g>
        ))}
      {gap && (
        <g data-empty-span>
          <rect
            x={x(gap.from)}
            y={PAD.top}
            width={Math.max(0, gapW)}
            height={plotH}
            style={{ fill: "var(--hs-chart-bg)" }}
          />
          <rect
            x={x(gap.from)}
            y={PAD.top}
            width={Math.max(0, gapW)}
            height={plotH}
            fill={`url(#${uid}-hatch)`}
          />
        </g>
      )}
      {years.map((t) => (
        <line
          key={t}
          x1={x(t)}
          x2={x(t)}
          y1={PAD.top}
          y2={PAD.top + plotH}
          className="stroke-separator/50"
          strokeWidth={1}
        />
      ))}
      {gap && emptyLabelFits(gapW, gapLabel?.length ?? 0) && (
        <text
          x={x(gap.from) + gapW / 2}
          y={PAD.top + plotH / 2}
          dy="0.32em"
          textAnchor="middle"
          className="fill-secondary text-[12px]"
          style={{
            paintOrder: "stroke",
            stroke: "var(--hs-chart-bg)",
            strokeWidth: 4,
            strokeLinejoin: "round",
          }}
        >
          {gapLabel}
        </text>
      )}
      {timeTicks.map((t, i) => (
        <text
          key={t}
          x={x(t)}
          y={height - 6}
          className="fill-secondary text-[11px] tabular-nums"
          textAnchor={
            timeTicks.length === 1
              ? "middle"
              : i === 0
                ? "start"
                : i === timeTicks.length - 1
                  ? "end"
                  : "middle"
          }
        >
          {formatTime(new Date(t))}
        </text>
      ))}
      {trend && points.length > 0 && (
        <line
          data-trend
          x1={x(trend.t1)}
          y1={y(trend.v1)}
          x2={x(trend.t2)}
          y2={y(trend.v2)}
          clipPath={`url(#${uid}-clip)`}
          style={{
            stroke: "var(--hs-trend)",
            strokeWidth: 1.5,
            strokeDasharray: "6 4",
          }}
        />
      )}
      {ribbon && (
        <polygon points={ribbon} style={{ fill: "var(--hs-range)" }} />
      )}
      {baseLine && (
        <polyline
          points={baseLine}
          className="fill-none stroke-secondary"
          strokeWidth={1.5}
          strokeLinejoin="round"
          strokeLinecap="round"
        />
      )}
      {points.length > 1 && (
        <polyline
          points={line}
          style={{
            fill: "none",
            stroke: INK[tone],
            strokeWidth: 2,
            strokeLinejoin: "round",
            strokeLinecap: "round",
          }}
        />
      )}
      {points.map((p, i) => (
        <Marker
          key={`${p.t}-${i}`}
          point={p}
          tone={tone}
          cx={x(p.t)}
          cy={y(p.value)}
          isDense={geo.isDense}
          isActive={false}
        />
      ))}
    </svg>
  )
})

/** A point's marker: 4 px with a 2 px ring of the chart's surface, or a
 * 2 px dot with a 1 px ring on a dense series; the active point grows
 * 1.5 px. A toned point also draws an outer ring of its own shape 3 px
 * outside, so the status reads without hiding the source's shape. */
function Marker(props: {
  point: Pt
  tone: Tone
  cx: number
  cy: number
  isDense: boolean
  isActive: boolean
}) {
  const { point, cx, cy, isDense } = props
  const shape = point.marker ?? "circle"
  const grow = (isDense ? -2 : 0) + (props.isActive ? 1.5 : 0)
  return (
    <>
      {point.tone && (
        <MarkerShape
          shape={shape}
          cx={cx}
          cy={cy}
          grow={grow + 3}
          style={{ fill: "none", stroke: INK[point.tone], strokeWidth: 1.5 }}
        />
      )}
      <MarkerShape
        shape={shape}
        cx={cx}
        cy={cy}
        grow={grow}
        style={{
          fill: INK[point.tone ?? props.tone],
          stroke: "var(--hs-chart-bg)",
          strokeWidth: isDense ? 1 : 2,
        }}
      />
    </>
  )
}

/** The crosshair and the emphasised marker, in a small SVG over the static
 * layer, so moving them re-renders nothing else. */
function CrosshairLayer(props: {
  geo: Geometry
  x: number
  point?: Pt
  tone: Tone
}) {
  const { geo, point } = props
  return (
    <svg
      className="pointer-events-none absolute inset-0 overflow-visible"
      width={geo.width}
      height={geo.height}
      viewBox={`0 0 ${geo.width} ${geo.height}`}
      aria-hidden
    >
      <line
        x1={props.x}
        x2={props.x}
        y1={PAD.top}
        y2={PAD.top + geo.plotH}
        className="stroke-secondary/60"
        strokeWidth={1}
      />
      {point && (
        <Marker
          point={point}
          tone={props.tone}
          cx={geo.x(point.t)}
          cy={geo.y(point.value)}
          isDense={geo.isDense}
          isActive
        />
      )}
    </svg>
  )
}

/** The crosshair another chart with the same `syncKey` asks for. Only this
 * small component subscribes, and a chart off screen ignores the asks. */
const SyncCrosshair = memo(function SyncCrosshair(props: {
  syncKey: string
  self: object
  geo: Geometry
  points: Pt[]
  tone: Tone
  visible: RefObject<boolean>
  isSuppressed: boolean
}) {
  const { syncKey, self, geo, visible } = props
  const [hit, setHit] = useState<{ x: number; i: number | null } | null>(null)
  useEffect(() => {
    let last: { x: number; i: number | null } | null = null
    const put = (next: { x: number; i: number | null } | null) => {
      if (
        last === next ||
        (last && next && last.x === next.x && last.i === next.i)
      )
        return
      last = next
      setHit(next)
    }
    const fn: SyncListener = (t, from) => {
      if (from === self) return
      const { tStart, tEnd } = geo
      if (
        t === null ||
        !visible.current ||
        tStart === undefined ||
        tEnd === undefined ||
        t < tStart ||
        t > tEnd
      ) {
        put(null)
        return
      }
      const x = Math.round(geo.x(t) * 2) / 2
      const near = nearestIndex(geo.xs, x)
      put({
        x,
        i: near !== null && Math.abs(geo.xs[near] - x) <= 6 ? near : null,
      })
    }
    let set = syncCharts.get(syncKey)
    if (!set) {
      set = new Set()
      syncCharts.set(syncKey, set)
    }
    set.add(fn)
    return () => {
      set.delete(fn)
      if (!set.size) syncCharts.delete(syncKey)
      put(null)
    }
  }, [syncKey, self, geo, visible])
  if (!hit || props.isSuppressed) return null
  return (
    <CrosshairLayer
      geo={geo}
      x={hit.x}
      point={hit.i !== null ? props.points[hit.i] : undefined}
      tone={props.tone}
    />
  )
})

/**
 * One dated series on one axis, with value bands and a muted baseline
 * behind it: a lab value over three years against its target zones and the
 * lab's range, resting heart rate over six months against its 30-day mean.
 * A crosshair follows the pointer (or the arrow keys once the chart has
 * focus) and snaps to the nearest point; the caller hears of it through
 * `onActiveChange`, and every chart sharing a `syncKey` shows the same
 * date.
 *
 * Points carry a marker shape per source, which `legend` names; toned
 * points get a ring; `emptyBefore` hatches the stretch of a shared axis
 * before the first point; `trendline` draws a fitted dashed line. Enter or
 * Space pins the point until Escape or blur; on touch a tap pins it and a
 * second tap on the same point, or a tap outside, clears it, and a touch
 * that drags (a scroll) pins nothing. Every point draws a marker: 4 px, or
 * a 2 px dot when points sit under 10 px apart.
 *
 * The chart is a named group with a text alternative (how many values,
 * from when to when, the latest, the bands, the trend, the empty span),
 * and the point the arrow keys reach is read out.
 */
export function TrendChart({
  data,
  label,
  bands,
  baseline,
  unit,
  formatValue = defaultValue,
  formatTick = defaultTick,
  formatTime = defaultTime,
  timeRange,
  tone = "accent",
  height = 200,
  empty = "No data",
  emptyBefore,
  trendline,
  legend,
  renderReadout,
  readout = "tooltip",
  onActiveChange,
  gutter,
  syncKey,
  valueRange,
  className,
  onKeyDown,
  onPointerLeave,
  onBlur,
  ...rest
}: TrendChartProps) {
  const rootRef = useRef<HTMLDivElement>(null)
  const [plotRef, width] = useWidth<HTMLDivElement>()
  const bubbleRef = useRef<HTMLDivElement>(null)
  const [active, setActive] = useState<number | null>(null)
  const [pinned, setPinned] = useState(false)
  const [spoken, setSpoken] = useState("")
  const [self] = useState(() => ({}))
  const shared = useRef<number | null>(null)
  const visible = useRef(true)
  const touch = useRef<{ id: number; x: number; y: number } | null>(null)
  const uid = useId().replace(/[^a-zA-Z0-9_-]/g, "")

  const rangeStart =
    timeRange?.start !== undefined ? toMs(timeRange.start) : undefined
  const rangeEnd =
    timeRange?.end !== undefined ? toMs(timeRange.end) : undefined

  const points = useMemo<Pt[]>(() => {
    const inRange = (t: number) =>
      (rangeStart === undefined || t >= rangeStart) &&
      (rangeEnd === undefined || t <= rangeEnd)
    return data
      .map((p) => ({
        src: p,
        value: p.value,
        tone: p.tone,
        marker: p.marker,
        t: toMs(p.at),
      }))
      .filter(
        (p) => Number.isFinite(p.t) && Number.isFinite(p.value) && inRange(p.t)
      )
      .sort((a, b) => a.t - b.t)
  }, [data, rangeStart, rangeEnd])
  const base = useMemo(() => {
    const inRange = (t: number) =>
      (rangeStart === undefined || t >= rangeStart) &&
      (rangeEnd === undefined || t <= rangeEnd)
    return (baseline ?? [])
      .map((p) => ({ value: p.value, low: p.low, high: p.high, t: toMs(p.at) }))
      .filter((p) => Number.isFinite(p.t) && inRange(p.t))
      .sort((a, b) => a.t - b.t)
  }, [baseline, rangeStart, rangeEnd])

  const trend = useMemo(
    () =>
      trendline
        ? {
            t1: toMs(trendline.from.at),
            v1: trendline.from.value,
            t2: toMs(trendline.to.at),
            v2: trendline.to.value,
          }
        : undefined,
    [trendline]
  )

  // Domains and scales. Time spans the data (or the requested range);
  // values span the data, the baseline, every finite band edge, the
  // trendline and the forced `valueRange`, widened to round ticks.
  const rangeMin = valueRange?.min
  const rangeMax = valueRange?.max
  const geo = useMemo<Geometry>(() => {
    const tStart = rangeStart ?? points[0]?.t
    const tEnd = rangeEnd ?? points[points.length - 1]?.t
    const { lo, hi, ticks } = valueDomain(
      [
        ...points.map((p) => p.value),
        ...base.flatMap((p) => [p.value, p.low, p.high]),
        ...(bands ?? []).flatMap((b) => [b.from, b.to]),
        ...(trend && points.length ? [trend.v1, trend.v2] : []),
      ].filter((v): v is number => typeof v === "number" && Number.isFinite(v)),
      { min: rangeMin, max: rangeMax }
    )
    const step = tickStep(ticks)
    const tickLabels = ticks.map((v) => formatTick(v, step))
    const start = gutter ?? gutterWidth(points.length ? tickLabels : [])
    const plotW = Math.max(0, width - start - PAD.end)
    const plotH = Math.max(0, height - PAD.top - PAD.bottom)
    const x = timeScale(tStart ?? 0, tEnd ?? 0, start, plotW)
    const y = (v: number) => PAD.top + (1 - (v - lo) / (hi - lo)) * plotH
    const xs = points.map((p) => x(p.t))
    return {
      width,
      height,
      start,
      plotW,
      plotH,
      tStart,
      tEnd,
      vLo: lo,
      vHi: hi,
      ticks,
      tickLabels,
      x,
      y,
      xs,
      isDense: markerRadius(medianSpacing(xs)) === 2,
    }
  }, [
    points,
    base,
    bands,
    trend,
    rangeMin,
    rangeMax,
    rangeStart,
    rangeEnd,
    width,
    height,
    formatTick,
    gutter,
  ])

  // The empty span before the first point.
  const gap =
    emptyBefore && geo.tStart !== undefined && points.length
      ? emptySpan(geo.tStart, points[0].t)
      : undefined
  const gapFrom = gap?.from
  const gapTo = gap?.to
  const gapMemo = useMemo(
    () =>
      gapFrom !== undefined && gapTo !== undefined
        ? { from: gapFrom, to: gapTo }
        : undefined,
    [gapFrom, gapTo]
  )
  const gapLabel =
    gapTo !== undefined
      ? (typeof emptyBefore === "object"
          ? emptyBefore.label
          : defaultEmptyLabel)(new Date(gapTo))
      : undefined

  // Off-screen charts skip synchronised crosshair work.
  useEffect(() => {
    const el = rootRef.current
    if (!el || !syncKey || typeof IntersectionObserver === "undefined") return
    const io = new IntersectionObserver(([entry]) => {
      visible.current = entry.isIntersecting
    })
    io.observe(el)
    return () => io.disconnect()
  }, [syncKey])

  const activeIdx = active !== null && active < points.length ? active : null

  // The live active index, pin and points. Handlers read these refs, never
  // the values of the render that made them: several events can arrive
  // before React commits the first one's update (End, Left and Escape in
  // one burst).
  const activeRef = useRef<number | null>(null)
  const pinnedRef = useRef(false)
  const pointsRef = useRef(points)
  const callbackRef = useRef(onActiveChange)
  useLayoutEffect(() => {
    pointsRef.current = points
    callbackRef.current = onActiveChange
  })
  const liveIdx = () => {
    const i = activeRef.current
    return i !== null && i < pointsRef.current.length ? i : null
  }

  // Tell the caller when this chart's own point or pin changes,
  // synchronously from the handler that changed it, never from an effect,
  // which would chain renders and lag a commit behind the keys.
  const reported = useRef<{ src: TrendPoint | null; pinned: boolean }>({
    src: null,
    pinned: false,
  })
  const report = () => {
    const i = liveIdx()
    const src = i !== null ? (pointsRef.current[i]?.src ?? null) : null
    const isPinned = pinnedRef.current
    const prev = reported.current
    if (prev.src === src && prev.pinned === isPinned) return
    reported.current = { src, pinned: isPinned }
    callbackRef.current?.(src, isPinned)
  }
  const pin = (next: boolean) => {
    pinnedRef.current = next
    setPinned(next)
  }
  const share = (t: number | null) => {
    if (!syncKey || shared.current === t) return
    shared.current = t
    for (const fn of syncCharts.get(syncKey) ?? []) fn(t, self)
  }
  const select = (i: number | null) => {
    activeRef.current = i
    setActive(i)
    share(i === null ? null : (pointsRef.current[i]?.t ?? null))
    report()
  }
  const clear = () => {
    pin(false)
    select(null)
  }

  // A pinned point clears on a tap or click outside the chart.
  const clearRef = useRef(clear)
  useLayoutEffect(() => {
    clearRef.current = clear
  })
  useEffect(() => {
    if (!pinned) return
    const onDown = (e: globalThis.PointerEvent) => {
      if (!rootRef.current?.contains(e.target as Node)) clearRef.current()
    }
    document.addEventListener("pointerdown", onDown)
    return () => document.removeEventListener("pointerdown", onDown)
  }, [pinned])

  const pointAt = (px: number) => nearestIndex(geo.xs, px)
  const localX = (e: PointerEvent<HTMLDivElement>) =>
    e.clientX - e.currentTarget.getBoundingClientRect().left

  const onPlotMove = (e: PointerEvent<HTMLDivElement>) => {
    // Touch never hovers: it pins on a tap, below.
    if (e.pointerType === "touch" || pinnedRef.current) return
    const i = pointAt(localX(e))
    if (i !== liveIdx()) select(i)
  }
  const onPlotDown = (e: PointerEvent<HTMLDivElement>) => {
    if (e.pointerType === "touch") {
      // Wait for the finger to lift: a drag is a page scroll, not a tap.
      touch.current = { id: e.pointerId, x: e.clientX, y: e.clientY }
      return
    }
    pin(false)
    select(pointAt(localX(e)))
  }
  const onPlotUp = (e: PointerEvent<HTMLDivElement>) => {
    const start = touch.current
    touch.current = null
    if (e.pointerType !== "touch" || !start || start.id !== e.pointerId) return
    if (!isTap(e.clientX - start.x, e.clientY - start.y)) return
    const i = pointAt(localX(e))
    // A tap pins; a second tap on the same point clears.
    if (pinnedRef.current && i === liveIdx()) {
      clear()
      return
    }
    pin(true)
    select(i)
  }
  const onPlotCancel = (e: PointerEvent<HTMLDivElement>) => {
    // The browser took the gesture (a scroll): nothing stays pinned.
    if (e.pointerType !== "touch") return
    touch.current = null
    if (pinnedRef.current || liveIdx() !== null) clear()
  }

  const fmt = (v: number) =>
    unit ? `${formatValue(v)} ${unit}` : formatValue(v)
  const sayPoint = (p: Pt) =>
    [fmt(p.value), formatTime(new Date(p.t)), ...(p.src.detail ?? [])].join(
      ", "
    )

  const onKey = (e: KeyboardEvent<HTMLDivElement>) => {
    onKeyDown?.(e)
    const pts = pointsRef.current
    if (e.defaultPrevented || !pts.length) return
    const last = pts.length - 1
    const at = liveIdx()
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault()
      if (at === null) {
        pin(true)
        select(last)
        setSpoken(`${sayPoint(pts[last])}, pinned`)
      } else {
        pin(!pinnedRef.current)
        report()
        setSpoken(pinnedRef.current ? "Pinned" : "Unpinned")
      }
      return
    }
    if (e.key === "Escape") {
      if (at === null && !pinnedRef.current) return
      e.preventDefault()
      clear()
      setSpoken("")
      return
    }
    const next =
      e.key === "ArrowRight"
        ? Math.min(last, (at ?? -1) + 1)
        : e.key === "ArrowLeft"
          ? Math.max(0, (at ?? last + 1) - 1)
          : e.key === "Home"
            ? 0
            : e.key === "End"
              ? last
              : undefined
    if (next === undefined) return
    e.preventDefault()
    select(next)
    setSpoken(sayPoint(pts[next]))
  }

  const cur = activeIdx !== null ? points[activeIdx] : undefined
  const curX = cur ? geo.x(cur.t) : 0
  const curY = cur ? Math.max(PAD.top, geo.y(cur.value)) : 0

  // The readout bubble, placed by its measured size so it stays inside
  // the frame at the edges: moved by hand after layout, no second render.
  const guessAbove = readoutSide(curY, BUBBLE_GUESS.h, height) === "top"
  useLayoutEffect(() => {
    const holder = bubbleRef.current
    const el = holder?.querySelector<HTMLElement>("[role=tooltip]")
    if (!holder || !el || !cur) return
    // Rounded up to 16 px steps, so readouts of nearly equal size do not
    // jump from one point to the next.
    const w = Math.ceil(el.offsetWidth / 16) * 16 || BUBBLE_GUESS.w
    const h = Math.ceil(el.offsetHeight / 16) * 16 || BUBBLE_GUESS.h
    const above = readoutSide(curY, h, height) === "top"
    const anchor = holder.firstElementChild as HTMLElement | null
    if (!anchor) return
    anchor.style.left = `${readoutAnchor(curX, w, width)}px`
    anchor.style.top = `${above ? curY : curY + MARKER_R + 2}px`
    el.classList.toggle("bottom-1.5", above)
    el.classList.toggle("top-1.5", !above)
  })

  const description = useMemo(() => {
    const f = (v: number) =>
      unit ? `${formatValue(v)} ${unit}` : formatValue(v)
    const n = points.length
    const newest = points[n - 1]
    const parts = n
      ? [
          `${n} ${n === 1 ? "value" : "values"}${
            n > 1
              ? ` from ${formatTime(new Date(points[0].t))} to ${formatTime(new Date(newest.t))}`
              : ""
          }`,
          `latest ${f(newest.value)} on ${formatTime(new Date(newest.t))}`,
        ]
      : [typeof empty === "string" ? empty : "No data"]
    return (
      [
        ...parts,
        ...(bands ?? []).map((b) =>
          b.from !== undefined && b.to !== undefined
            ? `${b.label} ${f(b.from)} to ${f(b.to)}`
            : b.from !== undefined
              ? `${b.label} from ${f(b.from)}`
              : b.to !== undefined
                ? `${b.label} up to ${f(b.to)}`
                : b.label
        ),
        ...(trendline?.label ? [trendline.label] : []),
        ...(gapLabel ? [gapLabel] : []),
      ]
        // A part written as a sentence keeps no full stop of its own: the
        // parts are joined, and the whole is closed once.
        .map((part) => part.replace(/\.$/, ""))
        .join("; ")
    )
  }, [points, bands, trendline, gapLabel, unit, formatValue, formatTime, empty])

  return (
    <div
      {...rest}
      ref={rootRef}
      className={cx("w-full rounded-[6px]", className)}
      role="group"
      data-points={points.length}
      data-pinned={pinned || undefined}
      aria-label={label}
      aria-describedby={`${uid}-desc`}
      tabIndex={points.length ? 0 : undefined}
      onKeyDown={onKey}
      onPointerLeave={(e) => {
        onPointerLeave?.(e)
        // A touch lift fires pointerleave; a pinned point outlives it.
        if (
          e.defaultPrevented ||
          e.pointerType === "touch" ||
          pinnedRef.current
        )
          return
        if (liveIdx() !== null) select(null)
      }}
      onBlur={(e: FocusEvent<HTMLDivElement>) => {
        onBlur?.(e)
        if (e.currentTarget.contains(e.relatedTarget)) return
        if (pinnedRef.current || liveIdx() !== null) clear()
      }}
    >
      <p id={`${uid}-desc`} className="sr-only">
        {description}.
        {points.length > 0 &&
          " The left and right arrow keys step through the values."}
      </p>
      <span className="sr-only" aria-live="polite">
        {spoken}
      </span>
      <div
        ref={plotRef}
        className="relative w-full touch-pan-y"
        style={{ height }}
        onPointerMove={onPlotMove}
        onPointerDown={onPlotDown}
        onPointerUp={onPlotUp}
        onPointerCancel={onPlotCancel}
      >
        {width > 0 && (
          <TrendStatic
            geo={geo}
            points={points}
            base={base}
            bands={bands}
            tone={tone}
            gap={gapMemo}
            gapLabel={gapLabel}
            trend={trend}
            formatTime={formatTime}
            uid={uid}
          />
        )}
        {width > 0 && cur && (
          <CrosshairLayer geo={geo} x={curX} point={cur} tone={tone} />
        )}
        {width > 0 && syncKey && (
          <SyncCrosshair
            syncKey={syncKey}
            self={self}
            geo={geo}
            points={points}
            tone={tone}
            visible={visible}
            isSuppressed={cur !== undefined}
          />
        )}
        {!points.length && (
          <div className="absolute inset-0 grid place-items-center px-4 text-center text-[13px] text-secondary">
            {empty}
          </div>
        )}
        {cur && readout === "tooltip" && (
          <div ref={bubbleRef} aria-hidden>
            <Bubble
              left={readoutAnchor(curX, BUBBLE_GUESS.w, width)}
              top={guessAbove ? curY : curY + MARKER_R + 2}
              side={guessAbove ? "top" : "bottom"}
              role="tooltip"
            >
              {renderReadout ? (
                renderReadout(cur.src)
              ) : (
                <>
                  <strong className="font-semibold tabular-nums">
                    {fmt(cur.value)}
                  </strong>
                  <span className="text-secondary">
                    {formatTime(new Date(cur.t))}
                  </span>
                  {cur.src.detail?.map((d) => (
                    <span key={d} className="text-secondary">
                      {d}
                    </span>
                  ))}
                </>
              )}
            </Bubble>
          </div>
        )}
      </div>
      {legend && legend.length > 0 && (
        <ChartKey
          className="mt-2"
          label={`${label} key`}
          size="sm"
          items={legend}
          current={cur?.src.series}
        />
      )}
    </div>
  )
}
