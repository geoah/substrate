// @vitest-environment jsdom
/** The action button's order for an action that both prompts and confirms:
 * the form collects the values, the confirm asks about them (the action's
 * description, the record it acts on), and the write runs only after the
 * confirm, under the one idempotency key the form was opened with. A
 * cancelled confirm returns to the filled form rather than dropping it. */

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

/** The keys minted, numbered, so a test can tell one opened form's key from
 * a second minting. */
const keys = vi.hoisted(() => ({ minted: 0 }))
vi.mock("@/lib/apps/form", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/apps/form")>()),
  newIdempotencyKey: () => `key-${++keys.minted}`,
}))

import { Toaster } from "@/components/ui/toast"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { ActionHost } from "@/lib/apps/spec"
import { viewSpec } from "@/lib/apps/view-spec"
import { ActionButton } from "./action-button"

const TASK = "ada.example.com/tasks/task"

const task: KindInfo = {
  identity: TASK,
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    names: { singular: "task" },
    displayTemplate: "{name}",
    properties: {
      name: { type: "string" },
      status: { type: "state", states: ["open", "done"], initial: "open" },
    },
  },
}

const kinds = [task]

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

const t1 = record(TASK, "t1", {
  title: "Pick a launch date",
  name: "Pick a launch date",
  status: "open",
})

const view = record("substrate.reamde.dev/core/view", "tasks-open", {
  name: "Open tasks",
  layout: "list",
  kind: { ref: `substrate.reamde.dev/core/kind/${TASK}` },
  actions: [
    {
      name: "rename",
      verb: "patch",
      prompt: ["name"],
      confirm: true,
      description: "Renames the task.",
    },
    {
      name: "add",
      verb: "create",
      prompt: ["name"],
      confirm: true,
      description: "Adds a task.",
    },
  ],
})

interface Call {
  method: string
  url: string
  headers: Record<string, string>
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
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    const method = init?.method ?? "GET"
    const body = init?.body ? JSON.parse(String(init.body)) : undefined
    calls.push({
      method,
      url,
      headers: (init?.headers ?? {}) as Record<string, string>,
      body,
    })
    const path = url.split("?")[0]
    if (method === "PATCH" && path.endsWith("/tasks/task/t1")) {
      return Promise.resolve(
        json({
          ...t1,
          properties: { ...t1.properties, ...body.properties },
          version: 2,
        })
      )
    }
    if (method === "POST" && path.endsWith("/tasks/task")) {
      return Promise.resolve(
        json(
          record(TASK, "t9", {
            ...body.properties,
            title: body.properties.name,
            status: "open",
          })
        )
      )
    }
    return Promise.resolve(json({ error: `unexpected ${method} ${url}` }, 500))
  })
}

/** jsdom has no matchMedia; the sheets ask it once per mount. */
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

function renderAction(name: string, row?: SubstrateRecord) {
  const spec = viewSpec(view, kinds)
  expect(spec.problems.filter((p) => p.severity === "error")).toEqual([])
  const action = spec.actions.find((a) => a.name === name)!
  const host: ActionHost = {
    spec,
    kind: task,
    kinds,
    ctx: { inputs: {}, mode: "page" },
  }
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <Toaster>
        <ActionButton host={host} action={action} record={row} />
      </Toaster>
    </QueryClientProvider>
  )
}

const writes = (method: string) => calls.filter((c) => c.method === method)

beforeEach(() => {
  calls.length = 0
  keys.minted = 0
  stubViewport(390)
  stubFetch()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("an action with a prompt and a confirm", () => {
  it("asks the form, then the confirm over what it collected, then writes", async () => {
    renderAction("rename", t1)
    fireEvent.click(screen.getByRole("button", { name: "Rename" }))

    const sheet = await screen.findByRole("dialog", { name: "Rename" })
    const name = within(sheet).getByLabelText(/^Name/) as HTMLInputElement
    expect(name.value).toBe("Pick a launch date")
    fireEvent.change(name, { target: { value: "Pick the launch date" } })
    fireEvent.click(within(sheet).getByRole("button", { name: "Rename" }))

    // The confirm, with the description and the record it is about; no
    // write yet.
    const confirm = await screen.findByRole("dialog", { name: "Rename?" })
    expect(within(confirm).getByText("Renames the task.")).toBeTruthy()
    expect(within(confirm).getByText("Pick a launch date")).toBeTruthy()
    expect(writes("PATCH")).toHaveLength(0)

    fireEvent.click(within(confirm).getByRole("button", { name: "Rename" }))
    await waitFor(() => expect(writes("PATCH")).toHaveLength(1))
    const patch = writes("PATCH")[0]
    expect(patch.url).toContain(`/${TASK}/t1`)
    expect(patch.body).toEqual({
      properties: { name: "Pick the launch date" },
      ifVersion: 1,
    })
    // Both close once the write landed.
    await waitFor(() => {
      expect(screen.queryByRole("dialog", { name: "Rename?" })).toBeNull()
      expect(screen.queryByRole("dialog", { name: "Rename" })).toBeNull()
    })
  })

  it("keeps the filled form and the one key through a cancelled confirm", async () => {
    renderAction("add")
    fireEvent.click(screen.getByRole("button", { name: "Add" }))
    const sheet = await screen.findByRole("dialog", { name: "Add" })
    fireEvent.change(within(sheet).getByLabelText(/^Name/), {
      target: { value: "Call the printer" },
    })
    fireEvent.click(within(sheet).getByRole("button", { name: "Add" }))

    const first = await screen.findByRole("dialog", { name: "Add?" })
    expect(within(first).getByText("Adds a task.")).toBeTruthy()
    fireEvent.click(within(first).getByRole("button", { name: "Cancel" }))
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Add?" })).toBeNull()
    )
    expect(writes("POST")).toHaveLength(0)

    // The form is where it was, holding what was typed.
    const again = screen.getByRole("dialog", { name: "Add" })
    expect(
      (within(again).getByLabelText(/^Name/) as HTMLInputElement).value
    ).toBe("Call the printer")
    fireEvent.click(within(again).getByRole("button", { name: "Add" }))
    const second = await screen.findByRole("dialog", { name: "Add?" })
    fireEvent.click(within(second).getByRole("button", { name: "Add" }))

    await waitFor(() => expect(writes("POST")).toHaveLength(1))
    const post = writes("POST")[0]
    expect(post.body).toEqual({ properties: { name: "Call the printer" } })
    // One key for the one opened form, cancel and confirm included.
    expect(post.headers["Idempotency-Key"]).toBe("key-1")
    expect(keys.minted).toBe(1)
    await waitFor(() =>
      expect(screen.queryByRole("dialog", { name: "Add" })).toBeNull()
    )
  })
})
