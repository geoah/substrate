// @vitest-environment jsdom
/** The view screen over a detail, under a real router: the header's actions
 * act on the subject the segment names. A header transition is offered only
 * along an arm the machine admits from the subject's state, runs as a CAS
 * patch on that record, and the header follows it into the new state. */

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
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

/** The screen joins the shared change-feed tail; a test has no stream. */
vi.mock("@/hooks/use-live-records", () => ({ useLiveRecords: () => {} }))

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { ViewScreen } from "./view-screen"

const PROJECT = "ada.example.com/tasks/project"

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
    properties: {
      name: { type: "string" },
      summary: { type: "markdown" },
      status: {
        type: "state",
        states: ["active", "onhold", "done"],
        initial: "active",
        transitions: [
          { from: "active", to: "onhold" },
          { from: "onhold", to: "active" },
          { from: "active", to: "done" },
        ],
      },
    },
  },
}

function record(
  kind: string,
  id: string,
  properties: Record<string, unknown>,
  version = 1
): SubstrateRecord {
  return {
    id,
    kind,
    properties,
    labels: {},
    version,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  }
}

const website = record(PROJECT, "website", {
  title: "Website relaunch",
  name: "Website relaunch",
  status: "active",
  summary: "Move the marketing site to the new design system.",
})

/** A detail whose transitions sit in the header, where the screen draws
 * them. */
const detailView = record("substrate.reamde.dev/core/view", "tasks-project", {
  name: "Project",
  layout: "detail",
  kind: { ref: `substrate.reamde.dev/core/kind/${PROJECT}` },
  show: ["summary"],
  actions: [
    { name: "pause", verb: "transition", to: "onhold", placement: "header" },
    { name: "resume", verb: "transition", to: "active", placement: "header" },
  ],
})

interface Call {
  method: string
  url: string
  body?: unknown
}
const calls: Call[] = []

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function stubFetch() {
  let current = website
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url, body })
    const path = url.split("?")[0]
    if (path.endsWith("/core/kind")) {
      return Promise.resolve(
        json({ records: [project], head: 1, generation: "g" })
      )
    }
    if (path.endsWith("/core/view/tasks-project")) {
      return Promise.resolve(json(detailView))
    }
    if (method === "PATCH" && path.endsWith("/tasks/project/website")) {
      current = {
        ...current,
        properties: { ...current.properties, ...body.properties },
        version: current.version + 1,
      }
      return Promise.resolve(json(current))
    }
    if (path.endsWith("/tasks/project/website")) {
      return Promise.resolve(json(current))
    }
    return Promise.resolve(json({ error: `unexpected ${method} ${url}` }, 404))
  })
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
  const { id = "", record } = useParams({ strict: false })
  return <ViewScreen id={id} segment={record} />
}

function mount(path: string) {
  const rootRoute = createRootRoute({ component: () => <Outlet /> })
  const routes = [
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/views/$id",
      component: Page,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/views/$id/$record",
      component: Page,
    }),
    createRoute({
      getParentRoute: () => rootRoute,
      path: "/apps",
      component: () => <p>launcher</p>,
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

beforeEach(() => {
  calls.length = 0
  stubViewport(390)
  stubFetch()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("a detail's header actions", () => {
  it("act on the subject: offered along its arms, patched with its version, following it", async () => {
    mount("/views/tasks-project/website")
    // The subject is on the page, and the header offers Pause and not
    // Resume: the subject is active.
    await screen.findByRole("heading", { level: 2, name: "Website relaunch" })
    const pause = await screen.findByRole("button", { name: "Pause" })
    expect(pause.closest("header")).not.toBeNull()
    expect(screen.queryByRole("button", { name: "Resume" })).toBeNull()

    fireEvent.click(pause)
    await waitFor(() => {
      expect(calls.some((c) => c.method === "PATCH")).toBe(true)
    })
    const patch = calls.find((c) => c.method === "PATCH")!
    expect(patch.url).toContain(`/${PROJECT}/website`)
    expect(patch.body).toEqual({
      properties: { status: "onhold" },
      ifVersion: 1,
    })

    // The header followed the record: Resume now, Pause gone, badge moved.
    const resume = await screen.findByRole("button", { name: "Resume" })
    expect(resume.closest("header")).not.toBeNull()
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull()
    await screen.findByText("onhold")
  })
})
