import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import {
  changesInfiniteOptions,
  changesSearch,
  fetchChangesPage,
  fetchHistoryPage,
  parseWatchLine,
  resetRunsSupport,
  resetValuesSupport,
  runRowsQueryOptions,
  seekBoundary,
  type SeekProbe,
} from "./changes"
import type { ChangePage, ChangeRow, ChangeRunPage } from "./types"

const T0 = Date.parse("2026-08-05T12:00:00Z")

function row(seq: number, tsMs = T0 + seq * 1000): ChangeRow {
  return {
    seq,
    ts: new Date(tsMs).toISOString(),
    actor: "a",
    op: "put",
    recordId: `e-${seq}`,
    kind: "samples.substrate.reamde.dev/tasks/task",
  }
}

describe("changesSearch", () => {
  it("carries every server-side facet, repeated params for lists", () => {
    const params = changesSearch({
      kinds: [
        "samples.substrate.reamde.dev/people/person",
        "samples.substrate.reamde.dev/tasks/task",
      ],
      actors: ["owner"],
      ops: ["put", "merge"],
      recordId: "e1",
      recordKind: "samples.substrate.reamde.dev/tasks/task",
      q: "needle",
    })
    expect(params.getAll("kinds")).toEqual([
      "samples.substrate.reamde.dev/people/person",
      "samples.substrate.reamde.dev/tasks/task",
    ])
    expect(params.getAll("actors")).toEqual(["owner"])
    expect(params.getAll("ops")).toEqual(["put", "merge"])
    expect(params.get("recordId")).toBe("e1")
    expect(params.get("recordKind")).toBe(
      "samples.substrate.reamde.dev/tasks/task"
    )
    expect(params.get("q")).toBe("needle")
  })

  it("refuses recordId without its recordKind companion (server rejects either alone)", () => {
    expect(changesSearch({ recordId: "e1" }).has("recordId")).toBe(false)
    expect(
      changesSearch({
        recordKind: "samples.substrate.reamde.dev/tasks/task",
      }).has("recordKind")
    ).toBe(false)
  })

  it("stays empty for an empty filter", () => {
    expect(changesSearch({}).toString()).toBe("")
  })
})

describe("changesInfiniteOptions paging", () => {
  const opts = changesInfiniteOptions({}, { first: 3 })
  // Every page names the history generation it was read in; the continuation
  // carries it back so the next page walks the same history.
  const page = (changes: ChangeRow[], cursor?: number): ChangePage => ({
    changes,
    cursor,
    head: changes[0]?.seq ?? 0,
    generation: "gen-1",
  })
  const next = (p: ChangePage) =>
    opts.getNextPageParam(p, [p], { before: 0 }, [{ before: 0 }])

  it("continues on the server cursor, not on a full page", () => {
    expect(next(page([row(9), row(8), row(7)], 7))).toEqual({
      before: 7,
      generation: "gen-1",
    })
  })

  it("a short page still continues while a cursor comes back (scope filtering)", () => {
    expect(next(page([row(9)], 9))).toEqual({ before: 9, generation: "gen-1" })
  })

  it("stops when the cursor is omitted (the feed's beginning)", () => {
    expect(next(page([row(2), row(1)]))).toBeUndefined()
  })

  it("stops once a page crosses the since floor", () => {
    const withFloor = changesInfiniteOptions(
      {},
      { first: 3, sinceMs: T0 + 8_000 }
    )
    const below = page([row(9), row(8), row(7)], 7)
    expect(
      withFloor.getNextPageParam(below, [below], { before: 0 }, [{ before: 0 }])
    ).toBeUndefined()
    const above = page([row(12), row(11), row(10)], 10)
    expect(
      withFloor.getNextPageParam(above, [above], { before: 0 }, [{ before: 0 }])
    ).toEqual({ before: 10, generation: "gen-1" })
  })
})

