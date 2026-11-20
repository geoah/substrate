import { FakeSubstrate } from "@substrate/testing"
import { SubstrateClient } from "@substrate/client"
import { afterEach, describe, expect, it, vi } from "vitest"
import type { SourceSummary } from "../data/sources"
import { KIND } from "../kinds"
import { METRIC_GROUPS, SESSION_GROUPS, metricPath } from "./groups"
import { byClass, buildSourceList } from "./list"
import { keepLinkedTogether, linkProps, readLink } from "./links"
import { metricOrder, moved, priorityWrites, sessionOrder } from "./ranks"

const heart = METRIC_GROUPS.find((g) => g.id === "heart")!
const labs = METRIC_GROUPS.find((g) => g.id === "labs")!

function source(
  id: string,
  cls: string,
  name: string,
  metrics: string[],
  extra: Partial<SourceSummary> = {}
): SourceSummary {
  return {
    id,
    name,
    class: cls,
    metrics: metrics.map((metric) => ({ metric })),
    nights: 0,
    workouts: 0,
    reports: 0,
    priorities: {},
    ...extra,
  }
}

const band = source(
  "band/x",
  "service",
  "Band",
  ["health/resting_heart_rate"],
  {
    priorities: { "health/resting_heart_rate": 2, "health/steps": 3 },
    nights: 300,
    workouts: 40,
  }
)
const watch = source(
  "watch/x",
  "watch",
  "Watch",
  ["health/resting_heart_rate", "health/hrv_sdnn"],
  { priorities: { "health/resting_heart_rate": 1 }, workouts: 90 }
)
const relay = source(
  "phone/x/band",
  "app",
  "Band via phone",
  ["health/hrv_sdnn"],
  { nights: 299 }
)
const lab = source("lab/x", "lab", "Lab", ["labs/ldl_cholesterol"])

afterEach(() => vi.unstubAllGlobals())

describe("the list", () => {
  it("joins the records, the latest read and the counts", () => {
    const list = buildSourceList({
      sources: [
        {
          id: "watch/x",
          kind: KIND.source,
          version: 3,
          createdAt: "",
          updatedAt: "",
          properties: {
            name: "Watch",
            class: "watch",
            priorities: { [metricPath("health/steps")]: 1 },
          },
        },
      ],
      latest: [
        {
          start: "2026-11-01T08:00:00Z",
          end: "2026-11-01T08:00:00Z",
          group: {
            metric: metricPath("health/steps"),
            source: `${KIND.source}/watch/x`,
          },
          values: [],
          count: 4,
        },
        {
          start: "2026-10-01T08:00:00Z",
          end: "2026-10-01T08:00:00Z",
          group: {
            metric: metricPath("labs/ldl_cholesterol"),
            source: `${KIND.source}/lab/no-record`,
          },
          values: [],
          count: 1,
        },
      ],
      nights: new Map([["watch/x", 12]]),
      workouts: new Map(),
      reports: new Map([["lab/no-record", 2]]),
    })
    expect(list).toEqual([
      {
        id: "watch/x",
        name: "Watch",
        class: "watch",
        metrics: [{ metric: "health/steps", latest: "2026-11-01T08:00:00Z" }],
        nights: 12,
        workouts: 0,
        reports: 0,
        priorities: { "health/steps": 1 },
      },
      {
        id: "lab/no-record",
        name: "no record",
        metrics: [
          { metric: "labs/ldl_cholesterol", latest: "2026-10-01T08:00:00Z" },
        ],
        nights: 0,
        workouts: 0,
        reports: 2,
        priorities: {},
      },
    ])
    expect(byClass([lab, relay, watch, band]).map((g) => g.header)).toEqual([
      "Watches",
      "Labs",
      "Services",
      "Apps",
    ])
  })
})

describe("ranks", () => {
  it("orders a group's sources: the ranked by rank, then the unranked", () => {
    expect(
      metricOrder(heart, [relay, band, lab, watch]).map((s) => s.id)
    ).toEqual(["watch/x", "band/x", "phone/x/band"])
    expect(
      metricOrder(labs, [relay, band, lab, watch]).map((s) => s.id)
    ).toEqual(["lab/x"])
  })

  it("writes the priorities of every metric of a group, keeping the others", () => {
    const order = moved(metricOrder(heart, [relay, band, watch]), 2, 0)
    expect(order.map((s) => s.id)).toEqual([
      "phone/x/band",
      "watch/x",
      "band/x",
    ])
    const writes = priorityWrites(heart, order, [
      "health/resting_heart_rate",
      "health/hrv_rmssd",
      "health/hrv_sdnn",
      "health/steps",
    ])
    const at = (rank: number) => ({
      [metricPath("health/resting_heart_rate")]: rank,
      [metricPath("health/hrv_rmssd")]: rank,
      [metricPath("health/hrv_sdnn")]: rank,
    })
    expect(writes).toEqual([
      { id: "phone/x/band", priorities: at(1) },
      { id: "watch/x", priorities: at(2) },
      {
        id: "band/x",
        priorities: { ...at(3), [metricPath("health/steps")]: 3 },
      },
    ])
  })

  it("orders sessions by the profile, then by class and name, a link's sources together", () => {
    const list = [relay, band, watch, lab]
    expect(sessionOrder("nights", [], list, []).map((s) => s.id)).toEqual([
      "band/x",
      "phone/x/band",
    ])
    expect(
      sessionOrder("workouts", ["band/x"], list, []).map((s) => s.id)
    ).toEqual(["band/x", "watch/x"])
    const link = {
      id: "l",
      version: 1,
      name: "Band",
      sources: ["band/x", "phone/x/band"],
    }
    expect(
      keepLinkedTogether(["phone/x/band", "watch/x", "q", "band/x"], [link])
    ).toEqual(["phone/x/band", "band/x", "watch/x", "q"])
    expect(SESSION_GROUPS.map((g) => g.id)).toEqual(["nights", "workouts"])
  })
})

describe("one device", () => {
  it("writes a link of two sources under a new id, and reads it back", async () => {
    const fake = new FakeSubstrate()
    vi.stubGlobal("fetch", fake.fetch)
    const client = new SubstrateClient(fake.origin, "tok")
    const id = "0194a1b2-0000-7000-8000-0000000000ff"
    const written = await client.put(
      KIND.sourcelink,
      id,
      linkProps(" Demo Band ", ["band/x", "phone/x/band"]),
      0
    )
    expect(written.properties).toEqual({
      name: "Demo Band",
      sources: [`${KIND.source}/band/x`, `${KIND.source}/phone/x/band`],
    })
    expect(readLink(written)).toEqual({
      id,
      version: 1,
      name: "Demo Band",
      sources: ["band/x", "phone/x/band"],
    })
  })
})
