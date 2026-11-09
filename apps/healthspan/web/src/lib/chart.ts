// Chart helpers the Labs and Healthspan screens share: bands clipped to the
// data so a far band never flattens the line, and tick labels capped at six
// characters so a fixed 52 px gutter fits every chart and plots align.

import { stepDecimals } from "./trendMath"

/** The fixed value gutter of every card chart, in px (six characters). */
export const CHART_GUTTER = 52

/** The widest tick label, in characters. */
export const TICK_MAX_CHARS = 6

/** A band as a chart draws it; the shape of the TrendChart's `TrendBand`. */
export interface ClippableBand {
  from?: number
  to?: number
}

/** Bands clipped to the data window [min minus 25 percent of the span, max
 * plus 25 percent]: a band lying entirely outside it is dropped, and a
 * bound beyond it becomes open, so the band runs to the plot's edge without
 * stretching the value axis and without drawing an edge line where the
 * band does not really end. `values` are the drawn points plus the
 * trendline ends. With no values there is nothing to clip to and no band
 * is drawn: a chart with no points in its range shows no zone. */
export function clipBands<B extends ClippableBand>(
  bands: readonly B[],
  values: number[]
): B[] {
  const finite = values.filter(Number.isFinite)
  if (!finite.length) return []
  const lo = Math.min(...finite)
  const hi = Math.max(...finite)
  const pad = Math.max((hi - lo) * 0.25, Math.abs(hi) * 0.02, 1e-6)
  const min = lo - pad
  const max = hi + pad
  const out: B[] = []
  for (const b of bands) {
    if (
      (b.from !== undefined && b.from >= max) ||
      (b.to !== undefined && b.to <= min)
    )
      continue
    out.push({
      ...b,
      from: b.from !== undefined && b.from > min ? b.from : undefined,
      to: b.to !== undefined && b.to < max ? b.to : undefined,
    })
  }
  return out
}

function grouped(v: number, decimals: number): string {
  return v.toLocaleString("en-GB", {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  })
}

/** A value-axis label: the step's decimals with en-GB grouping, thousands
 * as "1.2K" (millions "3.4M") when the step is 1,000 or more, and never
 * more than six characters: decimals are dropped first, then the value is
 * shortened to K or M. */
export function tickText(value: number, step: number): string {
  const short = (v: number) => {
    const abs = Math.abs(v)
    const [div, suffix] = abs >= 1e6 ? [1e6, "M"] : [1e3, "K"]
    const n = v / div
    for (const d of [1, 0]) {
      const s = `${grouped(n, Number.isInteger(n) ? 0 : d)}${suffix}`
      if (s.length <= TICK_MAX_CHARS) return s
    }
    return `${grouped(n, 0)}${suffix}`
  }
  if (step >= 1000) return short(value)
  for (let d = stepDecimals(step); d >= 0; d--) {
    const s = grouped(value, d)
    if (s.length <= TICK_MAX_CHARS) return s
  }
  return short(value)
}
