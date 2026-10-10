// @vitest-environment jsdom
/** Home's count of open alerts: nothing while none is open, the count and
 * one link per alert to the page it lives on while some are, and a way to
 * the rest past the first few. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, render, screen, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { NeedsAttention } from "./needs-attention"
import { ALERT_KIND } from "@/lib/alerts"
import { openAlertsQueryOptions } from "@/lib/api/alerts"
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

const CORE = "substrate.reamde.dev/core"

function open(id: string, summary: string, about: string): SubstrateRecord {
  const at = new Date(Date.now() - 5 * 60_000).toISOString()
  return {
    id,
    kind: ALERT_KIND,
    properties: {
      key: id,
      level: "error",
      state: "open",
      summary,
      lastSeenAt: at,
      about: [{ ref: about }],
    },
    labels: {},
    version: 1,
    createdAt: at,
    updatedAt: at,
  }
}

function renderHome(records: SubstrateRecord[], count: number) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: Infinity } },
  })
  client.setQueryData(openAlertsQueryOptions.queryKey, { records, count })
  return render(
    <QueryClientProvider client={client}>
      <NeedsAttention />
    </QueryClientProvider>
  )
}

describe("NeedsAttention", () => {
  it("renders nothing while no alert is open", () => {
    const { container } = renderHome([], 0)
    expect(container.textContent).toBe("")
  })

  it("counts the open alerts and links each to the page it lives on", () => {
    renderHome(
      [
        open(
          "trigger.parked/on-gmail",
          "Trigger on-gmail has parked deliveries to function syncgmail",
          `${CORE}/function/providers.substrate.reamde.dev/google/syncgmail`
        ),
        open(
          "trigger.parked/on-notes",
          "Trigger on-notes has parked deliveries to agent titler",
          `${CORE}/agent/ada.example.com/notes/titler`
        ),
      ],
      2
    )
    const section = screen.getByRole("region", { name: "Needs attention" })
    expect(within(section).getByText("2 open alerts")).toBeTruthy()
    const tool = within(section)
      .getByText("Trigger on-gmail has parked deliveries to function syncgmail")
      .closest("a")
    expect(tool?.getAttribute("data-to")).toBe("/tools/$authority/$pkg/$name")
    expect(JSON.parse(tool?.getAttribute("data-params") ?? "{}")).toEqual({
      authority: "providers.substrate.reamde.dev",
      pkg: "google",
      name: "syncgmail",
    })
    const agent = within(section)
      .getByText("Trigger on-notes has parked deliveries to agent titler")
      .closest("a")
    expect(agent?.getAttribute("data-to")).toBe("/agents/$id")
    expect(JSON.parse(agent?.getAttribute("data-params") ?? "{}")).toEqual({
      id: "ada.example.com/notes/titler",
    })
    expect(section.textContent).not.toContain("more")
  })

  it("names how many more there are past the first five", () => {
    const records = Array.from({ length: 6 }, (_, i) =>
      open(
        `trigger.parked/t${i}`,
        `Trigger t${i} has parked deliveries`,
        `${CORE}/function/a.example.com/p/f${i}`
      )
    )
    renderHome(records, 7)
    const section = screen.getByRole("region", { name: "Needs attention" })
    expect(within(section).getByText("7 open alerts")).toBeTruthy()
    expect(
      within(section).queryByText("Trigger t5 has parked deliveries")
    ).toBeNull()
    expect(section.textContent).toContain("And 2 more.")
    const all = within(section).getByText("See all alerts").closest("a")
    expect(all?.getAttribute("data-to")).toBe("/data/$authority/$pkg/$name")
  })
})
