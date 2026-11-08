import { afterEach, describe, expect, it, vi } from "vitest"
import { SubstrateClient } from "./client"
import { follow, parseWatchFrame, type WatchEvent } from "./watch"

describe("parseWatchFrame", () => {
  it("reads the bookmark, a row and the terminal error", () => {
    expect(parseWatchFrame('{"bookmark":41,"generation":"g7"}')).toEqual({
      type: "bookmark",
      cursor: { seq: 41, generation: "g7" },
    })
    expect(
      parseWatchFrame(
        '{"seq":42,"ts":"2026-11-08T12:00:00Z","kind":"a.example/p/k","recordId":"r1","op":"delete"}'
      )
    ).toEqual({
      type: "row",
      row: {
        seq: 42,
        kind: "a.example/p/k",
        recordId: "r1",
        op: "delete",
        removes: true,
      },
    })
    expect(
      parseWatchFrame('{"error":{"code":"compacted","message":"gone"}}')
    ).toEqual({ type: "failed", code: "compacted", message: "gone" })
  })

  it("skips a heartbeat, a blank line and anything else", () => {
    expect(parseWatchFrame("{}")).toBeUndefined()
    expect(parseWatchFrame("   ")).toBeUndefined()
    expect(parseWatchFrame("not json")).toBeUndefined()
    expect(parseWatchFrame("[1,2]")).toBeUndefined()
  })
})

/** A stream that hands out `lines`, then ends or stays open. */
function ndjson(lines: string[], end = true): Response {
  const encoder = new TextEncoder()
  return new Response(
    new ReadableStream<Uint8Array>({
      start(c) {
        // Split across chunks the way a network does.
        const text = lines.map((l) => l + "\n").join("")
        const mid = Math.floor(text.length / 2)
        c.enqueue(encoder.encode(text.slice(0, mid)))
        c.enqueue(encoder.encode(text.slice(mid)))
        if (end) c.close()
      },
    })
  )
}

const row = (seq: number) =>
  JSON.stringify({
    seq,
    ts: "2026-11-08T12:00:00Z",
    kind: "a.example/p/k",
    recordId: `r${seq}`,
    op: "put",
  })

describe("follow", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("resumes from the last row it saw when the connection drops", async () => {
    const urls: URL[] = []
    const answers = [
      () =>
        ndjson(['{"bookmark":10,"generation":"g1"}', row(11), "{}", row(12)]),
      () => ndjson(['{"bookmark":12,"generation":"g1"}', row(13)], false),
    ]
    vi.stubGlobal(
      "fetch",
      vi.fn((input: string) => {
        urls.push(new URL(input))
        return Promise.resolve(
          answers.shift()?.() ?? new Response(null, { status: 500 })
        )
      })
    )
    const events: WatchEvent[] = []
    const abort = new AbortController()
    follow(
      new SubstrateClient("https://substrate.example.com", "t"),
      ["a.example/p/k"],
      null,
      (e) => events.push(e),
      abort.signal,
      { pauses: [0] }
    )
    await vi.waitFor(() => expect(urls).toHaveLength(2))
    await vi.waitFor(() =>
      expect(events.filter((e) => e.type === "row")).toHaveLength(3)
    )
    abort.abort()
    expect(urls[0].searchParams.get("from")).toBeNull()
    expect(urls[1].searchParams.get("from")).toBe("12")
    expect(urls[1].searchParams.get("generation")).toBe("g1")
    expect(events[0]).toEqual({
      type: "opened",
      cursor: { seq: 10, generation: "g1" },
    })
    expect(events.map((e) => e.type)).toContain("interrupted")
  })

  it("asks for a relist on a 410 and on a compacted frame", async () => {
    for (const answer of [
      () =>
        new Response(JSON.stringify({ error: { code: "compacted" } }), {
          status: 410,
        }),
      () =>
        ndjson([
          '{"bookmark":3,"generation":"g1"}',
          '{"error":{"code":"compacted","message":"x"}}',
        ]),
    ]) {
      vi.stubGlobal(
        "fetch",
        vi.fn(() => Promise.resolve(answer()))
      )
      const events: WatchEvent[] = []
      const abort = new AbortController()
      follow(
        new SubstrateClient("https://substrate.example.com"),
        ["a.example/p/k"],
        { seq: 1, generation: "g0" },
        (e) => events.push(e),
        abort.signal,
        { pauses: [0] }
      )
      await vi.waitFor(() => expect(events.at(-1)).toEqual({ type: "relist" }))
      abort.abort()
    }
  })

  it("stops on a 401 and on a missing scope", async () => {
    for (const [res, type] of [
      [new Response("{}", { status: 401 }), "signedOut"],
      [
        new Response(
          JSON.stringify({ error: { code: "forbidden", message: "no" } }),
          {
            status: 403,
            headers: {
              "WWW-Authenticate": 'Bearer error="insufficient_scope"',
            },
          }
        ),
        "needsApproval",
      ],
    ] as const) {
      const fetch = vi.fn(() => Promise.resolve(res))
      vi.stubGlobal("fetch", fetch)
      const events: WatchEvent[] = []
      follow(
        new SubstrateClient("https://substrate.example.com"),
        ["a.example/p/k"],
        null,
        (e) => events.push(e),
        new AbortController().signal,
        { pauses: [0] }
      )
      await vi.waitFor(() => expect(events.at(-1)?.type).toBe(type))
      await new Promise((r) => setTimeout(r, 10))
      expect(fetch).toHaveBeenCalledTimes(1)
    }
  })

  it("drops a stream that falls silent and opens another", async () => {
    let calls = 0
    vi.stubGlobal(
      "fetch",
      vi.fn(() => {
        calls++
        return Promise.resolve(
          ndjson(['{"bookmark":5,"generation":"g1"}'], false)
        )
      })
    )
    const events: WatchEvent[] = []
    const abort = new AbortController()
    follow(
      new SubstrateClient("https://substrate.example.com"),
      ["a.example/p/k"],
      null,
      (e) => events.push(e),
      abort.signal,
      { pauses: [0], idleTimeout: 30 }
    )
    await vi.waitFor(() => expect(calls).toBeGreaterThanOrEqual(2))
    abort.abort()
    expect(events).toContainEqual({
      type: "interrupted",
      words: "The connection went quiet. Reconnecting…",
    })
  })
})
