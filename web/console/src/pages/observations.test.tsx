// @vitest-environment jsdom
/** The streams table: the latest read, one row per stream with its newest
 * value in its unit, in the records grid with its machinery (the filter bar
 * and the search narrowing the rows, the sort and the grouping in the
 * address, saved views), a row's metric opening the timeline on its
 * stream, the selection in the address and the Timeline panel drawing it,
 * and the page saying so where nothing records measurements. Served by a
 * stand-in records route. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react"
import { NuqsTestingAdapter, type UrlUpdateEvent } from "nuqs/adapters/testing"
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import type { AggregateResult, AggregateRow } from "@/lib/api/types"
import { DEFAULT_SETTINGS, type ConsoleAction } from "@/lib/console-preferences"
import { encodeFilter } from "@/lib/filters"
import type { SavedView } from "@/lib/saved-views"

import {
  APPLE,
  BODY_MASS,
  GARMIN,
  HEART,
  KINDS,
  LATEST,
  LATEST_CHANNELS,
  LATEST_PEOPLE,
  MIA,
  RECOVERY,
  SLEEP_STAGE,
  STEPS_METRIC,
  WITHINGS,
  latestAnswer,
  metricKind,
  observationsServer,
  sourceKind,
  type SeenRead,
} from "@/lib/observations.test-fixtures"

vi.mock("@tanstack/react-router", () => ({
  // The link carries where it goes, as the address the timeline reads.
  Link: ({ children, search }: { children: ReactNode; search?: unknown }) => (
    <a href="#" data-search={JSON.stringify(search ?? null)}>
      {children}
    </a>
  ),
  useNavigate: () => vi.fn(),
}))

import { ObservationsPage } from "./observations"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  localStorage.clear()
})

/** A filter token, as the bar writes one: a field's values, any of. */
function filter(field: string, ...values: string[]): string {
  return encodeURIComponent(
    encodeFilter({ field, op: "eq", value: values.join(",") })
  )
}

function mount(
  searchParams = "",
  kinds = KINDS,
  respond: (read: SeenRead) => AggregateResult | Response = (read) =>
    latestAnswer(read),
  views: SavedView[] = [],
  technical = false,
  kindsFail = false
) {
  const server = observationsServer(respond, kinds)
  vi.stubGlobal(
    "fetch",
    vi.fn((input: RequestInfo | URL) =>
      kindsFail && String(input).includes("core%2Fkind")
        ? Promise.resolve(unavailable())
        : server.fetch(input)
    )
  )
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  const urls: UrlUpdateEvent[] = []
  const actions: ConsoleAction[] = []
  render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: technical,
          views,
        },
        busy: false,
        change: (action) => void actions.push(action),
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <NuqsTestingAdapter
          searchParams={searchParams}
          hasMemory
          onUrlUpdate={(e) => void urls.push(e)}
        >
          <ObservationsPage />
        </NuqsTestingAdapter>
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
  /** The address as the page last wrote it. */
  const param = (key: string) => urls.at(-1)?.searchParams.get(key) ?? null
  return Object.assign(server, { urls, actions, param })
}

/** The problem the server names when a latest read makes too many groups. */
const PROBLEM =
  "aggregate.budget.maxGroups: the latest read makes 201 groups, over 200; narrow the filter or raise maxGroups (the cap is 2000)"

/** The server's 422 for a read over its group budget. */
function refusal(problem = PROBLEM): Response {
  return new Response(
    JSON.stringify({
      error: {
        code: "validation",
        message: "the aggregate read was refused",
        problems: [problem],
      },
    }),
    { status: 422, headers: { "Content-Type": "application/json" } }
  )
}

/** The problem the server names when a facet's read passes the group cap. */
const CAP_PROBLEM =
  "aggregate.budget.maxGroups: the read makes 2001 groups, over 2000; narrow the filter or raise maxGroups (the cap is 2000)"

/** The server's 503 for a read it could not run just then. */
function unavailable(): Response {
  return new Response(
    JSON.stringify({
      error: { code: "unavailable", message: "the database is restarting" },
    }),
    { status: 503, headers: { "Content-Type": "application/json" } }
  )
}

/** The table's body rows, once the streams have landed. */
async function rows(): Promise<HTMLElement[]> {
  const table = await screen.findByRole("table")
  await waitFor(() =>
    expect(within(table).getAllByText("Heart rate").length).toBeGreaterThan(0)
  )
  return within(table).getAllByRole("row").slice(1)
}

