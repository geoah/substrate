/** The aggregate reads the observations pages issue: discovery's and each
 * chart's, and the budgets they are held to. */

import { describe, expect, it } from "vitest"

import { MAX_BUCKETS, MAX_BUCKETS_CAP } from "@/lib/observations-time"
import {
  discoveryChunks,
  facetRead,
  laneRead,
  latestRead,
  MAX_GROUPS,
  MAX_GROUPS_CAP,
  MAX_METRICS,
  MAX_SAMPLES,
  MAX_SAMPLES_CAP,
  sharedReads,
  type LaneSpec,
} from "@/lib/observations-reads"
import {
  BODY_MASS,
  GARMIN,
  HEART,
  MIA,
  METRIC,
  SOURCE,
  STEPS_METRIC,
} from "@/lib/observations.test-fixtures"

const serverBudgets = import.meta.glob<string>(
  "../../../../internal/substrate/aggregate.go",
  { query: "?raw", import: "default", eager: true }
)

const OBSERVED = "substrate.reamde.dev/core/observed"

const spec = (over: Partial<LaneSpec> = {}): LaneSpec => ({
  metricPath: `${METRIC}/health/heart_rate`,
  shape: "gauge",
  reducer: "avg",
  from: "raw",
  range: {
    start: new Date(2026, 8, 1).getTime(),
    end: new Date(2026, 9, 1).getTime(),
  },
  bucket: { calendar: "day", timezone: "Europe/Athens" },
  group: "metric",
  sources: [],
  sourceOrder: ["garmin-forerunner-265"],
  about: [],
  ...over,
})

describe("discovery's reads", () => {
  it("asks for the newest value of every stream, no range and no bucket", () => {
    // At the group cap: one metric of a few hundred rooms is past the
    // default 200 groups.
    expect(latestRead()).toEqual({
      filter: { implements: OBSERVED },
      aggregate: {
        groupBy: ["metric", "source", "about", "channel", "statistic"],
        reducers: ["last"],
        budget: { maxGroups: 2000 },
      },
    })
  })

  it("narrows on the server and reads its metrics in bounded batches", () => {
    expect(latestRead({ metrics: [HEART], sources: [GARMIN] }).filter).toEqual({
      implements: OBSERVED,
      properties: { metric: { in: [HEART] }, source: { in: [GARMIN] } },
    })
    const ids = (n: number) => Array.from({ length: n }, (_, i) => `m${i}`)
    expect(discoveryChunks(ids(20)).map((c) => c.length)).toEqual([8, 8, 4])
    const many = discoveryChunks(ids(1000))
    expect(many.length).toBeLessThanOrEqual(32)
    expect(many.flat()).toEqual(ids(1000))
    expect(discoveryChunks([])).toEqual([])
  })

  it("reads a facet's values under the other choices, never its own", () => {
    const narrow = { metrics: [HEART], sources: [GARMIN], abouts: [MIA] }
    expect(facetRead("source", narrow)).toEqual({
      filter: {
        implements: OBSERVED,
        properties: { metric: { in: [HEART] }, about: { in: [MIA] } },
      },
      aggregate: {
        groupBy: ["source"],
        reducers: ["last"],
        budget: { maxGroups: 2000 },
      },
    })
    expect(facetRead("about", narrow).filter?.properties).toEqual({
      metric: { in: [HEART] },
      source: { in: [GARMIN] },
    })
    expect(facetRead("about", narrow).aggregate.groupBy).toEqual(["about"])
  })
})

