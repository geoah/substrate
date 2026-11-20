import { describe, expect, it } from "vitest"
import type { CatalogueEntry, MetricRecord } from "../catalogue/types"
import { KIND } from "../kinds"
import { detectColumns, missingRoles, templateCsv } from "./columns"
import { readTable } from "./csv"
import { derivedMintedId, labSlug, recordId } from "./ids"
import { candidateMetrics, findEntry, type HeldResult } from "./match"
import { MAX_ROWS, buildPlan, resultRecord } from "./plan"
import { LAB_SOURCE, reportKey, resultKey, sameAsPlanned } from "./records"
import { matchRows, readRows, tickedAtFirst, type MatchInput } from "./resolve"
import {
  detectDayOrder,
  normaliseUnit,
  parseDate,
  parseFlag,
  parseRange,
  parseValue,
} from "./values"

const metric = (
  id: string,
  name: string,
  unit: string,
  loinc: string[] = [],
  precision = 2
): MetricRecord => ({
  id,
  name,
  unit,
  codes: loinc.map((code) => ({ system: "http://loinc.org", code })),
  category: "labs",
  precision,
})

const METRICS: MetricRecord[] = [
  metric("labs/ldl_cholesterol", "LDL cholesterol", "mmol/L", ["22748-8"]),
  metric(
    "labs/ldl_cholesterol_mass",
    "LDL cholesterol (mass)",
    "mg/dL",
    ["2089-1"],
    0
  ),
  metric("labs/vitamin_d", "Vitamin D", "nmol/L", ["68438-1"], 0),
  metric("labs/vitamin_d_mass", "Vitamin D (mass)", "ng/mL", ["62292-8"], 0),
  metric("labs/crp", "C-reactive protein", "mg/L", ["1988-5"], 1),
  metric("labs/ferritin", "Ferritin", "ug/L", ["2276-4"], 0),
]

/** Fixture entries of the catalogue's type: an entry keyed on its codes
 * and names, never a metric id of anyone's. */
const CATALOGUE: CatalogueEntry[] = [
  {
    code: "ldl",
    name: "LDL cholesterol",
    aliases: ["LDL-C", "Low density lipoprotein"],
    loinc: ["22748-8", "2089-1"],
    displayUnit: "mmol/L",
  },
  {
    code: "vitd",
    name: "Vitamin D",
    aliases: ["25-OH vitamin D", "25(OH)D"],
    loinc: ["68438-1", "62292-8"],
    displayUnit: "nmol/L",
  },
  {
    code: "crp",
    name: "C-reactive protein",
    aliases: ["CRP"],
    loinc: ["1988-5"],
    displayUnit: "mg/L",
  },
]

const FACTORS: Record<string, number> = {
  "mmol/L>mmol/L": 1,
  "ng/mL>ug/L": 1,
  "ug/L>ng/mL": 1,
  "mg/dL>mg/L": 10,
  "mg/L>mg/dL": 0.1,
  "nmol/L>nmol/L": 1,
}

const convert = (value: string, from: string, to: string) =>
  Promise.resolve(
    from === to
      ? value
      : FACTORS[`${from}>${to}`] === undefined
        ? undefined
        : String(Number(value) * FACTORS[`${from}>${to}`])
  )

function input(held: HeldResult[] = []): MatchInput {
  return {
    catalogue: CATALOGUE,
    metrics: METRICS,
    picks: new Map(),
    convert,
    held: () => Promise.resolve(held),
  }
}

