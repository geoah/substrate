/** The alert reads: the open alerts about a set of records, and every open
 * alert with its count. Both go through the one records route with the kind
 * and the open state inside the filter, and both are refreshed by a write to
 * an alert. */

import { afterEach, describe, expect, it, vi } from "vitest"

import { ALERT_KIND } from "@/lib/alerts"

import { openAlertsAboutQueryOptions, openAlertsQueryOptions } from "./alerts"
import { recordWriteReaches } from "./records"

afterEach(() => {
  vi.unstubAllGlobals()
})

/** Stubs fetch with an empty page and answers the filter the read sent. */
function captureFilter() {
  const urls: string[] = []
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string) => {
      urls.push(String(url))
      return new Response(
        JSON.stringify({ records: [], head: 1, generation: "g", count: 0 }),
        { status: 200, headers: { "Content-Type": "application/json" } }
      )
    })
  )
  return () => {
    const q = new URL(urls[0], "http://x").searchParams
    return { filter: JSON.parse(q.get("filter") ?? "{}"), q }
  }
}

const signal = new AbortController().signal

/** Runs a query's function the way the query client would, with a signal. */
function run<T>(queryFn: unknown): Promise<T> {
  return (queryFn as (c: { signal: AbortSignal }) => Promise<T>)({ signal })
}

describe("openAlertsAboutQueryOptions", () => {
  it("asks for the open alerts whose about names one of the refs", async () => {
    const sent = captureFilter()
    const opts = openAlertsAboutQueryOptions([
      "substrate.reamde.dev/core/function/b.example.com/p/two",
      "substrate.reamde.dev/core/function/a.example.com/p/one",
    ])
    await run(opts.queryFn)
    const { filter, q } = sent()
    expect(filter).toEqual({
      kinds: [ALERT_KIND],
      properties: { state: { eq: "open" } },
      referencing: {
        refs: [
          "substrate.reamde.dev/core/function/a.example.com/p/one",
          "substrate.reamde.dev/core/function/b.example.com/p/two",
        ],
        property: "about",
      },
    })
    expect(q.get("orderBy")).toBe("updatedAt:desc")
  })

  it("is disabled with no refs, and refreshed by an alert write", () => {
    expect(openAlertsAboutQueryOptions([]).enabled).toBe(false)
    expect(
      recordWriteReaches(
        openAlertsAboutQueryOptions(["x.example.com/p/k/1"]).queryKey,
        ALERT_KIND,
        "trigger.parked/t"
      )
    ).toBe(true)
  })
})

describe("openAlertsQueryOptions", () => {
  it("asks for every open alert with the count of the open set", async () => {
    const sent = captureFilter()
    const res = await run<{ count: number }>(openAlertsQueryOptions.queryFn)
    const { filter, q } = sent()
    expect(filter).toEqual({
      kinds: [ALERT_KIND],
      properties: { state: { eq: "open" } },
    })
    expect(q.get("count")).toBe("1")
    expect(res.count).toBe(0)
    expect(
      recordWriteReaches(
        openAlertsQueryOptions.queryKey,
        ALERT_KIND,
        "trigger.parked/t"
      )
    ).toBe(true)
  })
})
