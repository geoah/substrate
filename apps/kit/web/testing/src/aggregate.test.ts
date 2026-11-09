import {
  SubstrateClient,
  SubstrateError,
  type AggregateAnswer,
} from "@substrate/client"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { FakeSubstrate } from "./index"

const steps = "substrate.reamde.dev/observations/metric/health/steps"
const sample = "substrate.reamde.dev/observations/observation"
const watch = "substrate.reamde.dev/observations/source/applehealth/watch"

let fake: FakeSubstrate
let client: SubstrateClient

beforeEach(() => {
  fake = new FakeSubstrate()
  vi.stubGlobal("fetch", fake.fetch)
  client = new SubstrateClient(fake.origin, "tok")
})
afterEach(() => vi.unstubAllGlobals())

describe("FakeSubstrate's aggregate read", () => {
  it("answers a canned read and keeps the query it took", async () => {
    const daily: AggregateAnswer = {
      metric: steps,
      unit: "steps",
      bucket: { calendar: "day", timezone: "Europe/Athens", place: "start" },
      from: "raw",
      exact: false,
      rows: [
        {
          start: "2026-08-12T00:00:00+03:00",
          end: "2026-08-13T00:00:00+03:00",
          form: "amount",
          from: "raw",
          group: { source: watch },
          sources: [watch],
          coverage: "PT2H56M",
          values: [{ reducer: "sum", value: "7465.4839260528", unit: "steps" }],
          count: 176,
        },
      ],
      sources: [watch],
      head: { changelog: 0, generation: "" },
    }
    fake.answerAggregate((q) => q.aggregate.metric?.ref === steps, daily)
    const got = await client.aggregate(
      {
        metric: { ref: steps },
        range: {
          gte: "2026-08-12T00:00:00+03:00",
          lt: "2026-08-13T00:00:00+03:00",
        },
        bucket: { calendar: "day", timezone: "Europe/Athens" },
        groupBy: ["source"],
        reducers: ["sum"],
        sources: { policy: "priority" },
      },
      { filter: { kinds: [sample] } }
    )
    expect(got).toEqual(daily)
    // Typed as the answer: a row's value and its group read through.
    expect(got.rows[0].values[0].value).toBe("7465.4839260528")
    expect(got.rows[0].group?.source).toBe(watch)
    expect(fake.aggregates).toEqual([
      {
        aggregate: {
          metric: { ref: steps },
          range: {
            gte: "2026-08-12T00:00:00+03:00",
            lt: "2026-08-13T00:00:00+03:00",
          },
          bucket: { calendar: "day", timezone: "Europe/Athens" },
          groupBy: ["source"],
          reducers: ["sum"],
          sources: { policy: "priority" },
        },
        filter: { kinds: [sample] },
      },
    ])
  })

  it("answers from the query, the latest registration first", async () => {
    fake.answerAggregate(
      () => true,
      () => ({ exact: true, rows: [], head: { changelog: 0, generation: "" } })
    )
    fake.answerAggregate(
      (q) => q.aggregate.latest === true,
      (q) => ({
        exact: true,
        rows: (q.aggregate.groupBy ?? []).map((key) => ({
          start: "2026-08-12T07:00:00+03:00",
          end: "2026-08-12T07:00:00+03:00",
          group: key === "metric" ? { metric: "x" } : { source: "x" },
          values: [{ reducer: "last", value: "1" }],
          count: 1,
        })),
        head: { changelog: 0, generation: "" },
      })
    )
    const latest = await client.aggregate({
      latest: true,
      groupBy: ["metric", "source"],
    })
    expect(latest.rows.map((r) => r.group)).toEqual([
      { metric: "x" },
      { source: "x" },
    ])
    const other = await client.aggregate({ metric: { ref: steps } })
    expect(other.rows).toEqual([])
    expect(fake.aggregates.map((q) => q.downsample)).toEqual([
      undefined,
      undefined,
    ])
  })

  it("refuses a read nothing answers, and one naming an undeclared kind", async () => {
    await expect(
      client.aggregate({ metric: { ref: steps } })
    ).rejects.toMatchObject({ status: 422, code: "validation" })
    fake.undeclared.add(sample)
    fake.answerAggregate(() => true, {
      exact: true,
      rows: [],
      head: { changelog: 0, generation: "" },
    })
    const refused = client.aggregate(
      { metric: { ref: steps } },
      { filter: { kinds: [sample] } }
    )
    await expect(refused).rejects.toBeInstanceOf(SubstrateError)
    await expect(refused).rejects.toMatchObject({ status: 404 })
    await expect(
      client.list({ kinds: [sample], first: 1 })
    ).rejects.toMatchObject({ status: 404 })
  })
})