describe("the file", () => {
  it("reads quoted fields, a byte order mark and both separators", () => {
    const csv =
      '﻿Date,Test,Value\r\n2026-03-12,"LDL, direct","3.4"\r\n\r\n2026-03-12,"A ""quoted"" name",1\n'
    const t = readTable(csv)
    expect(t.delimiter).toBe(",")
    expect(t.headers).toEqual(["Date", "Test", "Value"])
    expect(t.rows).toEqual([
      ["2026-03-12", "LDL, direct", "3.4"],
      ["2026-03-12", 'A "quoted" name', "1"],
    ])
    const tsv = readTable("Date\tTest\tValue\n2026-03-12\tLDL\t3.4\n")
    expect(tsv.delimiter).toBe("\t")
    expect(tsv.rows[0]).toEqual(["2026-03-12", "LDL", "3.4"])
    expect(readTable("a,b\n1,2\n", "results.tsv").delimiter).toBe("\t")
  })

  it("recognises the columns by their headers", () => {
    expect(
      detectColumns([
        "Collection Date",
        "Analyte",
        "LOINC code",
        "Result",
        "Units",
        "Reference Range",
        "Flag",
        "Laboratory",
        "Accession No.",
        "Comments",
      ])
    ).toEqual([
      "date",
      "name",
      "loinc",
      "value",
      "unit",
      "range",
      "flag",
      "lab",
      "accession",
      "ignore",
    ])
    expect(
      detectColumns(["Date", "Test", "Value (mmol/L)", "Ref low", "Ref high"])
    ).toEqual(["date", "name", "value", "low", "high"])
    expect(missingRoles(["date", "ignore"])).toEqual(["value", "name"])
  })

  it("offers a template that reads back whole", () => {
    const t = readTable(templateCsv())
    expect(detectColumns(t.headers)).not.toContain("ignore")
    expect(t.rows).toHaveLength(3)
  })
})

describe("cells", () => {
  it("reads every form of a range", () => {
    expect(parseRange("3.5-5.0")).toEqual({ low: "3.5", high: "5.0" })
    expect(parseRange("3.5 - 5.0 mmol/L")).toEqual({ low: "3.5", high: "5.0" })
    expect(parseRange("3.5 to 5.0")).toEqual({ low: "3.5", high: "5.0" })
    expect(parseRange("3.5–5.0")).toEqual({ low: "3.5", high: "5.0" })
    expect(parseRange("-1.0 - 1.0")).toEqual({ low: "-1.0", high: "1.0" })
    expect(parseRange("(0.4-4.0)")).toEqual({ low: "0.4", high: "4.0" })
    expect(parseRange("<5")).toEqual({ high: "5" })
    expect(parseRange("≤ 5.0")).toEqual({ high: "5.0" })
    expect(parseRange("up to 3")).toEqual({ high: "3" })
    expect(parseRange(">60")).toEqual({ low: "60" })
    expect(parseRange("≥ 1.0 mmol/L")).toEqual({ low: "1.0" })
    expect(parseRange("60+")).toEqual({ low: "60" })
    expect(parseRange("90 or more")).toEqual({ low: "90" })
    expect(parseRange("Negative")).toEqual({ text: "Negative" })
  })

  it("keeps a comparator on a value", () => {
    expect(parseValue("<5")).toEqual({
      kind: "number",
      value: "5",
      comparator: "<",
    })
    expect(parseValue("< 0.6")).toEqual({
      kind: "number",
      value: "0.6",
      comparator: "<",
    })
    expect(parseValue("≥90")).toEqual({
      kind: "number",
      value: "90",
      comparator: ">=",
    })
    expect(parseValue("3,4")).toEqual({ kind: "number", value: "3.4" })
    expect(parseValue("1,234.5")).toEqual({ kind: "number", value: "1234.5" })
    expect(parseValue("5.2 mmol/l")).toEqual({
      kind: "number",
      value: "5.2",
      unit: "mmol/L",
    })
    expect(parseValue("Not detected")).toEqual({
      kind: "text",
      text: "Not detected",
    })
  })

  it("reads flags and mends a unit's commonest slips", () => {
    expect(parseFlag("High")).toBe("H")
    expect(parseFlag("l")).toBe("L")
    expect(parseFlag("Critical high")).toBe("HH")
    expect(parseFlag("")).toBeUndefined()
    expect(normaliseUnit("µmol/l")).toBe("umol/L")
    expect(normaliseUnit("mg/dl")).toBe("mg/dL")
    expect(normaliseUnit("miu/l")).toBe("mIU/L")
  })

  it("reads dates in the common formats, asking where the order is open", () => {
    expect(parseDate("2026-03-12")).toMatchObject({ y: 2026, m: 3, d: 12 })
    expect(parseDate("2026-03-12T08:30:00+02:00")).toMatchObject({
      hh: 8,
      mm: 30,
      offset: 120,
    })
    expect(parseDate("12 Mar 2026")).toMatchObject({ y: 2026, m: 3, d: 12 })
    expect(parseDate("Mar 12, 2026")).toMatchObject({ y: 2026, m: 3, d: 12 })
    expect(parseDate("03/12/2026", "mdy")).toMatchObject({ m: 3, d: 12 })
    expect(parseDate("03/12/2026", "dmy")).toMatchObject({ m: 12, d: 3 })
    expect(parseDate("31/02/2026")).toBeUndefined()
    expect(detectDayOrder(["13/03/2026", "01/04/2026"])).toBe("dmy")
    expect(detectDayOrder(["03/13/2026", "04/01/2026"])).toBe("mdy")
    expect(detectDayOrder(["03/04/2026", "05/06/2026"])).toBeUndefined()
    expect(detectDayOrder(["2026-03-04"])).toBe("dmy")
  })
})

