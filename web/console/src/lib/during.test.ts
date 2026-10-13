/** A workout's and a sleep's "During this": which records get it, the
 * reads it makes over the record's own span, and when a night's stages
 * draw as a hypnogram. The suite runs in Europe/Athens. */

import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  duringOf,
  duringTimelineSearch,
  heartCurveRead,
  hypnogramOf,
  stageListParams,
  stageRows,
  stageSegments,
  stageTotalsRead,
  zonesRead,
} from "@/lib/during"
import { isoWithOffset } from "@/lib/observations-time"
import {
  AUTHORITY,
  GARMIN,
  KINDS,
  METRIC,
  MIA,
} from "@/lib/observations.test-fixtures"

function healthKind(name: string, traits: string[]): KindInfo {
  return {
    identity: `${AUTHORITY}/health/${name}`,
    name,
    authority: AUTHORITY,
    package: "health",
    version: 1,
    source: "installed",
    description: "",
    definition: { authority: AUTHORITY, package: "health", traits },
  }
}

const WORKOUT_KIND = healthKind("workout", [
  "substrate.reamde.dev/core/temporal(range)",
  "substrate.reamde.dev/core/revised",
])
const SLEEP_KIND = healthKind("sleep", [
  "substrate.reamde.dev/core/temporal(range)",
])
const ALL = [...KINDS, WORKOUT_KIND, SLEEP_KIND]

function record(
  kind: KindInfo,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id: "garmin/ada/21100008900",
    kind: kind.identity,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-10-11T07:00:00Z",
    updatedAt: "2026-10-11T07:00:00Z",
  }
}

const RUN = record(WORKOUT_KIND, {
  sport: "running",
  at: "2026-10-11T05:25:00Z",
  endsAt: "2026-10-11T06:59:00Z",
  source: { ref: GARMIN },
})

describe("which records show what was measured during them", () => {
  it("is a health record spanning a window that is no measured value", () => {
    const during = duringOf(RUN, WORKOUT_KIND, ALL)!
    expect(during).toMatchObject({
      window: {
        start: Date.parse("2026-10-11T05:25:00Z"),
        end: Date.parse("2026-10-11T06:59:00Z"),
      },
      heart: `${METRIC}/health/heart_rate`,
      zones: true,
      source: GARMIN,
    })
    expect(during.stage).toBeUndefined()
    const night = duringOf(
      record(SLEEP_KIND, {
        at: "2026-10-06T23:10:00+03:00",
        endsAt: "2026-10-07T06:20:00+03:00",
      }),
      SLEEP_KIND,
      ALL
    )!
    expect(night).toMatchObject({
      stage: `${METRIC}/health/sleep_stage`,
      zones: false,
    })
    // Another package's span, a measured value, or a span with no end.
    expect(
      duringOf(RUN, { ...WORKOUT_KIND, package: "fitness" }, ALL)
    ).toBeUndefined()
    const labresult = healthKind("labresult", [
      "substrate.reamde.dev/core/temporal(range)",
      "substrate.reamde.dev/core/observed(single)",
    ])
    expect(duringOf(RUN, labresult, ALL)).toBeUndefined()
    expect(
      duringOf(
        record(WORKOUT_KIND, { at: "2026-10-11T05:25:00Z" }),
        WORKOUT_KIND,
        ALL
      )
    ).toBeUndefined()
    // No observed kind pins a metric: nothing to read.
    expect(duringOf(RUN, WORKOUT_KIND, [WORKOUT_KIND])).toBeUndefined()
  })
})

