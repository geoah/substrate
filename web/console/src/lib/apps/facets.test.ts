/** Facets on the wire and as chips: the selection ANDs into the request
 * (`eq` for one value, `in` for several, `contains` on a repeated property)
 * INSIDE what the declared filter admits and without touching it, a value
 * the view does not admit is dropped and said, and the chips come from the
 * declaration for a closed set and from the remembered referents for a
 * reference. */

import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  applyFacets,
  facetGroups,
  facetParam,
  facetProblems,
  referentChips,
  withFacets,
} from "./facets"
import type { ViewSpec } from "./spec"

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
      priority: {
        type: "enum",
        values: [
          { value: "none", label: "None" },
          { value: "low", label: "Low" },
          { value: "high", label: "High" },
        ],
        default: "none",
      },
      status: {
        type: "state",
        states: ["proposed", "open", "done"],
        initial: "open",
      },
      project: { type: "reference", kind: "project" },
      tags: { type: "reference", kind: "tag", repeated: true },
      colors: {
        type: "enum",
        repeated: true,
        values: [{ value: "red" }, { value: "blue" }],
      },
    },
  },
}

function spec(over: Partial<ViewSpec>): ViewSpec {
  return {
    id: "v",
    name: "V",
    layout: "list",
    kind: task.identity,
    requiresAtLeast: {},
    filter: {},
    orderBy: [],
    show: [],
    facets: [],
    window: {},
    first: 50,
    related: [],
    attach: [],
    replaces: false,
    actions: [],
    permissions: { reads: { kinds: [] }, writes: [], call: [], agents: [] },
    problems: [],
    ...over,
  }
}

function row(id: string, properties: Record<string, unknown>): SubstrateRecord {
  return {
    id,
    kind: task.identity,
    properties,
    labels: {},
    version: 1,
    createdAt: "",
    updatedAt: "",
  }
}

describe("withFacets", () => {
  const declared = { properties: { status: { in: ["open", "proposed"] } } }

  it("leaves the filter alone with nothing picked", () => {
    expect(withFacets(declared, {}, task)).toBe(declared)
    expect(withFacets(declared, undefined, task)).toBe(declared)
    expect(withFacets(declared, { priority: [] }, task)).toBe(declared)
  })

  it("ANDs one value as eq and several as in, and never mutates", () => {
    const one = withFacets(declared, { priority: ["high"] }, task)
    expect(one.properties).toEqual({
      status: { in: ["open", "proposed"] },
      priority: { eq: "high" },
    })
    const two = withFacets(declared, { priority: ["high", "low"] }, task)
    expect(two.properties?.priority).toEqual({ in: ["high", "low"] })
    expect(declared).toEqual({
      properties: { status: { in: ["open", "proposed"] } },
    })
  })

  it("narrows the declared value test and keeps the rest of the cond", () => {
    const out = withFacets(
      { properties: { status: { in: ["open", "proposed"], exists: true } } },
      { status: ["open"] },
      task
    )
    expect(out.properties?.status).toEqual({ exists: true, eq: "open" })
  })

  it("matches a repeated property item-wise with the latest pick", () => {
    const out = withFacets({}, { tags: ["t/a/b/1", "t/a/b/2"] }, task)
    expect(out.properties?.tags).toEqual({ contains: "t/a/b/2" })
  })

  it("intersects with the view's own eq: that value alone survives", () => {
    const one = { properties: { status: { eq: "open" } } }
    expect(withFacets(one, { status: ["open"] }, task).properties).toEqual({
      status: { eq: "open" },
    })
    const out = applyFacets(one, { status: ["done"] }, task)
    expect(out.filter).toEqual(one)
    expect(out.selection).toEqual({})
    expect(out.problems).toEqual([
      {
        path: "facets.status",
        message: expect.stringMatching(/"done" is not among the values/),
        severity: "warning",
      },
    ])
  })

  it("intersects with the view's own in: the picks inside it, as eq or in", () => {
    expect(
      withFacets(declared, { status: ["done", "open"] }, task).properties
        ?.status
    ).toEqual({ eq: "open" })
    expect(
      withFacets(declared, { status: ["proposed", "open"] }, task).properties
        ?.status
    ).toEqual({ in: ["proposed", "open"] })
    const outside = applyFacets(declared, { status: ["done"] }, task)
    expect(outside.filter.properties?.status).toEqual({
      in: ["open", "proposed"],
    })
    expect(outside.problems.map((p) => p.path)).toEqual(["facets.status"])
  })

  it("a shared URL naming a referent the view does not show changes nothing on the wire", () => {
    const mine = {
      properties: {
        project: { eq: { ref: "ada.example.com/tasks/project/home" } },
      },
    }
    const out = applyFacets(
      mine,
      { project: ["ada.example.com/tasks/project/taxes"] },
      task
    )
    expect(out.filter).toEqual(mine)
    expect(facetProblems(mine, out.selection, task)).toEqual([])
    expect(
      facetProblems(
        mine,
        { project: ["ada.example.com/tasks/project/taxes"] },
        task
      )
    ).toHaveLength(1)
    expect(
      withFacets(
        mine,
        { project: ["ada.example.com/tasks/project/home"] },
        task
      ).properties?.project
    ).toEqual({ eq: "ada.example.com/tasks/project/home" })
  })

  it("keeps a repeated reference's contains and adds the pick beside it", () => {
    const held = { properties: { tags: { contains: "t/a/b/1" } } }
    expect(
      withFacets(held, { tags: ["t/a/b/1"] }, task).properties?.tags
    ).toEqual({ contains: "t/a/b/1" })
    expect(
      withFacets(held, { tags: ["t/a/b/2"] }, task).properties?.tags
    ).toEqual({ contains: "t/a/b/1", in: ["t/a/b/2"] })
    expect(
      applyFacets(
        { properties: { tags: { contains: "t/a/b/1", in: ["t/a/b/2"] } } },
        { tags: ["t/a/b/3"] },
        task
      ).problems
    ).toHaveLength(1)
  })

  it("refuses a second containment on a repeated property that is not a reference", () => {
    const held = { properties: { colors: { contains: "red" } } }
    const out = applyFacets(held, { colors: ["blue"] }, task)
    expect(out.filter).toEqual(held)
    expect(out.problems[0]).toMatchObject({
      path: "facets.colors",
      message: expect.stringMatching(/held to "red"/),
    })
    expect(withFacets(held, { colors: ["red"] }, task)).toEqual(held)
  })

  it("leaves a token-valued test to the read that resolves it", () => {
    const tokened = { properties: { project: { eq: "$input.me" } } }
    expect(withFacets(tokened, { project: ["p/x/y/z"] }, task)).toEqual(tokened)
    expect(facetProblems(tokened, { project: ["p/x/y/z"] }, task)).toEqual([])
  })

  it("names the URL key it lives under", () => {
    expect(facetParam("priority")).toBe("f.priority")
  })
})

