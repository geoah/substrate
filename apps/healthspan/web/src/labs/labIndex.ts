// The lab reads folded into a LabIndex (docs/plans/healthspan.md, Reading
// an entry). Every result belongs to a subject: the catalogue entry that
// claims its metric, else its metric alone. Results are then grouped into
// specimens, one point each:
//
// 1. A result and its derivations (`derivedFrom`, the twins `convertlab`
//    writes in the analyte's other unit form) are one specimen.
// 2. Two specimens of one subject and channel are one only by identity: the
//    same report, or the same report accession from the same lab. Two
//    results that merely agree stay two points.
// 3. The point shows the member in the display form: the original where it
//    is in that form, else the twin. A specimen with none is converted
//    through the analyte record where one converts it, and otherwise shown
//    apart, in its own unit.

import type { SubstrateRecord } from "@substrate/client"
import { CATALOGUE, unitForms, type CatalogueEntry } from "../catalogue"
import { claimMetrics, type UnitForm } from "../catalogue/resolve"
import { toAnalyte, toMetric, toReport, toResult, toSource } from "./records"
import {
  seriesKey,
  type Band,
  type LabIndex,
  type LabReport,
  type LabResult,
  type MetricInfo,
  type Series,
  type ShownVia,
  type Specimen,
  type Subject,
} from "./types"
import {
  EMPTY_GRAPH,
  convert,
  throughAnalyte,
  type Analyte,
  type UnitGraph,
} from "./units"

export interface LabRows {
  results: SubstrateRecord[]
  reports: SubstrateRecord[]
  metrics: SubstrateRecord[]
  analytes: SubstrateRecord[]
  sources: SubstrateRecord[]
}

/** Unit pairs the graph joins as plain ratios that are never one lab form:
 * HbA1c's percent and mmol/mol relate by a formula, not a factor. */
const NEVER_ONE_FORM = new Set(["percent>mmol/mol", "mmol/mol>percent"])

class Groups {
  private parent = new Map<string, string>()
  find(id: string): string {
    let root = id
    for (let up = this.parent.get(root); up; up = this.parent.get(root))
      root = up
    // Path compression, so long chains stay cheap.
    for (let at = id, up = this.parent.get(at); up; up = this.parent.get(at)) {
      this.parent.set(at, root)
      at = up
    }
    return root
  }
  union(a: string, b: string): void {
    const ra = this.find(a)
    const rb = this.find(b)
    if (ra !== rb) this.parent.set(rb, ra)
  }
}

function byTime(a: { at: string }, b: { at: string }): number {
  return Date.parse(a.at) - Date.parse(b.at)
}

/** The identity of the report a result names: its accession from its lab
 * where it has one, else the report itself. */
function reportIdentity(
  reports: ReadonlyMap<string, LabReport>,
  r: LabResult
): string | undefined {
  if (!r.report) return undefined
  const rep = reports.get(r.report)
  const lab = rep?.source ?? rep?.lab
  if (rep?.accession && lab) return `accession:${lab}:${rep.accession}`
  return `report:${r.report}`
}

/** A result's numbers moved by `f` (a conversion), its unit now `unit`;
 * undefined where its value does not convert. */
export function moved(
  r: LabResult,
  unit: string | undefined,
  f: (v: number) => number | undefined
): LabResult | undefined {
  if (r.value === undefined) return { ...r }
  const value = f(r.value)
  if (value === undefined) return undefined
  const b = (x: number | undefined) => (x === undefined ? undefined : f(x))
  const ranges: Band[] = r.ranges.map((x) => ({
    ...x,
    low: b(x.low),
    high: b(x.high),
  }))
  return { ...r, value, unit, ranges }
}

