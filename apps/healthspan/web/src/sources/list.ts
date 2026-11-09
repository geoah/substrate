// The list of sources (docs/plans/healthspan.md, Sources): every
// `observations/source` record, what it reports with the newest value's
// time from one latest read grouped by metric and source, and the nights,
// workouts and lab reports it holds. One pure transform, which the live
// read and the demo both run.

import type { AggregateRow, SubstrateRecord } from "@substrate/client"
import type { MetricRecord } from "../catalogue/types"
import type { SourceSummary } from "../data/sources"
import { KIND } from "../kinds"
import { priorityKey } from "./groups"

export interface SourceListInput {
  sources: readonly SubstrateRecord[]
  /** The latest read's rows, grouped by metric and source. */
  latest: readonly AggregateRow[]
  /** Records per source id. */
  nights: ReadonlyMap<string, number>
  workouts: ReadonlyMap<string, number>
  reports: ReadonlyMap<string, number>
}

function idAfter(path: string | undefined, kind: string): string | undefined {
  const prefix = `${kind}/`
  return path?.startsWith(prefix) ? path.slice(prefix.length) : undefined
}

function priorities(r: SubstrateRecord): Record<string, number> {
  const raw = r.properties.priorities
  const out: Record<string, number> = {}
  if (!raw || typeof raw !== "object") return out
  for (const [k, v] of Object.entries(raw as Record<string, unknown>)) {
    const n = typeof v === "number" ? v : Number(v)
    if (Number.isFinite(n) && n >= 1) out[priorityKey(k)] = n
  }
  return out
}

/** A name for a source that holds values but has no record of its own:
 * its id's last part. */
function nameFromId(id: string): string {
  const last = id.split("/").filter(Boolean).pop() ?? id
  return last.replace(/[-_]+/g, " ")
}

export function buildSourceList(input: SourceListInput): SourceSummary[] {
  const byId = new Map<string, SourceSummary>()
  for (const r of input.sources) {
    const name = r.properties.name
    const cls = r.properties.class
    byId.set(r.id, {
      id: r.id,
      name: typeof name === "string" && name ? name : nameFromId(r.id),
      class: typeof cls === "string" && cls ? cls : undefined,
      metrics: [],
      nights: 0,
      workouts: 0,
      reports: 0,
      priorities: priorities(r),
    })
  }
  const ensure = (id: string): SourceSummary => {
    let s = byId.get(id)
    if (!s) {
      s = {
        id,
        name: nameFromId(id),
        metrics: [],
        nights: 0,
        workouts: 0,
        reports: 0,
        priorities: {},
      }
      byId.set(id, s)
    }
    return s
  }
  for (const row of input.latest) {
    const source = idAfter(row.group?.source, KIND.source)
    const metric = idAfter(row.group?.metric, KIND.metric)
    if (!source || !metric) continue
    const s = ensure(source)
    const at = row.start
    const known = s.metrics.find((m) => m.metric === metric)
    if (!known) s.metrics.push({ metric, latest: at })
    else if (at && (!known.latest || Date.parse(at) > Date.parse(known.latest)))
      known.latest = at
  }
  for (const [id, n] of input.nights) if (n > 0) ensure(id).nights = n
  for (const [id, n] of input.workouts) if (n > 0) ensure(id).workouts = n
  for (const [id, n] of input.reports) if (n > 0) ensure(id).reports = n
  for (const s of byId.values())
    s.metrics.sort((a, b) => a.metric.localeCompare(b.metric))
  return [...byId.values()]
}

/** The classes in the order the list shows them. */
export const CLASS_ORDER = [
  "watch",
  "ring",
  "scale",
  "lab",
  "service",
  "app",
  "manual",
  "meter",
  "thermometer",
  "contact",
  "camera",
]

const CLASS_HEADERS: Record<string, string> = {
  watch: "Watches",
  ring: "Rings",
  scale: "Scales",
  lab: "Labs",
  service: "Services",
  app: "Apps",
  manual: "Entered by hand",
  meter: "Meters",
  thermometer: "Thermometers",
  contact: "Contact sensors",
  camera: "Cameras",
}

const CLASS_WORDS: Record<string, string> = {
  watch: "Watch",
  ring: "Ring",
  scale: "Scale",
  lab: "Lab",
  service: "Service",
  app: "App",
  manual: "Entered by hand",
  meter: "Meter",
  thermometer: "Thermometer",
  contact: "Contact sensor",
  camera: "Camera",
}

export function classWord(cls: string | undefined): string {
  return (cls && CLASS_WORDS[cls]) || "Other"
}

function classRank(cls: string | undefined): number {
  const i = cls ? CLASS_ORDER.indexOf(cls) : -1
  return i < 0 ? CLASS_ORDER.length : i
}

/** The sources by class, in the class order, each class by name. */
export function byClass(
  list: readonly SourceSummary[]
): { cls: string | undefined; header: string; sources: SourceSummary[] }[] {
  const groups = new Map<string, SourceSummary[]>()
  for (const s of list) {
    const key = s.class && CLASS_ORDER.includes(s.class) ? s.class : ""
    const g = groups.get(key) ?? []
    g.push(s)
    groups.set(key, g)
  }
  return [...groups.entries()]
    .sort((a, b) => classRank(a[0] || undefined) - classRank(b[0] || undefined))
    .map(([cls, sources]) => ({
      cls: cls || undefined,
      header: (cls && CLASS_HEADERS[cls]) || "Other",
      sources: sources.sort(
        (a, b) => a.name.localeCompare(b.name) || a.id.localeCompare(b.id)
      ),
    }))
}

/** Sorts sources the way an unranked group shows them: class, then name. */
export function defaultOrder(a: SourceSummary, b: SourceSummary): number {
  return (
    classRank(a.class) - classRank(b.class) ||
    a.name.localeCompare(b.name) ||
    a.id.localeCompare(b.id)
  )
}

/** A metric's name: its record's, else its id's last part in words. */
export function metricName(
  id: string,
  metrics: ReadonlyMap<string, MetricRecord>
): string {
  const m = metrics.get(id)
  if (m?.name) return m.name
  const last = id.split("/").pop() ?? id
  const words = last.replace(/_/g, " ")
  return words.charAt(0).toUpperCase() + words.slice(1)
}

/** The newest value's time among a source's metrics. */
export function newest(s: SourceSummary): string | undefined {
  let best: string | undefined
  for (const m of s.metrics)
    if (m.latest && (!best || Date.parse(m.latest) > Date.parse(best)))
      best = m.latest
  return best
}

/** A source's metric records, the ones the repository holds, by id. */
export function metricIndex(
  metrics: readonly MetricRecord[]
): Map<string, MetricRecord> {
  return new Map(metrics.map((m) => [m.id, m]))
}
