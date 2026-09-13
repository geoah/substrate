/** The declaration the guest reads: the machine's arms, the enum labels, the
 * temporal point, all off a `KindInfo` and nothing else. */

import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import { declarationOf, humanizeName, temporalPointOf } from "./declaration"

const task: KindInfo = {
  identity: "ada.example.com/tasks/task",
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 8,
  source: "installed",
  description: "a task",
  definition: {
    displayTemplate: "{name|title}",
    traits: ["temporal(point: dueAt)", "recurring"],
    properties: {
      name: { type: "string", required: true },
      priority: {
        type: "enum",
        values: [
          { value: "none", label: "" },
          { value: "high", label: "High priority" },
        ],
      },
      status: {
        type: "state",
        initial: "open",
        states: ["proposed", "open", "done", "abandoned"],
        transitions: [
          { from: "proposed", to: "open" },
          { from: "open", to: "done" },
          { from: "open", to: "abandoned" },
        ],
      },
      dueAt: { type: "datetime" },
    },
  },
}

const event: KindInfo = {
  ...task,
  identity: "ada.example.com/calendar/calendarevent",
  name: "calendarevent",
  package: "calendar",
  definition: {
    traits: ["temporal(range)"],
    properties: {
      summary: { type: "string" },
      status: { type: "string" },
    },
  },
}

describe("declarationOf", () => {
  const d = declarationOf(task)

  it("lists the properties, required first, with labels", () => {
    expect(d.properties.map((p) => p.name)).toEqual([
      "name",
      "dueAt",
      "priority",
      "status",
    ])
    expect(d.property("dueAt")?.label).toBe("Due at")
    expect(d.property("status")?.states).toEqual([
      "proposed",
      "open",
      "done",
      "abandoned",
    ])
    expect(d.displayTemplate).toBe("{name|title}")
  })

  it("finds the sole state property and refuses to guess between two", () => {
    expect(d.stateProperty()?.name).toBe("status")
    expect(d.stateProperty("status")?.name).toBe("status")
    expect(d.stateProperty("priority")).toBeUndefined()
    const two = declarationOf({
      ...task,
      definition: {
        properties: {
          a: { type: "state", states: ["x", "y"] },
          b: { type: "state", states: ["x", "y"] },
        },
      },
    })
    expect(two.stateProperty()).toBeUndefined()
    expect(two.stateProperty("b")?.name).toBe("b")
  })

  it("admits only the declared arms, and every move where none are declared", () => {
    expect(d.admits("status", "open", "done")).toBe(true)
    expect(d.admits("status", "proposed", "done")).toBe(false)
    expect(d.admits("status", "done", "done")).toBe(false)
    expect(d.transitions("status")).toHaveLength(3)
    const free = declarationOf({
      ...task,
      definition: {
        properties: { s: { type: "state", states: ["a", "b"] } },
      },
    })
    expect(free.admits("s", "a", "b")).toBe(true)
    expect(free.admits("s", undefined, "b")).toBe(false)
  })

  it("labels an enum value and falls back to the value", () => {
    expect(d.labelOf("priority", "high")).toBe("High priority")
    expect(d.labelOf("priority", "none")).toBe("none")
    expect(d.labelOf("status", "open")).toBe("open")
    expect(d.labelOf("missing", 3)).toBe("3")
  })

  it("reads the temporal point from the binding: a remap, a range, none", () => {
    expect(d.temporalPoint).toBe("dueAt")
    expect(declarationOf(event).temporalPoint).toBe("at")
    expect(
      temporalPointOf({ ...task, definition: { properties: {} } })
    ).toBeUndefined()
    expect(
      temporalPointOf({ ...task, definition: { traits: ["temporal(point)"] } })
    ).toBe("at")
  })
})

describe("humanizeName", () => {
  it("spaces camelCase and keeps acronyms", () => {
    expect(humanizeName("backfillDepth")).toBe("Backfill depth")
    expect(humanizeName("baseURL")).toBe("Base URL")
    expect(humanizeName("name")).toBe("Name")
  })
})