export function toLabIndex(
  rows: LabRows,
  graph: UnitGraph = EMPTY_GRAPH,
  entries: readonly CatalogueEntry[] = CATALOGUE
): LabIndex {
  const metrics = new Map<string, MetricInfo>(
    rows.metrics.map((r) => [r.id, toMetric(r)])
  )
  const sources = new Map(rows.sources.map((r) => [r.id, toSource(r)]))
  const reports = new Map(rows.reports.map((r) => [r.id, toReport(r)]))
  const analytes = new Map<string, Analyte>()
  for (const r of rows.analytes) {
    const a = toAnalyte(r)
    if (a && !analytes.has(a.metric)) analytes.set(a.metric, a)
  }

  const all = rows.results
    .map((r) => toResult(r, metrics, graph))
    .filter((r) => r.metric)
  // A result whose metric has no record still reads, under its id.
  for (const r of all)
    if (!metrics.has(r.metric))
      metrics.set(r.metric, { id: r.metric, name: r.metric, codes: [] })
  const byId = new Map(all.map((r) => [r.id, r]))
  const claims = claimMetrics(entries, [...metrics.values()])

  // 1. Derivations.
  const groups = new Groups()
  for (const r of all)
    for (const d of r.derivedFrom ?? []) if (byId.has(d)) groups.union(r.id, d)
  const linked = new Map<string, LabResult[]>()
  for (const r of all) {
    const root = groups.find(r.id)
    const list = linked.get(root) ?? []
    list.push(r)
    linked.set(root, list)
  }

  const originalsFirst = (ms: LabResult[]) => [
    ...ms.filter((m) => !m.derivedFrom),
    ...ms.filter((m) => m.derivedFrom),
  ]
  const subjectOf = (members: LabResult[]): string => {
    const ordered = originalsFirst(members)
    for (const m of ordered) {
      const e = claims.get(m.metric)
      if (e) return e.code
    }
    return ordered[0].metric
  }

  // 2. Identity, within a subject and a channel.
  interface Draft {
    subject: string
    channel?: string
    members: LabResult[]
  }
  const drafts: Draft[] = [...linked.values()].map((members) => {
    const lead = originalsFirst(members)[0]
    return { subject: subjectOf(members), channel: lead.channel, members }
  })
  const merged = new Groups()
  const firstWith = new Map<string, number>()
  drafts.forEach((d, i) => {
    for (const m of d.members) {
      const id = reportIdentity(reports, m)
      if (!id) continue
      const key = `${d.subject}\n${d.channel ?? ""}\n${id}`
      const seen = firstWith.get(key)
      if (seen === undefined) firstWith.set(key, i)
      else merged.union(String(seen), String(i))
    }
  })
  const specimensByRoot = new Map<string, Draft>()
  drafts.forEach((d, i) => {
    const root = merged.find(String(i))
    const into = specimensByRoot.get(root)
    if (into) into.members.push(...d.members)
    else specimensByRoot.set(root, { ...d, members: [...d.members] })
  })

  // The subjects, with their forms.
  const subjectMetrics = new Map<string, Set<string>>()
  for (const [metric, e] of claims) {
    const set = subjectMetrics.get(e.code) ?? new Set()
    set.add(metric)
    subjectMetrics.set(e.code, set)
  }
  for (const d of specimensByRoot.values()) {
    const set = subjectMetrics.get(d.subject) ?? new Set()
    for (const m of d.members) set.add(m.metric)
    subjectMetrics.set(d.subject, set)
  }
  const entryByCode = new Map(entries.map((e) => [e.code, e]))
  const subjects = new Map<string, Subject>()
  const subjectFor = (code: string): Subject => {
    const have = subjects.get(code)
    if (have) return have
    const ids = [...(subjectMetrics.get(code) ?? [])]
    const ms = ids
      .map((id) => metrics.get(id))
      .filter((m): m is MetricInfo => !!m)
      .sort((a, b) => a.id.localeCompare(b.id))
    // Two metrics an analyte converts between by formula are two forms,
    // whatever their units.
    const formulaPairs = new Set(NEVER_ONE_FORM)
    for (const m of ms)
      for (const c of analytes.get(m.id)?.conversions ?? []) {
        const t = metrics.get(c.target)
        if (m.unit && t?.unit && m.unit !== t.unit) {
          formulaPairs.add(`${m.unit}>${t.unit}`)
          formulaPairs.add(`${t.unit}>${m.unit}`)
        }
      }
    const oneForm = (a: string, b: string) =>
      !formulaPairs.has(`${a}>${b}`) && convert(graph, 1, a, b) !== undefined
    const entry = entryByCode.get(code)
    const own = entry ? undefined : metrics.get(code)
    const unit = entry?.displayUnit ?? own?.unit
    const forms: UnitForm[] = unit
      ? unitForms(unit, ms, oneForm)
      : [{ unit: undefined, display: true, metrics: ms }]
    const shared = entry?.metric ? metrics.get(entry.metric) : undefined
    const displayMetric =
      forms[0].metrics.find((m) => m.unit === unit) ?? forms[0].metrics[0]
    const s: Subject = {
      code,
      entry,
      name: entry?.name ?? own?.name ?? code,
      unit,
      precision: entry?.precision ?? displayMetric?.precision,
      description:
        entry?.description ?? (shared ?? displayMetric ?? own)?.description,
      category: (shared ?? displayMetric ?? ms[0])?.category,
      forms,
    }
    subjects.set(code, s)
    return s
  }

  // 3. The point each specimen shows.
  const show = (
    subject: Subject,
    members: LabResult[]
  ): { shown: LabResult; via: ShownVia } => {
    const display = subject.forms[0]
    const inDisplay = new Set(display.metrics.map((m) => m.id))
    const ordered = [...members].sort(
      (a, b) =>
        Number(!!a.derivedFrom) - Number(!!b.derivedFrom) ||
        Number(b.metric === subject.entry?.metric) -
          Number(a.metric === subject.entry?.metric) ||
        a.id.localeCompare(b.id)
    )
    for (const m of ordered) {
      if (!inDisplay.has(m.metric)) continue
      const from = m.unit ?? metrics.get(m.metric)?.unit
      const r = moved(m, subject.unit, (v) =>
        convert(graph, v, from, subject.unit)
      )
      if (r) return { shown: r, via: m.derivedFrom ? "twin" : "original" }
    }
    for (const m of ordered) {
      if (m.value === undefined) continue
      const r = throughAnalytes(m, subject)
      if (r) return { shown: r, via: "converted" }
    }
    return { shown: ordered[0], via: "apart" }
  }

  const throughAnalytes = (
    m: LabResult,
    subject: Subject
  ): LabResult | undefined => {
    const display = new Set(subject.forms[0].metrics.map((x) => x.id))
    const from = m.unit ?? metrics.get(m.metric)?.unit
    const analyte = analytes.get(m.metric)
    for (const c of analyte?.conversions ?? []) {
      if (!display.has(c.target)) continue
      const target = metrics.get(c.target)?.unit
      const r = moved(m, subject.unit, (v) => {
        const there = throughAnalyte(graph, v, from, analyte!, c, target)
        return there === undefined
          ? undefined
          : convert(graph, there, target, subject.unit)
      })
      if (r) return r
    }
    // An analyte record with a molar mass but no conversion to this form
    // (one of the owner's own metrics) still converts by it.
    const molarMass =
      analyte?.molarMass ??
      subject.forms
        .flatMap((f) => f.metrics)
        .map((x) => analytes.get(x.id)?.molarMass)
        .find((x) => !!x)
    if (!molarMass) return undefined
    return moved(m, subject.unit, (v) =>
      throughAnalyte(
        graph,
        v,
        from,
        { molarMass },
        { formula: "molar-mass", parameters: {} },
        subject.unit
      )
    )
  }

  const specimens: Specimen[] = []
  for (const [root, d] of specimensByRoot) {
    const subject = subjectFor(d.subject)
    const { shown, via } = show(subject, d.members)
    specimens.push({
      key: root,
      subject: d.subject,
      channel: d.channel,
      shown,
      via,
      members: d.members.sort((a, b) => a.id.localeCompare(b.id)),
    })
  }
  specimens.sort(
    (a, b) => byTime(a.shown, b.shown) || a.key.localeCompare(b.key)
  )

  const series = new Map<string, Series>()
  for (const s of specimens) {
    const key = seriesKey(s.subject, s.channel)
    const one = series.get(key) ?? {
      key,
      code: s.subject,
      channel: s.channel,
      results: [],
      specimens: [],
      apart: [],
    }
    if (s.via === "apart") one.apart.push(s)
    else {
      one.results.push(s.shown)
      one.specimens.push(s)
    }
    series.set(key, one)
  }

  return {
    metrics,
    sources,
    reports,
    analytes,
    subjects,
    series,
    specimens,
    results: specimens.map((s) => s.shown),
    graph,
  }
}

/** The members of a specimen in a form other than the one shown, as their
 * own results: HbA1c's percent twin beside its mmol/mol. */
export function twinsOf(index: LabIndex, s: Specimen): LabResult[] {
  const subject = index.subjects.get(s.subject)
  const display = new Set(subject?.forms[0]?.metrics.map((m) => m.id) ?? [])
  return s.members.filter((m) => m.id !== s.shown.id && !display.has(m.metric))
}
