import { afterEach, describe, expect, it } from "vitest"

import {
  authorityCountsQueryOptions,
  recentChangesQueryOptions,
  RECENT_CHANGES_PAGE,
} from "./overview"
import type { ChangeRow, KindInfo } from "./types"

// ── the activity read: one flat page ────────────────────────────────────────

const realFetch = globalThis.fetch

afterEach(() => {
  globalThis.fetch = realFetch
})

/** Newest-first rows below `belowSeq`. */
function rowsBelow(belowSeq: number, n: number): ChangeRow[] {
  return Array.from({ length: n }, (_, i) => {
    const seq = belowSeq - 1 - i
    return {
      seq,
      ts: new Date(Date.parse("2026-08-06T12:00:00Z") - i * 1000).toISOString(),
      actor: `actor-${i % 5}`,
      op: "put",
      recordId: `e${seq}`,
      kind: "samples.substrate.reamde.dev/tasks/task",
    }
  })
}

function serveChanges(feed: (before: number) => ChangeRow[]): string[] {
  const calls: string[] = []
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = new URL(String(input), "http://test")
    calls.push(url.search)
    const before = Number(url.searchParams.get("before") ?? 0)
    const first = Number(url.searchParams.get("first"))
    return new Response(
      JSON.stringify({ changes: feed(before).slice(0, first) }),
      { status: 200 }
    )
  }) as typeof fetch
  return calls
}

describe("recentChangesQueryOptions", () => {
  const run = () =>
    recentChangesQueryOptions().queryFn!({ signal: undefined } as never)

  it("reads exactly one page of the newest changes (the card is flat now)", async () => {
    const calls = serveChanges((before) => rowsBelow(before || 10_000, 500))
    const rows = (await run()) as ChangeRow[]
    expect(calls).toHaveLength(1)
    expect(calls[0]).toContain(`first=${RECENT_CHANGES_PAGE}`)
    expect(rows).toHaveLength(RECENT_CHANGES_PAGE)
  })

  it("serves a short feed whole", async () => {
    const calls = serveChanges(() => rowsBelow(10, 9))
    const rows = (await run()) as ChangeRow[]
    expect(calls).toHaveLength(1)
    expect(rows).toHaveLength(9)
  })
})

function kindInfo(name: string, authority: string, pkg = "data"): KindInfo {
  return {
    identity: `${authority}/${pkg}/${name}`,
    name,
    authority,
    package: pkg,
    version: 0,
    source: "schema",
    description: "",
    definition: {},
  }
}

describe("authorityCountsQueryOptions", () => {
  it("keys on the authority's own name-sorted kinds only", () => {
    const kinds = [
      kindInfo("task", "acme.dev"),
      kindInfo("person", "acme.dev"),
      kindInfo("issue", "other.dev"),
    ]
    const opts = authorityCountsQueryOptions("acme.dev", kinds)
    expect(opts.queryKey).toEqual([
      "overview",
      "counts",
      "acme.dev",
      ["data/person", "data/task"],
    ])
  })

  it("caches for minutes — the dashboard glances, it does not audit", () => {
    const opts = authorityCountsQueryOptions("acme.dev", [])
    expect(opts.staleTime).toBeGreaterThanOrEqual(5 * 60_000)
  })

  it("probes one kind at a time, in order — a zone holds one connection", async () => {
    const kinds = [
      kindInfo("a", "g.dev"),
      kindInfo("b", "g.dev"),
      kindInfo("c", "g.dev"),
      kindInfo("d", "g.dev"),
    ]
    let inFlight = 0
    let peak = 0
    const savedFetch = globalThis.fetch
    globalThis.fetch = (async () => {
      inFlight++
      peak = Math.max(peak, inFlight)
      await new Promise((r) => setTimeout(r, 1))
      inFlight--
      return new Response(JSON.stringify({ records: [] }), { status: 200 })
    }) as typeof fetch
    try {
      const opts = authorityCountsQueryOptions("g.dev", kinds)
      const out = await opts.queryFn!({ signal: undefined } as never)
      expect(out.map((c) => c.kind.name)).toEqual(["a", "b", "c", "d"])
      expect(out.every((c) => c.count?.value === 0 && !c.count.capped)).toBe(
        true
      )
      // Four kinds, never two probes at once: the walk is the ceiling.
      expect(peak).toBe(1)
    } finally {
      globalThis.fetch = savedFetch
    }
  })

  it("keeps counting when the substrate refuses one collection", async () => {
    const kinds = [
      kindInfo("tenant", "core.dev"),
      kindInfo("actor", "core.dev"),
    ]
    globalThis.fetch = (async (input: RequestInfo | URL) => {
      if (String(input).includes("/tenant")) {
        return new Response(
          JSON.stringify({ error: { code: "forbidden", message: "no" } }),
          { status: 403 }
        )
      }
      return new Response(JSON.stringify({ records: [] }), { status: 200 })
    }) as typeof fetch
    const opts = authorityCountsQueryOptions("core.dev", kinds)
    const out = await opts.queryFn!({ signal: undefined } as never)
    expect(out).toEqual([
      { kind: kinds[1], count: { value: 0, capped: false } }, // actor sorts first
      { kind: kinds[0] }, // tenant: refused, no count — never the authority's failure
    ])
  })
})
