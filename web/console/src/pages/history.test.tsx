// @vitest-environment jsdom
/** A view of History that filters by who made a change waits for the whole
 * actor directory: while it loads it shows it is loading, a failed read (any
 * page of it) says so with a way to try again, and only a set that loaded
 * and holds nobody of the view's type reads as nobody having changed
 * anything. */

import { QueryClient, QueryClientProvider } from "@tanstack/react-query"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { NuqsTestingAdapter } from "nuqs/adapters/testing"
import { afterEach, describe, expect, it, vi } from "vitest"

import type { ChangeFeedFilter } from "@/lib/api/changes"
import type { HistoryFeedOptions } from "@/hooks/use-history-feed"

const feeds: { filter: ChangeFeedFilter; enabled?: boolean }[] = []

vi.mock("@/hooks/use-history-feed", () => ({
  useEverydayChanges: () => ({}),
  useHistoryFeed: (filter: ChangeFeedFilter, opts: HistoryFeedOptions) => {
    feeds.push({ filter, enabled: opts.enabled })
    return {
      entries: [],
      status: "off",
      isPending: false,
      error: null,
      hasOlder: false,
      loadingOlder: false,
      loadOlder: () => {},
      retry: () => {},
    }
  },
}))
vi.mock("@/hooks/use-console-preferences", async (importOriginal) => ({
  ...(await importOriginal<typeof import("@/hooks/use-console-preferences")>()),
  useTechnicalDetails: () => [false, () => {}],
}))
vi.mock("@/components/changelog/changelog-panel", () => ({
  ChangelogPanel: () => null,
}))

import { HistoryPage } from "./history"

function actor(id: string, type: string) {
  return {
    kind: "substrate.reamde.dev/core/actor",
    id,
    properties: { type, name: id },
    labels: {},
    version: 1,
    createdAt: "2026-11-13T09:00:00Z",
    updatedAt: "2026-11-13T09:00:00Z",
  }
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  })
}

function renderAt(view: string) {
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  })
  render(
    <QueryClientProvider client={client}>
      <NuqsTestingAdapter searchParams={`?view=${view}`}>
        <HistoryPage />
      </NuqsTestingAdapter>
    </QueryClientProvider>
  )
}

const YOU_EMPTY = "You haven’t changed anything yet."

afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  feeds.length = 0
})

describe("History's views by actor", () => {
  it("show they are loading while the directory loads", () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(() => new Promise<Response>(() => {}))
    )
    renderAt("you")
    expect(document.querySelector("[aria-busy]")).not.toBeNull()
    expect(screen.queryByText(YOU_EMPTY)).toBeNull()
    expect(feeds.every((f) => !f.enabled)).toBe(true)
  })

  it("say a later page failed, and try again", async () => {
    let failing = true
    vi.stubGlobal(
      "fetch",
      vi.fn(async (input: RequestInfo | URL) => {
        const after = new URL(
          String(input),
          "http://console.test"
        ).searchParams.get("after")
        if (!after)
          return json({
            records: [actor("agent-1", "agent")],
            cursor: "c1",
            head: 9,
            generation: "g",
          })
        if (failing)
          return json(
            { error: { code: "unavailable", message: "the server is busy" } },
            503
          )
        return json({
          records: [actor("person-1", "person")],
          head: 9,
          generation: "g",
        })
      })
    )
    renderAt("you")
    expect(
      await screen.findByText(/Who made each change didn’t load/)
    ).toBeTruthy()
    expect(screen.queryByText(YOU_EMPTY)).toBeNull()
    expect(feeds.every((f) => !f.enabled)).toBe(true)

    failing = false
    fireEvent.click(screen.getByRole("button", { name: "Try again" }))
    await vi.waitFor(() =>
      expect(feeds.at(-1)).toEqual({
        filter: { actors: ["person-1"] },
        enabled: true,
      })
    )
    expect(screen.queryByText(/didn’t load/)).toBeNull()
  })

  it("read as nobody's only once the whole set loaded without one", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () =>
        json({
          records: [actor("person-1", "person")],
          head: 9,
          generation: "g",
        })
      )
    )
    renderAt("agents")
    expect(
      await screen.findByText("None of your agents has changed anything yet.")
    ).toBeTruthy()
    expect(feeds.every((f) => !f.enabled)).toBe(true)
  })
})
