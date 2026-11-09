import { describe, expect, it } from "vitest"
import {
  claimMetrics,
  entryByCode,
  entryMetrics,
  matchEntry,
  metricForUnit,
  type MetricRecord,
} from "../catalogue"
import { demoUnitGraph } from "../demo/labs"
import { rec, ref } from "../demo/shared"
import { KIND } from "../kinds"
import { toLabIndex, twinsOf, type LabRows } from "./labIndex"
import { convertible } from "./units"

const graph = demoUnitGraph()
const LOINC = "http://loinc.org"

const metric = (id: string, unit: string, code?: string, category = "labs") =>
  rec(KIND.metric, id, {
    name: id,
    unit: ref(KIND.unit, unit),
    codes: code ? [{ system: LOINC, code }] : [],
    category,
  })

const analyte = (
  id: string,
  molarMass: number | undefined,
  target: string,
  formula = "molar-mass",
  parameters?: Record<string, string>
) =>
  rec(KIND.analyte, id, {
    metric: ref(KIND.metric, id),
    ...(molarMass ? { molarMass: String(molarMass) } : {}),
    conversions: [
      {
        target: ref(KIND.metric, target),
        formula,
        ...(parameters ? { parameters } : {}),
      },
    ],
  })

let n = 0
const result = (
  metricId: string,
  value: number,
  unit: string,
  o: {
    id?: string
    at?: string
    report?: string
    derivedFrom?: string
    channel?: string
    high?: number
  } = {}
) =>
  rec(KIND.labresult, o.id ?? `r${++n}`, {
    metric: ref(KIND.metric, metricId),
    at: o.at ?? "2026-03-01T08:00:00Z",
    value: { value: String(value), unit },
    ...(o.report ? { report: ref(KIND.labreport, o.report) } : {}),
    ...(o.derivedFrom
      ? { derivedFrom: [ref(KIND.labresult, o.derivedFrom)] }
      : {}),
    ...(o.channel ? { channel: o.channel } : {}),
    ...(o.high !== undefined
      ? { referenceRanges: [{ high: { value: String(o.high), unit } }] }
      : {}),
  })

const report = (id: string, accession?: string, lab?: string) =>
  rec(KIND.labreport, id, {
    at: "2026-03-01T08:00:00Z",
    ...(accession ? { accession } : {}),
    ...(lab ? { source: ref(KIND.source, lab) } : {}),
  })

const rows = (o: Partial<LabRows>): LabRows => ({
  results: [],
  reports: [],
  metrics: [],
  analytes: [],
  sources: [],
  ...o,
})

const LDL = metric("labs/ldl_cholesterol", "mmol/L", "22748-8")
const LDL_MASS = metric("labs/ldl_cholesterol_mass", "mg/dL", "2089-1")
const LDL_ANALYTES = [
  analyte("labs/ldl_cholesterol", 386.65, "labs/ldl_cholesterol_mass"),
  analyte("labs/ldl_cholesterol_mass", 386.65, "labs/ldl_cholesterol"),
]
const ldlSeries = (index: ReturnType<typeof toLabIndex>) =>
  index.series.get("ldl_cholesterol")

describe("resolving an entry", () => {
  const records: MetricRecord[] = [
    {
      id: "owner/ldl",
      name: "LDL",
      unit: "mmol/L",
      codes: [{ system: LOINC, code: "22748-8" }],
    },
    {
      id: "labs/ldl_cholesterol",
      name: "LDL cholesterol",
      unit: "mmol/L",
      codes: [],
    },
    {
      id: "labs/ldl_cholesterol_mass",
      name: "LDL (mass)",
      unit: "mg/dL",
      codes: [{ system: LOINC, code: "2089-1" }],
    },
    {
      id: "owner/hba1c",
      name: "HbA1c",
      unit: "percent",
      codes: [{ system: "healthkit", code: "22748-8" }],
    },
  ]
  it("takes a metric by its LOINC code, then the shared one by its id", () => {
    const claims = claimMetrics(records)
    expect(claims.get("owner/ldl")?.code).toBe("ldl_cholesterol")
    expect(claims.get("labs/ldl_cholesterol")?.code).toBe("ldl_cholesterol")
    expect(claims.get("labs/ldl_cholesterol_mass")?.code).toBe(
      "ldl_cholesterol"
    )
    // A code of another system is not a LOINC code.
    expect(claims.has("owner/hba1c")).toBe(false)
    expect(
      entryMetrics(entryByCode("ldl_cholesterol")!, records).map((m) => m.id)
    ).toEqual([
      "owner/ldl",
      "labs/ldl_cholesterol",
      "labs/ldl_cholesterol_mass",
    ])
  })
  it("matches an import's row by code, then by name or alias", () => {
    expect(matchEntry({ loinc: "2089-1" })?.code).toBe("ldl_cholesterol")
    expect(matchEntry({ name: "LDL Cholesterol" })?.code).toBe(
      "ldl_cholesterol"
    )
    expect(matchEntry({ name: "ldl-c" })?.code).toBe("ldl_cholesterol")
    expect(matchEntry({ name: "B-HbA1c" })?.code).toBe("hba1c")
    expect(matchEntry({ name: "Something else" })).toBeUndefined()
  })
  it("picks the metric an import writes a unit under", () => {
    const ldl = entryByCode("ldl_cholesterol")!
    const conv = (a: string, b: string) => convertible(graph, a, b)
    expect(metricForUnit(ldl, "mmol/L", records, conv)?.id).toBe(
      "labs/ldl_cholesterol"
    )
    expect(metricForUnit(ldl, "mg/dL", records, conv)?.id).toBe(
      "labs/ldl_cholesterol_mass"
    )
    // umol/L converts to the display form; the shared metric comes first.
    expect(metricForUnit(ldl, "umol/L", records, conv)?.id).toBe(
      "labs/ldl_cholesterol"
    )
    expect(metricForUnit(ldl, "U/L", records, conv)).toBeUndefined()
  })
})

