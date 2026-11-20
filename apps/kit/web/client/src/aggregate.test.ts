import { afterEach, describe, expect, it, vi } from "vitest"
import type { AggregateAnswer } from "./aggregate"
import { SubstrateClient } from "./client"
import { SubstrateError } from "./errors"

const heartRate = "substrate.reamde.dev/observations/metric/health/heart_rate"

describe("SubstrateClient.aggregate", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("sends the body, the filter and the downsample as JSON on the records route", async () => {
    const answer: AggregateAnswer = {
      metric: heartRate,
      unit: "1/min",
      exact: false,
      rows: [],
      head: { changelog: 0, generation: "" },
    }
    const seen: { url: URL; init: RequestInit }[] = []
    vi.stubGlobal(
      "fetch",
      vi.fn((input: string, init: RequestInit) => {
        seen.push({ url: new URL(input), init })
        return Promise.resolve(new Response(JSON.stringify(answer)))
      })
    )
    const abort = new AbortController()
    const got = await new SubstrateClient(
      "https://substrate.example.com/",
      "tok"
    ).aggregate(
      {
        metric: { ref: heartRate },
        range: {
          gte: "2026-07-20T00:00:00+03:00",
          lt: "2026-07-27T00:00:00+03:00",
        },
        reducers: [{ durationIn: [95, 114] }, "avg"],
      },
      {
        filter: { kinds: ["substrate.reamde.dev/observations/trace"] },
        downsample: { points: 800 },
        signal: abort.signal,
      }
    )
    expect(got).toEqual(answer)
    const [{ url, init }] = seen
    expect(url.pathname).toBe("/api/v1/records")
    expect(init.method).toBe("GET")
    expect(init.signal).toBe(abort.signal)
    expect(new Headers(init.headers).get("Authorization")).toBe("Bearer tok")
    expect(JSON.parse(url.searchParams.get("aggregate")!)).toEqual({
      metric: { ref: heartRate },
      range: {
        gte: "2026-07-20T00:00:00+03:00",
        lt: "2026-07-27T00:00:00+03:00",
      },
      reducers: [{ durationIn: [95, 114] }, "avg"],
    })
    expect(JSON.parse(url.searchParams.get("filter")!)).toEqual({
      kinds: ["substrate.reamde.dev/observations/trace"],
    })
    expect(JSON.parse(url.searchParams.get("downsample")!)).toEqual({
      points: 800,
    })
  })

  it("sends no filter or downsample it was not given", async () => {
    const urls: URL[] = []
    vi.stubGlobal(
      "fetch",
      vi.fn((input: string) => {
        urls.push(new URL(input))
        return Promise.resolve(
          new Response(
            JSON.stringify({ exact: true, rows: [], head: { changelog: 0 } })
          )
        )
      })
    )
    await new SubstrateClient("https://substrate.example.com", "tok").aggregate(
      { latest: true, groupBy: ["metric", "source"] }
    )
    expect([...urls[0].searchParams.keys()]).toEqual(["aggregate"])
  })

  it("throws the envelope's refusal", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() =>
        Promise.resolve(
          new Response(
            JSON.stringify({
              error: {
                code: "validation",
                message: "sum does not apply to a gauge",
                problemDetails: [
                  {
                    path: "aggregate.reducers[0]",
                    message: "sum does not apply to a gauge",
                  },
                ],
              },
            }),
            { status: 422 }
          )
        )
      )
    )
    const read = new SubstrateClient(
      "https://substrate.example.com",
      "tok"
    ).aggregate({
      metric: { ref: heartRate },
      range: { gte: "2026-07-20T00:00:00Z", lt: "2026-07-21T00:00:00Z" },
      reducers: ["sum"],
    })
    await expect(read).rejects.toBeInstanceOf(SubstrateError)
    await expect(read).rejects.toMatchObject({
      status: 422,
      code: "validation",
    })
  })
})
