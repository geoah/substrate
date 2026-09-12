// @vitest-environment jsdom
/** The timeline layout against a stubbed substrate: two implementors read
 * on different points (`dueAt` for tasks, `at` for events) merged into one
 * day-grouped order, the log kind left out, a range shown as a time span and
 * a point as its time, the state badge, a failed implementor named in the
 * footer while the others render, a cut page reported, and a row tap. Then
 * the chosen range: the address's `from`/`to` read instead of the window, a
 * chip and a date input rewriting the address and re-requesting on the new
 * bounds, the empty state naming the range, the scroll landing on today only
 * while today is in range, and a card ignoring the address. The stub answers
 * only the rows inside the bounds it was asked for. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import type { ViewContext, ViewSpec } from "@/lib/apps/spec"
import { shortTime } from "@/lib/format"
import TimelineLayout from "./timeline"
import {
  QUICK_RANGES,
  rangeBounds,
  rangeLabel,
  windowRange,
} from "./timeline-range"
import {
  dayKey,
  dayLabel,
  groupByDay,
  isTimelineKind,
  mergeRows,
  timelineRead,
} from "./timeline-window"

const TASK = "ada.example.com/tasks/task"
const EVENT = "ada.example.com/calendar/calendarevent"
const TASKLOG = "ada.example.com/tasks/tasklog"
const TEMPORAL = "substrate.reamde.dev/core/temporal"

function kind(
  identity: string,
  name: string,
  definition: Record<string, unknown>
): KindInfo {
  const [authority, pkg] = identity.split("/")
  return {
    identity,
    name,
    authority,
    package: pkg,
    version: 1,
    source: "installed",
    description: "",
    definition: { names: { singular: name }, ...definition },
  }
}

const task = kind(TASK, "task", {
  displayTemplate: "{name|title}",
  traits: ["temporal(point: dueAt)", "recurring"],
  properties: {
    name: { type: "string" },
    status: {
      type: "state",
      states: ["proposed", "open", "done", "abandoned"],
      initial: "open",
    },
  },
})

const event = kind(EVENT, "calendarevent", {
  displayTemplate: "{summary}",
  traits: ["temporal(range)"],
  properties: { summary: { type: "string" }, status: { type: "string" } },
})

const tasklog = kind(TASKLOG, "tasklog", {
  displayTemplate: "{status}",
  traits: ["temporal(point)", "occurrencelog"],
  properties: {
    status: { type: "state", states: ["done", "skipped"], initial: "done" },
  },
})

function at(daysFromNow: number, hour: number, minute = 0): string {
  const d = new Date()
  d.setDate(d.getDate() + daysFromNow)
  d.setHours(hour, minute, 0, 0)
  return d.toISOString()
}

function record(
  id: string,
  kindRef: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  return {
    id,
    kind: kindRef,
    properties,
    labels: {},
    version: 1,
    createdAt: "2026-09-01T00:00:00Z",
    updatedAt: "2026-09-01T00:00:00Z",
  }
}

const tasks = [
  record("landlord", TASK, {
    title: "Call the landlord",
    dueAt: at(0, 11),
    status: "open",
  }),
  record("notes", TASK, {
    title: "Write the retro notes",
    dueAt: at(1, 10),
    status: "proposed",
  }),
]

const events = [
  record("sync", EVENT, {
    title: "Platform sync",
    at: at(0, 9),
    endsAt: at(0, 9, 30),
  }),
]

const traitSpec: ViewSpec = {
  id: "timeline",
  name: "Timeline",
  layout: "timeline",
  trait: TEMPORAL,
  requiresAtLeast: {},
  filter: {},
  orderBy: [],
  show: [],
  facets: [],
  window: { past: "P7D", future: "P30D" },
  first: 50,
  related: [],
  attach: ["launcher", "home"],
  replaces: false,
  actions: [],
  permissions: { reads: { kinds: [] }, writes: [], call: [], agents: [] },
  problems: [],
}

const calls: { method: string; url: string }[] = []

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), { status })
}

/** The rows whose point falls inside the `gte`/`lt` the request carries, as
 * the engine would answer, so a changed range changes what comes back. */
function inside(url: string, records: SubstrateRecord[], point: string) {
  const raw = new URL(url, "http://x").searchParams.get("filter")
  const cond = raw ? JSON.parse(raw).properties?.[point] : undefined
  if (!cond?.gte || !cond?.lt) return records
  return records.filter((r) => {
    const t = Date.parse(String(r.properties[point]))
    return t >= Date.parse(cond.gte) && t < Date.parse(cond.lt)
  })
}

