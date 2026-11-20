// The pure rules behind the TrendChart (ui/TrendChart.tsx): time and value
// domains, year hairlines, the empty span, marker geometry and where the
// readout sits. No React and no DOM, so every rule is tested on its own
// (chart.test.ts).

/** A moment on the time axis: an ISO 8601 string, epoch milliseconds or a
 * Date. */
export type TrendTime = string | number | Date

/** A point's marker shape. Shape names a series or a source (the key says
 * which); it never carries status, tone does. */
export type TrendMarker = "circle" | "square" | "diamond" | "triangle" | "cross"

export const DAY_MS = 86_400_000
/** Average month length used for the 18-month hairline threshold. */
export const MONTH_MS = 30.4375 * DAY_MS
/** Year hairlines appear when the axis spans at least this many months. */
export const YEAR_LINES_MIN_MONTHS = 18
/** The empty span's label is drawn inside it only when it is this wide. */
export const EMPTY_LABEL_MIN_PX = 96
/** The most ticks a value axis draws. */
export const MAX_TICKS = 8

export function toMs(t: TrendTime): number {
  return t instanceof Date
    ? t.getTime()
    : typeof t === "number"
      ? t
      : Date.parse(t)
}

/** Three to six round tick values whose first and last enclose [lo, hi],
 * so the highest point never sits on the frame. */
export function niceTicks(lo: number, hi: number): number[] {
  const span = hi - lo || Math.abs(hi) || 1
  const raw = span / 4
  const mag = 10 ** Math.floor(Math.log10(raw))
  const step =
    [1, 2, 2.5, 5, 10].map((m) => m * mag).find((s) => span / s <= 5) ??
    10 * mag
  const first = Math.floor(lo / step + 1e-9) * step
  const top = Math.ceil(hi / step - 1e-9) * step
  // Ticks by index, never by repeated addition: when the step is below the
  // values' float resolution (0.2 and 0.20000000000000004) `v += step` does
  // not move and a loop would never end. At most MAX_TICKS ticks.
  const count = Math.min(
    MAX_TICKS - 1,
    Math.max(0, Math.round((top - first) / step))
  )
  const out: number[] = []
  for (let i = 0; i <= count; i++) {
    const v = first + i * step
    out.push(Math.abs(v) < step * 1e-9 ? 0 : v)
  }
  return out
}

/** The value axis: every finite value plus the optional forced range,
 * widened to round ticks. A single value gets 10 percent either side. */
export function valueDomain(
  values: number[],
  range?: { min?: number; max?: number }
): { lo: number; hi: number; ticks: number[] } {
  const all = [...values, range?.min, range?.max].filter(
    (v): v is number => typeof v === "number" && Number.isFinite(v)
  )
  let lo = all.length ? Math.min(...all) : 0
  let hi = all.length ? Math.max(...all) : 1
  // Values equal up to float noise (a flat series and a fitted trend that
  // ends at 0.20000000000000004) count as one value.
  if (hi - lo <= Math.max(Math.abs(lo), Math.abs(hi)) * 1e-9) {
    const pad = Math.abs(lo) * 0.1 || 1
    lo -= pad
    hi += pad
  }
  const ticks = niceTicks(lo, hi)
  return {
    lo: Math.min(lo, ticks[0] ?? lo),
    hi: Math.max(hi, ticks[ticks.length - 1] ?? hi),
    ticks,
  }
}

/** Decimal places a tick step needs to print exactly: 0 for 5, 1 for 0.5,
 * 2 for 0.25. Capped at 6. */
export function stepDecimals(step: number): number {
  if (!Number.isFinite(step) || step <= 0) return 0
  for (let d = 0; d < 6; d++) {
    const scaled = step * 10 ** d
    if (Math.abs(scaled - Math.round(scaled)) < 1e-6 * Math.max(1, scaled))
      return d
  }
  return 6
}

/** The default tick label: the step's own decimals, en-GB grouping, no
 * unit ("10,000", "2.5", "0.25"). */
export function defaultTick(value: number, step: number): string {
  const d = stepDecimals(step)
  return value.toLocaleString("en-GB", {
    minimumFractionDigits: d,
    maximumFractionDigits: d,
  })
}

