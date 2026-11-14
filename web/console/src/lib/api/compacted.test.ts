/** A position the server can no longer resume (`410 compacted`): a walk
 * starts over from its first page, an infinite query drops its pages for a
 * fresh first page, each under its own allowance of RULE W (one recovery a
 * minute, by time alone), a refused seek is dropped and read again, and any
 * other refusal of a continuation stays what it is. */

import {
  InfiniteQueryObserver,
  notifyManager,
  QueryClient,
} from "@tanstack/react-query"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { actorsQueryOptions } from "./actors"
import { llmModelsQueryOptions } from "./agents"
import {
  changesInfiniteOptions,
  historyInfiniteOptions,
  seekQueryOptions,
} from "./changes"
import { forgetRecoveries, restartRefusedWalks } from "./compacted"
import { fetchKinds } from "./kinds"
import { referencingInfiniteOptions } from "./records"
import type { ChangeRow, Page, SubstrateRecord } from "./types"

const json = (body: unknown, status = 200) =>
  new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  })

const compacted = () =>
  json(
    {
      error: {
        code: "compacted",
        message: "the walk's order changed since it began; list again",
        head: 40,
        generation: "g2",
      },
    },
    410
  )

const otherFilter = () =>
  json(
    {
      error: {
        code: "validation",
        message: "the cursor was minted for another filter",
      },
    },
    422
  )

const record = (kind: string, id: string): SubstrateRecord =>
  ({ kind, id, properties: {} }) as unknown as SubstrateRecord

const KIND = "substrate.reamde.dev/core/kind"
const TASK = "acme.example.com/tasks/task"

/** The server, answering each request by `answer` and keeping its query. */
function serve(answer: (q: URLSearchParams) => Response) {
  const asked: URLSearchParams[] = []
  const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
    const q = new URL(String(input), "http://x").searchParams
    asked.push(q)
    return answer(q)
  })
  vi.stubGlobal("fetch", fetchMock)
  return asked
}

afterEach(() => {
  vi.unstubAllGlobals()
  forgetRecoveries()
})

/** RULE W's clock, held still and stepped by hand: `vi.waitFor` steps fake
 * timers, and with them a faked Date, as it polls. */
function stillClock(start: number) {
  let at = start
  const spy = vi.spyOn(Date, "now").mockImplementation(() => at)
  return {
    set: (ms: number) => {
      at = ms
    },
    restore: () => spy.mockRestore(),
  }
}

