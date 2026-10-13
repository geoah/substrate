// @vitest-environment jsdom
/** The quantity datatype, client-side: the engine's unit table read through
 * its accessors, the substrate's own checks on the stored shape, the exact
 * text an editor holds, and the title form a reader sees. */

import { beforeEach, describe, expect, it } from "vitest"

import prints from "./units.prints.json"

import {
  MEASURES,
  canonicalDecimal,
  canonicalUnit,
  checkQuantity,
  compareDecimal,
  formatQuantity,
  formatQuantityText,
  isQuantity,
  lastUnit,
  measureLabel,
  measureOf,
  openingUnit,
  parseQuantityText,
  quantityExample,
  readQuantity,
  rememberUnit,
  unitLabel,
  unitSymbol,
  unitsFor,
} from "./units"

const q = (value: unknown, unit: unknown, comparator?: unknown) =>
  comparator === undefined ? { value, unit } : { value, unit, comparator }

beforeEach(() => localStorage.clear())

describe("the unit table", () => {
  it("lists the engine's measures with their units", () => {
    const mass = MEASURES.find((m) => m.name === "mass")
    expect(mass?.display).toBe("kg")
    expect(mass?.units).toContain("[lb_av]")
    expect(measureLabel("temperature-difference")).toBe(
      "Temperature difference"
    )
  })

  it("prints a listed code by its symbol", () => {
    expect(unitSymbol("Cel")).toBe("°C")
    expect(unitSymbol("ug")).toBe("μg")
    expect(unitSymbol("kW.h")).toBe("kWh")
    expect(unitSymbol("[lb_av]")).toBe("lb")
    expect(unitSymbol("1")).toBe("")
    expect(unitSymbol("{count}")).toBe("count")
  })

  it("prints a compound the way the engine does", () => {
    expect(unitSymbol("{beats}/min")).toBe("beats/min")
    expect(unitSymbol("{steps}")).toBe("steps")
    expect(unitSymbol("ug/min")).toBe("μg/min")
    expect(unitSymbol("mL{total}")).toBe("mL")
    expect(unitSymbol("cm3")).toBe("cm³")
    expect(unitSymbol("s-1")).toBe("s⁻¹")
    expect(unitSymbol("GJ")).toBe("GJ")
    expect(unitSymbol("uL")).toBe("μL")
    expect(unitSymbol("kcal/(kg.h)")).toBe("kcal/(kg·h)")
    expect(unitSymbol("10*3/mL")).toBe("10³/mL")
    expect(unitSymbol("10*3/uL")).toBe("10³/μL")
    expect(unitSymbol("kV.A{reactive}")).toBe("kvar")
    // A code naming an atom the table does not carry prints as itself.
    expect(unitSymbol("furlong/h")).toBe("furlong/h")
  })

  it("prints every code the engine's own print is pinned for", () => {
    for (const [code, print] of Object.entries(prints)) {
      expect([code, unitSymbol(code)]).toEqual([code, print])
    }
  })

  it("places a listed code in its measure, the primary one where two list it", () => {
    expect(measureOf("kg")).toBe("mass")
    expect(measureOf("Cel")).toBe("temperature")
    expect(measureOf("1")).toBe("fraction")
    expect(measureOf("GJ")).toBeUndefined()
    expect(measureOf("toString")).toBeUndefined()
  })

  it("labels a unit by its symbol with the code beside it where they differ", () => {
    expect(unitLabel("Cel")).toBe("°C (Cel)")
    expect(unitLabel("kg")).toBe("kg")
    expect(unitLabel("1")).toBe("no unit (1)")
    expect(unitLabel("GJ")).toBe("GJ")
  })
})

describe("unitsFor", () => {
  it("offers the declared units, else the declared measure's", () => {
    expect(unitsFor({ measures: "mass", units: ["kg", "[lb_av]"] })).toEqual([
      { measure: "mass", units: ["kg", "[lb_av]"] },
    ])
    const [temperature] = unitsFor({ measures: "temperature" })
    expect(temperature).toEqual({
      measure: "temperature",
      units: ["K", "Cel", "[degF]"],
    })
  })

  it("groups every unit under one measure when nothing is declared", () => {
    const groups = unitsFor({})
    const codes = groups.flatMap((g) => g.units)
    expect(new Set(codes).size).toBe(codes.length)
    expect(groups.find((g) => g.measure === "temperature")?.units).toContain(
      "Cel"
    )
    expect(groups.some((g) => g.measure === "temperature-difference")).toBe(
      false
    )
    expect(groups.find((g) => g.measure === "count")?.units).toEqual([
      "{count}",
    ])
  })
})