describe("facetGroups", () => {
  it("offers a closed set in declared order, held to the filter, with labels", () => {
    const groups = facetGroups(
      spec({
        facets: ["priority", "status"],
        filter: { properties: { status: { in: ["open", "proposed"] } } },
      }),
      task,
      new Map(),
      {}
    )
    expect(groups.map((g) => g.label)).toEqual(["Priority", "Status"])
    expect(groups[0].chips).toEqual([
      { value: "none", label: "None" },
      { value: "low", label: "Low" },
      { value: "high", label: "High" },
    ])
    expect(groups[1].chips.map((c) => c.value)).toEqual(["open", "proposed"])
    expect(groups.map((g) => g.single)).toEqual([false, false])
  })

  it("offers the referents the rows named, by title, and keeps a pick the rows lost", () => {
    const rows = [
      row("t1", { project: { ref: "ada.example.com/tasks/project/home" } }),
      row("t2", { project: { ref: "ada.example.com/tasks/project/taxes" } }),
      row("t3", {}),
    ]
    const titles = new Map([["ada.example.com/tasks/project/taxes", "Taxes"]])
    const seen = new Map([["project", referentChips(rows, "project", titles)]])
    const groups = facetGroups(spec({ facets: ["project"] }), task, seen, {
      project: ["ada.example.com/tasks/project/gone"],
    })
    expect(groups).toHaveLength(1)
    expect(groups[0].chips).toEqual([
      { value: "ada.example.com/tasks/project/gone", label: "gone" },
      { value: "ada.example.com/tasks/project/home", label: "home" },
      { value: "ada.example.com/tasks/project/taxes", label: "Taxes" },
    ])
  })

  it("draws no group for a reference nobody named yet, and one value at a time for a repeated one", () => {
    expect(
      facetGroups(spec({ facets: ["project"] }), task, new Map(), {})
    ).toEqual([])
    const groups = facetGroups(
      spec({ facets: ["tags"] }),
      task,
      new Map([["tags", new Map([["t/a/b/1", "1"]])]]),
      {}
    )
    expect(groups[0].single).toBe(true)
  })
})