describe("a walk refused compacted", () => {
  it("starts over from the first page once, and holds every row once", async () => {
    let refused = 0
    const asked = serve((q) => {
      const after = q.get("after")
      if (after === "old-1") {
        refused++
        return compacted()
      }
      if (after === "new-1")
        return json({ records: [record(KIND, TASK + "s")], head: 41 })
      // The first page, read twice: the second walk's carries another token.
      return json({
        records: [record(KIND, TASK)],
        cursor: refused ? "new-1" : "old-1",
        head: 40,
      })
    })
    const kinds = await fetchKinds()
    expect(kinds.map((k) => k.identity)).toEqual([TASK, TASK + "s"])
    expect(asked.map((q) => q.get("after"))).toEqual([
      null,
      "old-1",
      null,
      "new-1",
    ])
  })

  it("answers the fresh walk's head, the one a watch resumes from", async () => {
    let refused = 0
    serve((q) => {
      if (q.get("after") === "old-1") {
        refused++
        return compacted()
      }
      if (q.get("after") === "new-1")
        return json({
          records: [record(TASK, "b")],
          head: 50,
          generation: "g2",
        })
      return json({
        records: [record(TASK, "a")],
        cursor: refused ? "new-1" : "old-1",
        head: refused ? 41 : 30,
        generation: refused ? "g2" : "g1",
      })
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const page = await client.fetchQuery(llmModelsQueryOptions())
    expect(page.records.map((r) => r.id)).toEqual(["a", "b"])
    expect(page).toMatchObject({ head: 41, generation: "g2" })
  })

  it("fails on a second refusal, never a third walk", async () => {
    const asked = serve((q) =>
      q.get("after")
        ? compacted()
        : json({ records: [record(KIND, TASK)], cursor: "c", head: 40 })
    )
    await expect(fetchKinds()).rejects.toMatchObject({
      code: "compacted",
      status: 410,
    })
    expect(asked).toHaveLength(4)
  })

  it("leaves a 422 on a continuation the refusal it is", async () => {
    const asked = serve((q) =>
      q.get("after")
        ? otherFilter()
        : json({ records: [record(KIND, TASK)], cursor: "c", head: 40 })
    )
    await expect(fetchKinds()).rejects.toMatchObject({
      code: "validation",
      status: 422,
    })
    expect(asked).toHaveLength(2)
  })

  it("starts the actor registry over, and names every actor once", async () => {
    const ACTOR = "substrate.reamde.dev/core/actor"
    const actor = (id: string): SubstrateRecord =>
      ({
        kind: ACTOR,
        id,
        properties: { type: "agent", name: id },
      }) as unknown as SubstrateRecord
    let refused = 0
    const asked = serve((q) => {
      if (q.get("after") === "old-1") {
        refused++
        return compacted()
      }
      if (q.get("after") === "new-1") return json({ records: [actor("b")] })
      return json({
        records: [actor("a")],
        cursor: refused ? "new-1" : "old-1",
      })
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    const actors = await client.fetchQuery(actorsQueryOptions)
    expect(actors.map((a) => a.id)).toEqual(["a", "b"])
    expect(asked.map((q) => q.get("after"))).toEqual([
      null,
      "old-1",
      null,
      "new-1",
    ])
    client.clear()
  })
})

/** RULE W's minute on a walk read whole: the walk started over at T0 is
 * refused again `after` ms on, the clock stepped as the continuation is
 * answered. */
describe("a walk refused again, by the walk's clock", () => {
  const T0 = Date.parse("2026-11-14T09:00:00Z")
  let clock: ReturnType<typeof stillClock>
  beforeEach(() => {
    clock = stillClock(T0)
  })
  afterEach(() => clock.restore())

  function refusedAgainAfter(after: number) {
    let walk = 0
    return serve((q) => {
      const cursor = q.get("after")
      if (!cursor) {
        walk++
        return json({ records: [record(KIND, TASK)], cursor: `w${walk}` })
      }
      if (walk === 1) return compacted()
      if (walk === 2) {
        clock.set(T0 + after)
        return compacted()
      }
      return json({ records: [record(KIND, TASK + "s")] })
    })
  }

  it("fails a refusal 59,999 ms after the start-over", async () => {
    const asked = refusedAgainAfter(59_999)
    await expect(fetchKinds()).rejects.toMatchObject({ code: "compacted" })
    expect(asked.map((q) => q.get("after"))).toEqual([null, "w1", null, "w2"])
  })

  it("starts over again on a refusal 60,000 ms after the start-over", async () => {
    const asked = refusedAgainAfter(60_000)
    const kinds = await fetchKinds()
    expect(kinds.map((k) => k.identity)).toEqual([TASK, TASK + "s"])
    expect(asked.map((q) => q.get("after"))).toEqual([
      null,
      "w1",
      null,
      "w2",
      null,
      "w3",
    ])
  })
})

/** The statuses an observer shows as React reads them: told of a change on a
 * later task, it reads the result as it stands then. */
function statuses(observer: {
  subscribe(listener: () => void): () => void
  getCurrentResult(): { status: string }
}) {
  const seen: string[] = []
  const unsubscribe = observer.subscribe(
    notifyManager.batchCalls(() => {
      seen.push(observer.getCurrentResult().status)
    })
  )
  return { seen, unsubscribe }
}

describe("an infinite query refused compacted", () => {
  let client: QueryClient
  let stop: () => void
  beforeEach(() => {
    client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
    stop = restartRefusedWalks(client)
  })
  afterEach(() => {
    stop()
    client.clear()
  })

  const ids = (data: { pages: Page[] } | undefined) =>
    data?.pages.map((p) => p.records.map((r) => r.id))

  it("replaces its pages with a fresh first page, and shows no error", async () => {
    let walk = 0
    const asked = serve((q) => {
      if (q.get("after") === "old-1") return compacted()
      if (!q.get("after")) walk++
      return walk === 1
        ? json({ records: [record(TASK, "a")], cursor: "old-1", head: 1 })
        : json({
            records: [record(TASK, "a"), record(TASK, "b")],
            cursor: "new-1",
            head: 2,
          })
    })
    const observer = new InfiniteQueryObserver(
      client,
      referencingInfiniteOptions("acme.example.com/people/person/p1")
    )
    const { seen, unsubscribe } = statuses(observer)
    await vi.waitFor(() =>
      expect(ids(observer.getCurrentResult().data)).toEqual([["a"]])
    )
    await observer.fetchNextPage()
    await vi.waitFor(() => {
      const now = observer.getCurrentResult()
      expect(now.isFetching).toBe(false)
      expect(ids(now.data)).toEqual([["a", "b"]])
    })
    expect(observer.getCurrentResult().error).toBeNull()
    expect(observer.getCurrentResult().hasNextPage).toBe(true)
    expect(seen).not.toContain("error")
    expect(asked.map((q) => q.get("after"))).toEqual([null, "old-1", null])
    unsubscribe()
  })

  it("fails a walk refused again at once: its error, no pages, and no refused cursor sent again", async () => {
    let first = 0
    const asked = serve((q) => {
      if (q.get("after")) return compacted()
      first++
      return json({ records: [record(TASK, "a")], cursor: `c${first}` })
    })
    const observer = new InfiniteQueryObserver(
      client,
      referencingInfiniteOptions("acme.example.com/people/person/p1")
    )
    const { unsubscribe } = statuses(observer)
    await vi.waitFor(() =>
      expect(ids(observer.getCurrentResult().data)).toEqual([["a"]])
    )
    await observer.fetchNextPage()
    await vi.waitFor(() => {
      expect(observer.getCurrentResult().isFetching).toBe(false)
      expect(ids(observer.getCurrentResult().data)).toEqual([["a"]])
    })
    expect(asked).toHaveLength(3)
    await observer.fetchNextPage()
    await vi.waitFor(() => {
      const now = observer.getCurrentResult()
      expect(now.status).toBe("error")
      expect(now.data).toBeUndefined()
    })
    expect(observer.getCurrentResult().error).toMatchObject({
      code: "compacted",
    })
    expect(observer.getCurrentResult().hasNextPage).toBe(false)
    expect(asked.map((q) => q.get("after"))).toEqual([null, "c1", null, "c2"])
    unsubscribe()
  })

  it("fails a refusal of the same list within the minute, though its cache entry went and it was read again", async () => {
    let first = 0
    const asked = serve((q) => {
      if (q.get("after")) return compacted()
      first++
      return json({ records: [record(TASK, "a")], cursor: `c${first}` })
    })
    const options = referencingInfiniteOptions(
      "acme.example.com/people/person/p1"
    )
    const read = async () => {
      const observer = new InfiniteQueryObserver(client, options)
      const watched = statuses(observer)
      await vi.waitFor(() =>
        expect(ids(observer.getCurrentResult().data)).toEqual([["a"]])
      )
      return { observer, ...watched }
    }
    const before = await read()
    await before.observer.fetchNextPage()
    await vi.waitFor(() => {
      expect(before.observer.getCurrentResult().isFetching).toBe(false)
      expect(ids(before.observer.getCurrentResult().data)).toEqual([["a"]])
    })
    expect(before.seen).not.toContain("error")
    // The list unmounted and its entry collected, then read again.
    before.unsubscribe()
    client.removeQueries({ queryKey: options.queryKey })
    const after = await read()
    await after.observer.fetchNextPage()
    await vi.waitFor(() => {
      const now = after.observer.getCurrentResult()
      expect(now.status).toBe("error")
      expect(now.data).toBeUndefined()
    })
    expect(after.observer.getCurrentResult().error).toMatchObject({
      code: "compacted",
    })
    expect(asked.map((q) => q.get("after"))).toEqual([
      null,
      "c1",
      null,
      null,
      "c3",
    ])
    after.unsubscribe()
  })

  it("leaves a 422 on a later page the error it is, and keeps its pages", async () => {
    const asked = serve((q) =>
      q.get("after")
        ? otherFilter()
        : json({ records: [record(TASK, "a")], cursor: "c", head: 1 })
    )
    const observer = new InfiniteQueryObserver(
      client,
      referencingInfiniteOptions("acme.example.com/people/person/p1")
    )
    const { unsubscribe } = statuses(observer)
    await vi.waitFor(() =>
      expect(ids(observer.getCurrentResult().data)).toEqual([["a"]])
    )
    await observer.fetchNextPage()
    await new Promise((resolve) => setTimeout(resolve, 20))
    const now = observer.getCurrentResult()
    expect(now.status).toBe("error")
    expect(now.error).toMatchObject({ code: "validation" })
    expect(ids(now.data)).toEqual([["a"]])
    expect(asked).toHaveLength(2)
    unsubscribe()
  })

  describe("by the query's clock", () => {
    const T0 = Date.parse("2026-11-14T10:00:00Z")
    let clock: ReturnType<typeof stillClock>
    beforeEach(() => {
      clock = stillClock(T0)
    })
    afterEach(() => clock.restore())

    /** A list whose first page's cursor `c1` is refused at T0 (the
     * recovery), and whose second page's cursor `d<n>` is refused once the
     * clock reaches `refuseFrom`. */
    function list(refuseFrom: number) {
      let first = 0
      return serve((q) => {
        const after = q.get("after")
        if (!after) {
          first++
          return json({ records: [record(TASK, "a")], cursor: `c${first}` })
        }
        if (after === "c1") return compacted()
        if (after.startsWith("d") && Date.now() >= refuseFrom)
          return compacted()
        return json({
          records: [record(TASK, "b")],
          cursor: `d${after.slice(1)}`,
        })
      })
    }

    /** The query read, refused and started over at T0. */
    async function startedOver() {
      const observer = new InfiniteQueryObserver(
        client,
        referencingInfiniteOptions("acme.example.com/people/person/p1")
      )
      const watched = statuses(observer)
      await vi.waitFor(() =>
        expect(ids(observer.getCurrentResult().data)).toEqual([["a"]])
      )
      await observer.fetchNextPage()
      await vi.waitFor(() => {
        expect(observer.getCurrentResult().isFetching).toBe(false)
        expect(ids(observer.getCurrentResult().data)).toEqual([["a"]])
      })
      return { observer, ...watched }
    }

    it("fails a refusal 59,999 ms after the start-over, though a list landed and a change re-read it, and sends that cursor never again", async () => {
      const at = T0 + 59_999
      const asked = list(at)
      const { observer, seen, unsubscribe } = await startedOver()
      clock.set(T0 + 30_000)
      // A list that lands, then a delivered change that reads it again.
      await observer.fetchNextPage()
      await vi.waitFor(() =>
        expect(ids(observer.getCurrentResult().data)).toEqual([["a"], ["b"]])
      )
      await observer.refetch()
      await vi.waitFor(() => {
        expect(observer.getCurrentResult().isFetching).toBe(false)
        expect(ids(observer.getCurrentResult().data)).toEqual([["a"], ["b"]])
      })
      expect(seen).not.toContain("error")
      clock.set(at)
      await observer.fetchNextPage()
      await vi.waitFor(() => {
        const now = observer.getCurrentResult()
        expect(now.status).toBe("error")
        expect(now.data).toBeUndefined()
      })
      const refused = asked.at(-1)?.get("after")
      expect(refused).toBe("d3")
      expect(observer.getCurrentResult().error).toMatchObject({
        code: "compacted",
      })
      expect(observer.getCurrentResult().hasNextPage).toBe(false)
      const sent = asked.length
      // Load more, then Try again: each reads the first page.
      await observer.fetchNextPage()
      await vi.waitFor(() =>
        expect(ids(observer.getCurrentResult().data)).toEqual([["a"]])
      )
      await observer.refetch()
      await vi.waitFor(() =>
        expect(observer.getCurrentResult().isFetching).toBe(false)
      )
      expect(asked.slice(sent).map((q) => q.get("after"))).toEqual([null, null])
      unsubscribe()
    })

    it("starts over on a refusal 60,000 ms after the start-over, though a list landed between", async () => {
      const at = T0 + 60_000
      const asked = list(at)
      const { observer, seen, unsubscribe } = await startedOver()
      clock.set(T0 + 30_000)
      await observer.fetchNextPage()
      await vi.waitFor(() =>
        expect(ids(observer.getCurrentResult().data)).toEqual([["a"], ["b"]])
      )
      clock.set(at)
      await observer.fetchNextPage()
      await vi.waitFor(() => {
        const now = observer.getCurrentResult()
        expect(now.isFetching).toBe(false)
        expect(ids(now.data)).toEqual([["a"]])
      })
      expect(observer.getCurrentResult().error).toBeNull()
      expect(observer.getCurrentResult().hasNextPage).toBe(true)
      expect(seen).not.toContain("error")
      expect(asked.map((q) => q.get("after"))).toEqual([
        null,
        "c1",
        null,
        "c2",
        "d2",
        null,
      ])
      unsubscribe()
    })
  })

  it("reads History again from its newest page when an older one is refused", async () => {
    const run = (seq: number) => ({
      actor: "console",
      kind: TASK,
      op: "put",
      count: 1,
      newestSeq: seq,
      oldestSeq: seq,
      newestTs: "2026-11-14T00:00:00Z",
      oldestTs: "2026-11-14T00:00:00Z",
      rows: [],
    })
    let restored = false
    const asked = serve((q) => {
      if (q.get("before")) {
        if (q.get("generation") === "g1") {
          restored = true
          return compacted()
        }
        return json({ runs: [run(1)], head: 9, generation: "g2" })
      }
      return restored
        ? json({ runs: [run(9)], cursor: 9, head: 9, generation: "g2" })
        : json({ runs: [run(8)], cursor: 8, head: 8, generation: "g1" })
    })
    const observer = new InfiniteQueryObserver(
      client,
      historyInfiniteOptions({})
    )
    const { seen, unsubscribe } = statuses(observer)
    const newest = () =>
      observer
        .getCurrentResult()
        .data?.pages.map((p) => (p.runs ?? []).map((r) => r.newestSeq))
    await vi.waitFor(() => expect(newest()).toEqual([[8]]))
    await observer.fetchNextPage()
    await vi.waitFor(() => {
      expect(observer.getCurrentResult().isFetching).toBe(false)
      expect(newest()).toEqual([[9]])
    })
    expect(seen).not.toContain("error")
    expect(asked.map((q) => [q.get("before"), q.get("generation")])).toEqual([
      [null, null],
      ["8", "g1"],
      [null, null],
    ])
    unsubscribe()
  })
})

describe("history until an instant, from a seek the server refuses", () => {
  const T = Date.parse("2026-11-14T12:00:00Z")
  const row = (seq: number, ms: number): ChangeRow => ({
    seq,
    ts: new Date(ms).toISOString(),
    actor: "console",
    op: "put",
    recordId: `t${seq}`,
    kind: TASK,
  })
  // The restored history: seq 1 before the instant, 2 and 3 after it.
  const rows = [row(3, T + 2000), row(2, T + 1000), row(1, T - 1000)]

  it("drops the kept seek, seeks again, and reads under the fresh generation", async () => {
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    // A position the console kept, from the history before a restore.
    client.setQueryData(seekQueryOptions(T).queryKey, {
      before: 5,
      generation: "old",
    })
    const asked = serve((q) => {
      if (q.get("generation") === "old") return compacted()
      const before = Number(q.get("before") ?? Infinity)
      const below = rows.filter((r) => r.seq < before)
      return json({
        changes: below.slice(0, Number(q.get("first"))),
        head: 3,
        generation: "new",
      })
    })
    const page = await client.fetchInfiniteQuery(
      changesInfiniteOptions({}, { first: 2, untilMs: T })
    )
    expect(page.pages[0].changes.map((r) => r.seq)).toEqual([1])
    expect(client.getQueryData(seekQueryOptions(T).queryKey)).toEqual({
      before: 2,
      generation: "new",
    })
    const sent = asked.map((q) => [q.get("before"), q.get("generation")])
    expect(sent[0]).toEqual(["5", "old"])
    // Refused once, never sent again.
    expect(sent.slice(1)).not.toContainEqual(["5", "old"])
    expect(sent.at(-1)).toEqual(["2", "new"])
    client.clear()
  })
})
