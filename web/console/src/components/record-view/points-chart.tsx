/** Sparse readings over months or years, such as a lab's results, drawn by
 * hand in SVG: a dot per reading in its tone (hollow where the caller marks
 * it), a hairline between consecutive readings of one series, and each
 * reading's own reference range as a stepped wash from its instant to the
 * next reading's, in the range's tone. A reading written as a bound
 * (`<0.5`) is a triangle pointing toward its open side and joins no
 * hairline: its true value is somewhere past the mark. The bucket model of `TimelineChart` does not fit a few
 * readings years apart, so this chart places each one at its own instant.
 *
 * Hovering finds the nearest reading by x and one tooltip names it: its
 * title, value, range, date, and the caller's note. Focusing a reading's
 * link by keyboard rings it and shows the same tooltip. A reading that
 * names its record opens it on a click. A tone's colour
 * always sits beside a word in the tooltip and the table under the chart. */

import { Link } from "@tanstack/react-router"
import { useMemo, useState, type PointerEvent } from "react"

import { useWidth } from "@/hooks/use-width"
import { splitKind } from "@/lib/definition"
import { linearScale, niceDomain, tickLabels } from "@/lib/chart"
import { readingText } from "@/lib/observations"
import { floatDigits } from "@/lib/units"
import { autoBucket, dayWords, type TimeRange } from "@/lib/observations-time"
import { toneColor, toneSoft, type Band, type Tone } from "@/lib/record-view"
import { AXIS_LEFT, timeAxis } from "@/lib/time-axis"

export interface ChartPoint {
  /** The reading's instant, in milliseconds. */
  at: number
  value: number
  /** A bound such as `<`: the true value lies past `value`. */
  comparator?: string
  /** Its own reference range; an absent bound is open. */
  low?: number
  high?: number
  /** How the range is drawn; a lab's normal range is good. */
  rangeTone?: Tone
  tone: Tone
  /** What the reading is, for the tooltip. */
  title: string
  /** A reading drawn as a ring, which the caller's notes explain. */
  hollow?: boolean
  /** Lines the tooltip adds, in the caller's words. */
  notes?: string[]
  /** The line the reading belongs to, such as a body region: hairlines
   * join readings of one series. */
  series?: string
  /** The record it is, opened on a click. */
  kind?: string
  id?: string
}

const TOP = 10
/** Room above the plot for the axis's unit caption. */
const CAPTION = 14
const AXIS = 26
const LEFT = AXIS_LEFT

function finite(v: number | undefined): v is number {
  return v !== undefined && Number.isFinite(v)
}

/** Whether a reading is a bound below its digits (`<0.5`), above them
 * (`>90`), or neither. */
function boundSide(p: ChartPoint): "below" | "above" | undefined {
  if (p.comparator === "<" || p.comparator === "<=") return "below"
  if (p.comparator === ">" || p.comparator === ">=") return "above"
  return undefined
}

/** A triangle around (`cx`, `cy`) pointing down for a bound below its
 * digits and up for one above. */
function trianglePath(cx: number, cy: number, side: "below" | "above") {
  const r = 5
  const tip = side === "below" ? cy + r : cy - r
  const base = side === "below" ? cy - r * 0.6 : cy + r * 0.6
  return `M${cx - r} ${base} L${cx + r} ${base} L${cx} ${tip} Z`
}

/** The stroke of a range's edge in its tone: a toneless one the strong
 * border. */
function edgeColor(tone: Tone): string {
  return tone === "none" ? "var(--border-strong)" : toneColor(tone)
}

/** A reading in words, its comparator in front. */
function valueWords(p: ChartPoint, unit?: string): string {
  return readingText(floatDigits(p.value), unit, undefined, p.comparator)
}

/** A reading's range in words: "range 2.6 to 3.4 mmol/L", "under 3
 * mmol/L", "over 3 mmol/L", or "no range". */
