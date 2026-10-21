/** One figure of a record view's `stats` block: a bordered tile with its
 * label, its value (or the words said instead), the band the value falls in
 * and its change against a baseline. A bounded score may draw as a `Dial`,
 * an arc from `min` to `max` with its bands under the value; a reading with
 * a baseline may draw as a `RangeBar`, a bullet bar with the baseline's
 * lowest to highest behind the value. A tone's colour always sits beside
 * its word (docs/console/design-guide.md): colour never carries a status
 * alone. */

import type { ReactNode } from "react"

import { readingText } from "@/lib/observations"
import {
  toneColor,
  toneSoft,
  toneText,
  type Band,
  type Tone,
} from "@/lib/record-view"
import { floatDigits, unitSymbol } from "@/lib/units"

export interface StatTileProps {
  label: string
  /** The value as text, without its unit. */
  value?: string
  /** What the tile says where there is no value. */
  words?: string
  /** A word after the value that says what it is of, such as "so far"
   * for a window still running. */
  qualifier?: string
  /** The value as a number, for a dial or a bar. */
  numeric?: number | null
  unit?: string
  /** The band the value falls in, as the caller judged it (`bandOf`): the
   * dial and the bar draw this one and never judge the value again. */
  band?: { label: string; tone: Tone }
  /** The change against the baseline, in words, in its tone. */
  delta?: { text: string; tone: Tone }
  /** The daily values before the window: average, lowest, highest, and
   * how many days. */
  baseline?: { avg: number; low: number; high: number; days: number }
  show: "number" | "dial" | "bar"
  min?: number
  max?: number
  bands?: Band[]
  note?: string
  /** Where the tile links: a page about the figure. */
  href?: string
  /** The value as the caller draws it (a declared property's own
   * rendering), in place of `value` in the number show. */
  figure?: ReactNode
  /** The bar's bands are a reference range: its bounds are drawn and
   * printed (`RangeBar`'s `marked`). */
  marked?: boolean
  /** The decimal places a bar prints its bounds with. */
  precision?: number
}

export function StatTile(props: StatTileProps) {
  const {
    label,
    value,
    words,
    numeric,
    unit,
    band,
    delta,
    baseline,
    show,
    bands = [],
    note,
    href,
    qualifier,
  } = props
  const said = value !== undefined || props.figure !== undefined
  const symbol = unit ? unitSymbol(unit) : ""
  let figure: ReactNode
  if (show === "dial") {
    const min = props.min ?? 0
    const max = props.max ?? boundsMax(bands, numeric) ?? 100
    figure = (
      <Dial
        value={numeric ?? null}
        min={min}
        max={max}
        bands={bands}
        band={band}
        text={value ?? ""}
      />
    )
  } else {
    figure = (
      <div className="flex flex-wrap items-baseline gap-x-2">
        {props.figure !== undefined ? (
          props.figure
        ) : value !== undefined ? (
          <span className="text-[22px] font-medium tabular-nums">
            {value}
            {symbol && (
              <span className="ml-0.5 text-[13px] font-normal text-muted-foreground">
                {symbol}
              </span>
            )}
          </span>
        ) : (
          <span className="text-muted-foreground">{words ?? "no reading"}</span>
        )}
        {said && qualifier && <Qualifier text={qualifier} />}
        {said && band && <BandWord band={band} />}
      </div>
    )
  }
  return (
    <div
      data-slot="stat-tile"
      className="flex h-full min-w-0 flex-col gap-1 rounded-md border border-border p-3"
    >
      <div className="text-[12.5px] text-muted-foreground">
        {href ? (
          <a href={href} className="hover:text-foreground hover:underline">
            {label}
          </a>
        ) : (
          label
        )}
      </div>
      {figure}
      {show === "dial" && value === undefined && words && (
        <span className="text-[12.5px] text-muted-foreground">{words}</span>
      )}
      {show === "dial" && said && qualifier && <Qualifier text={qualifier} />}
      {show === "bar" && (
        <RangeBar
          value={numeric ?? null}
          min={props.min}
          max={props.max}
          bands={bands}
          baseline={baseline}
          unit={unit}
          tone={band?.tone}
          marked={props.marked}
          precision={props.precision}
        />
      )}
      {delta && (
        <span
          data-slot="stat-delta"
          className="text-[12.5px]"
          style={{ color: toneText(delta.tone) }}
        >
          {delta.text}
        </span>
      )}
      {note && <span className="text-[12.5px] text-faint">{note}</span>}
    </div>
  )
}