describe("matching", () => {
  it("finds an entry by code, then by name or alias in any case", () => {
    expect(findEntry({ loinc: "2089-1" }, CATALOGUE)?.code).toBe("ldl")
    expect(findEntry({ name: "ldl-c" }, CATALOGUE)?.code).toBe("ldl")
    expect(findEntry({ name: "25 (OH) D" }, CATALOGUE)?.code).toBe("vitd")
    expect(findEntry({ name: "Glucose" }, CATALOGUE)).toBeUndefined()
  })

  it("orders an entry's metrics by its codes, the row's own first", () => {
    const entry = CATALOGUE[0]
    expect(
      candidateMetrics({ loinc: "2089-1" }, entry, CATALOGUE, METRICS).map(
        (m) => m.id
      )
    ).toEqual(["labs/ldl_cholesterol_mass", "labs/ldl_cholesterol"])
    expect(
      candidateMetrics({ name: "Ferritin" }, undefined, CATALOGUE, METRICS).map(
        (m) => m.id
      )
    ).toEqual(["labs/ferritin"])
  })

  it("matches by unit, leaves out a unit that converts to none, and marks what may already be here", async () => {
    const table = readTable(
      [
        "Date,Test,Value,Unit,Range",
        "2026-03-12,LDL cholesterol,3.40,mmol/L,<3.0",
        "2026-03-12,LDL cholesterol,131,mg/dL,<116",
        "2026-03-12,Vitamin D,30,ug/dL,",
        "2026-03-12,Ferritin,80,ng/mL,30-400",
        "2026-03-12,CRP,<0.6,mg/L,<5",
        "2026-03-12,Glucose,5.1,mmol/L,",
        ",LDL cholesterol,3.4,mmol/L,",
      ].join("\n")
    )
    const roles = detectColumns(table.headers)
    const rows = readRows(table, roles, "dmy", "Example Lab")
    const matched = await matchRows(
      rows,
      input([
        {
          metric: "labs/ldl_cholesterol",
          day: "2026-03-12",
          value: 3.4,
          unit: "mmol/L",
        },
      ])
    )
    const [ldl, ldlMass, vitd, ferritin, crp, glucose, undated] = matched
    expect(ldl.metric?.id).toBe("labs/ldl_cholesterol")
    expect(ldl.range).toEqual({ high: "3.0" })
    expect(ldl.held).toBe(true)
    expect(tickedAtFirst(ldl)).toBe(false)
    expect(ldlMass.metric?.id).toBe("labs/ldl_cholesterol_mass")
    expect(ldlMass.held).toBeUndefined()
    expect(vitd.problem).toBe(
      "Its unit, ug/dL, converts to none of its metrics' (nmol/L, ng/mL)."
    )
    expect(ferritin.metric?.id).toBe("labs/ferritin")
    expect(ferritin.unit).toBe("ng/mL")
    expect(crp.metric?.id).toBe("labs/crp")
    expect(crp.comparator).toBe("<")
    expect(glucose.unmatched).toBe(true)
    expect(undated.problem).toBe("It has no date.")
  })

  it("takes the owner's pick for a name nothing knows", async () => {
    const table = readTable("Date,Test,Value,Unit\n2026-03-12,Ferr.,80,ug/L\n")
    const rows = readRows(table, detectColumns(table.headers), "dmy", "Lab")
    const picks = new Map([["ferr", "labs/ferritin"]])
    const [row] = await matchRows(rows, { ...input(), picks })
    expect(row.metric?.id).toBe("labs/ferritin")
    expect(row.problem).toBeUndefined()
  })
})

