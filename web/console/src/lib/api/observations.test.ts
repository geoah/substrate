/** The aggregate read's request builder: one `GET /api/v1/records` with the
 * filter and the aggregate as JSON query parameters, and nothing the
 * aggregate mode refuses beside them. */

import { QueryClient } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"

import { latestRead } from "@/lib/observations-reads"
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
  const kinds = [OBSERVATION, TRACE]
  const read = (
    reducers: string[],
    change: Partial<AggregateRead["aggregate"]> = {}
  ): AggregateRead => ({
    ...sleepRead,
    aggregate: { ...sleepRead.aggregate, reducers, ...change },
  })
  const answer = (n: number) => ({ ...LATEST, rows: LATEST.rows.slice(n) })

  /** A client holding each read's answer, the later ones newer. */
  function cached(...held: [AggregateRead, number][]): QueryClient {
    const client = new QueryClient()
    vi.useFakeTimers()
    try {
      held.forEach(([r, n], at) => {
        vi.setSystemTime(1000 * (at + 1))
        client.setQueryData(aggregateQueryOptions(r, kinds).queryKey, answer(n))
      })
    } finally {
      vi.useRealTimers()
    }
    return client
  }

  it("is the newest answer for the metric over the same range", () => {
    const client = cached(
      [read(["sum", "count"]), 1],
      [read(["count", "sum"], { bucket: { calendar: "day" } }), 2],
      // Another range is another chart.
      [
        read(["count"], {
          range: {
            gte: "2026-08-01T00:00:00+03:00",
            lt: "2026-09-01T00:00:00+03:00",
          },
        }),
        3,
      ]
    )
    expect(heldAnswer(client, read(["count"]), kinds)).toEqual(answer(2))
    expect(heldAnswer(client, read(["count"]), [OBSERVATION])).toBeUndefined()
    expect(
      heldAnswer(
        client,
        read(["count"], {
          range: {
            gte: "2026-07-01T00:00:00+03:00",
            lt: "2026-09-01T00:00:00+03:00",
          },
        }),
        kinds
      )
    ).toBeUndefined()
    expect(heldAnswer(client, latestRead(), kinds)).toBeUndefined()
  })

  it("holds nothing the new read would caption, group or pick otherwise", () => {
    const client = cached(
      [read(["sum", "count"]), 1],
      [read(["sum", "count"], { unit: "h" }), 2],
      [read(["sum", "count"], { groupBy: ["source", "channel"] }), 3]
    )
    // A reducer no held read asked for draws nothing.
    expect(heldAnswer(client, read(["max"]), kinds)).toBeUndefined()
    expect(heldAnswer(client, read(["sum", "max"]), kinds)).toBeUndefined()
    // Minutes are not hours: each unit holds its own answer.
    expect(heldAnswer(client, read(["sum"]), kinds)).toEqual(answer(1))
    expect(heldAnswer(client, read(["sum"], { unit: "h" }), kinds)).toEqual(
      answer(2)
    )
    expect(
      heldAnswer(client, read(["sum"], { unit: "s" }), kinds)
    ).toBeUndefined()
    // Rows grouped by code are not series of sources.
    expect(
      heldAnswer(
        client,
        read(["sum"], { groupBy: ["source", "channel"] }),
        kinds
      )
    ).toEqual(answer(3))
    expect(
      heldAnswer(client, read(["sum"], { groupBy: ["about"] }), kinds)
    ).toBeUndefined()
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
