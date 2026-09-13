// @vitest-environment jsdom
/** The custom layout's gate, and that ONE READ decides it. The document is
 * mounted (the `substrate/mount` post with the source and a port) only when
 * the view's single-record read shows `source`, `permissions` and `kind` at
 * owner tier, and everything that then runs is taken from THAT read: the
 * source posted, the grant the bridge checks over the port, the page. The
 * spec the layout was mounted with, decoded from some other read of the row,
 * counts for its id alone. Below owner tier the frame is not there, the
 * review line is, and nothing is posted; without a filter the frame is there
 * and no page is read; a collection read newer than the single read has the
 * single read taken again before the mount follows it. */

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

import { viewsQueryOptions } from "@/lib/api/apps"
import type { KindInfo, Page, SubstrateRecord } from "@/lib/api/types"
import {
  Endpoint,
  ERROR,
  METHODS,
  MOUNT_TYPE,
  READY_TYPE,
  TOOLS,
  type CallToolResult,
} from "@/lib/apps/bridge/protocol"
import type { ViewSpec } from "@/lib/apps/spec"
import { viewSpec } from "@/lib/apps/view-spec"
import { CustomView } from "./custom-view"

vi.mock("@/hooks/use-live-records", () => ({ useLiveRecords: () => {} }))

const TASK = "ada.example.com/tasks/task"
const PERSON = "ada.example.com/people/person"
const SOURCE =
  '<p id="x">hi</p><script type="module">import "substrate/app"</script>'
const OTHER_SOURCE = "<p>an agent wrote this</p>"
const NEWER_SOURCE = "<p>the owner's third draft</p>"

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
const personKind: KindInfo = {
  ...taskKind,
  identity: PERSON,
  name: "person",
  package: "people",
}
const kinds = [taskKind, personKind]

const owner = { manager: "substratectl", tier: "owner" as const }
const machine = {
  manager: "agent:ada.example.com:tasks:planner",
  tier: "machine" as const,
}

interface Row {
  version?: number
  name?: string
  source?: string
  reads?: string[]
  /** `false`: the row carries no `filter` property at all. */
  filter?: boolean
  permissionsTier?: typeof owner | typeof machine
}

function kindRef(identity: string) {
  return { ref: `substrate.reamde.dev/core/kind/${identity}` }
}

function viewRecord(o: Row = {}): SubstrateRecord {
  const properties: Record<string, unknown> = {
    name: o.name ?? "Done this quarter",
    description: "Done tasks per completion day.",
    layout: "custom",
    kind: kindRef(TASK),
    permissions: { reads: { kinds: (o.reads ?? [TASK]).map(kindRef) } },
    source: o.source ?? SOURCE,
  }
  const propertyMeta: NonNullable<SubstrateRecord["propertyMeta"]> = {
    name: owner,
    layout: owner,
    kind: owner,
    permissions: o.permissionsTier ?? owner,
    source: owner,
  }
  if (o.filter !== false) {
    properties.filter = { properties: { status: { eq: "done" } } }
    propertyMeta.filter = owner
  }
  return {
    id: "tasks-heatmap",
    kind: "substrate.reamde.dev/core/view",
    properties,
    labels: {},
    version: o.version ?? 1,
    createdAt: "2026-09-12T00:00:00Z",
    updatedAt: "2026-09-12T00:00:00Z",
    propertyMeta,
  }
}

const page: Page = { records: [], head: 1, generation: "g" }

/** The wire: the single-record read answers from `reads` in order, the last
 * repeating; the tasks collection answers an empty page. */
function serve(reads: SubstrateRecord[]) {
  let served = 0
  return vi.fn(async (input: RequestInfo | URL) => {
    const url = String(input instanceof Request ? input.url : input)
    let body: unknown
    if (url.includes("/core/view/")) {
      body = reads[Math.min(served, reads.length - 1)]
      served++
    } else if (url.includes("/tasks/task")) {
      body = page
    }
    return new Response(
      JSON.stringify(body ?? { error: { code: "not_found", message: url } }),
      {
        status: body ? 200 : 404,
        headers: { "content-type": "application/json" },
      }
    )
  })
}

function fetchedUrls(): string[] {
  return (fetch as unknown as ReturnType<typeof vi.fn>).mock.calls.map((c) =>
    String(c[0])
  )
}

function mount(spec: ViewSpec, client = new QueryClient()) {
  client.setDefaultOptions({ queries: { retry: false } })
  const onOpenRecord = vi.fn()
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: () => (
      <CustomView
        spec={spec}
        kind={taskKind}
        kinds={kinds}
        ctx={{ inputs: {}, mode: "page" }}
        onOpenRecord={onOpenRecord}
      />
    ),
  })
  const router = createRouter({
    routeTree: rootRoute.addChildren([indexRoute]),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  })
  render(
    <QueryClientProvider client={client}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  )
}

/** The shell announcing itself from the frame's window. */
function ready(frame: HTMLIFrameElement) {
  act(() => {
    window.dispatchEvent(
      new MessageEvent("message", {
        data: { type: READY_TYPE },
        source: frame.contentWindow,
      })
    )
  })
}

