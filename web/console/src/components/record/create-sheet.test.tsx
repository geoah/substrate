// @vitest-environment jsdom
/** The new-record sheet, held to every kind this repository ships: each one
 * opens, every row it folds away opens too, and every row's editor opens,
 * without a throw (the sweep, in create-sheet.kinds-1.test.tsx to
 * create-sheet.kinds-4.test.tsx). A new record asks first for what it must
 * have, then for the records it points at and the times it is about, and
 * folds the rest. */

import { cleanup, fireEvent, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { setIn, templateYAML } from "@/lib/record-yaml"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: React.ReactNode }) => <a>{children}</a>,
  useNavigate: () => vi.fn(),
}))

import {
  KINDS,
  SWEEP_FILES,
  kindOf,
  openFold,
  renderSheet,
  shownRows,
} from "./create-sheet.kinds"
import { TABLE_UNITS } from "@/lib/unit-reader"

const TASK = "samples.substrate.reamde.dev/tasks/task"
const PERSON = "samples.substrate.reamde.dev/people/person"

// A sweep file gone missing is a quarter of the kinds never drawn, which no
// failing test would say.
const sweepFiles = Object.keys(
  import.meta.glob("./create-sheet.kinds-*.test.tsx")
)

afterEach(cleanup)

describe("the create sheet", () => {
  it("reads every shipped kind", () => {
    expect(KINDS.length).toBeGreaterThan(100)
    expect(KINDS.map((k) => k.identity)).toContain(TASK)
    expect(sweepFiles.sort()).toEqual(
      Array.from(
        { length: SWEEP_FILES },
        (_, i) => `./create-sheet.kinds-${i + 1}.test.tsx`
      )
    )
  })

  it("asks a task for its Assignee, Project and Due at, and folds the rest", () => {
    renderSheet(kindOf(TASK), KINDS, false)
    expect(shownRows()).toEqual(["assignee", "project", "dueAt"])
    // The series' machinery and the time a move stamps fold last.
    expect(
      screen.getByRole("button", { name: /\d+ more:/ }).textContent
    ).toMatch(/^11 more: Status, Priority, URL, Recurrence of, Source,/)
    expect(screen.queryAllByRole("alert")).toEqual([])
  })

  it("starts a task where its machine starts it, and says a move comes later", () => {
    renderSheet(kindOf(TASK), KINDS, false)
    openFold()
    const status = document.querySelector("[data-property=status]")
    expect(status?.textContent).toContain("Open")
    expect(status?.textContent).toContain(
      "Set by moving it after the task exists"
    )
    expect(screen.queryByRole("button", { name: /^Status .*edit$/ })).toBeNull()
  })

  // The task's `source` points at any kind: until one is chosen its picker
  // names no kind, and it must not ask for the plural of nothing.
  it("opens a task's any-kind Source on a collection to pick first", () => {
    renderSheet(kindOf(TASK))
    openFold()
    fireEvent.click(screen.getByRole("button", { name: /^Source .*edit$/ }))
    expect(screen.getByText("Pick a collection")).toBeTruthy()
    const source = document.getElementById("sheet-source")
    if (!source) throw new Error("the task's Source picker is not drawn")
    expect(source.textContent).toContain("Pick a collection first")
    fireEvent.click(source)
    expect(
      screen.getByPlaceholderText("Search records, or type an id")
    ).toBeTruthy()
    expect(
      screen.getByText(/Pick a collection first, and its records/)
    ).toBeTruthy()
  })

  it("names nothing wrong on a new person before anything is typed", () => {
    renderSheet(kindOf(PERSON), KINDS, false)
    openFold()
    expect(shownRows()).toContain("relationship")
    expect(screen.queryAllByRole("alert")).toEqual([])
    // An enum reads in its display words, never its stored values.
    fireEvent.click(
      screen.getByRole("button", { name: /^Relationship .*edit$/ })
    )
    expect(screen.getByText("Public figure")).toBeTruthy()
  })

  // The draft is YAML the user types, so the title property can hold a
  // mapping; the heading field is text and must not print "[object Object]".
  it("leaves the title field empty while the draft's title is not text", () => {
    const task = kindOf(TASK)
    const text = setIn(
      templateYAML(TABLE_UNITS, task),
      ["data", "properties", "name"],
      {
        first: "a",
      }
    )
    renderSheet(task, KINDS, false, text)
    expect(
      screen.getByRole<HTMLInputElement>("textbox", { name: "Name" }).value
    ).toBe("")
  })
})