function stubFetch(over: { eventStatus?: number; taskCursor?: string } = {}) {
  vi.stubGlobal("fetch", (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input)
    calls.push({ method: init?.method ?? "GET", url })
    if (url.includes("/implementors")) {
      return Promise.resolve(json({ items: [task, event, tasklog] }))
    }
    if (url.includes(`/${TASK}?`)) {
      return Promise.resolve(
        json({ records: inside(url, tasks, "dueAt"), cursor: over.taskCursor })
      )
    }
    if (url.includes(`/${EVENT}?`)) {
      if (over.eventStatus) {
        return Promise.resolve(
          json(
            {
              error: {
                code: "unavailable",
                message: "the calendar is asleep",
              },
            },
            over.eventStatus
          )
        )
      }
      return Promise.resolve(json({ records: inside(url, events, "at") }))
    }
    return Promise.resolve(
      json({ error: { message: `no stub for ${url}` } }, 404)
    )
  })
}

const PAGE: ViewContext = { inputs: {}, mode: "page" }

function renderTimeline(
  spec: ViewSpec = traitSpec,
  onOpenRecord = vi.fn(),
  viewKind?: KindInfo,
  over: { search?: string; ctx?: ViewContext } = {}
) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const urls: URLSearchParams[] = []
  const utils = render(
    <NuqsTestingAdapter
      searchParams={over.search ?? ""}
      hasMemory
      onUrlUpdate={(e) => urls.push(e.searchParams)}
    >
      <QueryClientProvider client={client}>
        <TimelineLayout
          spec={spec}
          kind={viewKind}
          kinds={[task, event, tasklog]}
          ctx={over.ctx ?? PAGE}
          onOpenRecord={onOpenRecord}
        />
      </QueryClientProvider>
    </NuqsTestingAdapter>
  )
  return { ...utils, onOpenRecord, urls }
}

function decode(url: string) {
  const params = new URL(url, "http://x").searchParams
  return {
    orderBy: params.get("orderBy"),
    first: params.get("first"),
    filter: JSON.parse(params.get("filter") ?? "{}"),
  }
}

function readOf(identity: string) {
  const call = calls.find((c) => c.url.includes(`/${identity}?`))
  return call ? decode(call.url) : undefined
}

/** The most recent read of one kind: what a changed range asked for. */
function lastReadOf(identity: string) {
  const call = calls.findLast((c) => c.url.includes(`/${identity}?`))
  return call ? decode(call.url) : undefined
}

function lastUrl(urls: URLSearchParams[]) {
  const last = urls[urls.length - 1]
  return { from: last?.get("from") ?? null, to: last?.get("to") ?? null }
}

