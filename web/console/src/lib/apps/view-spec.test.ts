/** One row decoded: defaults resolved, references read down to identities,
 * and the problems one row can answer alone, each at the path it sits at. */

import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  defaultShow,
  floorProblems,
  floorShortfalls,
  packageOf,
  viewSpec,
} from "./view-spec"

const task: KindInfo = {
  identity: "ada.example.com/tasks/task",
  name: "task",
  authority: "ada.example.com",
  package: "tasks",
  version: 1,
  source: "installed",
  description: "",
  definition: {
    displayTemplate: "{name|title}",
    traits: ["temporal(point: dueAt)"],
    properties: {
      name: { type: "string" },
      priority: { type: "enum", values: ["none", "high"], default: "none" },
      status: {
        type: "state",
        states: ["open", "done"],
        initial: "open",
        transitions: [{ from: "open", to: "done" }],
      },
      completedAt: { type: "datetime" },
      project: { type: "reference", kind: "project" },
      url: { type: "url" },
      syncedBy: { type: "string", writer: "connector" },
      updatedAt: { type: "datetime" },
    },
  },
}

function view(properties: Record<string, unknown>): SubstrateRecord {
  return {
    id: "v",
    kind: "substrate.reamde.dev/core/view",
    properties: { name: "V", layout: "list", ...properties },
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }
}

const KIND_REF = {
  ref: "substrate.reamde.dev/core/kind/ada.example.com/tasks/task",
}

describe("viewSpec", () => {
  it("reads references down to identities and resolves defaults", () => {
    const spec = viewSpec(
      view({
        kind: KIND_REF,
        opens: { ref: "substrate.reamde.dev/core/view/other" },
        actions: [
          { name: "done", verb: "transition", to: "done" },
          { name: "add", verb: "create", prompt: ["name"] },
        ],
      }),
      [task]
    )
    expect(spec.kind).toBe("ada.example.com/tasks/task")
    expect(spec.opens).toBe("other")
    expect(spec.first).toBe(50)
    expect(spec.actions.map((a) => [a.label, a.placement])).toEqual([
      ["Done", "row"],
      ["Add", "primary"],
    ])
    expect(spec.problems).toEqual([])
  })

  it("names what one row gets wrong, at its path", () => {
    const spec = viewSpec(
      view({
        kind: KIND_REF,
        trait: { ref: "substrate.reamde.dev/core/trait/temporal" },
        show: ["prority", "updatedAt"],
        groupBy: "completedAt",
        actions: [
          { name: "close", verb: "transition", to: "closed" },
          { name: "sync", verb: "patch", set: { syncedBy: "x" } },
          { name: "web", verb: "link", href: "name" },
          {
            name: "pin",
            verb: "create",
            prompt: ["name"],
            set: { project: "$record" },
          },
        ],
      }),
      [task]
    )
    const at = (path: string) =>
      spec.problems.find((p) => p.path === path)?.message
    expect(at("kind")).toMatch(/not both/)
    expect(at("show[0]")).toMatch(/did you mean priority/)
    expect(at("show[1]")).toMatch(/the kind declares/)
    expect(at("groupBy")).toMatch(/temporal point/)
    expect(at("actions[0].to")).toMatch(/not a state/)
    expect(at("actions[1].set.syncedBy")).toMatch(/connector/)
    expect(at("actions[2].href")).toMatch(/url-typed/)
    expect(at("actions[3].set")).toMatch(/\$record/)
    // The bad transition, patch and link are dropped; the create stays.
    expect(spec.actions.map((a) => a.name)).toEqual(["pin"])
  })

  it("holds the layout contracts", () => {
    const board = viewSpec(
      view({ kind: KIND_REF, layout: "board", groupBy: "name" }),
      [task]
    )
    expect(board.problems.some((p) => p.severity === "error")).toBe(true)
    const custom = viewSpec(
      view({ kind: KIND_REF, layout: "custom", source: "<p>" }),
      [task]
    )
    expect(custom.problems.map((p) => p.path)).toContain(
      "permissions.reads.kinds"
    )
  })

  it("leaves a kind the registry lacks to the renderer", () => {
    const spec = viewSpec(view({ kind: KIND_REF, show: ["anything"] }), [])
    expect(spec.kind).toBe("ada.example.com/tasks/task")
    expect(spec.problems).toEqual([])
  })
})

