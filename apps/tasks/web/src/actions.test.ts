import { SubstrateClient } from "@substrate/client"
import {
  MemoryStorage,
  Outbox,
  RecordWrites,
  steadyConnectivity,
  type RepositoryStorage,
} from "@substrate/local"
import type { Writes } from "@substrate/react"
import { FakeSubstrate, until } from "@substrate/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { makeActions, pendingOf, type TaskDraft } from "./actions"
import type { Declaration, Envelope } from "./sources"
import { buildModel } from "./state"

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
const task = "substrate.reamde.dev/tasks/task"
const t7 = `${task}/t7`

let fake: FakeSubstrate
let storage: RepositoryStorage
let outboxes: Outbox[]

beforeEach(() => {
  fake = new FakeSubstrate()
  vi.stubGlobal("fetch", fake.fetch)
  storage = new MemoryStorage()
  outboxes = []
})

afterEach(async () => {
  for (const o of outboxes) await o.close()
  vi.unstubAllGlobals()
})

/** The kit's outbox over the fake, as the app opens it. */
function outbox(): Outbox {
  const o = new Outbox({
    client: new SubstrateClient(fake.origin, "tok"),
    storage,
    connectivity: steadyConnectivity,
    backoff: [5],
  })
  outboxes.push(o)
  return o
}

/** The kit's writes onto an outbox, as `useWrites` makes them. */
function writesOn(o: Outbox): Writes {
  const keep = async (
    make: () => ReturnType<typeof RecordWrites.edit>,
    id: string
  ) => {
    await o.write(make())
    return { kept: true as const, id, shown: undefined }
  }
  return {
    create: (kind, id, props) =>
      keep(() => RecordWrites.create(kind, id, props), id),
    change: (kind, id, props) =>
      keep(() => RecordWrites.change(kind, id, props), id),
    edit: (kind, id, props, seen, opts) =>
      keep(() => RecordWrites.edit(kind, id, props, seen, opts?.hold), id),
    remove: (kind, id) => keep(() => RecordWrites.delete(kind, id), id),
  }
}

function actionsOn(
  c: Case,
  o: Outbox,
  over: { refuse?: (title: string, why: string) => void; window?: number } = {}
) {
  const model = buildModel({
    declarations: c.declarations,
    records: c.records,
    grant: c.grant,
    now: new Date(c.now),
    timeZone: c.timeZone,
    listed: true,
  })
  const typed = {
    create: () =>
      Promise.resolve({ kept: true as const, id: "", shown: undefined }),
    change: () =>
      Promise.resolve({ kept: true as const, id: "", shown: undefined }),
    edit: () =>
      Promise.resolve({ kept: true as const, id: "", shown: undefined }),
    remove: () =>
      Promise.resolve({ kept: true as const, id: "", shown: undefined }),
  }
  return makeActions({
    model,
    tasks: typed,
    lists: typed,
    writes: writesOn(o),
    decisions: {
      pending: () => pendingOf(o.getSnapshot().entries),
      withdraw: (id) => o.withdraw(id),
    },
    refuse: over.refuse ?? (() => {}),
    undoWindow: over.window ?? 80,
  })
}

const tick = (ms: number) => new Promise((r) => setTimeout(r, ms))

