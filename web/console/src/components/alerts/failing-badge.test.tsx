// @vitest-environment jsdom
/** The failing badge on an agent's, a tool's or a provider's page: nothing
 * while the page's triggers are healthy, and a labeled "Failing" pill with
 * how long and which triggers while one of them fails. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { FailingBadge } from "./failing-badge"
import { triggerStatusesQueryOptions } from "@/lib/api/sync"
import type { TriggerStatus } from "@/lib/api/types"

beforeEach(() => {
  vi.stubGlobal(
    "fetch",
    vi.fn(() => Promise.reject(new Error("offline")))
  )
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

const BOOM = "widgets.test.dev/widgets/boom"

function status(
  id: string,
  health: string,
  failingSince?: string
): TriggerStatus {
  return {
    id,
    kind: "record",
    callable: BOOM,
    enabled: true,
    head: 10,
    parked: 3,
    pending: 0,
    inFlight: 0,
    health,
    failingSince,
  }
}

function renderBadge(statuses: TriggerStatus[]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(triggerStatusesQueryOptions.queryKey, statuses)
  return render(
    <QueryClientProvider client={client}>
      <FailingBadge callables={[BOOM]} />
    </QueryClientProvider>
  )
}

describe("FailingBadge", () => {
  it("renders nothing while the page's triggers are healthy", () => {
    const { container } = renderBadge([status("on-boom", "ok")])
    expect(container.textContent).toBe("")
  })

  it("shows the pill, how long it has failed and the trigger, each labeled", () => {
    const since = new Date(Date.now() - 3 * 60 * 60_000).toISOString()
    renderBadge([status("on-boom", "failing", since)])
    expect(screen.getByText("Failing")).toBeTruthy()
    expect(screen.getByText("Since")).toBeTruthy()
    const age = screen.getByText("3h ago")
    expect(age.getAttribute("title")).toBe(since)
    expect(screen.getByText("Trigger")).toBeTruthy()
    expect(screen.getByText("on-boom")).toBeTruthy()
  })

  it("lists every failing trigger under a plural label", () => {
    const since = new Date(Date.now() - 2 * 60 * 60_000).toISOString()
    renderBadge([
      status("on-boom", "failing", since),
      status("on-boom-hourly", "failing", since),
    ])
    expect(screen.getByText("Triggers")).toBeTruthy()
    expect(screen.getByText("on-boom, on-boom-hourly")).toBeTruthy()
  })
})
