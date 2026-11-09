import type { Writes } from "@substrate/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { choosingStore, makeActions, pendingStore, undoWindow } from "./actions"
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

/** The actions over a contract case, with the kit's writes recorded. */
function over(c: Case) {
  const changes: { kind: string; id: string; properties: unknown }[] = []
  const kept = () =>
    Promise.resolve({ kept: true as const, id: "", shown: undefined })
  const writes: Writes = {
    create: kept,
    change: (kind, id, properties) => {
      changes.push({ kind, id, properties })
      return kept()
    },
    edit: kept,
    remove: kept,
  }
  const typed = {
    create: kept,
    change: kept,
    edit: kept,
    remove: kept,
  }
  const model = buildModel({
    declarations: c.declarations,
    records: c.records,
    grant: c.grant,
    now: new Date(c.now),
    timeZone: c.timeZone,
    listed: true,
  })
  const actions = makeActions(model, typed, typed, writes)
  return { actions, changes }
}

const suggestion = "substrate.reamde.dev/tasks/task/t7"

describe("the undo window", () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    vi.useRealTimers()
    pendingStore.set(new Map())
    choosingStore.set(undefined)
  })

  it("writes nothing when an accept is undone within it", () => {
    const { actions, changes } = over(cases["../../contract/own-tasks.json"])
    actions.act(suggestion, "accept")
    expect(pendingStore.get().get(suggestion)?.action).toBe("accept")
    vi.advanceTimersByTime(undoWindow - 1)
    actions.reopen(suggestion)
    expect(pendingStore.get().has(suggestion)).toBe(false)
    vi.advanceTimersByTime(undoWindow * 2)
    expect(changes).toEqual([])
  })

  it("writes the contract's patch once when it passes", () => {
    const { actions, changes } = over(cases["../../contract/own-tasks.json"])
    actions.act(suggestion, "accept", { edits: { priority: "high" } })
    // A second decision while the window is open is no second write.
    actions.act(suggestion, "accept")
    vi.advanceTimersByTime(undoWindow - 1)
    expect(changes).toEqual([])
    vi.advanceTimersByTime(1)
    expect(changes).toEqual([
      {
        kind: "substrate.reamde.dev/tasks/task",
        id: "t7",
        properties: { status: "open", priority: "high" },
      },
    ])
    vi.advanceTimersByTime(undoWindow * 2)
    expect(changes).toHaveLength(1)
    expect(pendingStore.get().has(suggestion)).toBe(false)
  })

  it("asks which value where the move has several, and writes the one chosen", () => {
    const c = cases["../../contract/two-destinations.json"]
    const { actions, changes } = over(c)
    const path = "ada.example.com/flow/ticket/k1"
    actions.act(path, "accept")
    expect(choosingStore.get()).toMatchObject({
      path,
      action: "accept",
      choices: ["backlog", "active"],
    })
    actions.act(path, "accept", { to: "active" })
    vi.advanceTimersByTime(undoWindow)
    expect(changes).toEqual([
      {
        kind: "ada.example.com/flow/ticket",
        id: "k1",
        properties: { status: "active" },
      },
    ])
  })

  it("writes a reopen at once, clearing what the close stamped", () => {
    const { actions, changes } = over(cases["../../contract/own-tasks.json"])
    actions.reopen("substrate.reamde.dev/tasks/task/t8")
    expect(changes).toEqual([
      {
        kind: "substrate.reamde.dev/tasks/task",
        id: "t8",
        properties: { status: "open", completedAt: null },
      },
    ])
  })
})