describe("canonicalUnit", () => {
  it("maps the engine's aliases and spellings to codes", () => {
    expect(canonicalUnit("°C")).toBe("Cel")
    expect(canonicalUnit("bpm")).toBe("/min")
    expect(canonicalUnit("µg")).toBe("ug")
    expect(canonicalUnit("μg")).toBe("ug")
    expect(canonicalUnit("m³")).toBe("m3")
    expect(canonicalUnit("  kg ")).toBe("kg")
  })

  it("reads a listed unit's print symbol as its code", () => {
    expect(canonicalUnit("lb")).toBe("[lb_av]")
    expect(canonicalUnit("mph")).toBe("[mi_i]/h")
    expect(canonicalUnit("ft³")).toBe("[cft_i]")
  })

  it("leaves what it cannot read for the server", () => {
    expect(canonicalUnit("GJ")).toBe("GJ")
    expect(canonicalUnit("furlong")).toBe("furlong")
  })

  it("never reads a code as another code's print symbol", () => {
    // pt is the picotonne and ft the femtotonne: a stored value in either
    // edits back to itself, never to a pint or a foot.
    expect(canonicalUnit("pt")).toBe("pt")
    expect(canonicalUnit("ft")).toBe("ft")
    expect(parseQuantityText(formatQuantityText(q("2", "pt")))).toEqual({
      value: q("2", "pt"),
    })
    expect(canonicalUnit("count")).toBe("count")
  })
})

