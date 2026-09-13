/** The cache surgery around a write: an optimistic transition drops a row
 * from the pages it left and puts it back, dropped row included, when the
 * server refuses; a created row is placed once per cached list it matches,
 * in an infinite query's first page and nowhere else. */

import type { InfiniteData } from "@tanstack/react-query"
import { QueryClient } from "@tanstack/react-query"
import { beforeEach, describe, expect, it, vi } from "vitest"

import {
  patchRecord,
  recordQueryOptions,
  recordsInfiniteOptions,
  recordsQueryOptions,
} from "@/lib/api/records"
import {
  ApiError,
  type KindInfo,
  type Page,
  type SubstrateRecord,
} from "@/lib/api/types"
import { insertIntoCaches, runAction, updateCaches } from "./actions"
import type { ActionSpec, ViewSpec } from "./spec"

vi.mock("@/components/ui/toast", () => ({
  toast: { add: vi.fn(() => "t"), close: vi.fn() },
}))

vi.mock("@/lib/api/records", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/lib/api/records")>()),
  patchRecord: vi.fn(),
  createRecord: vi.fn(),
  deleteRecord: vi.fn(),
}))

const task: KindInfo = {
  identity: "ada.example.com/tasks/task",
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    properties: {
      name: { type: "string" },
      status: {
        type: "state",
        states: ["open", "done"],
        initial: "open",
        transitions: [
          { from: "open", to: "done" },
          { from: "done", to: "open" },
        ],
      },
    },
  },
}

function row(id: string, status: string, version = 1): SubstrateRecord {
  return {
    id,
    kind: task.identity,
    properties: { name: id, status },
    labels: {},
    version,
    createdAt: "",
    updatedAt: "",
  }
}

const done: ActionSpec = {
  name: "done",
  label: "Done",
  verb: "transition",
  placement: "row",
  to: "done",
  prompt: [],
  set: {},
  confirm: false,
}

const spec: ViewSpec = {
  id: "v",
  name: "V",
  layout: "list",
  kind: task.identity,
  requiresAtLeast: {},
  filter: { properties: { status: { eq: "open" } } },
  orderBy: [],
  show: [],
  facets: [],
  window: {},
  first: 50,
  related: [],
  attach: [],
  replaces: false,
  actions: [done],
  permissions: { reads: { kinds: [] }, writes: [], call: [], agents: [] },
  problems: [],
}

const list = { authority: "ada.example.com", package: "tasks", name: "task" }
const openPages = recordsInfiniteOptions({
  ...list,
  filter: { properties: { status: { eq: "open" } } },
})
const openPage = recordsQueryOptions({
  ...list,
  filter: { properties: { status: { eq: "open" } } },
})
const donePage = recordsQueryOptions({
  ...list,
  filter: { properties: { status: { eq: "done" } } },
})
const single = (id: string) =>
  recordQueryOptions(list.authority, list.package, list.name, id)

function page(records: SubstrateRecord[]): Page {
  return { records, head: 0, generation: "g" }
}

function seeded() {
  const client = new QueryClient()
  client.setQueryData<InfiniteData<Page>>(openPages.queryKey, {
    pages: [page([row("t1", "open"), row("t2", "open")]), page([])],
    pageParams: ["", "c1"],
  })
  client.setQueryData<Page>(openPage.queryKey, page([row("t1", "open")]))
  client.setQueryData<Page>(donePage.queryKey, page([row("t3", "done")]))
  client.setQueryData(single("t1").queryKey, row("t1", "open"))
  return client
}

const idsIn = (client: QueryClient, key: readonly unknown[]) => {
  const data = client.getQueryData<Page | InfiniteData<Page>>(key)
  if (!data) return []
  return "pages" in data
    ? data.pages.map((p) => p.records.map((r) => r.id))
    : data.records.map((r) => r.id)
}

beforeEach(() => {
  vi.mocked(patchRecord).mockReset()
})