type Posted = [{ type: string; html: string }, string, MessagePort[]]

beforeEach(() => {
  localStorage.setItem("substrate.token", "t")
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("the custom layout's gate", () => {
  it("mounts the document at owner provenance: the source and a port, posted once the shell is ready", async () => {
    vi.stubGlobal("fetch", serve([viewRecord()]))
    mount(viewSpec(viewRecord(), kinds))
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

    ready(frame)
    expect(posted).toHaveBeenCalledTimes(1)
    const [message, target, transfer] = posted.mock.calls[0] as Posted
    expect(message).toEqual({ type: MOUNT_TYPE, html: SOURCE })
    expect(target).toBe("*")
    expect(transfer[0]).toBeInstanceOf(MessagePort)

    // The post is armed once per navigation the host made: a second ready,
    // as from a document the guest navigated its frame to, gets nothing.
    ready(frame)
    expect(posted).toHaveBeenCalledTimes(1)
    expect(screen.queryByText(/needs the owner's review/)).toBeNull()
    expect(screen.queryByText(/No page is pushed/)).toBeNull()
  })

  it("withholds the mount below owner provenance and says the view needs review", async () => {
    const row = viewRecord({ permissionsTier: machine })
    vi.stubGlobal("fetch", serve([row]))
    mount(viewSpec(row, kinds))
    await screen.findByText(/needs the owner's review/)
    expect(document.querySelector("iframe")).toBeNull()
    expect(screen.getByText(/Done tasks per completion day/)).toBeTruthy()
    expect(
      screen.getByRole("link", { name: "Open source" }).getAttribute("href")
    ).toContain("/data/substrate.reamde.dev/core/view/tasks-heatmap")
    // No page was read for a document that will not run.
    expect(fetchedUrls().some((u) => u.includes("/tasks/task"))).toBe(false)
  })

  it("runs the single read's source under the single read's grant, whatever spec it was mounted with", async () => {
    // The row as the collection cached it: an agent's wider grant under its
    // own document. The single read is the owner's row.
    const widened = viewRecord({
      source: OTHER_SOURCE,
      reads: [TASK, PERSON],
    })
    vi.stubGlobal("fetch", serve([viewRecord()]))
    mount(viewSpec(widened, kinds))
    const frame = (await screen.findByTitle(
      "Done this quarter"
    )) as HTMLIFrameElement
    const posted = vi.fn()
    frame.contentWindow!.postMessage = posted
    ready(frame)
    const [message, , transfer] = posted.mock.calls[0] as Posted
    expect(message.html).toBe(SOURCE)

    // Over the very port that was handed to the shell: the grant the bridge
    // checks is the single read's, which does not read people.
    const guest = new Endpoint(transfer[0], {
      onRequest: () => ({}),
      onNotification() {},
    })
    await expect(
      guest.call(METHODS.toolsCall, {
        name: TOOLS.list,
        arguments: { kind: PERSON },
      })
    ).rejects.toMatchObject({
      code: ERROR.forbidden,
      message: `the view's grant does not read ${PERSON}`,
    })
    const listed = await guest.call<CallToolResult>(METHODS.toolsCall, {
      name: TOOLS.list,
      arguments: { kind: TASK },
    })
    expect(listed.structuredContent?.records).toEqual([])
    guest.close()
  })

  it("re-reads the row when a collection read holds a newer version, and mounts what the re-read says", async () => {
    const first = viewRecord()
    const third = viewRecord({
      version: 3,
      name: "Done this quarter, again",
      source: NEWER_SOURCE,
    })
    // The collection (an app screen's read) is already at version 3; the
    // single read answers version 1 first, then 3.
    const client = new QueryClient()
    client.setQueryData(viewsQueryOptions().queryKey, {
      records: [third],
      head: 3,
      generation: "g",
    } satisfies Page)
    vi.stubGlobal("fetch", serve([first, third]))
    mount(viewSpec(first, kinds), client)

    const frame = (await screen.findByTitle(
      "Done this quarter, again"
    )) as HTMLIFrameElement
    expect(fetchedUrls().filter((u) => u.includes("/core/view/"))).toHaveLength(
      2
    )
    const posted = vi.fn()
    frame.contentWindow!.postMessage = posted
    ready(frame)
    expect(posted).toHaveBeenCalledTimes(1)
    const [message] = posted.mock.calls[0] as Posted
    expect(message.html).toBe(NEWER_SOURCE)
  })

  it("mounts a row without a filter, reads no page for it, and says so", async () => {
    const unfiltered = viewRecord({ filter: false })
    vi.stubGlobal("fetch", serve([unfiltered]))
    mount(viewSpec(unfiltered, kinds))
    const frame = (await screen.findByTitle(
      "Done this quarter"
    )) as HTMLIFrameElement
    expect(frame.getAttribute("sandbox")).toBe("allow-scripts")
    expect(screen.getByText(/No page is pushed to this document/)).toBeTruthy()
    expect(screen.queryByText(/needs the owner's review/)).toBeNull()
    await act(() => new Promise((r) => setTimeout(r, 0)))
    expect(fetchedUrls().some((u) => u.includes("/tasks/task"))).toBe(false)
  })
})
