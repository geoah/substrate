// @vitest-environment jsdom
/** The screen-record hook against a stubbed substrate. A sheet opened
 * locally (the segment already spent on the parent) is read through the
 * single-record query and never held as the tapped row: the row primes the
 * read and the read replaces it, a cache write reaches the sheet, and the
 * version the next `ifVersion` would carry is the current one. The selection
 * clears when the parent or the view changes and when the sheet closes; a
 * tap while the segment is free goes through the route instead. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { act, cleanup, renderHook, waitFor } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const back = vi.fn()
vi.mock("@tanstack/react-router", () => ({
  useRouter: () => ({ history: { back } }),
  useRouterState: () => undefined,
  useCanGoBack: () => false,
  useNavigate: () => vi.fn(),
}))

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { viewSpec } from "@/lib/apps/view-spec"
import { useScreenRecord } from "./use-screen-record"

const PROJECT = "ada.example.com/tasks/project"
const TASK = "ada.example.com/tasks/task"
const VIEW = "substrate.reamde.dev/core/view"

const project: KindInfo = {
  identity: PROJECT,
  name: "project",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
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
  version: 1,
  source: "installed",
  description: "",
  definition: {
    names: { singular: "task" },
    displayTemplate: "{name}",
    properties: {
      name: { type: "string" },
      project: { type: "reference", kind: "project" },
      status: { type: "state", states: ["open", "done"], initial: "open" },
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

const website = record(PROJECT, "website", { name: "Website relaunch" })
const taxes = record(PROJECT, "taxes", { name: "Taxes 2026" })
/** The row as a list page had it ... */
const row = record(TASK, "t1", {
  name: "Pick a launch date",
  status: "open",
  project: { ref: `${PROJECT}/website` },
})
/** ... and the record as the substrate has it now: moved once since. */
const served = { ...row, version: 2 }

function viaView(id: string) {
  return record(VIEW, id, {
    name: "Open tasks",
    layout: "list",
    kind: { ref: `substrate.reamde.dev/core/kind/${TASK}` },
    via: "project",
  })
}
const byProject = viaView("tasks-by-project")

const calls: string[] = []

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function stubFetch() {
  vi.stubGlobal("fetch", (input: RequestInfo | URL) => {
    const url = String(input)
    calls.push(url)
    const path = url.split("?")[0]
    if (path.endsWith("/tasks/project/website")) {
      return Promise.resolve(json(website))
    }
    if (path.endsWith("/tasks/project/taxes")) {
      return Promise.resolve(json(taxes))
    }
    if (path.endsWith("/tasks/task/t1")) return Promise.resolve(json(served))
    return Promise.resolve(json({ error: `unexpected ${url}` }, 500))
  })
}

interface Props {
  segment?: string
  view: SubstrateRecord
}

function setup(segment?: string, view = byProject) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  )
  const toRecord = vi.fn()
  const toView = vi.fn()
  const clear = vi.fn()
  const hook = renderHook(
    (props: Props) =>
      useScreenRecord({
        spec: viewSpec(props.view, kinds),
        kind: task,
        kinds,
        segment: props.segment,
        toRecord,
        toView,
        clear,
      }),
    { wrapper, initialProps: { segment, view } }
  )
  return { ...hook, client, toRecord, toView, clear }
}

const t1Key = ["record", "ada.example.com", "tasks", "task", "t1"]

beforeEach(() => {
  calls.length = 0
  back.mockReset()
  stubFetch()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("a sheet opened locally under a parent", () => {
  it("is read through the record query, so a cache write reaches it", async () => {
    const { result, client, toRecord } = setup("website")
    await waitFor(() =>
      expect(result.current.parent?.record.id).toBe("website")
    )
    expect(result.current.role).toBe("parent")
    expect(result.current.sheetOpen).toBe(false)

    act(() => result.current.openRecord(row))
    // The segment is spent: nothing moves in history.
    expect(toRecord).not.toHaveBeenCalled()
    expect(result.current.sheetOpen).toBe(true)
    expect(result.current.sheetSegment).toBe(`${TASK}/t1`)
    // The row primes the read and shows at once ...
    expect(result.current.sheetRecord?.version).toBe(1)
    // ... and the read replaces it with the record as it is now.
    await waitFor(() => expect(result.current.sheetRecord?.version).toBe(2))
    expect(calls.some((u) => u.endsWith("/tasks/task/t1"))).toBe(true)

    // What a transition does to the cache is what the sheet shows next.
    act(() => {
      client.setQueryData(t1Key, {
        ...served,
        version: 3,
        properties: { ...served.properties, status: "done" },
      })
    })
    await waitFor(() => expect(result.current.sheetRecord?.version).toBe(3))
    expect(result.current.sheetRecord?.properties.status).toBe("done")
  })

  it("clears when the parent changes", async () => {
    const { result, rerender } = setup("website")
    await waitFor(() => expect(result.current.parent).toBeDefined())
    act(() => result.current.openRecord(row))
    await waitFor(() => expect(result.current.sheetRecord?.version).toBe(2))

    rerender({ segment: "taxes", view: byProject })
    expect(result.current.sheetOpen).toBe(false)
    expect(result.current.sheetRecord).toBeUndefined()
    expect(result.current.sheetSegment).toBeUndefined()
    await waitFor(() => expect(result.current.parent?.record.id).toBe("taxes"))
    expect(result.current.sheetOpen).toBe(false)
  })

  it("clears when the view changes", async () => {
    const { result, rerender } = setup("website")
    await waitFor(() => expect(result.current.parent).toBeDefined())
    act(() => result.current.openRecord(row))
    expect(result.current.sheetOpen).toBe(true)

    rerender({ segment: "website", view: viaView("tasks-by-project-too") })
    expect(result.current.sheetOpen).toBe(false)
    expect(result.current.sheetRecord).toBeUndefined()
  })

  it("closes without touching history", async () => {
    const { result, clear } = setup("website")
    await waitFor(() => expect(result.current.parent).toBeDefined())
    act(() => result.current.openRecord(row))
    expect(result.current.sheetOpen).toBe(true)

    act(() => result.current.closeSheet())
    expect(result.current.sheetOpen).toBe(false)
    expect(result.current.sheetRecord).toBeUndefined()
    expect(back).not.toHaveBeenCalled()
    expect(clear).not.toHaveBeenCalled()
  })
})

describe("a tap while the segment is free", () => {
  it("goes through the route, stamped, with the read primed", () => {
    const { result, client, toRecord } = setup(undefined)
    expect(result.current.role).toBeUndefined()
    act(() => result.current.openRecord(row))
    expect(toRecord).toHaveBeenCalledWith(`${TASK}/t1`, {
      replace: false,
      state: { sheet: true },
    })
    expect(result.current.sheetOpen).toBe(false)
    expect(client.getQueryData(t1Key)).toEqual(row)
  })
})
