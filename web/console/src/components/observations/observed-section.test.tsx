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
  METRIC,
  METRICS,
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

/** Mounts the section; `metrics` answers a read of those records by id
 * before the stand-in server's own. */
function mount(
  record: SubstrateRecord,
  kind: KindInfo,
  metrics: SubstrateRecord[] = []
) {
  const server = observationsServer(() => LATEST)
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const filter = JSON.parse(
        new URL(String(input), "http://test").searchParams.get("filter") ?? "{}"
      ) as { kinds?: string[]; ids?: string[] }
      const own = metrics.filter(
        (m) => filter.kinds?.includes(m.kind) && filter.ids?.includes(m.id)
      )
      return own.length
        ? new Response(
            JSON.stringify({ records: own, head: 0, generation: "g" })
          )
        : server.fetch(input)
    })
  )
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

  it("reads its stats to its metric's declared places", async () => {
    const whole: SubstrateRecord = {
      ...METRICS.find((m) => m.id === "health/heart_rate")!,
    }
    whole.properties = { ...whole.properties, displayPrecision: 0 }
    const view = mount(HEART_TRACE, traceKind, [whole])
    const stats = view.container.querySelector(
      "[data-slot=trace-stats]"
    ) as HTMLElement
    const fact = (label: string) =>
      within(stats).getByText(label).nextElementSibling?.textContent
    await waitFor(() => expect(fact("Average")).toBe("132 /min"))
    expect(fact("Standard deviation")).toBe("35 /min")
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

  // The page was the component's own state: from page three of one trace,
  // another record's twenty points sliced from the hundredth and showed
  // none, with no pager to go back.
  it("starts another record's readings on their first page", () => {
    const long: SubstrateRecord = {
      ...HEART_TRACE,
      properties: {
        ...HEART_TRACE.properties,
        offsets: undefined,
        every: "PT1S",
        values: Array.from({ length: 160 }, (_, i) => String(60 + (i % 40))),
      },
    }
    const short: SubstrateRecord = {
      ...long,
      id: `${HEART_TRACE.id}/short`,
      properties: { ...long.properties, values: Array(20).fill("61") },
    }
    const view = mount(long, traceKind)
    fireEvent.click(screen.getByRole("button", { name: "Next page" }))
    fireEvent.click(screen.getByRole("button", { name: "Next page" }))
    const show = (record: SubstrateRecord) =>
      view.rerender(
        <QueryClientProvider client={new QueryClient()}>
          <ObservedSection record={record} kind={traceKind} />
        </QueryClientProvider>
      )
    const shown = () =>
      within(screen.getByRole("table", { name: "Values" }))
        .getAllByRole("row")
        .slice(1)
    // A rewrite of the same record that shortens it keeps a page it holds.
    show({ ...short, id: long.id })
    expect(shown()).toHaveLength(20)
    // Another record starts on its first page.
    show(long)
    expect(shown()[0].textContent).toContain(`${60 + (100 % 40)} /min`)
    show(short)
    expect(shown()).toHaveLength(20)
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

  it("shows the measure the engine stamped, and none where it stamped none", () => {
    const stamped = {
      ...WEIGHING,
      properties: { ...WEIGHING.properties, measure: "mass" },
    }
    const view = mount(stamped, observationKind)
    const facts = view.container.querySelector("[data-slot=observed-value]")!
    expect(
      within(facts as HTMLElement).getByText("Measure").nextElementSibling
        ?.textContent
    ).toBe("Mass")
    cleanup()
    const bare = mount(WEIGHING, observationKind)
    expect(
      within(
        bare.container.querySelector(
          "[data-slot=observed-value]"
        ) as HTMLElement
      ).queryByText("Measure")
    ).toBe(null)
  })

  it("says a summary's statistic in words, as a chart's reducer reads", () => {
    const summary = (statistic: string, period: string): SubstrateRecord => ({
      ...WEIGHING,
      properties: {
        ...WEIGHING.properties,
        form: "summary",
        statistic,
        period,
      },
    })
    const fact = (view: ReturnType<typeof mount>) =>
      within(
        view.container.querySelector(
          "[data-slot=observed-value]"
        ) as HTMLElement
      ).getByText("Statistic").nextElementSibling?.textContent
    expect(fact(mount(summary("avg", "night"), observationKind))).toBe(
      "Average, per night"
    )
    cleanup()
    expect(fact(mount(summary("vendor", "day"), observationKind))).toBe(
      "The vendor's own, per day"
    )
  })

  it("reads a lab result against its range, and an instant once", () => {
    const ldl: SubstrateRecord = {
      ...WEIGHING,
      properties: {
        ...WEIGHING.properties,
        value: { value: "108", unit: "mg/dL" },
        at: "2026-10-05T05:10:00Z",
        endsAt: "2026-10-05T05:10:00Z",
        interpretation: [
          {
            system:
              "http://terminology.hl7.org/CodeSystem/v3-ObservationInterpretation",
            code: "N",
            display: "Normal",
          },
        ],
        referenceRanges: [{ high: { value: "120", unit: "mg/dL" } }],
      },
    }
    const view = mount(ldl, observationKind)
    const facts = view.container.querySelector(
      "[data-slot=observed-value]"
    ) as HTMLElement
    const fact = (label: string) =>
      within(facts).queryByText(label)?.nextElementSibling?.textContent
    expect(fact("Value")).toBe("108 mg/dLnormal (under 120 mg/dL)")
    expect(fact("When")).toContain("Oct 5")
    expect(within(facts).queryByText("From")).toBe(null)
    expect(within(facts).queryByText("Until")).toBe(null)
  })

  it("draws a result against its own range in the value's unit", () => {
    const result = (range: Record<string, unknown>): SubstrateRecord => ({
      ...WEIGHING,
      properties: {
        ...WEIGHING.properties,
        value: { value: "3.6", unit: "mmol/L" },
        interpretation: [{ code: "H" }],
        referenceRanges: [range],
      },
    })
    const view = mount(
      result({
        low: { value: "2.6", unit: "mmol/L" },
        high: { value: "3.4", unit: "mmol/L" },
      }),
      observationKind
    )
    const bar = view.container.querySelector("[data-slot=range-bar]")!
    // A scale of values never negative starts at zero, its top padded by
    // 5%: 3.6 sits at 3.6 / 3.78 of it.
    expect(Number(bar.getAttribute("data-value-at"))).toBeCloseTo(95.24, 1)
    const band = bar.querySelector("[data-band]") as HTMLElement
    expect(band.getAttribute("data-band")).toBe("reference range")
    // A lab's normal range is drawn in the good tone, a dashed edge at each
    // bound, each bound printed where it falls.
    expect(band.style.background).toBe("var(--ok-soft)")
    expect(bar.querySelectorAll("[data-slot=range-edge]")).toHaveLength(2)
    const printed = [...bar.querySelectorAll("[data-slot=range-bound]")].map(
      (b) => b.textContent
    )
    expect(printed).toEqual(["0 mmol/L", "2.6 mmol/L", "3.4 mmol/L"])
    // A high flag marks the value in the poor tone, beside its word.
    const marker = bar.querySelector("[data-slot=range-value]") as HTMLElement
    expect(marker.style.background).toBe("var(--destructive)")
    expect(view.container.textContent).toContain("high (2.6 to 3.4 mmol/L)")
    cleanup()
    // A range in another unit is said in words, never drawn.
    const other = mount(
      result({ high: { value: "60", unit: "mg/dL" } }),
      observationKind
    )
    expect(other.container.querySelector("[data-slot=range-bar]")).toBe(null)
    cleanup()
    // A bound in another unit of the same measure is converted: 3 mmol/L
    // is 3000 umol/L.
    const converted = mount(
      result({ high: { value: "3000", unit: "umol/L" } }),
      observationKind
    )
    expect(
      converted.container.querySelector("[data-slot=range-bound]")?.textContent
    ).toBe("0 mmol/L")
    expect(
      [...converted.container.querySelectorAll("[data-slot=range-bound]")].map(
        (b) => b.textContent
      )
    ).toContain("3 mmol/L")
  })

  it("draws a one-sided range from zero and a cut-off in its own tone", () => {
    const result = (
      value: string,
      range: Record<string, unknown>
    ): SubstrateRecord => ({
      ...WEIGHING,
      properties: {
        ...WEIGHING.properties,
        value: { value, unit: "ng/mL" },
        referenceRanges: [range],
      },
    })
    const under = mount(
      result("2.8", { high: { value: "3", unit: "ng/mL" } }),
      observationKind
    )
    const bar = under.container.querySelector("[data-slot=range-bar]")!
    // 0 to 3 padded by 5% at the top: 2.8 sits at 2.8 / 3.15 of it.
    expect(Number(bar.getAttribute("data-value-at"))).toBeCloseTo(88.89, 1)
    const band = bar.querySelector("[data-band]") as HTMLElement
    expect(band.style.left).toBe("0%")
    expect(bar.querySelectorAll("[data-slot=range-edge]")).toHaveLength(1)
    expect(bar.textContent).toContain("3 ng/mL")
    cleanup()
    // A cut-off the lab marks poor (a high band with a low bound alone)
    // is not drawn as normal.
    const cut = mount(
      result("2.8", {
        low: { value: "4", unit: "ng/mL" },
        text: "high",
        tone: "poor",
      }),
      observationKind
    )
    const high = cut.container.querySelector("[data-band]") as HTMLElement
    expect(high.getAttribute("data-band")).toBe("high")
    expect(high.style.background).toBe("var(--bad-soft)")
    cleanup()
    // A bound such as <0.5 has no place on the scale: no bar.
    const bound = mount(
      {
        ...result("0.5", { high: { value: "3", unit: "ng/mL" } }),
        properties: {
          ...result("0.5", { high: { value: "3", unit: "ng/mL" } }).properties,
          value: { value: "0.5", unit: "ng/mL", comparator: "<" },
        },
      },
      observationKind
    )
    expect(bound.container.querySelector("[data-slot=range-bar]")).toBe(null)
  })

  it("reads a value to its metric's declared places, and a bound whole", async () => {
    const hrv: SubstrateRecord = {
      ...WEIGHING,
      kind: METRIC,
      id: "health/hrv_rmssd",
      properties: {
        name: "Heart rate variability",
        form: "gauge",
        unit: "ms",
        displayPrecision: 0,
      },
    }
    const reading = (value: Record<string, string>): SubstrateRecord => ({
      ...WEIGHING,
      properties: {
        ...WEIGHING.properties,
        metric: { ref: `${METRIC}/health/hrv_rmssd` },
        value,
      },
    })
    const valueOf = (view: ReturnType<typeof mount>) =>
      within(
        view.container.querySelector(
          "[data-slot=observed-value]"
        ) as HTMLElement
      ).getByText("Value").nextElementSibling as HTMLElement
    // As the streams table reads it: 58 ms, the stored digits on hover.
    let view = mount(reading({ value: "58.42", unit: "ms" }), observationKind, [
      hrv,
    ])
    await waitFor(() => expect(valueOf(view).textContent).toBe("58 ms"))
    expect(valueOf(view).querySelector("[title]")?.getAttribute("title")).toBe(
      "58.42"
    )
    cleanup()
    // A lab's bound keeps its comparator and every digit.
    view = mount(
      reading({ value: "0.005", unit: "ng/mL", comparator: "<" }),
      observationKind,
      [hrv]
    )
    expect(valueOf(view).textContent).toBe("<0.005 ng/mL")
  })

  it("shows nothing for a kind that does not bind the trait", () => {
    const view = mount(WEIGHING, metricKind)
    expect(view.container.textContent).toBe("")
  })
})