function pointRangeWords(p: ChartPoint, unit?: string): string {
  const low = finite(p.low) ? p.low : undefined
  const high = finite(p.high) ? p.high : undefined
  if (low !== undefined && high !== undefined) {
    return `range ${readingText(floatDigits(low))} to ${readingText(floatDigits(high), unit)}`
  }
  if (high !== undefined) return `under ${readingText(floatDigits(high), unit)}`
  if (low !== undefined) return `over ${readingText(floatDigits(low), unit)}`
  return "no range"
}

export function PointsChart({
  points,
  domain,
  unit,
  highlight,
  bands,
  label,
  height = 200,
}: {
  points: ChartPoint[]
  /** The time the x axis spans. */
  domain: TimeRange
  /** The values' unit, captioned above the axis. */
  unit?: string
  /** The index into `points` of the reading ringed: the page's own. */
  highlight?: number
  /** Value ranges washed behind the plot. */
  bands?: Band[]
  /** The chart's accessible name. */
  label: string
  /** The plot's height in pixels. */
  height?: number
}) {
  const { ref, width } = useWidth(720)
  const [active, setActive] = useState<number | null>(null)

  const layout = useMemo(() => {
    // Drawn in time order; each keeps the caller's index for `highlight`.
    const order = points
      .map((p, i) => ({ p, i }))
      .sort((a, b) => a.p.at - b.p.at)
    const top = unit ? TOP + CAPTION : TOP
    const axis = timeAxis(domain, autoBucket(domain), width)
    const { x, right } = axis
    const values: number[] = []
    for (const { p } of order) {
      values.push(p.value)
      if (finite(p.low)) values.push(p.low)
      if (finite(p.high)) values.push(p.high)
    }
    for (const b of bands ?? []) {
      if (finite(b.low)) values.push(b.low)
      if (finite(b.high)) values.push(b.high)
    }
    const { domain: yDomain, ticks } = niceDomain(values)
    // Hairlines join consecutive readings of one series that are not
    // bounds, in time order.
    const joins: [number, number][] = []
    const lastOf = new Map<string, number>()
    order.forEach(({ p }, n) => {
      if (boundSide(p)) return
      const key = p.series ?? ""
      const prev = lastOf.get(key)
      if (prev !== undefined) joins.push([prev, n])
      lastOf.set(key, n)
    })
    const y = linearScale(yDomain, [top + height, top])
    const clampX = (v: number) => Math.min(right, Math.max(LEFT, v))
    const clampY = (v: number) => Math.min(top + height, Math.max(top, v))
    const yOf = (v: number | undefined, open: number) =>
      finite(v) ? clampY(y(v)) : open
    const placed = order.map(({ p, i }, n) => {
      const next = order[n + 1]?.p.at ?? domain.end
      const hasRange = finite(p.low) || finite(p.high)
      const y0 = yOf(p.high, top)
      const y1 = yOf(p.low, top + height)
      return {
        p,
        i,
        cx: x(p.at),
        cy: y(p.value),
        range: hasRange
          ? {
              x0: clampX(x(p.at)),
              x1: clampX(x(next)),
              y0: Math.min(y0, y1),
              y1: Math.max(y0, y1),
              // The bounds the result's own range names; an open side
              // (an "under 3.0" range has no low) draws no edge.
              edges: [p.high, p.low].filter(finite).map((v) => clampY(y(v))),
              tone: p.rangeTone ?? "good",
            }
          : undefined,
      }
    })
    const washes = (bands ?? []).map((b) => {
      const y0 = yOf(b.high, top)
      const y1 = yOf(b.low, top + height)
      return {
        y0: Math.min(y0, y1),
        y1: Math.max(y0, y1),
        color: toneSoft(b.tone),
        label: b.label,
      }
    })
    return {
      top,
      right,
      svgHeight: top + height + AXIS,
      y,
      ticks,
      labels: tickLabels(ticks),
      xLabels: axis.labels,
      placed,
      washes,
      joins,
    }
  }, [points, domain, unit, bands, height, width])

  const {
    top,
    right,
    svgHeight,
    y,
    ticks,
    labels,
    xLabels,
    placed,
    washes,
    joins,
  } = layout

  if (points.length === 0) {
    return (
      <p data-slot="points-chart" className="text-muted-foreground">
        No results in this range.
      </p>
    )
  }

  // The reading nearest the pointer across, and among readings at one
  // instant (the regions of one scan) the one nearest it up and down.
  function nearest(event: PointerEvent<SVGSVGElement>): number | null {
    const box = event.currentTarget.getBoundingClientRect()
    const px = event.clientX - box.left
    const py = event.clientY - box.top
    let best: number | null = null
    let gap = Infinity
    let rise = Infinity
    placed.forEach((d, n) => {
      const g = Math.abs(d.cx - px)
      const r = Math.abs(d.cy - py)
      if (g < gap - 0.5 || (Math.abs(g - gap) <= 0.5 && r < rise)) {
        gap = g
        rise = r
        best = n
      }
    })
    return best
  }

  const shown = active !== null ? placed[active] : undefined
  const tipWidth = 240
  const tipLeft = shown
    ? shown.cx > width - tipWidth - 16
      ? Math.max(0, shown.cx - tipWidth - 8)
      : shown.cx + 14
    : 0

  return (
    <div
      ref={ref}
      role="group"
      aria-label={label}
      aria-roledescription="chart"
      data-slot="points-chart"
      className="relative w-full"
    >
      <svg
        width={width}
        height={svgHeight}
        className="block touch-none select-none"
        onPointerMove={(e) => setActive(nearest(e))}
        onPointerLeave={() => setActive(null)}
      >
        {ticks.map((t) => (
          <line
            key={t}
            x1={LEFT}
            x2={right}
            y1={y(t)}
            y2={y(t)}
            stroke="var(--border)"
            shapeRendering="crispEdges"
          />
        ))}
        {washes.map((b, n) => (
          <g key={n}>
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
        <g data-axis="left">
          {unit && (
            <text
              x={LEFT - 8}
              y={top - 10}
              textAnchor="end"
              className="fill-muted-foreground text-[11.5px]"
            >
              {unit}
            </text>
          )}
          {ticks.map((t, n) => (
            <text
              key={t}
              x={LEFT - 8}
              y={y(t)}
              textAnchor="end"
              dominantBaseline="middle"
              className="fill-faint text-[11.5px] tabular-nums"
            >
              {labels[n]}
            </text>
          ))}
        </g>
        {/* A result's reference range is where its value counts as normal,
            so it is drawn in the good tone unless the range names another
            (a cut-off past which a value is abnormal), with a dashed edge
            at each bound the lab printed. */}
        {placed.map((d) =>
          d.range ? (
            <g key={`range-${d.i}`}>
              <rect
                data-slot="point-range"
                x={d.range.x0}
                y={d.range.y0}
                width={Math.max(0, d.range.x1 - d.range.x0)}
                height={d.range.y1 - d.range.y0}
                fill={toneSoft(d.range.tone)}
              />
              {d.range.edges.map((ey, n) => (
                <line
                  key={n}
                  data-slot="point-range-edge"
                  x1={d.range!.x0}
                  x2={d.range!.x1}
                  y1={ey}
                  y2={ey}
                  stroke={edgeColor(d.range!.tone)}
                  strokeOpacity={0.55}
                  strokeDasharray="4 3"
                  shapeRendering="crispEdges"
                />
              ))}
            </g>
          ) : null
        )}
        {joins.map(([a, b]) => {
          // The hovered reading's own line is drawn in the text colour, so
          // one region's chain stands out among the others.
          const lit =
            shown !== undefined &&
            (shown.p.series ?? "") === (placed[b].p.series ?? "")
          return (
            <line
              key={`hair-${placed[b].i}`}
              data-slot="point-hairline"
              data-lit={lit ? "" : undefined}
              x1={placed[a].cx}
              y1={placed[a].cy}
              x2={placed[b].cx}
              y2={placed[b].cy}
              stroke={lit ? "var(--foreground)" : "var(--border-strong)"}
            />
          )
        })}
        {shown && (
          <line
            data-slot="chart-crosshair"
            x1={shown.cx}
            x2={shown.cx}
            y1={top}
            y2={top + height}
            stroke="var(--border-strong)"
          />
        )}
        {placed.map((d, n) => {
          const lit = highlight === d.i
          const dot = (
            <g key={`dot-${d.i}`}>
              <circle
                data-slot="point-focus"
                cx={d.cx}
                cy={d.cy}
                r={7}
                fill="none"
                stroke="var(--ring)"
                strokeWidth={2}
                className="opacity-0 group-focus-visible:opacity-100"
              />
              {lit && (
                <circle
                  data-slot="point-highlight"
                  cx={d.cx}
                  cy={d.cy}
                  r={8}
                  fill="none"
                  stroke="var(--border-strong)"
                  strokeWidth={2}
                />
              )}
              {boundSide(d.p) ? (
                <path
                  data-slot="point"
                  data-bound={boundSide(d.p)}
                  data-hollow={d.p.hollow ? "" : undefined}
                  d={trianglePath(d.cx, d.cy, boundSide(d.p)!)}
                  fill="var(--background)"
                  stroke={toneColor(d.p.tone)}
                  strokeWidth={2}
                />
              ) : (
                <circle
                  data-slot="point"
                  data-hollow={d.p.hollow ? "" : undefined}
                  cx={d.cx}
                  cy={d.cy}
                  r={4}
                  fill={d.p.hollow ? "var(--background)" : toneColor(d.p.tone)}
                  stroke={d.p.hollow ? toneColor(d.p.tone) : undefined}
                  strokeWidth={d.p.hollow ? 2 : undefined}
                />
              )}
            </g>
          )
          const kind = d.p.kind ? splitKind(d.p.kind) : undefined
          if (!kind?.authority || !kind.pkg || !kind.name || !d.p.id) {
            return dot
          }
          return (
            <Link
              key={`dot-${d.i}`}
              to="/data/$authority/$pkg/$name/$id"
              params={{
                authority: kind.authority,
                pkg: kind.pkg,
                name: kind.name,
                id: d.p.id,
              }}
              aria-label={`${d.p.title}, ${valueWords(d.p, unit)}, ${dayWords(d.p.at, true)}`}
              className="group cursor-pointer outline-none"
              onFocus={() => setActive(n)}
              onBlur={() => setActive(null)}
            >
              {dot}
            </Link>
          )
        })}
        {xLabels.map((l) => (
          <text
            key={l.i}
            x={l.at}
            y={top + height + 17}
            textAnchor="middle"
            className="fill-faint text-[11.5px] tabular-nums"
          >
            {l.text}
          </text>
        ))}
      </svg>
      {shown && (
        <div
          role="status"
          data-slot="chart-tooltip"
          className="pointer-events-none absolute top-2 z-10 flex flex-col gap-0.5 rounded-md border border-border bg-popover p-2.5 text-[12.5px] text-popover-foreground shadow-card"
          style={{ left: tipLeft, width: tipWidth }}
        >
          <span className="font-medium">{shown.p.title}</span>
          <span className="tabular-nums">{valueWords(shown.p, unit)}</span>
          <span className="text-muted-foreground">
            {pointRangeWords(shown.p, unit)}
          </span>
          <span className="text-muted-foreground">
            {dayWords(shown.p.at, true)}
          </span>
          {shown.p.notes?.map((note) => (
            <span key={note} className="text-muted-foreground">
              {note}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
