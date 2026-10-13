/** The units a reading may be shown in: the codes of its measure the
 * server converts the stored unit to, and no other. */

import { describe, expect, it } from "vitest"

import { MEASURES } from "@/lib/units"

import { converts, everydayUnits, unitChoices } from "./unit-choices"

describe("the units a reading may be shown in", () => {
  it("narrows the output unit to the stored unit's measure", () => {
    expect(unitChoices("kg")).toContain("[lb_av]")
    expect(unitChoices("kg")).not.toContain("J")
    expect(unitChoices("/min")).toEqual(
      expect.arrayContaining(["/min", "/s", "/h"])
    )
    // An annotation the table does not list converts to nothing.
    expect(unitChoices("{steps}")).toEqual(["{steps}"])
    expect(unitChoices(undefined)).toEqual([])
    // A declared measure decides where the unit alone is ambiguous.
    expect(unitChoices("Cel", "temperature")).toContain("[degF]")
  })

  it("offers only the units the server converts the stored one to", () => {
    // An annotation is part of a unit: `{count}/min` counts something else.
    expect(unitChoices("/min")).toEqual(["/s", "/min", "/h", "/d", "/wk"])
    expect(unitChoices("{count}/min")).toEqual(["{count}/min"])
    expect(unitChoices("{count}")).toEqual(["{count}"])
    // An assay unit converts only to itself under another prefix, per volume
    // to per volume.
    expect(unitChoices("m[IU]/L")).toEqual(["[IU]/L", "m[IU]/L", "[IU]/mL"])
    expect(unitChoices("[IU]")).toEqual(["[IU]"])
    expect(unitChoices("[arb'U]")).toEqual(["[arb'U]"])
    // A level converts to nothing but itself; rad reaches deg only by pi.
    expect(unitChoices("dBm")).toEqual(["dBm"])
    expect(unitChoices("deg")).toEqual(["deg", "'", "''"])
    expect(unitChoices("rad")).toEqual(["rad"])
  })

  it("offers every code of a plain measure, and itself always", () => {
    // Measures whose codes carry no annotation, assay unit or pi convert
    // throughout, the way the server's table admits them.
    for (const m of MEASURES) {
      for (const code of m.units) expect(converts(m, code, code)).toBe(true)
    }
    for (const name of ["mass", "length", "volume", "duration", "energy"]) {
      const m = MEASURES.find((x) => x.name === name)!
      for (const a of m.units) {
        for (const b of m.units) expect(converts(m, a, b)).toBe(true)
      }
    }
    expect(unitChoices("Cel", "temperature-difference")).toEqual([
      "K",
      "Cel",
      "[degF]",
    ])
  })
})

describe("the units a chart offers day to day", () => {
  it("offers body weight in kilograms, pounds and stones, and keeps a pick", () => {
    const mass = unitChoices("kg", "mass")
    expect(mass).toContain("ug")
    expect(everydayUnits(mass, "kg")).toEqual(["kg", "[lb_av]", "[stone_av]"])
    // A unit picked from the address stays offered.
    expect(everydayUnits(mass, "kg", "t")).toEqual([
      "kg",
      "t",
      "[lb_av]",
      "[stone_av]",
    ])
    // A stored unit with no everyday list keeps the measure's.
    expect(everydayUnits(["Cel", "K", "[degF]"], "Cel")).toEqual([
      "Cel",
      "K",
      "[degF]",
    ])
  })
})
