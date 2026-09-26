// @vitest-environment jsdom
/** The one list a reader picks a declared value from: words first, the
 * stored value beside them only with Technical details on, the keyboard
 * alone enough to pick, and several at once where the caller allows. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest"

import { ChoiceList, type ChoiceOption } from "./choice-list"
import {
  ConsolePreferencesContext,
  type ConsolePreferencesContextValue,
} from "@/hooks/use-console-preferences"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"

beforeAll(() => {
  // cmdk scrolls the highlighted row into view; jsdom has no layout.
  Element.prototype.scrollIntoView ??= () => {}
})
afterEach(cleanup)

const OPTIONS: ChoiceOption[] = [
  { value: "proposed", label: "Suggested" },
  { value: "open", label: "Open" },
  { value: "done", label: "Done" },
]

function preferences(technical: boolean): ConsolePreferencesContextValue {
  return {
    preferences: {
      collapsed: [],
      favorites: [],
      sidebarOpen: true,
      ...DEFAULT_SETTINGS,
      technicalDetails: technical,
    },
    busy: false,
    change: () => {},
    set: () => {},
  }
}

function mount(
  props: Partial<Parameters<typeof ChoiceList>[0]> = {},
  technical = false
) {
  const onChange = vi.fn()
  render(
    <ConsolePreferencesContext.Provider value={preferences(technical)}>
      <ChoiceList
        label="Status"
        options={OPTIONS}
        selected={[]}
        onChange={onChange}
        {...props}
      />
    </ConsolePreferencesContext.Provider>
  )
  return onChange
}

const rows = () => [...document.querySelectorAll("[data-slot=command-item]")]

describe("ChoiceList", () => {
  it("reads in words, the stored value beside them only in technical mode", () => {
    mount()
    expect(rows().map((r) => r.textContent)).toEqual([
      "Suggested",
      "Open",
      "Done",
    ])
    cleanup()
    mount({}, true)
    expect(rows()[0].textContent).toBe("Suggestedproposed")
  })

  it("opens on the chosen row, picks with the keyboard and marks the choice", () => {
    const onChange = mount({ selected: ["open"] })
    expect(rows()[1].textContent).toContain("(chosen)")
    expect(rows()[1].querySelector("[data-slot=choice-check]")).toBeTruthy()
    const list = screen.getByRole("listbox", { name: "Status" })
    const root = list.closest("[cmdk-root]")!
    fireEvent.keyDown(root, { key: "ArrowDown" })
    fireEvent.keyDown(root, { key: "Enter" })
    expect(onChange).toHaveBeenCalledExactlyOnceWith(["done"])
  })

  it("toggles membership in declaration order when several may be chosen", () => {
    const onChange = mount({ multiple: true, selected: ["done"] })
    fireEvent.click(screen.getByText("Suggested"))
    expect(onChange).toHaveBeenLastCalledWith(["proposed", "done"])
    fireEvent.click(screen.getByText("Done"))
    expect(onChange).toHaveBeenLastCalledWith([])
    expect(
      screen.getByRole("listbox").getAttribute("aria-multiselectable")
    ).toBe("true")
  })

  it("offers a filter box once the list is long, searching the words", () => {
    const many = Array.from({ length: 10 }, (_, i) => ({
      value: `v${i}`,
      label: `Choice ${i}`,
    }))
    mount({ options: many })
    const box = screen.getByPlaceholderText("Filter…")
    fireEvent.change(box, { target: { value: "choice 7" } })
    expect(rows().map((r) => r.textContent)).toEqual(["Choice 7"])
  })
})
