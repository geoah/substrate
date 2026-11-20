import { describe, expect, it } from "vitest"
import { resultParts, resultText } from "./result"
import type { LabResult } from "./types"

const base: LabResult = {
  id: "x",
  metric: "m",
  at: "2026-01-01T00:00:00Z",
  ranges: [],
  interpretation: [],
}
const r = (o: Partial<LabResult>): LabResult => ({ ...base, ...o })

describe("one result formatter", () => {
  it("prints a number at its precision with its comparator and unit", () => {
    expect(resultText(r({ value: 3.4, unit: "mmol/L" }), 2)).toBe("3.40 mmol/L")
    expect(
      resultText(r({ value: 0.5, comparator: "<", unit: "mg/L" }), 1)
    ).toBe("<0.5 mg/L")
    expect(resultParts(r({ value: 54, unit: "percent" }), 0)).toEqual({
      shape: "numeric",
      value: "54",
      unit: "%",
    })
    expect(resultText(r({ value: 5 }), 0, "umol/L")).toBe("5 µmol/L")
  })
  it("prints a coded result as the lab wrote it, else its code", () => {
    expect(resultText(r({ coded: { display: "No flag" } }))).toBe("No flag")
    expect(resultText(r({ coded: { code: "not-detected" } }))).toBe(
      "not-detected"
    )
  })
  it("prints free text in full", () => {
    const text = "High creatinine ?AKI ?CKD. Suggest repeat"
    expect(resultParts(r({ text }))).toEqual({
      shape: "text",
      value: text,
      unit: "",
    })
  })
  it("prints a ratio as a titre", () => {
    expect(resultText(r({ ratio: { numerator: 1, denominator: 80 } }))).toBe(
      "1:80"
    )
  })
  it("prints a value the importer could not normalize as received", () => {
    expect(resultText(r({ sourceValue: "12.3", sourceUnit: "mmol/mol" }))).toBe(
      "12.3 mmol/mol"
    )
  })
  it("prints the reason for an absent result, and says when there is none", () => {
    expect(resultText(r({ absentReason: "not-performed" }))).toBe(
      "Not performed"
    )
    expect(resultText(r({}))).toBe("No result")
  })
})