beforeEach(() => {
  calls.length = 0
  stubFetch()
})

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("timeline window", () => {
  it("keeps the kinds that bind a point and leaves the logs out", () => {
    expect(isTimelineKind(task)).toBe(true)
    expect(isTimelineKind(event)).toBe(true)
    expect(isTimelineKind(tasklog)).toBe(false)
    expect(
      isTimelineKind(
        kind("ada.example.com/x/y", "y", { traits: ["occurrencelog"] })
      )
    ).toBe(false)
    expect(isTimelineKind(kind("ada.example.com/x/z", "z", {}))).toBe(false)
  })

  it("reads each kind on its own point with the window as gte/lt", () => {
    const now = Date.parse("2026-09-12T12:00:00Z")
    const t = timelineRead(traitSpec, { inputs: {}, mode: "page" }, task, now)
    expect(t.point).toBe("dueAt")
    expect(t.range).toBe(false)
    expect(t.params?.orderBy).toBe("dueAt:asc")
    expect(t.params?.first).toBe(50)
    expect(t.params?.filter?.properties?.dueAt).toEqual({
      gte: "2026-09-05T12:00:00.000Z",
      lt: "2026-10-12T12:00:00.000Z",
    })
    const e = timelineRead(traitSpec, { inputs: {}, mode: "page" }, event, now)
    expect(e.point).toBe("at")
    expect(e.range).toBe(true)
    expect(e.params?.filter?.properties?.at?.gte).toBe(
      "2026-09-05T12:00:00.000Z"
    )
  })

  it("defaults the window to a week back and a month ahead", () => {
    const now = Date.parse("2026-09-12T12:00:00Z")
    const t = timelineRead(
      { ...traitSpec, window: {} },
      { inputs: {}, mode: "page" },
      task,
      now
    )
    expect(t.params?.filter?.properties?.dueAt).toEqual({
      gte: "2026-09-05T12:00:00.000Z",
      lt: "2026-10-12T12:00:00.000Z",
    })
  })

  it("takes chosen bounds in place of the window, on the kind's own point", () => {
    const now = Date.parse("2026-09-12T12:00:00Z")
    const bounds = rangeBounds({ from: "2026-09-01", to: "2026-09-30" })
    const t = timelineRead(traitSpec, PAGE, task, now, bounds)
    expect(t.params?.filter?.properties?.dueAt).toEqual(bounds)
    const e = timelineRead(traitSpec, PAGE, event, now, bounds)
    expect(e.params?.filter?.properties?.at).toEqual(bounds)
    expect(e.params?.orderBy).toBe("at:asc")
  })

  it("merges pages by instant and folds them into local days", () => {
    const now = Date.now()
    const ctx = { inputs: {}, mode: "page" as const }
    const rows = mergeRows([
      { read: timelineRead(traitSpec, ctx, task, now), records: tasks },
      { read: timelineRead(traitSpec, ctx, event, now), records: events },
    ])
    expect(rows.map((r) => r.record.id)).toEqual(["sync", "landlord", "notes"])
    expect(rows[0].endsAt).toBeDefined()
    expect(rows[1].endsAt).toBeUndefined()
    const days = groupByDay(rows)
    expect(days.map((d) => d.rows.length)).toEqual([2, 1])
    expect(days[0].key).toBe(dayKey(now))
    expect(dayLabel(days[0].key, now)).toBe("Today")
    expect(dayLabel(days[1].key, now)).toBe("Tomorrow")
  })
})

describe("TimelineLayout", () => {
  it("interleaves two implementors read on different points under one day", async () => {
    renderTimeline()
    const today = await screen.findByRole("region", { name: "Today" })
    const rows = within(today)
      .getAllByRole("button")
      .filter((b) => b.tagName === "DIV")
      .map((b) => b.textContent ?? "")
    expect(rows).toHaveLength(2)
    expect(rows[0]).toContain("Platform sync")
    expect(rows[0]).toContain("calendarevent")
    expect(rows[1]).toContain("Call the landlord")
    expect(rows[1]).toContain("task")
    const tomorrow = screen.getByRole("region", { name: "Tomorrow" })
    expect(within(tomorrow).getByText("Write the retro notes")).toBeTruthy()
    expect(today.getAttribute("aria-current")).toBe("date")
    expect(tomorrow.getAttribute("aria-current")).toBeNull()
  })

  it("asks each implementor for its own point and never asks the log", async () => {
    renderTimeline()
    await screen.findByText("Platform sync")
    const t = readOf(TASK)!
    expect(t.orderBy).toBe("dueAt:asc")
    expect(t.first).toBe("50")
    expect(t.filter.properties.dueAt.gte).toBeDefined()
    expect(t.filter.properties.dueAt.lt).toBeDefined()
    const e = readOf(EVENT)!
    expect(e.orderBy).toBe("at:asc")
    expect(e.filter.properties.at.gte).toBeDefined()
    expect(readOf(TASKLOG)).toBeUndefined()
  })

  it("shows a range as a span, a point as its time, and the state badge", async () => {
    renderTimeline()
    await screen.findByText("Platform sync")
    const start = shortTime(events[0].properties.at as string)
    const end = shortTime(events[0].properties.endsAt as string)
    expect(screen.getByText(`${start}–${end}`)).toBeTruthy()
    expect(
      screen.getByText(shortTime(tasks[0].properties.dueAt as string))
    ).toBeTruthy()
    expect(screen.getByText("open")).toBeTruthy()
    expect(screen.getByText("proposed")).toBeTruthy()
  })

  it("names an implementor it could not read and still renders the rest", async () => {
    stubFetch({ eventStatus: 503 })
    renderTimeline()
    await screen.findByText("Call the landlord")
    expect(
      await screen.findByText(
        /Could not read calendarevent: the calendar is asleep/
      )
    ).toBeTruthy()
    expect(screen.queryByText("Platform sync")).toBeNull()
    expect(screen.getByRole("button", { name: "Retry" })).toBeTruthy()
  })

  it("says when an implementor's page was cut", async () => {
    stubFetch({ taskCursor: "more" })
    renderTimeline()
    expect(await screen.findByText("showing the first 2 of task")).toBeTruthy()
  })

  it("opens on today once every implementor has settled", async () => {
    const spy = vi.spyOn(Element.prototype, "scrollIntoView")
    renderTimeline()
    await screen.findByText("Platform sync")
    await screen.findByText("Call the landlord")
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1))
    const target = spy.mock.instances[0] as unknown as Element
    expect(target.getAttribute("aria-current")).toBe("date")
    spy.mockRestore()
  })

  it("reports a row tap through onOpenRecord", async () => {
    const { onOpenRecord } = renderTimeline()
    fireEvent.click(await screen.findByText("Platform sync"))
    expect(onOpenRecord).toHaveBeenCalledTimes(1)
    expect(onOpenRecord.mock.calls[0][0].id).toBe("sync")
  })

  it("reads one kind directly for a kind view, without the implementors", async () => {
    renderTimeline(
      { ...traitSpec, trait: undefined, kind: TASK },
      vi.fn(),
      task
    )
    await screen.findByText("Call the landlord")
    expect(calls.some((c) => c.url.includes("/implementors"))).toBe(false)
    expect(readOf(EVENT)).toBeUndefined()
  })
})

