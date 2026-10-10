// @vitest-environment jsdom
/** The alerts panel on a provider's, an agent's or a tool's page: nothing
 * while nothing is open, and each open alert with its level, summary, last
 * error, labeled times and count, and a link to its own record. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { AlertsPanel } from "./alerts-panel"
import { ALERT_KIND } from "@/lib/alerts"
import { openAlertsAboutQueryOptions } from "@/lib/api/alerts"
import type { SubstrateRecord } from "@/lib/api/types"

vi.mock("@tanstack/react-router", () => ({
  Link: ({
    to,
    params,
    children,
    ...rest
  }: {
    to: string
    params?: Record<string, string>
    children: React.ReactNode
  } & React.AnchorHTMLAttributes<HTMLAnchorElement>) => (
    <a data-to={to} data-params={JSON.stringify(params ?? {})} {...rest}>
      {children}
    </a>
  ),
}))

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

const FN = "substrate.reamde.dev/core/function/widgets.test.dev/widgets/boom"

function alertRecord(
  id: string,
  properties: Record<string, unknown>
): SubstrateRecord {
  const at = new Date(Date.now() - 5 * 60_000).toISOString()
  return {
    id,
    kind: ALERT_KIND,
    properties,
    labels: {},
    version: 1,
    createdAt: at,
    updatedAt: at,
  }
}

function renderPanel(records: SubstrateRecord[]) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(openAlertsAboutQueryOptions([FN]).queryKey, records)
  return render(
    <QueryClientProvider client={client}>
      <AlertsPanel refs={[FN]} />
    </QueryClientProvider>
  )
}

describe("AlertsPanel", () => {
  it("renders nothing while no alert about the page is open", () => {
    const { container } = renderPanel([
      alertRecord("trigger.parked/on-boom", { state: "resolved" }),
    ])
    expect(container.textContent).toBe("")
  })

  it("lists an open alert with each fact under its label", () => {
    const now = Date.now()
    renderPanel([
      alertRecord("trigger.parked/on-boom", {
        key: "trigger.parked/on-boom",
        level: "error",
        state: "open",
        summary: "Trigger on-boom has parked deliveries to function boom",
        detail: "egress blocked\nTraceback (most recent call last)",
        count: 3,
        firstSeenAt: new Date(now - 2 * 3_600_000).toISOString(),
        lastSeenAt: new Date(now - 5 * 60_000).toISOString(),
        about: [{ ref: FN }],
      }),
    ])
    const section = screen.getByRole("region", { name: "Needs attention" })
    expect(within(section).getByText("1 open alert")).toBeTruthy()
    expect(within(section).getByText("Error")).toBeTruthy()
    expect(
      within(section).getByText(
        "Trigger on-boom has parked deliveries to function boom"
      )
    ).toBeTruthy()
    // The first line of the error, labeled, never the traceback under it.
    expect(within(section).getByText("Last error:")).toBeTruthy()
    expect(within(section).getByText("egress blocked")).toBeTruthy()
    expect(section.textContent).not.toContain("Traceback")
    expect(within(section).getByText("First seen")).toBeTruthy()
    expect(within(section).getByText("Last seen")).toBeTruthy()
    expect(within(section).getByText("Parked deliveries")).toBeTruthy()
    expect(within(section).getByText("3")).toBeTruthy()
    const link = within(section).getByText("Full details").closest("a")
    expect(link?.getAttribute("data-to")).toBe(
      "/data/$authority/$pkg/$name/$id"
    )
    expect(JSON.parse(link?.getAttribute("data-params") ?? "{}")).toEqual({
      authority: "substrate.reamde.dev",
      pkg: "core",
      name: "alert",
      id: "trigger.parked/on-boom",
    })
  })
})