/** The table's body rows as they stand. */
function bodyRows(): HTMLElement[] {
  return within(screen.getByRole("table")).getAllByRole("row").slice(1)
}

/** An applied filter's control, by its field's label: `Source | Garmin`. */
function filterControl(label: string): HTMLElement {
  return screen.getAllByRole("button", { name: new RegExp(`^${label}.+`) })[0]
}

/** Picks one value of a facet from "Add filter", then closes the menu. */
async function addFilter(facet: string, value: RegExp) {
  fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
  fireEvent.click(await screen.findByRole("option", { name: facet }))
  fireEvent.click(await screen.findByRole("option", { name: value }))
  fireEvent.keyDown(document.activeElement ?? document.body, { key: "Escape" })
  await waitFor(() =>
    expect(screen.queryByRole("option", { name: value })).toBe(null)
  )
}

describe("the streams table", () => {
  it("lists every stream in one read", async () => {
    const server = mount()
    await rows()
    expect(server.seen).toEqual(
      expect.arrayContaining([
        {
          filter: { implements: "substrate.reamde.dev/core/observed" },
          aggregate: {
            groupBy: ["metric", "source", "about", "channel"],
            reducers: ["last"],
            budget: { maxGroups: 2000 },
          },
        },
      ])
    )
    // No list of metrics and no batch: the one read answered every stream.
    expect(
      server.seen.filter(
        (r) =>
          r.aggregate.groupBy?.join() === "metric" ||
          r.filter.properties?.metric
      )
    ).toEqual([])
  })

  it("lists the metrics, then their streams a few at a time, where one read of every stream makes too many groups", async () => {
    const server = mount("", KINDS, (read) =>
      read.aggregate.groupBy?.length === 4 && !read.filter.properties?.metric
        ? refusal(CAP_PROBLEM)
        : latestAnswer(read)
    )
    expect(await rows()).toHaveLength(6)
    expect(screen.queryByText(/didn’t load/)).toBe(null)
    expect(
      server.seen.some((r) => r.aggregate.groupBy?.join() === "metric")
    ).toBe(true)
    const batches = server.seen.filter((r) => r.filter.properties?.metric)
    expect(batches).toHaveLength(1)
    expect(
      [...(batches[0].filter.properties?.metric?.in ?? [])].sort()
    ).toEqual([BODY_MASS, HEART, RECOVERY, SLEEP_STAGE, STEPS_METRIC].sort())
  })

  it("narrows the streams read to the chosen metric on the server", async () => {
    const server = mount(`?filter=${filter("metric", BODY_MASS)}`)
    const table = await screen.findByRole("table")
    await waitFor(() => expect(table.textContent).toContain("Weight"))
    const streamReads = server.seen.filter(
      (r) => r.aggregate.groupBy?.length === 4
    )
    expect(streamReads.map((r) => r.filter.properties)).toEqual([
      { metric: { in: [BODY_MASS] } },
    ])
    // The metric filter still offers the other metrics.
    fireEvent.click(filterControl("Metric"))
    expect(
      await screen.findByRole("option", { name: /Heart rate/ })
    ).toBeTruthy()
  })

  it("names a chosen metric by its record, spelled by id or by path", async () => {
    for (const spelling of ["health/body_mass", BODY_MASS]) {
      mount(`?filter=${filter("metric", spelling)}`)
      await waitFor(() => expect(bodyRows()).toHaveLength(1))
      await waitFor(() =>
        expect(filterControl("Metric").textContent).toContain("Weight")
      )
      // The menu checks the chosen metric, whichever way it was spelled.
      fireEvent.click(filterControl("Metric"))
      const weight = await screen.findByRole("option", { name: /Weight/ })
      expect(weight.getAttribute("aria-selected")).toBe("true")
      cleanup()
    }
  })

  it("lists the chosen metric's streams when only the list of every metric is refused", async () => {
    // The list of every metric is the one read grouped by metric alone.
    mount(`?filter=${filter("metric", BODY_MASS)}`, KINDS, (read) =>
      read.aggregate.groupBy?.join() === "metric"
        ? refusal()
        : latestAnswer(read)
    )
    const table = await screen.findByRole("table")
    await waitFor(() => expect(table.textContent).toContain("Weight"))
    expect(screen.queryByText("The streams didn’t load")).toBe(null)
    const note = await screen.findByRole("alert", {
      name: "The other metrics didn’t load",
    })
    expect(note.textContent).toContain(PROBLEM)
  })

  it("says a refused read, naming its problems, never that nothing was measured", async () => {
    mount("", KINDS, () => refusal())
    expect(await screen.findByText("The streams didn’t load")).toBeTruthy()
    expect(screen.getByText(PROBLEM)).toBeTruthy()
    expect(screen.queryByText(/Nothing has been measured/)).toBe(null)
  })

  it("lists one row per stream with its metric, source, form and latest value", async () => {
    mount()
    const body = await rows()
    expect(body).toHaveLength(6)
    const first = body[0].textContent ?? ""
    expect(first).toContain("Heart rate")
    expect(first).toContain("Level")
    expect(first).toContain("96 /min")
    expect(first).toContain("1,812")
    await waitFor(() =>
      expect(body[0].textContent).toContain("Garmin Forerunner 265")
    )
    const weight = body.find((r) => r.textContent?.includes("Weight"))!
    expect(weight.textContent).toContain("72.40 kg")
    const sleep = body.find((r) => r.textContent?.includes("Sleep stage"))!
    expect(sleep.textContent).toContain("Category")
    // Its newest value is a code, which a coded `last` answers in `code`.
    expect(sleep.textContent).toContain("Awake")
    // A stream about the owner names nobody; one about Mia names her record.
    expect(weight.textContent).toContain("You")
    expect(sleep.textContent).not.toContain("You")
  })

  it("shows a newest value to the metric's places, the stored digits on hover and in technical mode", async () => {
    const precise: AggregateResult = {
      ...LATEST,
      rows: LATEST.rows.map((r, i) =>
        i === 1
          ? {
              ...r,
              values: [{ reducer: "last", value: "61.405858", unit: "/min" }],
            }
          : r
      ),
    }
    mount("", KINDS, (read) => latestAnswer(read, precise))
    await rows()
    const apple = await waitFor(() => {
      const row = bodyRows().find((r) => r.textContent?.includes("61.41"))
      expect(row).toBeTruthy()
      return row!
    })
    // No displayPrecision on the fixture's metric: two places.
    expect(apple.textContent).toContain("61.41 /min")
    expect(within(apple).getByTitle("61.405858")).toBeTruthy()
    cleanup()
    mount("", KINDS, (read) => latestAnswer(read, precise), [], true)
    await rows()
    await waitFor(() =>
      expect(
        bodyRows().some((r) => r.textContent?.includes("61.405858 /min"))
      ).toBe(true)
    )
  })

  it("narrows to the chosen sources and readings from the address", async () => {
    mount(`?filter=${filter("source", "withings-body-plus")}`)
    const table = await screen.findByRole("table")
    await waitFor(() =>
      expect(within(table).getAllByRole("row").slice(1)).toHaveLength(1)
    )
    expect(table.textContent).toContain("Weight")
    cleanup()
    mount(`?filter=${filter("shape", "summary")}`)
    const again = await screen.findByRole("table")
    await waitFor(() => expect(again.textContent).toContain("Recovery"))
    expect(within(again).getAllByRole("row").slice(1)).toHaveLength(1)
  })

  it("searches the streams' words", async () => {
    mount("?search=sleep")
    const table = await screen.findByRole("table")
    await waitFor(() => expect(table.textContent).toContain("Sleep stage"))
    expect(within(table).getAllByRole("row").slice(1)).toHaveLength(1)
  })

  it("offers each facet's values by name in the filter bar", async () => {
    mount()
    await rows()
    fireEvent.click(screen.getByRole("button", { name: /Add filter/ }))
    fireEvent.click(await screen.findByRole("option", { name: "Source" }))
    expect(
      await screen.findByRole("option", { name: /Withings Body\+/ })
    ).toBeTruthy()
  })

  it("offers the other sources when the chosen source's menu reopens", async () => {
    mount()
    await rows()
    await addFilter("Source", /Garmin Forerunner 265/)
    // The streams read narrows to the Garmin on the server.
    await waitFor(() => expect(bodyRows()).toHaveLength(2))
    fireEvent.click(filterControl("Source"))
    fireEvent.click(await screen.findByRole("option", { name: /Apple Watch/ }))
    await waitFor(() => expect(bodyRows()).toHaveLength(3))
    await waitFor(() =>
      expect(screen.getByRole("table").textContent).toContain("Apple Watch S10")
    )
  })

  it("offers the other people when the chosen person's menu reopens", async () => {
    mount("", KINDS, (read) => latestAnswer(read, LATEST_PEOPLE))
    await rows()
    await addFilter("About", /mia/)
    // Mia's sleep and heart rate, read narrowed to her on the server.
    await waitFor(() => expect(bodyRows()).toHaveLength(2))
    fireEvent.click(filterControl("About"))
    fireEvent.click(await screen.findByRole("option", { name: /leo/ }))
    await waitFor(() => expect(bodyRows()).toHaveLength(3))
  })

  it("says the full Source list is unavailable when only its read passes the group cap", async () => {
    // More than 2,000 sources: the Source menu's own read is refused, the
    // chosen source's streams are not.
    mount(`?filter=${filter("source", GARMIN)}`, KINDS, (read) =>
      read.aggregate.groupBy?.join() === "source"
        ? refusal(CAP_PROBLEM)
        : latestAnswer(read)
    )
    await waitFor(() => expect(bodyRows()).toHaveLength(2))
    const note = await screen.findByRole("alert", {
      name: "The full Source list is unavailable",
    })
    expect(note.textContent).toContain("more than 2,000 sources")
    expect(note.textContent).toContain(CAP_PROBLEM)
    expect(screen.queryByText("The streams didn’t load")).toBe(null)
    // The menu offers the chosen source, and says nothing of the rest.
    fireEvent.click(filterControl("Source"))
    expect(
      await screen.findByRole("option", { name: /Garmin Forerunner 265/ })
    ).toBeTruthy()
    expect(screen.queryByRole("option", { name: /Apple Watch/ })).toBe(null)
  })

  it("says the About list didn’t load, keeps the streams, and reads it again on a retry", async () => {
    let failures = 1
    mount(`?filter=${filter("about", MIA)}`, KINDS, (read) => {
      if (read.aggregate.groupBy?.join() === "about" && failures > 0) {
        failures--
        return unavailable()
      }
      return latestAnswer(read, LATEST_PEOPLE)
    })
    // Mia's sleep and heart rate stay on the page.
    await waitFor(() => expect(bodyRows()).toHaveLength(2))
    const note = await screen.findByRole("alert", {
      name: "The About list didn’t load",
    })
    expect(note.textContent).toContain("the database is restarting")
    fireEvent.click(filterControl("About"))
    expect(await screen.findByRole("option", { name: /mia/ })).toBeTruthy()
    expect(screen.queryByRole("option", { name: /leo/ })).toBe(null)
    fireEvent.keyDown(document.activeElement ?? document.body, {
      key: "Escape",
    })
    fireEvent.click(within(note).getByRole("button", { name: "Try again" }))
    await waitFor(() =>
      expect(
        screen.queryByRole("alert", { name: "The About list didn’t load" })
      ).toBe(null)
    )
    fireEvent.click(filterControl("About"))
    expect(await screen.findByRole("option", { name: /leo/ })).toBeTruthy()
    expect(bodyRows()).toHaveLength(2)
  })

  it("opens the timeline narrowed to a row's stream from its metric", async () => {
    mount()
    const body = await rows()
    const weight = body.find((r) => r.textContent?.includes("Weight"))!
    const link = within(weight).getByRole("link", { name: "Weight" })
    expect(JSON.parse(link.getAttribute("data-search")!)).toEqual({
      metric: BODY_MASS,
      source: WITHINGS,
    })
  })

  it("says the kinds did not load, never that nothing records measurements", async () => {
    mount("", KINDS, (read) => latestAnswer(read), [], false, true)
    expect(await screen.findByText("The kinds didn’t load")).toBeTruthy()
    expect(screen.getByText(/the database is restarting/)).toBeTruthy()
    expect(screen.getByRole("button", { name: "Try again" })).toBeTruthy()
    expect(screen.queryByText(/Nothing here records measurements/)).toBe(null)
  })

  it("says nothing records measurements where no kind binds the trait", async () => {
    const server = mount("", [metricKind, sourceKind])
    expect(
      await screen.findByText("Nothing here records measurements yet")
    ).toBeTruthy()
    expect(server.seen).toEqual([])
  })
})