describe("a refused transition", () => {
  it("puts back the row it dropped from the pages it left", async () => {
    const client = seeded()
    const during: unknown[] = []
    vi.mocked(patchRecord).mockImplementation(() => {
      during.push(idsIn(client, openPages.queryKey))
      during.push(idsIn(client, donePage.queryKey))
      return Promise.reject(new ApiError("conflict", "moved", 409))
    })
    const out = await runAction({
      action: done,
      spec,
      kind: task,
      kinds: [task],
      ctx: { inputs: {}, mode: "page" },
      queryClient: client,
      record: row("t1", "open"),
    })
    expect(out.ok).toBe(false)
    // Optimistically the row left the open pages. A page it newly matches is
    // not guessed at: the server's order decides where it lands there.
    expect(during).toEqual([[["t2"], []], ["t3"]])
    // Rolled back to exactly what was there, version included.
    expect(idsIn(client, openPages.queryKey)).toEqual([["t1", "t2"], []])
    expect(idsIn(client, openPage.queryKey)).toEqual(["t1"])
    expect(idsIn(client, donePage.queryKey)).toEqual(["t3"])
    expect(client.getQueryData(single("t1").queryKey)).toEqual(
      row("t1", "open")
    )
  })

  it("does not undo another row's update that landed while it was out", async () => {
    const client = seeded()
    vi.mocked(patchRecord).mockImplementation(() => {
      // t2 moved on (another action's success, a change from the tail)
      // while t1's patch was on the wire.
      updateCaches(client, task, row("t2", "open", 5))
      return Promise.reject(new ApiError("conflict", "moved", 409))
    })
    const out = await runAction({
      action: done,
      spec,
      kind: task,
      kinds: [task],
      ctx: { inputs: {}, mode: "page" },
      queryClient: client,
      record: row("t1", "open"),
    })
    expect(out.ok).toBe(false)
    expect(idsIn(client, openPages.queryKey)).toEqual([["t1", "t2"], []])
    const first = client.getQueryData<InfiniteData<Page>>(openPages.queryKey)!
      .pages[0].records
    expect(first.find((r) => r.id === "t1")).toEqual(row("t1", "open"))
    expect(first.find((r) => r.id === "t2")).toEqual(row("t2", "open", 5))
  })

  it("keeps the saved shape when the server agrees", async () => {
    const client = seeded()
    vi.mocked(patchRecord).mockResolvedValue(row("t1", "done", 2))
    const out = await runAction({
      action: done,
      spec,
      kind: task,
      kinds: [task],
      ctx: { inputs: {}, mode: "page" },
      queryClient: client,
      record: row("t1", "open"),
    })
    expect(out.ok).toBe(true)
    expect(idsIn(client, openPages.queryKey)).toEqual([["t2"], []])
    expect(idsIn(client, openPage.queryKey)).toEqual([])
    expect(idsIn(client, donePage.queryKey)).toEqual(["t3"])
    expect(client.getQueryData(single("t1").queryKey)).toEqual(
      row("t1", "done", 2)
    )
  })
})

describe("insertIntoCaches", () => {
  it("places a created row once per matching list, in the first page", () => {
    const client = seeded()
    insertIntoCaches(client, task, row("t9", "open"))
    expect(idsIn(client, openPages.queryKey)).toEqual([["t9", "t1", "t2"], []])
    expect(idsIn(client, openPage.queryKey)).toEqual(["t9", "t1"])
    expect(idsIn(client, donePage.queryKey)).toEqual(["t3"])
  })

  it("leaves a list that already holds the row as it is", () => {
    const client = seeded()
    client.setQueryData<InfiniteData<Page>>(openPages.queryKey, {
      pages: [page([row("t9", "open"), row("t1", "open")]), page([])],
      pageParams: ["", "c1"],
    })
    insertIntoCaches(client, task, row("t9", "open"))
    expect(idsIn(client, openPages.queryKey)).toEqual([["t9", "t1"], []])
    expect(idsIn(client, openPage.queryKey)).toEqual(["t9", "t1"])
  })
})