/** The step between round ticks; 0 for fewer than two. */
export function tickStep(ticks: number[]): number {
  return ticks.length > 1 ? ticks[1] - ticks[0] : 0
}

/** Advance of one character of the 11 px axis text, in px: an estimate,
 * so the gutter and the time labels are sized without measuring. */
export const AXIS_CHAR_PX = 7
/** The least room between two neighbouring time labels, in px. */
export const TIME_LABEL_GAP_PX = 12
/** The value gutter never narrows below this, so charts whose labels fit
 * (up to five characters) share one plot start and their dates align. */
export const GUTTER_MIN_PX = 45

/** Width of the value-axis gutter: the widest tick label's estimated width
 * plus the 6 px gap to the plot and a 4 px margin, at least 45 px. */
export function gutterWidth(labels: string[]): number {
  const chars = labels.reduce((m, l) => Math.max(m, l.length), 0)
  return Math.max(GUTTER_MIN_PX, Math.ceil(chars * AXIS_CHAR_PX) + 10)
}

/** How many time labels fit under a plot `plotW` px wide whose longest
 * label is `labelChars` characters: 3 (start, middle, end) when three
 * labels fit with at least 12 px between neighbours, else 2 (start and
 * end). The start label hugs the left, the end the right, the middle is
 * centred, so the tight gap is plotW / 2 - 1.5 x label width. */
export function timeTickCount(plotW: number, labelChars: number): 2 | 3 {
  const w = labelChars * AXIS_CHAR_PX
  return plotW >= 3 * w + 2 * TIME_LABEL_GAP_PX ? 3 : 2
}

/** The median x distance in px between neighbouring points (in order);
 * `Infinity` for fewer than two points. */
export function medianSpacing(xs: number[]): number {
  if (xs.length < 2) return Number.POSITIVE_INFINITY
  const gaps = xs
    .slice(1)
    .map((x, i) => Math.abs(x - xs[i]))
    .sort((a, b) => a - b)
  const mid = Math.floor(gaps.length / 2)
  return gaps.length % 2 ? gaps[mid] : (gaps[mid - 1] + gaps[mid]) / 2
}

/** Marker radius for points this far apart: the 4 px marker when they are
 * 10 px or more apart, else a 2 px dot, so a dense daily series reads as a
 * dotted line rather than a smear of overlapping markers. */
export function markerRadius(spacingPx: number): 2 | 4 {
  return spacingPx >= 10 ? 4 : 2
}

/** Whether a touch moved far enough between down and up to be a drag, not
 * a tap. */
export function isTap(dx: number, dy: number, slop = 10): boolean {
  return Math.hypot(dx, dy) < slop
}

/** Maps a time to an x position across `[x0, x0 + width]`; a zero-length
 * domain puts everything in the middle. */
export function timeScale(
  start: number,
  end: number,
  x0: number,
  width: number
): (t: number) => number {
  const span = end - start
  return (t) => x0 + (span > 0 ? ((t - start) / span) * width : width / 2)
}

/** Every local 1 January strictly after `start` and at or before `end`,
 * in epoch ms, when the span is at least `minMonths` months; otherwise
 * none. */
export function yearBoundaries(
  start: number,
  end: number,
  minMonths = YEAR_LINES_MIN_MONTHS
): number[] {
  if (!Number.isFinite(start) || !Number.isFinite(end)) return []
  if (end - start < minMonths * MONTH_MS) return []
  const out: number[] = []
  for (let y = new Date(start).getFullYear() + 1; ; y++) {
    const t = new Date(y, 0, 1).getTime()
    if (t > end) break
    if (t > start) out.push(t)
  }
  return out
}

/** The span from the axis start to the first point, when the first point
 * is at least one day after the start; undefined otherwise. */
export function emptySpan(
  start: number,
  first: number
): { from: number; to: number } | undefined {
  if (!Number.isFinite(start) || !Number.isFinite(first)) return undefined
  if (first - start < DAY_MS) return undefined
  return { from: start, to: first }
}

/** Average advance of one character of the small text, in px, used to
 * estimate a label's width without measuring it. */
