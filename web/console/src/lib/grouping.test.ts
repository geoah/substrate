import { describe, expect, it } from "vitest"

import type { KindInfo } from "@/lib/api/types"
import type { DeclaredProperty } from "@/lib/definition"
import {
  groupFilter,
  groupKeyOf,
  groupSegments,
  groupRunNote,
  groupWords,
  groupableProperties,
  groupedOrderBy,
  treeGroupKeys,
} from "./grouping"

const TASK: KindInfo = {
  identity: "example.com/tasks/task",
  name: "task",
  authority: "example.com",
  package: "tasks",
  version: 1,
  source: "user",
  description: "",
  definition: {
    properties: {
      name: { type: "string" },
      status: { type: "state", states: ["open", "done"], initial: "open" },
      priority: { type: "enum", values: ["low", "high"] },
      project: { type: "reference", kind: "example.com/tasks/project" },
      labels: { type: "enum", values: ["a", "b"], repeated: true },
      watchers: {
        type: "reference",
        kind: "example.com/people/person",
        repeated: true,
      },
    },
  },
} as never

const prop = (name: string) =>
  groupableProperties(TASK).find((p) => p.name === name) as DeclaredProperty

describe("grouping", () => {
  it("offers enums, states and single references", () => {
    expect(groupableProperties(TASK).map((p) => p.name)).toEqual([
      "priority",
      "project",
      "status",
    ])
  })

  it("keys a record by its value, a reference by its path, nothing as empty", () => {
    expect(groupKeyOf({ priority: "high" }, prop("priority"))).toBe("high")
    expect(
      groupKeyOf(
        { project: { ref: "example.com/tasks/project/p1" } },
        prop("project")
      )
    ).toBe("example.com/tasks/project/p1")
    expect(groupKeyOf({}, prop("project"))).toBe("")
  })

  it("orders by the group first, keeping the view's own order inside it", () => {
    expect(groupedOrderBy("updatedAt:desc", undefined)).toBe("updatedAt:desc")
    expect(groupedOrderBy("updatedAt:desc", "priority")).toBe(
      "priority:asc,updatedAt:desc"
    )
    expect(groupedOrderBy("priority:desc", "priority")).toBe("priority:desc")
  })

  it("narrows the view's filter to one group", () => {
    const base = { properties: { status: { eq: "open" } } }
    expect(groupFilter(base, prop("priority"), "high")).toEqual({
      properties: { status: { eq: "open" }, priority: { eq: "high" } },
    })
    expect(groupFilter(undefined, prop("project"), "p/1")).toEqual({
      properties: { project: { in: ["p/1"] } },
    })
    expect(groupFilter(undefined, prop("project"), "")).toEqual({
      properties: { project: { exists: false } },
    })
  })

  it("cuts the rows where the group changes", () => {
    expect(
      groupSegments(["a1", "a2", "b1", "a3"], (r) => r[0]).map((s) => [
        s.key,
        s.rows.length,
      ])
    ).toEqual([
      ["a", 2],
      ["b", 1],
      ["a", 1],
    ])
  })

  it("keeps a subtree under its top-level row's group", () => {
    const rows = [
      { id: "r1", g: "x" },
      { id: "c1", g: "y" },
      { id: "r2", g: "y" },
    ]
    const depth = new Map([
      ["r1", 0],
      ["c1", 1],
      ["r2", 0],
    ])
    expect([
      ...treeGroupKeys(
        rows,
        (id) => depth.get(id),
        (r) => r.g
      ),
    ]).toEqual([
      ["r1", "x"],
      ["c1", "x"],
      ["r2", "y"],
    ])
  })

  it("names a group in words for its fold control", () => {
    expect(groupWords(prop("priority"), "high", "Priority")).toBe(
      "Priority: High"
    )
    expect(groupWords(prop("status"), "done", "Status")).toBe("Status: Done")
    expect(
      groupWords(
        prop("project"),
        "example.com/tasks/project/p1",
        "Project",
        new Map([["example.com/tasks/project/p1", "Launch"]])
      )
    ).toBe("Project: Launch")
    expect(groupWords(prop("project"), "", "Project")).toBe("No project")
  })

  it("says a run of a group carries across pages", () => {
    const count = { value: 19, capped: false }
    const at = (first: boolean, last: boolean, rows: number) => ({
      first,
      last,
      rows,
    })
    expect(groupRunNote(at(true, false, 2), count, 2, true)).toBe(
      "continued from the previous page"
    )
    expect(groupRunNote(at(false, true, 5), count, 1, true)).toBe(
      "continues on the next page"
    )
    expect(groupRunNote(at(true, true, 19), count, 2, false)).toBeUndefined()
    expect(groupRunNote(at(true, false, 2), count, 1, true)).toBeUndefined()
    expect(
      groupRunNote(at(true, false, 2), { value: 1000, capped: true }, 2, true)
    ).toBeUndefined()
  })
})
