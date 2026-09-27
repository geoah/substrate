// @vitest-environment jsdom
/** The tree hook against a faked list route: one read per level naming every
 * parent on it, rows opening onto their children, and a flat page when the
 * kind nests by nothing. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { DeclaredProperty } from "@/lib/definition"

import { useRecordTree } from "./use-record-tree"

const TEAM = "acme.example.com/people/team"

const team: KindInfo = {
  identity: TEAM,
  name: "team",
  authority: "acme.example.com",
  package: "people",
  version: 1,
  source: "installed",
  description: "",
  definition: {},
}

const parent: DeclaredProperty = {
  name: "parent",
  kind: "reference",
  repeated: false,
  to: "team",
}

function record(id: string, parentId?: string): SubstrateRecord {
  return {
    id,
    kind: TEAM,
    properties: parentId
      ? { name: id, parent: { ref: `${TEAM}/${parentId}` } }
      : { name: id },
    labels: {},
    version: 1,
    createdAt: "2026-09-22T00:00:00Z",
    updatedAt: "2026-09-22T00:00:00Z",
  }
}

const ALL = [
  record("engineering"),
  record("design"),
  record("platform", "engineering"),
  record("product", "engineering"),
  record("infra", "platform"),
]

/** The list route, answering `parent in [...]` from ALL. */
function answer(input: RequestInfo | URL): Response {
  const url = new URL(String(input), "http://console.test")
  const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
    properties?: { parent?: { in?: string[] } }
  }
  const parents = filter.properties?.parent?.in ?? []
  const records = ALL.filter((r) => {
    const held = r.properties.parent as { ref?: string } | undefined
    return held?.ref !== undefined && parents.includes(held.ref)
  })
  return new Response(
    JSON.stringify({
      records,
      head: 1,
      generation: "g",
      included: { [`${TEAM}/engineering`]: ALL[0] },
    }),
    { status: 200 }
  )
}

function filterOf(call: unknown): unknown {
  const url = new URL(String(call), "http://console.test")
  return JSON.parse(url.searchParams.get("filter") ?? "{}")
}

