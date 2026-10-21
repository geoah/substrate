// @vitest-environment jsdom
/** The unit reader a component takes: the engine's table where the server
 * says its switch is off, the repository's graph where it is on, and neither
 * while the graph loads or after it failed. The graph is fetched again when
 * the change stream names a unit or conversion record, when the watch opens,
 * after a compaction and when the reader mounts again. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { forgetUnitGraphs } from "@/lib/api/units"
import type { ChangeRow } from "@/lib/api/types"
import { fixtureTable } from "@/lib/unit-graph.test-fixtures"

interface Watch {
  filter?: { kinds?: string[] }
  onRow: (row: ChangeRow) => void
  onStatus?: (status: string) => void
  onCompacted?: () => void
  stopped: boolean
}

const watch = vi.hoisted(() => ({ calls: [] as Watch[] }))

vi.mock("@/lib/api/changes", () => ({
  watchChanges: (opts: Omit<Watch, "stopped">) => {
    const held: Watch = { ...opts, stopped: false }
    watch.calls.push(held)
    return {
      stop: () => {
        held.stopped = true
      },
    }
  },
}))

import { useUnitReader } from "./use-unit-reader"

/** A server whose switch is `graphOn` and whose graph is at `revision`,
 * which a test moves; `units` answers the graph route in its place. */
function serve(
  graphOn: boolean,
  revision = 1,
  units?: () => Promise<Response>
) {
  const server = { revision, fetched: 0 }
  const fetch = vi.fn((input: RequestInfo | URL) => {
    const url = String(input)
    if (url.includes("server.json")) {
      return Promise.resolve(
        new Response(JSON.stringify({ units: { graph: graphOn } }), {
          status: 200,
        })
      )
    }
    server.fetched++
    if (units) return units()
    return Promise.resolve(
      new Response(
        JSON.stringify({
          ...fixtureTable,
          revision: server.revision,
          measures: [],
          aliases: {},
          unseeded: [],
        }),
        { status: 200 }
      )
    )
  })
  vi.stubGlobal("fetch", fetch)
  return Object.assign(server, { fetch })
}

function client() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } })
}

function wrapper(held = client()) {
  return ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={held}>{children}</QueryClientProvider>
  )
}

const UNIT_ROW: ChangeRow = {
  seq: 9,
  ts: "2026-10-21T00:00:00Z",
  actor: "api",
  op: "put",
  recordId: "lb-g",
  kind: "substrate.reamde.dev/core/conversion",
}

afterEach(() => {
  cleanup()
  forgetUnitGraphs()
  watch.calls = []
  vi.unstubAllGlobals()
})

describe("useUnitReader", () => {
  it("reads the table while the switch is off, and never fetches the graph", async () => {
    const server = serve(false)
    const { result } = renderHook(() => useUnitReader(), { wrapper: wrapper() })
    await waitFor(() => expect(result.current.status).toBe("table"))
    expect(result.current.graph).toBeUndefined()
    expect(result.current.canonical("lb")).toBe("[lb_av]")
    expect(server.fetched).toBe(0)
    expect(watch.calls).toHaveLength(0)
  })

  it("reads the graph once the switch is on, and drops it on a unit change", async () => {
    const server = serve(true)
    const { result } = renderHook(() => useUnitReader(), { wrapper: wrapper() })
    await waitFor(() => expect(result.current.graph?.revision).toBe(1))
    expect(result.current.status).toBe("ready")
    expect(result.current.canonical("[lb_av]")).toBe("lb")
    expect(watch.calls.at(-1)?.filter?.kinds).toEqual([
      "substrate.reamde.dev/core/unit",
      "substrate.reamde.dev/core/conversion",
    ])
    server.revision = 2
    watch.calls.at(-1)!.onRow(UNIT_ROW)
    await waitFor(() => expect(result.current.graph?.revision).toBe(2))
  })

  it("answers nothing from the table while the graph loads", async () => {
    serve(true, 1, () => new Promise<Response>(() => undefined))
    const { result } = renderHook(() => useUnitReader(), { wrapper: wrapper() })
    expect(result.current.status).toBe("loading")
    await waitFor(() => expect(watch.calls).toHaveLength(1))
    const units = result.current
    expect(units.status).toBe("loading")
    expect(units.graph).toBeUndefined()
    // The table converts these and prints Cel as °C; the pending reader
    // does neither.
    expect(units.convert(1, "kg", "[lb_av]")).toBeUndefined()
    expect(units.symbol("Cel")).toBe("Cel")
    expect(units.unitsFor({ measures: "mass" })).toEqual([])
    expect(units.check({ value: "1", unit: "kg" })).toMatch(/still loading/)
    expect(units.format({ value: "37", unit: "Cel" })).toBe("37 Cel")
  })

  it("says why when the graph fails, and answers nothing from the table", async () => {
    serve(true, 1, () =>
      Promise.resolve(
        new Response(
          JSON.stringify({ error: { code: "internal", message: "boom" } }),
          { status: 500 }
        )
      )
    )
    const { result } = renderHook(() => useUnitReader(), { wrapper: wrapper() })
    await waitFor(() => expect(result.current.status).toBe("error"))
    expect(result.current.error).toBeInstanceOf(Error)
    expect(result.current.convert(1, "kg", "[lb_av]")).toBeUndefined()
    expect(result.current.check({ value: "1", unit: "kg" })).toMatch(
      /did not load/
    )
  })

  it("fetches the graph again once the watch is live, for a change made before it opened", async () => {
    const server = serve(true)
    const { result } = renderHook(() => useUnitReader(), { wrapper: wrapper() })
    await waitFor(() => expect(result.current.graph?.revision).toBe(1))
    // Committed after the first fetch and before the watch's start: no row
    // will ever name it.
    server.revision = 2
    watch.calls.at(-1)!.onStatus?.("live")
    await waitFor(() => expect(result.current.graph?.revision).toBe(2))
  })

  it("fetches the graph again after a compaction, and watches from the head again", async () => {
    const server = serve(true)
    const { result } = renderHook(() => useUnitReader(), { wrapper: wrapper() })
    await waitFor(() => expect(result.current.graph?.revision).toBe(1))
    const first = watch.calls.at(-1)!
    server.revision = 2
    first.onCompacted?.()
    await waitFor(() => expect(result.current.graph?.revision).toBe(2))
    await waitFor(() => expect(watch.calls).toHaveLength(2))
    expect(first.stopped).toBe(true)
    expect(watch.calls[1].stopped).toBe(false)
  })

  it("sees a change made while it was unmounted when it mounts again", async () => {
    const server = serve(true)
    const shared = client()
    const first = renderHook(() => useUnitReader(), {
      wrapper: wrapper(shared),
    })
    await waitFor(() => expect(first.result.current.graph?.revision).toBe(1))
    first.unmount()
    expect(watch.calls.at(-1)?.stopped).toBe(true)
    // Another client edits a conversion while nothing watches.
    server.revision = 2
    const again = renderHook(() => useUnitReader(), {
      wrapper: wrapper(shared),
    })
    await waitFor(() => expect(again.result.current.graph?.revision).toBe(2))
  })
})
