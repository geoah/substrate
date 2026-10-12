/** The aggregate read's request builder: one `GET /api/v1/records` with the
 * filter and the aggregate as JSON query parameters, and nothing the
 * aggregate mode refuses beside them. */

import { QueryClient } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"

import { latestRead, type AggregateRead } from "@/lib/observations"
import { LATEST, OBSERVATION, TRACE } from "@/lib/observations.test-fixtures"

import {
  aggregateCurl,
  aggregatePath,
  aggregateQueryOptions,
} from "./observations"
import { recordWriteReaches } from "./records"

const sleepRead: AggregateRead = {
  filter: { kinds: [OBSERVATION] },
  aggregate: {
    metric: { ref: "ada.example.com/observations/metric/health/sleep_stage" },
    from: "raw",
    range: {
      gte: "2026-09-01T00:00:00+03:00",
      lt: "2026-10-01T00:00:00+03:00",
    },
    bucket: {
      records: { filter: { kinds: ["ada.example.com/health/sleep"] } },
    },
    groupBy: ["code"],
    reducers: ["sum"],
    sources: {
      policy: "priority",
      order: ["whoop-4-0", "garmin-forerunner-265"],
    },
    unit: "min",
  },
}

function params(path: string): URLSearchParams {
  return new URL(path, "http://test").searchParams
}

afterEach(() => vi.unstubAllGlobals())

describe("aggregatePath", () => {
  it("puts the filter and the aggregate on the records route as JSON", () => {
    const path = aggregatePath(sleepRead)
    expect(path.startsWith("/api/v1/records?")).toBe(true)
    const q = params(path)
    expect(JSON.parse(q.get("filter")!)).toEqual(sleepRead.filter)
    expect(JSON.parse(q.get("aggregate")!)).toEqual(sleepRead.aggregate)
  })

  it("sends nothing the aggregate mode refuses beside it", () => {
    const q = params(aggregatePath(sleepRead))
    for (const refused of [
      "q",
      "watch",
      "count",
      "orderBy",
      "first",
      "after",
      "offset",
      "expand",
    ]) {
      expect(q.has(refused)).toBe(false)
    }
  })

  it("leaves out an empty filter and carries a downsample on its own", () => {
    const q = params(
      aggregatePath(
        { filter: {}, aggregate: { reducers: ["last"] } },
        { points: 800, method: "m4" }
      )
    )
    expect(q.has("filter")).toBe(false)
    expect(JSON.parse(q.get("downsample")!)).toEqual({
      points: 800,
      method: "m4",
    })
  })
})

describe("aggregateQueryOptions", () => {
  it("reads the answer off the records route, keyed by the observed kinds", async () => {
    const fetchMock = vi.fn<typeof fetch>(
      async () => new Response(JSON.stringify(LATEST), { status: 200 })
    )
    vi.stubGlobal("fetch", fetchMock)
    const options = aggregateQueryOptions(latestRead(), [OBSERVATION, TRACE])
    const client = new QueryClient()
    const answer = await client.fetchQuery(options)
    expect(answer.rows).toHaveLength(6)
    const [url, init] = fetchMock.mock.calls[0]
    expect(init?.method).toBe("GET")
    expect(JSON.parse(params(String(url)).get("aggregate")!)).toEqual({
      groupBy: ["metric", "source", "about", "channel"],
      reducers: ["last"],
    })
    // A write to a trace makes this read stale; one to an unrelated kind
    // does not.
    expect(recordWriteReaches(options.queryKey, TRACE, "x")).toBe(true)
    expect(
      recordWriteReaches(options.queryKey, "ada.example.com/tasks/task", "x")
    ).toBe(false)
  })
})

describe("aggregateCurl", () => {
  it("spells the read as a curl call against the CLI's environment", () => {
    const curl = aggregateCurl(sleepRead)
    expect(curl).toContain('curl -G "$SUBSTRATE_SERVER/api/v1/records"')
    expect(curl).toContain('-H "Authorization: Bearer $SUBSTRATE_TOKEN"')
    expect(curl).toContain(
      `--data-urlencode 'aggregate=${JSON.stringify(sleepRead.aggregate)}'`
    )
  })
})
