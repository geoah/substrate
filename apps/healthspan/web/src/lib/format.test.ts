import { describe, expect, it } from "vitest"
import {
  ago,
  formatRate,
  formatShortDate,
  formatValue,
  rangeText,
  rangeWithUnit,
} from "./format"
import { factor, unitLabel, unitSymbol, withUnit } from "./units"

describe("formatShortDate", () => {
  it("prints the day and the short month", () => {
    expect(formatShortDate("2026-10-24T08:00:00Z")).toBe("24 Oct")
    expect(formatShortDate(new Date(2026, 0, 3, 12))).toBe("3 Jan")
  })
})

describe("formatValue", () => {
  it("prints as many decimals as a value needs, up to three", () => {
    expect(formatValue(3)).toBe("3")
    expect(formatValue(3.4)).toBe("3.40")
    expect(formatValue(12.25)).toBe("12.3")
    expect(formatValue(0.5, 1, "<")).toBe("<0.5")
    expect(formatValue(undefined)).toBe("-")
  })
})

describe("ranges", () => {
  it("reads both bounds, either bound, or neither", () => {
    expect(rangeText(7, 12, 1)).toBe("7.0 to 12.0")
    expect(rangeText(undefined, 3, 1)).toBe("up to 3.0")
    expect(rangeText(90, undefined, 0)).toBe("90 or more")
    expect(rangeText(undefined, undefined)).toBe("")
  })
  it("puts the unit after the value, tight for a percentage", () => {
    expect(rangeWithUnit(undefined, 3, 2, "mmol/L")).toBe("up to 3.00 mmol/L")
    expect(rangeWithUnit(7, 12, 1, "percent")).toBe("7.0 to 12.0%")
  })
})

describe("formatRate and ago", () => {
  it("prints a rate without its sign", () => {
    expect(formatRate(-0.54)).toBe("0.5%/month")
  })
  it("says how long ago in days, months or years", () => {
    const to = new Date(2026, 9, 25, 12)
    expect(ago(new Date(2026, 9, 25, 8), to)).toBe("today")
    expect(ago(new Date(2026, 9, 24, 12), to)).toBe("yesterday")
    expect(ago(new Date(2025, 7, 1), to)).toBe("14 months ago")
    expect(ago(new Date(2023, 9, 1), to)).toBe("3 years ago")
  })
})

describe("units", () => {
  it("reads a unit reference and prints its symbol", () => {
    expect(unitSymbol("substrate.reamde.dev/core/unit/umol/L")).toBe("umol/L")
    expect(unitLabel("umol/L")).toBe("µmol/L")
    expect(withUnit("54", "percent")).toBe("54%")
    expect(withUnit("3.4", "mmol/L")).toBe("3.4 mmol/L")
  })
  it("converts linearly where it knows how, and by 1 between equals", () => {
    expect(factor("g/L", "mg/dL")).toBe(100)
    expect(factor("kg", "kg")).toBe(1)
    expect(factor("mmol/L", "mg/dL")).toBeUndefined()
  })
})