function Qualifier({ text }: { text: string }) {
  return (
    <span
      data-slot="stat-qualifier"
      className="text-[12.5px] text-muted-foreground"
    >
      {text}
    </span>
  )
}

function BandWord({ band }: { band: { label: string; tone: Tone } }) {
  return (
    <span
      data-slot="stat-band"
      className="text-[12.5px] font-medium"
      style={{ color: toneText(band.tone) }}
    >
      {band.label}
    </span>
  )
}

/** The largest finite bound among the bands and the value: a dial's upper
 * end where the view names none. */
function boundsMax(
  bands: readonly Band[],
  value: number | null | undefined
): number | undefined {
  const all = bands
    .flatMap((b) => [b.low, b.high])
    .concat(value ?? undefined)
    .filter((v): v is number => v !== undefined && Number.isFinite(v))
  return all.length ? Math.max(...all) : undefined
}

/** A point on the circle of radius `r` around (`cx`, `cy`) at `deg`
 * degrees, 0 to the right and 180 to the left, counted upwards. */
function polar(cx: number, cy: number, r: number, deg: number) {
  const rad = (deg * Math.PI) / 180
  return { x: cx + r * Math.cos(rad), y: cy - r * Math.sin(rad) }
}

const round = (n: number) => Number(n.toFixed(2))

/** The SVG path of an arc on the upper half circle from angle `from` to
 * angle `to` (degrees, 180 the left end, 0 the right end), drawn
 * clockwise on screen as a dial fills. */
// eslint-disable-next-line react-refresh/only-export-components -- a pure path builder, exported for its test
export function arcPath(
  cx: number,
  cy: number,
  r: number,
  from: number,
  to: number
): string {
  const a = polar(cx, cy, r, from)
  const b = polar(cx, cy, r, to)
  const large = Math.abs(from - to) > 180 ? 1 : 0
  const sweep = from > to ? 1 : 0
  return `M${round(a.x)} ${round(a.y)} A${r} ${r} 0 ${large} ${sweep} ${round(b.x)} ${round(b.y)}`
}

const DIAL = { cx: 60, cy: 62, r: 50 }

/** A value's angle on the dial: `min` at 180, `max` at 0. */
function angleOf(v: number, min: number, max: number): number {
  const span = max - min || 1
  const t = Math.min(1, Math.max(0, (v - min) / span))
  return 180 - t * 180
}

export function Dial({
  value,
  min,
  max,
  bands,
  band,
  text,
}: {
  value: number | null
  min: number
  max: number
  bands: Band[]
  /** The band the value falls in, as the tile judged it: its word and tone
   * are drawn as given. */
  band?: { label: string; tone: Tone }
  text: string
}) {
  const { cx, cy, r } = DIAL
  const tone = band?.tone ?? "none"
  return (
    <div data-slot="stat-dial" className="flex flex-col items-start gap-0.5">
      <svg width={120} height={72} viewBox="0 0 120 72" aria-hidden>
        <path
          data-slot="dial-track"
          d={arcPath(cx, cy, r, 180, 0)}
          fill="none"
          stroke="var(--hover)"
          strokeWidth={10}
        />
        {bands.map((b, i) => {
          const lo = Math.max(min, b.low ?? min)
          const hi = Math.min(max, b.high ?? max)
          if (!(hi > lo)) return null
          return (
            <path
              key={i}
              data-band={b.label ?? ""}
              d={arcPath(
                cx,
                cy,
                r,
                angleOf(lo, min, max),
                angleOf(hi, min, max)
              )}
              fill="none"
              stroke={toneSoft(b.tone)}
              strokeWidth={10}
            />
          )
        })}
        {value !== null && value > min && (
          <path
            data-slot="dial-value"
            d={arcPath(cx, cy, r, 180, angleOf(value, min, max))}
            fill="none"
            stroke={toneColor(tone)}
            strokeWidth={10}
          />
        )}
        <text
          x={60}
          y={58}
          textAnchor="middle"
          className="fill-foreground text-[22px] font-medium tabular-nums"
        >
          {value === null ? "" : text}
        </text>
      </svg>
      {value === null ? (
        <span className="text-[12.5px] text-muted-foreground">no reading</span>
      ) : (
        <>
          {/* The number is drawn inside the decorative SVG; a reader of the
              page hears it here. */}
          <span className="sr-only">{text}</span>
          {band?.label && <BandWord band={band} />}
        </>
      )}
    </div>
  )
}