describe("decimals", () => {
  it("spells the digits canonically and keeps the scale", () => {
    expect(canonicalDecimal("+007.50")).toEqual({ digits: "7.50" })
    expect(canonicalDecimal("-0.00")).toEqual({ digits: "0.00" })
    expect(canonicalDecimal("1e3").error).toMatch(/without an exponent/)
    expect(canonicalDecimal(".5").error).toMatch(/expected a decimal/)
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

describe("checkQuantity", () => {
  it("admits what the substrate admits", () => {
    expect(checkQuantity(q("72.5", "kg"))).toBeUndefined()
    expect(checkQuantity(q("5", "GJ"))).toBeUndefined()
    expect(
      checkQuantity(q("0.5", "ng/mL", "<"), { comparator: true })
    ).toBeUndefined()
    expect(isQuantity(q("37", "Cel"))).toBe(true)
    expect(isQuantity(q(37, "Cel"))).toBe(false)
  })

  it("names the reason a value is wrong, in the substrate's words", () => {
    expect(checkQuantity("72.5 kg")).toMatch(/is an object/)
    expect(checkQuantity(q(72.5, "kg"))).toMatch(/written as a string/)
    expect(checkQuantity(q("7e1", "kg"))).toMatch(/without an exponent/)
    expect(checkQuantity(q("72.5", ""))).toMatch(/needs unit/)
    expect(checkQuantity({ ...q("1", "kg"), scale: 2 })).toMatch(
      /"scale" is none of them/
    )
    expect(checkQuantity(q("0.5", "ng/mL", "<"))).toMatch(
      /states values exactly/
    )
    expect(checkQuantity(q("0.5", "ng/mL", "~"), { comparator: true })).toMatch(
      /one of <, <=, >=, >/
    )
  })

  it("holds a listed unit to the declared measure and units", () => {
    expect(checkQuantity(q("72", "km"), { measures: "mass" })).toBe(
      "unit: km measures length, not mass"
    )
    expect(checkQuantity(q("37", "Cel"), { measures: "temperature" })).toBe(
      undefined
    )
    expect(
      checkQuantity(q("1", "g"), { measures: "mass", units: ["kg"] })
    ).toMatch(/not one of the declared units kg/)
    // An alias is checked as the code the server stores it as.
    expect(
      checkQuantity(q("37", "°C"), { measures: "temperature", units: ["Cel"] })
    ).toBeUndefined()
  })

  it("bounds a value in the bound's own unit and leaves other units to the server", () => {
    const spec = {
      measures: "mass",
      minQuantity: { value: "40", unit: "kg" },
      maxQuantity: { value: "300", unit: "kg" },
    }
    expect(checkQuantity(q("39.9", "kg"), spec)).toBe("must be >= 40 kg")
    expect(checkQuantity(q("300.01", "kg"), spec)).toBe("must be <= 300 kg")
    expect(checkQuantity(q("40", "kg"), spec)).toBeUndefined()
    expect(checkQuantity(q("10", "[lb_av]"), spec)).toBeUndefined()
  })
})

describe("the text", () => {
  it("renders the title form the server renders", () => {
    expect(formatQuantity(q("72.5", "kg"))).toBe("72.5 kg")
    expect(formatQuantity(q("37", "Cel"))).toBe("37 °C")
    expect(formatQuantity(q("0.5", "ng/mL", "<"))).toBe("<0.5 ng/mL")
    expect(formatQuantity(q("8000", "{steps}"))).toBe("8000 steps")
    expect(formatQuantity(q("62", "{beats}/min"))).toBe("62 beats/min")
    expect(formatQuantity(q("90", "deg"))).toBe("90°")
    expect(formatQuantity(q("0.25", "1"))).toBe("0.25")
    expect(formatQuantity(q("5", "GJ"))).toBe("5 GJ")
    expect(formatQuantity({ value: "5" })).toBe("")
  })

  it("edits as the exact text and reads back to the same value", () => {
    for (const value of [
      q("72.5", "kg"),
      q("37", "Cel"),
      q("0.5", "ng/mL", "<"),
      q("155", "[lb_av]"),
      q("0.25", "1"),
      q("-3.20", "kW.h/(100.km)"),
    ]) {
      expect(parseQuantityText(formatQuantityText(value))).toEqual({ value })
    }
    expect(formatQuantityText(q("37", "Cel"))).toBe("37 Cel")
    expect(formatQuantityText("37 Cel")).toBe("")
  })

  it("reads what a person types", () => {
    expect(parseQuantityText("37 °C").value).toEqual(q("37", "Cel"))
    expect(parseQuantityText("62 bpm").value).toEqual(q("62", "/min"))
    expect(parseQuantityText("155 lb").value).toEqual(q("155", "[lb_av]"))
    expect(parseQuantityText("72.5kg").value).toEqual(q("72.5", "kg"))
    expect(parseQuantityText("1eV").value).toEqual(q("1", "eV"))
    expect(parseQuantityText("≤ 5 mmol/L").value).toEqual(
      q("5", "mmol/L", "<=")
    )
    expect(parseQuantityText("+007.50 kg").value).toEqual(q("7.50", "kg"))
  })

  it("asks for what is missing", () => {
    expect(parseQuantityText("").error).toMatch(/a number and a unit/)
    expect(parseQuantityText("kg").error).toMatch(/number before the unit/)
    expect(parseQuantityText("72.5").error).toBe("choose a unit")
    expect(parseQuantityText("72,5 kg").error).toBe(
      "expected a number like 72.5"
    )
    expect(parseQuantityText("1e3 kg").error).toMatch(/without an exponent/)
    expect(parseQuantityText("72 kilo grams").error).toMatch(/without spaces/)
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

  it("opens a blank value on the default, the display unit, then the last pick", () => {
    expect(openingUnit({ measures: "mass", default: q("70", "[lb_av]") })).toBe(
      "[lb_av]"
    )
    expect(openingUnit({ measures: "temperature" })).toBe("Cel")
    expect(openingUnit({})).toBe("")
    rememberUnit("[lb_av]")
    expect(lastUnit()).toBe("[lb_av]")
    expect(openingUnit({})).toBe("[lb_av]")
    expect(openingUnit({ measures: "mass" })).toBe("kg")
    expect(openingUnit({ units: ["g", "[lb_av]"] })).toBe("[lb_av]")
    expect(openingUnit({ units: ["g", "mg"] })).toBe("g")
    rememberUnit("furlong")
    expect(lastUnit()).toBe("[lb_av]")
  })

  it("writes its example in the declared unit, quoted where YAML needs it", () => {
    expect(quantityExample({})).toBe('{value: "72.5", unit: kg}')
    expect(quantityExample({ measures: "temperature" })).toBe(
      '{value: "72.5", unit: Cel}'
    )
    expect(quantityExample({ units: ["[lb_av]"] })).toBe(
      '{value: "72.5", unit: "[lb_av]"}'
    )
  })
})