describe("one chart's request", () => {
  it("asks for the reducer and, on a level, the band around it", () => {
    const read = laneRead(spec())
    expect(read.filter).toEqual({ implements: OBSERVED })
    expect(read.aggregate).toMatchObject({
      metric: { ref: `${METRIC}/health/heart_rate` },
      from: "raw",
      bucket: { calendar: "day", timezone: "Europe/Athens" },
      reducers: ["avg", "min", "max"],
    })
    expect(read.aggregate.range?.gte).toMatch(/^2026-09-01T00:00:00/)
    expect(read.aggregate.groupBy).toBeUndefined()
    expect(read.aggregate.sources).toBeUndefined()
    expect(laneRead(spec({ reducer: "max" })).aggregate.reducers).toEqual([
      "max",
      "min",
    ])
  })

  it("groups by the reader's choice and resolves overlap by priority", () => {
    const bySource = laneRead(
      spec({ group: "source", sourceOrder: ["a", "b"] })
    ).aggregate
    // A source's channels stay apart: a WHOOP cycle beside its workouts.
    expect(bySource.groupBy).toEqual(["source", "channel"])
    expect(bySource.sources).toBeUndefined()
    // Unranked by the reader, the server's own priorities order them.
    const merged = laneRead(spec({ sourceOrder: ["a", "b"] })).aggregate
    expect(merged.sources).toEqual({ policy: "priority" })
    const ranked = laneRead(
      spec({ sourceOrder: ["a", "b"], ranked: true })
    ).aggregate
    expect(ranked.sources).toEqual({ policy: "priority", order: ["a", "b"] })
  })

  it("narrows to the chosen channels", () => {
    const read = laneRead(spec({ channels: ["workout"] }))
    expect(read.filter?.properties).toEqual({ channel: { in: ["workout"] } })
    expect(laneRead(spec({ channels: [] })).filter?.properties).toBeUndefined()
  })

  it("narrows to the chosen sources and things by path", () => {
    const read = laneRead(
      spec({
        sources: [`${SOURCE}/garmin-forerunner-265`],
        about: ["ada.example.com/people/person/mia"],
      })
    )
    expect(read.filter?.properties).toEqual({
      source: { in: [`${SOURCE}/garmin-forerunner-265`] },
      about: { in: ["ada.example.com/people/person/mia"] },
    })
  })

  it("groups a coded metric by code and asks timeInState bare", () => {
    const read = laneRead(
      spec({ shape: "coded", reducer: "timeInState", group: "source" })
    ).aggregate
    expect(read.groupBy).toEqual(["code"])
    // Bare, it answers the time each code holds, each value with its code.
    expect(read.reducers).toEqual(["timeInState"])
  })

  it("holds its budgets to the server's", () => {
    // The server's defaults and caps (internal/substrate/aggregate.go): a
    // read past them is refused before anything is read.
    const [go] = Object.values(serverBudgets)
    const constant = (name: string) =>
      Number(new RegExp(`\\b${name}\\s*=\\s*(\\d+)`).exec(go)?.[1])
    expect(MAX_BUCKETS).toBe(constant("AggregateMaxBuckets"))
    expect(MAX_BUCKETS_CAP).toBe(constant("AggregateMaxBucketsCap"))
    expect(MAX_GROUPS_CAP).toBe(constant("AggregateMaxGroupsCap"))
    expect(MAX_GROUPS).toBe(constant("AggregateMaxGroups"))
    expect(MAX_SAMPLES).toBe(constant("AggregateMaxSamples"))
    expect(MAX_SAMPLES_CAP).toBe(constant("AggregateMaxSamplesCap"))
    expect(MAX_METRICS).toBe(constant("AggregateMaxMetrics"))
  })

  it("sends the output unit only where one was picked", () => {
    expect(laneRead(spec()).aggregate.unit).toBeUndefined()
    expect(laneRead(spec({ unit: "/s" })).aggregate.unit).toBe("/s")
  })
})

