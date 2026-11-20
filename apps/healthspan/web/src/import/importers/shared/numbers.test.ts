import { describe, expect, it } from "vitest"
import {
  commaDecimal,
  compareDecimals,
  isNumber,
  numberText,
  pythonFloat,
  sameDecimal,
  scaleDecimal,
  splitComparator,
  strip,
} from "./numbers"

describe("a provider's numbers", () => {
  it("splits a comparator from its digits, the Unicode signs as ASCII", () => {
    expect(splitComparator("< 5")).toEqual({ comparator: "<", value: "5" })
    expect(splitComparator("≤5.0")).toEqual({ comparator: "<=", value: "5.0" })
    expect(splitComparator(" ≥ 1.5 ")).toEqual({
      comparator: ">=",
      value: "1.5",
    })
    expect(splitComparator(">=60")).toEqual({ comparator: ">=", value: "60" })
    expect(splitComparator(" 2.90 ")).toEqual({ value: "2.90" })
    expect(splitComparator("<5 mmol/L")).toEqual({ value: "<5 mmol/L" })
    expect(splitComparator(" Negative ")).toEqual({ value: "Negative" })
  })

  it("takes a plain decimal as a number and nothing else", () => {
    for (const n of ["2.9", "-1", " 3 ", "0.000"])
      expect(isNumber(n)).toBe(true)
    for (const n of ["+1", "1e5", ".5", "1,5", "", "1.", "1.2.3", "five"])
      expect(isNumber(n)).toBe(false)
  })

  it("reads a decimal comma with points between thousands", () => {
    expect(commaDecimal("12.345")).toBe("12345")
    expect(commaDecimal("20,8")).toBe("20.8")
    expect(commaDecimal("1.234,5")).toBe("1234.5")
    expect(numberText("20,8", { comma: true })).toBe("20.8")
    expect(numberText("-", { comma: true })).toBeUndefined()
    expect(numberText(" 4.5 ")).toBe("4.5")
    expect(numberText("4,5")).toBeUndefined()
  })

  it("compares and scales as decimals, never as floats", () => {
    expect(sameDecimal("2.90", "2.9")).toBe(true)
    expect(sameDecimal("0.1", "0.10000000000000001")).toBe(false)
    expect(compareDecimals("10", "9.5")).toBe(1)
    expect(compareDecimals("-1", "0")).toBe(-1)
    expect(compareDecimals("x", "0")).toBeUndefined()
    expect(scaleDecimal("3.0", "10")).toBe("30")
    expect(scaleDecimal("0.50", "1")).toBe("0.5")
    expect(scaleDecimal("1.25", "10")).toBe("12.5")
    expect(scaleDecimal("0.1", "3")).toBe("0.3")
    expect(scaleDecimal("x", "10")).toBeUndefined()
  })

  it("prints a float as Python's str does", () => {
    const cases: [number, string][] = [
      [140, "140.0"],
      [0.1, "0.1"],
      [1e-5, "1e-05"],
      [0.0001, "0.0001"],
      [1e16, "1e+16"],
      [123.456, "123.456"],
      [-2.5, "-2.5"],
      [1.5e300, "1.5e+300"],
      [1.2345678901234567e19, "1.2345678901234567e+19"],
      [9999999999999998, "9999999999999998.0"],
      [1e22, "1e+22"],
      [5e-324, "5e-324"],
      [0, "0.0"],
    ]
    for (const [n, text] of cases) expect(pythonFloat(n)).toBe(text)
  })

  it("strips what Python strips, and keeps a byte-order mark", () => {
    expect(strip("\u001f a \u001f")).toBe("a")
    expect(strip("\u00a0x\u3000")).toBe("x")
    expect(strip("\ufeffa")).toBe("\ufeffa")
  })
})