describe("defaultShow and packageOf", () => {
  it("drops the heading from the default cells", () => {
    expect(defaultShow(task)).not.toContain("name")
    expect(defaultShow(task)).toContain("priority")
  })
  it("reads the package identity off a kind identity", () => {
    expect(packageOf("ada.example.com/tasks/task")).toBe(
      "ada.example.com/tasks"
    )
  })
})

describe("facets and descriptions", () => {
  it("keeps a facet on a state, an enum or a reference and names the rest", () => {
    const spec = viewSpec(
      view({
        kind: KIND_REF,
        facets: ["priority", "status", "project", "name", "nope"],
      }),
      [task]
    )
    expect(spec.facets).toEqual(["priority", "status", "project"])
    expect(spec.problems.map((p) => [p.path, p.severity])).toEqual([
      ["facets[3]", "warning"],
      ["facets[4]", "warning"],
    ])
    expect(spec.problems[0].message).toMatch(
      /name is not a state, an enum or a reference/
    )
    expect(spec.problems[1].message).toMatch(/"nope" is not a property/)
  })

  it("leaves a trait view's facets to the render", () => {
    const spec = viewSpec(
      view({
        trait: { ref: "substrate.reamde.dev/core/trait/temporal" },
        layout: "timeline",
        facets: ["status"],
      }),
      [task]
    )
    expect(spec.facets).toEqual(["status"])
    expect(spec.problems).toEqual([])
  })

  it("carries an action's description", () => {
    const spec = viewSpec(
      view({
        kind: KIND_REF,
        actions: [
          {
            name: "done",
            verb: "transition",
            to: "done",
            description: "Marks the task done.",
          },
        ],
      }),
      [task]
    )
    expect(spec.actions[0].description).toBe("Marks the task done.")
  })

  it("says a shadowed column once, as what the ordering means", () => {
    const spec = viewSpec(
      view({
        kind: KIND_REF,
        orderBy: [{ property: "updatedAt", desc: true }],
        show: ["updatedAt"],
      }),
      [task]
    )
    expect(spec.problems).toEqual([
      {
        path: "orderBy[0].property",
        message:
          "Ordered by when the substrate last wrote each record, not the `updatedAt` the kind declares.",
        severity: "warning",
      },
    ])
    const shown = viewSpec(view({ kind: KIND_REF, show: ["updatedAt"] }), [
      task,
    ])
    expect(shown.problems.map((p) => p.message)).toEqual([
      "Shows when the substrate last wrote each record, not the `updatedAt` the kind declares.",
    ])
  })
})

describe("floorProblems", () => {
  const floors = {
    requiresAtLeast: {
      "ada.example.com/tasks": 3,
      "ada.example.com/people": 1,
    },
  }

  it("names a package below its floor and one not installed", () => {
    expect(floorShortfalls(floors, { "ada.example.com/tasks": 2 })).toEqual([
      { package: "ada.example.com/tasks", floor: 3, installed: 2 },
      { package: "ada.example.com/people", floor: 1, installed: undefined },
    ])
    expect(floorProblems(floors, { "ada.example.com/tasks": 2 })).toEqual([
      {
        path: "requiresAtLeast.ada.example.com/tasks",
        message: "needs ada.example.com/tasks at version 3 (installed 2)",
        severity: "error",
      },
      {
        path: "requiresAtLeast.ada.example.com/people",
        message: "needs ada.example.com/people at version 1 (not installed)",
        severity: "error",
      },
    ])
  })

  it("is quiet when every floor is met, and while the versions are unread", () => {
    expect(
      floorProblems(floors, {
        "ada.example.com/tasks": 3,
        "ada.example.com/people": 4,
      })
    ).toEqual([])
    expect(floorProblems(floors, undefined)).toEqual([])
    expect(floorProblems({ requiresAtLeast: {} }, {})).toEqual([])
  })

  it("reads the floor off the row", () => {
    const spec = viewSpec(
      view({ kind: KIND_REF, requiresAtLeast: { "ada.example.com/tasks": 2 } }),
      [task]
    )
    expect(floorProblems(spec, { "ada.example.com/tasks": 1 })).toHaveLength(1)
    expect(floorProblems(spec, { "ada.example.com/tasks": 2 })).toEqual([])
  })
})
