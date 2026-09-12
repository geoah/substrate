// @vitest-environment jsdom
/** The detail layout against a stubbed substrate: the subject from
 * `ctx.parent`, its title and state badge, the `show` prose kept as prose,
 * only the transitions the machine admits from the current state (Pause and
 * Done on an active project; Resume and Done on one on hold), a transition
 * landing as a CAS patch and the buttons following the new state, and each
 * `related` view mounted scoped to this record with its create as the
 * section's trailing button. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: React.ReactNode
  }) => <a {...rest}>{children}</a>,
  useNavigate: () => vi.fn(),
}))

/** The renderer joins the shared change-feed tail; a test has no stream. */
vi.mock("@/hooks/use-live-records", () => ({ useLiveRecords: () => {} }))

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { ViewContext } from "@/lib/apps/spec"
import { viewSpec } from "@/lib/apps/view-spec"
import DetailLayout from "./detail"

const PROJECT = "ada.example.com/tasks/project"
const TASK = "ada.example.com/tasks/task"

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
        states: ["active", "onhold", "done", "abandoned"],
        initial: "active",
        transitions: [
          { from: "active", to: "onhold" },
          { from: "onhold", to: "active" },
          { from: "active", to: "done" },
          { from: "active", to: "abandoned" },
          { from: "onhold", to: "done" },
          { from: "onhold", to: "abandoned" },
          { from: "done", to: "active" },
          { from: "abandoned", to: "active" },
        ],
      },
    },
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
    traits: ["temporal(point: dueAt)"],
    properties: {
      name: { type: "string" },
      priority: {
        type: "enum",
        default: "none",
        values: ["none", "low", "medium", "high", "urgent"],
      },
      project: { type: "reference", kind: "project", mustExist: true },
      status: {
        type: "state",
        states: ["proposed", "open", "done", "abandoned"],
        initial: "open",
        transitions: [
          { from: "proposed", to: "open" },
          { from: "open", to: "done" },
          { from: "done", to: "open" },
        ],
      },
    },
  },
}

const kinds = [project, task]

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
  summary:
    "Move the marketing site to the new design system.\n\nBefore the October launch.",
})
const taxes = record(PROJECT, "taxes", {
  title: "Taxes 2026",
  name: "Taxes 2026",
  status: "onhold",
  summary: "Receipts and the filing.",
})

const tasks = [
  record(TASK, "t1", {
    title: "Pick a launch date",
    name: "Pick a launch date",
    status: "open",
    project: { ref: `${PROJECT}/website` },
    dueAt: "2026-09-10T09:00:00Z",
  }),
  record(TASK, "t2", {
    title: "Draft the copy",
    name: "Draft the copy",
    status: "proposed",
    priority: "high",
    project: { ref: `${PROJECT}/website` },
  }),
]

const detailView = record("substrate.reamde.dev/core/view", "tasks-project", {
  name: "Project",
  layout: "detail",
  kind: { ref: `substrate.reamde.dev/core/kind/${PROJECT}` },
  show: ["summary"],
  related: [
    { view: { ref: "substrate.reamde.dev/core/view/tasks-by-project" } },
  ],
  actions: [
    { name: "pause", verb: "transition", to: "onhold" },
    { name: "resume", verb: "transition", to: "active" },
    { name: "done", verb: "transition", to: "done" },
  ],
})

const relatedView = record(
  "substrate.reamde.dev/core/view",
  "tasks-by-project",
  {
    name: "Open tasks",
    layout: "list",
    kind: { ref: `substrate.reamde.dev/core/kind/${TASK}` },
    via: "project",
    filter: { properties: { status: { in: ["open", "proposed"] } } },
    show: ["dueAt", "priority"],
    actions: [
      { name: "done", verb: "transition", to: "done" },
      { name: "add", verb: "create", prompt: ["name", "dueAt"] },
    ],
  }
)

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

function page(records: SubstrateRecord[]) {
  return json({ records, head: 1, generation: "g" })
}

function stubFetch(subject: SubstrateRecord) {
  let current = subject
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url, body })
    const path = url.split("?")[0]
    if (method === "PATCH" && path.endsWith(`/tasks/project/${current.id}`)) {
      current = {
        ...current,
        properties: { ...current.properties, ...body.properties },
        version: current.version + 1,
      }
      return Promise.resolve(json(current))
    }
    if (path.endsWith(`/tasks/project/${current.id}`)) {
      return Promise.resolve(json(current))
    }
    if (path.endsWith("/core/view/tasks-by-project")) {
      return Promise.resolve(json(relatedView))
    }
    if (path.endsWith("/tasks/task")) return Promise.resolve(page(tasks))
    if (path.endsWith("/tasks/project")) {
      return Promise.resolve(page([website, taxes]))
    }
    return Promise.resolve(json({ error: `unexpected ${method} ${url}` }, 500))
  })
}