describe("seekBoundary", () => {
  /** A feed of rows seq 1..n at T0+seq seconds, with optional gc gaps. */
  function probeOf(seqs: number[]): SeekProbe {
    const rows = seqs.map((s) => row(s)).sort((a, b) => b.seq - a.seq)
    return async (maxSeq) => rows.find((r) => r.seq <= maxSeq)
  }

  it("finds the page boundary for an instant inside the feed", async () => {
    const seqs = Array.from({ length: 100 }, (_, i) => i + 1)
    const probe = probeOf(seqs)
    const head = await probe(Infinity)
    expect(await seekBoundary(probe, head, T0 + 42_000)).toBe(43)
  })

  it("answers 0 (read from head) when the instant covers everything", async () => {
    const probe = probeOf([1, 2, 3])
    const head = await probe(Infinity)
    expect(await seekBoundary(probe, head, T0 + 60_000)).toBe(0)
  })

  it("yields an empty page when the instant predates the feed", async () => {
    const probe = probeOf([5, 6, 7])
    const head = await probe(Infinity)
    const before = await seekBoundary(probe, head, T0)
    expect(before).toBeGreaterThan(0)
    expect(await probe(before - 1)).toBeUndefined()
  })

  it("steps over gc gaps", async () => {
    const probe = probeOf([1, 2, 50, 51, 90])
    const head = await probe(Infinity)
    expect(await seekBoundary(probe, head, T0 + 50_000)).toBe(51)
  })

  it("an empty feed seeks to the head", async () => {
    expect(await seekBoundary(async () => undefined, undefined, T0)).toBe(0)
  })
})

describe("parseWatchLine", () => {
  it("reads a change row", () => {
    const line = JSON.stringify(row(7))
    expect(parseWatchLine(line)?.row?.seq).toBe(7)
  })

  it("reads the leading bookmark", () => {
    expect(parseWatchLine('{"bookmark":32700}')?.bookmark).toBe(32700)
  })

  it("reads the history generation beside the bookmark", () => {
    const line = parseWatchLine('{"bookmark":32700,"generation":"7f3a0c2e"}')
    expect(line).toEqual({ bookmark: 32700, generation: "7f3a0c2e" })
  })

  it("reads the terminal error control frame", () => {
    const line = JSON.stringify({
      error: { code: "compacted", message: "below the horizon", problems: [] },
    })
    expect(parseWatchLine(line)?.error).toEqual({
      code: "compacted",
      message: "below the horizon",
      problems: [],
    })
  })

  it("skips heartbeats, blanks and garbage without throwing", () => {
    expect(parseWatchLine("{}")).toBeNull()
    expect(parseWatchLine("   ")).toBeNull()
    expect(parseWatchLine("not json")).toBeNull()
    expect(parseWatchLine('"just a string"')).toBeNull()
  })
})

describe("values", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => {
    resetValuesSupport()
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })
  const page: ChangePage = { changes: [], head: 0, generation: "g" }

  it("asks with values=1 and keeps it out of the facets it does not narrow", () => {
    expect(changesSearch({ values: true }).get("values")).toBe("1")
    expect(changesSearch({}).has("values")).toBe(false)
  })

  it("retries without values against a server that refuses the parameter, and remembers", async () => {
    fetchMock
      .mockResolvedValueOnce(
        new Response(
          JSON.stringify({
            error: {
              code: "bad_request",
              message: 'unknown query parameter "values"',
            },
          }),
          { status: 400 }
        )
      )
      .mockImplementation(() =>
        Promise.resolve(new Response(JSON.stringify(page), { status: 200 }))
      )
    const got = await fetchChangesPage({ filter: { values: true } })
    expect(got).toEqual(page)
    const urls = fetchMock.mock.calls.map(([url]) => String(url))
    expect(urls[0]).toContain("values=1")
    expect(urls[1]).not.toContain("values")
    await fetchChangesPage({ filter: { values: true } })
    expect(String(fetchMock.mock.calls[2][0])).not.toContain("values")
  })

  it("does not swallow any other refusal", async () => {
    fetchMock.mockResolvedValueOnce(
      new Response(
        JSON.stringify({
          error: {
            code: "bad_request",
            message: "recordId requires recordKind",
          },
        }),
        { status: 400 }
      )
    )
    await expect(
      fetchChangesPage({ filter: { values: true } })
    ).rejects.toThrow("recordId")
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })
})

