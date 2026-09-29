import { describe, expect, it } from "vitest"

import {
  isAllView,
  matchingView,
  newViewId,
  viewFromState,
  viewMatches,
  viewNameProblem,
  viewsFrom,
  viewsOf,
  type SavedView,
  type ViewState,
} from "./saved-views"

const DEFAULT_SORT = "updatedAt:desc"
const TASK = "example.com/tasks/task"

const state = (over: Partial<ViewState> = {}): ViewState => ({
  filter: [],
  sort: DEFAULT_SORT,
  nest: true,
  columns: ["title", "prop:status", "prop:priority", "updatedAt"],
  hidden: [],
  ...over,
})

describe("saved views", () => {
  it("reads only whole views, first id wins", () => {
    expect(
      viewsFrom([
        { id: "a", collection: TASK, name: "Open", filter: ["x~eq~1"] },
        { id: "a", collection: TASK, name: "Again" },
        { id: "b", collection: TASK },
        "junk",
        { id: "c", collection: TASK, name: "Flat", nest: false, group: "" },
      ])
    ).toEqual([
      { id: "a", collection: TASK, name: "Open", filter: ["x~eq~1"] },
      { id: "c", collection: TASK, name: "Flat", nest: false },
    ])
    expect(viewsFrom(undefined)).toEqual([])
  })

  it("keeps each collection's own", () => {
    const views: SavedView[] = [
      { id: "a", collection: TASK, name: "Open" },
      { id: "b", collection: "example.com/people/person", name: "Work" },
    ]
    expect(viewsOf(views, TASK).map((v) => v.id)).toEqual(["a"])
  })

  it("saves the shape and leaves the defaults out", () => {
    const view = viewFromState(
      { id: "v", collection: TASK, name: "  Urgent " },
      state({
        filter: ["priority~eq~urgent"],
        group: "project",
        hidden: ["prop:priority"],
      }),
      DEFAULT_SORT
    )
    expect(view).toEqual({
      id: "v",
      collection: TASK,
      name: "Urgent",
      filter: ["priority~eq~urgent"],
      columns: ["title", "prop:status", "prop:priority", "updatedAt"],
      hidden: ["prop:priority"],
      group: "project",
    })
    expect(
      viewMatches(
        view,
        state({
          filter: ["priority~eq~urgent"],
          group: "project",
          hidden: ["prop:priority"],
        }),
        DEFAULT_SORT
      )
    ).toBe(true)
  })

  it("replaces a view with what the page shows, forgetting what it dropped", () => {
    const old: SavedView = {
      id: "v",
      collection: TASK,
      name: "Open",
      filter: ["status~eq~open"],
      sort: "dueAt:asc",
      nest: false,
      group: "project",
      columns: ["title", "updatedAt"],
      hidden: ["prop:priority"],
    }
    const replaced = viewFromState(old, state(), DEFAULT_SORT)
    expect(replaced).toEqual({
      id: "v",
      collection: TASK,
      name: "Open",
      columns: ["title", "prop:status", "prop:priority", "updatedAt"],
      hidden: [],
    })
    expect(viewMatches(replaced, state(), DEFAULT_SORT)).toBe(true)
  })

  it("matches what the page shows, and lets go when it changes", () => {
    const view: SavedView = {
      id: "v",
      collection: TASK,
      name: "Open by project",
      filter: ["status~eq~open", "priority~eq~high"],
      sort: "dueAt:asc",
      group: "project",
    }
    const shown = state({
      filter: ["priority~eq~high", "status~eq~open"],
      sort: "dueAt:asc",
      group: "project",
    })
    expect(viewMatches(view, shown, DEFAULT_SORT)).toBe(true)
    expect(
      viewMatches(view, { ...shown, sort: DEFAULT_SORT }, DEFAULT_SORT)
    ).toBe(false)
    expect(
      viewMatches(view, { ...shown, group: undefined }, DEFAULT_SORT)
    ).toBe(false)
    expect(viewMatches(view, { ...shown, nest: false }, DEFAULT_SORT)).toBe(
      false
    )
    expect(
      viewMatches(view, { ...shown, filter: ["status~eq~open"] }, DEFAULT_SORT)
    ).toBe(false)
  })

  it("compares columns only where the view stored them, and only the ones both know", () => {
    const view: SavedView = {
      id: "v",
      collection: TASK,
      name: "Narrow",
      columns: ["title", "prop:priority", "prop:status", "prop:gone"],
      hidden: ["prop:status", "prop:gone"],
    }
    const shown = state({
      columns: ["title", "prop:priority", "prop:status", "updatedAt"],
      hidden: ["prop:status"],
    })
    expect(viewMatches(view, shown, DEFAULT_SORT)).toBe(true)
    expect(viewMatches(view, { ...shown, hidden: [] }, DEFAULT_SORT)).toBe(
      false
    )
    expect(
      viewMatches(view, { ...shown, columns: state().columns }, DEFAULT_SORT)
    ).toBe(false)
    const bare: SavedView = { id: "b", collection: TASK, name: "Bare" }
    expect(viewMatches(bare, state({ hidden: ["x"] }), DEFAULT_SORT)).toBe(true)
  })

  it("prefers the picked view among views of the same shape", () => {
    const a: SavedView = { id: "a", collection: TASK, name: "A", group: "x" }
    const b: SavedView = { id: "b", collection: TASK, name: "B", group: "x" }
    const now = state({ group: "x" })
    expect(matchingView([a, b], now, DEFAULT_SORT)?.id).toBe("a")
    expect(matchingView([a, b], now, DEFAULT_SORT, "b")?.id).toBe("b")
    expect(matchingView([a, b], state(), DEFAULT_SORT, "b")).toBeUndefined()
  })

  it("knows the collection as it opens", () => {
    expect(isAllView(state({ hidden: ["prop:status"] }), DEFAULT_SORT)).toBe(
      true
    )
    expect(isAllView(state({ group: "project" }), DEFAULT_SORT)).toBe(false)
    expect(isAllView(state({ nest: false }), DEFAULT_SORT)).toBe(false)
  })

  it("refuses an empty, a reserved or a taken name", () => {
    const views: SavedView[] = [{ id: "a", collection: TASK, name: "Open" }]
    expect(viewNameProblem(" ", views)).toBeDefined()
    expect(viewNameProblem("all", views)).toBeDefined()
    expect(viewNameProblem("open", views)).toBeDefined()
    expect(viewNameProblem("open", views, "a")).toBeUndefined()
    expect(viewNameProblem("Mine", views)).toBeUndefined()
  })

  it("mints an id nobody has", () => {
    const id = newViewId([{ id: "a", collection: TASK, name: "x" }])
    expect(id).toMatch(/^[a-z0-9]{8}$/)
  })
})
