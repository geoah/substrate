// @vitest-environment jsdom
/** A card passes the gate the screen passes: a `requiresAtLeast` floor the
 * repository is below is refused on the overview and in a browse tab exactly
 * as at `/apps/$id`, and nothing mounts before the package versions have
 * landed, because a card is where an app mounts on its own and a floor the
 * card skipped is a floor that guards nothing. */

import { cleanup, render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterProvider,
} from "@tanstack/react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { AppCard } from "./app-card"

vi.mock("@/hooks/use-live-records", () => ({ useLiveRecords: () => {} }))

const TASK = "ada.example.com/tasks/task"
const owner = { manager: "substratectl", tier: "owner" as const }

const taskKind: KindInfo = {
  identity: TASK,
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: { properties: { name: { type: "string" } } },
}

function appRecord(over: Record<string, unknown> = {}): SubstrateRecord {
  return {
    id: "tasks",
    kind: "substrate.reamde.dev/core/app",
    properties: {
      name: "Tasks",
      runtime: "react",
      source: "export default function Tasks() { return null }",
      permissions: {
        reads: { kinds: [{ ref: `substrate.reamde.dev/core/kind/${TASK}` }] },
      },
      attach: ["home", "browse"],
      ...over,
    },
    labels: {},
    version: 1,
    createdAt: "2026-09-12T00:00:00Z",
    updatedAt: "2026-09-12T00:00:00Z",
    propertyMeta: {
      name: owner,
      runtime: owner,
      source: owner,
      permissions: owner,
      requiresAtLeast: owner,
    },
  }
}

/** The row as the list serves it: no `propertyMeta`. */
function listRow(record: SubstrateRecord): SubstrateRecord {
  const row = { ...record }
  delete row.propertyMeta
  return row
}

const packageRow = (id: string, version: number): SubstrateRecord => ({
  id,
  kind: "substrate.reamde.dev/core/package",
  properties: { version },
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

const json = (body: unknown) =>
  new Response(JSON.stringify(body), { status: 200 })

describe("AppCard", () => {
  const fetchMock = vi.fn<typeof fetch>()
  beforeEach(() => {
    fetchMock.mockReset()
    vi.stubGlobal("fetch", fetchMock)
  })
  afterEach(() => {
    cleanup()
    vi.unstubAllGlobals()
  })

  function serve(
    app: SubstrateRecord,
    packages: SubstrateRecord[] | Promise<SubstrateRecord[]>
  ) {
    fetchMock.mockImplementation(async (url) => {
      const path = new URL(String(url), "http://console").pathname
      if (path === "/api/v1/substrate.reamde.dev/core/kind") {
        return json({ kinds: [taskKind], head: 1, generation: "g" })
      }
      if (path === "/api/v1/substrate.reamde.dev/core/app/tasks") {
        return json(app)
      }
      if (path === "/api/v1/substrate.reamde.dev/core/package") {
        return json({ records: await packages, head: 1, generation: "g" })
      }
      return json({ records: [], head: 1, generation: "g" })
    })
  }

  function mount(app: SubstrateRecord, at: "home" | "browse") {
    const rootRoute = createRootRoute({
      component: () => <AppCard app={listRow(app)} at={at} />,
    })
    const router = createRouter({
      routeTree: rootRoute,
      history: createMemoryHistory({ initialEntries: ["/"] }),
    })
    const client = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    })
    render(
      <QueryClientProvider client={client}>
        <RouterProvider router={router} />
      </QueryClientProvider>
    )
  }

  const settle = () => new Promise((r) => setTimeout(r, 40))
  const floored = () =>
    appRecord({ requiresAtLeast: { "ada.example.com/tasks": 5 } })

  it("refuses a floor the repository is below on the overview, as the screen does", async () => {
    serve(floored(), [packageRow("ada.example.com/tasks", 3)])
    mount(floored(), "home")
    expect(
      await screen.findByText(
        /holds ada.example.com\/tasks at version 3; the app needs 5/
      )
    ).toBeTruthy()
    expect(document.querySelector("iframe")).toBeNull()
  })

  it("refuses the same floor in a browse tab", async () => {
    serve(floored(), [packageRow("ada.example.com/tasks", 3)])
    mount(floored(), "browse")
    expect(await screen.findByText(/the app needs 5/)).toBeTruthy()
    expect(document.querySelector("iframe")).toBeNull()
  })

  it("mounts nothing before the package versions land, and the frame once they clear the floor", async () => {
    let land: (rows: SubstrateRecord[]) => void = () => {}
    const packages = new Promise<SubstrateRecord[]>((r) => (land = r))
    serve(floored(), packages)
    mount(floored(), "home")
    await settle()
    expect(document.querySelector("iframe")).toBeNull()
    expect(screen.queryByText(/the app needs 5/)).toBeNull()
    land([packageRow("ada.example.com/tasks", 5)])
    expect(await screen.findByTitle("Tasks")).toBeTruthy()
  })
})
