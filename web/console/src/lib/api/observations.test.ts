/** The aggregate read's request builder: one `GET /api/v1/records` with the
 * filter and the aggregate as JSON query parameters, and nothing the
 * aggregate mode refuses beside them. */

import { QueryClient } from "@tanstack/react-query"
import { afterEach, describe, expect, it, vi } from "vitest"

import { latestRead } from "@/lib/observations-reads"
import {
  BODY_MASS,
  GARMIN,
  HEART,
  LATEST,
  OBSERVATION,
  STEPS,
  STEPS_METRIC,
  TRACE,
  WEIGHT,
} from "@/lib/observations.test-fixtures"

import {
  aggregateCurl,
  aggregatePath,
  aggregateQueryOptions,
  heldAnswer,
  metricAnswer,
  metricReads,
  type AggregateRead,
} from "./observations"
import { recordWriteReaches } from "./records"
import type { AggregateResult } from "./types"

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
      groupBy: ["metric", "source", "about", "channel", "statistic"],
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

  it("holds nothing read in another summary statistic", () => {
    const statistic = (eq: string): AggregateRead => ({
      ...read(["avg"]),
      filter: { ...sleepRead.filter, properties: { statistic: { eq } } },
    })
    const client = cached([statistic("avg"), 1], [read(["avg"]), 2])
    // A cycle's average is no stand-in for its highest.
    expect(heldAnswer(client, statistic("max"), kinds)).toBeUndefined()
    expect(heldAnswer(client, statistic("avg"), kinds)).toEqual(answer(1))
    // A read naming no statistic holds only another that names none.
    expect(heldAnswer(client, read(["avg"]), kinds)).toEqual(answer(2))
  })
})

/** Weight and steps over September in days, read together. */
const together: AggregateRead = {
  filter: { implements: "substrate.reamde.dev/core/observed" },
  aggregate: {
    metrics: [
      { ref: BODY_MASS, unit: "[lb_av]", reducers: ["avg", "min", "max"] },
      { ref: STEPS_METRIC, from: "prefer-raw", statistic: "sum" },
      { ref: HEART, reducers: ["max"] },
    ],
    from: "raw",
    range: sleepRead.aggregate.range,
    bucket: { calendar: "day", fallbackTimezone: "Europe/Athens" },
    reducers: ["sum"],
    sources: { policy: "priority" },
    budget: { maxBuckets: 4320, maxSamples: 3000000, maxGroups: 600 },
  },
}

/** Weight's and steps' answers as one several-metric answer. */
const togetherAnswer: AggregateResult = {
  units: { [BODY_MASS]: "[lb_av]", [STEPS_METRIC]: "{steps}", [HEART]: "/min" },
  exact: false,
  head: WEIGHT.head,
  sources: [GARMIN],
  rows: [
    ...WEIGHT.rows.map((r) => ({ ...r, group: { metric: BODY_MASS } })),
    ...STEPS.rows.map((r) => ({
      ...r,
      group: { metric: STEPS_METRIC, source: GARMIN },
    })),
  ],
}

describe("metricReads", () => {
  it("is each metric's own read of a several-metric read", () => {
    const [weight, steps, heart] = metricReads(together)
    const shared = {
      range: together.aggregate.range,
      bucket: together.aggregate.bucket,
      sources: together.aggregate.sources,
    }
    expect(weight).toEqual({
      filter: together.filter,
      aggregate: {
        ...shared,
        metric: { ref: BODY_MASS },
        unit: "[lb_av]",
        reducers: ["avg", "min", "max"],
        from: "raw",
      },
    })
    // The read's reducers where the entry names none, the entry's from and
    // statistic, which narrows the summaries as the filter's would.
    expect(steps).toEqual({
      filter: { ...together.filter, properties: { statistic: { eq: "sum" } } },
      aggregate: {
        ...shared,
        metric: { ref: STEPS_METRIC },
        reducers: ["sum"],
        from: "prefer-raw",
      },
    })
    expect(heart.aggregate.reducers).toEqual(["max"])
    expect(heart.aggregate.unit).toBeUndefined()
    // A read of one metric is its own.
    expect(metricReads(sleepRead)).toEqual([sleepRead])
  })
})

describe("metricAnswer", () => {
  it("is one metric's rows of a several-metric answer, as its own read answers", () => {
    const weight = metricAnswer(togetherAnswer, together, BODY_MASS)!
    expect(weight.metric).toBe(BODY_MASS)
    expect(weight.unit).toBe("[lb_av]")
    expect(weight.units).toBeUndefined()
    // Not grouped by metric: no row names it, and a row with no other group
    // has none.
    expect(weight.rows).toEqual(WEIGHT.rows)
    const steps = metricAnswer(togetherAnswer, together, STEPS_METRIC)!
    expect(steps.rows.map((r) => r.group)).toEqual(
      STEPS.rows.map(() => ({ source: GARMIN }))
    )
    // A metric the answer names with no row answers none.
    expect(metricAnswer(togetherAnswer, together, HEART)!.rows).toEqual([])
    // Grouped by metric, the rows keep it.
    const byMetric = {
      ...together,
      aggregate: { ...together.aggregate, groupBy: ["metric" as const] },
    }
    expect(
      metricAnswer(togetherAnswer, byMetric, BODY_MASS)!.rows[0].group
    ).toEqual({ metric: BODY_MASS })
    // An answer that does not name the metric holds none of it.
    expect(metricAnswer(togetherAnswer, together, `${HEART}x`)).toBeUndefined()
    // A read of one metric answers it whole.
    expect(metricAnswer(WEIGHT, sleepRead, BODY_MASS)).toBe(WEIGHT)
  })
})

describe("heldAnswer across several metrics", () => {
  const kinds = [OBSERVATION, TRACE]
  const [weightAlone, stepsAlone] = metricReads(together)

  it("holds a metric's rows of a several-metric answer for its own read", () => {
    const client = new QueryClient()
    client.setQueryData(
      aggregateQueryOptions(together, kinds).queryKey,
      togetherAnswer
    )
    const held = heldAnswer(
      client,
      {
        ...weightAlone,
        aggregate: { ...weightAlone.aggregate, reducers: ["avg"] },
      },
      kinds
    )
    expect(held?.rows).toEqual(WEIGHT.rows)
    // Kilograms are not pounds.
    expect(
      heldAnswer(
        client,
        {
          ...weightAlone,
          aggregate: { ...weightAlone.aggregate, unit: "kg" },
        },
        kinds
      )
    ).toBeUndefined()
  })

  it("holds a several-metric read's metrics from their own answers", () => {
    const client = new QueryClient()
    client.setQueryData(aggregateQueryOptions(weightAlone, kinds).queryKey, {
      ...WEIGHT,
      unit: "[lb_av]",
    })
    // Steps read for a count alone: nothing held for its sum.
    client.setQueryData(
      aggregateQueryOptions(
        {
          ...stepsAlone,
          aggregate: { ...stepsAlone.aggregate, reducers: ["count"] },
        },
        kinds
      ).queryKey,
      STEPS
    )
    const held = heldAnswer(client, together, kinds)!
    expect(held.units).toEqual({ [BODY_MASS]: "[lb_av]" })
    expect(metricAnswer(held, together, BODY_MASS)?.rows).toEqual(WEIGHT.rows)
    expect(metricAnswer(held, together, STEPS_METRIC)).toBeUndefined()
    expect(heldAnswer(new QueryClient(), together, kinds)).toBeUndefined()
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
