import { describe, expect, it } from "vitest"
import {
  convert,
  convertible,
  EMPTY_GRAPH,
  parseRational,
  parseUnitGraph,
  throughAnalyte,
} from "./units"

// The shape `GET /api/v1/units` answers.
const graph = parseUnitGraph({
  units: [
    { id: "g/L", root: "g/L", factor: "1" },
    { id: "mg/dL", root: "g/L", factor: "1/100" },
    { id: "mol/L", root: "mmol/L", factor: "1000" },
    { id: "mmol/L", root: "mmol/L", factor: "1" },
    { id: "Cel", root: "K", factor: "1", offset: "273.15" },
    { id: "degF", root: "K", factor: "5/9", offset: "45967/180" },
    { id: "K", root: "K", factor: "1" },
    { id: "percent", root: "1", factor: "0.01" },
    { id: "mmol/mol", root: "1", factor: "0.001" },
    { bogus: true },
  ],
  revision: 3,
})

describe("the unit graph", () => {
  it("reads exact digits and fractions", () => {
    expect(parseRational("1/100")).toBe(0.01)
    expect(parseRational("273.15")).toBe(273.15)
    expect(parseRational("x")).toBeUndefined()
    expect(graph.units.size).toBe(9)
  })
  it("converts within a component, offsets included", () => {
    expect(convert(graph, 250, "mg/dL", "g/L")).toBeCloseTo(2.5)
    expect(convert(graph, 37, "Cel", "degF")).toBeCloseTo(98.6, 6)
    expect(convert(graph, 3, "mmol/L", "mmol/L")).toBe(3)
  })
  it("never converts across components", () => {
    expect(convert(graph, 1, "mg/dL", "mmol/L")).toBeUndefined()
    expect(convertible(graph, "g/L", "mol/L")).toBe(false)
  })
  it("falls back to the table for a unit the graph lacks", () => {
    expect(convert(EMPTY_GRAPH, 2, "kg", "g")).toBe(2000)
    expect(convert(EMPTY_GRAPH, 2, "kg", "mmol/L")).toBeUndefined()
  })
})

describe("through an analyte", () => {
  it("converts a mass and an amount concentration by the molar mass", () => {
    const ldl = { molarMass: 386.65 }
    const mm = { formula: "molar-mass", parameters: {} }
    expect(throughAnalyte(graph, 116, "mg/dL", ldl, mm, "mmol/L")).toBeCloseTo(
      1160 / 386.65,
      9
    )
    expect(throughAnalyte(graph, 3, "mmol/L", ldl, mm, "mg/dL")).toBeCloseTo(
      (3 * 386.65) / 10,
      9
    )
    expect(throughAnalyte(graph, 3, "mmol/L", {}, mm, "mg/dL")).toBeUndefined()
  })
  it("converts HbA1c between its scales by the master equation", () => {
    const p = { slope: 10.929, intercept: 2.15 }
    expect(
      throughAnalyte(
        graph,
        6,
        "percent",
        {},
        { formula: "ngsp-to-ifcc", parameters: p },
        "mmol/mol"
      )
    ).toBeCloseTo(42.08, 2)
    expect(
      throughAnalyte(
        graph,
        42.08,
        "mmol/mol",
        {},
        { formula: "ifcc-to-ngsp", parameters: p },
        "percent"
      )
    ).toBeCloseTo(6, 3)
    expect(
      throughAnalyte(
        graph,
        6,
        "percent",
        {},
        { formula: "unknown", parameters: p },
        "mmol/mol"
      )
    ).toBeUndefined()
  })
})
