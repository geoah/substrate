import { describe, expect, it } from "vitest"
import {
  latestLine,
  markOf,
  seriesLegend,
  sourceMarkers,
  sourceOptions,
  sourcesByCount,
  type SourcedResult,
} from "./sourceMarkers"

const res = (
  source: string | undefined,
  at = "2026-01-01T12:00:00Z"
): SourcedResult => ({ source, at })

const many = (source: string, n: number) =>
  Array.from({ length: n }, () => res(source))

const names = new Map(
  ["a", "b", "c", "d", "e", "f", "g"].map((id) => [
    id,
    { name: `Lab ${id.toUpperCase()}` },
  ])
)

const byCount = (results: SourcedResult[]) =>
  sourceMarkers(sourcesByCount(results, names), names)

describe("sourceMarkers", () => {
  const results = [
    ...many("c", 3),
    ...many("a", 10),
    ...many("e", 1),
    ...many("b", 7),
    ...many("d", 2),
    ...many("f", 1),
    ...many("g", 1),
  ]
  const marks = byCount(results)
  it("orders sources by all-time count, most results first, ties by name", () => {
    expect(sourcesByCount(results, names)).toEqual([
      { id: "a", count: 10 },
      { id: "b", count: 7 },
      { id: "c", count: 3 },
      { id: "d", count: 2 },
      { id: "e", count: 1 },
      { id: "f", count: 1 },
      { id: "g", count: 1 },
    ])
  })
  it("assigns shapes by the order given", () => {
    expect([...marks.values()].map((m) => [m.id, m.marker])).toEqual([
      ["a", "circle"],
      ["b", "square"],
      ["c", "diamond"],
      ["d", "triangle"],
      ["e", "cross"],
      ["f", "cross"],
      ["g", "cross"],
    ])
    expect(marks.get("a")).toMatchObject({ name: "Lab A", count: 10 })
    const given = sourceMarkers([{ id: "g" }, { id: "a" }, { id: "g" }], names)
    expect([...given.values()].map((m) => [m.id, m.marker, m.count])).toEqual([
      ["g", "circle", 0],
      ["a", "square", 0],
    ])
  })
  it("gives a sixth source cross and still names it", () => {
    expect(marks.get("f")).toMatchObject({ marker: "cross", name: "Lab F" })
  })
  it("names a source with no record by its id", () => {
    expect(sourceMarkers([{ id: "zz" }], names).get("zz")?.name).toBe("zz")
  })
  it("gives an unknown source cross", () => {
    expect(markOf(marks, { source: "zz" }).marker).toBe("cross")
  })
  it("lists the filter options by count with the count in brackets", () => {
    expect(sourceOptions(marks).slice(0, 2)).toEqual([
      { value: "a", label: "Lab A (10)" },
      { value: "b", label: "Lab B (7)" },
    ])
  })
  it("counts results without a source under one unnamed entry", () => {
    const m = byCount([res(undefined), res("a")])
    expect(m.get("")?.name).toBe("Unknown source")
    expect(sourceOptions(m).map((o) => o.value)).toEqual(["a"])
  })
})

describe("seriesLegend", () => {
  const marks = byCount([...many("a", 10), ...many("b", 7)])
  it("lists the sources present with counts that sum to the results", () => {
    const results = [...many("b", 6), ...many("a", 3)]
    const legend = seriesLegend(results, marks)
    expect(legend.map((l) => l.label)).toEqual([
      "Lab A, 3 results",
      "Lab B, 6 results",
    ])
    expect(legend.map((l) => l.marker)).toEqual(["circle", "square"])
    expect(legend.reduce((a, l) => a + l.count, 0)).toBe(results.length)
  })
  it("counts the results in range out of all of them", () => {
    const results = [
      res("a", "2020-01-01T12:00:00Z"),
      ...many("a", 2),
      res("b", "2020-01-01T12:00:00Z"),
    ]
    const recent = (r: SourcedResult) => r.at > "2025-01-01"
    expect(seriesLegend(results, marks, recent).map((l) => l.label)).toEqual([
      "Lab A, 2 of 3 results",
      "Lab B, 0 of 1 result",
    ])
  })
  it("says one result in the singular", () => {
    expect(seriesLegend(many("b", 1), marks)[0].label).toBe("Lab B, 1 result")
  })
})

describe("latestLine", () => {
  it("names the newest result's date and source", () => {
    const line = latestLine(names, [
      res("a", "2026-01-01T12:00:00Z"),
      res("b", "2026-10-20T12:00:00Z"),
    ])
    expect(line?.text).toBe("Latest: 20 Oct 2026, Lab B")
    expect(line?.source).toBe("Lab B")
  })
  it("leaves the source out when the result names none", () => {
    expect(
      latestLine(names, [res(undefined, "2026-10-20T12:00:00Z")])?.text
    ).toBe("Latest: 20 Oct 2026")
    expect(latestLine(names, [])).toBeUndefined()
  })
})
