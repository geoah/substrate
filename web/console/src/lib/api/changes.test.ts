import { describe, expect, it } from "vitest"

import {
  changesInfiniteOptions,
  changesSearch,
  parseWatchLine,
  seekBoundary,
  type ChangesPage,
  type SeekProbe,
} from "./changes"
import type { ChangeRow } from "./types"

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
  const page = (changes: ChangeRow[], cursor?: number): ChangesPage => ({
    changes,
    cursor,
    generation: "gen-1",
  })
  const next = (p: ChangesPage) =>
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
