// @vitest-environment jsdom
/** `Timeline`: rows from every kind on one page, sectioned by the local day
 * of `at` else `dueAt`, empty days left out, today marked, a range as a
 * span, a kind badge per row, a tap, and the note when the page was cut. */

import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { SubstrateRecord } from "@/lib/api/types"

const sdk = vi.hoisted(() => ({
  host: { navigate: vi.fn(() => Promise.resolve()), toast: vi.fn() },
  records: {},
  useKind: () => undefined,
}))
vi.mock("./sdk", () => sdk)

import { Timeline } from "./timeline"

const TASK = "ada.example.com/tasks/task"
const EVENT = "ada.example.com/calendar/calendarevent"

function at(daysFromNow: number, hour: number, minute = 0): string {
  const d = new Date()
  d.setDate(d.getDate() + daysFromNow)
  d.setHours(hour, minute, 0, 0)
  return d.toISOString()
}

const record = (
  id: string,
  kind: string,
  properties: Record<string, unknown>
): SubstrateRecord => ({
  id,
  kind,
  properties,
  labels: {},
  version: 1,
  createdAt: "",
  updatedAt: "",
})

const rows = [
  record("notes", TASK, { name: "Write the retro notes", dueAt: at(1, 10) }),
  record("landlord", TASK, { name: "Call the landlord", dueAt: at(0, 11) }),
  record("sync", EVENT, {
    title: "Platform sync",
    at: at(0, 9),
    endsAt: at(0, 9, 30),
  }),
  record("gone", TASK, { name: "Last week", dueAt: at(-3, 9) }),
  record("undated", TASK, { name: "No date" }),
]

const sections = () =>
  [...document.querySelectorAll("section.kit-day")].map((s) => ({
    label: s.getAttribute("aria-label"),
    today: s.getAttribute("aria-current") === "date",
    titles: [...s.querySelectorAll(".kit-timeline-title")].map(
      (t) => t.textContent
    ),
  }))

describe("Timeline", () => {
  beforeEach(() => vi.clearAllMocks())
  afterEach(cleanup)

  it("folds records into local days, today marked, ranges as spans, a kind badge each", () => {
    render(<Timeline page={{ records: rows, loading: false }} />)
    const days = sections()
    expect(days).toHaveLength(3)
    expect(days[0].today).toBe(false)
    expect(days[1]).toEqual({
      label: "Today",
      today: true,
      titles: ["Platform sync", "Call the landlord"],
    })
    expect(days[2]).toEqual({
      label: "Tomorrow",
      today: false,
      titles: ["Write the retro notes"],
    })
    expect(screen.queryByText("No date")).toBeNull()
    const times = [...document.querySelectorAll(".kit-timeline-time")].map(
      (t) => t.textContent
    )
    expect(times).toContain("09:00–09:30")
    expect(times).toContain("11:00")
    const badges = [...document.querySelectorAll(".kit-badge")].map(
      (b) => b.textContent
    )
    expect(badges).toEqual(["task", "calendarevent", "task", "task"])
    expect(document.querySelector(".kit-now")).toBeNull()
  })

  it("draws a line where today falls when today has nothing", () => {
    const { container } = render(
      <Timeline
        page={{ records: [rows[3], rows[0]], loading: false }}
        kindLabel={(r) => (r.kind === TASK ? "Task" : "Event")}
      />
    )
    const children = [...container.querySelector(".kit-timeline")!.children]
    expect(children.map((c) => c.className)).toEqual([
      "kit-day",
      "kit-now",
      "kit-day",
    ])
    expect(screen.getAllByText("Task")).toHaveLength(2)
  })

  it("reports a tap, or opens the record through the host without one", () => {
    const onTap = vi.fn()
    const { unmount } = render(
      <Timeline page={{ records: [rows[2]], loading: false }} onTap={onTap} />
    )
    fireEvent.click(screen.getByRole("button", { name: /Platform sync/ }))
    expect(onTap).toHaveBeenCalledWith(rows[2])
    unmount()
    render(<Timeline page={{ records: [rows[2]], loading: false }} />)
    fireEvent.keyDown(screen.getByRole("button", { name: /Platform sync/ }), {
      key: "Enter",
    })
    expect(sdk.host.navigate).toHaveBeenCalledWith({
      record: { kind: EVENT, id: "sync" },
    })
  })

  it("notes a cut page, spins while loading, and says when nothing is scheduled", () => {
    const { rerender } = render(
      <Timeline
        page={{ records: [rows[1]], loading: false, incomplete: true }}
      />
    )
    expect(document.querySelector(".kit-list-foot")?.textContent).toBe(
      "Showing the first 1; more were not loaded."
    )
    rerender(<Timeline page={{ records: [], loading: true }} />)
    expect(screen.getByRole("status", { name: "Loading" })).toBeTruthy()
    rerender(<Timeline page={{ records: [], loading: false }} />)
    expect(screen.getByRole("status").textContent).toBe("Nothing scheduled")
  })
})
