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

import {
  KINDS,
  LATEST,
  metricKind,
  observationsServer,
  sourceKind,
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

function mount(searchParams = "", kinds = KINDS) {
  const server = observationsServer(() => LATEST, kinds)
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

/** The table's body rows, once the streams have landed. */
async function rows(): Promise<HTMLElement[]> {
  const table = await screen.findByRole("table")
  await waitFor(() =>
    expect(within(table).getAllByText("Heart rate").length).toBeGreaterThan(0)
  )
  return within(table).getAllByRole("row").slice(1)
}

describe("the streams table", () => {
  it("asks for the latest read of every observed kind", async () => {
    const server = mount()
    await rows()
    expect(server.seen[0]).toEqual({
      filter: { implements: "substrate.reamde.dev/core/observed" },
      aggregate: { groupBy: ["metric", "source", "about"], reducers: ["last"] },
    })
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
      search: { metric: "health/body_mass", source: "withings-body-plus" },
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
