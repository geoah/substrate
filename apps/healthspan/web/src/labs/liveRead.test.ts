import { SubstrateClient } from "@substrate/client"
import { FakeSubstrate } from "@substrate/testing"
import { afterEach, describe, expect, it, vi } from "vitest"
import { readLabs } from "../data/labs"
import { demoLabRows, demoUnitGraph } from "../demo/labs"
import { toLabIndex } from "./labIndex"

const today = new Date("2026-10-25T00:00:00")

function seeded(): FakeSubstrate {
  const fake = new FakeSubstrate()
  const rows = demoLabRows(today)
  for (const r of [
    ...rows.metrics,
    ...rows.sources,
    ...rows.analytes,
    ...rows.reports,
    ...rows.results,
  ])
    fake.seed(r.kind, r.id, r.properties)
  return fake
}

/** The graph `GET /api/v1/units` answers, in the server's shape. */
function unitsAnswer() {
  return {
    units: [...demoUnitGraph().units.values()].map((u) => ({
      id: u.id,
      name: u.id,
      symbol: u.id,
      aliases: [],
      root: u.root,
      factor: String(u.factor),
    })),
    conversions: [],
    revision: 1,
  }
}

afterEach(() => vi.unstubAllGlobals())

describe("the live labs read", () => {
  it("lists every lab kind and the unit graph into the same index the demo makes", async () => {
    const fake = seeded()
    // The client fetches by URL string.
    vi.stubGlobal("fetch", (input: string, init?: RequestInit) => {
      const url = new URL(input)
      if (url.pathname === "/api/v1/units")
        return Promise.resolve(Response.json(unitsAnswer()))
      return fake.fetch(input, init)
    })
    const client = new SubstrateClient(fake.origin, "tok")
    const index = await readLabs(client, new AbortController().signal)
    const local = toLabIndex(demoLabRows(today), demoUnitGraph())
    expect(index.results.length).toBe(local.results.length)
    expect([...index.series.keys()].sort()).toEqual(
      [...local.series.keys()].sort()
    )
    expect(
      index.series.get("vitamin_b12")?.results.map((r) => r.value)
    ).toEqual(local.series.get("vitamin_b12")?.results.map((r) => r.value))
    // Every list read pages 500 at a time.
    const lists = fake.requests.filter((r) => r.path === "/api/v1/records")
    expect(lists.every((r) => r.query.get("first") === "500")).toBe(true)
  })

  it("reads on without the units route, through the built-in table", async () => {
    const fake = seeded()
    vi.stubGlobal("fetch", fake.fetch)
    const client = new SubstrateClient(fake.origin, "tok")
    const index = await readLabs(client, new AbortController().signal)
    // kg and g convert by the table: the scans' lean mass still reads.
    expect(
      index.series.get("health/lean_mass")?.results.map((r) => r.unit)
    ).toEqual(["kg", "kg", "kg"])
    expect(index.series.get("ldl_cholesterol")?.results.length).toBe(9)
  })
})
