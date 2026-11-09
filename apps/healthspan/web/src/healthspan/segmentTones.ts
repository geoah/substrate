// Segment tones for the Healthspan contributor and pace bars; the signal
// and body scales are in scales.ts. A bar is a
// scale cut into equal segments, each with a tone and an intensity
// level from 1 (faint) to 4 (full); the ZoneBar component draws them. The
// contributor bars follow the engine's ramp (engine/segments.ts):
// grey where a value neither adds nor removes years, orange (`warning`)
// where it adds them and teal (`success`) where it removes them, each in
// four steps against that bar's own worst or best segment. Every status
// colour sits beside a word on the screen.

import type { ZoneBarLevel, ZoneBarSegment } from "../ui/ZoneBar"
import { PACE_MAX, PACE_MIN, yearsAt } from "../engine/engine"
import type { Metric } from "../engine/report"
import { ADDS_YEARS } from "../engine/focus"

export type ZoneLevel = ZoneBarLevel
export type ZoneSegment = ZoneBarSegment

/** A segment's years below this, either way, is neutral. */
export const NEUTRAL_YEARS = 0.025

const level = (ratio: number): ZoneLevel =>
  Math.min(4, Math.max(1, Math.ceil(ratio * 4 - 1e-9))) as ZoneLevel

/** Contributor bar segments from the years at each segment's midpoint
 * (`metric.scale.segments`). A bar that never removes years (lean body
 * mass) never shows `success`.
 *
 * `held` overrides segments by index: a segment that holds a marker, or
 * the target, says what the marker's own value says, not what its
 * midpoint says (`markerTone`). So the colour under each marker agrees with
 * the row's badge and impact sign. */
export function segmentTones(
  years: number[],
  held?: Map<number, ZoneSegment>
): ZoneSegment[] {
  const worst = Math.max(0, ...years)
  const best = Math.min(0, ...years)
  return years.map((y, i) => {
    const h = held?.get(i)
    if (h) return h
    if (Math.abs(y) < NEUTRAL_YEARS) return { tone: "neutral" }
    return y > 0
      ? { tone: "warning", level: level(y / worst) }
      : { tone: "success", level: level(y / best) }
  })
}

/** A marker's segment by the badge rule: `warning` when its value adds
 * `ADDS_YEARS` or more, `success` when it removes at least `NEUTRAL_YEARS`,
 * else `neutral`. The level is the midpoint's when the tones agree, so a
 * bar keeps its ramp. */
export function markerTone(
  markerYears: number,
  base: ZoneSegment
): ZoneSegment {
  const tone =
    markerYears >= ADDS_YEARS
      ? "warning"
      : markerYears <= -NEUTRAL_YEARS
        ? "success"
        : "neutral"
  if (tone === "neutral") return { tone }
  return { tone, level: base.tone === tone ? base.level : 1 }
}

/** A contributor bar's segments. Each segment holding a marker takes that
 * marker's tone (the 30-day one wins a shared segment, as the badge is
 * about the last 30 days); the target's segment without a marker is
 * neutral. */
export function contributorSegments(m: Metric, target?: number): ZoneSegment[] {
  const base = segmentTones(m.scale.segments)
  const { min, max, segments } = m.scale
  const w = (max - min) / segments.length
  const index = (v: number) =>
    Math.max(0, Math.min(segments.length - 1, Math.floor((v - min) / w)))
  const held = new Map<number, ZoneSegment>()
  if (target !== undefined && target >= min && target <= max)
    held.set(index(target), { tone: "neutral" })
  for (const v of [m.sixMonth, m.thirtyDay]) {
    if (v === null) continue
    const i = index(v)
    held.set(i, markerTone(yearsAt(m.curve, v), base[i]))
  }
  return segmentTones(segments, held)
}

/** A bar of `count` equal segments over [min, max], toned by each
 * segment's midpoint. */
export function bandSegments(
  min: number,
  max: number,
  count: number,
  at: (mid: number) => ZoneSegment
): ZoneSegment[] {
  const w = (max - min) / count
  return Array.from({ length: count }, (_, i) => at(min + (i + 0.5) * w))
}

export interface ZoneScale {
  min: number
  max: number
  segments: ZoneSegment[]
}

// ---- recovery ------------------------------------------------------------

/** WHOOP's recovery bands: red below 34, yellow 34 to 66, green from 67. */
export const RECOVERY_YELLOW = 34
export const RECOVERY_GREEN = 67

export function recoveryWord(v: number): {
  word: "Green" | "Yellow" | "Red"
  tone: "success" | "warning" | "danger"
} {
  return v >= RECOVERY_GREEN
    ? { word: "Green", tone: "success" }
    : v >= RECOVERY_YELLOW
      ? { word: "Yellow", tone: "warning" }
      : { word: "Red", tone: "danger" }
}

// ---- blood oxygen ------------------------------------------------------------

/** The app rule's threshold: below 95 percent is low. */
export const SPO2_LOW = 95

// ---- pace of aging -------------------------------------------------------------

/** Paces within this of 1.0x are steady (neutral). */
export const PACE_STEADY = 0.05

/** The pace bar: forty segments from `min` to `max` (the engine's clamp),
 * `success` below 1.0x, `neutral` within 0.05 of it, `warning` above, the
 * level rising with the distance from 1.0x in four equal steps. */
export function paceScale(min: number, max: number): ZoneScale {
  const far = Math.max(1 - min, max - 1)
  return {
    min,
    max,
    segments: bandSegments(min, max, 40, (m) => {
      const d = m - 1
      if (Math.abs(d) <= PACE_STEADY + 1e-9) return { tone: "neutral" }
      return {
        tone: d < 0 ? "success" : "warning",
        level: level(Math.abs(d) / far),
      }
    }),
  }
}

/** The engine's clamp, -1.0x to 3.0x. */
export const PACE_SCALE: ZoneScale = paceScale(PACE_MIN, PACE_MAX)

/** "Reversing" below 0, "Steady" over the bar's grey segments (0.9x up to
 * 1.1x), "Slowing" below them, "Accelerated" above. */
export function paceWord(
  pace: number
): "Reversing" | "Slowing" | "Steady" | "Accelerated" {
  if (pace < 0) return "Reversing"
  if (pace >= 1 - 2 * PACE_STEADY && pace < 1 + 2 * PACE_STEADY) return "Steady"
  return pace < 1 ? "Slowing" : "Accelerated"
}