export const SMALL_CHAR_PX = 6.6

/** Whether the empty span's label fits inside it at this pixel width: the
 * span is at least 96 px and wider than the label's estimated width plus a
 * 6 px inset either side. */
export function emptyLabelFits(widthPx: number, labelChars = 0): boolean {
  return (
    widthPx >= Math.max(EMPTY_LABEL_MIN_PX, labelChars * SMALL_CHAR_PX + 12)
  )
}

/** Index of the entry in `xs` nearest to `px`; null for an empty list. */
export function nearestIndex(xs: number[], px: number): number | null {
  if (!xs.length) return null
  let best = 0
  for (let i = 1; i < xs.length; i++) {
    if (Math.abs(xs[i] - px) < Math.abs(xs[best] - px)) best = i
  }
  return best
}

/** Horizontal centre for a readout bubble of `bubble` px over a point at
 * `x`, kept `margin` px inside a frame of `frame` px. A bubble wider than
 * the frame centres on it. */
export function readoutAnchor(
  x: number,
  bubble: number,
  frame: number,
  margin = 4
): number {
  const half = bubble / 2 + margin
  if (frame <= 2 * half) return frame / 2
  return Math.min(Math.max(x, half), frame - half)
}

/** Which side of an anchor at `y` px a bubble of `bubbleH` px opens on, in
 * a frame `frameH` px tall: above when it fits, else below when that fits,
 * else whichever side has more room. */
export function readoutSide(
  y: number,
  bubbleH: number,
  frameH: number,
  gap = 4
): "top" | "bottom" {
  if (y - bubbleH - gap >= 0) return "top"
  if (y + bubbleH + gap <= frameH) return "bottom"
  return y >= frameH - y ? "top" : "bottom"
}

/** SVG path data for a marker of the given shape centred on (cx, cy). The
 * shapes share one visual weight with the 8 px circle: square 7 px, diamond
 * 9 px across, triangle 9 px wide, cross 9 px across with 3.5 px arms.
 * `grow` enlarges every dimension (the active point). Circle returns a path
 * too; the chart itself draws circles as `<circle>`. */
export function markerPath(
  shape: TrendMarker,
  cx: number,
  cy: number,
  grow = 0
): string {
  const f = (n: number) => Number(n.toFixed(2))
  switch (shape) {
    case "square": {
      const h = 3.5 + grow
      return `M${f(cx - h)},${f(cy - h)}h${f(2 * h)}v${f(2 * h)}h${f(-2 * h)}Z`
    }
    case "diamond": {
      const h = 4.5 + grow
      return `M${f(cx)},${f(cy - h)}L${f(cx + h)},${f(cy)}L${f(cx)},${f(cy + h)}L${f(cx - h)},${f(cy)}Z`
    }
    case "triangle": {
      // Equilateral, 9 px wide, centred on its centroid.
      const w = 9 + 2 * grow
      const ht = (w * Math.sqrt(3)) / 2
      const top = cy - (2 * ht) / 3
      const base = cy + ht / 3
      return `M${f(cx)},${f(top)}L${f(cx + w / 2)},${f(base)}L${f(cx - w / 2)},${f(base)}Z`
    }
    case "cross": {
      // An X: a plus of 3.5 px arms turned 45 degrees, 9 px across.
      const r = 5 + grow
      const a = 1.75 + grow / 3
      const pts: [number, number][] = [
        [-a, -r],
        [a, -r],
        [a, -a],
        [r, -a],
        [r, a],
        [a, a],
        [a, r],
        [-a, r],
        [-a, a],
        [-r, a],
        [-r, -a],
        [-a, -a],
      ]
      const c = Math.SQRT1_2
      return `${pts
        .map(
          ([px, py], i) =>
            `${i ? "L" : "M"}${f(cx + (px - py) * c)},${f(cy + (px + py) * c)}`
        )
        .join("")}Z`
    }
    default: {
      const r = 4 + grow
      return `M${f(cx - r)},${f(cy)}a${r},${r} 0 1,0 ${f(2 * r)},0a${r},${r} 0 1,0 ${f(-2 * r)},0Z`
    }
  }
}
