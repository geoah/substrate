// @vitest-environment jsdom
/** `List`: rows from a page or an array, sectioned by a datetime's buckets,
 * a state's declared order or a function's keys with the A–Z rail, and the
 * page's own footer: Load more while a cursor remains, a note when the page
 * was cut, the empty and loading states. */

import {
  cleanup,
  fireEvent,
  render,
  screen,
  within,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

const sdk = vi.hoisted(() => ({
  host: {
    navigate: vi.fn(() => Promise.resolve()),
    openLink: vi.fn(() => Promise.resolve()),
    toast: vi.fn(),
  },
  records: {},
  useKind: vi.fn(),
}))
vi.mock("./sdk", () => sdk)

import { List } from "./list"
import { Row } from "./row"

const TASK = "ada.example.com/tasks/task"
const DAY = 86_400_000
const at = (days: number) => new Date(Date.now() + days * DAY).toISOString()

const task = (
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord => ({
  id,
  kind: TASK,
  properties: { name: id, ...properties },
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

const page = (
  records: SubstrateRecord[],
  over: Record<string, unknown> = {}
) => ({
  records,
  loading: false,
  ...over,
})

const headers = (container: HTMLElement) =>
  [...container.querySelectorAll(".kit-list-header")].map(
    (h) => h.firstElementChild?.textContent
  )

const rowTitles = (container: HTMLElement) =>
  [...container.querySelectorAll(".kit-row-title")].map((r) => r.textContent)

describe("List", () => {
  beforeEach(() => {
    vi.clearAllMocks()
    sdk.useKind.mockReturnValue(undefined)
  })
  afterEach(cleanup)

  it("buckets a datetime property from Overdue to Undated, present ones only", () => {
    const { container } = render(
      <List
        page={page([
          task("later", { dueAt: at(20) }),
          task("none", {}),
          task("tmrw", { dueAt: at(1) }),
          task("late", { dueAt: at(-1) }),
        ])}
        groupBy="dueAt"
      >
        {(t) => <Row key={t.id} title={t.id} />}
      </List>
    )
    expect(headers(container)).toEqual([
      "Overdue",
      "Tomorrow",
      "Later",
      "Undated",
    ])
    expect(rowTitles(container)).toEqual(["late", "tmrw", "later", "none"])
    expect(container.querySelector(".kit-index")).toBeNull()
  })

  it("sections a state by the declaration the SDK holds", () => {
    sdk.useKind.mockReturnValue({
      identity: TASK,
      properties: [
        {
          name: "status",
          kind: "state",
          states: ["open", "done"],
          initial: "open",
        },
      ],
    })
    const { container } = render(
      <List
        page={page([
          task("b", { status: "done" }),
          task("a", { status: "open" }),
          task("c", {}),
        ])}
        groupBy="status"
      >
        {(t) => <Row key={t.id} title={t.id} />}
      </List>
    )
    expect(sdk.useKind).toHaveBeenCalledWith(TASK)
    expect(headers(container)).toEqual(["open", "done", "None"])
    expect(rowTitles(container)).toEqual(["a", "b", "c"])
  })

  it("groups by a function with an index rail that jumps to a section", () => {
    const scroll = vi.fn()
    Element.prototype.scrollIntoView = scroll
    const people = [task("Zoe", {}), task("Ada", {}), task("Alan", {})]
    const { container } = render(
      <List records={people} groupBy={(p) => p.id[0].toUpperCase()} index>
        {(p) => <Row key={p.id} title={p.id} />}
      </List>
    )
    expect(headers(container)).toEqual(["A", "Z"])
    expect(rowTitles(container)).toEqual(["Ada", "Alan", "Zoe"])
    const rail = screen.getByRole("navigation", { name: "Index" })
    const letters = [...rail.querySelectorAll(".kit-index-item")]
    expect(letters).toHaveLength(27)
    expect(letters.filter((l) => l.className.includes("absent"))).toHaveLength(
      25
    )
    const z = within(rail).getByText("Z")
    document.elementFromPoint = () => z
    fireEvent.pointerDown(rail, { clientX: 0, clientY: 0 })
    expect(scroll).toHaveBeenCalledTimes(1)
  })

  it("offers Load more while a cursor remains and notes a cut page", () => {
    const loadMore = vi.fn()
    const { container, rerender } = render(
      <List page={page([task("a", {})], { cursor: "c1", loadMore })}>
        {(t) => <Row key={t.id} title={t.id} />}
      </List>
    )
    fireEvent.click(screen.getByRole("button", { name: "Load more" }))
    expect(loadMore).toHaveBeenCalledTimes(1)
    rerender(
      <List page={page([task("a", {})], { incomplete: true })}>
        {(t) => <Row key={t.id} title={t.id} />}
      </List>
    )
    expect(screen.queryByRole("button", { name: "Load more" })).toBeNull()
    expect(container.querySelector(".kit-list-foot")?.textContent).toBe(
      "Showing the first 1; more were not loaded."
    )
  })

  it("shows the empty text, a spinner while loading, and the error", () => {
    const { rerender } = render(
      <List page={page([])} empty="Nothing open">
        {(t) => <Row key={t.id} title={t.id} />}
      </List>
    )
    expect(screen.getByRole("status").textContent).toBe("Nothing open")
    rerender(
      <List page={page([], { loading: true })}>
        {(t) => <Row key={t.id} title={t.id} />}
      </List>
    )
    expect(screen.getByRole("status", { name: "Loading" })).toBeTruthy()
    rerender(
      <List page={page([], { error: new Error("forbidden") })}>
        {(t) => <Row key={t.id} title={t.id} />}
      </List>
    )
    expect(screen.getByRole("status").textContent).toContain("forbidden")
    rerender(
      <List records={[] as SubstrateRecord[]} empty={<p>custom</p>}>
        {(t) => <Row key={t.id} title={t.id} />}
      </List>
    )
    expect(screen.getByText("custom")).toBeTruthy()
  })
})
