/** Facets on the wire and as chips: the selection ANDs into the request
 * (`eq` for one value, `in` for several, `contains` on a repeated property)
 * without touching the declared filter, and the chips come from the
 * declaration for a closed set and from the remembered referents for a
 * reference. */

import { describe, expect, it } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import { facetGroups, facetParam, referentChips, withFacets } from "./facets"
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
