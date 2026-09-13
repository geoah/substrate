// @vitest-environment jsdom
/** The app screen under a record segment, under a real router: a `via` view
 * holds its read and its actions until the parent the segment names is
 * read, so no unscoped collection request leaves and no create is offered
 * without its seed; once the parent lands the read is scoped to it and the
 * primary create is there. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
  useParams,
} from "@tanstack/react-router"
import { act, cleanup, render, screen, waitFor } from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

/** The screen joins the shared change-feed tail; a test has no stream. */
vi.mock("@/hooks/use-live-records", () => ({ useLiveRecords: () => {} }))

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { AppScreen } from "./app-screen"

const PROJECT = "ada.example.com/tasks/project"
const TASK = "ada.example.com/tasks/task"
const VIEW = "substrate.reamde.dev/core/view"

const project: KindInfo = {
  identity: PROJECT,
  name: "project",
  authority: "ada.example.com",
  package: "tasks",
  version: 8,
  source: "installed",
  description: "",
  definition: {
    names: { singular: "project" },
    displayTemplate: "{name}",
    properties: { name: { type: "string" } },
  },
}

const task: KindInfo = {
  identity: TASK,
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 8,
  source: "installed",
  description: "",
  definition: {
    names: { singular: "task" },
    displayTemplate: "{name|title}",
    properties: {
      name: { type: "string" },
      project: { type: "reference", kind: "project", mustExist: true },
      status: {
        type: "state",
        states: ["proposed", "open", "done"],
        initial: "open",
      },
    },
  },
}

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  }
}

const website = record(PROJECT, "website", {
  title: "Website relaunch",
  name: "Website relaunch",
})

const tasks = [
  record(TASK, "t1", {
    title: "Pick a launch date",
    name: "Pick a launch date",
    status: "open",
    project: { ref: `${PROJECT}/website` },
  }),
]

const byProject = record(VIEW, "tasks-by-project", {
  name: "Open tasks",
  layout: "list",
  kind: { ref: `substrate.reamde.dev/core/kind/${TASK}` },
  via: "project",
  filter: { properties: { status: { in: ["open", "proposed"] } } },
  actions: [{ name: "add", verb: "create", prompt: ["name"] }],
})

const app = record("substrate.reamde.dev/core/app", "tasks", {
  name: "Tasks",
  screens: [{ name: "open", view: { ref: `${VIEW}/tasks-by-project` } }],
})

interface Call {
  method: string
  url: string
}
const calls: Call[] = []

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function page(records: unknown[]) {
  return json({ records, head: 1, generation: "g" })
}

/** The stub holds the parent's read until the test lets it go. */
function stubFetch() {
  let release: () => void = () => {}
  const gate = new Promise<void>((resolve) => {
    release = resolve
  })
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? "GET"
    calls.push({ method, url })
    const path = url.split("?")[0]
    if (path.endsWith("/core/kind"))
      return Promise.resolve(page([project, task]))
    if (path.endsWith("/core/app/tasks")) return Promise.resolve(json(app))
    if (path.endsWith("/core/view")) return Promise.resolve(page([byProject]))
    if (path.endsWith("/tasks/project/website")) {
      return gate.then(() => json(website))
    }
    if (path.endsWith("/tasks/task")) return Promise.resolve(page(tasks))
    return Promise.resolve(json({ error: `unexpected ${method} ${url}` }, 404))
  })
  return { release }
}

/** jsdom has no matchMedia; the chrome asks it once per mount. */
function stubViewport(width: number) {
  Object.defineProperty(window, "innerWidth", {
    value: width,
    configurable: true,
    writable: true,
  })
  vi.stubGlobal("matchMedia", (query: string) => ({
    matches: width < 768,
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    addListener() {},
    removeListener() {},
    dispatchEvent: () => false,
  }))
}

function Page() {
  const { id = "", screen: name, record } = useParams({ strict: false })
  return <AppScreen id={id} screen={name} segment={record} />
}

function mount(path: string) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const routes = [
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/apps",
      component: () => <p>launcher</p>,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/apps/$id",
      component: Page,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/apps/$id/$screen",
      component: Page,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/apps/$id/$screen/$record",
      component: Page,
    }),
  ]
  const router = createRouter({
    routeTree: rootRoute.addChildren(routes),
    history: createMemoryHistory({ initialEntries: [path] }),
  })
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <NuqsTestingAdapter searchParams="" hasMemory>
      <QueryClientProvider client={client}>
        <Toaster>
          <RouterProvider router={router} />
        </Toaster>
      </QueryClientProvider>
    </NuqsTestingAdapter>
  )
}

const taskReads = () => calls.filter((c) => c.url.includes("/tasks/task"))

beforeEach(() => {
  calls.length = 0
  stubViewport(390)
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("a via view under a parent segment", () => {
  it("reads nothing and offers no create until the parent is read, then reads scoped", async () => {
    const { release } = stubFetch()
    mount("/apps/tasks/open/website")
    // The chrome is up and the parent's read is out ...
    await screen.findByRole("heading", { level: 1, name: "Tasks" })
    await waitFor(() => {
      expect(calls.some((c) => c.url.includes("/tasks/project/website"))).toBe(
        true
      )
    })
    // ... and while it is, no collection read has left and no Add is
    // offered: a read now would be the whole collection, a create now would
    // seed no project.
    await act(() => new Promise((resolve) => setTimeout(resolve, 30)))
    expect(taskReads()).toHaveLength(0)
    expect(screen.queryByRole("button", { name: "Add" })).toBeNull()

    await act(async () => release())
    await screen.findByText("Pick a launch date")
    expect(taskReads().length).toBeGreaterThan(0)
    for (const read of taskReads()) {
      const filter = JSON.parse(
        new URL(read.url, "http://console").searchParams.get("filter") ?? "{}"
      )
      expect(filter.properties.project).toEqual({ eq: `${PROJECT}/website` })
    }
    expect(screen.getByRole("button", { name: "Add" })).toBeTruthy()
    expect(screen.getByText("Website relaunch")).toBeTruthy()
  })
})
