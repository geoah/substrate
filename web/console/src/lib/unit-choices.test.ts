/** The units a reading may be shown in: the units of its linear component
 * in the shipped seed, and no other. */

import { describe, expect, it } from "vitest"

import { SHIPPED_UNITS as units } from "./unit-graph.test-fixtures"

describe("the units a reading may be shown in", () => {
  it("narrows the output unit to the stored unit's component", () => {
    expect(units.choices("kg")).toContain("lb")
    expect(units.choices("kg")).not.toContain("J")
    expect(units.choices("kg")[0]).toBe("kg")
    expect(units.choices("1/min")).toEqual(
      expect.arrayContaining(["1/min", "1/s", "1/h"])
    )
    // A stored code reads as its unit's id.
    expect(units.choices("/min")[0]).toBe("1/min")
    expect(units.choices(undefined)).toEqual([])
    // A unit the repository does not hold converts to nothing.
    expect(units.choices("furlong")).toEqual(["furlong"])
  })

  it("offers only the units the server converts the stored one to", () => {
    expect(units.choices("count")).toEqual(["count"])
    expect(units.choices("steps")).toEqual(["steps"])
    // An assay unit converts only to itself under another prefix.
    expect([...units.choices("mIU/L")].sort()).toEqual([
      "IU/L",
      "IU/mL",
      "mIU/L",
    ])
    // A level converts to nothing but itself; rad keeps no edge to deg.
    expect(units.choices("dBm")).toEqual(["dBm"])
    expect([...units.choices("deg")].sort()).toEqual([
      "arcmin",
      "arcsec",
      "deg",
    ])
    expect(units.choices("rad")).toEqual(["rad"])
    expect(units.choices("Cel")).toEqual(["Cel", "K", "degF"])
  })
})

describe("the units a chart offers day to day", () => {
  it("offers body weight in kilograms, pounds and stones, and keeps a pick", () => {
    const mass = units.choices("kg")
    expect(mass).toContain("ug")
    expect(units.everyday(mass, "kg")).toEqual(["kg", "lb", "st"])
    // A unit picked from the address stays offered.
    expect(units.everyday(mass, "kg", "t")).toEqual(["kg", "lb", "st", "t"])
    // A stored unit read by its code finds the same list.
    expect(units.everyday(mass, "[lb_av]")).toEqual(mass)
    // A stored unit with no everyday list keeps the component's.
    expect(units.everyday(["Cel", "K", "degF"], "Cel")).toEqual([
      "Cel",
      "K",
      "degF",
    ])
  })
})
