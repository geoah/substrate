/** The aggregate read's request builder: one `GET /api/v1/records` with the
 * filter and the aggregate as JSON query parameters, and nothing the
 * aggregate mode refuses beside them. */

import { QueryClient } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"

import { latestRead } from "@/lib/observations"
import { LATEST, OBSERVATION, TRACE } from "@/lib/observations.test-fixtures"

import {
  aggregateCurl,
  aggregatePath,
  aggregateQueryOptions,
  heldAnswer,
  type AggregateRead,
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

  it("leaves out an empty filter", () => {
    const q = params(
      aggregatePath({ filter: {}, aggregate: { reducers: ["last"] } })
    )
    expect(q.has("filter")).toBe(false)
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
      budget: { maxGroups: 2000 },
    })
    // A write to a trace makes this read stale; one to an unrelated kind
    // does not.
    expect(recordWriteReaches(options.queryKey, TRACE, "x")).toBe(true)
    expect(
      recordWriteReaches(options.queryKey, "ada.example.com/tasks/task", "x")
    ).toBe(false)
  })
})

describe("heldAnswer", () => {
  it("is the newest answer for the metric over the same range", () => {
    const client = new QueryClient()
    const kinds = [OBSERVATION, TRACE]
    const read = (reducer: string, gte = sleepRead.aggregate.range!.gte) => ({
      ...sleepRead,
      aggregate: {
        ...sleepRead.aggregate,
        reducers: [reducer],
        range: { ...sleepRead.aggregate.range!, gte },
      },
    })
    const answer = (n: number) => ({ ...LATEST, rows: LATEST.rows.slice(n) })
    vi.useFakeTimers()
    try {
      vi.setSystemTime(1000)
      client.setQueryData(
        aggregateQueryOptions(read("sum"), kinds).queryKey,
        answer(1)
      )
      vi.setSystemTime(2000)
      client.setQueryData(
        aggregateQueryOptions(read("count"), kinds).queryKey,
        answer(2)
      )
      vi.setSystemTime(3000)
      // Another range is another chart.
      client.setQueryData(
        aggregateQueryOptions(read("max", "2026-08-01T00:00:00+03:00"), kinds)
          .queryKey,
        answer(3)
      )
    } finally {
      vi.useRealTimers()
    }
    expect(heldAnswer(client, read("min"), kinds)).toEqual(answer(2))
    expect(heldAnswer(client, read("min"), [OBSERVATION])).toBeUndefined()
    expect(
      heldAnswer(client, read("min", "2026-07-01T00:00:00+03:00"), kinds)
    ).toBeUndefined()
    expect(heldAnswer(client, latestRead(), kinds)).toBeUndefined()
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