function wrapper() {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return ({ children }: { children: React.ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
}

describe("useRecordTree", () => {
  const fetchMock = vi.fn<typeof fetch>()

  beforeEach(() => {
    vi.stubGlobal("fetch", fetchMock)
    fetchMock.mockImplementation(async (input) => answer(input))
  })

  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
    fetchMock.mockReset()
  })

  it("reads one level at a time and opens rows onto their children", async () => {
    const roots = [ALL[0], ALL[1]]
    const { result } = renderHook(
      () =>
        useRecordTree({
          kind: team,
          property: parent,
          roots,
          filter: undefined,
          orderBy: "name:asc",
          expand: ["parent"],
        }),
      { wrapper: wrapper() }
    )
    expect(result.current.active).toBe(true)
    expect(result.current.rows.map((r) => r.id)).toEqual([
      "engineering",
      "design",
    ])

    await waitFor(() =>
      expect(result.current.nodes.get("engineering")?.children).toBe("some")
    )
    expect(result.current.nodes.get("design")?.children).toBe("none")

    // One read for the whole level, naming both roots, in the table's own
    // order and expansion, at the server's page cap.
    expect(fetchMock).toHaveBeenCalledTimes(1)
    const url = new URL(
      String(fetchMock.mock.calls[0][0]),
      "http://console.test"
    )
    expect(url.pathname).toBe("/api/v1/records")
    expect(filterOf(fetchMock.mock.calls[0][0])).toEqual({
      kinds: [TEAM],
      properties: {
        parent: { in: [`${TEAM}/engineering`, `${TEAM}/design`] },
      },
    })
    expect(url.searchParams.get("first")).toBe("500")
    expect(url.searchParams.get("orderBy")).toBe("name:asc")
    expect(url.searchParams.get("expand")).toBe("parent")

    act(() => result.current.toggle("engineering"))
    await waitFor(() =>
      expect(result.current.rows.map((r) => r.id)).toEqual([
        "engineering",
        "platform",
        "product",
        "design",
      ])
    )
    expect(result.current.nodes.get("platform")).toMatchObject({ depth: 1 })
    // Opening a level asks the next one whole, so the chevrons under it are
    // right before anyone clicks.
    await waitFor(() =>
      expect(result.current.nodes.get("platform")?.children).toBe("some")
    )
    expect(result.current.nodes.get("product")?.children).toBe("none")
    expect(filterOf(fetchMock.mock.calls[1][0])).toEqual({
      kinds: [TEAM],
      properties: {
        parent: { in: [`${TEAM}/platform`, `${TEAM}/product`] },
      },
    })
    // What the level reads carried in `included` reaches the table.
    expect(result.current.included[`${TEAM}/engineering`]).toBeDefined()

    act(() => result.current.toggle("engineering"))
    expect(result.current.rows.map((r) => r.id)).toEqual([
      "engineering",
      "design",
    ])
  })

  it("is the flat page, untouched, when the kind nests by nothing", () => {
    const roots = [ALL[0]]
    const { result } = renderHook(
      () =>
        useRecordTree({
          kind: team,
          property: undefined,
          roots,
          filter: undefined,
          orderBy: "name:asc",
          expand: [],
        }),
      { wrapper: wrapper() }
    )
    expect(result.current.active).toBe(false)
    expect(result.current.rows).toBe(roots)
    expect(fetchMock).not.toHaveBeenCalled()
  })

  /** Filtered: the page is the matches, one more read asks which of the
   * parents it names match, and each match is drawn once, under its parent
   * where that parent matches, else on top with its parent as context. */
  it("nests the matches of a filter under the matches they belong to", async () => {
    const research = record("research", "design")
    const matches = new Set(["engineering", "platform", "infra", "research"])
    fetchMock.mockImplementation(async (input) => {
      const url = new URL(String(input), "http://console.test")
      const filter = JSON.parse(url.searchParams.get("filter") ?? "{}") as {
        ids?: string[]
        properties?: { parent?: { in?: string[] } }
      }
      let records = [...ALL, research].filter((r) => matches.has(r.id))
      if (filter.ids)
        records = records.filter((r) => filter.ids!.includes(r.id))
      const parents = filter.properties?.parent?.in
      if (parents) {
        records = records.filter((r) => {
          const held = r.properties.parent as { ref?: string } | undefined
          return held?.ref !== undefined && parents.includes(held.ref)
        })
      }
      return new Response(
        JSON.stringify({ records, head: 1, generation: "g" }),
        { status: 200 }
      )
    })
    const page = [ALL[4], research, ALL[0], ALL[2]] // infra, research, engineering, platform
    const { result } = renderHook(
      () =>
        useRecordTree({
          kind: team,
          property: parent,
          roots: page,
          filter: { search: "ops" },
          filtered: true,
          orderBy: "name:asc",
          expand: [],
        }),
      { wrapper: wrapper() }
    )
    // Nothing is drawn until the top level is known.
    expect(result.current.loading).toBe(true)
    expect(result.current.rows).toEqual([])

    await waitFor(() =>
      expect(result.current.rows.map((r) => r.id)).toEqual([
        "research",
        "engineering",
        "platform",
        "infra",
      ])
    )
    expect(result.current.loading).toBe(false)
    expect(result.current.nodes.get("infra")?.depth).toBe(2)
    expect([...result.current.context]).toEqual([
      ["research", `${TEAM}/design`],
    ])
    // The parents' read carries the view's own filter, narrowed to the ids.
    expect(filterOf(fetchMock.mock.calls[0][0])).toEqual({
      kinds: [TEAM],
      search: "ops",
      ids: ["platform", "design", "engineering"],
    })

    // A toggle on a row that opened by itself closes it.
    act(() => result.current.toggle("engineering"))
    expect(result.current.rows.map((r) => r.id)).toEqual([
      "research",
      "engineering",
    ])
  })
})
