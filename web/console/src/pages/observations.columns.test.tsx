// @vitest-environment jsdom
/** The streams grid's columns keep their identity across a render that
 * changes neither the selection nor the streams (a column being resized),
 * so the table's column model is not rebuilt on every pointer move. */

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
import { afterEach, expect, it, vi } from "vitest"

import * as table from "@/components/data-table/data-table"
import { ConsolePreferencesContext } from "@/hooks/use-console-preferences"
import { DEFAULT_SETTINGS } from "@/lib/console-preferences"
import {
  latestAnswer,
  observationsServer,
} from "@/lib/observations.test-fixtures"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a href="#">{children}</a>,
  useNavigate: () => vi.fn(),
}))

vi.mock("@/components/data-table/data-table", async (actual) => {
  const real =
    await actual<typeof import("@/components/data-table/data-table")>()
  return { ...real, useDataTable: vi.fn(real.useDataTable) }
})

import { ObservationsPage } from "./observations"

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  localStorage.clear()
})

it("keeps its columns while a column is resized", async () => {
  const server = observationsServer((read) => latestAnswer(read))
  vi.stubGlobal("fetch", vi.fn(server.fetch))
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <ConsolePreferencesContext.Provider
      value={{
        preferences: {
          collapsed: [],
          favorites: [],
          sidebarOpen: true,
          ...DEFAULT_SETTINGS,
          technicalDetails: false,
          views: [],
        },
        busy: false,
        change: () => {},
        set: () => {},
      }}
    >
      <QueryClientProvider client={client}>
        <NuqsTestingAdapter hasMemory>
          <ObservationsPage />
        </NuqsTestingAdapter>
      </QueryClientProvider>
    </ConsolePreferencesContext.Provider>
  )
  const grid = await screen.findByRole("table")
  // Settled: the rows, and the sources' titles read after them.
  await waitFor(() =>
    expect(
      within(grid).getAllByText("Garmin Forerunner 265").length
    ).toBeGreaterThan(0)
  )
  const columns = () =>
    vi.mocked(table.useDataTable).mock.calls.map(([o]) => o.columns)
  const before = columns().at(-1)
  const calls = columns().length
  const handle = within(grid)
    .getAllByRole("columnheader")[1]
    .querySelector("[role=presentation]")!
  fireEvent.pointerDown(handle, { clientX: 300 })
  fireEvent.pointerMove(window, { clientX: 340 })
  fireEvent.pointerUp(window, { clientX: 340 })
  await waitFor(() => expect(columns().length).toBeGreaterThan(calls))
  expect(columns().at(-1)).toBe(before)
})
