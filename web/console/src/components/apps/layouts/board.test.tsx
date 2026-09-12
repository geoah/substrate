// @vitest-environment jsdom
/** The board layout against a stubbed substrate: one column per state the
 * filter admits (else every declared one, in declaration order) with its
 * count, cards titled by the record, a drag admitted only along an arm the
 * machine declares and landing as a CAS transition, the segmented control
 * over one list under 768 px, a tap reported to the screen, the empty text
 * and the cut-page note. */

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

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { viewSpec } from "@/lib/apps/view-spec"
import BoardLayout from "./board"

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

/** A machine with a dead end: nothing leaves `done`. */
const STEP = "ada.example.com/flow/step"
const step: KindInfo = {
  identity: STEP,
  name: "step",
  authority: "ada.example.com",
  package: "flow",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    names: { singular: "step" },
    displayTemplate: "{name}",
    properties: {
      name: { type: "string" },
      phase: {
        type: "state",
        states: ["todo", "doing", "done"],
        initial: "todo",
        transitions: [
          { from: "todo", to: "doing" },
          { from: "doing", to: "done" },
        ],
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
    version: 3,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  }
}

const projects = [
  record(PROJECT, "website", {
    title: "Website relaunch",
    name: "Website relaunch",
    status: "active",
  }),
  record(PROJECT, "homelab", {
    title: "Home lab",
    name: "Home lab",
    status: "active",
  }),
  record(PROJECT, "taxes", {
    title: "Taxes 2026",
    name: "Taxes 2026",
    status: "onhold",
  }),
]

const steps = [
  record(STEP, "plan", { title: "Plan", name: "Plan", phase: "todo" }),
  record(STEP, "ship", { title: "Ship", name: "Ship", phase: "done" }),
]

function view(
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return record("substrate.reamde.dev/core/view", id, properties)
}

const boardView = view("tasks-projects", {
  name: "Projects",
  layout: "board",
  kind: { ref: `substrate.reamde.dev/core/kind/${PROJECT}` },
  groupBy: "status",
  filter: { properties: { status: { in: ["active", "onhold"] } } },
  opens: { ref: "substrate.reamde.dev/core/view/tasks-project" },
  actions: [{ name: "add", verb: "create", prompt: ["name"] }],
})

const stepView = view("steps", {
  name: "Steps",
  layout: "board",
  kind: { ref: `substrate.reamde.dev/core/kind/${STEP}` },
  groupBy: "phase",
  empty: "Nothing in this phase",
})

interface Call {
  method: string
  url: string
  body?: unknown
}
const calls: Call[] = []
let cursor: string | undefined

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function stubFetch() {
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({ method, url, body })
    const rows = url.includes("/tasks/project") ? projects : steps
    if (method === "GET") {
      return Promise.resolve(
        json({ records: rows, cursor, head: 1, generation: "g" })
      )
    }
    if (method === "PATCH") {
      const id = url.split("?")[0].split("/").pop() ?? ""
      const row = rows.find((r) => r.id === id)!
      return Promise.resolve(
        json({
          ...row,
          properties: { ...row.properties, ...body.properties },
          version: row.version + 1,
        })
      )
    }
    return Promise.resolve(json({ error: "unexpected" }, 500))
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

function renderBoard(
  record: SubstrateRecord,
  kinds: KindInfo[],
  mode: "page" | "card" = "page"
) {
  const spec = viewSpec(record, kinds)
  expect(spec.problems.filter((p) => p.severity === "error")).toEqual([])
  const kind = kinds.find((k) => k.identity === spec.kind)
  const onOpenRecord = vi.fn()
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const utils = render(
    <QueryClientProvider client={client}>
      <Toaster>
        <BoardLayout
          spec={spec}
          kind={kind}
          kinds={kinds}
          ctx={{ inputs: {}, mode }}
          onOpenRecord={onOpenRecord}
        />
      </Toaster>
    </QueryClientProvider>
  )
  return { ...utils, onOpenRecord, spec }
}

const dataTransfer = () => ({
  setData: vi.fn(),
  getData: vi.fn(),
  effectAllowed: "",
  dropEffect: "",
})

beforeEach(() => {
  calls.length = 0
  cursor = undefined
  stubViewport(1024)
  stubFetch()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("BoardLayout", () => {
  it("draws one column per state the filter admits, with its count and its cards", async () => {
    renderBoard(boardView, [project])
    await screen.findByText("Website relaunch")
    const active = screen.getByRole("region", { name: "active" })
    const onhold = screen.getByRole("region", { name: "onhold" })
    expect(within(active).getAllByRole("button")).toHaveLength(2)
    expect(within(active).getByText("2")).toBeTruthy()
    expect(within(onhold).getByText("Taxes 2026")).toBeTruthy()
    expect(within(onhold).getByText("1")).toBeTruthy()
    // done and abandoned are declared but not admitted: no column.
    expect(screen.queryByRole("region", { name: "done" })).toBeNull()
  })

  it("draws every declared state in declaration order when the filter admits all", async () => {
    renderBoard(stepView, [step])
    await screen.findByText("Plan")
    // The toaster is a landmark too; the columns are the sections.
    const names = [...document.querySelectorAll("section[aria-label]")].map(
      (el) => el.getAttribute("aria-label")
    )
    expect(names).toEqual(["todo", "doing", "done"])
    expect(
      within(screen.getByRole("region", { name: "doing" })).getByText(
        "Nothing in this phase"
      )
    ).toBeTruthy()
  })

  it("does not repeat the heading as a cell", async () => {
    renderBoard(boardView, [project])
    const card = (await screen.findByText("Home lab")).closest("button")!
    expect(card.textContent).toBe("Home lab")
  })

  it("lets a card drag only along a declared arm", async () => {
    renderBoard(stepView, [step])
    const plan = (await screen.findByText("Plan")).closest("button")!
    const ship = screen.getByText("Ship").closest("button")!
    expect(plan.getAttribute("draggable")).toBe("true")
    // Nothing leaves `done`, so the card is not draggable at all.
    expect(ship.getAttribute("draggable")).toBe("false")
  })

  it("lands a drop as the transition, a CAS patch of the state", async () => {
    renderBoard(stepView, [step])
    const plan = (await screen.findByText("Plan")).closest("button")!
    fireEvent.dragStart(plan, { dataTransfer: dataTransfer() })
    fireEvent.drop(screen.getByRole("region", { name: "doing" }), {
      dataTransfer: dataTransfer(),
    })
    await waitFor(() => {
      expect(calls.some((c) => c.method === "PATCH")).toBe(true)
    })
    const patch = calls.find((c) => c.method === "PATCH")!
    expect(patch.url).toContain("/ada.example.com/flow/step/plan")
    expect(patch.body).toEqual({
      properties: { phase: "doing" },
      ifVersion: 3,
    })
    await screen.findByText("Plan: doing")
  })

  it("writes nothing when the drop is on a column the machine refuses", async () => {
    renderBoard(stepView, [step])
    const plan = (await screen.findByText("Plan")).closest("button")!
    fireEvent.dragStart(plan, { dataTransfer: dataTransfer() })
    // todo → done is not an arm.
    fireEvent.drop(screen.getByRole("region", { name: "done" }), {
      dataTransfer: dataTransfer(),
    })
    await new Promise((r) => setTimeout(r, 20))
    expect(calls.filter((c) => c.method === "PATCH")).toEqual([])
  })

  it("reports a tap to the screen, which owns opens", async () => {
    const { onOpenRecord } = renderBoard(boardView, [project])
    fireEvent.click(await screen.findByText("Home lab"))
    expect(onOpenRecord).toHaveBeenCalledWith(
      expect.objectContaining({ id: "homelab" })
    )
  })

  it("becomes a segmented control over one list under 768 px", async () => {
    stubViewport(390)
    renderBoard(boardView, [project])
    await screen.findByText("Website relaunch")
    const tabs = screen.getByRole("tablist", { name: "Columns" })
    const names = within(tabs)
      .getAllByRole("tab")
      .map((t) => t.textContent)
    expect(names).toEqual(["active2", "onhold1"])
    expect(document.querySelectorAll("section[aria-label]")).toHaveLength(0)
    expect(screen.queryByText("Taxes 2026")).toBeNull()
    fireEvent.click(within(tabs).getByRole("tab", { name: /onhold/ }))
    await screen.findByText("Taxes 2026")
    expect(screen.queryByText("Home lab")).toBeNull()
    // No card is draggable under a finger.
    expect(
      screen
        .getByText("Taxes 2026")
        .closest("button")!
        .getAttribute("draggable")
    ).toBe("false")
  })

  it("leaves the create to the screen on a page and draws it in a card", async () => {
    const page = renderBoard(boardView, [project], "page")
    await screen.findByText("Website relaunch")
    expect(page.queryByRole("button", { name: /Add/ })).toBeNull()
    cleanup()
    renderBoard(boardView, [project], "card")
    await screen.findByText("Website relaunch")
    expect(screen.getByRole("button", { name: /Add/ })).toBeTruthy()
  })

  it("says when the page was cut", async () => {
    cursor = "more"
    renderBoard(view("two", { ...boardView.properties, first: 3 }), [project])
    await screen.findByText("Showing the first 3; more were not loaded.")
  })
})