/** A selection token, as the page writes one for a stream of the fixture:
 * the metric and the source by id. */
function token(metric: string, source: string): string {
  return `${metric}|${source}||`
}

/** Heart rate by source over three days, an average per day. */
function heartBySource(read: SeenRead): AggregateResult {
  const day = (d: string, source: string, value: string): AggregateRow => ({
    start: `2026-10-0${d}T00:00:00Z`,
    end: `2026-10-0${Number(d) + 1}T00:00:00Z`,
    group: { source },
    form: "gauge",
    values: [
      { reducer: "avg", value, unit: "/min" },
      { reducer: "min", value: String(Number(value) - 10), unit: "/min" },
      { reducer: "max", value: String(Number(value) + 30), unit: "/min" },
    ],
    count: 60,
    bounded: 0,
    synthetic: false,
    unavailable: false,
  })
  const kept = read.filter.properties?.source?.in as string[] | undefined
  return {
    ...LATEST,
    rows: [
      day("4", GARMIN, "71"),
      day("4", APPLE, "64"),
      day("5", GARMIN, "75"),
      day("5", APPLE, "62"),
    ].filter((r) => !kept || kept.includes(r.group!.source!)),
  }
}

function answer(read: SeenRead): AggregateResult {
  if (read.aggregate.metric?.ref === HEART) return heartBySource(read)
  if (read.aggregate.metric) return { ...LATEST, rows: [] }
  return latestAnswer(read)
}

