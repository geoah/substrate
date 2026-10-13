/** What a workout's or a sleep's record page shows of what was measured
 * while it lasted (the "During this" section), without React: which records
 * get the section, the reads it makes over the record's own `[at, endsAt)`,
 * how a night's sleep stages draw, and the timeline's address for the same
 * window. The heart rate and the sleep stages are the health sample's
 * metrics (`health/heart_rate`, `health/sleep_stage`), under the metric
 * kind the observed kinds pin. */

import type { AggregateRead } from "@/lib/api/observations"
import {
  readReference,
  type Cond,
  type KindInfo,
  type RecordFilter,
  type SubstrateRecord,
} from "@/lib/api/types"
import type { ListParams } from "@/lib/api/records"
import { temporalProperties } from "@/lib/definition"
import {
  HEART_RATE_ZONES,
  OBSERVED_TRAIT_IDENTITY,
  observedKinds,
  observedPins,
} from "@/lib/observations"
import {
  autoBucket,
  bucketOf,
  isoWithOffset,
  type TimeRange,
} from "@/lib/observations-time"
import { kindHasTrait } from "@/lib/traits"

/** The health sample's metric ids the section reads. */
export const HEART_RATE_ID = "health/heart_rate"
export const SLEEP_STAGE_ID = "health/sleep_stage"

/** What one record's section reads: its window, the metrics by record path,
 * who it is about and what recorded it, and which parts it shows. */
export interface During {
  window: TimeRange
  heart: string
  /** A sleep's stages metric; absent for anything but a sleep. */
  stage?: string
  /** A workout shows its time in each heart rate zone. */
  zones: boolean
  /** The device or app that recorded the record, a record path. */
  source?: string
  /** Who it is about, a record path; absent is the repository's owner. */
  about?: string
  /** The observed kinds, for the reads' query keys. */
  observed: string[]
}

/** The section a record gets: any record of a kind of the health package
 * that binds `temporal(range)` and not `observed` (a workout, a sleep),
 * with a window that ends after it starts, in a repository whose observed
 * kinds pin a metric kind. Undefined for every other record. */
export function duringOf(
  record: SubstrateRecord,
  kind: KindInfo | undefined,
  kinds: readonly KindInfo[]
): During | undefined {
  if (!kind || kind.package !== "health") return undefined
  if (kindHasTrait(kind, OBSERVED_TRAIT_IDENTITY)) return undefined
  if (!temporalProperties(kind).includes("endsAt")) return undefined
  const p = record.properties ?? {}
  const at = typeof p.at === "string" ? Date.parse(p.at) : NaN
  const end = typeof p.endsAt === "string" ? Date.parse(p.endsAt) : NaN
  if (Number.isNaN(at) || Number.isNaN(end) || end <= at) return undefined
  const observed = observedKinds(kinds)
  const metric = observedPins(observed).metric
  if (!metric) return undefined
  return {
    window: { start: at, end },
    heart: `${metric}/${HEART_RATE_ID}`,
    ...(kind.name === "sleep" ? { stage: `${metric}/${SLEEP_STAGE_ID}` } : {}),
    zones: kind.name === "workout",
    source: readReference(p.source)?.path,
    about: readReference(p.about)?.path,
    observed: observed.map((k) => k.identity),
  }
}

/** The streams about who the record is about: a named record, or the
 * owner's, which name nobody. */
function aboutCond(d: Pick<During, "about">): Cond {
  return d.about ? { eq: d.about } : { exists: false }
}

function rangeOf(window: TimeRange) {
  return { gte: isoWithOffset(window.start), lt: isoWithOffset(window.end) }
}

/** Where sources overlap, the record's own device first, then the rest as
 * their records' priorities rank them. */
function bySource(d: Pick<During, "source">) {
  return {
    policy: "priority",
    ...(d.source ? { order: [d.source] } : {}),
  }
}

/** The heart rate over `window` (the record's, or a part zoomed into): an
 * average with its lowest and highest per bucket, the bucket sized to the
 * window as the timeline's automatic one is (`autoBucket`), one line. */
export function heartCurveRead(
  d: During,
  window: TimeRange,
  timezone: string
): AggregateRead {
  const filter: RecordFilter = {
    implements: OBSERVED_TRAIT_IDENTITY,
    properties: { about: aboutCond(d) },
  }
  return {
    filter,
    aggregate: {
      metric: { ref: d.heart },
      from: "raw",
      range: rangeOf(window),
      bucket: bucketOf(autoBucket(window), timezone),
      reducers: ["avg", "min", "max"],
      sources: bySource(d),
    },
  }
}

