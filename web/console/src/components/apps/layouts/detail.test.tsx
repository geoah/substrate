// @vitest-environment jsdom
/** The detail layout against a stubbed substrate: the subject from
 * `ctx.parent`, its title and state badge, the `show` prose kept as prose,
 * only the transitions the machine admits from the current state (Pause and
 * Done on an active project; Resume and Done on one on hold), a transition
 * landing as a CAS patch and the buttons following the new state, and each
 * `related` view mounted scoped to this record with its create as the
 * section's trailing button. Then the line of views (`ctx.ancestors`), run
 * through the renderer: a detail that relates itself and two that relate
 * each other stop at one line instead of mounting without end, while the
 * same view under two sibling sections mounts twice. */

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

import { ViewRenderer } from "@/components/apps/view-renderer"
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

function stubFetch(
  subject: SubstrateRecord,
  views = [relatedView],
  packages: SubstrateRecord[] = []
) {
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
    const view = views.find((v) => path.endsWith(`/core/view/${v.id}`))
    if (view) return Promise.resolve(json(view))
    if (path.endsWith("/core/kind")) return Promise.resolve(page([]))
    if (path.endsWith("/core/package")) return Promise.resolve(page(packages))
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

/** A detail on `project` whose sections are the given views. */
function detailOf(id: string, name: string, related: string[]) {
  return record("substrate.reamde.dev/core/view", id, {
    name,
    layout: "detail",
    kind: { ref: `substrate.reamde.dev/core/kind/${PROJECT}` },
    show: ["summary"],
    related: related.map((view) => ({
      view: { ref: `substrate.reamde.dev/core/view/${view}` },
    })),
  })
}

/** The page mount as a screen makes it: through the renderer, which is what
 * puts the view on the line its sections inherit. */
function renderMounted(view: SubstrateRecord, subject: SubstrateRecord) {
  const spec = viewSpec(view, kinds)
  const ctx: ViewContext = {
    inputs: {},
    parent: { record: subject, kind: project },
    mode: "page",
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Toaster>
        <ViewRenderer
          spec={spec}
          kinds={kinds}
          ctx={ctx}
          onOpenRecord={vi.fn()}
        />
      </Toaster>
    </QueryClientProvider>
  )
}

describe("the line of views a section inherits", () => {
  it("refuses a detail that relates itself, once", async () => {
    const self = detailOf("tasks-project-self", "Project", [
      "tasks-project-self",
    ])
    stubFetch(website, [self])
    renderMounted(self, website)
    await screen.findByRole("heading", { level: 2, name: "Website relaunch" })
    const section = await screen.findByRole("region", { name: "Project" })
    await within(section).findByText(
      "tasks-project-self is already open above this"
    )
    // One detail, not a second one under it.
    expect(screen.getAllByRole("heading", { level: 2 })).toHaveLength(1)
    expect(within(section).queryByRole("heading", { level: 2 })).toBeNull()
  })

  it("stops a two-view cycle at the view already open above", async () => {
    const a = detailOf("tasks-project-a", "Project A", ["tasks-project-b"])
    const b = detailOf("tasks-project-b", "Project B", ["tasks-project-a"])
    stubFetch(website, [a, b])
    renderMounted(a, website)
    // B mounts under A as a detail of the same subject ...
    const sectionB = await screen.findByRole("region", { name: "Project B" })
    await within(sectionB).findByRole("heading", {
      level: 2,
      name: "Website relaunch",
    })
    // ... and A under B is the line, not a third detail.
    const sectionA = await within(sectionB).findByRole("region", {
      name: "Project A",
    })
    await within(sectionA).findByText(
      "tasks-project-a is already open above this"
    )
    expect(screen.getAllByRole("heading", { level: 2 })).toHaveLength(2)
  })

  it("lets the same view mount under two sibling sections", async () => {
    const a = detailOf("tasks-project-a", "Project A", [
      "tasks-project-b",
      "tasks-project-c",
    ])
    const b = detailOf("tasks-project-b", "Project B", ["tasks-by-project"])
    const c = detailOf("tasks-project-c", "Project C", ["tasks-by-project"])
    stubFetch(website, [a, b, c, relatedView])
    renderMounted(a, website)
    await waitFor(() => {
      expect(
        screen.getAllByRole("region", { name: "Open tasks" })
      ).toHaveLength(2)
    })
    for (const section of screen.getAllByRole("region", {
      name: "Open tasks",
    })) {
      await within(section).findByText("Pick a launch date")
    }
    expect(screen.queryByText(/is already open above this/)).toBeNull()
  })
})

describe("a floor the view declares", () => {
  it("says the shortfall in place of the rows while the package is below it", async () => {
    const floored = record("substrate.reamde.dev/core/view", "tasks-floor", {
      name: "Project",
      layout: "detail",
      kind: { ref: `substrate.reamde.dev/core/kind/${PROJECT}` },
      show: ["summary"],
      requiresAtLeast: { "ada.example.com/tasks": 9 },
    })
    const installed = record(
      "substrate.reamde.dev/core/package",
      "ada.example.com/tasks",
      { version: 3 }
    )
    stubFetch(website, [floored], [installed])
    renderMounted(floored, website)
    await screen.findByText(
      "needs ada.example.com/tasks at version 9 (installed 3)"
    )
    expect(screen.queryByRole("heading", { level: 2 })).toBeNull()

    cleanup()
    installed.properties.version = 9
    renderMounted(floored, website)
    await screen.findByRole("heading", { level: 2, name: "Website relaunch" })
  })
})
