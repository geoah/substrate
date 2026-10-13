// @vitest-environment jsdom
/** The record page's observed section: a trace's values as a sparkline, its
 * managed stats as labelled values and its points as a paged table; a single
 * value's facts under their labels; nothing for a kind that does not bind
 * the trait. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { KindInfo, SubstrateRecord } from "@/lib/api/types"
import {
  GARMIN,
  HEART,
  HEART_TRACE,
  WEIGHING,
  metricKind,
  observationKind,
  observationsServer,
  traceKind,
  LATEST,
} from "@/lib/observations.test-fixtures"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    children,
    search,
  }: {
    children: ReactNode
    search?: Record<string, string>
  }) => <a data-search={JSON.stringify(search ?? {})}>{children}</a>,
}))

import { ObservedSection } from "./observed-section"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

function mount(record: SubstrateRecord, kind: KindInfo) {
  const server = observationsServer(() => LATEST)
  vi.stubGlobal("fetch", vi.fn(server.fetch))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  return render(
    <QueryClientProvider client={client}>
      <ObservedSection record={record} kind={kind} />
    </QueryClientProvider>
  )
}

describe("a trace", () => {
  it("draws its values over its period and reads its stats under labels", () => {
    const view = mount(HEART_TRACE, traceKind)
    expect(screen.getByRole("heading", { name: "Readings" })).toBeTruthy()
    expect(view.container.textContent).toContain("7 values")
    const line = view.container.querySelector(
      "svg path[stroke='var(--series-1)']"
    )
    expect(line?.getAttribute("d")?.match(/[ML]/g)).toHaveLength(7)
    // 06:06 to 06:30 is longer than the trace's 4-minute hold: two runs.
    expect(line?.getAttribute("d")?.match(/M/g)).toHaveLength(2)
    const stats = view.container.querySelector("[data-slot=trace-stats]")!
    const fact = (label: string) =>
      within(stats as HTMLElement).getByText(label).nextElementSibling
        ?.textContent
    expect(fact("Values")).toBe("1,812")
    expect(fact("Lowest")).toBe("55 /min")
    expect(fact("Highest")).toBe("171 /min")
    expect(fact("Average")).toBe("131.6 /min")
    expect(fact("Standard deviation")).toBe("34.87 /min")
    expect(fact("Covered")).toBe("59m 58s")
  })

  it("follows its box's width as the page around it changes", () => {
    let width = 500
    let resized = () => {}
    const clientWidth = Object.getOwnPropertyDescriptor(
      HTMLElement.prototype,
      "clientWidth"
    )
    Object.defineProperty(HTMLElement.prototype, "clientWidth", {
      configurable: true,
      get: () => width,
    })
    vi.stubGlobal(
      "ResizeObserver",
      class {
        constructor(callback: () => void) {
          resized = callback
        }
        observe() {}
        disconnect() {}
      }
    )
    try {
      const view = mount(HEART_TRACE, traceKind)
      const svg = () =>
        view.container.querySelector("[aria-roledescription=chart] svg")!
      expect(svg().getAttribute("width")).toBe("500")
      // The sidebar folds: the box narrows.
      width = 300
      act(() => resized())
      expect(svg().getAttribute("width")).toBe("300")
    } finally {
      if (clientWidth) {
        Object.defineProperty(HTMLElement.prototype, "clientWidth", clientWidth)
      }
    }
  })

  it("lists every value with its time and unit", () => {
    mount(HEART_TRACE, traceKind)
    const table = screen.getByRole("table", { name: "Values" })
    const rows = within(table).getAllByRole("row").slice(1)
    expect(rows).toHaveLength(7)
    expect(rows[6].textContent).toContain("124 /min")
  })

  it("pages a long trace fifty values at a time", () => {
    const long: SubstrateRecord = {
      ...HEART_TRACE,
      properties: {
        ...HEART_TRACE.properties,
        offsets: undefined,
        every: "PT1S",
        values: Array.from({ length: 120 }, (_, i) => String(60 + (i % 40))),
      },
    }
    mount(long, traceKind)
    const table = screen.getByRole("table", { name: "Values" })
    expect(within(table).getAllByRole("row").slice(1)).toHaveLength(50)
    fireEvent.click(screen.getByRole("button", { name: "Next page" }))
    const rows = within(table).getAllByRole("row").slice(1)
    expect(rows).toHaveLength(50)
    expect(rows[0].textContent).toContain(`${60 + (50 % 40)} /min`)
  })

  it("opens the timeline on its stream over its own period", () => {
    const view = mount(HEART_TRACE, traceKind)
    const link = within(view.container)
      .getByText("Open in the timeline")
      .closest("a")!
    expect(JSON.parse(link.dataset.search!)).toMatchObject({
      metric: HEART,
      source: GARMIN,
      range: "custom",
      start: "2026-10-07T06:00:00.000Z",
      end: "2026-10-07T07:00:00.000Z",
    })
  })
})

describe("a single value", () => {
  it("reads what was measured, by what, about what and how", async () => {
    const view = mount(WEIGHING, observationKind)
    expect(screen.getByRole("heading", { name: "Measurement" })).toBeTruthy()
    const facts = view.container.querySelector("[data-slot=observed-value]")!
    const fact = (label: string) =>
      within(facts as HTMLElement).getByText(label)
        .nextElementSibling as HTMLElement | null
    expect(fact("Value")?.textContent).toBe("72.40 kg")
    expect(fact("Form")?.textContent).toBe("Level")
    expect(fact("About")?.textContent).toBe("You")
    expect(fact("Revision")?.textContent).toBe("1")
    await waitFor(() => expect(fact("Metric")?.textContent).toContain("Weight"))
    await waitFor(() =>
      expect(fact("Source")?.textContent).toContain("Withings Body+")
    )
  })

  it("shows nothing for a kind that does not bind the trait", () => {
    const view = mount(WEIGHING, metricKind)
    expect(view.container.textContent).toBe("")
  })
})