describe("a decision", () => {
  it("is an edit on the version seen, held in the outbox through its window", async () => {
    fake.seed(task, "t7", { name: "Reply", status: "proposed" })
    const o = outbox()
    const actions = actionsOn(ownTasks, o, { window: 5000 })
    actions.act(t7, "accept", { edits: { priority: "high" } })
    await until(() => o.getSnapshot().waiting === 1)
    expect(o.getSnapshot().entries).toMatchObject([
      {
        method: "PATCH",
        kind: task,
        recordID: "t7",
        properties: { status: "open", priority: "high" },
        baseVersion: 1,
        guarded: false,
        tag: "accept",
      },
    ])
    expect(o.getSnapshot().entries[0].holdUntil).toBeGreaterThan(Date.now())
    expect(pendingOf(o.getSnapshot().entries).get(t7)?.action).toBe("accept")
    // A second press in the window is no second decision.
    actions.act(t7, "accept")
    await tick(20)
    expect(o.getSnapshot().waiting).toBe(1)
    expect(fake.writes()).toEqual([])
  })

  it("parks as a conflict when another device moved the record, never writing over it", async () => {
    fake.seed(task, "t7", { name: "Reply", status: "proposed" })
    // Another device dismissed it meanwhile: the record is at version 2.
    fake.change(task, "t7", { status: "abandoned" })
    const o = outbox()
    const actions = actionsOn(ownTasks, o)
    actions.act(t7, "accept")
    await until(() => o.getSnapshot().parked.length === 1, 2000, "the park")
    const patch = fake.requests.filter((r) => r.method === "PATCH")
    expect(patch).toHaveLength(1)
    expect(patch[0].body).toEqual({
      properties: { status: "open" },
      ifVersion: 1,
    })
    expect(o.getSnapshot().parked[0]).toMatchObject({ conflict: true })
    expect(fake.live(task, "t7")?.properties.status).toBe("abandoned")
    // It is not sent again on its own.
    await o.flush()
    expect(fake.requests.filter((r) => r.method === "PATCH")).toHaveLength(1)
  })

  it("writes nothing when undone within its window", async () => {
    fake.seed(task, "t7", { name: "Reply", status: "proposed" })
    const o = outbox()
    const actions = actionsOn(ownTasks, o, { window: 150 })
    actions.act(t7, "accept")
    await until(() => o.getSnapshot().waiting === 1)
    expect(await actions.undo(t7)).toBe(true)
    expect(o.getSnapshot().waiting).toBe(0)
    await tick(300)
    expect(fake.writes()).toEqual([])
  })

  it("writes once when its window passes", async () => {
    fake.seed(task, "t7", { name: "Reply", status: "proposed" })
    const o = outbox()
    const actions = actionsOn(ownTasks, o, { window: 60 })
    actions.act(t7, "dismiss")
    await until(() => fake.writes().length === 1, 2000, "the window")
    await tick(100)
    expect(fake.writes()).toEqual([`PATCH ${t7}`])
    expect(fake.live(task, "t7")?.properties.status).toBe("abandoned")
  })

  it("outlives a reload before its window closes, and is sent once at its moment", async () => {
    fake.seed(task, "t7", { name: "Reply", status: "proposed" })
    const first = outbox()
    actionsOn(ownTasks, first, { window: 200 }).act(t7, "accept")
    await until(() => first.getSnapshot().waiting === 1)
    await first.close()
    const second = outbox()
    await second.ready
    expect(pendingOf(second.getSnapshot().entries).get(t7)?.action).toBe(
      "accept"
    )
    expect(fake.writes()).toEqual([])
    await until(() => fake.writes().length === 1, 2000, "the moment")
    await tick(100)
    expect(fake.writes()).toEqual([`PATCH ${t7}`])
  })

  it("is sent at once by a reload after its window closed", async () => {
    fake.seed(task, "t7", { name: "Reply", status: "proposed" })
    const first = outbox()
    actionsOn(ownTasks, first, { window: 30 }).act(t7, "accept")
    await until(() => first.getSnapshot().waiting === 1)
    await first.close()
    await tick(60)
    const second = outbox()
    await second.ready
    expect(pendingOf(second.getSnapshot().entries).size).toBe(0)
    await until(() => fake.writes().length === 1, 2000, "the send")
    await tick(50)
    expect(fake.writes()).toEqual([`PATCH ${t7}`])
  })

  it("asks which value where the move has several", async () => {
    const o = outbox()
    const c = cases["../../contract/two-destinations.json"]
    const { choosingStore } = await import("./actions")
    actionsOn(c, o).act("ada.example.com/flow/ticket/k1", "accept")
    expect(choosingStore.get()?.choices).toEqual(["backlog", "active"])
    choosingStore.set(undefined)
    expect(o.getSnapshot().waiting).toBe(0)
  })
})

describe("a new task", () => {
  const errand = "ada.example.com/home/errand"
  const edges = cases["../../contract/edges.json"]
  /** The edges case with new tasks saved to its errands. */
  const toErrands: Case = {
    ...edges,
    records: [
      ...edges.records,
      {
        kind: "substrate.reamde.dev/tasks/preference",
        id: "p",
        version: 1,
        createdAt: edges.now,
        properties: { newTasks: errand },
      },
    ],
  }
  const draft: TaskDraft = {
    name: "Post the parcel",
    notes: "",
    link: "",
    dueKind: "none",
    priority: "high",
  }

  it("goes from quick add to the collection Save new tasks to names, through its readings", async () => {
    const o = outbox()
    actionsOn(toErrands, o).quickAdd("Post the parcel")
    await until(() => fake.writes().length === 1, 2000, "the create")
    expect(fake.writes()[0]).toMatch(new RegExp(`^PUT ${errand}/`))
    expect(fake.requests.at(-1)?.body).toEqual({
      properties: { name: "Post the parcel" },
      ifVersion: 0,
    })
  })

  it("goes from the editor there too", async () => {
    const o = outbox()
    const kept = await actionsOn(toErrands, o).save(undefined, draft, () => {})
    expect(kept).toBe(true)
    expect(o.getSnapshot().entries).toMatchObject([
      {
        method: "PUT",
        kind: errand,
        properties: { name: "Post the parcel", priority: "high" },
      },
    ])
  })

  it("is refused, with the reason, where the collection cannot take it", async () => {
    const second = cases["../../contract/second-collection.json"]
    const toChores: Case = {
      ...second,
      records: [
        ...second.records,
        {
          kind: "substrate.reamde.dev/tasks/preference",
          id: "p",
          version: 1,
          createdAt: second.now,
          properties: { newTasks: "ada.example.com/work/chore" },
        },
      ],
    }
    const o = outbox()
    const said: string[] = []
    const actions = actionsOn(toChores, o, {
      refuse: (title, why) => said.push(`${title}: ${why}`),
    })
    actions.quickAdd("Mow the lawn")
    let editor: string | undefined
    const kept = await actions.save(
      undefined,
      draft,
      (why, title) => (editor = `${title}: ${why}`)
    )
    expect(kept).toBe(false)
    const words =
      "Can't add a task: Chores has no title Substrate Tasks writes."
    expect(said).toEqual([words])
    expect(editor).toBe(words)
    expect(o.getSnapshot().waiting).toBe(0)
  })
})
