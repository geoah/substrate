/** The sheet's pure decisions: which rows show in what order and which are
 * locked, what one edit writes, and the dates a person reads. */

import { describe, expect, it } from "vitest"

import { friendlyDateTime, friendlyDay, fromLocalInput } from "./dates"
import { editStyle, propertyWrite } from "./sheet-model"
import { sheetRows } from "./sheet-rows"
import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { fieldOf } from "@/lib/record-form"
import { propSpecsByName } from "@/lib/record-schema"

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
    displayTemplate: "{name|title}",
    traits: ["substrate.reamde.dev/core/temporal(point: dueAt)"],
    properties: {
      name: { type: "string" },
      description: { type: "markdown" },
      zeta: { type: "string" },
      status: { type: "state", states: ["open", "done"], initial: "open" },
      project: { type: "reference", kind: "ada.example.com/tasks/project" },
      priority: { type: "enum", values: ["low", "high"] },
      stamp: { type: "datetime", managed: true },
      token: { type: "string", writer: "oauth" },
    },
  },
}

const record: SubstrateRecord = {
  id: "t1",
  kind: TASK,
  properties: {
    name: "A",
    title: "A",
    description: "prose",
    zeta: "z",
    status: "open",
    priority: "low",
    project: { ref: "ada.example.com/tasks/project/p1" },
    dueAt: "2026-09-26T10:00:00Z",
    extra: "not declared",
  },
  labels: {},
  version: 3,
  createdAt: "",
  updatedAt: "",
}

describe("sheetRows", () => {
  const rows = sheetRows(record, task)
  it("reads states, then references, enums, dates, then the rest", () => {
    expect(rows.filled.map((r) => r.name)).toEqual([
      "status",
      "project",
      "priority",
      "dueAt",
      "extra",
      "zeta",
    ])
  })

  it("names the title and body properties and keeps them off the rows", () => {
    expect(rows.title).toBe("name")
    expect(rows.body?.name).toBe("description")
    expect(rows.all.some((r) => r.name === "title")).toBe(false)
  })

  it("locks what the engine stamps, what a host keeps, and what nobody declared", () => {
    const lock = (name: string) => rows.all.find((r) => r.name === name)?.lock
    expect(lock("stamp")).toBe("managed")
    expect(lock("token")).toBe("host")
    expect(lock("extra")).toBe("undeclared")
    expect(lock("zeta")).toBeUndefined()
    expect(rows.all.find((r) => r.name === "zeta")?.field).toBeDefined()
  })

  it("locks every row of a provider's copy", () => {
    const ro = sheetRows(record, task, true)
    expect(ro.all.every((r) => r.lock === "provider" && !r.field)).toBe(true)
  })
})

describe("propertyWrite", () => {
  const field = (name: string) =>
    fieldOf(propSpecsByName(task).find((s) => s.name === name)!)

  it("writes the one property that moved", () => {
    expect(propertyWrite(field("zeta"), "z", "y")).toEqual({
      properties: { zeta: "y" },
    })
  })

  it("writes nothing when the value did not move", () => {
    expect(propertyWrite(field("zeta"), "z", "z")).toEqual({})
    expect(
      propertyWrite(
        field("project"),
        { ref: "ada.example.com/tasks/project/p1" },
        { kind: "ada.example.com/tasks/project", id: "p1" }
      )
    ).toEqual({})
  })

  it("empties a value with null, and a blank on an empty one is nothing", () => {
    expect(propertyWrite(field("zeta"), "z", "")).toEqual({
      properties: { zeta: null },
    })
    expect(propertyWrite(field("zeta"), undefined, "")).toEqual({})
  })

  it("writes a picked reference as its path", () => {
    expect(
      propertyWrite(field("project"), undefined, {
        kind: "ada.example.com/tasks/project",
        id: "p2",
      })
    ).toEqual({ properties: { project: "ada.example.com/tasks/project/p2" } })
  })

  it("edits a state and an enum from a list, a reference on its line", () => {
    expect(editStyle(field("status"))).toBe("pop")
    expect(editStyle(field("priority"))).toBe("pop")
    expect(editStyle(field("project"))).toBe("line")
    expect(editStyle(field("zeta"))).toBe("line")
  })
})

describe("dates", () => {
  const now = new Date(2026, 8, 25, 12, 0).getTime()
  it("says the day relative where that is shorter", () => {
    expect(friendlyDay(new Date(2026, 8, 25, 9).toISOString(), now)).toBe(
      "Today"
    )
    expect(friendlyDay(new Date(2026, 8, 26, 9).toISOString(), now)).toBe(
      "Tomorrow"
    )
    expect(friendlyDay(new Date(2026, 8, 24, 9).toISOString(), now)).toBe(
      "Yesterday"
    )
    expect(friendlyDay(new Date(2026, 9, 20, 9).toISOString(), now)).toBe(
      "20 Oct"
    )
    expect(friendlyDay(new Date(2025, 9, 20, 9).toISOString(), now)).toBe(
      "20 Oct 2025"
    )
  })

  it("adds the time unless it is midnight", () => {
    expect(
      friendlyDateTime(new Date(2026, 8, 26, 10, 5).toISOString(), now)
    ).toBe("Tomorrow, 10:05")
    expect(friendlyDateTime(new Date(2026, 8, 26).toISOString(), now)).toBe(
      "Tomorrow"
    )
  })

  it("turns a local input back into an instant", () => {
    expect(fromLocalInput("")).toBe("")
    expect(fromLocalInput("2026-09-26T10:00")).toBe(
      new Date(2026, 8, 26, 10, 0).toISOString().replace(".000Z", "Z")
    )
  })
})
