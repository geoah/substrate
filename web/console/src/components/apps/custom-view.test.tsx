// @vitest-environment jsdom
/** The custom layout's gate: the document is mounted (the `substrate/mount`
 * post with the source and a port) only when the view's single-record read
 * shows `source`, `permissions`, `kind` and `filter` at owner tier; below
 * that the frame is not there, the review line is, and nothing is posted. */

import { act, cleanup, render, screen } from "@testing-library/react"
import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, Page, SubstrateRecord } from "@/lib/api/types"
import { MOUNT_TYPE, READY_TYPE } from "@/lib/apps/bridge/protocol"
import { viewSpec } from "@/lib/apps/view-spec"
import { CustomView } from "./custom-view"

vi.mock("@/hooks/use-live-records", () => ({ useLiveRecords: () => {} }))

const TASK = "ada.example.com/tasks/task"
const SOURCE =
  '<p id="x">hi</p><script type="module">import "substrate/app"</script>'

const taskKind: KindInfo = {
  identity: TASK,
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    properties: {
      title: { type: "string" },
      status: { type: "state", states: ["todo", "done"] },
      completedAt: { type: "datetime" },
    },
  },
}

const owner = { manager: "substratectl", tier: "owner" as const }

function viewRecord(permissionsTier: "owner" | "machine"): SubstrateRecord {
  return {
    id: "tasks-heatmap",
    kind: "substrate.reamde.dev/core/view",
    properties: {
      name: "Done this quarter",
      description: "Done tasks per completion day.",
      layout: "custom",
      kind: { ref: `substrate.reamde.dev/core/kind/${TASK}` },
      filter: { properties: { status: { eq: "done" } } },
      permissions: {
        reads: { kinds: [{ ref: `substrate.reamde.dev/core/kind/${TASK}` }] },
      },
      source: SOURCE,
    },
    labels: {},
    version: 1,
    createdAt: "2026-09-12T00:00:00Z",
    updatedAt: "2026-09-12T00:00:00Z",
    propertyMeta: {
      name: owner,
      layout: owner,
      kind: owner,
      filter: owner,
      permissions: { ...owner, tier: permissionsTier },
      source: owner,
    },
  }
}

const page: Page = { records: [], head: 1, generation: "g" }

function serve(record: SubstrateRecord) {
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input instanceof Request ? input.url : input)
    const body = url.includes("/core/view/")
      ? record
      : url.includes("/tasks/task")
        ? page
        : undefined
    return new Response(
      JSON.stringify(body ?? { error: { code: "not_found", message: url } }),
      {
        status: body ? 200 : 404,
        headers: { "content-type": "application/json" },
      }
    )
  })
}

function mount(record: SubstrateRecord) {
  const spec = viewSpec(record, [taskKind])
  const onOpenRecord = vi.fn()
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: () => (
      <CustomView
        spec={spec}
        kind={taskKind}
        kinds={[taskKind]}
        ctx={{ inputs: {}, mode: "page" }}
        onOpenRecord={onOpenRecord}
      />
    ),
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
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
  return { spec }
}

beforeEach(() => {
  localStorage.setItem("substrate.token", "t")
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("the custom layout's gate", () => {
  it("mounts the document at owner provenance: the source and a port, posted once the shell is ready", async () => {
    vi.stubGlobal("fetch", serve(viewRecord("owner")))
    mount(viewRecord("owner"))
    const frame = (await screen.findByTitle(
      "Done this quarter"
    )) as HTMLIFrameElement
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts")
    expect(frame.src).toContain("/app-frame.html")
    const posted = vi.fn()
    frame.contentWindow!.postMessage = posted

    // A ready message from another window is not the shell's.
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { type: READY_TYPE },
          source: window,
        })
      )
    })
    expect(posted).not.toHaveBeenCalled()

    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { type: READY_TYPE },
          source: frame.contentWindow,
        })
      )
    })
    expect(posted).toHaveBeenCalledTimes(1)
    const [message, target, transfer] = posted.mock.calls[0] as [
      { type: string; html: string },
      string,
      MessagePort[],
    ]
    expect(message).toEqual({ type: MOUNT_TYPE, html: SOURCE })
    expect(target).toBe("*")
    expect(transfer[0]).toBeInstanceOf(MessagePort)

    // The post is armed once per navigation the host made: a second ready,
    // as from a document the guest navigated its frame to, gets nothing.
    act(() => {
      window.dispatchEvent(
        new MessageEvent("message", {
          data: { type: READY_TYPE },
          source: frame.contentWindow,
        })
      )
    })
    expect(posted).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/needs the owner's review/)).toBeNull()
  })

  it("withholds the mount below owner provenance and says the view needs review", async () => {
    vi.stubGlobal("fetch", serve(viewRecord("machine")))
    mount(viewRecord("machine"))
    await screen.findByText(/needs the owner's review/)
    expect(document.querySelector("iframe")).toBeNull()
    expect(screen.getByText(/Done tasks per completion day/)).toBeTruthy()
    expect(
      screen.getByRole("link", { name: "Open source" }).getAttribute("href")
    ).toContain("/data/substrate.reamde.dev/core/view/tasks-heatmap")
    // No page was read for a document that will not run.
    const calls = (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.map(
      (c) => String(c[0])
    )
    expect(calls.some((u) => u.includes("/tasks/task"))).toBe(false)
  })
})