describe("the reads over the record's span", () => {
  const during = duringOf(RUN, WORKOUT_KIND, ALL)!

  it("reads the owner's heart rate over the span, the record's device first", () => {
    const read = heartCurveRead(during, during.window, "Europe/Athens")
    expect(read.filter).toEqual({
      implements: "substrate.reamde.dev/core/observed",
      properties: { about: { exists: false } },
    })
    expect(read.aggregate).toEqual({
      metric: { ref: `${METRIC}/health/heart_rate` },
      from: "raw",
      range: {
        gte: isoWithOffset(Date.parse("2026-10-11T05:25:00Z")),
        lt: isoWithOffset(Date.parse("2026-10-11T06:59:00Z")),
      },
      // 94 minutes, read by the minute.
      bucket: { every: "PT1M" },
      reducers: ["avg", "min", "max"],
      sources: { policy: "priority", order: [GARMIN] },
    })
  })

  it("reads the time in each zone in minutes, one row over the span", () => {
    const read = zonesRead({ ...during, about: MIA })
    expect(read.filter?.properties).toEqual({ about: { eq: MIA } })
    expect(read.aggregate.bucket).toBeUndefined()
    expect(read.aggregate.unit).toBe("min")
    expect(read.aggregate.reducers).toEqual([
      { durationIn: [95, 114] },
      { durationIn: [114, 133] },
      { durationIn: [133, 152] },
      { durationIn: [152, 171] },
      { durationIn: [171, 220] },
    ])
  })

  it("reads a night's stages from the sleep's own source, by code", () => {
    const night = { ...during, stage: `${METRIC}/health/sleep_stage` }
    expect(stageTotalsRead(night)).toMatchObject({
      filter: {
        properties: { about: { exists: false }, source: { eq: GARMIN } },
      },
      aggregate: {
        metric: { ref: `${METRIC}/health/sleep_stage` },
        groupBy: ["code"],
        reducers: ["sum"],
        unit: "min",
      },
    })
    // A sleep naming no source takes one source's stages, by priority.
    expect(
      stageTotalsRead({ ...night, source: undefined }).aggregate.sources
    ).toEqual({ policy: "priority" })
    expect(stageListParams(night)).toMatchObject({
      orderBy: "at:asc",
      filter: {
        properties: {
          metric: { eq: `${METRIC}/health/sleep_stage` },
          source: { eq: GARMIN },
          at: {
            gte: "2026-10-11T05:25:00.000Z",
            lt: "2026-10-11T06:59:00.000Z",
          },
        },
      },
    })
  })

  it("opens the timeline over the span", () => {
    expect(duringTimelineSearch(during)).toEqual({
      metric: `${METRIC}/health/heart_rate`,
      range: "custom",
      start: "2026-10-11T05:25:00.000Z",
      end: "2026-10-11T06:59:00.000Z",
    })
  })
})

describe("a night's stages", () => {
  const stage = (code: string, at: string, endsAt: string) =>
    record(SLEEP_KIND, { code, at, endsAt })

  it("draws a hypnogram where the stages follow one another", () => {
    const segments = stageSegments([
      stage("light", "2026-10-06T23:20:00+03:00", "2026-10-07T00:05:00+03:00"),
      stage("awake", "2026-10-06T23:10:00+03:00", "2026-10-06T23:20:00+03:00"),
      stage("deep", "2026-10-07T00:05:00+03:00", "2026-10-07T01:00:00+03:00"),
      stage("rem", "2026-10-07T01:00:00+03:00", "2026-10-07T01:30:00+03:00"),
      { ...stage("rem", "", ""), properties: { code: "rem" } },
    ])
    expect(segments.map((s) => s.code)).toEqual([
      "awake",
      "light",
      "deep",
      "rem",
    ])
    expect(hypnogramOf(segments)).toHaveLength(4)
    expect(stageRows(segments)).toEqual(["awake", "rem", "light", "deep"])
  })

  it("draws none where each stage spans the whole night", () => {
    // WHOOP writes one span per stage over the night, the stage's total.
    const night = ["awake", "light", "deep", "rem"].map((code) =>
      stage(code, "2026-10-06T23:10:00+03:00", "2026-10-07T06:20:00+03:00")
    )
    expect(hypnogramOf(stageSegments(night))).toBeUndefined()
    expect(
      hypnogramOf(
        stageSegments([
          stage(
            "deep",
            "2026-10-06T23:10:00+03:00",
            "2026-10-07T06:20:00+03:00"
          ),
        ])
      )
    ).toBeUndefined()
  })
})