describe("the plan", () => {
  it("derives every id from the import's id and a key, in the server's minted shape", () => {
    // internal/runner/testdata/id_vectors.json
    expect(derivedMintedId("fixture", "t-1")).toBe("3c9ts84axnhyrvws6ntpphzves")
    expect(derivedMintedId("notes", "a note's text")).toBe(
      "6s6kh43nb5g2hr6d1cxq1zz3q8"
    )
    expect(derivedMintedId("x")).toBe("1de4b45ds6g1282rkwn7xtrcqn")
    const imp = "0194a1b2-0000-7000-8000-000000000001"
    // A file's records are keyed in the import's own namespace.
    expect(recordId(reportKey("1"), imp)).toBe(
      derivedMintedId(`healthspan/import/${imp}/labreport/1`)
    )
    expect(recordId(resultKey("1"), imp)).toBe(
      derivedMintedId(`healthspan/import/${imp}/labresult/1`)
    )
    expect(recordId(reportKey("1"), imp)).not.toBe(
      recordId(resultKey("1"), imp)
    )
    expect(recordId(resultKey("1"), "another")).not.toBe(
      recordId(resultKey("1"), imp)
    )
    expect(recordId(resultKey("1"), imp)).toMatch(
      /^[0-7][0-9a-hjkmnp-tv-z]{25}$/
    )
    expect(() => recordId(resultKey("1"))).toThrow()
    expect(labSlug("Ëxample  Lab, Ltd.")).toBe("example-lab-ltd")
  })

  it("groups reports by accession, else by day, one import per lab", async () => {
    const table = readTable(
      [
        "Date,Test,Value,Unit,Lab,Accession",
        "2026-03-12,LDL cholesterol,3.4,mmol/L,Lab A,X1",
        "2026-03-12,Ferritin,80,ug/L,Lab A,X1",
        "2026-04-02,Ferritin,90,ug/L,Lab A,X2",
        "2026-04-02,Ferritin,95,ug/L,Lab B,",
      ].join("\n")
    )
    const rows = await matchRows(
      readRows(table, detectColumns(table.headers), "dmy", ""),
      input()
    )
    const plan = buildPlan(rows, new Set(rows.map((r) => r.line)), {
      name: "results.csv",
      importer: "csv",
    })
    expect(plan.map((p) => [p.lab, p.reports.length, p.rows.length])).toEqual([
      ["Lab A", 2, 3],
      ["Lab B", 1, 1],
    ])
    expect(plan[0].reports[0]).toMatchObject({ key: "1", accession: "X1" })
    expect(plan[0].rows.map((r) => [r.key, r.report])).toEqual([
      ["1", "1"],
      ["2", "1"],
      ["3", "2"],
    ])
    // Each lab's plan: its source the lab's, a report before its results.
    const first = plan[0].plan
    expect(first).toMatchObject({
      importer: "csv",
      name: "results.csv (1 of 2)",
      source: LAB_SOURCE,
      documents: [],
    })
    expect(first.records.map((r) => [r.kind.split("/").pop(), r.key])).toEqual([
      ["labreport", "1"],
      ["labresult", "1"],
      ["labresult", "2"],
      ["labreport", "2"],
      ["labresult", "3"],
    ])
    expect(first.records[0].properties).toMatchObject({
      lab: "Lab A",
      accession: "X1",
      source: { $ref: LAB_SOURCE },
    })
  })

  it("splits a file of more than 2,000 rows between reports", async () => {
    const lines = ["Date,Test,Value,Unit,Accession"]
    for (let rep = 0; rep < 5; rep++)
      for (let i = 0; i < 900; i++)
        lines.push(
          `2026-03-${String(rep + 1).padStart(2, "0")},Ferritin,${i},ug/L,R${rep}`
        )
    const table = readTable(lines.join("\n"))
    const rows = await matchRows(
      readRows(table, detectColumns(table.headers), "dmy", "Lab"),
      input()
    )
    const plan = buildPlan(rows, new Set(rows.map((r) => r.line)), {
      name: "big.csv",
      importer: "csv",
    })
    expect(plan.map((p) => p.rows.length)).toEqual([1800, 1800, 900])
    expect(plan.every((p) => p.rows.length <= MAX_ROWS)).toBe(true)
    expect(plan.map((p) => p.reports.map((r) => r.accession))).toEqual([
      ["R0", "R1"],
      ["R2", "R3"],
      ["R4"],
    ])
    expect(plan[1].plan.name).toBe("big.csv (2 of 3)")
  })

  it("lists what was left out and why", async () => {
    const table = readTable(
      "Date,Test,Value,Unit\n2026-03-12,Ferritin,80,ug/L\n2026-03-12,Vitamin D,30,ug/dL\n"
    )
    const rows = await matchRows(
      readRows(table, detectColumns(table.headers), "dmy", "Lab"),
      input()
    )
    const plan = buildPlan(
      rows,
      new Set(rows.filter(tickedAtFirst).map((r) => r.line)),
      {
        name: "f.csv",
        importer: "csv",
      }
    )
    expect(plan[0].plan.left[0]).toMatchObject({
      what: "Line 3, Vitamin D",
      why: expect.stringMatching(/^Its unit, ug\/dL/),
    })
  })

  it("writes a result exactly as labs/labresult declares it", () => {
    const imp = "0194a1b2-0000-7000-8000-000000000001"
    const planned = resultRecord({
      key: "1",
      report: "1",
      name: "CRP",
      metric: "labs/crp",
      takenAt: "2026-03-12T12:00:00+02:00",
      value: "0.6",
      comparator: "<",
      unit: "mg/L",
      high: "5",
      flag: "H",
    })
    expect(planned).toMatchObject({
      kind: KIND.labresult,
      namespace: "import",
      key: "1",
      mode: "maintain",
    })
    expect(planned.properties).toEqual({
      metric: `${KIND.metric}/labs/crp`,
      source: { $ref: LAB_SOURCE },
      at: "2026-03-12T12:00:00+02:00",
      utcOffset: 120,
      form: "gauge",
      hold: "PT0S",
      value: { value: "0.6", unit: "mg/L", comparator: "<" },
      coding: { display: "CRP" },
      referenceRanges: [{ high: { value: "5", unit: "mg/L" } }],
      interpretation: [
        {
          system:
            "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation",
          code: "H",
          display: "High",
        },
      ],
      report: { $ref: reportKey("1") },
    })
    // As the runner writes it: every reference resolved, origin the import.
    const props: Record<string, unknown> = {
      ...planned.properties,
      source: `${KIND.source}/lab/x`,
      report: `${KIND.labreport}/${recordId(reportKey("1"), imp)}`,
      origin: `${KIND.import}/${imp}`,
    }
    const stored = {
      id: recordId(resultKey("1"), imp),
      kind: KIND.labresult,
      version: 1,
      createdAt: "",
      updatedAt: "",
      properties: {
        ...props,
        at: "2026-03-12T10:00:00Z",
        metric: { ref: props.metric },
        report: { ref: props.report },
        origin: { ref: props.origin },
        value: { value: "0.60", unit: "mg/L", comparator: "<" },
      },
    }
    expect(sameAsPlanned(KIND.labresult, props, stored)).toBe(true)
    expect(
      sameAsPlanned(KIND.labresult, props, {
        ...stored,
        properties: {
          ...stored.properties,
          value: { value: "0.7", unit: "mg/L" },
        },
      })
    ).toBe(false)
    const withValue = (value: Record<string, unknown>) => ({
      ...stored,
      properties: { ...stored.properties, value },
    })
    // The same number in another unit is another result: 0.6 mg/dL is ten
    // times 0.6 mg/L.
    expect(
      sameAsPlanned(
        KIND.labresult,
        props,
        withValue({ value: "0.6", unit: "mg/dL", comparator: "<" })
      )
    ).toBe(false)
    // A unit no record holds is stored under the server's spelling key, so
    // a result written `kg/m²` and read back `kg/m2` is the one written.
    const bmi = {
      ...props,
      value: { value: "24.1", unit: "kg/m\u00b2" },
    }
    expect(
      sameAsPlanned(
        KIND.labresult,
        bmi,
        withValue({ value: "24.1", unit: "kg/m2" })
      )
    ).toBe(true)
    // Two names of one unit are one unit, as the graph says.
    const key = (u: string) => (u === "milligram per liter" ? "mg/L" : u)
    expect(
      sameAsPlanned(
        KIND.labresult,
        props,
        withValue({
          value: "0.6",
          unit: "milligram per liter",
          comparator: "<",
        }),
        key
      )
    ).toBe(true)
  })
})