/** jsdom has no matchMedia; `useIsMobile` asks it once per mount. */
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

function renderDetail(subject?: SubstrateRecord) {
  const spec = viewSpec(detailView, kinds)
  expect(spec.problems.filter((p) => p.severity === "error")).toEqual([])
  const ctx: ViewContext = {
    inputs: {},
    parent: subject ? { record: subject, kind: project } : undefined,
    mode: "page",
  }
  const onOpenRecord = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const utils = render(
    <QueryClientProvider client={client}>
      <Toaster>
        <DetailLayout
          spec={spec}
          kind={project}
          kinds={kinds}
          ctx={ctx}
          onOpenRecord={onOpenRecord}
        />
      </Toaster>
    </QueryClientProvider>
  )
  return { ...utils, onOpenRecord }
}

const buttonNames = () =>
  screen
    .queryAllByRole("button")
    .map((b) => b.textContent?.trim())
    .filter(Boolean)

beforeEach(() => {
  calls.length = 0
  stubViewport(390)
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("DetailLayout", () => {
  it("shows the title, the state badge and the summary as prose", async () => {
    stubFetch(website)
    renderDetail(website)
    expect(
      screen.getByRole("heading", { level: 2, name: "Website relaunch" })
    ).toBeTruthy()
    expect(screen.getByText("active")).toBeTruthy()
    const paragraphs = screen
      .getAllByRole("paragraph")
      .map((p) => p.textContent)
    expect(paragraphs).toContain(
      "Move the marketing site to the new design system."
    )
    expect(paragraphs).toContain("Before the October launch.")
    await screen.findByText("Open tasks")
  })

  it("offers Pause and Done on an active project, never Resume", async () => {
    stubFetch(website)
    renderDetail(website)
    await screen.findByRole("button", { name: "Pause" })
    expect(screen.getByRole("button", { name: "Done" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Resume" })).toBeNull()
  })

  it("offers Resume and Done on a project on hold, never Pause", async () => {
    stubFetch(taxes)
    renderDetail(taxes)
    await screen.findByRole("button", { name: "Resume" })
    expect(screen.getByRole("button", { name: "Done" })).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull()
  })

  it("runs a transition as a CAS patch and follows the record into its new state", async () => {
    stubFetch(website)
    renderDetail(website)
    fireEvent.click(await screen.findByRole("button", { name: "Pause" }))
    await waitFor(() => {
      expect(calls.some((c) => c.method === "PATCH")).toBe(true)
    })
    const patch = calls.find((c) => c.method === "PATCH")!
    expect(patch.url).toContain(`/ada.example.com/tasks/project/website`)
    expect(patch.body).toEqual({
      properties: { status: "onhold" },
      ifVersion: 1,
    })
    await screen.findByRole("button", { name: "Resume" })
    expect(screen.queryByRole("button", { name: "Pause" })).toBeNull()
    expect(screen.getByText("onhold")).toBeTruthy()
  })

  it("mounts each related view scoped to this record, its create trailing the heading", async () => {
    stubFetch(website)
    renderDetail(website)
    const section = await screen.findByRole("region", { name: "Open tasks" })
    expect(within(section).getByRole("button", { name: /Add/ })).toBeTruthy()
    await within(section).findByText("Pick a launch date")
    expect(within(section).getByText("Draft the copy")).toBeTruthy()
    const read = calls.find(
      (c) => c.method === "GET" && c.url.includes("/tasks/task?")
    )!
    const filter = JSON.parse(
      new URL(read.url, "http://console").searchParams.get("filter") ?? "{}"
    )
    expect(filter.properties.project).toEqual({ eq: `${PROJECT}/website` })
    expect(filter.properties.status).toEqual({ in: ["open", "proposed"] })
    // The row action is the list's, hidden where the machine has no arm:
    // an open task offers Done, a proposed one does not.
    expect(
      within(section).getAllByRole("button", { name: "Done" })
    ).toHaveLength(1)
  })

  it("says so when there is no record", () => {
    stubFetch(website)
    renderDetail(undefined)
    expect(screen.getByText("No record")).toBeTruthy()
    expect(buttonNames()).toEqual([])
  })
})
