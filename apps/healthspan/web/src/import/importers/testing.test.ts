import { describe, expect, it } from "vitest"
import { convert, unitId } from "../../labs/units"
import { KINDS } from "./shared/keys"
import {
  fakeContext,
  inputFromRecord,
  planDiff,
  plannedRecord,
  sharedMetrics,
  sharedUnits,
  viewPlan,
} from "./testing"
import type { Plan } from "./types"

describe("the shared catalogues, as a test reads them", () => {
  it("holds every shared metric with its unit and LOINC codes", () => {
    const ldl = sharedMetrics().find((m) => m.id === "labs/ldl_cholesterol")
    expect(ldl).toMatchObject({
      name: "LDL cholesterol",
      unit: "mmol/L",
      category: "labs",
      precision: 2,
    })
    expect(ldl?.codes).toContainEqual({
      system: "http://loinc.org",
      code: "22748-8",
    })
    expect(sharedMetrics().some((m) => m.id === "health/body_mass")).toBe(true)
  })

  it("holds the seeded units and the packages' own, rooted by their conversions", () => {
    const g = sharedUnits()
    expect(unitId(g, "10*9/L")).toBe("1e9/L")
    expect(unitId(g, "mL/min/{1.73_m2}")).toBe("mL/min/1.73m2")
    expect(unitId(g, "mmol/L{glucose}")).toBe("mmol/L_glucose")
    expect(unitId(g, "fL")).toBe("fL")
    expect(convert(g, 250, "ng/L", "pg/mL")).toBeCloseTo(250, 9)
    expect(convert(g, 1, "kIU/L", "IU/mL")).toBeCloseTo(1, 9)
    expect(convert(g, 5, "mmol/L_glucose", "mg/dL_glucose")).toBeCloseTo(
      90.078,
      6
    )
    expect(convert(g, 1, "mg/dL", "mmol/L")).toBeUndefined()
  })

  it("builds a context of some metrics, or all", () => {
    const some = fakeContext({ metrics: ["labs/ldl_cholesterol"] })
    expect(some.metrics.map((m) => m.id)).toEqual(["labs/ldl_cholesterol"])
    const all = fakeContext({ answers: { centre: "x" } })
    expect(all.metrics.length).toBe(sharedMetrics().length)
    expect(all.answers).toEqual({ centre: "x" })
    expect(all.today.toISOString()).toBe("2026-01-01T12:00:00.000Z")
  })
})

describe("an invented input", () => {
  it("holds its files by path, text as UTF-8", async () => {
    const input = inputFromRecord("export", {
      "b/report.pdf": new Uint8Array([1, 2]),
      "a.json": '{"é":1}',
    })
    expect(input.files.map((f) => [f.path, f.type, f.size])).toEqual([
      ["a.json", "application/json", 8],
      ["b/report.pdf", "application/pdf", 2],
    ])
    expect(await input.files[0].text()).toBe('{"é":1}')
    expect(await input.files[1].bytes()).toEqual(new Uint8Array([1, 2]))
  })
})

describe("a plan against its golden fixture", () => {
  const plan: Plan = {
    importer: "numan",
    name: "export.json",
    records: [
      {
        kind: KINDS.labreport,
        namespace: "numan",
        key: "T1",
        mode: "maintain",
        properties: { lab: "Example", file: { $doc: "blob-sha256-aa" } },
        revision: 10,
      },
      {
        kind: KINDS.labresult,
        namespace: "numan",
        key: "T1/LDL",
        mode: "maintain",
        properties: { value: { value: "2.9", unit: "mmol/L" }, text: null },
        revision: 10,
      },
    ],
    documents: [
      {
        digest: "blob-sha256-aa",
        name: "Numan report 2026-01-01.json",
        mediaType: "application/json",
        bytes: new Uint8Array(3),
      },
    ],
    left: [{ what: "x", why: "y" }],
  }

  it("differs in nothing from its own view", () => {
    const golden = viewPlan(plan)
    expect(golden.documents[0]).toEqual({
      digest: "blob-sha256-aa",
      name: "Numan report 2026-01-01.json",
      mediaType: "application/json",
      size: 3,
    })
    expect(planDiff(plan, golden)).toEqual([])
    expect(planDiff(golden, golden)).toEqual([])
  })

  it("names each difference by record and property", () => {
    const golden = viewPlan(plan)
    golden.records[1].properties.value = { value: "2.90", unit: "mmol/L" }
    golden.records.push({ ...golden.records[1], key: "T1/HDL" })
    golden.documents[0].name = "other.json"
    const diff = planDiff(plan, golden)
    expect(diff).toContain(
      `${KINDS.labresult} numan T1/LDL: properties.value.value: expected "2.90", found "2.9"`
    )
    expect(diff).toContain(`${KINDS.labresult} numan T1/HDL: missing`)
    expect(diff).toContain(
      'document blob-sha256-aa.name: expected "other.json", found "Numan report 2026-01-01.json"'
    )
  })

  it("names records written out of order", () => {
    const golden = viewPlan(plan)
    golden.records.reverse()
    expect(planDiff(plan, golden)[0]).toMatch(/written at position 1/)
  })

  it("finds a planned record by kind and key", () => {
    expect(plannedRecord(plan, KINDS.labresult, "T1/LDL")?.revision).toBe(10)
    expect(
      plannedRecord(plan, KINDS.labresult, "T1/LDL", "randox")
    ).toBeUndefined()
  })
})