/** The first band holding `v`, by the rule `bandOf` judges a tile's band
 * with (`low <= v < high`, an absent bound open), so a value on a shared
 * bound takes the upper band everywhere. The bands are the caller's, already
 * narrowed to the value's metric. */
function bandHolding(v: number, bands: readonly Band[]): Band | undefined {
  return bands.find(
    (b) => (b.low ?? -Infinity) <= v && v < (b.high ?? Infinity)
  )
}

/** A value's place along a bar from `min` to `max`, as a percent. */
function percentOf(v: number, min: number, max: number): number {
  const span = max - min || 1
  return round(Math.min(100, Math.max(0, ((v - min) / span) * 100)))
}

/** Where a printed bound sits under the bar: centred on its place, and
 * kept inside the bar at either end. */
function boundPlace(percent: number) {
  if (percent <= 10) return { left: `${percent}%` }
  if (percent >= 90) return { right: `${100 - percent}%` }
  return { left: `${percent}%`, transform: "translateX(-50%)" }
}

/** The gap, in percent of the bar, two printed bounds keep: about a label's
 * width on a tile's bar, so their words never run into each other. */
const BOUND_GAP = 25

/** The bounds a reference range prints under its bar: each bound where it
 * falls, bounds closer than `BOUND_GAP` printed together as one "135 to 145
 * mmol/L" between them, and the scale's named start (`min`, the zero of a
 * one-sided range) only where it keeps clear of them. A bound prints in
 * full, the digits it was written with, never cut to two places. */
function boundLabels(
  edges: readonly { v: number }[],
  min: number | undefined,
  at: (v: number) => number,
  unit?: string,
  precision?: number
): { at: number; text: string }[] {
  const full = (v: number, u?: string) =>
    readingText(floatDigits(v), u, Math.max(precision ?? 0, placesOf(v)))
  const values = [...new Set(edges.map((e) => e.v))].sort((a, b) => a - b)
  const groups: number[][] = []
  for (const v of values) {
    const last = groups.at(-1)
    if (last && at(v) - at(last[0]) < BOUND_GAP) last.push(v)
    else groups.push([v])
  }
  const out = groups.map((g) => {
    const first = g[0]
    const end = g.at(-1)!
    return g.length === 1
      ? { at: at(first), text: full(first, unit) }
      : {
          at: (at(first) + at(end)) / 2,
          text: `${full(first)} to ${full(end, unit)}`,
        }
  })
  if (
    min !== undefined &&
    !values.includes(min) &&
    out.every((l) => Math.abs(l.at - at(min)) >= BOUND_GAP)
  ) {
    out.unshift({ at: at(min), text: full(min, unit) })
  }
  return out
}

/** A number's decimal places as written, float noise cleared. */
function placesOf(v: number): number {
  const m = /\.(\d+)$/.exec(floatDigits(v))
  return m ? m[1].length : 0
}

