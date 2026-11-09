import { describe, expect, it } from "vitest"
import { fakeContext } from "../testing"
import { analyteKey, chooseMetric, type MetricChoice } from "./metric"

const ctx = fakeContext()

const metricOf = (c: MetricChoice) => ("metric" in c ? c.metric.id : undefined)

describe("an analyte's metric", () => {
  it("is its entry's metric of the value's unit form", () => {
    const molar = chooseMetric(
      { entry: "ldl_cholesterol" },
      { numeric: true, unit: "MMOL_L" },
      ctx
    )
    expect(molar).toMatchObject({
      kind: "value",
      metric: { id: "labs/ldl_cholesterol" },
      unit: { ucum: "mmol/L", id: "mmol/L" },
    })
    const mass = chooseMetric(
      { entry: "ldl_cholesterol" },
      { numeric: true, unit: "mg/dl" },
      ctx
    )
    expect(metricOf(mass)).toBe("labs/ldl_cholesterol_mass")
    const b12 = chooseMetric(
      { entry: "vitamin_b12" },
      { numeric: true, unit: "ng/l" },
      ctx
    )
    expect(metricOf(b12)).toBe("labs/vitamin_b12_mass")
    const hb = chooseMetric(
      { entry: "haemoglobin" },
      { numeric: true, unit: "g/l" },
      ctx
    )
    expect(hb).toMatchObject({ kind: "value", unit: { ucum: "g/L" } })
    expect(metricOf(hb)).toBe("labs/haemoglobin")
  })

  it("finds the entry by LOINC code or by name", () => {
    expect(
      metricOf(
        chooseMetric(
          { loinc: "22748-8" },
          { numeric: true, unit: "mmol/L" },
          ctx
        )
      )
    ).toBe("labs/ldl_cholesterol")
    expect(
      metricOf(
        chooseMetric(
          { name: "LDL Cholesterol" },
          { numeric: true, unit: "mmol/l" },
          ctx
        )
      )
    ).toBe("labs/ldl_cholesterol")
  })

  it("is the shared metric a table names for a measure outside the catalogue", () => {
    expect(
      chooseMetric(
        { metric: "health/fat_mass" },
        { numeric: true, unit: "g" },
        ctx
      )
    ).toMatchObject({
      kind: "value",
      metric: { id: "health/fat_mass" },
      unit: { ucum: "g" },
    })
    expect(
      chooseMetric(
        { metric: "health/blood_glucose" },
        { numeric: true, unit: "mmol/L" },
        ctx
      )
    ).toMatchObject({
      kind: "value",
      metric: { id: "health/blood_glucose" },
      unit: { ucum: "mmol/L{glucose}" },
    })
  })

  it("keeps a value in a unit the repository does not know as received", () => {
    expect(
      chooseMetric(
        { entry: "ldl_cholesterol" },
        { numeric: true, unit: "furlongs/L" },
        ctx
      )
    ).toMatchObject({
      kind: "asReceived",
      metric: { id: "labs/ldl_cholesterol" },
      failure: "unknown-unit",
    })
  })

  it("keeps a value no metric of the analyte takes as received", () => {
    expect(
      chooseMetric(
        { entry: "ldl_cholesterol" },
        { numeric: true, unit: "kg" },
        ctx
      )
    ).toMatchObject({
      kind: "asReceived",
      metric: { id: "labs/ldl_cholesterol" },
      failure: "unconvertible-unit",
    })
  })

  it("writes words under the display form's metric", () => {
    expect(
      chooseMetric({ entry: "ldl_cholesterol" }, { numeric: false }, ctx)
    ).toMatchObject({ kind: "words", metric: { id: "labs/ldl_cholesterol" } })
  })

  it("is none where the repository has no metric for it", () => {
    expect(
      chooseMetric(
        { metric: "labs/not_a_metric" },
        { numeric: true, unit: "g/L" },
        ctx
      )
    ).toMatchObject({ kind: "none" })
    expect(
      chooseMetric(
        { entry: "ldl_cholesterol" },
        { numeric: true, unit: "mmol/L" },
        fakeContext({ metrics: [] })
      )
    ).toEqual({
      kind: "none",
      why: "the repository has no metric for LDL cholesterol",
    })
  })

  it("names its unit overrides by the entry's code, else the metric's", () => {
    expect(analyteKey({ entry: "fsh" })).toBe("fsh")
    expect(analyteKey({ name: "LDL cholesterol" })).toBe("ldl_cholesterol")
    expect(analyteKey({ metric: "health/blood_glucose" })).toBe("blood_glucose")
    expect(analyteKey({})).toBeUndefined()
  })
})
