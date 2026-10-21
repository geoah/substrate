// @vitest-environment jsdom
/** The unit reader a component takes: the engine's table until the server
 * says its switch is on, then the repository's graph, dropped and fetched
 * again when the change stream names a unit or conversion record. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { forgetUnitGraphs } from "@/lib/api/units"
import type { ChangeRow } from "@/lib/api/types"
import { fixtureTable } from "@/lib/unit-graph.test-fixtures"

const watch = vi.hoisted(() => ({
  calls: [] as {
    filter?: { kinds?: string[] }
    onRow: (row: ChangeRow) => void
  }[],
}))

vi.mock("@/lib/api/changes", () => ({
  watchChanges: (opts: {
    filter?: { kinds?: string[] }
    onRow: (row: ChangeRow) => void
  }) => {
    watch.calls.push(opts)
    return { stop: () => undefined }
  },
}))

import { useUnitReader } from "./use-unit-reader"

function serve(graphOn: boolean, revisions: number[]) {
  let served = 0
  const fetch = vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes("server.json")) {
      return Promise.resolve(
        new Response(JSON.stringify({ units: { graph: graphOn } }), {
          status: 200,
        })
      )
    }
    const revision = revisions[Math.min(served, revisions.length - 1)]
    served++
    return Promise.resolve(
      new Response(
        JSON.stringify({
          ...fixtureTable,
          revision,
          measures: [],
          aliases: {},
          unseeded: [],
        }),
        { status: 200 }
      )
    )
  })
  vi.stubGlobal("fetch", fetch)
  return fetch
}

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
}

afterEach(() => {
  cleanup()
  forgetUnitGraphs()
  watch.calls = []
  vi.unstubAllGlobals()
})

describe("useUnitReader", () => {
  it("reads the table while the switch is off, and never fetches the graph", async () => {
    const fetch = serve(false, [1])
    const { result } = renderHook(() => useUnitReader(), { wrapper: wrapper() })
    await waitFor(() => expect(fetch).toHaveBeenCalled())
    expect(result.current.graph).toBeUndefined()
    expect(result.current.canonical("lb")).toBe("[lb_av]")
    expect(fetch.mock.calls.some((c) => String(c[0]).endsWith("/units"))).toBe(
      false
    )
    expect(watch.calls).toHaveLength(0)
  })

  it("reads the graph once the switch is on, and drops it on a unit change", async () => {
    serve(true, [1, 2])
    const { result } = renderHook(() => useUnitReader(), { wrapper: wrapper() })
    await waitFor(() => expect(result.current.graph?.revision).toBe(1))
    expect(result.current.canonical("[lb_av]")).toBe("lb")
    expect(watch.calls.at(-1)?.filter?.kinds).toEqual([
      "substrate.reamde.dev/core/unit",
      "substrate.reamde.dev/core/conversion",
    ])
    watch.calls.at(-1)!.onRow({
      seq: 9,
      ts: "2026-10-21T00:00:00Z",
      actor: "api",
      op: "put",
      recordId: "lb-g",
      kind: "substrate.reamde.dev/core/conversion",
    })
    await waitFor(() => expect(result.current.graph?.revision).toBe(2))
  })
})
