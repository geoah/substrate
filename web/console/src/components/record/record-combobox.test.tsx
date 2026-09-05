/** The record dropdown. It replaced a datalist, which hung off a text box and
 * showed nothing but the value: what a person is owed here is a list they can
 * OPEN and read, so the contract under test is that clicking shows the records
 * with what distinguishes them, that choosing one inserts it, and that a value
 * the list does not hold is still reachable. */

import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { RecordOption } from "@/lib/identities"
import { RecordCombobox } from "./record-combobox"

/** The function collection, as the picker offers it: one row per record, by
 * the id the write names it with. */
const HOST_FUNCTIONS: RecordOption[] = [
  {
    value: "substrate.reamde.dev/core/graphql",
    title: "",
    description: "Run a read-only GraphQL query against the repository.",
  },
  {
    value: "substrate.reamde.dev/core/propose",
    title: "",
    description:
      "Propose a reviewed change to the graph instead of writing it.",
  },
  {
    value: "crew.test.dev/summarize",
    title: "Summarize",
    description: "shorten a note",
  },
]

function open(
  over: Partial<React.ComponentProps<typeof RecordCombobox>> = {}
): { onSelect: ReturnType<typeof vi.fn> } {
  const onSelect = vi.fn()
  render(
    <RecordCombobox
      id="pick"
      value=""
      onSelect={onSelect}
      options={HOST_FUNCTIONS}
      loading={false}
      capped={false}
      ariaLabel="Tool"
      {...over}
    />
  )
  fireEvent.click(screen.getByLabelText(over.ariaLabel ?? "Tool"))
  return { onSelect }
}

function search(): HTMLInputElement {
  return screen.getByPlaceholderText(
    "Search records, or type an id"
  ) as HTMLInputElement
}

/** The record rows the list is offering right now, best match first. cmdk
 * keeps every item mounted and hides what does not match, so "offered" is what
 * is left visible, and it SORTS by score, so the head is the best answer. */
function showing(): string[] {
  return [...document.querySelectorAll("[cmdk-item]")]
    .filter((el) => !el.hasAttribute("hidden"))
    .map((el) => el.getAttribute("data-value") ?? "")
    .filter((v) => !v.startsWith("use-typed-"))
}

afterEach(cleanup)

describe("the record dropdown", () => {
  it("stays shut until it is opened, then shows the records", () => {
    const onSelect = vi.fn()
    render(
      <RecordCombobox
        id="pick"
        value=""
        onSelect={onSelect}
        options={HOST_FUNCTIONS}
        loading={false}
        capped={false}
        ariaLabel="Tool"
      />
    )
    expect(screen.queryByPlaceholderText(/Search records/)).toBeNull()
    fireEvent.click(screen.getByLabelText("Tool"))
    expect(search()).toBeTruthy()
  })

  it("gives every record its id and its one-liner, so a host function is a card", () => {
    open()
    expect(screen.getByText("substrate.reamde.dev/core/propose")).toBeTruthy()
    expect(
      screen.getByText(/Propose a reviewed change to the graph/)
    ).toBeTruthy()
    // A record with a title leads with it and keeps the id beside it.
    expect(screen.getByText("Summarize")).toBeTruthy()
    expect(screen.getByText("crew.test.dev/summarize")).toBeTruthy()
    expect(screen.getByText("shorten a note")).toBeTruthy()
  })

  it("inserts the record chosen", async () => {
    const { onSelect } = open()
    fireEvent.click(screen.getByText("substrate.reamde.dev/core/propose"))
    // The RECORD is what a selection inserts; the pin supplies the kind the
    // write joins onto it.
    expect(onSelect).toHaveBeenCalledWith("substrate.reamde.dev/core/propose")
    // Choosing closes it: the list has done its job. The close settles after
    // base-ui's animation check, so the unmount is awaited, not asserted flat.
    await waitFor(() =>
      expect(screen.queryByPlaceholderText(/Search records/)).toBeNull()
    )
  })

  it("narrows on what a reader can SEE, descriptions included", () => {
    open()
    // Nothing in this record's id or title says "shorten": only its
    // description does, and it is the best answer to it.
    fireEvent.change(search(), { target: { value: "shorten" } })
    expect(showing()[0]).toContain("crew.test.dev/summarize")
  })

  it("offers whatever is typed, because a record can be minted at any time", () => {
    const { onSelect } = open()
    fireEvent.change(search(), { target: { value: "crew.test.dev/not-yet" } })
    fireEvent.click(screen.getByText(/^Use/))
    expect(onSelect).toHaveBeenCalledWith("crew.test.dev/not-yet")
  })

  it("does not offer to type what the list already holds", () => {
    open()
    fireEvent.change(search(), {
      target: { value: "substrate.reamde.dev/core/propose" },
    })
    expect(screen.queryByText(/^Use/)).toBeNull()
  })

  it("selects with the keyboard and closes on escape", async () => {
    const { onSelect } = open()
    fireEvent.keyDown(search(), { key: "ArrowDown" })
    fireEvent.keyDown(search(), { key: "Enter" })
    expect(onSelect).toHaveBeenCalledTimes(1)
    cleanup()

    open()
    fireEvent.keyDown(search(), { key: "Escape" })
    await waitFor(() =>
      expect(screen.queryByPlaceholderText(/Search records/)).toBeNull()
    )
  })

  it("says it is reading rather than showing an empty list", () => {
    open({ options: [], loading: true })
    expect(screen.getByText(/Reading the collection/)).toBeTruthy()
  })

  it("says what went wrong, and leaves typing open anyway", () => {
    const { onSelect } = open({ options: [], error: "network error" })
    expect(screen.getByText("network error")).toBeTruthy()
    fireEvent.change(search(), { target: { value: "typed-anyway" } })
    fireEvent.click(screen.getByText(/^Use/))
    expect(onSelect).toHaveBeenCalledWith("typed-anyway")
  })

  it("says the collection is empty rather than that nothing matched", () => {
    open({ options: [] })
    expect(screen.getByText(/no records yet/)).toBeTruthy()
  })

  it("says so when the page did not hold the whole collection", () => {
    open({ capped: true })
    expect(screen.getByText(/Showing the first 3/)).toBeTruthy()
  })

  it("reads as an ADD where a repeated picker grows its list", () => {
    open({ adding: true, addLabel: "Add", ariaLabel: "Add Agents" })
    expect(screen.getByLabelText("Add Agents")).toBeTruthy()
  })
})
