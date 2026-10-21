/** The repository's unit graph, client-side (decision 0186): a spelling to
 * its id, exact conversion through a component's root, a difference without
 * the offset, nothing across components, and the declaration checks. */

import { describe, expect, it } from "vitest"

import { UnitGraph, compareRational, parseRational } from "./unit-graph"
import { fixtureTable } from "./unit-graph.test-fixtures"

const graph = () => new UnitGraph(fixtureTable)

describe("UnitGraph", () => {
  it("resolves an id, a code, an alias and a normalized spelling to the id", () => {
    const g = graph()
    expect(g.resolve("lb")).toBe("lb")
    expect(g.resolve("[lb_av]")).toBe("lb")
    expect(g.resolve(" pound ")).toBe("lb")
    expect(g.resolve("°C")).toBe("Cel")
    expect(g.resolve("µg")).toBe("ug")
    expect(g.resolve("stone")).toBeUndefined()
    expect(g.canonical("[degF]")).toBe("degF")
    expect(g.canonical("stone")).toBe("stone")
  })

  it("converts exactly through the component's root", () => {
    const g = graph()
    expect(g.convert(1, "lb", "kg")).toBe(0.45359237)
    expect(g.convert(23.4, "kg", "g")).toBe(23400)
    expect(g.convert(98.6, "[degF]", "Cel")).toBe(37)
    expect(g.convert(0, "Cel", "K")).toBe(273.15)
    const exact = g.convertExact(parseRational("1")!, "lb", "g")!
    expect(compareRational(exact, parseRational("453.59237")!)).toBe(0)
  })

  it("converts a difference by the factor alone", () => {
    const g = graph()
    expect(g.convert(1, "Cel", "degF", true)).toBe(1.8)
    expect(g.convert(10, "K", "Cel", true)).toBe(10)
    expect(g.convert(10, "K", "Cel")).toBe(-263.15)
  })

  it("converts nothing across components, an expression route included", () => {
    const g = graph()
    expect(g.convert(1, "kg", "K")).toBeUndefined()
    expect(g.convert(30, "mpg", "L/100km")).toBeUndefined()
    expect(g.converts("kg", "lb")).toBe(true)
    expect(g.converts("mpg", "L/100km")).toBe(false)
  })

  it("prints the graph's symbols", () => {
    const g = graph()
    expect(g.formatQuantity({ value: "37", unit: "Cel" })).toBe("37 °C")
    expect(g.formatQuantity({ value: "90", unit: "deg" })).toBe("90°")
    expect(
      g.formatQuantity({ value: "0.5", unit: "lb", comparator: "<" })
    ).toBe("<0.5 lb")
    expect(g.symbol("furlong")).toBe("furlong")
  })

  it("groups the units a declaration admits", () => {
    const g = graph()
    expect(g.unitsFor({ unit: "kg" })).toEqual([
      { heading: "gram", units: ["g", "kg", "lb", "ug"] },
    ])
    expect(g.unitsFor({ unit: "kg", units: ["[lb_av]"] })).toEqual([
      { units: ["kg", "lb"] },
    ])
    expect(g.unitsFor().map((grp) => grp.heading)).toContain("degree Celsius")
    expect(g.unitChoices("[lb_av]")).toEqual(["lb", "g", "kg", "ug"])
    expect(g.unitChoices("furlong")).toEqual(["furlong"])
  })

  it("holds a value to its declaration as the server does", () => {
    const g = graph()
    const spec = {
      unit: "kg",
      maxQuantity: { value: "200", unit: "lb" },
      minQuantity: { value: "0", unit: "kg" },
    }
    expect(
      g.checkQuantity({ value: "90.718474", unit: "kg" }, spec)
    ).toBeUndefined()
    expect(g.checkQuantity({ value: "90.7184741", unit: "kg" }, spec)).toBe(
      "must be <= 200 lb"
    )
    expect(g.checkQuantity({ value: "-1", unit: "g" }, spec)).toBe(
      "must be >= 0 kg"
    )
    expect(g.checkQuantity({ value: "1", unit: "K" }, spec)).toBe(
      "unit: K does not convert to kg, the declaration's unit"
    )
    expect(g.checkQuantity({ value: "1", unit: "stone" }, spec)).toBe(
      'unit: "stone" names no unit of this repository'
    )
    expect(
      g.checkQuantity({ value: "1", unit: "g" }, { unit: "kg", units: ["lb"] })
    ).toBe("unit: g is not one of the declared units kg, lb")
    expect(
      g.checkQuantity(
        { value: "18", unit: "degF" },
        { unit: "K", delta: true, maxQuantity: { value: "10", unit: "K" } }
      )
    ).toBeUndefined()
    expect(
      g.checkQuantity(
        { value: "10.5", unit: "Cel" },
        { unit: "K", delta: true, maxQuantity: { value: "10", unit: "K" } }
      )
    ).toBe("must be <= 10 K")
  })
})
