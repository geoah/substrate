// @vitest-environment jsdom
/** The quantity datatype, client-side: the stored shape, the decimal
 * grammar, the exact text an editor holds, and the title form a reader sees
 * through the shipped units (unit-graph.shipped.json). */

import { beforeEach, describe, expect, it } from "vitest"

import { SHIPPED_UNITS as units } from "./unit-graph.test-fixtures"
import {
  canonicalDecimal,
  compareDecimal,
  floatDigits,
  formatQuantityText,
  groupDigits,
  isQuantity,
  lastUnit,
  parseQuantityText,
  readQuantity,
  rememberUnit,
} from "./units"

const q = (value: unknown, unit: unknown, comparator?: unknown) =>
  comparator === undefined ? { value, unit } : { value, unit, comparator }

beforeEach(() => localStorage.clear())

describe("the shipped units", () => {
  it("prints a unit by its symbol", () => {
    expect(units.symbol("Cel")).toBe("°C")
    expect(units.symbol("ug")).toBe("μg")
    expect(units.symbol("kW.h")).toBe("kWh")
    expect(units.symbol("lb")).toBe("lb")
    expect(units.symbol("[lb_av]")).toBe("lb")
    expect(units.symbol("1")).toBe("")
    expect(units.symbol("count")).toBe("{count}")
    expect(units.symbol("steps")).toBe("steps")
    expect(units.symbol("1e3/uL")).toBe("10³/μL")
    // A unit the repository does not hold prints as written.
    expect(units.symbol("furlong/h")).toBe("furlong/h")
  })

  it("shows a value to its places, in groups of three, with its symbol", () => {
    expect(groupDigits("18194.61")).toBe("18,194.61")
    expect(groupDigits("-1234")).toBe("-1,234")
    expect(groupDigits("999")).toBe("999")
    expect(units.display({ value: "18194.61", unit: "m" })).toBe("18,194.61 m")
    expect(units.display({ value: "97.1836248", unit: "percent" })).toBe(
      "97.18 %"
    )
    expect(units.display({ value: "12008", unit: "kJ" }, 0)).toBe("12,008 kJ")
  })

  it("keeps a small value's significant digits, and a bound's every digit", () => {
    // Under one, the first two significant digits survive the cut.
    expect(units.display({ value: "0.004", unit: "mIU/L" })).toBe("0.004 mIU/L")
    expect(units.display({ value: "0.01449", unit: "ng/mL" })).toBe(
      "0.014 ng/mL"
    )
    expect(units.display({ value: "0.4567", unit: "1" })).toBe("0.46")
    expect(units.display({ value: "-0.0042", unit: "Cel" })).toBe("-0.0042 °C")
    // A comparator makes the value a bound: rounding would move it.
    expect(units.display(q("0.005", "ng/mL", "<"))).toBe("<0.005 ng/mL")
    expect(units.display(q("1234.5678", "mg/dL", ">="))).toBe(
      ">=1,234.5678 mg/dL"
    )
    expect(units.display(q("90", "s", "<"))).toBe("<90 s")
  })

  it("says a time of a minute or more in hours and minutes", () => {
    expect(units.display({ value: "282.6", unit: "min" })).toBe("4h 42m 36s")
    expect(units.display({ value: "21.08", unit: "min" })).toBe("21m 5s")
    // Under a minute, and a heart's variability in ms, read as digits.
    expect(units.display({ value: "0.35", unit: "s" })).toBe("0.35 s")
    expect(units.display({ value: "62", unit: "ms" })).toBe("62 ms")
  })

  it("labels a unit by its symbol with the id beside it where they differ", () => {
    expect(units.label("Cel")).toBe("°C (Cel)")
    expect(units.label("kg")).toBe("kg")
    expect(units.label("1")).toBe("no unit (1)")
    // A chart's menu names the symbol alone.
    expect(units.label("percent", false)).toBe("%")
    expect(units.label("1", false)).toBe("no unit")
  })

  it("offers the declared units, else the anchor's component", () => {
    expect(units.unitsFor({ unit: "kg", units: ["lb"] })).toEqual([
      { units: ["kg", "lb"] },
    ])
    const [temperature] = units.unitsFor({ unit: "Cel" })
    expect(temperature.units).toEqual(["Cel", "K", "degF"])
    // Nothing declared: every component once, no unit offered twice.
    const groups = units.unitsFor({})
    const ids = groups.flatMap((g) => g.units)
    expect(new Set(ids).size).toBe(ids.length)
    expect(ids).toContain("count")
  })

  it("maps an alias, a code and a normalized spelling to the id", () => {
    expect(units.canonical("°C")).toBe("Cel")
    expect(units.canonical("bpm")).toBe("1/min")
    expect(units.canonical("µg")).toBe("ug")
    expect(units.canonical("μg")).toBe("ug")
    expect(units.canonical("m³")).toBe("m3")
    expect(units.canonical("  kg ")).toBe("kg")
    expect(units.canonical("[lb_av]")).toBe("lb")
    expect(units.canonical("[mi_i]/h")).toBe("mph")
    expect(units.canonical("[cft_i]")).toBe("ft3")
    // What the repository does not hold is left for the server to refuse.
    expect(units.canonical("furlong")).toBe("furlong")
  })
})

