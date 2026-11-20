// The contributors worth working on, and the band where a contributor
// costs nothing. One makes the focus list when its 6-month average adds
// years, or when its last 30 days would add more than its 6 months do (it
// is slipping, which is what moves the pace of aging). The number is the
// years at stake: what it adds now, plus what the last 30 days would add
// on top if they held for six months.

import { formatImpact, formatValueText } from "./format"
import type { Metric } from "./report"

/** A 6-month impact at or above this adds years. */
export const ADDS_YEARS = 0.05
/** A 30-day impact this much above the 6-month one is slipping. */
export const SLIPPING = 0.1
/** A bar segment whose years are at or below this costs nothing. */
const NO_COST = 0.04

export function atStake(m: Metric): number {
  const now = m.impact ?? 0
  return Math.max(0, now) + Math.max(0, (m.impact30 ?? now) - now)
}

/** Whether the last 30 days would add `SLIPPING` years or more on top of
 * the 6 months. */
export function isSlipping(m: Metric): boolean {
  return (m.impact30 ?? 0) - (m.impact ?? 0) >= SLIPPING
}

/** The contributors that add years or are slipping, most years at stake
 * first. `limit` caps the list. */
export function focusList(
  metrics: Metric[],
  { limit }: { limit?: number } = {}
): Metric[] {
  const ranked = metrics
    .filter((m) => m.impact !== null)
    .filter((m) => (m.impact ?? 0) >= ADDS_YEARS || isSlipping(m))
    .sort((a, b) => atStake(b) - atStake(a))
  return limit === undefined ? ranked : ranked.slice(0, limit)
}

/** `1:07 h this month, 1:34 h over 6 months. Slipping: if it holds, it adds
 * 0.5 more years.` */
export function focusSentence(m: Metric): string {
  const now = formatValueText(m.thirtyDay, m.format)
  const before = formatValueText(m.sixMonth, m.format)
  const impact = m.impact ?? 0
  const recent = m.impact30 ?? impact
  const head = `${now} this month, ${before} over 6 months.`
  if (recent - impact >= SLIPPING)
    return `${head} Slipping: if it holds, it adds ${formatImpact(recent - impact).replace("+", "")} more years.`
  if (impact >= ADDS_YEARS) return `${head} Below target, adding years now.`
  return head
}

/** The value range where the bar is grey or green: the habit costs nothing.
 * Undefined when no segment qualifies. */
export function neutralBand(m: Metric): [number, number] | undefined {
  const segs = m.scale.segments
  const w = (m.scale.max - m.scale.min) / segs.length
  const ok = segs.map((y, i) => (y <= NO_COST ? i : -1)).filter((i) => i >= 0)
  if (ok.length === 0) return undefined
  return [m.scale.min + ok[0] * w, m.scale.min + (ok[ok.length - 1] + 1) * w]
}