/** The time in each heart rate zone over the whole record, in minutes: one
 * row, a `durationIn` per zone (`HEART_RATE_ZONES`). */
export function zonesRead(d: During): AggregateRead {
  return {
    filter: {
      implements: OBSERVED_TRAIT_IDENTITY,
      properties: { about: aboutCond(d) },
    },
    aggregate: {
      metric: { ref: d.heart },
      from: "raw",
      range: rangeOf(d.window),
      reducers: HEART_RATE_ZONES.map(([lo, hi]) => ({ durationIn: [lo, hi] })),
      unit: "min",
      sources: bySource(d),
    },
  }
}

/** The narrowing a sleep's stages are read under: the sleep's own source
 * where it names one, else every source, the first by priority. */
function stageProperties(d: During): Record<string, Cond> {
  return {
    about: aboutCond(d),
    ...(d.source ? { source: { eq: d.source } } : {}),
  }
}

/** A night's minutes in each stage: one row per code, summed over the
 * stages that start inside the sleep. */
export function stageTotalsRead(d: During): AggregateRead {
  return {
    filter: {
      implements: OBSERVED_TRAIT_IDENTITY,
      properties: stageProperties(d),
    },
    aggregate: {
      metric: { ref: d.stage ?? "" },
      from: "raw",
      range: rangeOf(d.window),
      groupBy: ["code"],
      reducers: ["sum"],
      unit: "min",
      ...(d.source ? {} : { sources: { policy: "priority" } }),
    },
  }
}

/** The list of a night's stage records, oldest first: the window read of
 * the observed kinds over the sleep's `[at, endsAt)`. */
export function stageListParams(d: During): ListParams {
  return {
    kinds: d.observed,
    first: 500,
    orderBy: "at:asc",
    filter: {
      properties: {
        metric: { eq: d.stage ?? "" },
        ...stageProperties(d),
        at: {
          gte: new Date(d.window.start).toISOString(),
          lt: new Date(d.window.end).toISOString(),
        },
      },
    },
  }
}

/** One stage of a night: its code and its span. */
export interface StageSegment {
  code: string
  start: number
  end: number
}

/** A night's stage records as segments, oldest first: each record naming a
 * code over a span that ends after it starts. */
export function stageSegments(
  records: readonly SubstrateRecord[]
): StageSegment[] {
  const out: StageSegment[] = []
  for (const r of records) {
    const p = r.properties ?? {}
    const start = typeof p.at === "string" ? Date.parse(p.at) : NaN
    const end = typeof p.endsAt === "string" ? Date.parse(p.endsAt) : NaN
    if (typeof p.code !== "string" || !p.code) continue
    if (Number.isNaN(start) || Number.isNaN(end) || end <= start) continue
    out.push({ code: p.code, start, end })
  }
  return out.sort((a, b) => a.start - b.start)
}

/** The segments as a hypnogram, where they are one: two or more that never
 * overlap, so each instant is in one stage. A source that writes one span
 * per stage over the whole night (WHOOP, Apple Health's aggregated nights)
 * gives totals and no order of stages, and draws none. */
export function hypnogramOf(
  segments: readonly StageSegment[]
): StageSegment[] | undefined {
  if (segments.length < 2) return undefined
  for (let i = 1; i < segments.length; i++) {
    if (segments[i].start < segments[i - 1].end) return undefined
  }
  return [...segments]
}

/** The rows of a hypnogram, top to bottom: awake and in bed above, then
 * REM, sleep the source did not stage, light and deep. A code outside them
 * goes last. */
const STAGE_ORDER = ["awake", "in_bed", "rem", "asleep", "light", "deep"]

export function stageRows(segments: readonly StageSegment[]): string[] {
  const codes = [...new Set(segments.map((s) => s.code))]
  const rank = (c: string) => {
    const at = STAGE_ORDER.indexOf(c)
    return at < 0 ? STAGE_ORDER.length : at
  }
  return codes.sort((a, b) => rank(a) - rank(b) || a.localeCompare(b))
}

/** The timeline over the record's window: heart rate, and a sleep's stages
 * beside it, about who the record is about. */
export function duringTimelineSearch(d: During): Record<string, string> {
  return {
    metric: [d.heart, ...(d.stage ? [d.stage] : [])].join(","),
    ...(d.about ? { about: d.about } : {}),
    range: "custom",
    start: new Date(d.window.start).toISOString(),
    end: new Date(d.window.end).toISOString(),
  }
}