describe("decimals", () => {
  it("spells the digits canonically and keeps the scale", () => {
    expect(canonicalDecimal("+007.50")).toEqual({ digits: "7.50" })
    expect(canonicalDecimal("-0.00")).toEqual({ digits: "0.00" })
    expect(canonicalDecimal("1e3").error).toMatch(/without an exponent/)
    expect(canonicalDecimal(".5").error).toMatch(/expected a decimal/)
  })

  it("holds a decimal to the engine's digit bound", () => {
    const bound = "9".repeat(1000)
    expect(canonicalDecimal(`0000${bound.slice(4)}`)).toEqual({
      digits: bound.slice(4),
    })
    expect(canonicalDecimal(bound)).toEqual({ digits: bound })
    expect(canonicalDecimal(`1.${bound}`).error).toMatch(/at most 1000 digits/)
    expect(canonicalDecimal(`${bound}9`).error).toMatch(/at most 1000 digits/)
  })

  it("orders two decimals exactly", () => {
    expect(compareDecimal("40", "39.999")).toBeGreaterThan(0)
    expect(compareDecimal("-2", "-10")).toBeGreaterThan(0)
    expect(compareDecimal("0.10", "0.1")).toBe(0)
    expect(compareDecimal("-0.5", "0")).toBeLessThan(0)
    expect(
      compareDecimal("90071992547409.93", "90071992547409.92")
    ).toBeGreaterThan(0)
  })
})

describe("the stored shape and the check", () => {
  it("admits what the substrate admits", () => {
    expect(units.check(q("72.5", "kg"))).toBeUndefined()
    expect(
      units.check(q("0.5", "ng/mL", "<"), { comparator: true })
    ).toBeUndefined()
    expect(isQuantity(q("37", "Cel"))).toBe(true)
    expect(isQuantity(q("5", "furlong"))).toBe(true)
    expect(isQuantity(q(37, "Cel"))).toBe(false)
    expect(isQuantity(q("37", ""))).toBe(false)
    expect(isQuantity(q("37", "Cel", "~"))).toBe(false)
    expect(isQuantity({ ...q("1", "kg"), scale: 2 })).toBe(false)
  })

  it("names the reason a value is wrong, in the substrate's words", () => {
    expect(units.check("72.5 kg")).toMatch(/is an object/)
    expect(units.check(q(72.5, "kg"))).toMatch(/written as a string/)
    expect(units.check(q("7e1", "kg"))).toMatch(/without an exponent/)
    expect(units.check(q("72.5", ""))).toMatch(/needs unit/)
    expect(units.check(q("5", "GJ"))).toMatch(/names no unit/)
    expect(units.check({ ...q("1", "kg"), scale: 2 })).toMatch(
      /"scale" is none of them/
    )
    expect(units.check(q("0.5", "ng/mL", "<"))).toMatch(/states values exactly/)
    expect(units.check(q("0.5", "ng/mL", "~"), { comparator: true })).toMatch(
      /one of <, <=, >=, >/
    )
  })

  it("holds a unit to the declared anchor and units", () => {
    expect(units.check(q("72", "km"), { unit: "kg" })).toBe(
      "unit: km does not convert to kg, the declaration's unit"
    )
    expect(units.check(q("37", "Cel"), { unit: "K" })).toBeUndefined()
    expect(units.check(q("1", "g"), { unit: "kg", units: ["lb"] })).toMatch(
      /not one of the declared units kg, lb/
    )
    // An alias is checked as the id the server stores it as.
    expect(
      units.check(q("37", "°C"), { unit: "Cel", units: ["degF"] })
    ).toBeUndefined()
  })

  it("bounds a value exactly, converted into the bound's unit", () => {
    const spec = {
      unit: "kg",
      minQuantity: { value: "40", unit: "kg" },
      maxQuantity: { value: "300", unit: "kg" },
    }
    expect(units.check(q("39.9", "kg"), spec)).toBe("must be >= 40 kg")
    expect(units.check(q("300.01", "kg"), spec)).toBe("must be <= 300 kg")
    expect(units.check(q("40", "kg"), spec)).toBeUndefined()
    expect(units.check(q("10", "lb"), spec)).toBe("must be >= 40 kg")
    expect(units.check(q("100", "lb"), spec)).toBeUndefined()
  })
})

