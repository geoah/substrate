// @vitest-environment jsdom
/** The graph client: GET /api/v1/units read into a graph cached by server,
 * repository and revision, the reader the switch selects, and the server's
 * conversion asked for a difference. */

import { afterEach, describe, expect, it, vi } from "vitest"

import { fixtureTable } from "../unit-graph.test-fixtures"

import type { UnitTable } from "./types"
import {
  CONVERSION_KIND,
  UNIT_KIND,
  convertOnServer,
  forgetUnitGraphs,
  graphOf,
  unitGraphQueryOptions,
  unitReader,
  unitRecordChanged,
} from "./units"

const table = (revision: number): UnitTable => ({
  ...fixtureTable,
  revision,
  measures: [],
  aliases: {},
  unseeded: [],
})

afterEach(() => {
  forgetUnitGraphs()
  vi.unstubAllGlobals()
})

describe("the unit graph cache", () => {
  it("keeps one graph per server, repository and revision", () => {
    const a = graphOf(table(1), "r1.example.com", "https://a")
    expect(graphOf(table(1), "r1.example.com", "https://a")).toBe(a)
    expect(graphOf(table(2), "r1.example.com", "https://a")).not.toBe(a)
    expect(graphOf(table(1), "r2.example.com", "https://a")).not.toBe(a)
    expect(graphOf(table(1), "r1.example.com", "https://b")).not.toBe(a)
    forgetUnitGraphs()
    expect(graphOf(table(1), "r1.example.com", "https://a")).not.toBe(a)
  })

  it("keys the query by server and repository, and fetches the units route", async () => {
    const fetch = vi.fn(
      async () => new Response(JSON.stringify(table(3)), { status: 200 })
    )
    vi.stubGlobal("fetch", fetch)
    const q = unitGraphQueryOptions("r1.example.com")
    expect(q.queryKey).toEqual(["units", location.origin, "r1.example.com"])
    const g = await q.queryFn!({} as never)
    expect(g.revision).toBe(3)
    expect(String((fetch.mock.calls[0] as unknown[])[0])).toBe("/api/v1/units")
    expect(unitGraphQueryOptions("r2.example.com").queryKey).not.toEqual(
      q.queryKey
    )
  })

  it("names the records a change drops it for", () => {
    expect(unitRecordChanged(UNIT_KIND)).toBe(true)
    expect(unitRecordChanged(CONVERSION_KIND)).toBe(true)
    expect(unitRecordChanged("substrate.reamde.dev/core/kind")).toBe(false)
  })
})

describe("unitReader", () => {
  it("reads the graph where the switch hands one, and the table where not", () => {
    const graph = graphOf(table(1), "r", "s")
    const g = unitReader(graph)
    expect(g.canonical("[lb_av]")).toBe("lb")
    expect(g.convert(1, "Cel", "degF", true)).toBe(1.8)
    expect(g.convert(1, "kg", "K")).toBeUndefined()
    const t = unitReader()
    expect(t.graph).toBeUndefined()
    expect(t.canonical("lb")).toBe("[lb_av]")
    expect(t.convert(1, "Cel", "[degF]", true)).toBe(1.8)
  })

  it("asks the server to convert a difference", async () => {
    const fetch = vi.fn(
      async () =>
        new Response(
          JSON.stringify({
            value: "1.8",
            unit: "degF",
            measure: "",
            exact: true,
          }),
          { status: 200 }
        )
    )
    vi.stubGlobal("fetch", fetch)
    const c = await convertOnServer("1", "Cel", "degF", true)
    expect(c.value).toBe("1.8")
    expect(String((fetch.mock.calls[0] as unknown[])[0])).toBe(
      "/api/v1/units/convert?value=1&from=Cel&to=degF&delta=true"
    )
  })
})
