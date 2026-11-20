// The records Sources and Import read, as the screens take them: a metric
// record as the catalogue matches it, a lab result as the duplicate check
// reads it, the imports in order, and the lab's source an import names
// when it is planned. Pure, so the demo runs them too.

import type { SubstrateRecord } from "@substrate/client"
import type { MetricRecord } from "../catalogue/types"
import { normalise } from "../import/columns"
import { readableLabId } from "../import/ids"
import type { HeldResult } from "../import/match"
import { refPath } from "../import/records"
import { localDay } from "../import/values"
import { KIND, type ImportRecord } from "../kinds"

export function readMetric(r: SubstrateRecord): MetricRecord {
  const p = r.properties
  const codes = (Array.isArray(p.codes) ? p.codes : []).flatMap((c) => {
    if (!c || typeof c !== "object") return []
    const o = c as Record<string, unknown>
    return typeof o.system === "string" && typeof o.code === "string"
      ? [{ system: o.system, code: o.code }]
      : []
  })
  const unit = refPath(p.unit)
  const unitPrefix = `${KIND.unit}/`
  return {
    id: r.id,
    name: typeof p.name === "string" ? p.name : r.id,
    unit: unit?.startsWith(unitPrefix) ? unit.slice(unitPrefix.length) : unit,
    codes,
    category: typeof p.category === "string" ? p.category : undefined,
    precision:
      typeof p.displayPrecision === "number" ? p.displayPrecision : undefined,
    description: typeof p.description === "string" ? p.description : undefined,
  }
}

/** A metric an import may write: one of the `labs` or `body` catalogue. */
export function isLabMetric(m: MetricRecord): boolean {
  return m.category === "labs" || m.category === "body"
}

/** Imports newest first: by their ids, which are time-ordered. */
export function sortImports(list: readonly ImportRecord[]): ImportRecord[] {
  return [...list].sort((a, b) => b.id.localeCompare(a.id))
}

/** The lab's source an import names when it is planned: a lab of the same
 * name the repository holds, or one an earlier writer named `lab/<name>`;
 * else none, and setup creates one. */
export function plannedSource(
  lab: string,
  sources: readonly SubstrateRecord[]
): string | undefined {
  const n = normalise(lab)
  const labs = sources.filter((s) => s.properties.class === "lab")
  const same = labs.find(
    (s) =>
      typeof s.properties.name === "string" &&
      normalise(s.properties.name) === n
  )
  return same?.id ?? labs.find((s) => s.id === readableLabId(lab))?.id
}

/** A lab result as the duplicate check reads it. */
export function heldOf(r: SubstrateRecord): HeldResult[] {
  const metric = refPath(r.properties.metric)
  const at = r.properties.at
  const prefix = `${KIND.metric}/`
  if (!metric?.startsWith(prefix) || typeof at !== "string") return []
  const v = r.properties.value as
    { value?: unknown; unit?: unknown } | undefined
  const unit = refPath(v?.unit)
  return [
    {
      metric: metric.slice(prefix.length),
      day: localDay(at),
      value: v?.value !== undefined ? Number(v.value) : undefined,
      unit: unit?.split("/core/unit/").pop(),
      text:
        typeof r.properties.text === "string" ? r.properties.text : undefined,
    },
  ]
}
