import { afterEach, describe, expect, it } from "vitest"

import {
  authorityCountsQueryOptions,
  recentChangesQueryOptions,
  RECENT_CHANGES_PAGE,
  Semaphore,
} from "./overview"
import type { ChangeRow, KindInfo } from "./types"

function deferred() {
  let resolve!: () => void
  const promise = new Promise<void>((r) => {
    resolve = r
  })
  return { promise, resolve }
}

describe("Semaphore", () => {
  it("never holds more than `limit` tasks in flight", async () => {
    const gate = new Semaphore(3)
    let inFlight = 0
    let peak = 0
    const gates = Array.from({ length: 7 }, deferred)
    const run = Promise.all(
      gates.map((g) =>
        gate.run(async () => {
          inFlight++
          peak = Math.max(peak, inFlight)
          await g.promise
          inFlight--
        })
      )
    )
    await Promise.resolve()
    expect(inFlight).toBe(3)
    for (const g of gates) g.resolve()
    await run
    expect(peak).toBe(3)
  })

  it("returns each task's own result", async () => {
    const gate = new Semaphore(2)
    const out = await Promise.all(
      [3, 1, 2].map((n) =>
        gate.run(async () => {
          await new Promise((r) => setTimeout(r, n))
          return n * 10
        })
      )
    )
    expect(out).toEqual([30, 10, 20])
  })

  it("releases the slot when a task throws, and keeps serving", async () => {
    const gate = new Semaphore(1)
    await expect(
      gate.run(async () => {
        throw new Error("boom")
      })
    ).rejects.toThrow("boom")
    expect(await gate.run(async () => "alive")).toBe("alive")
  })
})

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
      kind: "tasks.substrate.reamde.dev/task",
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

function kindInfo(name: string, authority: string): KindInfo {
  return {
    identity: `${authority}/${name}`,
    name,
    authority,
    version: 0,
    plural: `${name}s`,
    source: "schema",
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
      ["person", "task"],
    ])
  })

  it("caches for minutes — the dashboard glances, it does not audit", () => {
    const opts = authorityCountsQueryOptions("acme.dev", [])
    expect(opts.staleTime).toBeGreaterThanOrEqual(5 * 60_000)
  })

  it("walks every kind through the shared gate, bounded and in order", async () => {
    const kinds = [
      kindInfo("a", "g.dev"),
      kindInfo("b", "g.dev"),
      kindInfo("c", "g.dev"),
      kindInfo("d", "g.dev"),
    ]
    const gate = new Semaphore(1)
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
      const opts = authorityCountsQueryOptions("g.dev", kinds, gate)
      const out = await opts.queryFn!({ signal: undefined } as never)
      expect(out.map((c) => c.kind.name)).toEqual(["a", "b", "c", "d"])
      expect(out.every((c) => c.count?.value === 0 && !c.count.capped)).toBe(
        true
      )
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
    const opts = authorityCountsQueryOptions(
      "core.dev",
      kinds,
      new Semaphore(2)
    )
    const out = await opts.queryFn!({ signal: undefined } as never)
    expect(out).toEqual([
      { kind: kinds[1], count: { value: 0, capped: false } }, // actor sorts first
      { kind: kinds[0] }, // tenant: refused, no count — never the authority's failure
    ])
  })
})