export function RangeBar({
  value,
  min,
  max,
  bands,
  baseline,
  unit,
  precision,
  tone,
  marked = false,
}: {
  value: number | null
  min?: number
  max?: number
  bands: Band[]
  baseline?: { avg: number; low: number; high: number }
  unit?: string
  precision?: number
  /** The value marker's tone, where the caller judged it; else the tone of
   * the band holding the value. Sits beside the caller's word for it. */
  tone?: Tone
  /** The bands are a reference range: each finite bound draws a dashed
   * edge and is printed under the bar where the ends are not. */
  marked?: boolean
}) {
  const known = [
    // A reference range's scale holds its named start; a stats bar's
    // padding is the values' own.
    ...(marked ? [min, max] : []),
    value ?? undefined,
    ...bands.flatMap((b) => [b.low, b.high]),
    baseline?.low,
    baseline?.high,
  ].filter((v): v is number => v !== undefined && Number.isFinite(v))
  const lowest = known.length ? Math.min(...known) : 0
  const highest = known.length ? Math.max(...known) : 1
  const pad = (highest - lowest) * 0.05 || Math.abs(highest) * 0.05 || 1
  // The padding never takes a scale of non-negative values below zero.
  const lo = min ?? (lowest >= 0 ? Math.max(0, lowest - pad) : lowest - pad)
  const hi = max ?? highest + pad
  // The ends are labelled only where the caller named the scale: a padded
  // end is no bound of anything, and read as one it misleads.
  const labelled = min !== undefined && max !== undefined
  const at = (v: number) => percentOf(v, lo, hi)
  const text = (v: number) => readingText(String(round(v)), unit, precision)
  // A reference range's bounds, each once, where they fall on the scale.
  const edges = marked
    ? bands.flatMap((b) =>
        [b.low, b.high]
          .filter((v): v is number => v !== undefined && Number.isFinite(v))
          .map((v) => ({ v, tone: b.tone }))
      )
    : []
  const printed =
    labelled || !marked ? [] : boundLabels(edges, min, at, unit, precision)
  const valueAt = value === null ? undefined : at(value)
  const mark = toneColor(
    tone ??
      (value === null ? "none" : (bandHolding(value, bands)?.tone ?? "none"))
  )
  // A value in no band, or in a toneless one, is marked in the text colour.
  const markColor = mark === toneColor("none") ? "var(--foreground)" : mark
  return (
    <div
      data-slot="range-bar"
      data-value-at={valueAt === undefined ? undefined : String(valueAt)}
      className="flex w-full flex-col gap-1"
    >
      <div className="relative h-4 w-full">
        <div className="absolute inset-x-0 top-1 h-2 rounded-full bg-hover" />
        {bands.map((b, i) => {
          const left = at(b.low ?? lo)
          const right = at(b.high ?? hi)
          if (!(right > left)) return null
          return (
            <div
              key={i}
              data-band={b.label ?? ""}
              className="absolute top-1 h-2"
              style={{
                left: `${left}%`,
                width: `${right - left}%`,
                background: toneSoft(b.tone),
              }}
            />
          )
        })}
        {edges.map((e, i) => (
          <div
            key={`edge-${i}`}
            data-slot="range-edge"
            className="absolute top-0.5 h-3 w-0 -translate-x-1/2 border-l border-dashed"
            style={{
              left: `${at(e.v)}%`,
              borderColor:
                e.tone === "none" ? "var(--border-strong)" : toneColor(e.tone),
              opacity: 0.55,
            }}
          />
        ))}
        {baseline && (
          <>
            <div
              data-slot="range-baseline"
              className="absolute top-1 h-2 bg-border-strong/40"
              style={{
                left: `${at(baseline.low)}%`,
                width: `${Math.max(0, at(baseline.high) - at(baseline.low))}%`,
              }}
            />
            <div
              data-slot="range-average"
              className="absolute top-0.5 h-3 w-px bg-muted-foreground"
              style={{ left: `${at(baseline.avg)}%` }}
            />
          </>
        )}
        {valueAt !== undefined && (
          <div
            data-slot="range-value"
            className="absolute top-0 h-4 w-0.5 -translate-x-1/2"
            style={{ left: `${valueAt}%`, background: markColor }}
          >
            <span
              className="absolute -top-0.5 left-1/2 size-1.5 -translate-x-1/2 rounded-full"
              style={{ background: markColor }}
            />
          </div>
        )}
      </div>
      {labelled && (
        <div className="flex justify-between text-[11.5px] text-faint tabular-nums">
          <span>{text(lo)}</span>
          <span>{text(hi)}</span>
        </div>
      )}
      {printed.length > 0 && (
        <div className="relative h-4 text-[11.5px] text-faint tabular-nums">
          {printed.map((l) => (
            <span
              key={l.text}
              data-slot="range-bound"
              className="absolute whitespace-nowrap"
              style={boundPlace(l.at)}
            >
              {l.text}
            </span>
          ))}
        </div>
      )}
    </div>
  )
}
