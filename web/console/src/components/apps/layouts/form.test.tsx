// @vitest-environment jsdom
/** The form layout against a mocked write: the prompted fields in the
 * action's order with the heading required, the seed (the filter's `eq`,
 * the `via` parent) shown read-only and never asked, the create posted with
 * the seed folded in and an idempotency key, the fields cleared after it
 * lands, a refusal before the heading is typed, and the fallback to every
 * owner-writable property when the view declares no create. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
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

const createRecord = vi.fn()
vi.mock("@/lib/api/records", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/records")>()),
  createRecord: (...args: unknown[]) => createRecord(...args),
}))

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { ViewContext } from "@/lib/apps/spec"
import { viewSpec } from "@/lib/apps/view-spec"
import FormLayout from "./form"

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
      status: {
        type: "state",
        states: ["active", "onhold"],
        initial: "active",
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
      name: { type: "string", description: "the task's heading, one line" },
      priority: {
        type: "enum",
        default: "none",
        values: ["none", "low", "medium", "high", "urgent"],
      },
      project: { type: "reference", kind: PROJECT, mustExist: true },
      completedAt: { type: "datetime" },
      status: {
        type: "state",
        states: ["proposed", "open", "done"],
        initial: "open",
      },
      syncedAt: { type: "datetime", writer: "connector" },
    },
  },
}

const kinds = [project, task]

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
  status: "active",
})

const formView = record("substrate.reamde.dev/core/view", "tasks-add", {
  name: "Add a task",
  layout: "form",
  kind: { ref: `substrate.reamde.dev/core/kind/${TASK}` },
  via: "project",
  filter: { properties: { status: { eq: "open" } } },
  actions: [
    {
      name: "add",
      verb: "create",
      label: "Add task",
      prompt: ["name", "dueAt"],
    },
  ],
})

const bareView = record("substrate.reamde.dev/core/view", "tasks-any", {
  name: "A task",
  layout: "form",
  kind: { ref: `substrate.reamde.dev/core/kind/${TASK}` },
})

function renderForm(view: SubstrateRecord, parent?: SubstrateRecord) {
  const spec = viewSpec(view, kinds)
  expect(spec.problems.filter((p) => p.severity === "error")).toEqual([])
  const ctx: ViewContext = {
    inputs: {},
    parent: parent ? { record: parent, kind: project } : undefined,
    mode: "page",
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Toaster>
        <FormLayout
          spec={spec}
          kind={task}
          kinds={kinds}
          ctx={ctx}
          onOpenRecord={vi.fn()}
        />
      </Toaster>
    </QueryClientProvider>
  )
}

beforeEach(() => {
  createRecord.mockReset()
  vi.stubGlobal("fetch", () =>
    Promise.resolve(
      new Response(JSON.stringify({ records: [], head: 1, generation: "g" }), {
        status: 200,
        headers: { "Content-Type": "application/json" },
      })
    )
  )
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("FormLayout", () => {
  it("asks for the prompted fields in order, the heading required, and shows the seed read-only", () => {
    renderForm(formView, website)
    const name = screen.getByLabelText(/^Name/) as HTMLInputElement
    const dueAt = screen.getByLabelText(/^Due at/) as HTMLInputElement
    expect(name.tagName).toBe("INPUT")
    expect(dueAt.tagName).toBe("INPUT")
    expect(name.compareDocumentPosition(dueAt)).toBe(
      Node.DOCUMENT_POSITION_FOLLOWING
    )
    // The heading is required because the displayTemplate reads it first.
    expect(screen.getByText("Name").parentElement?.textContent).toContain("*")
    // The seed: the filter's eq and the via parent, shown and never asked.
    expect(screen.getAllByText("from the view")).toHaveLength(2)
    expect(screen.getByText("open")).toBeTruthy()
    expect(screen.getByText("Website relaunch")).toBeTruthy()
    expect(screen.queryByLabelText(/^Project/)).toBeNull()
    expect(screen.queryByLabelText(/^Status/)).toBeNull()
  })

  it("posts the typed values with the seed and an idempotency key, then clears", async () => {
    createRecord.mockImplementation(
      (_a: string, _p: string, _n: string, input: { properties: object }) =>
        Promise.resolve(
          record(TASK, "t9", { ...input.properties, title: "Call the printer" })
        )
    )
    renderForm(formView, website)
    const name = screen.getByLabelText(/^Name/) as HTMLInputElement
    fireEvent.change(name, { target: { value: "Call the printer" } })
    fireEvent.click(screen.getByRole("button", { name: "Add task" }))
    await waitFor(() => expect(createRecord).toHaveBeenCalledTimes(1))
    const [authority, pkg, kindName, input, opts] = createRecord.mock.calls[0]
    expect([authority, pkg, kindName]).toEqual([
      "ada.example.com",
      "tasks",
      "task",
    ])
    expect(input).toEqual({
      properties: {
        status: "open",
        project: { ref: `${PROJECT}/website` },
        name: "Call the printer",
      },
    })
    expect(typeof opts.idempotencyKey).toBe("string")
    expect(opts.idempotencyKey.length).toBeGreaterThan(8)
    await screen.findByText("Call the printer created")
    await waitFor(() => expect(name.value).toBe(""))
  })

  it("refuses to submit before the heading is typed", async () => {
    renderForm(formView, website)
    fireEvent.click(screen.getByRole("button", { name: "Add task" }))
    await screen.findByText("name is required.")
    expect(createRecord).not.toHaveBeenCalled()
  })

  it("without a parent seeds only the filter and leaves via alone", () => {
    renderForm(formView)
    expect(screen.getAllByText("from the view")).toHaveLength(1)
    expect(screen.getByText("open")).toBeTruthy()
  })

  it("asks for every owner-writable property when the view declares no create", () => {
    renderForm(bareView)
    expect(screen.getByLabelText(/^Name/)).toBeTruthy()
    expect(screen.getByLabelText(/^Priority/)).toBeTruthy()
    expect(screen.getByLabelText(/^Status/)).toBeTruthy()
    // A connector-written property is not the owner's to type.
    expect(screen.queryByLabelText(/^Synced at/)).toBeNull()
    expect(screen.getByRole("button", { name: "Add task" })).toBeTruthy()
  })
})
