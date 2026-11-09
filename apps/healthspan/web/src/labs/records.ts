// The lab kinds as the screens read them: a metric, a source, a report, an
// analyte and a result from their records. A result keeps its value in the
// unit it was written in, its ranges brought into that unit.

import { parseReference, type SubstrateRecord } from "@substrate/client"
import { KIND } from "../kinds"
import { unitSymbol } from "../lib/units"
import type {
  Band,
  LabReport,
  LabResult,
  MetricInfo,
  SourceInfo,
  StoredFile,
} from "./types"
import {
  convert,
  parseRational,
  type Analyte,
  type AnalyteConversion,
  type UnitGraph,
} from "./units"

type Props = Record<string, unknown>

function num(v: unknown): number | undefined {
  const n =
    typeof v === "number" ? v : typeof v === "string" ? Number(v) : Number.NaN
  return Number.isFinite(n) ? n : undefined
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v.trim() ? v : undefined
}

/** The id a reference names, for a reference of `kind`: `{ref}` as served
 * or the bare path. */
export function refId(v: unknown, kind: string): string | undefined {
  const path =
    typeof v === "string"
      ? v
      : v && typeof v === "object" && "ref" in v
        ? v.ref
        : undefined
  const ref = typeof path === "string" ? parseReference(path) : undefined
  return ref?.kind === kind ? ref.id : undefined
}

export function toMetric(r: SubstrateRecord): MetricInfo {
  const p = r.properties as Props
  const codes = Array.isArray(p.codes)
    ? (p.codes as unknown[]).flatMap((c) => {
        const o = c as Props | null
        const system = str(o?.system)
        const code = str(o?.code)
        return system && code ? [{ system, code }] : []
      })
    : []
  return {
    id: r.id,
    name: str(p.name) ?? r.id,
    unit: unitSymbol(p.unit),
    codes,
    precision: num(p.displayPrecision),
    category: str(p.category),
    description: str(p.description),
  }
}

export function toSource(r: SubstrateRecord): SourceInfo {
  const p = r.properties as Props
  return { id: r.id, name: str(p.name) ?? r.id, class: str(p.class) }
}

function fileOf(v: unknown): StoredFile | undefined {
  const o = v as Partial<StoredFile> | null | undefined
  if (!o || typeof o.digest !== "string") return undefined
  return {
    digest: o.digest,
    mediaType: typeof o.mediaType === "string" ? o.mediaType : undefined,
    name: typeof o.name === "string" ? o.name : undefined,
    size: typeof o.size === "number" ? o.size : undefined,
  }
}

export function toReport(r: SubstrateRecord): LabReport {
  const p = r.properties as Props
  return {
    id: r.id,
    at: str(p.at) ?? r.createdAt,
    lab: str(p.lab),
    accession: str(p.accession),
    issuedAt: str(p.issuedAt),
    status: str(p.status),
    review: str(p.review),
    source: refId(p.source, KIND.source),
    file: fileOf(p.file),
  }
}

export function toAnalyte(r: SubstrateRecord): Analyte | undefined {
  const p = r.properties as Props
  const metric = refId(p.metric, KIND.metric)
  if (!metric) return undefined
  const conversions: AnalyteConversion[] = Array.isArray(p.conversions)
    ? (p.conversions as unknown[]).flatMap((c) => {
        const o = c as Props | null
        const target = refId(o?.target, KIND.metric)
        const formula = str(o?.formula)
        if (!target || !formula) return []
        const parameters: Record<string, number> = {}
        const raw = o?.parameters
        if (raw && typeof raw === "object")
          for (const [k, v] of Object.entries(raw as Props)) {
            const n = parseRational(v)
            if (n !== undefined) parameters[k] = n
          }
        return [{ target, formula, parameters }]
      })
    : []
  return {
    id: r.id,
    metric,
    molarMass: parseRational(p.molarMass),
    conversions,
  }
}

/** A quantity's number, unit and comparator. */
function quantity(q: unknown): {
  value?: number
  unit?: string
  comparator?: string
} {
  const o = q as Props | null | undefined
  return {
    value: num(o?.value),
    unit: unitSymbol(o?.unit),
    comparator: str(o?.comparator),
  }
}

/** A range bound in `unit`: converted where it was written in another. */
function bound(q: unknown, unit: string | undefined, graph: UnitGraph) {
  if (q === undefined || q === null) return undefined
  const b = quantity(q)
  if (b.value === undefined) return undefined
  if (!b.unit || !unit || b.unit === unit) return b.value
  return convert(graph, b.value, b.unit, unit) ?? b.value
}

export function toResult(
  r: SubstrateRecord,
  metrics: ReadonlyMap<string, MetricInfo>,
  graph: UnitGraph
): LabResult {
  const p = r.properties as Props
  const metric = refId(p.metric, KIND.metric) ?? ""
  const v = quantity(p.value)
  const unit = v.unit ?? metrics.get(metric)?.unit
  const ranges: Band[] = Array.isArray(p.referenceRanges)
    ? (p.referenceRanges as Props[]).map((b) => ({
        low: bound(b?.low, unit, graph),
        high: bound(b?.high, unit, graph),
        text: str(b?.text),
        tone: str(b?.tone),
      }))
    : []
  const codedValue = p.codedValue as
    { code?: unknown; display?: unknown } | undefined
  const code = str(p.code)
  const coded =
    codedValue && (str(codedValue.code) || str(codedValue.display))
      ? { code: str(codedValue.code), display: str(codedValue.display) }
      : code
        ? { code }
        : undefined
  const ratio = p.ratio as Props | undefined
  const numerator = num(ratio?.numerator)
  const denominator = num(ratio?.denominator)
  const derived = Array.isArray(p.derivedFrom)
    ? (p.derivedFrom as unknown[])
        .map((d) => refId(d, KIND.labresult))
        .filter((d): d is string => !!d)
    : []
  const specimen = p.specimen as Props | undefined
  return {
    id: r.id,
    metric,
    source: refId(p.source, KIND.source),
    report: refId(p.report, KIND.labreport),
    at: str(p.at) ?? r.createdAt,
    value: v.value,
    unit: v.value !== undefined ? unit : undefined,
    comparator: v.comparator,
    coded,
    text: str(p.text),
    ratio:
      numerator !== undefined && denominator !== undefined
        ? { numerator, denominator }
        : undefined,
    absentReason: str(p.absentReason),
    sourceValue: str(p.sourceValue),
    sourceUnit: str(p.sourceUnit),
    ranges,
    interpretation: Array.isArray(p.interpretation)
      ? (p.interpretation as Props[])
          .filter((i) => typeof i?.code === "string")
          .map((i) => ({ code: String(i.code), display: str(i.display) }))
      : [],
    channel: str(p.channel),
    fasting:
      typeof specimen?.fasting === "boolean" ? specimen.fasting : undefined,
    derivedFrom: derived.length ? derived : undefined,
  }
}