describe("the text", () => {
  it("renders the title form the server renders", () => {
    expect(units.format(q("72.5", "kg"))).toBe("72.5 kg")
    expect(units.format(q("37", "Cel"))).toBe("37 °C")
    expect(units.format(q("0.5", "ng/mL", "<"))).toBe("<0.5 ng/mL")
    expect(units.format(q("8000", "steps"))).toBe("8000 steps")
    expect(units.format(q("90", "deg"))).toBe("90°")
    expect(units.format(q("0.25", "1"))).toBe("0.25")
    expect(units.format({ value: "5" })).toBe("")
  })

  it("edits as the exact text and reads back to the same value", () => {
    for (const value of [
      q("72.5", "kg"),
      q("37", "Cel"),
      q("0.5", "ng/mL", "<"),
      q("155", "lb"),
      q("0.25", "1"),
      q("-3.20", "kW.h/100km"),
    ]) {
      expect(units.parse(formatQuantityText(value))).toEqual({ value })
    }
    expect(formatQuantityText(q("37", "Cel"))).toBe("37 Cel")
    expect(formatQuantityText("37 Cel")).toBe("")
  })

  it("reads what a person types", () => {
    expect(units.parse("37 °C").value).toEqual(q("37", "Cel"))
    expect(units.parse("62 bpm").value).toEqual(q("62", "1/min"))
    expect(units.parse("155 lb").value).toEqual(q("155", "lb"))
    expect(units.parse("155 [lb_av]").value).toEqual(q("155", "lb"))
    expect(units.parse("72.5kg").value).toEqual(q("72.5", "kg"))
    expect(units.parse("1eV").value).toEqual(q("1", "eV"))
    expect(units.parse("≤ 5 mmol/L").value).toEqual(q("5", "mmol/L", "<="))
    expect(units.parse("+007.50 kg").value).toEqual(q("7.50", "kg"))
  })

  it("asks for what is missing", () => {
    const id = (s: string) => s
    expect(parseQuantityText("", id).error).toMatch(/a number and a unit/)
    expect(parseQuantityText("kg", id).error).toMatch(/number before the unit/)
    expect(parseQuantityText("72.5", id).error).toBe("choose a unit")
    expect(parseQuantityText("72,5 kg", id).error).toBe(
      "expected a number like 72.5"
    )
    expect(parseQuantityText("1e3 kg", id).error).toMatch(/without an exponent/)
    expect(parseQuantityText("72 kilo grams", id).error).toMatch(
      /without spaces/
    )
  })
})

describe("the declaration", () => {
  it("reads a bound authored with numeric digits", () => {
    expect(readQuantity({ value: 40, unit: "kg" })).toEqual(q("40", "kg"))
    expect(readQuantity({ value: "040.0", unit: "kg" })).toEqual(
      q("40.0", "kg")
    )
    expect(readQuantity(40)).toBeUndefined()
  })

  it("opens a blank value on the default, the anchor, then the last pick", () => {
    expect(units.opening({ unit: "kg", default: q("70", "lb") })).toBe("lb")
    expect(units.opening({ unit: "Cel" })).toBe("Cel")
    expect(units.opening({})).toBe("")
    units.remember("[lb_av]")
    expect(lastUnit((u) => u === "lb")).toBe("lb")
    expect(units.opening({})).toBe("lb")
    expect(units.opening({ unit: "kg" })).toBe("kg")
    expect(units.opening({ units: ["g", "lb"] })).toBe("lb")
    expect(units.opening({ units: ["g", "mg"] })).toBe("g")
    // A unit the repository does not hold is never remembered.
    units.remember("furlong")
    rememberUnit("furlong", () => false)
    expect(lastUnit((u) => u === "lb")).toBe("lb")
  })

  it("writes its example in the declared unit, quoted where YAML needs it", () => {
    expect(units.example({})).toBe('{value: "72.5", unit: kg}')
    expect(units.example({ unit: "Cel" })).toBe('{value: "72.5", unit: Cel}')
    expect(units.example({ units: ["kW.h/100km"] })).toBe(
      '{value: "72.5", unit: kW.h/100km}'
    )
    expect(units.example({ units: ["mL/kg/min"] })).toBe(
      '{value: "72.5", unit: mL/kg/min}'
    )
  })
})

describe("convert", () => {
  it("converts within one linear component", () => {
    expect(units.convert(450, "g", "kg")).toBeCloseTo(0.45, 9)
    expect(units.convert(1.2, "ug/L", "ng/mL")).toBe(1.2)
    expect(units.convert(37, "Cel", "degF")).toBeCloseTo(98.6, 9)
    // A difference of temperatures takes no offset.
    expect(units.convert(1, "Cel", "degF", { delta: true })).toBeCloseTo(1.8, 9)
  })

  it("answers without a binary float's noise", () => {
    expect(units.convert(23.4, "kg", "g")).toBe(23400)
    expect(units.convert(7.1, "kg", "g")).toBe(7100)
    expect(floatDigits(0.1 + 0.2)).toBe("0.3")
  })

  it("refuses units of two components", () => {
    expect(units.convert(92, "mg/dL", "mmol/L")).toBeUndefined()
    expect(units.convert(1, "kg", "m")).toBeUndefined()
  })
})
