// @vitest-environment jsdom
/** Hovering a trace's sparkline moves its marker and its readout alone: the
 * line is laid out once for the trace and its width, not once for every
 * value the pointer passes. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render } from "@testing-library/react"
import type { ReactNode } from "react"
import { afterEach, expect, it, vi } from "vitest"

import * as chart from "@/lib/chart"
import { HEART_TRACE, traceKind } from "@/lib/observations.test-fixtures"

import { ObservedSection } from "./observed-section"

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children: ReactNode }) => <a>{children}</a>,
}))

vi.mock("@/lib/chart", async (actual) => {
  const real = await actual<typeof import("@/lib/chart")>()
  return {
    ...real,
    linePath: vi.fn(real.linePath),
    niceDomain: vi.fn(real.niceDomain),
  }
})

afterEach(cleanup)

it("lays the line out once while the pointer passes the values", () => {
  const view = render(
    <QueryClientProvider client={new QueryClient()}>
      <ObservedSection record={HEART_TRACE} kind={traceKind} />
    </QueryClientProvider>
  )
  const svg = view.container.querySelector("[aria-roledescription=chart] svg")!
  vi.mocked(chart.linePath).mockClear()
  vi.mocked(chart.niceDomain).mockClear()
  for (const clientX of [40, 200, 400, 600]) {
    fireEvent.pointerMove(svg, { clientX })
  }
  expect(view.container.querySelector("svg circle")).not.toBe(null)
  expect(chart.linePath).not.toHaveBeenCalled()
  expect(chart.niceDomain).not.toHaveBeenCalled()
})