describe("a chart's requests", () => {
  const RANGE = laneRead(spec()).aggregate.range
  const lane = (over: Partial<LaneSpec> = {}, buckets = 30) => ({
    read: laneRead(spec(over)),
    buckets,
  })
  const heart = (over: Partial<LaneSpec> = {}) =>
    lane({
      sourceOrder: ["garmin-forerunner-265", "apple-watch-s10"],
      ...over,
    })
  const weight = (over: Partial<LaneSpec> = {}) =>
    lane({ metricPath: BODY_MASS, sourceOrder: ["withings"], ...over })

  it("reads metrics alike but for their own choices as one read", () => {
    const lanes = [
      heart({ from: "prefer-raw", statistic: "avg" }),
      lane({
        metricPath: STEPS_METRIC,
        shape: "amount",
        reducer: "sum",
        unit: "k{steps}",
        sourceOrder: ["garmin-forerunner-265", "apple-watch-s10"],
      }),
    ]
    const [only, ...rest] = sharedReads(lanes)
    expect(rest).toEqual([])
    expect(only.lanes).toEqual([0, 1])
    // Each metric's unit, reducers, from and statistic in its entry; the
    // statistic leaves the filter, which every metric shares.
    expect(only.read).toEqual({
      filter: { implements: OBSERVED },
      aggregate: {
        metrics: [
          {
            ref: HEART,
            reducers: ["avg", "min", "max"],
            from: "prefer-raw",
            statistic: "avg",
          },
          {
            ref: STEPS_METRIC,
            unit: "k{steps}",
            reducers: ["sum"],
            from: "raw",
          },
        ],
        range: RANGE,
        bucket: { calendar: "day", timezone: "Europe/Athens" },
        sources: { policy: "priority" },
        // Each metric keeps what its own read may spend: the server holds
        // the metrics to the budgets together.
        budget: {
          maxBuckets: 2 * MAX_BUCKETS,
          maxSamples: 2 * MAX_SAMPLES,
          maxGroups: 2 * MAX_GROUPS,
        },
      },
    })
  })

  it("reads apart what the metrics cannot share", () => {
    const base = heart()
    const lanes = [
      base,
      // One source: no priority policy.
      weight(),
      // A night lands on the day it ends.
      heart({
        metricPath: `${METRIC}/health/sleep_stage`,
        bucket: { calendar: "day", timezone: "Europe/Athens", place: "end" },
      }),
      // Grouped by source.
      heart({ metricPath: `${METRIC}/health/hrv`, group: "source" }),
      // Narrowed to another source.
      heart({ metricPath: `${METRIC}/health/spo2`, sources: [GARMIN] }),
      // In the reader's order.
      heart({ metricPath: `${METRIC}/health/vo2`, ranked: true }),
    ]
    const reads = sharedReads(lanes)
    expect(reads.map((r) => r.lanes)).toEqual([[0], [1], [2], [3], [4], [5]])
    // A metric alone keeps its own read.
    reads.forEach((r, i) => expect(r.read).toBe(lanes[i].read))
  })

  it("names at most 16 metrics and the buckets one read may make", () => {
    const many = Array.from({ length: MAX_METRICS + 4 }, (_, i) =>
      heart({ metricPath: `${METRIC}/health/m${i}` })
    )
    const reads = sharedReads(many)
    expect(reads.map((r) => r.lanes.length)).toEqual([MAX_METRICS, 4])
    expect(reads[0].read.aggregate.metrics).toHaveLength(MAX_METRICS)
    // The samples' and groups' budgets stop at the server's caps.
    expect(reads[0].read.aggregate.budget).toEqual({
      maxBuckets: MAX_BUCKETS_CAP,
      maxSamples: 16 * MAX_SAMPLES,
      maxGroups: MAX_GROUPS_CAP,
    })
    // Two reads past the default fit the cap together; a third does not,
    // and joins the next read with room.
    const big = (m: string, buckets: number) => ({
      ...heart({ metricPath: `${METRIC}/health/${m}`, maxBuckets: 10000 }),
      buckets,
    })
    const wide = sharedReads([
      big("a", 4000),
      big("b", 5000),
      big("c", 2000),
      big("d", 900),
    ])
    expect(wide.map((r) => r.lanes)).toEqual([[0, 1, 3], [2]])
    expect(wide[0].read.aggregate.budget?.maxBuckets).toBe(MAX_BUCKETS_CAP)
  })
})