const WINDOW =
  "range=custom&start=2026-10-04T00:00:00.000Z&end=2026-10-06T00:00:00.000Z&bucket=day"

describe("the streams table's machinery", () => {
  it("sorts by a column from its header, the sort in the address", async () => {
    const server = mount()
    await rows()
    fireEvent.click(screen.getByRole("button", { name: "Points" }))
    await waitFor(() => expect(server.param("sort")).toBe("count:asc"))
    expect(bodyRows()[0].textContent).toContain("Recovery")
    expect(bodyRows().at(-1)!.textContent).toContain("1,812")
    fireEvent.click(screen.getByRole("button", { name: "Points" }))
    await waitFor(() => expect(server.param("sort")).toBe("count:desc"))
    expect(bodyRows()[0].textContent).toContain("1,812")
  })

  it("groups the rows by a stream column, each group under a head", async () => {
    const server = mount()
    await rows()
    fireEvent.click(screen.getByRole("button", { name: "Group" }))
    fireEvent.click(
      await screen.findByRole("menuitemradio", { name: "Source" })
    )
    await waitFor(() => expect(server.param("group")).toBe("source"))
    const fold = await screen.findByRole("button", {
      name: "Hide Source: Garmin Forerunner 265",
    })
    expect(fold).toBeTruthy()
    // The Garmin's two streams sit together under their head.
    const garmin = fold.closest("tbody")!
    expect(within(garmin).getAllByRole("row")).toHaveLength(3)
  })

  it("saves a view of the filters, the sort and the grouping, never the search or the selection", async () => {
    const server = mount(
      `?filter=${filter("source", GARMIN)}&sort=count:asc&group=metric&search=heart&select=${token("health/heart_rate", "garmin-forerunner-265")}`
    )
    await waitFor(() => expect(bodyRows().length).toBeGreaterThan(0))
    fireEvent.click(screen.getByRole("button", { name: "Save view" }))
    fireEvent.change(screen.getByLabelText("Name"), {
      target: { value: "Garmin" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Save" }))
    await waitFor(() => expect(server.actions).toHaveLength(1))
    const action = server.actions[0] as { type: string; view: SavedView }
    expect(action.type).toBe("view")
    expect(action.view).toEqual({
      id: expect.any(String),
      collection: "observations/streams",
      name: "Garmin",
      filter: [decodeURIComponent(filter("source", GARMIN))],
      sort: "count:asc",
      group: "metric",
      columns: expect.arrayContaining(["select", "metric", "source"]),
      hidden: [],
    })
  })

  it("lights a saved view while the address shows what it names", async () => {
    const view: SavedView = {
      id: "v1",
      collection: "observations/streams",
      name: "Garmin",
      filter: [decodeURIComponent(filter("source", GARMIN))],
    }
    const pressed = () =>
      within(screen.getByRole("group", { name: "Views" }))
        .getAllByRole("button")
        .filter((b) => b.getAttribute("aria-pressed") === "true")
        .map((b) => b.textContent)
    mount(`?filter=${filter("source", GARMIN)}`, KINDS, undefined, [view])
    await waitFor(() => expect(bodyRows()).toHaveLength(2))
    expect(pressed()).toEqual(["Garmin"])
    fireEvent.click(screen.getByRole("button", { name: "All" }))
    await waitFor(() => expect(bodyRows()).toHaveLength(6))
    expect(pressed()).toEqual(["All"])
  })
})

describe("selecting streams", () => {
  it("keeps the selection in the address: a row, a shift-run, every row shown", async () => {
    const server = mount()
    await rows()
    const box = (i: number) =>
      within(bodyRows()[i]).getByRole("checkbox") as HTMLInputElement
    fireEvent.click(box(0))
    await waitFor(() =>
      expect(server.param("select")).toBe(
        token("health/heart_rate", "garmin-forerunner-265")
      )
    )
    expect(box(0).checked).toBe(true)
    expect(bodyRows()[0].getAttribute("aria-selected")).toBe("true")
    fireEvent.click(box(2), { shiftKey: true })
    await waitFor(() =>
      expect(server.param("select")?.split(",")).toHaveLength(3)
    )
    expect([box(0), box(1), box(2), box(3)].map((b) => b.checked)).toEqual([
      true,
      true,
      true,
      false,
    ])
    const all = screen.getByRole("checkbox", {
      name: "Select every stream shown",
    }) as HTMLInputElement
    expect(all.indeterminate).toBe(true)
    fireEvent.click(all)
    await waitFor(() =>
      expect(server.param("select")?.split(",")).toHaveLength(6)
    )
    fireEvent.click(
      screen.getByRole("checkbox", { name: "Clear every stream shown" })
    )
    await waitFor(() => expect(server.param("select")).toBe(null))
  })

  it("draws the selected streams in one chart, one line per source, read once per metric", async () => {
    const server = mount(
      `?timeline=true&by=source&${WINDOW}&select=${token("health/heart_rate", "garmin-forerunner-265")},${token("health/heart_rate", "apple-watch-s10")}`,
      KINDS,
      answer
    )
    const panel = await screen.findByRole("region", {
      name: "Timeline of the selected streams",
    })
    expect(await within(panel).findByText("2 streams over time")).toBeTruthy()
    await waitFor(() =>
      expect(server.seen.filter((r) => r.aggregate.metric)).toHaveLength(1)
    )
    const read = server.seen.find((r) => r.aggregate.metric)!
    expect(read.aggregate.metric).toEqual({ ref: HEART })
    expect(read.aggregate.groupBy).toEqual(["source", "channel"])
    expect([...(read.filter.properties?.source?.in ?? [])].sort()).toEqual(
      [APPLE, GARMIN].sort()
    )
    const legend = await within(panel).findByRole("list", {
      name: "Legend, Heart rate",
    })
    expect(
      within(legend)
        .getAllByRole("button")
        .map((b) => b.textContent)
        .sort()
    ).toEqual(["Apple Watch S10", "Garmin Forerunner 265"])
    await waitFor(() =>
      expect(
        panel.querySelectorAll(
          "[data-slot=timeline-chart] path[stroke-width='2']"
        )
      ).toHaveLength(2)
    )
  })

  it("says plainly which streams a third unit leaves out", async () => {
    const server = mount(
      `?timeline=true&${WINDOW}&select=${token("health/heart_rate", "garmin-forerunner-265")},${token("health/body_mass", "withings-body-plus")},${token("health/steps", "garmin-forerunner-265")}`,
      KINDS,
      answer
    )
    const panel = await screen.findByRole("region", {
      name: "Timeline of the selected streams",
    })
    const note = await within(panel).findByRole("note")
    expect(note.textContent).toBe(
      "Up to two units in one chart: Steps (steps) is left out. Deselect it, or deselect Heart rate (/min) or Weight (kg) to draw it here."
    )
    // Nothing is read for the metric left out.
    await waitFor(() =>
      expect(server.seen.filter((r) => r.aggregate.metric)).toHaveLength(2)
    )
    expect(
      server.seen.some((r) => r.aggregate.metric?.ref === STEPS_METRIC)
    ).toBe(false)
  })

  it("says why the panel's own read failed, never that a stream has no points", async () => {
    const heart = token("health/heart_rate", "garmin-forerunner-265")
    mount(`?timeline=true&${WINDOW}&select=${heart}`, KINDS, (read) =>
      // The panel reads the one selected metric's streams on its own.
      !read.aggregate.metric && read.filter.properties?.metric
        ? unavailable()
        : answer(read)
    )
    const panel = await screen.findByRole("region", {
      name: "Timeline of the selected streams",
    })
    expect(
      await within(panel).findByText(/the database is restarting/)
    ).toBeTruthy()
    expect(
      within(panel).getByRole("button", { name: "Try again" })
    ).toBeTruthy()
    expect(panel.textContent).not.toContain("no points any more")
    expect(panel.textContent).not.toContain("Select streams in the table")
    // The table's own read answered.
    await rows()
  })

  it("reads nothing for an empty selection, and no list of metrics for one", async () => {
    const server = mount(
      `?timeline=true&${WINDOW}&filter=${filter("source", GARMIN)}`,
      KINDS,
      answer
    )
    await waitFor(() => expect(bodyRows().length).toBeGreaterThan(0))
    await screen.findByText(/Select streams in the table/)
    // Every stream read is the table's, narrowed to the Garmin.
    const streamReads = () =>
      server.seen.filter(
        (r) => !r.aggregate.metric && r.aggregate.groupBy?.length === 4
      )
    expect(streamReads().length).toBeGreaterThan(0)
    expect(
      streamReads().every((r) => r.filter.properties?.source?.in?.length)
    ).toBe(true)
    // A selection reads its metric's streams, and no list of metrics.
    fireEvent.click(
      within(bodyRows()[0]).getByRole("checkbox", { name: /^Select / })
    )
    await waitFor(() =>
      expect(server.seen.some((r) => r.aggregate.metric)).toBe(true)
    )
    expect(
      server.seen.filter((r) => r.aggregate.groupBy?.join() === "metric")
    ).toEqual([])
  })

  it("draws a selected stream on no channel without the source's other channels", async () => {
    // One day of WHOOP heart rate: 60 on its cycles, 140 in its workouts,
    // 100 where the read merges the two.
    const day = (value: string, channel?: string): AggregateRow => ({
      start: "2026-10-04T00:00:00Z",
      end: "2026-10-05T00:00:00Z",
      form: "gauge",
      group: channel ? { channel } : {},
      values: [{ reducer: "avg", value, unit: "/min" }],
      count: 10,
      bounded: 0,
      synthetic: false,
      unavailable: false,
    })
    const server = mount(
      `?timeline=true&${WINDOW}&select=${token("health/heart_rate", "whoop-4-0")}`,
      KINDS,
      (read) => {
        if (!read.aggregate.metric) return latestAnswer(read, LATEST_CHANNELS)
        const rows = read.aggregate.groupBy?.includes("channel")
          ? [day("60"), day("140", "workout")]
          : [day("100")]
        return { ...LATEST_CHANNELS, rows }
      }
    )
    const panel = await screen.findByRole("region", {
      name: "Timeline of the selected streams",
    })
    await waitFor(() =>
      expect(server.seen.some((r) => r.aggregate.metric)).toBe(true)
    )
    expect(
      server.seen.find((r) => r.aggregate.metric)!.aggregate.groupBy
    ).toEqual(["channel"])
    fireEvent.click(
      await within(panel).findByRole("button", { name: "Show the numbers" })
    )
    const table = within(panel).getByRole("table", {
      name: "Heart rate in numbers",
    })
    expect(within(table).getAllByRole("row")).toHaveLength(2)
    expect(table.textContent).toContain("60")
    expect(table.textContent).not.toContain("140")
  })

  it("keeps a selected stream in the chart while a filter hides its row", async () => {
    const server = mount(
      `?timeline=true&${WINDOW}&filter=${filter("metric", BODY_MASS)}&select=${token("health/heart_rate", "garmin-forerunner-265")}`,
      KINDS,
      answer
    )
    await waitFor(() => expect(bodyRows()).toHaveLength(1))
    expect(bodyRows()[0].textContent).toContain("Weight")
    await waitFor(() =>
      expect(server.seen.some((r) => r.aggregate.metric?.ref === HEART)).toBe(
        true
      )
    )
  })
})
