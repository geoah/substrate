// @vitest-environment jsdom
/** The streams table: the latest read, one row per stream with its newest
 * value in its unit, the facet filters and the search narrowing the rows,
 * a row opening the timeline on its stream, and the page saying so where
 * nothing records measurements. Served by a stand-in records route. */

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
import type { ReactNode } from "react"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { AggregateResult } from "@/lib/api/types"

import {
  BODY_MASS,
  HEART,
  KINDS,
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

const navigate = vi.fn()

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
  useNavigate: () => navigate,
}))

import { ObservationsPage } from "./observations"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  navigate.mockReset()
})

function mount(
  searchParams = "",
  kinds = KINDS,
  respond: (read: SeenRead) => AggregateResult | Response = (read) =>
    latestAnswer(read)
) {
  const server = observationsServer(respond, kinds)
  vi.stubGlobal("fetch", vi.fn(server.fetch))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <NuqsTestingAdapter searchParams={searchParams}>
        <ObservationsPage />
      </NuqsTestingAdapter>
    </QueryClientProvider>
  )
  return server
}

/** The problem the server names when a latest read makes too many groups. */
const PROBLEM =
  "aggregate.budget.maxGroups: the latest read makes 201 groups, over 200; narrow the filter or raise maxGroups (the cap is 2000)"

/** The server's 422 for a read over its group budget. */
function refusal(): Response {
  return new Response(
    JSON.stringify({
      error: {
        code: "validation",
        message: "the aggregate read was refused",
        problems: [PROBLEM],
      },
    }),
    { status: 422, headers: { "Content-Type": "application/json" } }
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

describe("the streams table", () => {
  it("lists the metrics first, then reads their streams a few at a time", async () => {
    const server = mount()
    await rows()
    expect(server.seen[0]).toEqual({
      filter: { implements: "substrate.reamde.dev/core/observed" },
      aggregate: {
        groupBy: ["metric"],
        reducers: ["last"],
        budget: { maxGroups: 2000 },
      },
    })
    const streamReads = server.seen.filter(
      (r) => r.aggregate.groupBy?.length === 3
    )
    expect(streamReads).toHaveLength(1)
    expect(streamReads[0].aggregate).toEqual({
      groupBy: ["metric", "source", "about"],
      reducers: ["last"],
    })
    expect(
      [...(streamReads[0].filter.properties?.metric?.in ?? [])].sort()
    ).toEqual([BODY_MASS, HEART, RECOVERY, SLEEP_STAGE, STEPS_METRIC].sort())
  })

  it("lists every stream when one read of them all would be refused", async () => {
    // More than 200 streams: the server refuses a latest read that names
    // no metrics. Discovery never sends one.
    const server = mount("", KINDS, (read) =>
      read.aggregate.groupBy?.length === 3 && !read.filter.properties?.metric
        ? refusal()
        : latestAnswer(read)
    )
    expect(await rows()).toHaveLength(6)
    expect(screen.queryByText(/didn’t load/)).toBe(null)
    expect(server.seen.length).toBeGreaterThan(1)
  })

  it("narrows the streams read to the chosen metric on the server", async () => {
    const server = mount(`?metric=${encodeURIComponent(BODY_MASS)}`)
    const table = await screen.findByRole("table")
    await waitFor(() => expect(table.textContent).toContain("Weight"))
    const streamReads = server.seen.filter(
      (r) => r.aggregate.groupBy?.length === 3
    )
    expect(streamReads.map((r) => r.filter.properties)).toEqual([
      { metric: { in: [BODY_MASS] } },
    ])
    // The metric filter still offers the other metrics.
    fireEvent.click(screen.getByRole("button", { name: /^Metric/ }))
    expect(
      await screen.findByRole("option", { name: /Heart rate/ })
    ).toBeTruthy()
  })

  it("lists the chosen metric's streams when only the list of every metric is refused", async () => {
    // The list of every metric is the one read grouped by metric alone.
    mount(`?metric=${encodeURIComponent(BODY_MASS)}`, KINDS, (read) =>
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

  it("narrows to the chosen sources and forms from the address", async () => {
    mount("?source=withings-body-plus")
    const table = await screen.findByRole("table")
    await waitFor(() =>
      expect(within(table).getAllByRole("row").slice(1)).toHaveLength(1)
    )
    expect(table.textContent).toContain("Weight")
    cleanup()
    mount("?form=summary")
    const again = await screen.findByRole("table")
    await waitFor(() => expect(again.textContent).toContain("Recovery"))
    expect(within(again).getAllByRole("row").slice(1)).toHaveLength(1)
  })

  it("searches the streams' words", async () => {
    mount("?q=sleep")
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

  it("opens the timeline narrowed to a row's stream", async () => {
    mount()
    const body = await rows()
    fireEvent.click(body.find((r) => r.textContent?.includes("Weight"))!)
    expect(navigate).toHaveBeenCalledWith({
      to: "/observations/timeline",
      search: { metric: BODY_MASS, source: WITHINGS },
    })
  })

  it("says nothing records measurements where no kind binds the trait", async () => {
    const server = mount("", [metricKind, sourceKind])
    expect(
      await screen.findByText("Nothing here records measurements yet")
    ).toBeTruthy()
    expect(server.seen).toEqual([])
  })
})
