/** The timeline's pure half. A trait view reads each implementor on ITS OWN
 * bound point (`dueAt` for a task, `at` for an event), never on a shared
 * `at`: the engine leaves `at` NULL for a `temporal(point: dueAt)` kind, so
 * one cross-kind read would drop every task. Here: which implementors a
 * timeline reads, the one-page read per implementor with the window folded
 * in as `gte`/`lt` on that point, and the merge of every page into local
 * days. `now` is an argument so a read's key is stable across renders and a
 * test is deterministic. */

import type { ListParams } from "@/lib/api/records"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { temporalPoint } from "@/lib/apps/queries"
import type { Problem, ViewContext, ViewSpec } from "@/lib/apps/spec"
import { parseDuration } from "@/lib/apps/time"
import { substituteFilter } from "@/lib/apps/tokens"
import { splitKind, temporalProperties } from "@/lib/definition"
import { recordPath } from "@/lib/record-path"

/** The span when the view declares none: a week back, a month ahead. */
export const DEFAULT_PAST = "P7D"
export const DEFAULT_FUTURE = "P30D"

const DAY = 24 * 60 * 60_000

function traitsOf(kind: KindInfo): string[] {
  const traits = kind.definition?.traits
  return Array.isArray(traits)
    ? traits.filter((t): t is string => typeof t === "string")
    : []
}

/** Whether an implementor belongs on the timeline: it binds a temporal point
 * and it is not the record OF a slot. An occurrence log (the `occurrencelog`
 * trait, or a kind named `…log`) marks a slot done or skipped and would
 * repeat the slot it belongs to. */
export function isTimelineKind(kind: KindInfo): boolean {
  if (kind.name.endsWith("log")) return false
  if (traitsOf(kind).some((t) => /^occurrencelog\b/.test(t))) return false
  return Boolean(temporalPoint(kind))
}

export interface TimelineRead {
  kind: KindInfo
  /** The property the kind's instant lives under. */
  point: string
  /** Whether the kind binds `temporal(range)` and carries `endsAt`. */
  range: boolean
  /** Absent while a token cannot resolve; `problems` says why. */
  params?: ListParams
  problems: Problem[]
}

/** The `gte`/`lt` pair a read sends on the kind's point: the view's window
 * around `now`, or the bounds handed in when a person chose a range. */
export interface PointBounds {
  gte: string
  lt: string
}

/** The one-page read of one implementor: the view's filter with tokens
 * resolved, `via` folded in, the window (or the chosen `bounds`) as
 * `gte`/`lt` on the kind's point, ordered on that point, at the view's page
 * size. */
export function timelineRead(
  spec: ViewSpec,
  ctx: ViewContext,
  kind: KindInfo,
  now: number,
  bounds?: PointBounds
): TimelineRead {
  const point = temporalPoint(kind) ?? "at"
  const range = temporalProperties(kind).includes("endsAt")
  const { filter, problems } = substituteFilter(spec.filter, ctx)
  const properties = { ...(filter.properties ?? {}) }
  if (spec.via && ctx.parent) {
    properties[spec.via] = {
      eq: recordPath(ctx.parent.record.kind, ctx.parent.record.id),
    }
  }
  const past =
    parseDuration(spec.window.past ?? DEFAULT_PAST) ??
    parseDuration(DEFAULT_PAST) ??
    0
  const future =
    parseDuration(spec.window.future ?? DEFAULT_FUTURE) ??
    parseDuration(DEFAULT_FUTURE) ??
    0
  properties[point] = {
    ...(properties[point] ?? {}),
    gte: bounds?.gte ?? new Date(now - past).toISOString(),
    lt: bounds?.lt ?? new Date(now + future).toISOString(),
  }
  if (problems.length) return { kind, point, range, problems }
  const { authority, pkg, name } = splitKind(kind.identity)
  return {
    kind,
    point,
    range,
    problems,
    params: {
      authority,
      package: pkg,
      name,
      first: spec.first,
      filter: { ...filter, properties },
      orderBy: `${point}:asc`,
    },
  }
}

export interface TimelineRow {
  record: SubstrateRecord
  kind: KindInfo
  /** The instant, in epoch milliseconds. */
  at: number
  /** The end of a range kind's row, when the record carries one. */
  endsAt?: number
}

function instant(value: unknown): number | undefined {
  if (typeof value !== "string" || !value) return undefined
  const t = Date.parse(value)
  return Number.isNaN(t) ? undefined : t
}

/** Every page's rows as one sequence ordered by instant, then title, each
 * row read on its own kind's point. A row without an instant is dropped: the
 * window read excludes it server-side, and a timeline has no "Undated". */
export function mergeRows(
  pages: { read: TimelineRead; records: SubstrateRecord[] }[]
): TimelineRow[] {
  const rows: TimelineRow[] = []
  for (const { read, records } of pages) {
    for (const record of records) {
      const at = instant(record.properties[read.point])
      if (at === undefined) continue
      const endsAt = read.range ? instant(record.properties.endsAt) : undefined
      rows.push({ record, kind: read.kind, at, endsAt })
    }
  }
  return rows.sort(
    (a, b) =>
      a.at - b.at ||
      String(a.record.properties.title ?? "").localeCompare(
        String(b.record.properties.title ?? "")
      )
  )
}

function startOfDay(t: number): number {
  const d = new Date(t)
  d.setHours(0, 0, 0, 0)
  return d.getTime()
}

/** The local calendar day an instant falls on, as `YYYY-MM-DD`, which sorts
 * as it reads. */
export function dayKey(t: number): string {
  const d = new Date(t)
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`
}

/** The heading a day gets: Today, Tomorrow and Yesterday by name, every
 * other day its weekday and date, the year joining only once it differs. */
export function dayLabel(key: string, now = Date.now()): string {
  const [y, m, d] = key.split("-").map(Number)
  const day = new Date(y, m - 1, d).getTime()
  const days = Math.round((day - startOfDay(now)) / DAY)
  if (days === 0) return "Today"
  if (days === 1) return "Tomorrow"
  if (days === -1) return "Yesterday"
  const sameYear = y === new Date(now).getFullYear()
  return new Intl.DateTimeFormat(undefined, {
    weekday: "long",
    month: "short",
    day: "numeric",
    ...(sameYear ? {} : { year: "numeric" }),
  }).format(day)
}

export interface TimelineDay {
  key: string
  rows: TimelineRow[]
}

/** Rows already in instant order folded into their days, empty days absent
 * by construction. */
export function groupByDay(rows: TimelineRow[]): TimelineDay[] {
  const days: TimelineDay[] = []
  for (const row of rows) {
    const key = dayKey(row.at)
    const last = days[days.length - 1]
    if (last && last.key === key) last.rows.push(row)
    else days.push({ key, rows: [row] })
  }
  return days
}