describe("runs", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => {
    resetRunsSupport()
    resetValuesSupport()
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })
  const runs: ChangeRunPage = {
    runs: [
      {
        actor: "console",
        kind: "samples.substrate.reamde.dev/tasks/task",
        verb: "create",
        count: 60,
        records: 60,
        newestSeq: 90,
        oldestSeq: 31,
        newestTs: "2026-09-27T10:00:00Z",
        oldestTs: "2026-09-27T09:00:00Z",
      },
    ],
    cursor: 31,
    head: 95,
    generation: "g",
  }
  const answer = (body: unknown, status = 200) =>
    Promise.resolve(new Response(JSON.stringify(body), { status }))

  it("asks for runs, counted by first, never with values", async () => {
    fetchMock.mockImplementation(() => answer(runs))
    const got = await fetchHistoryPage({
      first: 6,
      before: 40,
      generation: "g",
      filter: { actors: ["console"], excludeKinds: ["x.example.com/a/b"] },
    })
    expect(got).toEqual(runs)
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://x")
    expect(url.searchParams.get("runs")).toBe("1")
    expect(url.searchParams.get("first")).toBe("6")
    expect(url.searchParams.get("before")).toBe("40")
    expect(url.searchParams.get("generation")).toBe("g")
    expect(url.searchParams.getAll("excludeKinds")).toEqual([
      "x.example.com/a/b",
    ])
    expect(url.searchParams.has("values")).toBe(false)
  })

  it("reads rows instead from a server without runs, and remembers", async () => {
    const page: ChangePage = { changes: [row(3)], head: 3, generation: "g" }
    fetchMock
      .mockImplementationOnce(() =>
        answer(
          {
            error: {
              code: "bad_request",
              message: 'unknown query parameter "runs"',
            },
          },
          400
        )
      )
      .mockImplementation(() => answer(page))
    expect(await fetchHistoryPage({ first: 6, rows: 60 })).toEqual(page)
    const urls = fetchMock.mock.calls.map(([u]) => String(u))
    expect(urls[0]).toContain("runs=1")
    expect(urls[1]).not.toContain("runs")
    expect(urls[1]).toContain("first=60")
    await fetchHistoryPage({ first: 6, rows: 60 })
    expect(fetchMock).toHaveBeenCalledTimes(3)
    expect(String(fetchMock.mock.calls[2][0])).not.toContain("runs")
  })

  it("reads a run's rows under the page filter narrowed to the run", async () => {
    fetchMock.mockImplementation(() =>
      answer({ changes: [row(9), row(8), row(7)], head: 95, generation: "g" })
    )
    const options = runRowsQueryOptions({
      actor: "console",
      kind: "samples.substrate.reamde.dev/tasks/task",
      recordId: "t1",
      newestSeq: 9,
      oldestSeq: 8,
      count: 2,
      generation: "g",
      filter: {
        actors: ["console", "api"],
        q: "x",
        excludeKinds: ["substrate.reamde.dev/core/token"],
        excludeOps: ["gc"],
      },
      values: true,
    })
    const rows = await options.queryFn!({
      signal: new AbortController().signal,
    } as never)
    // A row below the run is not the run's.
    expect(rows.map((r) => r.seq)).toEqual([9, 8])
    const url = new URL(String(fetchMock.mock.calls[0][0]), "http://x")
    expect(url.searchParams.get("before")).toBe("10")
    expect(url.searchParams.get("first")).toBe("2")
    expect(url.searchParams.getAll("actors")).toEqual(["console"])
    expect(url.searchParams.getAll("kinds")).toEqual([
      "samples.substrate.reamde.dev/tasks/task",
    ])
    expect(url.searchParams.get("recordId")).toBe("t1")
    expect(url.searchParams.get("q")).toBe("x")
    expect(url.searchParams.get("values")).toBe("1")
    expect(url.searchParams.has("excludeKinds")).toBe(false)
    expect(url.searchParams.getAll("excludeOps")).toEqual(["gc"])
  })
})
