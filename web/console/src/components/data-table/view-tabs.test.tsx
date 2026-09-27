// @vitest-environment jsdom
/** The saved-view strip: All and each view as tabs, the chosen one pressed, a
 * changed one marked with its save and discard, and every write asking first. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { SavedView } from "@/lib/saved-views"
import { ViewTabs } from "./view-tabs"

afterEach(cleanup)

const TASK = "example.com/tasks/task"
const OPEN: SavedView = { id: "v1", collection: TASK, name: "Open" }
const MINE: SavedView = { id: "v2", collection: TASK, name: "Mine" }

function draw(over: Partial<Parameters<typeof ViewTabs>[0]> = {}) {
  const props = {
    views: [OPEN, MINE],
    active: "all" as string | null,
    onPick: vi.fn(),
    onSave: vi.fn(),
    onRename: vi.fn(),
    onReplace: vi.fn(),
    onDelete: vi.fn(),
    ...over,
  }
  render(<ViewTabs {...props} />)
  return props
}

const pressed = () =>
  screen.getAllByRole("button", { pressed: true }).map((b) => b.textContent)

describe("ViewTabs", () => {
  it("presses the chosen tab and picks another", () => {
    const props = draw()
    expect(pressed()).toEqual(["All"])
    fireEvent.click(screen.getByRole("button", { name: "Mine" }))
    expect(props.onPick).toHaveBeenCalledWith(MINE)
    fireEvent.click(screen.getByRole("button", { name: "All" }))
    expect(props.onPick).toHaveBeenLastCalledWith(null)
  })

  it("chooses nothing when the page matches no tab", () => {
    draw({ active: null })
    expect(screen.queryAllByRole("button", { pressed: true })).toEqual([])
  })

  it("marks the view picked last once the page has moved away from it", () => {
    draw({ active: null, edited: "v1" })
    expect(pressed()).toEqual(["Open, changed"])
    expect(
      screen.getByRole("button", { name: "More for the “Open” view" })
    ).toBeTruthy()
    expect(
      screen.queryByRole("button", { name: "More for the “Mine” view" })
    ).toBeNull()
  })

  it("asks for a name before saving, and refuses a taken one", () => {
    const props = draw()
    fireEvent.click(screen.getByRole("button", { name: "Save view" }))
    const name = screen.getByLabelText("Name")
    fireEvent.change(name, { target: { value: "open" } })
    expect(
      screen.getByText("There is already a view called “Open”.")
    ).toBeTruthy()
    fireEvent.change(name, { target: { value: "  Urgent " } })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    expect(props.onSave).toHaveBeenCalledWith("Urgent")
  })
})
