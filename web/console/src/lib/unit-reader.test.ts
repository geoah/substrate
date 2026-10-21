/** The two readers: a repository's graph, and the pending one the console
 * reads while the graph loads or after it failed, which answers nothing. */

import { describe, expect, it } from "vitest"

import { UnitGraph } from "./unit-graph"
import { editedTable, SHIPPED_UNITS } from "./unit-graph.test-fixtures"
import { graphUnits, pendingUnits, unitsUnavailable } from "./unit-reader"

const edited = graphUnits(new UnitGraph(editedTable))

describe("the shipped units", () => {
  it("answer as a fresh repository's graph", () => {
    expect(SHIPPED_UNITS.status).toBe("ready")
    expect(SHIPPED_UNITS.canonical("[lb_av]")).toBe("lb")
    expect(SHIPPED_UNITS.convert(1, "kg", "lb")).toBeCloseTo(2.20462, 5)
    expect(SHIPPED_UNITS.convert(1, "Cel", "degF", { delta: true })).toBe(1.8)
    expect(SHIPPED_UNITS.symbol("sack25")).toBe("sack25")
    expect(unitsUnavailable(SHIPPED_UNITS)).toBeUndefined()
  })
})

describe("the graph reader", () => {
  it("converts through the repository's own factors", () => {
    expect(edited.status).toBe("ready")
    expect(edited.convert(1, "kg", "lb")).toBe(2)
    expect(edited.convert(1, "kg", "[lb_av]")).toBe(2)
    expect(edited.convert(2, "sack25", "kg")).toBe(50)
    expect(edited.convert(1, "Cel", "degF", { delta: true })).toBe(1.8)
    expect(edited.convert(1, "kg", "Cel")).toBeUndefined()
  })

  it("prints, offers and checks a unit only the repository holds", () => {
    expect(edited.symbol("sack25")).toBe("sack")
    expect(edited.label("sack25")).toBe("sack (sack25)")
    expect(edited.format({ value: "2", unit: "sack25" })).toBe("2 sack")
    expect(edited.display({ value: "2.345", unit: "sack25" })).toBe("2.35 sack")
    expect(edited.withSymbol("2", "sack25")).toBe("2 sack")
    expect(edited.unitsFor({ unit: "kg" })[0].units).toContain("sack25")
    expect(edited.choices("kg")).toContain("sack25")
    expect(edited.check({ value: "2", unit: "sack25" }, { unit: "kg" })).toBe(
      undefined
    )
    expect(edited.parse("2 sack25").value).toEqual({
      value: "2",
      unit: "sack25",
    })
    expect(edited.parse("155 [lb_av]").value).toEqual({
      value: "155",
      unit: "lb",
    })
    expect(edited.opening({ unit: "kg" })).toBe("kg")
    expect(edited.textExample({ unit: "kg" })).toBe("72.5 kg")
  })

  it("keeps a chart's everyday units, spelled as the graph's ids", () => {
    expect(edited.everyday(["g", "kg", "lb", "ug", "sack25"], "kg")).toEqual([
      "kg",
      "lb",
    ])
    expect(
      edited.everyday(["g", "kg", "lb", "ug", "sack25"], "kg", "sack25")
    ).toEqual(["kg", "lb", "sack25"])
  })
})

describe("the pending reader", () => {
  it("answers nothing while the graph loads", () => {
    const units = pendingUnits("loading")
    expect(units.status).toBe("loading")
    expect(units.graph).toBeUndefined()
    expect(units.convert(1, "kg", "[lb_av]")).toBeUndefined()
    expect(units.convert(5, "kg", "kg")).toBe(5)
    expect(units.symbol("Cel")).toBe("Cel")
    expect(units.unitsFor({})).toEqual([])
    expect(units.choices("kg")).toEqual(["kg"])
    expect(units.check({ value: "1", unit: "kg" })).toMatch(/still loading/)
    expect(unitsUnavailable(units)).toBe("The repository's units are loading")
  })

  it("names the failure", () => {
    const units = pendingUnits("error", new Error("boom"))
    expect(units.error?.message).toBe("boom")
    expect(units.check({ value: "1", unit: "kg" })).toBe(
      "the repository's units did not load: boom"
    )
    expect(unitsUnavailable(units)).toBe(
      "The repository's units did not load: boom"
    )
  })
})
