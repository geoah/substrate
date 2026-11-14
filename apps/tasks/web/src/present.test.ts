import { describe, expect, it } from "vitest"
import { heldScopes } from "./actions"
import { presentList, presentLists, row } from "./present"
import type { Declaration, Envelope } from "./sources"
import { buildModel, type Settings } from "./state"

interface Case {
  now: string
  timeZone: string
  grant: string[]
  declarations: Declaration[]
  records: Envelope[]
}

const cases = import.meta.glob<Case>("../../contract/*.json", {
  eager: true,
  import: "default",
})
const ownTasks = cases["../../contract/own-tasks.json"]
const second = cases["../../contract/second-collection.json"]

const settings: Settings = {
  remind: false,
  remindHour: 9,
  order: "manual",
  showCompleted: false,
}

function model(c: Case) {
  return buildModel({
    declarations: c.declarations,
    records: c.records,
    grant: c.grant,
    now: new Date(c.now),
    timeZone: c.timeZone,
    listed: true,
  })
}

describe("the presenter over the contract's cases", () => {
  it("leads Needs you with the queue row and ends it with the counted No date row", () => {
    const m = model(ownTasks)
    const view = presentList(
      { kind: "needs" },
      m,
      settings,
      undefined,
      new Map(),
      new Date(ownTasks.now)
    )
    expect(view.queue).toMatchObject({
      title: "1 suggestion",
      detail: "Tasks waiting for you to accept or dismiss",
    })
    expect(view.noDate).toMatchObject({
      title: "No date",
      detail: "Open tasks with no due date",
      count: 2,
    })
    expect(view.bands.map((b) => [b.title, b.count])).toEqual([
      ["Late or urgent", 3],
      ["Coming up", 1],
      ["Later", 1],
    ])
  })

  it("queues the suggestions with Dismiss and Accept where the collection is writable", () => {
    const m = model(second)
    const view = presentList(
      { kind: "suggestions" },
      m,
      settings,
      undefined,
      new Map(),
      new Date(second.now)
    )
    expect(view.title).toBe("Suggestions")
    expect(view.selects).toBe(true)
    expect(view.open.map((r) => [r.title, r.suggestion])).toEqual([
      [
        "Fix the fence · C-2",
        {
          written: expect.any(String),
          dismiss: true,
          accept: true,
          viewOnly: false,
        },
      ],
    ])
    expect(view.open[0].collection).toBe("Chores")
  })

  it("sections Lists by group kind, with No list, Missing and the groups with nothing open", () => {
    const lists = presentLists(model(ownTasks))
    expect(lists.search).toBe("Search task lists")
    expect(lists.sections).toHaveLength(1)
    const [s] = lists.sections
    expect([s.title, s.reference]).toEqual([
      "Task lists",
      "substrate.reamde.dev/tasks/list",
    ])
    expect(s.rows.map((r) => [r.title, r.detail, r.open])).toEqual([
      ["Home", "Task list", 4],
      ["Groceries", "Task list", 1],
      ["No list", "Tasks with no list", 1],
      ["Missing", "Tasks whose list is gone or out of reach", 1],
    ])
    expect(s.more).toEqual([])
  })

  it("names a group's kind, its open count and its pin", () => {
    const m = model(second)
    const view = presentList(
      { kind: "list", id: "ada.example.com/work/area/a1" },
      m,
      settings,
      undefined,
      new Map(),
      new Date(second.now)
    )
    expect(view.title).toBe("Garden")
    expect(view.summary).toEqual({ headline: "1 open", detail: "Area" })
    expect(view.queue?.title).toBe("1 suggestion")
    expect(view.group).toEqual({
      kind: "Area",
      reference: "ada.example.com/work/area",
      pinned: false,
    })
  })

  it("says a view-only collection's row has no decisions and no mark to toggle", () => {
    const c = cases["../../contract/view-only-and-pins.json"]
    const m = model(c)
    const t = m.task("ada.example.com/work/chore/y1")!
    const r = row(t, { now: new Date(c.now), model: m })
    expect(r.suggestion).toMatchObject({
      dismiss: false,
      accept: false,
      viewOnly: true,
    })
    expect(r.toggles).toBe(false)
  })

  it("says a task's group is out of reach while its chosen kind waits on approval", () => {
    const c = cases["../../contract/view-only-and-pins.json"]
    const m = model({
      ...c,
      records: c.records.map((r) =>
        r.kind === "substrate.reamde.dev/tasks/collection"
          ? { ...r, properties: { ...r.properties, group: "area" } }
          : r
      ),
    })
    const chore = m.collection("ada.example.com/work/chore")!
    expect(chore.group).toBeNull()
    expect(chore.waitingGroup).toMatchObject({
      property: "area",
      kind: "ada.example.com/work/area",
      readable: false,
    })
    const t = m.task("ada.example.com/work/chore/y1")!
    expect(
      row(t, { now: new Date(c.now), model: m, mixed: true }).listName
    ).toBe("Out of reach")
  })
})

describe("the scopes a sign-in again asks for beyond the request", () => {
  const request = [
    "write:substrate.reamde.dev/tasks/*",
    "read:implements(substrate.reamde.dev/core/lifecycle)",
  ]

  it("are the ones the token holds, never one inferred from the settings", () => {
    expect(heldScopes(request, request)).toEqual([])
    expect(
      heldScopes(
        [
          ...request,
          "write:ada.example.com/flow/ticket",
          "read:ada.example.com/work/area",
        ],
        request
      )
    ).toEqual([
      "read:ada.example.com/work/area",
      "write:ada.example.com/flow/ticket",
    ])
  })
})