describe("TimelineLayout range", () => {
  const now = Date.now()
  const window = windowRange(traitSpec, now)
  const quick = (key: string) =>
    QUICK_RANGES.find((q) => q.key === key)!.range(now)

  it("shows the window as the range and offers the chips with nothing pressed", async () => {
    renderTimeline()
    await screen.findByText("Platform sync")
    expect(screen.getByText(rangeLabel(window, now))).toBeTruthy()
    const group = screen.getByRole("group", { name: "Range" })
    const chips = within(group).getAllByRole("button")
    expect(chips.map((c) => c.textContent)).toEqual([
      "This week",
      "This month",
      "Next 3 months",
      "Past month",
      "Custom",
    ])
    expect(chips.every((c) => c.getAttribute("aria-pressed") === "false")).toBe(
      true
    )
    expect(screen.queryByRole("button", { name: "Reset" })).toBeNull()
    // The read is the window's own: a week before now, to the minute the
    // layout rounds to, not a day boundary.
    const gte = Date.parse(readOf(TASK)!.filter.properties.dueAt.gte)
    expect(Math.abs(gte - (now - 7 * 24 * 60 * 60_000))).toBeLessThan(60_000)
  })

  it("reads the days the address names instead of the window", async () => {
    const range = { from: "2026-09-01", to: "2026-09-30" }
    renderTimeline(traitSpec, vi.fn(), undefined, {
      search: `?from=${range.from}&to=${range.to}`,
    })
    await waitFor(() => expect(readOf(TASK)).toBeDefined())
    expect(readOf(TASK)?.filter.properties.dueAt).toEqual(rangeBounds(range))
    expect(readOf(EVENT)?.filter.properties.at).toEqual(rangeBounds(range))
    expect(screen.getByText(rangeLabel(range, now))).toBeTruthy()
    expect(screen.getByRole("button", { name: "Reset" })).toBeTruthy()
  })

  it("falls back to the window when the address is not a day", async () => {
    renderTimeline(traitSpec, vi.fn(), undefined, {
      search: "?from=yesterday&to=2026-02-30",
    })
    await screen.findByText("Platform sync")
    expect(screen.getByText(rangeLabel(window, now))).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Reset" })).toBeNull()
  })

  it("a chip rewrites the address and re-requests every kind on the new bounds", async () => {
    const { urls } = renderTimeline()
    await screen.findByText("Write the retro notes")
    const before = calls.length
    fireEvent.click(screen.getByRole("button", { name: "Past month" }))
    const past = quick("past")
    await waitFor(() => expect(lastUrl(urls)).toEqual(past))
    await waitFor(() => expect(calls.length).toBeGreaterThan(before))
    await waitFor(() =>
      expect(lastReadOf(TASK)?.filter.properties.dueAt).toEqual(
        rangeBounds(past)
      )
    )
    expect(lastReadOf(EVENT)?.filter.properties.at).toEqual(rangeBounds(past))
    // The stub answers only what falls inside: tomorrow's task is gone.
    await waitFor(() =>
      expect(screen.queryByText("Write the retro notes")).toBeNull()
    )
    expect(screen.getByText("Call the landlord")).toBeTruthy()
    expect(screen.getByText(rangeLabel(past, now))).toBeTruthy()
    expect(
      screen
        .getByRole("button", { name: "Past month" })
        .getAttribute("aria-pressed")
    ).toBe("true")
  })

  it("Custom reveals two date inputs prefilled from the range, and a changed day re-requests", async () => {
    const { urls } = renderTimeline()
    await screen.findByText("Platform sync")
    expect(screen.queryByLabelText("From")).toBeNull()
    fireEvent.click(screen.getByRole("button", { name: "Custom" }))
    const from = screen.getByLabelText("From") as HTMLInputElement
    const to = screen.getByLabelText("To") as HTMLInputElement
    expect(from.type).toBe("date")
    expect(from.value).toBe(window.from)
    expect(to.value).toBe(window.to)

    fireEvent.change(to, { target: { value: "2026-12-24" } })
    const range = { from: window.from, to: "2026-12-24" }
    await waitFor(() => expect(lastUrl(urls)).toEqual(range))
    await waitFor(() =>
      expect(lastReadOf(TASK)?.filter.properties.dueAt).toEqual(
        rangeBounds(range)
      )
    )
    expect(screen.getByText(rangeLabel(range, now))).toBeTruthy()
    expect(
      screen
        .getByRole("button", { name: "Custom" })
        .getAttribute("aria-pressed")
    ).toBe("true")
  })

  it("Reset clears the address and returns to the window", async () => {
    const { urls } = renderTimeline(traitSpec, vi.fn(), undefined, {
      search: "?from=2026-09-01&to=2026-09-30",
    })
    await waitFor(() => expect(readOf(TASK)).toBeDefined())
    fireEvent.click(screen.getByRole("button", { name: "Reset" }))
    await waitFor(() => expect(lastUrl(urls)).toEqual({ from: null, to: null }))
    await waitFor(() =>
      expect(screen.getByText(rangeLabel(window, now))).toBeTruthy()
    )
    expect(screen.queryByRole("button", { name: "Reset" })).toBeNull()
  })

  it("names the range when nothing falls in it", async () => {
    const range = { from: "2020-01-01", to: "2020-01-31" }
    renderTimeline(traitSpec, vi.fn(), undefined, {
      search: `?from=${range.from}&to=${range.to}`,
    })
    expect(await screen.findByText("Nothing in this range")).toBeTruthy()
    expect(screen.getAllByText(rangeLabel(range, now)).length).toBe(2)
  })

  it("opens on today while today is in range, else starts at the top", async () => {
    const spy = vi.spyOn(Element.prototype, "scrollIntoView")
    renderTimeline()
    await screen.findByText("Call the landlord")
    await waitFor(() => expect(spy).toHaveBeenCalledTimes(1))
    expect(
      (spy.mock.instances[0] as unknown as Element).getAttribute("aria-current")
    ).toBe("date")

    fireEvent.click(screen.getByRole("button", { name: "Custom" }))
    fireEvent.change(screen.getByLabelText("From"), {
      target: { value: "2020-01-01" },
    })
    fireEvent.change(screen.getByLabelText("To"), {
      target: { value: "2020-01-31" },
    })
    await screen.findByText("Nothing in this range")
    await waitFor(() => {
      const top = spy.mock.instances[
        spy.mock.calls.length - 1
      ] as unknown as Element
      expect(top.querySelector('[aria-label="Range"]')).not.toBeNull()
    })

    fireEvent.click(screen.getByRole("button", { name: "Reset" }))
    await screen.findByText("Call the landlord")
    await waitFor(() => {
      const last = spy.mock.instances[
        spy.mock.calls.length - 1
      ] as unknown as Element
      expect(last.getAttribute("aria-current")).toBe("date")
    })
    spy.mockRestore()
  })

  it("a card keeps the window and draws no control", async () => {
    renderTimeline(traitSpec, vi.fn(), undefined, {
      search: "?from=2020-01-01&to=2020-01-31",
      ctx: { inputs: {}, mode: "card" },
    })
    await screen.findByText("Platform sync")
    expect(screen.queryByRole("group", { name: "Range" })).toBeNull()
    expect(readOf(TASK)?.filter.properties.dueAt.gte).not.toBe(
      rangeBounds({ from: "2020-01-01", to: "2020-01-31" }).gte
    )
  })
})
