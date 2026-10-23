/** The aggregate reads the observations pages issue: discovery's and each
 * chart's, and the budgets they are held to. */

import { describe, expect, it } from "vitest"

import { MAX_GROUPS_CAP } from "@/lib/aggregate-rules"
import { ApiError } from "@/lib/api/types"
import {
  discoveryChunks,
  facetRead,
  indexPending,
  laneRead,
  latestRead,
  type LaneSpec,
} from "@/lib/observations-reads"
import {
  GARMIN,
  HEART,
  MIA,
  METRIC,
  SOURCE,
} from "@/lib/observations.test-fixtures"

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
        latest: true,
        budget: { maxGroups: MAX_GROUPS_CAP },
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
        latest: true,
        budget: { maxGroups: MAX_GROUPS_CAP },
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

  it("sends the output unit only where one was picked", () => {
    expect(laneRead(spec()).aggregate.unit).toBeUndefined()
    expect(laneRead(spec({ unit: "/s" })).aggregate.unit).toBe("/s")
  })
})

describe("a chart's requests", () => {
  it("names a recorded-local range of whole dates as dates", () => {
    const read = laneRead(
      spec({ bucket: { calendar: "day" }, localDates: true })
    ).aggregate
    // The server answers every date in it, at whatever offset it was
    // lived, so the read asks for exactly the reader's dates.
    expect(read.range).toEqual({
      gte: "2026-09-01",
      lt: "2026-10-01",
      recordedLocal: true,
    })
    expect(laneRead(spec()).aggregate.range?.recordedLocal).toBeUndefined()
  })

  it("names no place: the server takes the metric's", () => {
    expect(laneRead(spec()).aggregate.bucket?.place).toBeUndefined()
  })
})

describe("indexPending", () => {
  it("reads the re-derivation from the reason, never the message", () => {
    const message = "the observed index this read selects is being re-derived"
    expect(
      indexPending(
        new ApiError(
          "unavailable",
          message,
          503,
          [],
          [],
          2,
          "observed_rederiving"
        )
      )
    ).toBe(true)
    expect(
      indexPending(new ApiError("unavailable", message, 503, [], [], 2))
    ).toBe(false)
    expect(
      indexPending(
        new ApiError("unavailable", "every read slot is taken", 503, [], [], 1)
      )
    ).toBe(false)
  })
})