describe("reading an entry", () => {
  it("makes two metrics of one form one series in the display unit", () => {
    const index = toLabIndex(
      rows({
        metrics: [
          metric("labs/apolipoprotein_b", "g/L", "1884-6"),
          metric("owner/apob", "mg/dL", "1884-6"),
        ],
        results: [
          result("owner/apob", 90, "mg/dL", {
            at: "2025-01-01T08:00:00Z",
            high: 130,
          }),
          result("labs/apolipoprotein_b", 0.95, "g/L", {
            at: "2026-01-01T08:00:00Z",
          }),
        ],
      }),
      graph
    )
    const s = index.series.get("apolipoprotein_b")!
    expect(s.results.map((r) => [r.value, r.unit])).toEqual([
      [0.9, "g/L"],
      [0.95, "g/L"],
    ])
    expect(s.results[0].ranges[0].high).toBeCloseTo(1.3)
    expect(index.subjects.get("apolipoprotein_b")?.forms).toHaveLength(1)
  })

  it("shows a molar original and folds its mass twin into it", () => {
    const index = toLabIndex(
      rows({
        metrics: [LDL, LDL_MASS],
        analytes: LDL_ANALYTES,
        results: [
          result("labs/ldl_cholesterol", 3, "mmol/L", { id: "o" }),
          result("labs/ldl_cholesterol_mass", 116, "mg/dL", {
            id: "t",
            derivedFrom: "o",
          }),
        ],
      }),
      graph
    )
    const s = ldlSeries(index)!
    expect(s.results).toHaveLength(1)
    expect(s.results[0]).toMatchObject({ id: "o", value: 3, unit: "mmol/L" })
    expect(s.specimens[0].via).toBe("original")
    expect(twinsOf(index, s.specimens[0]).map((r) => r.id)).toEqual(["t"])
  })

  it("shows the molar twin of a mass original", () => {
    const index = toLabIndex(
      rows({
        metrics: [LDL, LDL_MASS],
        analytes: LDL_ANALYTES,
        results: [
          result("labs/ldl_cholesterol_mass", 116, "mg/dL", { id: "o" }),
          result("labs/ldl_cholesterol", 3, "mmol/L", {
            id: "t",
            derivedFrom: "o",
          }),
        ],
      }),
      graph
    )
    const s = ldlSeries(index)!
    expect(s.results).toHaveLength(1)
    expect(s.results[0]).toMatchObject({ id: "t", value: 3 })
    expect(s.specimens[0].via).toBe("twin")
  })

  it("converts a mass result whose twin is not written yet through the analyte", () => {
    const index = toLabIndex(
      rows({
        metrics: [LDL, LDL_MASS],
        analytes: LDL_ANALYTES,
        results: [
          result("labs/ldl_cholesterol_mass", 116, "mg/dL", {
            id: "o",
            high: 130,
          }),
        ],
      }),
      graph
    )
    const s = ldlSeries(index)!
    expect(s.specimens[0].via).toBe("converted")
    // 116 mg/dL is 1160 mg/L, over 386.65 g/mol.
    expect(s.results[0].value).toBeCloseTo(1160 / 386.65, 6)
    expect(s.results[0].unit).toBe("mmol/L")
    expect(s.results[0].ranges[0].high).toBeCloseTo(1300 / 386.65, 6)
    expect(s.apart).toEqual([])
  })

  it("converts HbA1c's percent by its formula, never by the unit graph", () => {
    const index = toLabIndex(
      rows({
        metrics: [
          metric("labs/hba1c_ifcc", "mmol/mol", "59261-8"),
          metric("labs/hba1c", "percent", "4548-4"),
        ],
        results: [result("labs/hba1c", 6, "percent")],
        analytes: [
          analyte("labs/hba1c", undefined, "labs/hba1c_ifcc", "ngsp-to-ifcc", {
            slope: "10.929",
            intercept: "2.15",
          }),
        ],
      }),
      graph
    )
    const subject = index.subjects.get("hba1c")!
    expect(subject.forms.map((f) => f.metrics.map((m) => m.id))).toEqual([
      ["labs/hba1c_ifcc"],
      ["labs/hba1c"],
    ])
    const s = index.series.get("hba1c")!
    expect(s.results[0].value).toBeCloseTo((6 - 2.15) * 10.929, 6)
  })

  it("shows apart, in its own unit, what no analyte converts", () => {
    const index = toLabIndex(
      rows({
        metrics: [LDL, LDL_MASS],
        results: [
          result("labs/ldl_cholesterol", 3, "mmol/L", {
            at: "2025-01-01T08:00:00Z",
          }),
          result("labs/ldl_cholesterol_mass", 116, "mg/dL"),
        ],
      }),
      graph
    )
    const s = ldlSeries(index)!
    expect(s.results.map((r) => r.value)).toEqual([3])
    expect(s.apart).toHaveLength(1)
    expect(s.apart[0].shown).toMatchObject({ value: 116, unit: "mg/dL" })
    expect(s.apart[0].via).toBe("apart")
  })

  it("makes one point of two results of one report under two metrics", () => {
    const index = toLabIndex(
      rows({
        metrics: [LDL, LDL_MASS],
        analytes: LDL_ANALYTES,
        reports: [report("rep")],
        results: [
          result("labs/ldl_cholesterol_mass", 116, "mg/dL", { report: "rep" }),
          result("labs/ldl_cholesterol", 3, "mmol/L", { report: "rep" }),
        ],
      }),
      graph
    )
    const s = ldlSeries(index)!
    expect(s.results).toHaveLength(1)
    expect(s.results[0].value).toBe(3)
    expect(s.specimens[0].members).toHaveLength(2)
  })

  it("makes one point of one accession from one lab under two reports", () => {
    const index = toLabIndex(
      rows({
        metrics: [LDL],
        reports: [
          report("a", "X1", "lab/a"),
          report("b", "X1", "lab/a"),
          report("c", "X1", "lab/b"),
        ],
        results: [
          result("labs/ldl_cholesterol", 3, "mmol/L", { report: "a" }),
          result("labs/ldl_cholesterol", 3, "mmol/L", { report: "b" }),
          result("labs/ldl_cholesterol", 3, "mmol/L", { report: "c" }),
        ],
      }),
      graph
    )
    // The same accession from another lab is another specimen.
    expect(ldlSeries(index)!.results).toHaveLength(2)
  })

  it("keeps two equal results of two reports as two points", () => {
    const index = toLabIndex(
      rows({
        metrics: [LDL],
        reports: [report("a"), report("b")],
        results: [
          result("labs/ldl_cholesterol", 3, "mmol/L", { report: "a" }),
          result("labs/ldl_cholesterol", 3, "mmol/L", { report: "b" }),
        ],
      }),
      graph
    )
    expect(ldlSeries(index)!.results).toHaveLength(2)
  })

  it("keeps a report's regions apart, one series each", () => {
    const index = toLabIndex(
      rows({
        metrics: [
          metric("health/body_fat_fraction", "percent", "41982-0", "body"),
        ],
        reports: [report("scan")],
        results: [
          result("health/body_fat_fraction", 20, "percent", { report: "scan" }),
          result("health/body_fat_fraction", 25, "percent", {
            report: "scan",
            channel: "trunk",
          }),
          result("health/body_fat_fraction", 17, "percent", {
            report: "scan",
            channel: "legs",
          }),
        ],
      }),
      graph
    )
    expect([...index.series.keys()].sort()).toEqual([
      "health/body_fat_fraction",
      "health/body_fat_fraction#legs",
      "health/body_fat_fraction#trunk",
    ])
    expect(
      index.subjects.get("health/body_fat_fraction")?.entry
    ).toBeUndefined()
  })

  it("converts a body value written in grams into the metric's kilograms", () => {
    const index = toLabIndex(
      rows({
        metrics: [metric("health/lean_mass", "kg", undefined, "body")],
        results: [result("health/lean_mass", 59800, "g")],
      }),
      graph
    )
    expect(index.series.get("health/lean_mass")!.results[0]).toMatchObject({
      value: 59.8,
      unit: "kg",
    })
  })
})
