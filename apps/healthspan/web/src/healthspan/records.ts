// Reading the shared kinds' property values as the server answers them: a
// reference as `{ref}` or a bare path, a quantity as `{value, unit}` with a
// decimal string, a decimal as a string, a duration as ISO 8601.

import type { AggregateAnswer, AggregateRow } from "@substrate/client"
import type { SubstrateRecord } from "@substrate/react"
import { unitSymbol } from "../lib/units"

export const SOURCE_PREFIX = "substrate.reamde.dev/observations/source/"
export const METRIC_PREFIX = "substrate.reamde.dev/observations/metric/"

/** `<authority>/<package>/<kind>/…`: an authority is a domain name. */
const FULL_PATH = /^[^/]+\.[^/]+\/[^/]+\/[^/]+\//

/** The id after `<prefix>` of a reference value, or undefined. */
export function refAfter(v: unknown, prefix: string): string | undefined {
  const path =
    typeof v === "string"
      ? v
      : v && typeof v === "object" && "ref" in v
        ? v.ref
        : undefined
  if (typeof path !== "string") return undefined
  if (path.startsWith(prefix)) return path.slice(prefix.length) || undefined
  // Another kind's path names nothing here; a bare id is the pinned kind's.
  return FULL_PATH.test(path) ? undefined : path || undefined
}

/** A source reference's id (`whoop/abc`). */
export const sourceId = (v: unknown) => refAfter(v, SOURCE_PREFIX)

/** A metric reference's id (`health/steps`). */
export const metricId = (v: unknown) => refAfter(v, METRIC_PREFIX)

/** A number, a decimal string, or a quantity's value. */
export function num(v: unknown): number | undefined {
  if (typeof v === "number") return Number.isFinite(v) ? v : undefined
  if (typeof v === "string" && v.trim() !== "") {
    const n = Number(v)
    return Number.isFinite(n) ? n : undefined
  }
  if (v && typeof v === "object" && "value" in v) return num(v.value)
  return undefined
}

/** A quantity's unit symbol. */
export function unitOf(v: unknown): string | undefined {
  if (!v || typeof v !== "object" || !("unit" in v)) return undefined
  return unitSymbol(v.unit)
}

const DURATION =
  /^P(?:(\d+(?:\.\d+)?)W)?(?:(\d+(?:\.\d+)?)D)?(?:T(?:(\d+(?:\.\d+)?)H)?(?:(\d+(?:\.\d+)?)M)?(?:(\d+(?:\.\d+)?)S)?)?$/

/** An ISO 8601 duration (`PT7H28M8S`, `P1DT2H`) in ms; a bare number is
 * seconds. */
export function durationMs(v: unknown): number | undefined {
  if (typeof v === "number") return Number.isFinite(v) ? v * 1000 : undefined
  if (typeof v !== "string") return undefined
  const m = DURATION.exec(v.trim())
  if (!m || v.trim() === "P" || v.trim() === "PT") return undefined
  const [w, d, h, min, s] = m.slice(1).map((x) => (x ? Number(x) : 0))
  return ((((w * 7 + d) * 24 + h) * 60 + min) * 60 + s) * 1000
}

export function str(r: SubstrateRecord, key: string): string | undefined {
  const v = r.properties[key]
  return typeof v === "string" && v !== "" ? v : undefined
}

export function int(r: SubstrateRecord, key: string): number | undefined {
  const v = num(r.properties[key])
  return v === undefined ? undefined : Math.round(v)
}

/** One value of an aggregate row by reducer position. */
export function rowValue(row: AggregateRow, i = 0): number | undefined {
  const v = row.values[i]?.value
  return v === null || v === undefined ? undefined : num(v)
}

/** A row's metric id, on a several-metric read. */
export function rowMetric(row: AggregateRow): string | undefined {
  const m = row.group?.metric
  if (!m) return undefined
  return m.startsWith(METRIC_PREFIX) ? m.slice(METRIC_PREFIX.length) : m
}

/** A row's local date (`YYYY-MM-DD`), as its start names it. */
export const rowDay = (row: AggregateRow) => row.start.slice(0, 10)

/** A recorded-local day read as one value per date, of one metric on a
 * several-metric read or of every row. */
export function dailyOf(
  answer: Pick<AggregateAnswer, "rows"> | undefined,
  metric?: string,
  reducer = 0
): Map<string, number> {
  const out = new Map<string, number>()
  for (const row of answer?.rows ?? []) {
    if (metric && rowMetric(row) !== metric) continue
    const v = rowValue(row, reducer)
    if (v !== undefined) out.set(rowDay(row), v)
  }
  return out
}
